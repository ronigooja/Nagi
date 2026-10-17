import AppKit

@MainActor @main enum NagiApp {
    private static let appDelegate = MenuAppDelegate()
    static func main() {
        let app = NSApplication.shared
        app.delegate = appDelegate
        app.mainMenu = makeMainMenu()
        app.run()
    }

    private static func makeMainMenu() -> NSMenu {
        let mainMenu = NSMenu()
        let appItem = NSMenuItem(title: "Nagi", action: nil, keyEquivalent: "")
        let appMenu = NSMenu(title: "Nagi")
        appMenu.addItem(withTitle: "Quit Nagi", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        mainMenu.addItem(appItem)

        // Accessory apps still need a main menu to route editing shortcuts to
        // the focused field editor, including while an NSAlert runs modally.
        let editItem = NSMenuItem(title: "Edit", action: nil, keyEquivalent: "")
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        let redo = editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        // Leave targets nil so AppKit uses the responder chain.
        editItem.submenu = editMenu
        mainMenu.addItem(editItem)
        let windowItem = NSMenuItem(title: "Window", action: nil, keyEquivalent: "")
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Close Window", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowItem.submenu = windowMenu
        mainMenu.addItem(windowItem)
        return mainMenu
    }
}

@MainActor final class MenuAppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private let model: AppModel
    var confirmProfileDeletion: (NSAlert) -> NSApplication.ModalResponse = { $0.runModal() }

    override convenience init() { self.init(model: AppModel()) }

    init(model: AppModel) {
        self.model = model
        super.init()
    }
    private let groupIcons = GroupIconLoader()
    private lazy var connectionsWindow = ConnectionsWindowController(model: model.connections)
    private var statusItem: NSStatusItem!
    private let menu = NSMenu()
    private var pulse: Timer?
    private var menuRefreshTask: Task<Void, Never>?
    private var menuTracking = false
    private var deferredMenuRebuild = false
    private var loadingTimer: Timer?
    private var loadingFrame = 0
    private var updateCleanupComplete = false

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
        if updateCleanupComplete { return .terminateNow }
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
        modeMenu.addItem(.separator())
        if let mode = model.mode {
            add("Save Current Mode as Default", command: ["mode", "save", mode],
                enabled: model.state == .running && !model.busy, to: modeMenu)
        } else {
            disabled("Current mode unavailable", to: modeMenu)
        }
        addSubmenu("Mode", modeMenu, enabled: model.state == .running)

