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
        appMenu.addItem(withTitle: L10n.text("app.ui.quit_nagi"), action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        mainMenu.addItem(appItem)

        // Accessory apps still need a main menu to route editing shortcuts to
        // the focused field editor, including while an NSAlert runs modally.
        let editItem = NSMenuItem(title: L10n.text("app.ui.edit"), action: nil, keyEquivalent: "")
        let editMenu = NSMenu(title: L10n.text("app.ui.edit"))
        editMenu.addItem(withTitle: L10n.text("app.ui.undo"), action: Selector(("undo:")), keyEquivalent: "z")
        let redo = editMenu.addItem(withTitle: L10n.text("app.ui.redo"), action: Selector(("redo:")), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: L10n.text("app.ui.cut"), action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: L10n.text("app.ui.copy"), action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: L10n.text("app.ui.paste"), action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: L10n.text("app.ui.select_all"), action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        // Leave targets nil so AppKit uses the responder chain.
        editItem.submenu = editMenu
        mainMenu.addItem(editItem)
        let windowItem = NSMenuItem(title: L10n.text("app.ui.window"), action: nil, keyEquivalent: "")
        let windowMenu = NSMenu(title: L10n.text("app.ui.window"))
        windowMenu.addItem(withTitle: L10n.text("app.ui.close_window"), action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        windowMenu.addItem(withTitle: L10n.text("app.ui.minimize"), action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
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
    private lazy var captureWindow = CaptureWindowController(model: model.capture)
    private lazy var connectionsWindow = ConnectionsWindowController(model: model.connections)
    private lazy var requestRewriteWindow = RequestRewriteWindowController(model: model.requestRewrite)
    private var statusItem: NSStatusItem!
    let menu = NSMenu()
    private var pulse: Timer?
    private var menuRefreshTask: Task<Void, Never>?
    private var menuTracking = false
    private var loadingTimer: Timer?
    private var loadingFrame = 0
    private var updateCleanupComplete = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.menu = menu
        menu.delegate = self
        model.onTrafficChange = { [weak self] in self?.renderStatus() }
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
        case .unavailable: state = L10n.text("app.ui.status_unavailable_3")
        }
        button.setAccessibilityLabel(model.applyingSetting ? L10n.text("app.ui.nagi_applying_setting") : L10n.format("app.ui.nagi", String(describing: L10n.protocolLabel(state)), String(describing: model.accessibilityTraffic)))
    }

    func menuShouldOpen(_ menu: NSMenu) -> Bool { !model.busy }

    func menuWillOpen(_ menu: NSMenu) {
        guard menu === self.menu, !menuTracking else { return }
        // Build the initial tree before AppKit starts tracking submenu items.
        rebuildMenu()
        menuTracking = true
    }

    func menuDidClose(_ menu: NSMenu) {
        guard menu === self.menu else { return }
        menuTracking = false
        // AppKit may still be unwinding submenu tracking. The next opening
        // rebuilds from current state without replacing items during teardown.
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        guard menu === self.menu else { return }
        rebuildMenu()
        guard menuRefreshTask == nil else { return }
        menuRefreshTask = Task {
            await model.refreshMenuData()
            rebuildMenu()
            menuRefreshTask = nil
        }
    }

    static func makeStatusMenuItem(state: EngineState, errors: [String], notice: String?,
                                   tunEnabled: Bool, tunAdapter: String?) -> NSMenuItem {
        let engineState: String
        switch state {
        case .starting: engineState = L10n.text("app.ui.starting")
        case .running: engineState = L10n.text("app.ui.running")
        case .stopped: engineState = L10n.text("app.ui.stopped")
        case .unavailable: engineState = L10n.text("app.ui.status_unavailable_2")
        }
        var errors = errors
        if tunEnabled && tunAdapter != "up" {
            errors.append(L10n.format("app.ui.tun_adapter_check_logs_in_cli", String(describing: tunAdapter.map { L10n.protocolLabel($0) } ?? L10n.text("app.tun.unknown"))))
        }
        let title = !errors.isEmpty ? L10n.text("app.ui.nagi_error")
            : (notice != nil ? L10n.text("app.ui.nagi_info") : L10n.format("app.ui.nagi_2", String(describing: engineState)))
        let status = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        guard !errors.isEmpty || notice != nil else {
            status.isEnabled = false
            return status
        }
        let font = NSFont.menuFont(ofSize: 0)
        let symbolName = errors.isEmpty ? "info.circle" : "exclamationmark.triangle"
        let iconSize: CGFloat = 14
        if let symbol = NSImage(systemSymbolName: symbolName, accessibilityDescription: errors.isEmpty ? L10n.text("app.ui.info") : L10n.text("app.ui.error"))?
            .withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: font.pointSize, weight: .regular)) {
            symbol.size = NSSize(width: iconSize, height: iconSize)
            let attachment = NSTextAttachment()
            attachment.image = symbol
            attachment.bounds = NSRect(x: 0, y: (font.capHeight - iconSize) / 2,
                                       width: iconSize, height: iconSize)
            let attributedTitle = NSMutableAttributedString(string: title + " ", attributes: [.font: font])
            attributedTitle.append(NSAttributedString(attachment: attachment))
            status.attributedTitle = attributedTitle
        }
        let details = NSMenu()
        for error in errors { details.addItem(wrappedStatusDetail(error)) }
        if let notice { details.addItem(wrappedStatusDetail(notice)) }
        status.submenu = details
        return status
    }

    private static func wrappedStatusDetail(_ text: String) -> NSMenuItem {
        let item = NSMenuItem(title: text, action: nil, keyEquivalent: "")
        item.isEnabled = false
        item.toolTip = text
        let padding: CGFloat = 12
        let width: CGFloat = 320
        let label = NSTextField(wrappingLabelWithString: text)
        label.font = .menuFont(ofSize: 0)
        label.textColor = .labelColor
        label.maximumNumberOfLines = 0
        label.lineBreakMode = .byWordWrapping
        label.preferredMaxLayoutWidth = width - padding * 2
        let size = label.cell!.cellSize(forBounds: NSRect(
            x: 0, y: 0, width: width - padding * 2, height: .greatestFiniteMagnitude))
        label.frame = NSRect(x: padding, y: 8, width: width - padding * 2, height: ceil(size.height))
        let view = NSView(frame: NSRect(x: 0, y: 0, width: width, height: label.frame.height + 16))
        view.addSubview(label)
        item.view = view
        return item
    }

    private func rebuildMenu() {
        // Keep the entire tree stable while any submenu is being browsed.
        guard !menuTracking else { return }
        menu.removeAllItems()
        menu.addItem(Self.makeStatusMenuItem(
            state: model.state,
            errors: [model.quitErrorMessage, model.errorMessage].compactMap { $0 },
            notice: model.notice, tunEnabled: model.tun == true, tunAdapter: model.tunAdapter))
        if model.state != .running {
            add(L10n.text("app.ui.retry_start"), action: #selector(retryStart), enabled: !model.busy)
        }
        menu.addItem(.separator())
        add(L10n.text("app.ui.system_proxy"), command: ["system-proxy", model.systemProxy == true ? "disable" : "enable"],
            checked: model.systemProxy == true, enabled: model.state == .running && model.systemProxy != nil && !model.busy)
        add("TUN", command: ["tun", model.tun == true ? "disable" : "enable"],
            checked: model.tun == true, enabled: model.state == .running && model.tun != nil && !model.busy)
        let modeMenu = NSMenu()
        for value in ["rule", "global", "direct"] {
            add(L10n.protocolLabel(value), command: ["mode", value], checked: model.mode == value,
                enabled: model.state == .running && !model.busy, to: modeMenu)
        }
        modeMenu.addItem(.separator())
        if let mode = model.mode {
            add(L10n.text("app.ui.save_current_mode_as_default"), command: ["mode", "save", mode],
                enabled: model.state == .running && !model.busy, to: modeMenu)
        } else {
            disabled(L10n.text("app.ui.current_mode_unavailable"), to: modeMenu)
        }
        addSubmenu(L10n.text("app.ui.mode"), modeMenu, enabled: model.state == .running)

        let groupsMenu = NSMenu()
        if model.groups.allSatisfy(\.hidden) { disabled(L10n.text("app.ui.no_visible_proxy_groups"), to: groupsMenu) }
        for group in model.groups {
            if group.hidden { continue }
            let choices = makeGroupMenu(group)
            let showsSelection = group.selectable && !group.selected.isEmpty
            let title = showsSelection ? "\(group.name) · \(group.selected)" : group.name
            let groupItem = addSubmenu(title, choices, to: groupsMenu)
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
        addSubmenu(L10n.text("app.ui.proxy_groups"), groupsMenu, enabled: model.state == .running)
        menu.addItem(.separator())

        addSubmenu(L10n.text("app.ui.profiles"), makeProfilesMenu(), enabled: !model.profiles.isEmpty)

        let subscriptionMenu = NSMenu()
        add(L10n.text("app.ui.add_and_apply_subscription"), action: #selector(addAndApplySubscription), enabled: !model.busy, to: subscriptionMenu)
        if model.subscriptions.isEmpty { disabled(L10n.text("app.ui.no_saved_subscriptions"), to: subscriptionMenu) }
        else { subscriptionMenu.addItem(.separator()) }
        for subscription in model.subscriptions {
            let actions = NSMenu()
            disabled(Self.quotaLabel(subscription), to: actions)
            if let expiry = subscription.expiresAt { disabled(L10n.format("app.ui.expires", String(describing: Self.subscriptionTime(expiry))), to: actions) }
            let nextUpdate = subscription.scheduleSeconds == 0 ? L10n.text("app.ui.next_update_off") :
                (subscription.schedulerRunning ? (subscription.nextDue.map { L10n.format("app.ui.next_update", String(describing: Self.subscriptionTime($0))) } ?? L10n.text("app.ui.next_update_pending")) : L10n.text("app.ui.next_update_scheduler_not_running"))
            disabled(nextUpdate, to: actions)
            if let pause = subscription.pauseReason { disabled(L10n.format("app.ui.auto_apply_paused", String(describing: L10n.protocolLabel(pause))), to: actions) }
            if let stage = subscription.lastFailureStage { disabled(L10n.format("app.ui.last_failure", String(describing: L10n.protocolLabel(stage)), String(describing: subscription.lastFailureReason ?? L10n.text("app.state.unknown"))), to: actions) }
            actions.addItem(.separator())
            let updateAndApply = add(L10n.text("app.ui.update_and_apply"), action: #selector(updateAndApplySubscription(_:)),
                                     enabled: !model.busy, to: actions)
            updateAndApply.representedObject = subscription.name
            add(L10n.text("app.ui.update_settings_2"), action: #selector(subscriptionUpdateSettings(_:)), enabled: !model.busy, to: actions).representedObject = subscription.name
            add(L10n.text("app.ui.latency_check_settings_2"), action: #selector(subscriptionLatencySettings(_:)), enabled: !model.busy, to: actions).representedObject = subscription.name
            actions.addItem(.separator())
            let delete = add(L10n.text("app.ui.delete_subscription"), action: #selector(deleteSubscription(_:)),
                             enabled: !model.busy, to: actions)
            delete.representedObject = subscription.name
            addSubmenu(subscription.name, actions, to: subscriptionMenu)
        }
        addSubmenu(L10n.text("app.ui.subscriptions"), subscriptionMenu)
        let providersMenu = NSMenu()
        add(L10n.text("app.ui.add_provider"), action: #selector(addProvider), enabled: !model.busy, to: providersMenu)
        providersMenu.addItem(.separator())
        if model.providersUnavailable { disabled(L10n.text("app.ui.proxy_providers_unavailable_2"), to: providersMenu) }
        else if model.providers.isEmpty { disabled(L10n.text("app.ui.no_proxy_providers"), to: providersMenu) }
        for provider in model.providers {
            let actions = NSMenu()
            disabled("\(provider.type) · \(L10n.format("app.provider.nodes_count", Int64(provider.nodes.count)))", to: actions)
            actions.addItem(.separator())
            add(L10n.text("app.ui.update_provider"), command: provider.updateCommand, enabled: model.state == .running && !model.busy, to: actions)
            let tests = NSMenu()
            add(L10n.text("app.ui.all_nodes"), command: provider.healthcheckCommand, enabled: model.state == .running && !model.busy, to: tests)
            if !provider.testableNodes.isEmpty { tests.addItem(.separator()) }
            for node in provider.testableNodes {
                let item = add("\(node) · \(provider.nodeDelays[node]?.label ?? L10n.text("app.ui.untested"))", command: ["proxy", "test", "provider", provider.name, node], enabled: model.state == .running && !model.busy, to: tests)
                item.toolTip = (provider.nodeDelays[node] ?? NodeDelay()).detail
            }
            addSubmenu(L10n.text("app.ui.test_latency"), tests, enabled: model.state == .running && !model.busy, to: actions)
            add(L10n.text("app.ui.update_settings_2"), action: #selector(providerUpdateSettings(_:)), enabled: !model.busy, to: actions).representedObject = provider.name
            add(L10n.text("app.ui.latency_check_settings_2"), action: #selector(providerLatencySettings(_:)), enabled: !model.busy, to: actions).representedObject = provider.name
            actions.addItem(.separator())
            add(L10n.text("app.ui.delete_provider"), command: provider.removeCommand, enabled: !model.busy, to: actions)
            addSubmenu(provider.name, actions, to: providersMenu)
        }
        addSubmenu(L10n.text("app.ui.proxy_providers"), providersMenu, enabled: !model.busy)
        menu.addItem(.separator())

        let displayMenu = NSMenu()
        for option in TrafficDisplay.allCases {
            let item = add(option.title, action: #selector(setDisplay(_:)), checked: model.display == option, to: displayMenu)
            item.representedObject = option.rawValue
        }
        add(L10n.text("app.ui.connections_2"), action: #selector(showConnections))
        add(L10n.text("app.ui.request_capture"), action: #selector(showCapture))
        if let capture = model.capture.status, ["recording", "paused"].contains(capture.state) {
            disabled(L10n.text("app.capture.menu_summary") + L10n.protocolLabel(capture.state), to: menu)
            add(L10n.text("app.ui.stop_capture"), action: #selector(stopCapture), enabled: model.capture.canOperate)
        }
        let rewriteMenu = NSMenu()
        let rewrite = model.requestRewrite
        disabled(rewrite.stale ? L10n.text("app.ui.state_unavailable_or_stale") : (rewrite.status?.menuTitle ?? L10n.text("app.ui.status_unavailable_2")), to: rewriteMenu)
        let rewriteEnabled = rewrite.status?.intent == true
        add(rewriteEnabled ? L10n.text("app.ui.disable_request_rewrite") : L10n.text("app.ui.enable_request_rewrite_2"),
            action: #selector(toggleRequestRewrite), checked: rewriteEnabled,
            enabled: rewriteEnabled ? rewrite.canOperate : rewrite.canEnable, to: rewriteMenu)
        add(L10n.text("app.ui.request_rewrite_2"), action: #selector(showRequestRewrite), to: rewriteMenu)
        addSubmenu(L10n.text("app.ui.request_rewrite_3"), rewriteMenu)
        menu.addItem(.separator())
        addSubmenu(L10n.text("app.ui.display"), displayMenu)
        let startupMenu = NSMenu()
        add(L10n.text("app.ui.open_menu_bar_app_at_login"), action: #selector(toggleAppLogin), checked: model.appLoginEnabled, to: startupMenu)
        let startupCommand = model.serviceInstalled == true ? ["service", "uninstall"] : ["service", "install"]
        add(L10n.text("app.ui.run_engine_at_login"), command: startupCommand,
            checked: model.serviceInstalled == true, enabled: model.serviceInstalled != nil && !model.busy, to: startupMenu)
        if model.serviceInstalled == nil { disabled(L10n.text("app.ui.engine_service_status_unavailable"), to: startupMenu) }
        addSubmenu(L10n.text("app.ui.login"), startupMenu)
        menu.addItem(.separator())
        add(L10n.text("app.ui.help"), action: #selector(openHelp))
        add(L10n.text("app.ui.about_nagi"), action: #selector(showAbout))
        add(L10n.text("app.ui.quit_nagi"), action: #selector(quit), enabled: !model.busy)
    }

    func makeProfilesMenu() -> NSMenu {
        let profileMenu = NSMenu()
        if model.profiles.isEmpty { disabled(L10n.text("app.ui.no_profiles"), to: profileMenu) }
        for name in model.profiles {
            add(name, command: ["profile", "use", name], checked: model.profile == name,
                enabled: !model.busy, to: profileMenu)
        }
        profileMenu.addItem(.separator())
        add(L10n.text("app.ui.apply_default_runtime_settings_to_current_profile"), action: #selector(applyDefaultOverride),
            checked: model.defaultOverrideApplied == true,
            enabled: !model.busy && model.defaultOverrideApplied != nil, to: profileMenu)
        let managementMenu = NSMenu()
        let ordinaryProfiles = model.profiles.filter { $0 != "default" }
        if ordinaryProfiles.isEmpty { disabled(L10n.text("app.ui.no_personal_profiles"), to: managementMenu) }
        for name in ordinaryProfiles {
            let deleteTitle = L10n.format("app.ui.delete", String(describing: name)) + (model.profile == name ? L10n.text("app.ui.switch_profiles_first") : "")
            add(deleteTitle, action: #selector(deleteProfile(_:)),
                enabled: !model.busy && model.deletableProfiles.contains(name),
                to: managementMenu).representedObject = name
        }
        addSubmenu(L10n.text("app.ui.manage_profiles"), managementMenu, enabled: !model.busy, to: profileMenu)
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
        guard sub.total > 0 else { return L10n.text("app.ui.quota_unknown") }
        let (sum, overflow) = max(0, sub.upload).addingReportingOverflow(max(0, sub.download))
        let used = overflow ? Int64.max : sum
        let available = max(0, sub.total - used)
        return L10n.format("app.ui.quota_available_total_uploaded_downloaded", String(describing: subscriptionBytes(available)), String(describing: subscriptionBytes(sub.total)), String(describing: subscriptionBytes(sub.upload)), String(describing: subscriptionBytes(sub.download)))
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
        MenuTimestampFormatter.shared.string(raw, timeZone: timeZone)
    }
    func makeGroupMenu(_ group: ProxyGroup) -> NSMenu {
        let choices = NSMenu()
        let automatic = group.automatic
        if automatic {
            if let fixed = group.fixed, !fixed.isEmpty {
                if fixed == group.selected {
                    disabled(L10n.format("app.ui.fixed", fixed), to: choices)
                } else {
                    disabled(L10n.format("app.ui.current_selection", group.selected), to: choices)
                    disabled(L10n.format("app.ui.manual_preference_not_active", fixed), to: choices)
                }
            } else {
                disabled(L10n.format("app.ui.automatic", group.selected), to: choices)
            }
            if let status = group.selectedStatus, status != "available" { disabled(L10n.format("app.ui.selection", String(describing: L10n.protocolLabel(status))), to: choices) }
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
            disabled("\(L10n.protocolLabel(group.kind)) · \(group.selected)", to: choices)
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
            let automatic = add(L10n.text("app.ui.restore_automatic_selection"), command: restoreCommand,
                enabled: !model.busy, to: choices)
            automatic.toolTip = L10n.text("app.ui.clear_the_fixed_choice_through_mihomo")
        }
        if group.nodes.isEmpty { disabled(L10n.text("app.ui.no_nodes_in_this_group"), to: choices) }
        choices.addItem(.separator())
        let test = add(group.testTitle, command: group.testCommand,
                       enabled: !model.busy && group.hasLatencyCandidates, to: choices)
        if group.nodes.isEmpty {
            test.toolTip = L10n.text("app.ui.no_nodes_in_this_group_2")
        } else {
            test.toolTip = group.hasLatencyCandidates ? group.testExplanation : L10n.text("app.ui.reject_actions_have_no_latency_to_test")
        }
        let singleTests = NSMenu()
        for node in group.testableNodes {
            let item = add(group.nodeTitle(node), command: ["proxy", "test", "group", group.name, node], enabled: !model.busy, to: singleTests)
            item.toolTip = (group.nodeDetail(node) ?? "") + L10n.text("app.ui.ntests_only_this_node_keeps_the_group")
        }
        addSubmenu(L10n.text("app.ui.test_single_node"), singleTests, enabled: !group.testableNodes.isEmpty, to: choices)
        return choices
    }

    private enum SourceSettingsKind: String {
        case provider
        case subscription
    }

    @objc private func providerUpdateSettings(_ sender: NSMenuItem) {
        openUpdateSettings(.provider, sender: sender)
    }

    @objc private func subscriptionUpdateSettings(_ sender: NSMenuItem) {
        openUpdateSettings(.subscription, sender: sender)
    }

    @objc private func providerLatencySettings(_ sender: NSMenuItem) {
        openLatencySettings(.provider, sender: sender)
    }

    @objc private func subscriptionLatencySettings(_ sender: NSMenuItem) {
        openLatencySettings(.subscription, sender: sender)
    }

    private func openUpdateSettings(_ kind: SourceSettingsKind, sender: NSMenuItem) {
        guard let name = sender.representedObject as? String, !model.busy else { return }
        SourceSettingsPanelController(model: model, kind: kind.rawValue, name: name, page: .update).runModal()
    }

    private func openLatencySettings(_ kind: SourceSettingsKind, sender: NSMenuItem) {
        guard let name = sender.representedObject as? String, !model.busy else { return }
        SourceSettingsPanelController(model: model, kind: kind.rawValue, name: name, page: .latency).runModal()
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
        alert.messageText = L10n.text("app.ui.choose_dns_proxy_group")
        alert.informativeText = L10n.text("app.ui.primary_dns_queries_will_follow_the_group")
        let popup = NSPopUpButton(frame: .zero, pullsDown: false)
        NativeControlStyle.configure(popup)
        popup.addItems(withTitles: model.dnsProxyGroups)
        popup.setFrameSize(NSSize(width: 320, height: popup.intrinsicContentSize.height))
        if !model.dnsProxyGroup.isEmpty, let index = model.dnsProxyGroups.firstIndex(of: model.dnsProxyGroup) { popup.selectItem(at: index) }
        alert.accessoryView = popup
        alert.addButton(withTitle: L10n.text("app.ui.apply"))
        alert.addButton(withTitle: L10n.text("app.ui.cancel"))
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
        alert.messageText = L10n.text("app.ui.add_provider_2")
        let instructions = L10n.text("app.ui.add_an_http_s_provider_to_the")
        alert.informativeText = instructions
        alert.addButton(withTitle: L10n.text("app.ui.add_provider_2"))
        alert.addButton(withTitle: L10n.text("app.ui.cancel"))
        let nameField = NSTextField()
        nameField.placeholderString = L10n.text("app.ui.provider_name")
        let urlField = NSTextField()
        urlField.placeholderString = "https://example.com/provider.yaml"
        let groupField = NSComboBox()
        groupField.isEditable = true
        groupField.placeholderString = L10n.text("app.ui.existing_or_new_group_name")
        groupField.addItems(withObjectValues: model.groups.filter {
            ["selector", "urltest", "fallback", "loadbalance"].contains($0.kind.lowercased())
        }.map(\.name))
        let intervalField = NSComboBox()
        intervalField.isEditable = true
        intervalField.addItems(withObjectValues: ["off", "3600", "21600", "86400"])
        intervalField.stringValue = "3600"
        let testURLField = NSTextField()
        testURLField.placeholderString = L10n.text("app.ui.optional_blank_inherits_configuration")
        let accessory = NativeControlStyle.form([
            (L10n.text("app.ui.name"), nameField), ("URL", urlField), (L10n.text("app.ui.group"), groupField),
            (L10n.text("app.ui.update_s"), intervalField), (L10n.text("app.ui.test_url"), testURLField)
        ])
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
                    alert.informativeText = L10n.text("app.ui.name_must_be_1_128_ascii_letters")
                    alert.window.initialFirstResponder = nameField
                    continue
                }
                guard let components = URLComponents(string: url),
                      ["http", "https"].contains(components.scheme?.lowercased() ?? ""),
                      components.host?.isEmpty == false else {
                    alert.informativeText = L10n.text("app.ui.enter_a_valid_http_or_https_url")
                    alert.window.initialFirstResponder = urlField
                    continue
                }
                guard components.user == nil && components.password == nil && components.fragment == nil else {
                    alert.informativeText = L10n.text("app.ui.use_an_http_or_https_url_without")
                    alert.window.initialFirstResponder = urlField
                    continue
                }
                guard !group.isEmpty, group.utf8.count <= 128,
                      group.unicodeScalars.allSatisfy({ scalar in
                          !CharacterSet.controlCharacters.contains(scalar) && !CharacterSet.newlines.contains(scalar)
                      }) else {
                    alert.informativeText = L10n.text("app.ui.choose_an_existing_group_or_enter_a")
                    alert.window.initialFirstResponder = groupField
                    continue
                }
                let updateInterval = intervalField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                guard Self.validUpdateInterval(updateInterval) else {
                    alert.informativeText = L10n.text("app.ui.update_interval_must_be_1_31536000_seconds_2")
                    alert.window.initialFirstResponder = intervalField
                    continue
                }
                let testURL = testURLField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                guard testURL.isEmpty || Self.validTestURL(testURL) else {
                    alert.informativeText = L10n.text("app.ui.test_url_must_be_http_s_without")
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
        alert.messageText = L10n.text("app.ui.add_and_apply_subscription_2")
        alert.informativeText = L10n.text("app.ui.downloads_and_applies_the_subscription_without_starting")
        alert.addButton(withTitle: L10n.text("app.ui.add_and_apply"))
        alert.addButton(withTitle: L10n.text("app.ui.cancel"))

        let nameField = NSTextField()
        nameField.placeholderString = L10n.text("app.ui.subscription_name")
        let urlField = NSTextField()
        urlField.placeholderString = "https://example.com/subscription"
        let scheduleField = NSComboBox()
        scheduleField.isEditable = true
        scheduleField.addItems(withObjectValues: ["off", "3600", "21600", "86400"])
        scheduleField.stringValue = "3600"; scheduleField.placeholderString = L10n.text("app.ui.seconds_or_off")
        let testURLField = NSTextField()
        testURLField.placeholderString = L10n.text("app.ui.optional_blank_inherits_configuration")
        let accessory = NativeControlStyle.form([
            (L10n.text("app.ui.name"), nameField), ("URL", urlField),
            (L10n.text("app.ui.update_s"), scheduleField), (L10n.text("app.ui.test_url"), testURLField)
        ])
        alert.accessoryView = accessory
        alert.window.initialFirstResponder = nameField
        NSApp.activate(ignoringOtherApps: true)

        while alert.runModal() == .alertFirstButtonReturn {
            let name = nameField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            let url = urlField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard name.range(of: "^[A-Za-z0-9_-]{1,128}$", options: .regularExpression) != nil else {
                alert.informativeText = L10n.text("app.ui.name_must_be_1_128_ascii_letters")
                alert.window.initialFirstResponder = nameField
                continue
            }
            guard !model.subscriptions.contains(where: { $0.name == name }) else {
                alert.informativeText = L10n.text("app.ui.a_subscription_with_this_name_already_exists")
                alert.window.initialFirstResponder = nameField
                continue
            }
            guard let components = URLComponents(string: url),
                  ["http", "https"].contains(components.scheme?.lowercased() ?? ""),
                  components.host?.isEmpty == false else {
                alert.informativeText = L10n.text("app.ui.enter_a_valid_http_or_https_url")
                alert.window.initialFirstResponder = urlField
                continue
            }
            guard components.user == nil && components.password == nil else {
                alert.informativeText = L10n.text("app.ui.remove_the_username_and_password_from_the")
                alert.window.initialFirstResponder = urlField
                continue
            }
            let schedule = scheduleField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard Self.validUpdateInterval(schedule) else {
                alert.informativeText = L10n.text("app.ui.update_interval_must_be_1_31536000_seconds_2")
                alert.window.initialFirstResponder = scheduleField; continue
            }
            let testURL = testURLField.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            guard testURL.isEmpty || Self.validTestURL(testURL) else {
                alert.informativeText = L10n.text("app.ui.test_url_must_be_http_s_without")
                alert.window.initialFirstResponder = testURLField; continue
            }
            model.addAndApplySubscription(name, url: url, schedule: schedule, testURL: testURL)
            return
        }
    }
    static func profileDeletionConfirmation(_ name: String) -> NSAlert {
        let alert = NSAlert()
        alert.messageText = L10n.format("app.ui.delete_profile", String(describing: name))
        alert.informativeText = L10n.text("app.ui.this_permanently_deletes_its_configuration_backups_local")
        alert.alertStyle = .warning
        let delete = alert.addButton(withTitle: L10n.text("app.ui.delete_profile_2"))
        let cancel = alert.addButton(withTitle: L10n.text("app.ui.cancel"))
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
        alert.messageText = L10n.format("app.ui.delete_subscription_2", String(describing: name))
        alert.informativeText = L10n.text("app.ui.this_removes_the_subscription_record_and_cached")
        alert.alertStyle = .warning
        alert.addButton(withTitle: L10n.text("app.ui.delete_subscription_3"))
        alert.addButton(withTitle: L10n.text("app.ui.cancel"))
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
        alert.window.title = L10n.text("app.ui.about_nagi")
        alert.messageText = "Nagi"
        alert.icon = NSApp.applicationIconImage
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        alert.informativeText = L10n.format("app.ui.n_na_menu_bar_proxy_client_for", String(describing: version.map { L10n.format("app.about.version", String(describing: $0)) } ?? L10n.text("app.about.version_unavailable")))
        alert.addButton(withTitle: L10n.text("app.ui.close"))
        let updateButton = alert.addButton(withTitle: L10n.text("app.ui.update"))
        updateButton.isEnabled = !model.busy
        updateButton.toolTip = L10n.text("app.ui.updates_nagi_and_reopens_this_app")
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertSecondButtonReturn { updateNagi() }
    }
    @objc private func showCapture() { captureWindow.present() }
    @objc private func stopCapture() { Task { await model.capture.mutate("stop") } }
    @objc private func showConnections() { connectionsWindow.present() }
    @objc private func showRequestRewrite() { requestRewriteWindow.present() }
    @objc private func toggleRequestRewrite() {
        let rewrite = model.requestRewrite
        let enabled = rewrite.status?.intent == true
        guard enabled ? rewrite.canOperate : rewrite.canEnable else { return }
        if !enabled {
            let alert = NSAlert()
            alert.messageText = L10n.text("app.ui.enable_request_rewrite")
            alert.informativeText = L10n.text("app.ui.intercept_domains_selected_by_enabled_rules_using")
            alert.addButton(withTitle: L10n.text("app.ui.cancel")); alert.addButton(withTitle: L10n.text("app.ui.enable_rewrite"))
            guard alert.runModal() == .alertSecondButtonReturn else { return }
        }
        Task { _ = await rewrite.perform([enabled ? "disable" : "enable"]) }
    }
    @objc private func quit() { NSApp.terminate(nil) }
    @objc private func updateNagi() {
        guard !model.busy else { return }
        let alert = NSAlert()
        alert.alertStyle = .informational
        alert.messageText = L10n.text("app.ui.update_nagi")
        alert.informativeText = L10n.text("app.ui.after_authorization_nagi_will_close_automatically_finish")
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

/// Shared by menu timestamps, including callers outside the main actor.
private final class MenuTimestampFormatter: @unchecked Sendable {
    static let shared = MenuTimestampFormatter()
    private let lock = NSLock()
    private let expression = try! NSRegularExpression(pattern: #"^(\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d)(?:\.[0-9]+)?(Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z"#)
    private let parser = DateFormatter()
    private let display = DateFormatter()

    private init() {
        parser.locale = Locale(identifier: "en_US_POSIX")
        parser.calendar = Calendar(identifier: .gregorian)
        parser.timeZone = TimeZone(secondsFromGMT: 0)
        parser.dateFormat = "yyyy-MM-dd'T'HH:mm:ssXXXXX"
        parser.isLenient = false
        display.locale = Locale(identifier: "en_US_POSIX")
        display.calendar = Calendar(identifier: .gregorian)
        display.dateFormat = "yyyy-MM-dd HH:mm"
    }

    func string(_ raw: String, timeZone: TimeZone) -> String {
        // Fractions do not affect minute precision; accept Go's nanoseconds too.
        guard let match = expression.firstMatch(in: raw, range: NSRange(raw.startIndex..., in: raw)),
              let seconds = Range(match.range(at: 1), in: raw),
              let zone = Range(match.range(at: 2), in: raw) else { return "unknown" }
        lock.lock()
        defer { lock.unlock() }
        guard let date = parser.date(from: String(raw[seconds]) + String(raw[zone])) else { return "unknown" }
        if display.timeZone != timeZone { display.timeZone = timeZone }
        return display.string(from: date)
    }
}
