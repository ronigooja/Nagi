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
    private var statusItem: NSStatusItem!
    private let menu = NSMenu()
    private var pulse: Timer?
    private var menuRefreshTask: Task<Void, Never>?
    private var loadingTimer: Timer?
    private var loadingFrame = 0

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.menu = menu
        menu.delegate = self
        model.onChange = { [weak self] in self?.renderStatus() }
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
            statusItem.menu = nil
        } else {
            statusItem.menu = menu
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
        let symbol = NSImage(systemSymbolName: "network.badge.shield.half.filled", accessibilityDescription: "Nagi")
        let full = symbol?.withSymbolConfiguration(NSImage.SymbolConfiguration(paletteColors: [.white, .white]))
        let icon: NSImage?
        if let symbol, let full {
            if model.applyingSetting {
                let network = symbol.withSymbolConfiguration(NSImage.SymbolConfiguration(paletteColors: [.clear, .white]))
                let shield = symbol.withSymbolConfiguration(NSImage.SymbolConfiguration(paletteColors: [.white, .clear]))
                let shieldOpacity: CGFloat = loadingFrame == 0 ? 1 : 0.5
                icon = NSImage(size: full.size, flipped: false) { rect in
                    network?.draw(in: rect, from: .zero, operation: .sourceOver, fraction: 1)
                    shield?.draw(in: rect, from: .zero, operation: .sourceOver, fraction: shieldOpacity)
                    return true
                }
            } else {
                let opacity: CGFloat = model.state == .running ? 1 : 0.5
                icon = NSImage(size: full.size, flipped: false) { rect in
                    full.draw(in: rect, from: .zero, operation: .sourceOver, fraction: opacity)
                    return true
                }
            }
        } else {
            icon = nil
        }
        icon?.isTemplate = false
        let stacked = model.display == .both
        button.title = ""
        button.imagePosition = .imageOnly
        if stacked, let icon {
            let font = NSFont.monospacedSystemFont(ofSize: 9, weight: .regular)
            let paragraph = NSMutableParagraphStyle()
            paragraph.alignment = .right
            let attributes: [NSAttributedString.Key: Any] = [
                .font: font, .foregroundColor: NSColor.white, .paragraphStyle: paragraph,
                .kern: -1.2
            ]
            let rows = model.statusTitle.split(separator: "\n").map(String.init)
            let iconWidth = min(icon.size.width, 20)
            let rateX = iconWidth + 2
            let rateWidth = ("999 KB/s" as NSString).size(withAttributes: attributes).width
            let size = NSSize(width: rateX + rateWidth, height: 22)
            button.image = NSImage(size: size, flipped: false) { _ in
                icon.draw(in: NSRect(x: 0, y: (size.height - 18) / 2, width: iconWidth, height: 18),
                    from: .zero, operation: .sourceOver, fraction: 1)
                for (index, row) in rows.prefix(2).enumerated() {
                    let rate = row.trimmingCharacters(in: .whitespaces) as NSString
                    let y: CGFloat = index == 0 ? 10 : 1
                    rate.draw(in: NSRect(x: rateX, y: y, width: rateWidth, height: 11),
                        withAttributes: attributes)
                }
                return true
            }
            statusItem.length = size.width
        } else {
            button.image = icon
            statusItem.length = NSStatusItem.variableLength
        }
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

    func menuNeedsUpdate(_ menu: NSMenu) {
        rebuildMenu()
        guard menuRefreshTask == nil else { return }
        menuRefreshTask = Task {
            await model.refreshMenuData()
            rebuildMenu()
            menuRefreshTask = nil
        }
    }

    private func rebuildMenu() {
        menu.removeAllItems()
        let stateTitle: String
        switch model.state {
        case .starting: stateTitle = "Nagi · Starting…"
        case .running: stateTitle = "Nagi · Running"
        case .stopped: stateTitle = "Nagi · Stopped"
        case .unavailable: stateTitle = "Nagi · Status unavailable"
        }
        let status = NSMenuItem(title: stateTitle, action: nil, keyEquivalent: "")
        let errors = [model.quitErrorMessage, model.errorMessage].compactMap { $0 }
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
        if model.groups.isEmpty { disabled("No proxy groups", to: groupsMenu) }
        for group in model.groups {
            let choices = NSMenu()
            if group.selectable {
                for node in group.nodes {
                    add(node, command: ["proxy", "select", group.name, node],
                        checked: group.selected == node, enabled: !model.busy, to: choices)
                }
            } else { disabled("\(group.kind) · \(group.selected)", to: choices) }
            addSubmenu(group.name, choices, enabled: group.selectable && !group.nodes.isEmpty, to: groupsMenu)
        }
        addSubmenu("Proxy Groups", groupsMenu, enabled: model.state == .running)
        menu.addItem(.separator())

        let profileMenu = NSMenu()
        if model.profiles.isEmpty { disabled("No profiles", to: profileMenu) }
        for name in model.profiles {
            if name == "default" {
                let actions = NSMenu()
                add("切换到此配置", action: #selector(switchToDefaultProfile),
                    checked: model.profile == name, enabled: !model.busy, to: actions)
                add("将本地默认配置应用到当前订阅", action: #selector(applyDefaultOverride),
                    enabled: !model.busy, to: actions)
                addSubmenu(name, actions, enabled: !model.busy, to: profileMenu)
            } else {
                add(name, command: ["profile", "use", name], checked: model.profile == name,
                    enabled: !model.busy, to: profileMenu)
            }
        }
        addSubmenu("Profiles", profileMenu, enabled: !model.profiles.isEmpty)

        let subscriptionMenu = NSMenu()
        add("Download Subscription…", action: #selector(downloadSubscription), enabled: !model.busy, to: subscriptionMenu)
        if model.subscriptions.isEmpty { disabled("No saved subscriptions", to: subscriptionMenu) }
        else { subscriptionMenu.addItem(.separator()) }
        for subscription in model.subscriptions {
            let actions = NSMenu()
            let apply = add("Apply", action: #selector(applySubscription(_:)),
                            enabled: !model.busy, to: actions)
            apply.representedObject = subscription.name
            let updateAndApply = add("Update and Apply", action: #selector(updateAndApplySubscription(_:)),
                                     enabled: !model.busy, to: actions)
            updateAndApply.representedObject = subscription.name
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
    private func addSubmenu(_ title: String, _ submenu: NSMenu, enabled: Bool = true, to target: NSMenu? = nil) {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.submenu = submenu; item.isEnabled = enabled
        (target ?? menu).addItem(item)
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
    @objc private func applyDefaultOverride() { model.applyDefaultOverrideToCurrentProfile() }
    @objc private func updateAndApplySubscription(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        model.updateAndApplySubscription(name)
    }
    @objc private func applySubscription(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        model.applySubscription(name)
    }
    @objc private func downloadSubscription() {
        let alert = NSAlert()
        alert.messageText = "Download Subscription"
        alert.informativeText = "Enter a name and the subscription URL."
        alert.addButton(withTitle: "Download")
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
            model.addAndDownloadSubscription(name, url: url)
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
    @objc private func showAbout() { NSApp.orderFrontStandardAboutPanel(nil); NSApp.activate(ignoringOtherApps: true) }
    @objc private func quit() { NSApp.terminate(nil) }
    @objc private func willSleep(_ notification: Notification) { model.pauseForSleep() }
    @objc private func didWake(_ notification: Notification) { model.resumeAfterWake() }
}
