import AppKit

@MainActor @main enum NagiApp {
    private static let appDelegate = MenuAppDelegate()
    static func main() {
        let app = NSApplication.shared
        app.delegate = appDelegate
        app.run()
    }
}

@MainActor final class MenuAppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private let model = AppModel()
    private let groupIcons = GroupIconLoader()
    private var statusItem: NSStatusItem!
    private let menu = NSMenu()
    private var pulse: Timer?
    private var menuRefreshTask: Task<Void, Never>?
    private var menuTracking = false
    private var deferredMenuRebuild = false
    private var loadingTimer: Timer?
    private var loadingFrame = 0

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.menu = menu
        menu.delegate = self
        model.onChange = { [weak self] in
            guard let self else { return }
            self.renderStatus()
            // Model refreshes can complete while the menu is detached during
            // an operation. Rebuild the tree before it is opened again so
            // profile override checkmarks reflect the completed CLI action.
            if !self.menuTracking && !self.model.busy { self.rebuildMenu() }
        }
        renderStatus()
        pulse = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.model.ensureFreshTraffic(); self?.renderStatus() }
        }
        NSWorkspace.shared.notificationCenter.addObserver(self, selector: #selector(willSleep(_:)),
            name: NSWorkspace.willSleepNotification, object: nil)
        NSWorkspace.shared.notificationCenter.addObserver(self, selector: #selector(didWake(_:)),
            name: NSWorkspace.didWakeNotification, object: nil)
        model.launch()
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        model.quit { succeeded in
            sender.reply(toApplicationShouldTerminate: succeeded)
        }
        return .terminateLater
    }

    private func renderStatus() {
        guard let button = statusItem?.button else { return }
        if model.busy {
            menu.cancelTracking()
            if statusItem.menu != nil { statusItem.menu = nil }
        } else {
            if statusItem.menu !== menu { statusItem.menu = menu }
        }
        if model.applyingSetting {
            if loadingTimer == nil {
                loadingFrame = 0
                loadingTimer = Timer.scheduledTimer(withTimeInterval: 0.4, repeats: true) { [weak self] _ in
                    Task { @MainActor [weak self] in
                        guard let self else { return }
                        self.loadingFrame = (self.loadingFrame + 1) % 2
                        self.renderStatus()
                    }
                }
            }
        } else {
            loadingTimer?.invalidate()
            loadingTimer = nil
            loadingFrame = 0
        }
        let opacity: CGFloat = model.applyingSetting
            ? (loadingFrame == 0 ? 1 : 0.5)
            : (model.state == .running ? 1 : 0.5)
        let stacked = model.display == .both
        button.title = ""
        button.imagePosition = .imageOnly
        let image = StatusItemImage.make(rates: stacked ? model.statusTitle : nil, opacity: opacity)
        button.image = image
        statusItem.length = stacked ? (image?.size.width ?? NSStatusItem.variableLength) : NSStatusItem.variableLength
        let state: String
        switch model.state {
        case .starting: state = "starting"
        case .running: state = "running"
        case .stopped: state = "stopped"
        case .unavailable: state = "status unavailable"
        }
        button.setAccessibilityLabel(model.applyingSetting ? "Nagi, applying setting" : "Nagi \(state), \(model.accessibilityTraffic)")
    }

    func menuShouldOpen(_ menu: NSMenu) -> Bool { !model.busy }

    func menuWillOpen(_ menu: NSMenu) {
        // Build the initial tree before AppKit starts tracking submenu items.
        rebuildMenu()
        menuTracking = true
        deferredMenuRebuild = false
    }

    func menuDidClose(_ menu: NSMenu) {
        menuTracking = false
        if deferredMenuRebuild {
            deferredMenuRebuild = false
            rebuildMenu()
        }
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        if !menuTracking { rebuildMenu() }
        guard menuRefreshTask == nil else { return }
        menuRefreshTask = Task {
            await model.refreshMenuData()
            if menuTracking {
                deferredMenuRebuild = true
            } else {
                rebuildMenu()
            }
            menuRefreshTask = nil
        }
    }

    private func rebuildMenu() {
        menu.removeAllItems()
        let errors = [model.quitErrorMessage, model.errorMessage].compactMap { $0 }
        let stateTitle: String
        switch model.state {
        case .starting: stateTitle = "Nagi · Starting…"
        case .running: stateTitle = errors.isEmpty ? "Nagi · Running" : "Nagi · Running ⚠"
        case .stopped: stateTitle = "Nagi · Stopped"
        case .unavailable: stateTitle = "Nagi · Status unavailable"
        }
        let status = NSMenuItem(title: stateTitle, action: nil, keyEquivalent: "")
        if !errors.isEmpty {
            let details = NSMenu()
            for error in errors {
                let item = NSMenuItem(title: "⚠ \(error)", action: nil, keyEquivalent: "")
                item.isEnabled = false; item.toolTip = error; details.addItem(item)
            }
            status.submenu = details
        } else {
            status.isEnabled = false
        }
        menu.addItem(status)
        if let notice = model.notice {
            let item = NSMenuItem(title: notice, action: nil, keyEquivalent: "")
            item.isEnabled = false; menu.addItem(item)
        }
        if model.state != .running {
            add("Retry Start", action: #selector(retryStart), enabled: !model.busy)
        }
        menu.addItem(.separator())
        add("System Proxy", command: ["system-proxy", model.systemProxy == true ? "disable" : "enable"],
            checked: model.systemProxy == true, enabled: model.state == .running && model.systemProxy != nil && !model.busy)
        add("TUN", command: ["tun", model.tun == true ? "disable" : "enable"],
            checked: model.tun == true, enabled: model.state == .running && model.tun != nil && !model.busy)
        if model.tun == true && model.tunAdapter != "up" {
            let warning = NSMenuItem(title: "TUN adapter: \(model.tunAdapter ?? "unknown") · Check logs in CLI", action: nil, keyEquivalent: "")
            warning.isEnabled = false; menu.addItem(warning)
        }
        let modeMenu = NSMenu()
        for value in ["rule", "global", "direct"] {
            add(value.capitalized, command: ["mode", value], checked: model.mode == value,
                enabled: model.state == .running && !model.busy, to: modeMenu)
        }
        addSubmenu("Mode", modeMenu, enabled: model.state == .running)

        let groupsMenu = NSMenu()
        if model.groups.allSatisfy(\.hidden) { disabled("No visible proxy groups", to: groupsMenu) }
        for group in model.groups {
            if group.hidden { continue }
            let choices = NSMenu()
            let automatic = group.automatic
            if automatic {
                disabled(group.fixed.map { "Fixed: \($0)" } ?? "Automatic: \(group.selected)", to: choices)
                if let status = group.selectedStatus, status != "available" { disabled("Selection: \(status)", to: choices) }
                choices.addItem(.separator())
            }
            if group.selectable {
                for node in group.nodes {
                    guard let command = group.selectCommand(node) else { continue }
                    add(node, command: command,
                        checked: group.selected == node, enabled: !model.busy, to: choices)
                }
            } else {
                disabled("\(group.kind) · \(group.selected)", to: choices)
                if !group.nodes.isEmpty { choices.addItem(.separator()) }
                for node in group.nodes {
                    let item = NSMenuItem(title: node, action: nil, keyEquivalent: "")
                    item.state = group.selected == node ? .on : .off
                    item.isEnabled = false
                    choices.addItem(item)
                }
            }
            if let restoreCommand = group.restoreAutomaticCommand {
                choices.addItem(.separator())
                let automatic = add("Restore Automatic Selection", command: restoreCommand,
                    enabled: !model.busy, to: choices)
                automatic.toolTip = "Clear the fixed choice through mihomo"
            }
            if group.nodes.isEmpty { disabled("No nodes in this group", to: choices) }
            let groupItem = addSubmenu(group.name, choices, to: groupsMenu)
            if let icon = group.icon {
                groupItem.representedObject = icon
                let image = groupIcons.image(for: icon) { [weak groupItem] image in
                    guard let groupItem, groupItem.representedObject as? String == icon else { return }
                    groupItem.image = image
                }
                if let image {
                    image.size = NSSize(width: 14, height: 14)
                    groupItem.image = image
                }
            }
        }
        addSubmenu("Proxy Groups", groupsMenu, enabled: model.state == .running)
        let providersMenu = NSMenu()
        add("Add Provider…", action: #selector(addProvider), enabled: !model.busy, to: providersMenu)
        providersMenu.addItem(.separator())
        if model.providersUnavailable { disabled("Proxy providers unavailable", to: providersMenu) }
        else if model.providers.isEmpty { disabled("No proxy providers", to: providersMenu) }
        for provider in model.providers {
            let actions = NSMenu()
            add("Update", command: provider.updateCommand, enabled: model.state == .running && !model.busy, to: actions)
            add("Healthcheck", command: provider.healthcheckCommand, enabled: model.state == .running && !model.busy, to: actions)
            let metadata = NSMenuItem(title: "\(provider.type) · \(provider.nodes.count) nodes", action: nil, keyEquivalent: "")
            metadata.isEnabled = false; actions.addItem(metadata)
            addSubmenu(provider.name, actions, to: providersMenu)
        }
        addSubmenu("Proxy Providers", providersMenu, enabled: !model.busy)
        menu.addItem(.separator())

        let profileMenu = NSMenu()
        if model.profiles.isEmpty { disabled("No profiles", to: profileMenu) }
        for name in model.profiles {
            if name == "default" {
                let actions = NSMenu()
                add("Use Default Profile", action: #selector(switchToDefaultProfile),
                    checked: model.profile == name, enabled: !model.busy, to: actions)
                add("Apply Local Default Settings to Current Profile", action: #selector(applyDefaultOverride),
                    checked: model.defaultOverrideApplied == true,
                    enabled: !model.busy && model.defaultOverrideApplied != nil, to: actions)
                addSubmenu(name, actions, enabled: !model.busy, to: profileMenu)
            } else {
                add(name, command: ["profile", "use", name], checked: model.profile == name,
                    enabled: !model.busy, to: profileMenu)
            }
        }
        addSubmenu("Profiles", profileMenu, enabled: !model.profiles.isEmpty)

        let subscriptionMenu = NSMenu()
        add("Add and Apply Subscription…", action: #selector(addAndApplySubscription), enabled: !model.busy, to: subscriptionMenu)
        if model.subscriptions.isEmpty { disabled("No saved subscriptions", to: subscriptionMenu) }
        else { subscriptionMenu.addItem(.separator()) }
        for subscription in model.subscriptions {
            let actions = NSMenu()
            let quota = Self.quotaLabel(subscription)
            let quotaItem = NSMenuItem(title: quota, action: nil, keyEquivalent: ""); quotaItem.isEnabled = false; actions.addItem(quotaItem)
            if let expiry = subscription.expiresAt { let expiryItem = NSMenuItem(title: "Expires: \(Self.subscriptionTime(expiry))", action: nil, keyEquivalent: ""); expiryItem.isEnabled = false; actions.addItem(expiryItem) }
            disabled(subscription.schedulerRunning ? (subscription.nextDue.map { "Next update: \(Self.subscriptionTime($0))" } ?? "Next update: pending") : "Scheduler: not running", to: actions)
            let updateAndApply = add("Update and Apply", action: #selector(updateAndApplySubscription(_:)),
                                     enabled: !model.busy, to: actions)
            updateAndApply.representedObject = subscription.name
            updateAndApply.toolTip = "Replaces configuration and clears local overrides and saved selections; does not start mihomo or enable traffic access."
            actions.addItem(.separator())
            let interval = subscription.scheduleSeconds > 0 ? "Schedule: every \(subscription.scheduleSeconds)s" : "Schedule: off"
            add(interval, action: #selector(scheduleSubscription(_:)), enabled: !model.busy, to: actions).representedObject = subscription.name
            add("Schedule: every hour", action: #selector(scheduleHour(_:)), enabled: !model.busy, to: actions).representedObject = subscription.name
            add(subscription.keyConfigured ? "Clear Encryption Key" : "Set Encryption Key…", action: #selector(subscriptionKey(_:)), enabled: !model.busy, to: actions).representedObject = subscription.name
            if let pause = subscription.pauseReason { disabled("Auto-apply paused: \(pause)", to: actions) }
            if let stage = subscription.lastFailureStage { disabled("Last failure: \(stage)/\(subscription.lastFailureReason ?? "unknown")", to: actions) }
            actions.addItem(.separator())
            let delete = add("Delete Subscription…", action: #selector(deleteSubscription(_:)),
                             enabled: !model.busy, to: actions)
            delete.representedObject = subscription.name
            addSubmenu(subscription.name, actions, to: subscriptionMenu)
        }
        addSubmenu("Subscriptions", subscriptionMenu)
        menu.addItem(.separator())

        let displayMenu = NSMenu()
        for option in TrafficDisplay.allCases {
            let item = add(option.title, action: #selector(setDisplay(_:)), checked: model.display == option, to: displayMenu)
            item.representedObject = option.rawValue
        }
        addSubmenu("Display", displayMenu)
        let startupMenu = NSMenu()
        add("Open menu bar app at login", action: #selector(toggleAppLogin), checked: model.appLoginEnabled, to: startupMenu)
        let startupCommand = model.serviceInstalled == true ? ["service", "uninstall"] : ["service", "install"]
        add("Run engine at login", command: startupCommand,
            checked: model.serviceInstalled == true, enabled: model.serviceInstalled != nil && !model.busy, to: startupMenu)
        if model.serviceInstalled == nil { disabled("Engine service status unavailable", to: startupMenu) }
        addSubmenu("Login", startupMenu)
        menu.addItem(.separator())
        add("Help", action: #selector(openHelp))
        add("About Nagi", action: #selector(showAbout))
        add("Quit Nagi", action: #selector(quit), enabled: !model.busy)
    }

    @discardableResult private func add(_ title: String, action: Selector, checked: Bool = false,
                                        enabled: Bool = true, to target: NSMenu? = nil) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
        item.target = self; item.state = checked ? .on : .off; item.isEnabled = enabled
        (target ?? menu).addItem(item)
        return item
    }
    @discardableResult private func add(_ title: String, command: [String], checked: Bool = false,
                                        enabled: Bool = true, to target: NSMenu? = nil) -> NSMenuItem {
        let item = add(title, action: #selector(runCommand(_:)), checked: checked, enabled: enabled, to: target)
        item.representedObject = command
        return item
    }
    @discardableResult private func addSubmenu(_ title: String, _ submenu: NSMenu, enabled: Bool = true, to target: NSMenu? = nil) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.submenu = submenu; item.isEnabled = enabled
        (target ?? menu).addItem(item)
        return item
    }
    static func quotaLabel(_ sub: Subscription) -> String {
        guard sub.total > 0 else { return "Quota: unknown" }
        let (sum, overflow) = max(0, sub.upload).addingReportingOverflow(max(0, sub.download))
        let used = overflow ? Int64.max : sum
        let available = max(0, sub.total - used)
        return "Quota: \(subscriptionBytes(available)) available / \(subscriptionBytes(sub.total)) total (\(subscriptionBytes(sub.upload)) uploaded, \(subscriptionBytes(sub.download)) downloaded)"
    }
    static func subscriptionBytes(_ bytes: Int64) -> String {
        guard bytes >= 0 else { return "unknown" }
        guard bytes >= 1024 else { return "\(bytes) B" }
        let units = ["KiB", "MiB", "GiB", "TiB", "PiB", "EiB"]
        var value = Double(bytes) / 1024
        var unit = 0
        while value >= 1024 && unit < units.count - 1 {
            value /= 1024
            unit += 1
        }
        var number = String(format: "%.2f", locale: Locale(identifier: "en_US_POSIX"), value)
        while number.hasSuffix("0") { number.removeLast() }
        if number.hasSuffix(".") { number.removeLast() }
        return "\(number) \(units[unit])"
    }
    static func subscriptionTime(_ raw: String, timeZone: TimeZone = .current) -> String {
        // Fractions do not affect minute precision. Removing them before parsing
        // also supports Go's nanoseconds and feeds with higher fractional precision.
        let pattern = #"^(\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d)(?:\.[0-9]+)?(Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z"#
        guard let expression = try? NSRegularExpression(pattern: pattern),
              let match = expression.firstMatch(in: raw, range: NSRange(raw.startIndex..., in: raw)),
              let seconds = Range(match.range(at: 1), in: raw),
              let zone = Range(match.range(at: 2), in: raw) else { return "unknown" }
        let parser = DateFormatter()
        parser.locale = Locale(identifier: "en_US_POSIX")
        parser.calendar = Calendar(identifier: .gregorian)
        parser.timeZone = TimeZone(secondsFromGMT: 0)
        parser.dateFormat = "yyyy-MM-dd'T'HH:mm:ssXXXXX"
        parser.isLenient = false
        guard let date = parser.date(from: String(raw[seconds]) + String(raw[zone])) else { return "unknown" }
        let display = DateFormatter()
        display.locale = Locale(identifier: "en_US_POSIX")
        display.calendar = Calendar(identifier: .gregorian)
        display.timeZone = timeZone
        display.dateFormat = "yyyy-MM-dd HH:mm"
        return display.string(from: date)
    }
    private func disabled(_ title: String, to target: NSMenu) {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.isEnabled = false; target.addItem(item)
    }
    @objc private func runCommand(_ sender: NSMenuItem) {
        guard let command = sender.representedObject as? [String] else { return }
        model.perform(command)
    }
    @objc private func switchToDefaultProfile() { model.switchToDefaultProfile() }
    @objc private func applyDefaultOverride() {
        if model.defaultOverrideApplied == true {
            model.applyDefaultOverrideToCurrentProfile()
            return
        }
        guard model.dnsProxyRequired else {
            model.applyDefaultOverrideToCurrentProfile()
            return
        }
        guard !model.dnsProxyGroups.isEmpty else {
            model.applyDefaultOverrideToCurrentProfile()
            return
        }
        let alert = NSAlert()
        alert.messageText = "Choose DNS Proxy Group"
        alert.informativeText = "Primary DNS queries will follow the group's selected proxy node."
        let popup = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 320, height: 26), pullsDown: false)
        popup.addItems(withTitles: model.dnsProxyGroups)
        if !model.dnsProxyGroup.isEmpty, let index = model.dnsProxyGroups.firstIndex(of: model.dnsProxyGroup) { popup.selectItem(at: index) }
        alert.accessoryView = popup
        alert.addButton(withTitle: "Apply")
        alert.addButton(withTitle: "Cancel")
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertFirstButtonReturn, let group = popup.selectedItem?.title {
            model.applyDefaultOverrideToCurrentProfile(group: group)
        }
    }
    @objc private func updateAndApplySubscription(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        model.updateAndApplySubscription(name)
    }
    @objc private func scheduleSubscription(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String,
              let sub = model.subscriptions.first(where: { $0.name == name }) else { return }
        model.scheduleSubscription(name, seconds: sub.scheduleSeconds > 0 ? nil : 3600)
    }
    @objc private func scheduleHour(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        model.scheduleSubscription(name, seconds: 3600)
    }
    @objc private func subscriptionKey(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String,
              let sub = model.subscriptions.first(where: { $0.name == name }) else { return }
        if sub.keyConfigured { model.clearSubscriptionKey(name); return }
        let panel = NSOpenPanel(); panel.canChooseFiles = true; panel.canChooseDirectories = false; panel.allowsMultipleSelection = false
        NSApp.activate(ignoringOtherApps: true)
        if panel.runModal() == .OK, let url = panel.url { model.setSubscriptionKey(name, file: url.path) }
    }
    @objc private func addProvider() {
        guard !model.busy else { return }
        let alert = NSAlert()
        alert.messageText = "Add Provider"
        let instructions = "Add an HTTP(S) provider to the selected profile. Choose an existing group or enter a new name. A new Select group includes DIRECT and leaves routing rules unchanged. Its choices appear in Select Node (Proxy Groups); use Global mode or existing routing rules to route through it. Saving while stopped does not start the engine."
        alert.informativeText = instructions
        alert.addButton(withTitle: "Add Provider")
        alert.addButton(withTitle: "Cancel")
        let accessory = NSView(frame: NSRect(x: 0, y: 0, width: 400, height: 140))
        func label(_ title: String, y: CGFloat) {
            let field = NSTextField(labelWithString: title)
            field.frame = NSRect(x: 0, y: y, width: 85, height: 24)
            accessory.addSubview(field)
        }
        label("Name", y: 108)
        let nameField = NSTextField(frame: NSRect(x: 90, y: 108, width: 310, height: 24))
        nameField.placeholderString = "Provider name"
        accessory.addSubview(nameField)
        label("URL", y: 74)
        let urlField = NSTextField(frame: NSRect(x: 90, y: 74, width: 310, height: 24))
        urlField.placeholderString = "https://example.com/provider.yaml"
        accessory.addSubview(urlField)
        label("Group", y: 40)
        let groupField = NSComboBox(frame: NSRect(x: 90, y: 40, width: 310, height: 24))
        groupField.isEditable = true
        groupField.placeholderString = "Existing or new group name"
        groupField.addItems(withObjectValues: model.groups.filter {
            ["selector", "urltest", "fallback", "loadbalance"].contains($0.kind.lowercased())
        }.map(\.name))
        accessory.addSubview(groupField)
        label("Interval (s)", y: 6)
        let intervalField = NSTextField(frame: NSRect(x: 90, y: 6, width: 310, height: 24))
        intervalField.stringValue = "3600"
        accessory.addSubview(intervalField)
        alert.accessoryView = accessory
        alert.window.initialFirstResponder = nameField
        NSApp.activate(ignoringOtherApps: true)
        Task { @MainActor in
            // Reuse the fields after local or CLI validation errors, retaining private input.
            while alert.runModal() == .alertFirstButtonReturn {
                let name = nameField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                let url = urlField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                let group = groupField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                guard name.range(of: "^[A-Za-z0-9_-]{1,128}$", options: .regularExpression) != nil else {
                    alert.informativeText = "Name must be 1–128 ASCII letters, numbers, underscores, or hyphens."
                    alert.window.initialFirstResponder = nameField
                    continue
                }
                guard let components = URLComponents(string: url),
                      ["http", "https"].contains(components.scheme?.lowercased() ?? ""),
                      components.host?.isEmpty == false else {
                    alert.informativeText = "Enter a valid HTTP or HTTPS URL."
                    alert.window.initialFirstResponder = urlField
                    continue
                }
                guard components.user == nil && components.password == nil && components.fragment == nil else {
                    alert.informativeText = "Use an HTTP or HTTPS URL without a username, password, or fragment."
                    alert.window.initialFirstResponder = urlField
                    continue
                }
                guard !group.isEmpty, group.utf8.count <= 128,
                      group.unicodeScalars.allSatisfy({ scalar in
                          !CharacterSet.controlCharacters.contains(scalar) && !CharacterSet.newlines.contains(scalar)
                      }) else {
                    alert.informativeText = "Choose an existing group or enter a printable group name of at most 128 UTF-8 bytes. Spaces inside the name are allowed."
                    alert.window.initialFirstResponder = groupField
                    continue
                }
                guard let interval = Int64(intervalField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)),
                      (1...31_536_000).contains(interval) else {
                    alert.informativeText = "Update interval must be 1–31536000 seconds."
                    alert.window.initialFirstResponder = intervalField
                    continue
                }
                if let error = await model.addProvider(name, url: url, group: group, intervalSeconds: interval) {
                    alert.informativeText = error
                    continue
                }
                return
            }
        }
    }
    @objc private func addAndApplySubscription() {
        let alert = NSAlert()
        alert.messageText = "Add and Apply Subscription"
        alert.informativeText = "Save, download, and apply this subscription. Applying replaces the target configuration and clears local overrides and saved selections. This does not start mihomo or enable traffic access."
        alert.addButton(withTitle: "Add and Apply")
        alert.addButton(withTitle: "Cancel")

        let accessory = NSView(frame: NSRect(x: 0, y: 0, width: 360, height: 70))
        let nameLabel = NSTextField(labelWithString: "Name")
        nameLabel.frame = NSRect(x: 0, y: 42, width: 55, height: 22)
        accessory.addSubview(nameLabel)
        let nameField = NSTextField(frame: NSRect(x: 60, y: 42, width: 300, height: 22))
        nameField.placeholderString = "Subscription name"
        accessory.addSubview(nameField)
        let urlLabel = NSTextField(labelWithString: "URL")
        urlLabel.frame = NSRect(x: 0, y: 8, width: 55, height: 22)
        accessory.addSubview(urlLabel)
        let urlField = NSTextField(frame: NSRect(x: 60, y: 8, width: 300, height: 22))
        urlField.placeholderString = "https://example.com/subscription"
        accessory.addSubview(urlField)
        alert.accessoryView = accessory
        alert.window.initialFirstResponder = nameField
        NSApp.activate(ignoringOtherApps: true)

        while alert.runModal() == .alertFirstButtonReturn {
            let name = nameField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            let url = urlField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard name.range(of: "^[A-Za-z0-9_-]{1,128}$", options: .regularExpression) != nil else {
                alert.informativeText = "Name must be 1–128 ASCII letters, numbers, underscores, or hyphens."
                alert.window.initialFirstResponder = nameField
                continue
            }
            guard !model.subscriptions.contains(where: { $0.name == name }) else {
                alert.informativeText = "A subscription with this name already exists. Choose another name."
                alert.window.initialFirstResponder = nameField
                continue
            }
            guard let components = URLComponents(string: url),
                  ["http", "https"].contains(components.scheme?.lowercased() ?? ""),
                  components.host?.isEmpty == false else {
                alert.informativeText = "Enter a valid HTTP or HTTPS URL."
                alert.window.initialFirstResponder = urlField
                continue
            }
            guard components.user == nil && components.password == nil else {
                alert.informativeText = "Remove the username and password from the URL."
                alert.window.initialFirstResponder = urlField
                continue
            }
            model.addAndApplySubscription(name, url: url)
            return
        }
    }
    @objc private func deleteSubscription(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        let alert = NSAlert()
        alert.messageText = "Delete subscription “\(name)”?"
        alert.informativeText = "This removes the subscription record and cached download. Any profile already applied from this subscription is kept."
        alert.alertStyle = .warning
        alert.addButton(withTitle: "Delete Subscription")
        alert.addButton(withTitle: "Cancel")
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertFirstButtonReturn { model.removeSubscription(name) }
    }
    @objc private func retryStart() { model.retryStart() }
    @objc private func setDisplay(_ sender: NSMenuItem) {
        if let value = sender.representedObject as? String, let display = TrafficDisplay(rawValue: value) { model.display = display }
    }
    @objc private func toggleAppLogin() { model.setAppLogin(!model.appLoginEnabled) }
    @objc private func openHelp() {
        NSWorkspace.shared.open(URL(string: "https://github.com/ronigooja/Nagi#documentation")!)
    }
    @objc private func showAbout() {
        let alert = NSAlert()
        alert.alertStyle = .informational
        alert.window.title = "About Nagi"
        alert.messageText = "Nagi"
        alert.icon = NSApp.applicationIconImage
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        alert.informativeText = "\(version.map { "Version \($0)" } ?? "Version unavailable")\n\nA menu bar proxy client for macOS."
        alert.addButton(withTitle: "Close")
        let updateButton = alert.addButton(withTitle: "Update")
        updateButton.isEnabled = !model.busy
        updateButton.toolTip = "Updates Nagi in Terminal, then reopens this app."
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertSecondButtonReturn { updateNagi() }
    }
    @objc private func quit() { NSApp.terminate(nil) }
    @objc private func updateNagi() {
        model.updateNagi { NSApp.terminate(nil) }
    }
    @objc private func willSleep(_ notification: Notification) { model.pauseForSleep() }
    @objc private func didWake(_ notification: Notification) { model.resumeAfterWake() }
}
