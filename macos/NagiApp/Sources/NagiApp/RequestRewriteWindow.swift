import AppKit
import Combine
import SwiftUI

@MainActor final class RewritePresentation: ObservableObject {
    let model: RequestRewriteModel
    @Published var revision = 0
    init(_ model: RequestRewriteModel) {
        self.model = model
        model.onChange = { [weak self] in self?.revision &+= 1 }
    }
}

/// Reuses the established accessory-window visibility/minimization hooks.
@MainActor final class RequestRewriteWindowController: NSWindowController, NSWindowDelegate {
    let model: RequestRewriteModel
    private var observers: [NSObjectProtocol] = []
    private var titleObservation: AnyCancellable?
    init(model: RequestRewriteModel) {
        self.model = model
        let window = ConnectionsWindow(contentRect: NSRect(x: 0, y: 0, width: 1080, height: 760),
            styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = L10n.text("app.ui.nagi_request_rewrite"); window.minimumFrameSize = NSSize(width: 850, height: 620)
        window.isReleasedWhenClosed = false; window.delegate = self
        window.onVisibilityChange = { [weak self] in self?.updateVisibility() }
        let presentation = RewritePresentation(model)
        titleObservation = presentation.$revision.sink { [weak window, weak model] _ in
            window?.title = model?.status?.profile.map { L10n.format("app.ui.nagi_request_rewrite_2", String(describing: $0)) } ?? L10n.text("app.ui.nagi_request_rewrite")
        }
        let content = NSHostingView(rootView: RequestRewriteView(presentation: presentation))
        // AppKit owns the frame; tab contents must not publish window size constraints.
        content.sizingOptions = []
        window.contentView = content
        if let screen = window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame {
            window.minimumFrameSize = NSSize(width: min(850, screen.width * 0.8), height: min(620, screen.height * 0.8))
            window.setFrame(Self.initialFrame(in: screen), display: false)
        } else {
            window.center()
        }
        observers.append(NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.refreshOnForeground() }
        })
        for name in [NSApplication.didHideNotification, NSApplication.didUnhideNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                Task { @MainActor in self?.updateVisibility() }
            })
        }
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    static func initialFrame(in screen: NSRect) -> NSRect {
        let size = NSSize(width: min(1080, screen.width * 0.9), height: min(760, screen.height * 0.9))
        return NSRect(x: screen.midX - size.width / 2, y: screen.midY - size.height / 2,
                      width: size.width, height: size.height)
    }
    deinit { observers.forEach { NotificationCenter.default.removeObserver($0) } }
    func present() {
        guard let window else { return }
        if window.isMiniaturized || (window as? ConnectionsWindow)?.minimizing == true { window.deminiaturize(nil) }
        showWindow(nil); NSApp.activate(ignoringOtherApps: true); window.makeKeyAndOrderFront(nil); updateVisibility()
    }
    private func updateVisibility() {
        model.setVisible((window as? ConnectionsWindow)?.readingPaused == false && window?.isVisible == true && window?.isMiniaturized == false && !NSApp.isHidden)
    }
    private func refreshOnForeground() {
        // Opening and foreground events share a read when they arrive together.
        if !model.isReading { model.refresh() }
    }
    func windowDidBecomeKey(_ notification: Notification) { refreshOnForeground() }
    func windowWillClose(_ notification: Notification) { model.setVisible(false) }
    func windowWillMiniaturize(_ notification: Notification) { (window as? ConnectionsWindow)?.beginMiniaturization() }
    func windowDidMiniaturize(_ notification: Notification) { updateVisibility() }
    func windowDidDeminiaturize(_ notification: Notification) { (window as? ConnectionsWindow)?.finishDeminiaturization(); updateVisibility() }
    func windowDidChangeOcclusionState(_ notification: Notification) { updateVisibility() }
}

private struct RewriteScriptSession: Identifiable {
    let id = UUID(); var draft: RewriteScriptDraft; let originalID: String?; let profile: String
}
private struct RewriteEditorSession: Identifiable {
    let id = UUID(); var draft: RewriteRuleDraft; let originalID: String?; let profile: String
}

