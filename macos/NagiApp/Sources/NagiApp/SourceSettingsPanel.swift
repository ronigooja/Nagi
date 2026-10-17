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
    private let loadResult = NSTextField(wrappingLabelWithString: "Loading settings…")
    private let retryLoad = NSButton(title: "Retry Loading", target: nil, action: nil)
    private let closeButton = NSButton(title: "Close", target: nil, action: nil)
    private let checkContext = NSTextField(wrappingLabelWithString: "")
    private let automatic = NSButton(checkboxWithTitle: "Automatic updates", target: nil, action: nil)
    private let updateChoice = NSPopUpButton(frame: .zero, pullsDown: false)
    private let updateCustom = NSTextField()
    private let intervalMode = NSPopUpButton(frame: .zero, pullsDown: false)
    private let intervalChoice = NSPopUpButton(frame: .zero, pullsDown: false)
    private let intervalCustom = NSTextField()
    private let urlMode = NSPopUpButton(frame: .zero, pullsDown: false)
    private let urlField = NSTextField()
    private let urlReason = NSTextField(wrappingLabelWithString: "")
    private let restore = NSButton(title: "Restore Original Interval", target: nil, action: nil)
    private let original = NSTextField(wrappingLabelWithString: "")
    private var saves: [SettingsSection: NSButton] = [:]
    private var retries: [SettingsSection: NSButton] = [:]
    private var results: [SettingsSection: NSTextField] = [:]
    private var progress: [SettingsSection: NSProgressIndicator] = [:]

    init(model: AppModel, kind: String, name: String, page: Page) {
        self.model = model; self.kind = kind; self.name = name; self.page = page
        let panel = SourceSettingsPanel(contentRect: NSRect(x: 0, y: 0, width: 520, height: 620),
            styleMask: [.titled, .closable], backing: .buffered, defer: false)
        super.init(window: panel)
        panel.title = "\(page == .update ? "Update Settings" : "Latency Check Settings") · \(name)"
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
        for (field, name) in [(updateCustom, "Custom update interval in seconds"), (intervalCustom, "Custom check interval in seconds"), (urlField, "Test URL")] {
            field.delegate = self; field.setAccessibilityLabel(name)
            field.placeholderString = field === urlField ? "Full HTTP(S) URL" : "Seconds (1–31536000)"
            if field.superview != nil { field.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true }
        }
        for (control, name) in [(updateChoice, "Update interval"), (intervalMode, "Check interval source"),
                                (intervalChoice, "Check interval"), (urlMode, "Test URL source")] {
            control.setAccessibilityLabel(name)
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
        save.tag = tag(section); saves[section] = save
        if section == .update { save.keyEquivalent = "\r" }
        let actions = NSStackView()
        actions.orientation = .horizontal
        actions.spacing = 8
        let spacer = NSView()
        actions.addArrangedSubview(spacer)
        actions.addArrangedSubview(save)
        add(actions)
        actions.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true
        let spinner = NSProgressIndicator(); spinner.style = .spinning; spinner.controlSize = .small
        progress[section] = spinner; actions.insertArrangedSubview(spinner, at: 1)
        let result = label("", secondary: true); results[section] = result; add(result)
        let retry = NSButton(title: "Refresh Saved Values", target: self, action: #selector(retrySection(_:)))
        retry.tag = tag(section); retries[section] = retry; add(retry)
    }
    private func buildUpdate() {
        if kind == "subscription" { automatic.title = "Automatically download and apply" }
        add(automatic)
        add(label("Update interval", heading: true))
        updateChoice.addItems(withTitles: ["1 hour", "6 hours", "24 hours", "Custom…"])
        add(updateChoice); add(updateCustom)
        restore.target = self; restore.action = #selector(restoreClicked)
        if kind == "provider" {
            add(original)
            add(restore)
        }
        sectionFooter(.update, title: "Save")
    }
    private func buildLatency() {
        add(checkContext)
        add(label("Check interval", heading: true))
        intervalMode.addItems(withTitles: ["Use inherited settings", "Custom interval"])
        intervalChoice.addItems(withTitles: ["5 minutes", "10 minutes", "30 minutes", "1 hour", "Custom…"])
        add(intervalMode); add(intervalChoice); add(intervalCustom)
        sectionFooter(.interval, title: "Save Interval")
        let divider = NSBox(); divider.boxType = .separator; add(divider)
        divider.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true
        add(label("Test URL", heading: true))
        urlMode.addItems(withTitles: ["Use inherited settings", "Custom URL"])
        urlMode.autoenablesItems = false
        add(urlMode); add(urlReason); add(urlField)
        sectionFooter(.url, title: "Save Test URL")
    }
    private func tag(_ section: SettingsSection) -> Int { section == .update ? 0 : section == .interval ? 1 : 2 }
    private func section(_ tag: Int) -> SettingsSection { tag == 0 ? .update : tag == 1 ? .interval : .url }

    private func syncDraft() {
        if page == .update {
            state.update.automatic = automatic.state == .on
            state.update.interval.selectedIndex = updateChoice.indexOfSelectedItem
            state.update.interval.customText = updateCustom.stringValue
        } else {
            state.interval.inherited = intervalMode.indexOfSelectedItem == 0
            state.interval.interval.selectedIndex = intervalChoice.indexOfSelectedItem
            state.interval.interval.customText = intervalCustom.stringValue
            state.urlInherited = urlMode.indexOfSelectedItem == 0
            state.urlText = urlField.stringValue
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
            intervalCustom.stringValue = state.interval.interval.customText
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
            intervalCustom.isHidden = state.interval.inherited || state.interval.interval.selectedIndex != state.interval.interval.presets.count
            urlMode.isEnabled = editing && !state.pendingRefresh.contains(.url)
            urlMode.item(at: 0)?.isEnabled = state.canResetURL
            urlField.isEnabled = urlMode.isEnabled && !state.urlInherited
            urlReason.isHidden = state.canResetURL
            urlReason.stringValue = "This source has no original test URL to restore. Enter a replacement URL."
        }
        restore.isHidden = kind != "provider" || snapshot?["can_reset"]?.bool != true
        original.isHidden = restore.isHidden
        restore.isEnabled = editing && !state.pendingRefresh.contains(.update)
        saves[.update]?.isEnabled = editing && state.canSaveUpdate
        saves[.interval]?.isEnabled = editing && state.canSaveInterval
        saves[.url]?.isEnabled = editing && state.canSaveURL
        if editing && state.updateChanged && !state.update.isValid { results[.update]?.stringValue = "Enter an interval from 1 to 31536000 seconds." }
        if editing && state.intervalChanged && !state.interval.isValid { results[.interval]?.stringValue = "Enter an interval from 1 to 31536000 seconds." }
        if editing && state.urlChanged && !state.urlInherited && !SourceSettingsSession.validURL(state.urlText) {
            results[.url]?.stringValue = "Enter a complete HTTP(S) URL without credentials, whitespace, or a fragment."
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
        for result in results.values where result.stringValue.hasPrefix("Enter ") { result.stringValue = "" }
        render()
    }
    func controlTextDidBeginEditing(_ obj: Notification) {
        (window?.firstResponder as? NSTextView)?.allowsUndo = true
    }
    func controlTextDidChange(_ obj: Notification) { draftChanged() }
    @objc private func retryInitial() { Task { await loadSettings() } }
    func loadSettings() async {
        guard !loading && !loaded else { return }
        loading = true; loadResult.stringValue = "Loading settings…"; render()
        do {
            if page == .update { try await read(.update, initialize: true) }
            else { try await read(.interval, initialize: true) }
            loaded = true; syncControls(page == .update ? [.update] : [.interval, .url])
        } catch { loadResult.stringValue = "Settings could not be loaded: \(error.localizedDescription)" }
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
                    throw CLIError.command("proxy_provider_error", "Provider update interval is unavailable.")
                }
                seconds = currentSeconds
                original.stringValue = (value["source_interval_seconds"]?.int64).map { "Original cadence: \(Self.cadence($0))" } ?? "Original interval unavailable"
            } else {
                seconds = try await model.subscriptionUpdateSettings(name)
            }
            if initialize || restore { state.loadUpdate(seconds: seconds); syncControls([.update]) }
        } else {
            let value = try await model.healthcheckSettings(kind: kind, name: name)
            snapshot = value
            let seconds = value["interval_seconds"]?.int64
            let detail: String
            if kind == "provider" { detail = (value["enabled"]?.bool).map { $0 ? "Own checks enabled" : "Own checks disabled" } ?? "Own check state unavailable" }
            else { detail = (value["profile"]?.string).flatMap { $0.isEmpty ? nil : "Profile: \($0)" } ?? "Not yet applied" }
            checkContext.stringValue = detail
            if initialize {
                state.loadInterval(seconds: seconds.flatMap { $0 > 0 ? $0 : nil }, inherited: value["source"]?.string != "override")
            }
            if initialize || section == .url {
                state.loadURL(inherited: value["url_source"]?.string != "override", canReset: value["can_reset_url"]?.bool != false,
                    value: value["test_url"]?.string ?? "")
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
        results[section]?.stringValue = restore ? "Restoring original interval…" : "Saving…"; render()
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
            state.failed(section); results[section]?.stringValue = "Save failed: \(failure)"; render(); return
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
        results[section]?.stringValue = "Saved. Refreshing current values…"; render()
        do {
            try await read(section, restore: restore)
            state.refreshSucceeded(section)
            results[section]?.stringValue = "Saved."
        } catch {
            state.failed(section)
            results[section]?.stringValue = "Saved, but current values could not be refreshed: \(error.localizedDescription). Refresh Saved Values only reads; it will not save again."
        }
        render()
    }
    @objc private func closeClicked() { requestClose() }
    func windowShouldClose(_ sender: NSWindow) -> Bool { requestClose(); return false }
    private func requestClose() {
        guard !loading, state.canClose else { return }
        window?.makeFirstResponder(nil); syncDraft()
        if state.needsDiscardConfirmation {
            let alert = NSAlert(); alert.messageText = "Discard unsaved changes?"
            alert.informativeText = "Saved settings are kept. Unsaved changes in this panel will be discarded."
            alert.addButton(withTitle: "Keep Editing"); alert.addButton(withTitle: "Discard Changes")
            guard alert.runModal() == .alertSecondButtonReturn else { return }
        }
        NSApp.stopModal()
    }
    private static func cadence(_ seconds: Int64) -> String {
        if seconds == 0 { return "Manual updates only" }
        if seconds % 3600 == 0 { return seconds == 3600 ? "Every hour" : "Every \(seconds / 3600) hours" }
        if seconds % 60 == 0 { return seconds == 60 ? "Every minute" : "Every \(seconds / 60) minutes" }
        return "every \(seconds) seconds"
    }
}
