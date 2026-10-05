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
            statusItem.menu = menu
        }
        let networkColor = NSColor.tertiaryLabelColor
        let shieldColor: NSColor
        if model.busy {
            shieldColor = loadingFrame == 0 ? .white : .clear
        } else {
            shieldColor = model.state == .running ? .white : .tertiaryLabelColor
        }
        let configuration = NSImage.SymbolConfiguration(paletteColors: [shieldColor, networkColor])
        let baseIcon = NSImage(systemSymbolName: "network.badge.shield.half.filled", accessibilityDescription: "Nagi")?
            .withSymbolConfiguration(configuration)
        let icon: NSImage?
        if model.state == .unavailable, let baseIcon,
           let warning = NSImage(systemSymbolName: "exclamationmark", accessibilityDescription: "Status unavailable")?
            .withSymbolConfiguration(NSImage.SymbolConfiguration(paletteColors: [.white])) {
            icon = NSImage(size: baseIcon.size, flipped: false) { _ in
                baseIcon.draw(in: NSRect(origin: .zero, size: baseIcon.size))
                warning.draw(in: NSRect(x: baseIcon.size.width * 0.62, y: baseIcon.size.height * 0.08,
                                        width: baseIcon.size.width * 0.28, height: baseIcon.size.height * 0.45))
                return true
            }
        } else {
            icon = baseIcon
        }
        icon?.isTemplate = false
        button.image = icon
        button.imagePosition = .imageLeading
        let title = model.statusTitle
        button.attributedTitle = NSAttributedString(string: title.isEmpty ? "" : "  " + title,
            attributes: [.font: NSFont.monospacedSystemFont(ofSize: NSFont.systemFontSize, weight: .regular)])
        let state: String
        switch model.state {
        case .starting: state = "starting"
        case .running: state = "running"
        case .stopped: state = "stopped"
        case .unavailable: state = "status unavailable"
        }
        button.setAccessibilityLabel(model.busy ? "Nagi, applying setting" : "Nagi \(state), \(model.accessibilityTraffic)")
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
        status.isEnabled = false; menu.addItem(status)
        if let error = model.quitErrorMessage {
            let item = NSMenuItem(title: "⚠ \(error)", action: nil, keyEquivalent: "")
            item.isEnabled = false; item.toolTip = error; menu.addItem(item)
        }
        if let error = model.errorMessage {
            let item = NSMenuItem(title: "⚠ \(error)", action: nil, keyEquivalent: "")
            item.isEnabled = false; item.toolTip = error; menu.addItem(item)
        }
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
            add(name, command: ["profile", "use", name], checked: model.profile == name,
                enabled: !model.busy, to: profileMenu)
        }
        addSubmenu("Profiles", profileMenu, enabled: !model.profiles.isEmpty)

        let subscriptionMenu = NSMenu()
        if model.subscriptions.isEmpty { disabled("No subscriptions · Add one with the CLI", to: subscriptionMenu) }
        for subscription in model.subscriptions {
            let actions = NSMenu()
            add("Update cached copy", command: ["subscription", "update", subscription.name],
                enabled: !model.busy, to: actions)
            add("Apply cached copy to profile", command: ["subscription", "apply", subscription.name],
                enabled: !model.busy, to: actions)
            if let updated = subscription.updatedAt { disabled("Last update: \(updated)", to: actions) }
            addSubmenu(subscription.name, actions, to: subscriptionMenu)
        }
        addSubmenu("Subscriptions", subscriptionMenu, enabled: !model.subscriptions.isEmpty)
        menu.addItem(.separator())

        let displayMenu = NSMenu()
        for option in TrafficDisplay.allCases {
            let item = add(option.title, action: #selector(setDisplay(_:)), checked: model.display == option, to: displayMenu)
            item.representedObject = option.rawValue
        }
        addSubmenu("Display", displayMenu)
        let startupMenu = NSMenu()
        add("Open app at login", action: #selector(toggleAppLogin), checked: model.appLoginEnabled, to: startupMenu)
        let startupCommand = model.serviceInstalled == true ? ["service", "uninstall"] : ["service", "install"]
        add("Start Nagi at login", command: startupCommand,
            checked: model.serviceInstalled == true, enabled: model.serviceInstalled != nil && !model.busy, to: startupMenu)
        if model.serviceInstalled == nil { disabled("Service status unavailable", to: startupMenu) }
        addSubmenu("Startup", startupMenu)
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
        let success = command.count >= 2 && command[0] == "subscription" && command[1] == "update"
            ? "Cache updated. Apply the cached copy separately." : nil
        model.perform(command, success: success)
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