@MainActor struct RequestRewriteView: View {
    @ObservedObject var presentation: RewritePresentation
    @State private var selectedScript: String?
    @State private var selectedRule: String?
    @State private var scriptSession: RewriteScriptSession?
    @State private var ruleSession: RewriteEditorSession?
    @State private var localError: String?
    @State private var editTask: Task<Void, Never>?
    @State private var loadingEditor = false
    @State private var page = 0
    @State private var showingHelp = false
    private var model: RequestRewriteModel { presentation.model }
    var body: some View {
        GeometryReader { geometry in
            content.frame(width: geometry.size.width, height: geometry.size.height, alignment: .topLeading)
        }
        .nativeFormControls()
        .onChange(of: model.status?.profile) { _ in selectedScript = nil; selectedRule = nil }
        .sheet(item: $ruleSession) { session in
            RewriteRuleEditor(presentation: presentation, draft: session.draft, originalID: session.originalID, profile: session.profile)
        }
        .sheet(item: $scriptSession) { session in
            RewriteScriptEditor(presentation: presentation, draft: session.draft, originalID: session.originalID, profile: session.profile)
        }
    }
    private var content: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Label(model.stale ? L10n.text("app.ui.status_unavailable_2") : (model.status.map { L10n.protocolLabel($0.actual) } ?? L10n.text("app.ui.unknown")),
                    systemImage: model.stale ? "exclamationmark.triangle" : "circle.fill")
                    .foregroundColor(model.stale ? .orange : .secondary)
                Spacer()
                if model.isReading || model.operationInProgress || loadingEditor { ProgressView().controlSize(.small) }
                Button(L10n.text("app.ui.refresh")) { model.refresh() }.disabled(model.isReading || !model.canOperate)
                Button(model.status?.intent == true ? L10n.text("app.ui.disable_rewrite") : L10n.text("app.ui.enable_rewrite")) { toggle() }
                    .disabled(model.status?.intent == true ? !model.canOperate : !model.canEnable)
                Button { showingHelp.toggle() } label: { Image(systemName: "questionmark.circle") }
                    .accessibilityLabel(L10n.text("app.ui.request_rewrite_help"))
                    .popover(isPresented: $showingHelp) {
                        VStack(alignment: .leading, spacing: 12) {
                            Text(verbatim: L10n.text("app.ui.enabled_rules_select_the_domains_to_intercept"))
                            Text(verbatim: L10n.text("app.ui.activation_requires_running_tun_a_compatible_engine"))
                            Text(verbatim: L10n.text("app.ui.certificate_pinning_client_certificates_ech_and_separate"))
                            Text(verbatim: L10n.text("app.ui.closing_this_window_leaves_rewriting_running"))
                        }.padding().frame(width: 360)
                    }
            }
            TabView(selection: $page) {
                rulesPage.tabItem { Text(verbatim: L10n.text("app.ui.rules")) }.tag(0)
                scriptsPage.tabItem { Text(verbatim: L10n.text("app.ui.scripts")) }.tag(1)
                statusPage.tabItem { Text(verbatim: L10n.text("app.ui.status")) }.tag(2)
                RewritePreviewView(presentation: presentation).tabItem { Text(verbatim: L10n.text("app.ui.local_preview")) }.tag(3)
                caPage.tabItem { Text(verbatim: L10n.text("app.ui.certificate_trust")) }.tag(4)
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
            if localError != nil || model.error != nil || model.notice != nil {
                ScrollView {
                    VStack(alignment: .leading, spacing: 6) {
                        if let message = localError ?? model.error { Text(verbatim: message).foregroundColor(.red).textSelection(.enabled) }
                        if let notice = model.notice { Text(verbatim: notice).foregroundColor(.secondary).textSelection(.enabled) }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }.frame(maxHeight: 80)
            }
            HStack {
                Text(verbatim: model.updated.map { L10n.format("app.ui.updated", String(describing: Self.time($0))) } ?? L10n.text("app.ui.not_updated_yet")).font(.caption).foregroundColor(.secondary)
                Spacer()
            }
        }
        .padding(18)
    }
    static func time(_ date: Date) -> String {
        let formatter = DateFormatter(); formatter.dateFormat = "yyyy-MM-dd HH:mm"; return formatter.string(from: date)
    }
    private var statusPage: some View {
        VStack(alignment: .leading, spacing: 14) {
            ScrollView {
                if let status = model.status {
                    VStack(alignment: .leading, spacing: 16) {
                        GroupBox(L10n.text("app.ui.runtime")) {
                            VStack(spacing: 10) {
                                RewriteDetailRow(L10n.text("app.ui.state"), value: L10n.protocolLabel(status.actual))
                                RewriteDetailRow(L10n.text("app.ui.saved_enable_setting"), value: RewriteStatus.bool(status.intent))
                                RewriteDetailRow(L10n.text("app.ui.tun_running"), value: RewriteStatus.bool(status.tunRunning))
                                RewriteDetailRow(L10n.text("app.ui.rewrite_service_running"), value: RewriteStatus.bool(status.value["service_running"]?.bool))
                                RewriteDetailRow(L10n.text("app.ui.certificate"), value: L10n.protocolLabel(status.value["ca"]?["state"]?.string ?? L10n.text("app.state.unknown")))
                                RewriteDetailRow(L10n.text("app.ui.trust"), value: L10n.protocolLabel(status.value["ca"]?["trust"]?.string ?? L10n.text("app.state.unknown")))
                            }.padding(8)
                        }
                        if let error = status.value["last_error"]?.displayString, !error.isEmpty {
                            Text(verbatim: error).foregroundColor(.red).textSelection(.enabled)
                        }
                        if let recovery = status.value["recovery"]?.displayString ?? status.value["recovery_command"]?.string, !recovery.isEmpty {
                            Text(verbatim: recovery).textSelection(.enabled)
                        }
                        DisclosureGroup(L10n.text("app.ui.runtime_details")) {
                            VStack(spacing: 10) {
                                RewriteDetailRow(L10n.text("app.ui.schema"), value: status.value["config_version"]?.int64.map(String.init) ?? L10n.text("app.state.unknown"))
                                RewriteDetailRow(L10n.text("app.ui.backend"), value: L10n.protocolLabel(status.value["backend"]?.string ?? "unknown"))
                                RewriteDetailRow(L10n.text("app.ui.protocol"), value: status.value["protocol_version"]?.int64.map(String.init) ?? L10n.text("app.state.unknown"))
                                RewriteDetailRow(L10n.text("app.ui.configured_version"), value: status.value["configured_rule_version"]?.string ?? L10n.text("app.state.unknown"))
                                RewriteDetailRow(L10n.text("app.ui.active_version"), value: status.value["active_rule_version"]?.string ?? L10n.text("app.state.unknown"))
                                RewriteDetailRow(L10n.text("app.ui.generation"), value: status.value["generation"]?.string ?? L10n.text("app.state.unknown"))
                                RewriteDetailRow(L10n.text("app.ui.engine_epoch"), value: status.value["engine_epoch"]?.string ?? L10n.text("app.state.unknown"))
                            }.padding(.top, 10)
                        }
                    }.padding(2)
                } else {
                    Text(verbatim: L10n.text("app.rewrite.refresh_unavailable")).foregroundColor(.secondary)
                }
            }
            HStack {
                Button(L10n.text("app.ui.validate_rules")) { run(["validate"], profile: model.status?.profile) }
                Button(L10n.text("app.ui.import_rules")) { pickInput(command: "import") }
                Button(L10n.text("app.ui.export_rules")) { saveExport(certificate: false) }
            }.disabled(!model.canOperate)
        }.padding()
    }
    private var scriptsPage: some View {
        VStack(alignment: .leading) {
            List(selection: $selectedScript) {
                ForEach(model.scripts, id: \.id) { item in
                    HStack { Text(verbatim: item.id).bold(); Spacer(); Text(verbatim: item.bodyAccess == "buffer" ? L10n.text("app.ui.buffered_body") : L10n.text("app.ui.metadata_only")).foregroundColor(.secondary) }.tag(item.id)
                }
            }
            .overlay { if model.scripts.isEmpty { Text(verbatim: model.listsFresh ? L10n.text("app.ui.no_scripts") : L10n.text("app.ui.scripts_unavailable")).foregroundColor(.secondary).allowsHitTesting(false) } }
            HStack {
                Button(L10n.text("app.ui.add_script")) { if let profile = model.status?.profile { scriptSession = RewriteScriptSession(draft: RewriteScriptDraft(), originalID: nil, profile: profile) } }
                Button(L10n.text("app.ui.edit_script")) { editScript() }.disabled(selectedScript == nil || loadingEditor)
                Button(L10n.text("app.ui.remove_2")) {
                    guard let id = selectedScript, model.scripts.contains(where: { $0.id == id }), let profile = model.status?.profile,
                        confirm(L10n.text("app.ui.remove_script"), L10n.format("app.ui.remove_scripts_referenced_by_rules_cannot_be", String(describing: id)), action: L10n.text("app.ui.remove_script_2")) else { return }
                    run(["scripts", "remove", id], profile: profile)
                }.disabled(selectedScript == nil)
            }.disabled(!model.canManage)
        }.padding()
    }
    private var rulesPage: some View {
        VStack(alignment: .leading) {
            List(selection: $selectedRule) {
                ForEach(Array(model.rules.enumerated()), id: \.element.id) { index, item in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: "\(index + 1). \(item.id) · \(item.enabled ? L10n.text("app.ui.enabled") : L10n.text("app.state.disabled"))").bold()
                        Text(verbatim: "\(RewriteLabels.stage(item.stage)) · \(item.domain) · \(item.methods.isEmpty ? L10n.text("app.rewrite.any_method") : item.methods.joined(separator: ", ")) · \(item.path)")
                        Text(verbatim: item.kinds.map(RewriteLabels.operation).joined(separator: " → ")).font(.caption).foregroundColor(.secondary)
                    }.padding(.vertical, 4).tag(item.id)
                }
            }
            .overlay { if model.rules.isEmpty { Text(verbatim: model.listsFresh ? L10n.text("app.ui.no_rules") : L10n.text("app.ui.rules_unavailable")).foregroundColor(.secondary).allowsHitTesting(false) } }
            HStack {
                Button(L10n.text("app.ui.add_rule")) { if let profile = model.status?.profile { ruleSession = RewriteEditorSession(draft: RewriteRuleDraft(), originalID: nil, profile: profile) } }
                Button(L10n.text("app.ui.edit_rule")) { editRule() }.disabled(selectedRule == nil || loadingEditor)
                Button(selectedSummary?.enabled == true ? L10n.text("app.ui.disable_rule") : L10n.text("app.ui.enable_rule")) {
                    guard let item = selectedSummary else { return }
                    run(["rules", item.enabled ? "disable" : "enable", item.id], profile: model.status?.profile)
                }.disabled(selectedSummary == nil)
                Button(L10n.text("app.ui.remove_2")) {
                    guard let item = selectedSummary, confirm(L10n.text("app.ui.remove_rule"), L10n.format("app.ui.remove_from_this_profile", String(describing: item.id)), action: L10n.text("app.ui.remove_rule_2")) else { return }
                    run(["rules", "remove", item.id], profile: model.status?.profile)
                }.disabled(selectedSummary == nil)
                Spacer()
                Button { moveRule(-1) } label: { Image(systemName: "arrow.up") }
                    .disabled(!canMove(-1)).help(L10n.text("app.ui.move_rule_up")).accessibilityLabel(L10n.text("app.ui.move_rule_up"))
                Button { moveRule(1) } label: { Image(systemName: "arrow.down") }
                    .disabled(!canMove(1)).help(L10n.text("app.ui.move_rule_down")).accessibilityLabel(L10n.text("app.ui.move_rule_down"))
            }.disabled(!model.canManage)
        }.padding()
    }
    private var selectedSummary: RewriteRuleSummary? { model.rules.first { $0.id == selectedRule } }
    private func canMove(_ offset: Int) -> Bool {
        guard let index = model.rules.firstIndex(where: { $0.id == selectedRule }) else { return false }
        return model.rules.indices.contains(index + offset)
    }
    private func moveRule(_ offset: Int) {
        guard canMove(offset), let index = model.rules.firstIndex(where: { $0.id == selectedRule }) else { return }
        var ids = model.rules.map(\.id); ids.swapAt(index, index + offset)
        run(["rules", "reorder", "-"], input: RewriteJSON.array(ids.map(RewriteJSON.string)).data, profile: model.status?.profile)
    }
    private var caPage: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                GroupBox(L10n.text("app.ui.certificate")) {
                    VStack(spacing: 10) {
                        RewriteDetailRow(L10n.text("app.ui.state"), value: L10n.protocolLabel(model.caStatus?["state"]?.string ?? L10n.text("app.state.unknown")))
                        RewriteDetailRow(L10n.text("app.ui.trust"), value: L10n.protocolLabel(model.caStatus?["trust"]?.string ?? L10n.text("app.state.unknown")))
                        RewriteDetailRow(L10n.text("app.ui.fingerprint"), value: model.caStatus?["fingerprint"]?.string ?? L10n.text("app.state.unknown"))
                        RewriteDetailRow(L10n.text("app.ui.valid_from"), value: certificateTime(model.caStatus?["not_before"]?.string))
                        RewriteDetailRow(L10n.text("app.ui.valid_until"), value: certificateTime(model.caStatus?["not_after"]?.string))
                    }.padding(8)
                }
                HStack {
                    Button(L10n.text("app.ui.refresh_ca_status")) { run(["ca", "status"]) }
                    Button(L10n.text("app.ui.generate_local_ca")) { run(["ca", "generate"]) }
                    Button(L10n.text("app.ui.export_public_certificate")) { saveExport(certificate: true) }
                }
                HStack {
                    Button(L10n.text("app.ui.install_login_keychain_trust")) {
                        if confirm(L10n.text("app.ui.trust_the_nagi_ca"), L10n.text("app.ui.install_this_ca_in_your_login_keychain"), action: L10n.text("app.ui.install_trust")) { run(["ca", "trust", "install"]) }
                    }
                    Button(L10n.text("app.ui.remove_login_keychain_trust")) {
                        if confirm(L10n.text("app.ui.remove_nagi_ca_trust"), L10n.text("app.ui.stop_rewriting_and_remove_login_keychain_trust"), action: L10n.text("app.ui.remove_trust")) { run(["ca", "trust", "remove"]) }
                    }
                }
            }.padding().disabled(!model.canOperate)
        }
    }
    private func certificateTime(_ raw: String?) -> String {
        guard model.caStatus?["state"]?.string == "generated", let raw, !raw.hasPrefix("0001-"), let date = ISO8601DateFormatter().date(from: raw) else { return L10n.text("app.state.unknown") }; return Self.time(date)
    }
    private func toggle() {
        if model.status?.intent == true { run(["disable"]); return }
        guard model.canEnable, confirm(L10n.text("app.ui.enable_request_rewrite"), L10n.text("app.ui.intercept_the_domains_selected_by_enabled_rules"), action: L10n.text("app.ui.enable_rewrite")) else { return }
        run(["enable"])
    }
    private func run(_ args: [String], input: Data? = nil, profile: String? = nil) {
        localError = nil
        Task { _ = await model.perform(args, input: input, profile: profile) }
    }
    private func editRule() {
        guard let item = selectedSummary, let profile = model.status?.profile, model.canManage else { return }
        loadingEditor = true; localError = nil
        editTask = Task {
            defer { loadingEditor = false; editTask = nil }
            do {
                let draft = try await model.sensitiveRule(id: item.id, profile: profile)
                try Task.checkCancellation()
                ruleSession = RewriteEditorSession(draft: draft, originalID: item.id, profile: profile)
            } catch is CancellationError { } catch { localError = error.localizedDescription }
        }
    }
    private func editScript() {
        guard let id = selectedScript, model.scripts.contains(where: { $0.id == id }), let profile = model.status?.profile, model.canManage else { return }
        loadingEditor = true; localError = nil
        editTask = Task {
            defer { loadingEditor = false; editTask = nil }
            do {
                let draft = try await model.sensitiveScript(id: id, profile: profile)
                try Task.checkCancellation()
                scriptSession = RewriteScriptSession(draft: draft, originalID: id, profile: profile)
            } catch is CancellationError { } catch { localError = error.localizedDescription }
        }
    }
    private func pickInput(command: String) {
        guard let profile = model.status?.profile else { return }
        let panel = NSOpenPanel(); panel.canChooseDirectories = false; panel.allowsMultipleSelection = false
        panel.message = L10n.text("app.ui.choose_a_private_local_json_document_owner")
        if panel.runModal() == .OK, let file = panel.url { run([command, file.path], profile: profile) }
    }
    private func saveExport(certificate: Bool) {
        let panel = NSSavePanel(); panel.nameFieldStringValue = certificate ? "nagi-ca.pem" : "request-rewrite.json"
        panel.message = certificate ? L10n.text("app.ui.export_only_the_public_certificate_the_private") : L10n.text("app.ui.sensitive_export_may_contain_credentials_and_complete")
        guard panel.runModal() == .OK, let file = panel.url else { return }
        let args = certificate ? ["ca", "export", file.path] : ["export", file.path] + (model.status?.profile.map { ["--profile", $0] } ?? [])
        Task {
            do { _ = try await model.exportFile(args); localError = nil }
            catch { localError = error.localizedDescription }
        }
    }
}

private struct RewriteDetailRow: View {
    let label: String
    let value: String
    init(_ label: String, value: String) { self.label = label; self.value = value }
    var body: some View {
        HStack(alignment: .top, spacing: 16) {
            Text(verbatim: label).foregroundColor(.secondary).frame(width: 160, alignment: .trailing)
            Text(verbatim: value).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

@MainActor private func confirm(_ title: String, _ detail: String, action: String) -> Bool {
    let alert = NSAlert(); alert.messageText = title; alert.informativeText = detail; alert.alertStyle = .warning
    alert.addButton(withTitle: L10n.text("app.ui.cancel")); alert.addButton(withTitle: action)
    return alert.runModal() == .alertSecondButtonReturn
}