        let groupsMenu = NSMenu()
        if model.groups.allSatisfy(\.hidden) { disabled("No visible proxy groups", to: groupsMenu) }
        for group in model.groups {
            if group.hidden { continue }
            let choices = makeGroupMenu(group)
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
            disabled("\(provider.type) · \(provider.nodes.count) nodes", to: actions)
            actions.addItem(.separator())
            add("Update Nodes Now", command: provider.updateCommand, enabled: model.state == .running && !model.busy, to: actions)
            let tests = NSMenu()
            add("All Nodes", command: provider.healthcheckCommand, enabled: model.state == .running && !model.busy, to: tests)
            if !provider.testableNodes.isEmpty { tests.addItem(.separator()) }
            for node in provider.testableNodes {
                let item = add("\(node) · \(provider.nodeDelays[node]?.label ?? "Untested")", command: ["proxy", "test", "provider", provider.name, node], enabled: model.state == .running && !model.busy, to: tests)
                item.toolTip = (provider.nodeDelays[node] ?? NodeDelay()).detail
            }
            addSubmenu("Test Latency", tests, enabled: model.state == .running && !model.busy, to: actions)
            add("Provider Settings…", action: #selector(providerSettings(_:)), enabled: !model.busy, to: actions).representedObject = provider.name
            actions.addItem(.separator())
            add("Delete Provider", command: provider.removeCommand, enabled: !model.busy, to: actions)
            addSubmenu(provider.name, actions, to: providersMenu)
        }
        addSubmenu("Proxy Providers", providersMenu, enabled: !model.busy)
        menu.addItem(.separator())

        addSubmenu("Profiles", makeProfilesMenu(), enabled: !model.profiles.isEmpty)

        let subscriptionMenu = NSMenu()
        add("Add and Apply Subscription…", action: #selector(addAndApplySubscription), enabled: !model.busy, to: subscriptionMenu)
        if model.subscriptions.isEmpty { disabled("No saved subscriptions", to: subscriptionMenu) }
        else { subscriptionMenu.addItem(.separator()) }
        for subscription in model.subscriptions {
            let actions = NSMenu()
            disabled(Self.quotaLabel(subscription), to: actions)
            if let expiry = subscription.expiresAt { disabled("Expires: \(Self.subscriptionTime(expiry))", to: actions) }
            let nextUpdate = subscription.scheduleSeconds == 0 ? "Next update: off" :
                (subscription.schedulerRunning ? (subscription.nextDue.map { "Next update: \(Self.subscriptionTime($0))" } ?? "Next update: pending") : "Next update: scheduler not running")
            disabled(nextUpdate, to: actions)
            if let pause = subscription.pauseReason { disabled("Auto-apply paused: \(pause)", to: actions) }
            if let stage = subscription.lastFailureStage { disabled("Last failure: \(stage)/\(subscription.lastFailureReason ?? "unknown")", to: actions) }
            actions.addItem(.separator())
            let updateAndApply = add("Update and Apply", action: #selector(updateAndApplySubscription(_:)),
                                     enabled: !model.busy, to: actions)
            updateAndApply.representedObject = subscription.name
            add("Subscription Settings…", action: #selector(subscriptionSettings(_:)), enabled: !model.busy, to: actions).representedObject = subscription.name
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
        add("Connections…", action: #selector(showConnections))
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

    func makeProfilesMenu() -> NSMenu {
        let profileMenu = NSMenu()
        if model.profiles.isEmpty { disabled("No profiles", to: profileMenu) }
        for name in model.profiles {
            if name == "default" {
                let actions = NSMenu()
                add("Use Default Profile", action: #selector(switchToDefaultProfile),
                    checked: model.profile == name, enabled: !model.busy, to: actions)
                add("Apply Default Runtime Settings to Current Profile", action: #selector(applyDefaultOverride),
                    checked: model.defaultOverrideApplied == true,
                    enabled: !model.busy && model.defaultOverrideApplied != nil, to: actions)
                addSubmenu(name, actions, enabled: !model.busy, to: profileMenu)
            } else {
                let actions = NSMenu()
                add("Use", command: ["profile", "use", name], checked: model.profile == name,
                    enabled: !model.busy, to: actions)
                let deleteTitle = model.profile == name
                    ? "Delete… (Switch profiles first)" : "Delete…"
                add(deleteTitle, action: #selector(deleteProfile(_:)),
                    enabled: !model.busy && model.deletableProfiles.contains(name),
                    to: actions).representedObject = name
                let item = addSubmenu(name, actions, enabled: !model.busy, to: profileMenu)
                item.state = model.profile == name ? .on : .off
            }
        }
        return profileMenu
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
    nonisolated static func subscriptionTime(_ raw: String, timeZone: TimeZone = .current) -> String {
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
    func makeGroupMenu(_ group: ProxyGroup) -> NSMenu {
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
                let item = add(group.nodeTitle(node), command: command,
                               checked: group.selected == node, enabled: !model.busy, to: choices)
                item.toolTip = group.nodeDetail(node)
            }
        } else {
            disabled("\(group.kind) · \(group.selected)", to: choices)
            if !group.nodes.isEmpty { choices.addItem(.separator()) }
            for node in group.nodes {
                let item = NSMenuItem(title: group.nodeTitle(node), action: nil, keyEquivalent: "")
                item.state = group.selected == node ? .on : .off
                item.isEnabled = false
                item.toolTip = group.nodeDetail(node)
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
        choices.addItem(.separator())
        let test = add(group.testTitle, command: group.testCommand,
                       enabled: !model.busy && group.hasLatencyCandidates, to: choices)
        if group.nodes.isEmpty {
            test.toolTip = "No nodes in this group."
        } else {
            test.toolTip = group.hasLatencyCandidates ? group.testExplanation : "Reject actions have no latency to test."
        }
        let singleTests = NSMenu()
        for node in group.testableNodes {
            let item = add(group.nodeTitle(node), command: ["proxy", "test", "group", group.name, node], enabled: !model.busy, to: singleTests)
            item.toolTip = (group.nodeDetail(node) ?? "") + "\nTests only this node; keeps the group selection."
        }
        addSubmenu("Test Single Node", singleTests, enabled: !group.testableNodes.isEmpty, to: choices)
        return choices
    }

    @objc private func providerSettings(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String, !model.busy else { return }
        Task {
            do {
                var download = try await model.providerUpdateSettings(name)
                var latency = try await model.healthcheckSettings(kind: "provider", name: name)
                let alert = NSAlert()
                alert.messageText = "Provider Settings · \(name)"
                let guidance = "Profile: \(download["profile"]?.string ?? "unknown"). Save each section separately. Each save applies only that section; other edits stay here until saved."
                alert.informativeText = guidance
                let form = NSView(frame: NSRect(x: 0, y: 0, width: 500, height: 450))
                func label(_ title: String, y: CGFloat, height: CGFloat = 20) -> NSTextField {
                    let field = NSTextField(wrappingLabelWithString: title)
                    field.frame = NSRect(x: 0, y: y, width: 500, height: height)
                    form.addSubview(field)
                    return field
                }
                func mode(y: CGFloat, inherit: String) -> NSPopUpButton {
                    let popup = NSPopUpButton(frame: NSRect(x: 0, y: y, width: 500, height: 26))
                    popup.addItems(withTitles: ["Set a local value", inherit])
                    form.addSubview(popup)
                    return popup
                }
                _ = label("Node source download interval", y: 420)
                let downloadCurrent = label("", y: 382, height: 36)
                let downloadMode = mode(y: 352, inherit: "Use original download interval")
                let downloadInput = NSComboBox(frame: NSRect(x: 0, y: 322, width: 500, height: 24))
                downloadInput.isEditable = true
                downloadInput.addItems(withObjectValues: ["off", "3600", "21600", "86400"])
                downloadInput.stringValue = (download["interval_seconds"]?.int64).map { $0 == 0 ? "off" : String($0) } ?? ""
                form.addSubview(downloadInput)
                _ = label("Automatic latency check interval", y: 292)
                let latencyCurrent = label("", y: 238, height: 50)
                let latencyMode = mode(y: 208, inherit: "Inherit subscription or original interval")
                let latencyInput = NSTextField(frame: NSRect(x: 0, y: 178, width: 500, height: 24))
                latencyInput.stringValue = (latency["interval_seconds"]?.int64).flatMap { $0 > 0 ? String($0) : nil } ?? ""
                latencyInput.placeholderString = "1–31536000 seconds"
                form.addSubview(latencyInput)
                _ = label("Latency test URL", y: 148)
                let urlCurrent = label("", y: 94, height: 50)
                let urlMode = mode(y: 64, inherit: "Inherit subscription or original URL")
                let urlInput = NSTextField(frame: NSRect(x: 0, y: 34, width: 500, height: 24))
                // The CLI exposes only the redacted origin, never the complete URL.
                urlInput.placeholderString = "Enter a complete HTTP(S) URL to replace the current URL"
                form.addSubview(urlInput)
                alert.accessoryView = form
                alert.addButton(withTitle: "Save Download")
                alert.addButton(withTitle: "Done")
                alert.addButton(withTitle: "Save Auto Latency")
                alert.addButton(withTitle: "Save Test URL")
                // Editing a field and pressing Return must not save another section.
                alert.buttons[0].keyEquivalent = ""
                alert.buttons[1].keyEquivalent = "\r"
                func updateCurrentValues() {
                    let downloadValue = (download["interval_seconds"]?.int64).map { $0 == 0 ? "off" : "\($0) seconds" } ?? "unknown"
                    downloadCurrent.stringValue = "Current: \(downloadValue) · Source: \(download["source"]?.string ?? "unknown")\nEnter 1–31536000 seconds, or off."
                    downloadMode.item(at: 1)?.isEnabled = download["can_reset"]?.bool == true
                    downloadMode.toolTip = download["can_reset"]?.bool == true ? "Restore the source profile’s download interval." : "This provider has no source interval to restore."
                    let interval = (latency["interval_seconds"]?.int64).flatMap { $0 > 0 ? "\($0) seconds" : nil } ??
                        (latency["interval_defaulted"]?.bool == true ? "mihomo default" : "unknown")
                    let enabled = (latency["enabled"]?.bool).map { $0 ? "enabled" : "disabled" } ?? "unknown"
                    latencyCurrent.stringValue = "Current: \(interval) · Source: \(latency["source"]?.string ?? "unknown")\nOwn checks: \(enabled). Saving does not enable disabled checks; unused checks may be skipped."
                    urlCurrent.stringValue = "Current origin (redacted): \(latency["test_url"]?.string ?? "not configured")\nSource: \(latency["url_source"]?.string ?? "unknown") · Enter the complete URL to replace it."
                    urlMode.item(at: 1)?.isEnabled = latency["can_reset_url"]?.bool != false
                    urlMode.toolTip = latency["can_reset_url"]?.bool == false ? "No usable inherited test URL is available." : "Restore the subscription or source test URL."
                }
                updateCurrentValues()
                while true {
                    let response = alert.runModal()
                    let arguments: [String]
                    let section: String
                    switch response.rawValue {
                    case NSApplication.ModalResponse.alertFirstButtonReturn.rawValue:
                        section = "Download interval"
                        if downloadMode.indexOfSelectedItem == 1 {
                            arguments = ["proxy", "provider", "interval", "reset", name]
                        } else {
                            let seconds = downloadInput.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                            guard Self.validUpdateInterval(seconds) else {
                                alert.informativeText = "Download interval: enter 1–31536000 seconds, or off.\n" + guidance
                                continue
                            }
                            arguments = ["proxy", "provider", "interval", "set", name, seconds]
                        }
                    case NSApplication.ModalResponse.alertThirdButtonReturn.rawValue:
                        section = "Automatic latency interval"
                        if latencyMode.indexOfSelectedItem == 1 {
                            arguments = ["proxy", "healthcheck", "reset", "provider", name]
                        } else {
                            guard let seconds = Int64(latencyInput.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)),
                                  (1...31_536_000).contains(seconds) else {
                                alert.informativeText = "Automatic latency interval: enter 1–31536000 seconds.\n" + guidance
                                continue
                            }
                            arguments = ["proxy", "healthcheck", "set", "provider", name, String(seconds)]
                        }
                    case NSApplication.ModalResponse.alertThirdButtonReturn.rawValue + 1:
                        section = "Latency test URL"
                        if urlMode.indexOfSelectedItem == 1 {
                            arguments = ["proxy", "healthcheck", "url", "reset", "provider", name]
                        } else {
                            let url = urlInput.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                            guard Self.validTestURL(url), url.rangeOfCharacter(from: .whitespacesAndNewlines) == nil else {
                                alert.informativeText = "Test URL: enter a complete HTTP(S) URL without credentials, whitespace, or a fragment.\n" + guidance
                                continue
                            }
                            arguments = ["proxy", "healthcheck", "url", "set", "provider", name, url]
                        }
                    default:
                        return
                    }
                    // Exactly one CLI mutation per save: no combined success can hide a partial failure.
                    if let failure = await model.saveSourceSetting(arguments) {
                        alert.informativeText = "\(section) change failed: \(failure)\nYour edits are retained. Earlier successful saves remain applied."
                        continue
                    }
                    alert.informativeText = "\(section) saved.\n" + guidance
                    do {
                        download = try await model.providerUpdateSettings(name)
                        latency = try await model.healthcheckSettings(kind: "provider", name: name)
                        updateCurrentValues()
                    } catch {
                        downloadCurrent.stringValue = "Current download settings unavailable; reopen to refresh."
                        latencyCurrent.stringValue = "Current latency settings unavailable; reopen to refresh."
                        urlCurrent.stringValue = "Current URL origin unavailable; reopen to refresh."
                        alert.informativeText = "\(section) saved, but current settings could not be refreshed: \(error.localizedDescription)\nOther edits are retained."
                    }
                }
            } catch {
                let alert = NSAlert()
                alert.messageText = "Provider settings unavailable"
                alert.informativeText = error.localizedDescription
                alert.runModal()
            }
        }
    }

    @objc private func saveSubscriptionSettingsSection(_ sender: NSButton) {
        sender.window?.makeFirstResponder(nil)
        NSApp.stopModal(withCode: NSApplication.ModalResponse(rawValue: 2000 + sender.tag))
    }

    @objc private func subscriptionSettings(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String, !model.busy,
              let subscription = model.subscriptions.first(where: { $0.name == name }) else { return }
        Task {
            do {
                let settings = try await model.healthcheckSettings(kind: "subscription", name: name)
                let alert = NSAlert()
                alert.messageText = "Subscription Settings · \(name)"
                alert.informativeText = "Save each setting separately. Closing keeps settings already saved."
                alert.addButton(withTitle: "Close")
                let content = NSView(frame: NSRect(x: 0, y: 0, width: 500, height: 510))
                func label(_ text: String, x: CGFloat = 12, y: CGFloat, width: CGFloat = 470, height: CGFloat = 32, in parent: NSView) -> NSTextField {
                    let field = NSTextField(wrappingLabelWithString: text)
                    field.frame = NSRect(x: x, y: y, width: width, height: height)
                    field.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
                    parent.addSubview(field)
                    return field
                }
                func saveButton(_ title: String, tag: Int, y: CGFloat, in parent: NSView) {
                    let button = NSButton(title: title, target: self, action: #selector(saveSubscriptionSettingsSection(_:)))
                    button.tag = tag
                    button.frame = NSRect(x: 12, y: y, width: 220, height: 28)
                    parent.addSubview(button)
                }
                let updates = NSBox(frame: NSRect(x: 0, y: 324, width: 500, height: 186))
                updates.title = "Automatic Updates"
                content.addSubview(updates)
                let updateContent = updates.contentView!
                let currentUpdate = label("", y: 126, in: updateContent)
                let automatic = NSButton(checkboxWithTitle: "Automatically download and apply new configuration", target: nil, action: nil)
                automatic.state = subscription.scheduleSeconds > 0 ? .on : .off
                automatic.frame = NSRect(x: 12, y: 96, width: 470, height: 24)
                updateContent.addSubview(automatic)
                _ = label("Download interval (seconds, 1–31536000):", y: 65, width: 270, height: 22, in: updateContent)
                let downloadInterval = NSComboBox(frame: NSRect(x: 290, y: 65, width: 180, height: 26))
                downloadInterval.isEditable = true
                downloadInterval.addItems(withObjectValues: ["3600", "21600", "86400"])
                downloadInterval.stringValue = subscription.scheduleSeconds > 0 ? String(subscription.scheduleSeconds) : ""
                downloadInterval.placeholderString = "Required when enabled"
                updateContent.addSubview(downloadInterval)
                saveButton("Save Automatic Updates", tag: 1, y: 25, in: updateContent)

                let latency = NSBox(frame: NSRect(x: 0, y: 54, width: 500, height: 264))
                latency.title = "Automatic Latency Checks"
                content.addSubview(latency)
                let latencyContent = latency.contentView!
                let currentLatency = label("", y: 184, height: 58, in: latencyContent)
                let intervalMode = NSPopUpButton(frame: NSRect(x: 12, y: 152, width: 285, height: 26), pullsDown: false)
                intervalMode.addItems(withTitles: ["Set interval (seconds)", "Use Original Configuration Interval"])
                latencyContent.addSubview(intervalMode)
                let checkInterval = NSTextField(frame: NSRect(x: 305, y: 152, width: 165, height: 24))
                checkInterval.stringValue = (settings["interval_seconds"]?.int64).flatMap { $0 > 0 ? String($0) : nil } ?? ""
                checkInterval.placeholderString = "1–31536000"
                latencyContent.addSubview(checkInterval)
                saveButton("Save Latency Interval", tag: 2, y: 116, in: latencyContent)
                let currentURL = label("", y: 72, height: 40, in: latencyContent)
                let urlMode = NSPopUpButton(frame: NSRect(x: 12, y: 40, width: 155, height: 26), pullsDown: false)
                urlMode.addItems(withTitles: ["Replace Test URL", "Inherit Test URL"])
                latencyContent.addSubview(urlMode)
                let testURL = NSTextField(frame: NSRect(x: 175, y: 40, width: 295, height: 24))
                testURL.placeholderString = "Complete HTTP(S) URL"
                latencyContent.addSubview(testURL)
                saveButton("Save Test URL", tag: 3, y: 4, in: latencyContent)
                let result = label("Saving intervals or URLs keeps disabled checks disabled.", x: 0, y: 0, width: 500, height: 46, in: content)
                alert.accessoryView = content

                func showUpdateStatus() {
                    let seconds = model.subscriptions.first(where: { $0.name == name })?.scheduleSeconds ?? subscription.scheduleSeconds
                    currentUpdate.stringValue = "Current: \(seconds > 0 ? "on · every \(seconds) seconds" : "off")\nSource: saved subscription schedule"
                }
                func showLatencyStatus(_ value: JSONValue) {
                    let interval = (value["interval_seconds"]?.int64).flatMap { $0 > 0 ? "\($0) seconds" : nil } ??
                        (value["interval_defaulted"]?.bool == true ? "original configuration" : "unknown")
                    let profile = value["profile"]?.string ?? ""
                    let enabled = (value["enabled"]?.bool).map { $0 ? "enabled" : "disabled" } ?? "varies by source configuration"
                    currentLatency.stringValue = "Profile: \(profile.isEmpty ? "not applied" : profile)\nInterval: \(interval) · Source: \(value["source"]?.string ?? "unknown")\nChecks: \(enabled). Saving keeps disabled checks disabled."
                    currentURL.stringValue = "Current URL origin: \(value["test_url"]?.string ?? "original configuration") (redacted)\nSource: \(value["url_source"]?.string ?? "unknown"); enter a complete URL to replace."
                    currentURL.toolTip = currentURL.stringValue
                    urlMode.item(at: 1)?.isEnabled = value["can_reset_url"]?.bool != false
                }
                showUpdateStatus()
                showLatencyStatus(settings)
                NSApp.activate(ignoringOtherApps: true)
                while true {
                    let response = alert.runModal().rawValue
                    guard (2001...2003).contains(response) else { return }
                    let failure: String?
                    let saved: String
                    if response == 2001 {
                        let seconds = downloadInterval.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                        guard automatic.state != .on || (Int64(seconds).map { (1...31_536_000).contains($0) } == true) else {
                            result.stringValue = "Enter a download interval from 1 to 31536000 seconds before enabling automatic updates."
                            continue
                        }
                        failure = await model.saveSourceSetting(["subscription", "schedule", name, automatic.state == .on ? seconds : "off"])
                        saved = "Automatic updates"
                        showUpdateStatus()
                    } else if response == 2002 {
                        let reset = intervalMode.indexOfSelectedItem == 1
                        let seconds = Int64(checkInterval.stringValue.trimmingCharacters(in: .whitespacesAndNewlines))
                        guard reset || (seconds.map { (1...31_536_000).contains($0) } == true) else {
                            result.stringValue = "Enter a latency interval from 1 to 31536000 seconds."
                            continue
                        }
                        failure = await model.saveHealthcheckSettings(kind: "subscription", name: name, seconds: reset ? nil : seconds)
                        saved = "Latency interval"
                    } else {
                        let reset = urlMode.indexOfSelectedItem == 1
                        let url = testURL.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                        guard reset || Self.validTestURL(url) else {
                            result.stringValue = "Enter a complete HTTP(S) URL without credentials or a fragment."
                            continue
                        }
                        failure = await model.saveSourceSetting(["proxy", "healthcheck", "url", reset ? "reset" : "set", "subscription", name] + (reset ? [] : [url]))
                        saved = "Test URL"
                    }
                    if let failure {
                        result.stringValue = "\(saved) save failed: \(failure)\nYour edits are retained; previously saved settings remain saved."
                    } else {
                        result.stringValue = "\(saved) saved. Other edits still require their own Save button."
                        do {
                            showLatencyStatus(try await model.healthcheckSettings(kind: "subscription", name: name))
                        } catch {
                            result.stringValue = "\(saved) saved; refreshing latency settings failed: \(error.localizedDescription)"
                        }
                    }
                    result.toolTip = result.stringValue
                }
            } catch {
                let alert = NSAlert()
                alert.messageText = "Subscription settings unavailable"
                alert.informativeText = error.localizedDescription
                alert.runModal()
            }
        }
    }

    static func validTestURL(_ raw: String) -> Bool {
        guard let url = URLComponents(string: raw), ["http", "https"].contains(url.scheme?.lowercased() ?? ""),
              url.host?.isEmpty == false, url.user == nil, url.password == nil, url.fragment == nil else { return false }
        return true
    }
    static func validUpdateInterval(_ raw: String) -> Bool {
        raw == "off" || Int64(raw).map { (1...31_536_000).contains($0) } == true
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
    @objc private func addProvider() {
        guard !model.busy else { return }
        let alert = NSAlert()
        alert.messageText = "Add Provider"
        let instructions = "Add an HTTP(S) provider to the selected profile. A new group does not change routing."
        alert.informativeText = instructions
        alert.addButton(withTitle: "Add Provider")
        alert.addButton(withTitle: "Cancel")
        let accessory = NSView(frame: NSRect(x: 0, y: 0, width: 400, height: 174))
        func label(_ title: String, y: CGFloat) {
            let field = NSTextField(labelWithString: title)
            field.frame = NSRect(x: 0, y: y, width: 85, height: 24)
            accessory.addSubview(field)
        }
        label("Name", y: 142)
        let nameField = NSTextField(frame: NSRect(x: 90, y: 142, width: 310, height: 24))
        nameField.placeholderString = "Provider name"
        accessory.addSubview(nameField)
        label("URL", y: 108)
        let urlField = NSTextField(frame: NSRect(x: 90, y: 108, width: 310, height: 24))
        urlField.placeholderString = "https://example.com/provider.yaml"
        accessory.addSubview(urlField)
        label("Group", y: 74)
        let groupField = NSComboBox(frame: NSRect(x: 90, y: 74, width: 310, height: 24))
        groupField.isEditable = true
        groupField.placeholderString = "Existing or new group name"
        groupField.addItems(withObjectValues: model.groups.filter {
            ["selector", "urltest", "fallback", "loadbalance"].contains($0.kind.lowercased())
        }.map(\.name))
        accessory.addSubview(groupField)
        label("Update (s)", y: 40)
        let intervalField = NSComboBox(frame: NSRect(x: 90, y: 40, width: 310, height: 24))
        intervalField.isEditable = true
        intervalField.addItems(withObjectValues: ["off", "3600", "21600", "86400"])
        intervalField.stringValue = "3600"
        accessory.addSubview(intervalField)
        label("Test URL", y: 6)
        let testURLField = NSTextField(frame: NSRect(x: 90, y: 6, width: 310, height: 24))
        testURLField.placeholderString = "Optional; blank inherits configuration"
        accessory.addSubview(testURLField)
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
                let updateInterval = intervalField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                guard Self.validUpdateInterval(updateInterval) else {
                    alert.informativeText = "Update interval must be 1–31536000 seconds, or off."
                    alert.window.initialFirstResponder = intervalField
                    continue
                }
                let testURL = testURLField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                guard testURL.isEmpty || Self.validTestURL(testURL) else {
                    alert.informativeText = "Test URL must be HTTP(S), without credentials or a fragment."
                    alert.window.initialFirstResponder = testURLField; continue
                }
                if let error = await model.addProvider(name, url: url, group: group,
                                                       intervalSeconds: Int64(updateInterval) ?? 3600,
                                                       updateOff: updateInterval == "off", testURL: testURL) {
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
        alert.informativeText = "Downloads and applies the subscription without starting the engine. Keeps local settings and nodes; clears saved selections."
        alert.addButton(withTitle: "Add and Apply")
        alert.addButton(withTitle: "Cancel")

        let accessory = NSView(frame: NSRect(x: 0, y: 0, width: 400, height: 140))
        let nameLabel = NSTextField(labelWithString: "Name")
        nameLabel.frame = NSRect(x: 0, y: 110, width: 85, height: 22)
        accessory.addSubview(nameLabel)
        let nameField = NSTextField(frame: NSRect(x: 90, y: 110, width: 310, height: 22))
        nameField.placeholderString = "Subscription name"
        accessory.addSubview(nameField)
        let urlLabel = NSTextField(labelWithString: "URL")
        urlLabel.frame = NSRect(x: 0, y: 76, width: 85, height: 22)
        accessory.addSubview(urlLabel)
        let urlField = NSTextField(frame: NSRect(x: 90, y: 76, width: 310, height: 22))
        urlField.placeholderString = "https://example.com/subscription"
        accessory.addSubview(urlField)
        let scheduleLabel = NSTextField(labelWithString: "Update (s)")
        scheduleLabel.frame = NSRect(x: 0, y: 42, width: 85, height: 22); accessory.addSubview(scheduleLabel)
        let scheduleField = NSComboBox(frame: NSRect(x: 90, y: 42, width: 310, height: 22))
        scheduleField.isEditable = true
        scheduleField.addItems(withObjectValues: ["off", "3600", "21600", "86400"])
        scheduleField.stringValue = "3600"; scheduleField.placeholderString = "Seconds, or off"
        accessory.addSubview(scheduleField)
        let testLabel = NSTextField(labelWithString: "Test URL")
        testLabel.frame = NSRect(x: 0, y: 8, width: 85, height: 22); accessory.addSubview(testLabel)
        let testURLField = NSTextField(frame: NSRect(x: 90, y: 8, width: 310, height: 22))
        testURLField.placeholderString = "Optional; blank inherits configuration"
        accessory.addSubview(testURLField)
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
            let schedule = scheduleField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard Self.validUpdateInterval(schedule) else {
                alert.informativeText = "Update interval must be 1–31536000 seconds, or off."
                alert.window.initialFirstResponder = scheduleField; continue
            }
            let testURL = testURLField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard testURL.isEmpty || Self.validTestURL(testURL) else {
                alert.informativeText = "Test URL must be HTTP(S), without credentials or a fragment."
                alert.window.initialFirstResponder = testURLField; continue
            }
            model.addAndApplySubscription(name, url: url, schedule: schedule, testURL: testURL)
            return
        }
    }
    static func profileDeletionConfirmation(_ name: String) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = "Delete profile “\(name)”?"
        alert.informativeText = "This permanently deletes its configuration, backups, local overrides and settings, and saved proxy choices, and disconnects its subscription associations. Subscription records and cached downloads are kept."
        alert.alertStyle = .warning
        let delete = alert.addButton(withTitle: "Delete Profile")
        let cancel = alert.addButton(withTitle: "Cancel")
        let window = alert.window
        // NSAlert normally assigns Return to its first button. Keep deletion explicit.
        delete.keyEquivalent = ""
        cancel.keyEquivalent = "\r"
        window.defaultButtonCell = cancel.cell as? NSButtonCell
        window.initialFirstResponder = cancel
        return alert
    }

    @objc func deleteProfile(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String,
              !model.busy, model.deletableProfiles.contains(name) else { return }
        let alert = Self.profileDeletionConfirmation(name)
        NSApp.activate(ignoringOtherApps: true)
        guard confirmProfileDeletion(alert) == .alertFirstButtonReturn else { return }
        // Recheck current state in AppModel after the modal confirmation closes.
        model.removeProfile(name)
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
        updateButton.toolTip = "Updates Nagi and reopens this app."
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertSecondButtonReturn { updateNagi() }
    }
    @objc private func showConnections() { connectionsWindow.present() }
    @objc private func quit() { NSApp.terminate(nil) }
    @objc private func updateNagi() {
        guard !model.busy else { return }
        let alert = NSAlert()
        alert.alertStyle = .informational
        alert.messageText = "Update Nagi"
        alert.informativeText = "After authorization, Nagi will close automatically, finish updating in the background, and reopen."
        alert.addButton(withTitle: "OK")
        NSApp.activate(ignoringOtherApps: true)
        alert.runModal()
        model.updateNagi {
            // Finish CLI cleanup before entering AppKit's termination
            // handshake. AppKit waits for applicationShouldTerminate while
            // the main actor is blocked, so starting model.quit from that
            // callback would deadlock.
            self.model.quit { succeeded in
                guard succeeded else { return }
                self.updateCleanupComplete = true
                DispatchQueue.main.async { NSApp.terminate(nil) }
            }
        }
    }
    @objc private func willSleep(_ notification: Notification) { model.pauseForSleep() }
    @objc private func didWake(_ notification: Notification) { model.resumeAfterWake() }
}
