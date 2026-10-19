import AppKit

private final class SettingsDocumentView: NSView {
    override var isFlipped: Bool { true }
}

private final class SourceSettingsPanel: NSPanel {
    var requestClose: (() -> Void)?
    override func cancelOperation(_ sender: Any?) { requestClose?() }
}

/// One modal session remains active while CLI reads and writes are in flight.
@MainActor final class SourceSettingsPanelController: NSWindowController, NSWindowDelegate, NSTextFieldDelegate {
    enum Page { case update, latency }
    private let model: AppModel
    private let kind: String
    private let name: String
    private let page: Page
    private var state = SourceSettingsSession()
    private var loaded = false
    private var loading = false
    private var snapshot: JSONValue?
    private let body = NSStackView()
    private let content = NSStackView()
    private let loadResult = NSTextField(wrappingLabelWithString: L10n.text("app.ui.loading_settings"))
    private let retryLoad = NSButton(title: L10n.text("app.ui.retry_loading"), target: nil, action: nil)
    private let closeButton = NSButton(title: L10n.text("app.ui.close"), target: nil, action: nil)
    private let checkContext = NSTextField(wrappingLabelWithString: "")
    private let automatic = NSButton(checkboxWithTitle: L10n.text("app.ui.automatic_updates"), target: nil, action: nil)
    private let updateChoice = NSPopUpButton(frame: .zero, pullsDown: false)
    private let updateCustom = NSTextField()
    private let intervalMode = NSPopUpButton(frame: .zero, pullsDown: false)
    private let intervalChoice = NSPopUpButton(frame: .zero, pullsDown: false)
    private let intervalCustom = NSTextField()
    private let urlMode = NSPopUpButton(frame: .zero, pullsDown: false)
    private let urlField = NSTextField()
    private let urlReason = NSTextField(wrappingLabelWithString: "")
    private let restore = NSButton(title: L10n.text("app.ui.restore_original_interval"), target: nil, action: nil)
    private let original = NSTextField(wrappingLabelWithString: "")
    private var saves: [SettingsSection: NSButton] = [:]
    private var retries: [SettingsSection: NSButton] = [:]
    private var results: [SettingsSection: NSTextField] = [:]
    private var validationSections: Set<SettingsSection> = []
    private var progress: [SettingsSection: NSProgressIndicator] = [:]

    init(model: AppModel, kind: String, name: String, page: Page) {
        self.model = model; self.kind = kind; self.name = name; self.page = page
        let panel = SourceSettingsPanel(contentRect: NSRect(x: 0, y: 0, width: 520, height: 620),
            styleMask: [.titled, .closable], backing: .buffered, defer: false)
        super.init(window: panel)
        panel.title = "\(page == .update ? L10n.text("app.ui.update_settings") : L10n.text("app.ui.latency_check_settings")) · \(name)"
        panel.delegate = self
        panel.requestClose = { [weak self] in self?.requestClose() }
        panel.isReleasedWhenClosed = false
        build(panel)
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) is unavailable") }

    func runModal() {
        guard let window else { return }
        NSApp.activate(ignoringOtherApps: true)
        window.center()
        window.makeKeyAndOrderFront(nil)
        Task { await loadSettings() }
        NSApp.runModal(for: window)
        window.orderOut(nil)
    }

