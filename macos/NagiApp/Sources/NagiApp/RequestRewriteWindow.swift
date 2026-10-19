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
        window.title = "Nagi Request Rewrite"; window.minSize = NSSize(width: 850, height: 620)
        window.isReleasedWhenClosed = false; window.delegate = self
        window.onVisibilityChange = { [weak self] in self?.updateVisibility() }
        let presentation = RewritePresentation(model)
        titleObservation = presentation.$revision.sink { [weak window, weak model] _ in
            window?.title = model?.status?.profile.map { "Nagi Request Rewrite — \($0)" } ?? "Nagi Request Rewrite"
        }
        window.contentView = NSHostingView(rootView: RequestRewriteView(presentation: presentation))
        window.center()
        for name in [NSApplication.didHideNotification, NSApplication.didUnhideNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                Task { @MainActor in self?.updateVisibility() }
            })
        }
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    deinit { observers.forEach { NotificationCenter.default.removeObserver($0) } }
    func present() {
        guard let window else { return }
        if window.isMiniaturized || (window as? ConnectionsWindow)?.minimizing == true { window.deminiaturize(nil) }
        showWindow(nil); NSApp.activate(ignoringOtherApps: true); window.makeKeyAndOrderFront(nil); updateVisibility()
    }
    private func updateVisibility() {
        model.setVisible((window as? ConnectionsWindow)?.readingPaused == false && window?.isVisible == true && window?.isMiniaturized == false && !NSApp.isHidden)
    }
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
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Label(model.stale ? "Status unavailable" : (model.status?.actual.capitalized ?? "Unknown"),
                    systemImage: model.stale ? "exclamationmark.triangle" : "circle.fill")
                    .foregroundColor(model.stale ? .orange : .secondary)
                Spacer()
                if model.isReading || model.operationInProgress || loadingEditor { ProgressView().controlSize(.small) }
                Button("Refresh") { model.refresh() }.disabled(model.isReading || !model.canOperate)
                Button(model.status?.intent == true ? "Disable Rewrite" : "Enable Rewrite") { toggle() }
                    .disabled(model.status?.intent == true ? !model.canOperate : !model.canEnable)
                Button { showingHelp.toggle() } label: { Image(systemName: "questionmark.circle") }
                    .accessibilityLabel("Request rewrite help")
                    .popover(isPresented: $showingHelp) {
                        VStack(alignment: .leading, spacing: 12) {
                            Text("Enabled rules select the domains to intercept and run in list order within each stage. With no enabled rules, rewriting stays idle.")
                            Text("Activation requires running TUN, a compatible engine and a trusted local CA. Configure the CA in Certificate & Trust.")
                            Text("Certificate pinning, client certificates, ECH and separate application trust stores can prevent interception. Local preview sends no network requests.")
                            Text("Closing this window leaves rewriting running.")
                        }.padding().frame(width: 360)
                    }
            }
            TabView(selection: $page) {
                rulesPage.tabItem { Text("Rules") }.tag(0)
                scriptsPage.tabItem { Text("Scripts") }.tag(1)
                statusPage.tabItem { Text("Status") }.tag(2)
                RewritePreviewView(presentation: presentation).tabItem { Text("Local Preview") }.tag(3)
                caPage.tabItem { Text("Certificate & Trust") }.tag(4)
            }
            if let message = localError ?? model.error { Text(message).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            if let notice = model.notice { Text(notice).foregroundColor(.secondary).textSelection(.enabled) }
            HStack {
                Text(model.updated.map { "Updated \(Self.time($0))" } ?? "Not updated yet").font(.caption).foregroundColor(.secondary)
                Spacer()
            }
        }
        .padding(18)
        .nativeFormControls()
        .onChange(of: model.status?.profile) { _ in selectedScript = nil; selectedRule = nil }
        .sheet(item: $ruleSession) { session in
            RewriteRuleEditor(presentation: presentation, draft: session.draft, originalID: session.originalID, profile: session.profile)
        }
        .sheet(item: $scriptSession) { session in
            RewriteScriptEditor(presentation: presentation, draft: session.draft, originalID: session.originalID, profile: session.profile)
        }

    }
    static func time(_ date: Date) -> String {
        let formatter = DateFormatter(); formatter.dateFormat = "yyyy-MM-dd HH:mm"; return formatter.string(from: date)
    }
    private var statusPage: some View {
        VStack(alignment: .leading, spacing: 14) {
            ScrollView {
                if let status = model.status {
                    VStack(alignment: .leading, spacing: 16) {
                        GroupBox("Runtime") {
                            VStack(spacing: 10) {
                                RewriteDetailRow("State", value: status.actual.capitalized)
                                RewriteDetailRow("Saved enable setting", value: RewriteStatus.bool(status.intent))
                                RewriteDetailRow("TUN running", value: RewriteStatus.bool(status.tunRunning))
                                RewriteDetailRow("Rewrite service running", value: RewriteStatus.bool(status.value["service_running"]?.bool))
                                RewriteDetailRow("Certificate", value: status.value["ca"]?["state"]?.string ?? "unknown")
                                RewriteDetailRow("Trust", value: status.value["ca"]?["trust"]?.string ?? "unknown")
                            }.padding(8)
                        }
                        if let error = status.value["last_error"]?.string, !error.isEmpty {
                            Text(error).foregroundColor(.red).textSelection(.enabled)
                        }
                        if let recovery = status.value["recovery"]?.string ?? status.value["recovery_command"]?.string, !recovery.isEmpty {
                            Text(recovery).textSelection(.enabled)
                        }
                        DisclosureGroup("Runtime Details") {
                            VStack(spacing: 10) {
                                RewriteDetailRow("Schema", value: status.value["config_version"]?.int64.map(String.init) ?? "unknown")
                                RewriteDetailRow("Backend", value: status.value["backend"]?.string ?? "unknown")
                                RewriteDetailRow("Protocol", value: status.value["protocol_version"]?.int64.map(String.init) ?? "unknown")
                                RewriteDetailRow("Configured version", value: status.value["configured_rule_version"]?.string ?? "unknown")
                                RewriteDetailRow("Active version", value: status.value["active_rule_version"]?.string ?? "unknown")
                                RewriteDetailRow("Generation", value: status.value["generation"]?.string ?? "unknown")
                                RewriteDetailRow("Engine epoch", value: status.value["engine_epoch"]?.string ?? "unknown")
                            }.padding(.top, 10)
                        }
                    }.padding(2)
                } else {
                    Text("Status unavailable. Refresh to try again.").foregroundColor(.secondary)
                }
            }
            HStack {
                Button("Validate Rules") { run(["validate"], profile: model.status?.profile) }
                Button("Import Rules…") { pickInput(command: "import") }
                Button("Export Rules…") { saveExport(certificate: false) }
            }.disabled(!model.canOperate)
        }.padding()
    }
    private var scriptsPage: some View {
        VStack(alignment: .leading) {
            List(selection: $selectedScript) {
                ForEach(model.scripts, id: \.id) { item in
                    HStack { Text(item.id).bold(); Spacer(); Text(item.bodyAccess == "buffer" ? "Buffered body" : "Metadata only").foregroundColor(.secondary) }.tag(item.id)
                }
            }
            .overlay { if model.scripts.isEmpty { Text(model.listsFresh ? "No scripts" : "Scripts unavailable").foregroundColor(.secondary).allowsHitTesting(false) } }
            HStack {
                Button("Add Script…") { if let profile = model.status?.profile { scriptSession = RewriteScriptSession(draft: RewriteScriptDraft(), originalID: nil, profile: profile) } }
                Button("Edit Script…") { editScript() }.disabled(selectedScript == nil || loadingEditor)
                Button("Remove…") {
                    guard let id = selectedScript, model.scripts.contains(where: { $0.id == id }), let profile = model.status?.profile,
                        confirm("Remove Script?", "Remove \(id)? Scripts referenced by rules cannot be removed.", action: "Remove Script") else { return }
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
                        Text("\(index + 1). \(item.id) · \(item.enabled ? "Enabled" : "Disabled")").bold()
                        Text("\(RewriteLabels.stage(item.stage)) · \(item.domain) · \(item.methods.isEmpty ? "Any method" : item.methods.joined(separator: ", ")) · \(item.path)")
                        Text(item.kinds.map(RewriteLabels.operation).joined(separator: " → ")).font(.caption).foregroundColor(.secondary)
                    }.padding(.vertical, 4).tag(item.id)
                }
            }
            .overlay { if model.rules.isEmpty { Text(model.listsFresh ? "No rules" : "Rules unavailable").foregroundColor(.secondary).allowsHitTesting(false) } }
            HStack {
                Button("Add Rule…") { if let profile = model.status?.profile { ruleSession = RewriteEditorSession(draft: RewriteRuleDraft(), originalID: nil, profile: profile) } }
                Button("Edit Rule…") { editRule() }.disabled(selectedRule == nil || loadingEditor)
                Button(selectedSummary?.enabled == true ? "Disable Rule" : "Enable Rule") {
                    guard let item = selectedSummary else { return }
                    run(["rules", item.enabled ? "disable" : "enable", item.id], profile: model.status?.profile)
                }.disabled(selectedSummary == nil)
                Button("Remove…") {
                    guard let item = selectedSummary, confirm("Remove Rule?", "Remove \(item.id) from this profile.", action: "Remove Rule") else { return }
                    run(["rules", "remove", item.id], profile: model.status?.profile)
                }.disabled(selectedSummary == nil)
                Spacer()
                Button { moveRule(-1) } label: { Image(systemName: "arrow.up") }
                    .disabled(!canMove(-1)).help("Move rule up").accessibilityLabel("Move rule up")
                Button { moveRule(1) } label: { Image(systemName: "arrow.down") }
                    .disabled(!canMove(1)).help("Move rule down").accessibilityLabel("Move rule down")
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
        VStack(alignment: .leading, spacing: 16) {
            GroupBox("Certificate") {
                VStack(spacing: 10) {
                    RewriteDetailRow("State", value: model.caStatus?["state"]?.string ?? "unknown")
                    RewriteDetailRow("Trust", value: model.caStatus?["trust"]?.string ?? "unknown")
                    RewriteDetailRow("Fingerprint", value: model.caStatus?["fingerprint"]?.string ?? "unknown")
                    RewriteDetailRow("Valid from", value: certificateTime(model.caStatus?["not_before"]?.string))
                    RewriteDetailRow("Valid until", value: certificateTime(model.caStatus?["not_after"]?.string))
                }.padding(8)
            }
            HStack {
                Button("Refresh CA Status") { run(["ca", "status"]) }
                Button("Generate Local CA") { run(["ca", "generate"]) }
                Button("Export Public Certificate…") { saveExport(certificate: true) }
            }
            HStack {
                Button("Install Login Keychain Trust…") {
                    if confirm("Trust the Nagi CA?", "Install this CA in your login keychain for request interception. macOS may request authorization. Applications with independent certificate stores need separate configuration.", action: "Install Trust") { run(["ca", "trust", "install"]) }
                }
                Button("Remove Login Keychain Trust…") {
                    if confirm("Remove Nagi CA Trust?", "Stop rewriting and remove login keychain trust. The enable setting and CA are retained. Re-enable rewriting after restoring trust.", action: "Remove Trust") { run(["ca", "trust", "remove"]) }
                }
            }
            Spacer()
        }.padding().disabled(!model.canOperate)
    }
    private func certificateTime(_ raw: String?) -> String {
        guard model.caStatus?["state"]?.string == "generated", let raw, !raw.hasPrefix("0001-"), let date = ISO8601DateFormatter().date(from: raw) else { return "unknown" }; return Self.time(date)
    }
    private func toggle() {
        if model.status?.intent == true { run(["disable"]); return }
        guard model.canEnable, confirm("Enable Request Rewrite?", "Intercept the domains selected by enabled rules using the current profile and trusted local CA. Without enabled rules, intent is saved as idle. Existing connections may be interrupted when rules activate.", action: "Enable Rewrite") else { return }
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
        panel.message = "Choose a private local JSON document (owner-only permissions). Import replaces the complete document; enabled must be false."
        if panel.runModal() == .OK, let file = panel.url { run([command, file.path], profile: profile) }
    }
    private func saveExport(certificate: Bool) {
        let panel = NSSavePanel(); panel.nameFieldStringValue = certificate ? "nagi-ca.pem" : "request-rewrite.json"
        panel.message = certificate ? "Export only the public certificate. The private key is excluded." : "Sensitive export may contain credentials and complete URL values. Keep this file private."
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
            Text(label).foregroundColor(.secondary).frame(width: 160, alignment: .trailing)
            Text(value).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

@MainActor private func confirm(_ title: String, _ detail: String, action: String) -> Bool {
    let alert = NSAlert(); alert.messageText = title; alert.informativeText = detail; alert.alertStyle = .warning
    alert.addButton(withTitle: "Cancel"); alert.addButton(withTitle: action)
    return alert.runModal() == .alertSecondButtonReturn
}