    private func build(_ panel: NSPanel) {
        for button in [retryLoad, closeButton, restore] { NativeControlStyle.configure(button) }
        automatic.controlSize = .regular
        automatic.font = .systemFont(ofSize: NSFont.systemFontSize)
        let root = NSView()
        panel.contentView = root
        body.orientation = .vertical; body.alignment = .leading; body.spacing = 16
        body.translatesAutoresizingMaskIntoConstraints = false
        let scroll = NSScrollView()
        scroll.hasVerticalScroller = true; scroll.drawsBackground = false
        scroll.translatesAutoresizingMaskIntoConstraints = false
        let document = SettingsDocumentView()
        document.translatesAutoresizingMaskIntoConstraints = false
        scroll.documentView = document
        document.addSubview(body)
        root.addSubview(scroll)
        closeButton.target = self; closeButton.action = #selector(closeClicked)
        closeButton.keyEquivalent = "\u{1b}"
        closeButton.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(closeButton)
        NSLayoutConstraint.activate([
            scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor), scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            scroll.topAnchor.constraint(equalTo: root.topAnchor), scroll.bottomAnchor.constraint(equalTo: closeButton.topAnchor, constant: -12),
            closeButton.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -24), closeButton.bottomAnchor.constraint(equalTo: root.bottomAnchor, constant: -16),
            document.leadingAnchor.constraint(equalTo: scroll.contentView.leadingAnchor),
            document.topAnchor.constraint(equalTo: scroll.contentView.topAnchor),
            document.widthAnchor.constraint(equalTo: scroll.contentView.widthAnchor),
            body.leadingAnchor.constraint(equalTo: document.leadingAnchor, constant: 24),
            body.trailingAnchor.constraint(equalTo: document.trailingAnchor, constant: -24),
            body.topAnchor.constraint(equalTo: document.topAnchor, constant: 24),
            body.bottomAnchor.constraint(equalTo: document.bottomAnchor, constant: -24)
        ])
        body.addArrangedSubview(loadResult)
        retryLoad.target = self; retryLoad.action = #selector(retryInitial)
        body.addArrangedSubview(retryLoad)
        content.orientation = .vertical; content.alignment = .leading; content.spacing = 12
        body.addArrangedSubview(content)
        content.widthAnchor.constraint(equalTo: body.widthAnchor).isActive = true
        if page == .update { buildUpdate() } else { buildLatency() }
        for view in body.arrangedSubviews + content.arrangedSubviews {
            if let field = view as? NSTextField, !field.isEditable {
                field.widthAnchor.constraint(equalTo: body.widthAnchor).isActive = true
            }
        }
        for view in [loadResult, original, urlReason, checkContext] { configureLabel(view) }
        for view in [original, urlReason, checkContext] {
            view.textColor = .secondaryLabelColor
            view.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        }
        for (field, name) in [(updateCustom, L10n.text("app.ui.custom_update_interval_in_seconds")), (intervalCustom, L10n.text("app.ui.custom_check_interval_in_seconds")), (urlField, L10n.text("app.ui.test_url"))] {
            NativeControlStyle.configure(field)
            field.delegate = self; field.setAccessibilityLabel(name)
            field.placeholderString = field === urlField ? L10n.text("app.ui.full_http_s_url") : L10n.text("app.ui.seconds_1_31536000")
            if field.superview != nil { field.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true }
        }
        for (control, name) in [(updateChoice, L10n.text("app.ui.update_interval")), (intervalMode, L10n.text("app.ui.check_interval_source")),
                                (intervalChoice, L10n.text("app.ui.check_interval")), (urlMode, L10n.text("app.ui.test_url_source"))] {
            NativeControlStyle.configure(control)
            control.setAccessibilityLabel(name)
            if control.superview != nil { control.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true }
        }
        for control in [automatic, updateChoice, intervalMode, intervalChoice, urlMode] as [NSControl] {
            control.target = self; control.action = #selector(draftChanged)
        }
        let visibleHeight = NSScreen.main?.visibleFrame.height ?? 800
        let preferredHeight: CGFloat = page == .update ? (kind == "provider" ? 280 : 220) : 480
        panel.setContentSize(NSSize(width: 520, height: min(preferredHeight, max(220, visibleHeight - 90))))
        render()
    }

    private func configureLabel(_ field: NSTextField) {
        field.font = .preferredFont(forTextStyle: .body)
        field.textColor = .labelColor
        field.maximumNumberOfLines = 0
        field.setContentCompressionResistancePriority(.required, for: .vertical)
    }
    private func label(_ text: String, heading: Bool = false, secondary: Bool = false) -> NSTextField {
        let field = NSTextField(wrappingLabelWithString: text)
        configureLabel(field)
        if heading { field.font = .preferredFont(forTextStyle: .headline) }
        if secondary { field.textColor = .secondaryLabelColor }
        return field
    }
    private func add(_ view: NSView) { content.addArrangedSubview(view) }
    private func sectionFooter(_ section: SettingsSection, title: String) {
        let save = NSButton(title: title, target: self, action: #selector(saveClicked(_:)))
        NativeControlStyle.configure(save)
        save.tag = tag(section); saves[section] = save
        if section == .update { save.keyEquivalent = "\r" }
        let actions = NSStackView()
        actions.orientation = .horizontal
        actions.alignment = .centerY
        actions.spacing = 8
        let spacer = NSView()
        actions.addArrangedSubview(spacer)
        actions.addArrangedSubview(save)
        add(actions)
        actions.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true
        let spinner = NSProgressIndicator(); spinner.style = .spinning; spinner.controlSize = .small
        progress[section] = spinner; actions.insertArrangedSubview(spinner, at: 1)
        let result = label("", secondary: true); results[section] = result; add(result)
        let retry = NSButton(title: L10n.text("app.ui.refresh_saved_values"), target: self, action: #selector(retrySection(_:)))
        NativeControlStyle.configure(retry)
        retry.tag = tag(section); retries[section] = retry; add(retry)
    }
    private func buildUpdate() {
        if kind == "subscription" { automatic.title = L10n.text("app.ui.automatically_download_and_apply") }
        add(automatic)
        add(label(L10n.text("app.ui.update_interval"), heading: true))
        updateChoice.addItems(withTitles: [L10n.text("app.ui.1_hour"), L10n.text("app.ui.6_hours"), L10n.text("app.ui.24_hours"), L10n.text("app.ui.custom")])
        add(updateChoice); add(updateCustom)
        restore.target = self; restore.action = #selector(restoreClicked)
        if kind == "provider" {
            add(original)
            add(restore)
        }
        sectionFooter(.update, title: L10n.text("app.ui.save"))
    }
    private func buildLatency() {
        if kind == "provider" { add(checkContext) }
        add(label(L10n.text("app.ui.check_interval"), heading: true))
        intervalMode.addItems(withTitles: [L10n.text("app.ui.use_inherited_settings"), L10n.text("app.ui.custom_interval")])
        intervalChoice.addItems(withTitles: [L10n.text("app.ui.5_minutes"), L10n.text("app.ui.10_minutes"), L10n.text("app.ui.30_minutes"), L10n.text("app.ui.1_hour"), L10n.text("app.ui.custom")])
        add(intervalMode); add(intervalChoice); add(intervalCustom)
        sectionFooter(.interval, title: L10n.text("app.ui.save_interval"))
        let divider = NSBox(); divider.boxType = .separator; add(divider)
        divider.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true
        add(label(L10n.text("app.ui.test_url"), heading: true))
        urlMode.addItems(withTitles: [L10n.text("app.ui.use_inherited_settings"), L10n.text("app.ui.custom_url")])
        urlMode.autoenablesItems = false
        add(urlMode); add(urlReason); add(urlField)
        sectionFooter(.url, title: L10n.text("app.ui.save_test_url"))
    }
    private func tag(_ section: SettingsSection) -> Int { section == .update ? 0 : section == .interval ? 1 : 2 }
    private func section(_ tag: Int) -> SettingsSection { tag == 0 ? .update : tag == 1 ? .interval : .url }

    private func syncDraft() {
        if page == .update {
            state.update.automatic = automatic.state == .on
            state.update.interval.selectedIndex = updateChoice.indexOfSelectedItem
            state.update.interval.customText = updateCustom.stringValue
        } else {
            let inheritedInterval = intervalMode.indexOfSelectedItem == 0
            if inheritedInterval != state.interval.inherited {
                state.selectIntervalMode(inherited: inheritedInterval, inheritedSeconds: snapshot?["inherited_interval_seconds"]?.int64)
                syncControls([.interval])
            } else if !inheritedInterval {
                state.interval.interval.selectedIndex = intervalChoice.indexOfSelectedItem
                state.interval.interval.customText = intervalCustom.stringValue
            }
            let inheritedURL = urlMode.indexOfSelectedItem == 0
            if inheritedURL != state.urlInherited {
                state.selectURLMode(inherited: inheritedURL, inheritedValue: snapshot?["inherited_test_url"]?.string ?? "")
                syncControls([.url])
            } else if !inheritedURL {
                state.urlText = urlField.stringValue
            }
        }
    }

    private func syncControls(_ sections: Set<SettingsSection>) {
        if page == .update && sections.contains(.update) {
            automatic.state = state.update.automatic ? .on : .off
            updateChoice.selectItem(at: state.update.interval.selectedIndex)
            updateCustom.stringValue = state.update.interval.customText
        }
        if page == .latency && sections.contains(.interval) {
            intervalMode.selectItem(at: state.interval.inherited ? 0 : 1)
            intervalChoice.selectItem(at: state.interval.interval.selectedIndex)
            intervalCustom.stringValue = state.interval.inherited
                ? state.interval.interval.seconds.map { L10n.format("app.ui.seconds", String(describing: $0)) } ?? ""
                : state.interval.interval.customText
        }
        if page == .latency && sections.contains(.url) {
            urlMode.selectItem(at: state.urlInherited ? 0 : 1)
            urlField.stringValue = state.urlText
        }
    }
    private func render() {
        content.isHidden = !loaded
        retryLoad.isHidden = loaded || loading
        loadResult.isHidden = loaded
        closeButton.isEnabled = !loading && state.canClose
        window?.standardWindowButton(.closeButton)?.isEnabled = closeButton.isEnabled
        let editing = loaded && !loading && state.busy == nil
        if page == .update {
            automatic.isEnabled = editing && !state.pendingRefresh.contains(.update)
            updateChoice.isEnabled = automatic.isEnabled && state.update.automatic
            updateCustom.isEnabled = updateChoice.isEnabled
            updateCustom.isHidden = state.update.interval.selectedIndex != state.update.interval.presets.count
        } else {
            intervalMode.isEnabled = editing && !state.pendingRefresh.contains(.interval)
            intervalChoice.isEnabled = intervalMode.isEnabled && !state.interval.inherited
            intervalCustom.isEnabled = intervalChoice.isEnabled
            intervalChoice.isHidden = state.interval.inherited
            intervalCustom.isHidden = !state.interval.inherited && state.interval.interval.selectedIndex != state.interval.interval.presets.count
            intervalCustom.placeholderString = state.interval.inherited
                ? inheritedPlaceholder(snapshot?["inherited_interval_status"]?.string)
                : L10n.text("app.ui.seconds_1_31536000")
            urlMode.isEnabled = editing && !state.pendingRefresh.contains(.url)
            urlMode.item(at: 0)?.isEnabled = state.canResetURL
            urlField.isEnabled = urlMode.isEnabled && !state.urlInherited
            urlField.placeholderString = state.urlInherited ? inheritedPlaceholder(snapshot?["inherited_url_status"]?.string) : L10n.text("app.ui.full_http_s_url")
            urlReason.isHidden = state.canResetURL
            urlReason.stringValue = L10n.text("app.ui.this_source_has_no_original_test_url")
        }
        restore.isHidden = kind != "provider" || snapshot?["can_reset"]?.bool != true
        original.isHidden = restore.isHidden
        restore.isEnabled = editing && !state.pendingRefresh.contains(.update)
        saves[.update]?.isEnabled = editing && state.canSaveUpdate
        saves[.interval]?.isEnabled = editing && state.canSaveInterval
        saves[.url]?.isEnabled = editing && state.canSaveURL
        if editing && state.updateChanged && !state.update.isValid { validationSections.insert(.update); results[.update]?.stringValue = L10n.text("app.ui.enter_an_interval_from_1_to_31536000") }
        if editing && state.intervalChanged && !state.interval.isValid { validationSections.insert(.interval); results[.interval]?.stringValue = L10n.text("app.ui.enter_an_interval_from_1_to_31536000") }
        if editing && state.urlChanged && !state.urlInherited && !SourceSettingsSession.validURL(state.urlText) {
            validationSections.insert(.url)
            results[.url]?.stringValue = L10n.text("app.ui.enter_a_complete_http_s_url_without")
        }
        for section in [SettingsSection.update, .interval, .url] {
            results[section]?.isHidden = results[section]?.stringValue.isEmpty != false
            let working = state.busy == section
            progress[section]?.isHidden = !working
            if working { progress[section]?.startAnimation(nil) } else { progress[section]?.stopAnimation(nil) }
            retries[section]?.isHidden = !state.pendingRefresh.contains(section)
            retries[section]?.isEnabled = editing
        }

    }

    @objc private func draftChanged() {
        syncDraft()
        for section in validationSections { results[section]?.stringValue = "" }
        validationSections.removeAll()
        render()
    }
    func controlTextDidBeginEditing(_ obj: Notification) {
        (window?.firstResponder as? NSTextView)?.allowsUndo = true
    }
    func controlTextDidChange(_ obj: Notification) { draftChanged() }
    @objc private func retryInitial() { Task { await loadSettings() } }
    func loadSettings() async {
        guard !loading && !loaded else { return }
        loading = true; loadResult.stringValue = L10n.text("app.ui.loading_settings"); render()
        do {
            if page == .update { try await read(.update, initialize: true) }
            else { try await read(.interval, initialize: true) }
            loaded = true; syncControls(page == .update ? [.update] : [.interval, .url])
        } catch { loadResult.stringValue = L10n.format("app.ui.settings_could_not_be_loaded", String(describing: error.localizedDescription)) }
        loading = false; render()
        if loaded { window?.makeFirstResponder(page == .update ? automatic : intervalMode) }
    }

    private func read(_ section: SettingsSection, initialize: Bool = false, restore: Bool = false) async throws {
        if page == .update {
            let seconds: Int64
            if kind == "provider" {
                let value = try await model.providerUpdateSettings(name)
                snapshot = value
                guard let currentSeconds = value["interval_seconds"]?.int64 else {
                    throw CLIError.command("proxy_provider_error", L10n.text("app.ui.provider_update_interval_is_unavailable"))
                }
                seconds = currentSeconds
                original.stringValue = (value["source_interval_seconds"]?.int64).map { L10n.format("app.ui.original_cadence", String(describing: Self.cadence($0))) } ?? L10n.text("app.ui.original_interval_unavailable")
            } else {
                seconds = try await model.subscriptionUpdateSettings(name)
            }
            if initialize || restore { state.loadUpdate(seconds: seconds); syncControls([.update]) }
        } else {
            let value = try await model.healthcheckSettings(kind: kind, name: name)
            snapshot = value
            let seconds = value["interval_seconds"]?.int64
            if kind == "provider" {
                checkContext.stringValue = (value["enabled"]?.bool).map { $0 ? L10n.text("app.ui.own_checks_enabled") : L10n.text("app.ui.own_checks_disabled") } ?? L10n.text("app.ui.own_check_state_unavailable")
            }
            if initialize || section == .interval {
                let inherited = value["source"]?.string != "override"
                let displayed = inherited ? value["inherited_interval_seconds"]?.int64 : seconds
                state.loadInterval(seconds: displayed.flatMap { $0 > 0 ? $0 : nil }, inherited: inherited)
                syncControls([.interval])
            }
            if initialize || section == .url {
                let inherited = value["url_source"]?.string != "override"
                state.loadURL(inherited: inherited, canReset: value["can_reset_url"]?.bool != false,
                    value: (inherited ? value["inherited_test_url"]?.string : value["test_url"]?.string) ?? "")
                syncControls([.url])
            }
        }
    }

    @objc private func saveClicked(_ sender: NSButton) {
        window?.makeFirstResponder(nil); syncDraft()
        let section = section(sender.tag)
        guard (section == .update ? state.canSaveUpdate : section == .interval ? state.canSaveInterval : state.canSaveURL) else { return }
        Task { await save(section) }
    }
    @objc private func restoreClicked() { Task { await save(.update, restore: true) } }
    private func save(_ section: SettingsSection, restore: Bool = false) async {
        guard !loading, !state.pendingRefresh.contains(section), state.begin(section) else { return }
        results[section]?.stringValue = restore ? L10n.text("app.ui.restoring_original_interval") : L10n.text("app.ui.saving"); render()
        let failure: String?
        switch section {
        case .update:
            let value = state.update.value ?? 0
            let args = kind == "provider" ? (restore ? ["proxy", "provider", "interval", "reset", name] : ["proxy", "provider", "interval", "set", name, value == 0 ? "off" : String(value)]) : ["subscription", "schedule", name, value == 0 ? "off" : String(value)]
            failure = await model.saveSourceSetting(args)
        case .interval:
            failure = await model.saveHealthcheckSettings(kind: kind, name: name, seconds: state.interval.value)
        case .url:
            failure = await model.saveSourceSetting(["proxy", "healthcheck", "url", state.urlInherited ? "reset" : "set", kind, name] + (state.urlInherited ? [] : [state.urlText]))
        }
        if let failure {
            state.failed(section); results[section]?.stringValue = L10n.format("app.ui.save_failed", String(describing: failure)); render(); return
        }
        state.saveSucceeded(section)
        if section == .url { syncControls([.url]) }
        await refreshSaved(section, restore: restore)
    }
    @objc private func retrySection(_ sender: NSButton) {
        let section = section(sender.tag)
        guard state.pendingRefresh.contains(section), state.begin(section) else { return }
        Task { await refreshSaved(section, restore: section == .update) }
    }
    private func refreshSaved(_ section: SettingsSection, restore: Bool = false) async {
        results[section]?.stringValue = L10n.text("app.ui.saved_refreshing_current_values"); render()
        do {
            try await read(section, restore: restore)
            state.refreshSucceeded(section)
            results[section]?.stringValue = L10n.text("app.ui.saved")
        } catch {
            state.failed(section)
            results[section]?.stringValue = L10n.format("app.ui.saved_but_current_values_could_not_be", String(describing: error.localizedDescription))
        }
        render()
    }
    @objc private func closeClicked() { requestClose() }
    func windowShouldClose(_ sender: NSWindow) -> Bool { requestClose(); return false }
    private func requestClose() {
        guard !loading, state.canClose else { return }
        window?.makeFirstResponder(nil); syncDraft()
        if state.needsDiscardConfirmation {
            let alert = NSAlert(); alert.messageText = L10n.text("app.ui.discard_unsaved_changes")
            alert.informativeText = L10n.text("app.ui.saved_settings_are_kept_unsaved_changes_in")
            alert.addButton(withTitle: L10n.text("app.ui.keep_editing")); alert.addButton(withTitle: L10n.text("app.ui.discard_changes"))
            guard alert.runModal() == .alertSecondButtonReturn else { return }
        }
        NSApp.stopModal()
    }
    private func inheritedPlaceholder(_ status: String?) -> String {
        switch status {
        case "mixed": return L10n.text("app.ui.varies_by_group_or_provider")
        case "not_applied": return L10n.text("app.ui.not_yet_applied")
        default: return L10n.text("app.ui.inherited_value_unavailable")
        }
    }
    private static func cadence(_ seconds: Int64) -> String {
        if seconds == 0 { return L10n.text("app.ui.manual_updates_only") }
        if seconds % 3600 == 0 { return seconds == 3600 ? L10n.text("app.ui.every_hour") : L10n.format("app.ui.every_hours", String(describing: seconds / 3600)) }
        if seconds % 60 == 0 { return seconds == 60 ? L10n.text("app.ui.every_minute") : L10n.format("app.ui.every_minutes", String(describing: seconds / 60)) }
        return L10n.format("app.settings.cadence_seconds", String(describing: seconds))
    }
}
