import AppKit
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
    init(model: RequestRewriteModel) {
        self.model = model
        let window = ConnectionsWindow(contentRect: NSRect(x: 0, y: 0, width: 1080, height: 760),
            styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = "Nagi Request Rewrite"; window.minSize = NSSize(width: 850, height: 620)
        window.isReleasedWhenClosed = false; window.delegate = self
        window.onVisibilityChange = { [weak self] in self?.updateVisibility() }
        window.contentView = NSHostingView(rootView: RequestRewriteView(presentation: RewritePresentation(model)))
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

private struct RewriteDomainDraft: Identifiable {
    var id = ""; var pattern = ""; var originalID: String?; let profile: String
}
private struct RewriteEditorSession: Identifiable {
    let id = UUID(); var draft: RewriteRuleDraft; let originalID: String?; let profile: String
}

@MainActor struct RequestRewriteView: View {
    @ObservedObject var presentation: RewritePresentation
    @State private var selectedDomain: String?
    @State private var selectedRule: String?
    @State private var domainDraft: RewriteDomainDraft?
    @State private var ruleSession: RewriteEditorSession?
    @State private var localError: String?
    @State private var editTask: Task<Void, Never>?
    @State private var loadingEditor = false
    @State private var page = 0
    private var model: RequestRewriteModel { presentation.model }
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Request Rewrite").font(.title2.bold())
                Text((model.status?.profile).map { "Profile: \($0)" } ?? "Profile unknown").foregroundColor(.secondary)
                Spacer()
                if model.isReading || model.operationInProgress || loadingEditor { ProgressView().controlSize(.small) }
                Button("Refresh") { model.refresh() }.disabled(model.isReading || !model.canOperate)
                Button(model.status?.intent == true ? "Disable Rewrite" : "Enable Rewrite") { toggle() }
                    .disabled(model.status?.intent == true ? !model.canOperate : !model.canEnable)
            }
            Text("Default off. Rewriting requires an actually running TUN, a compatible engine, a generated CA and explicit trust. Ordinary HTTP/SOCKS listeners are outside this feature.")
                .font(.callout).foregroundColor(.secondary)
            if model.stale { Text("Status is stale or unavailable. Refresh before enabling. Saved intent does not prove activation.").foregroundColor(.orange) }
            TabView(selection: $page) {
                statusPage.tabItem { Text("Status & Recovery") }.tag(0)
                domainsPage.tabItem { Text("Authorized Domains") }.tag(1)
                rulesPage.tabItem { Text("Ordered Rules") }.tag(2)
                RewritePreviewView(presentation: presentation).tabItem { Text("Local Preview") }.tag(3)
                caPage.tabItem { Text("Certificate & Trust") }.tag(4)
            }
            if let message = localError ?? model.error { Text(message).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            if let notice = model.notice { Text(notice).foregroundColor(.secondary).textSelection(.enabled) }
            HStack {
                Text(model.updated.map { "Updated \(Self.time($0))" } ?? "Not updated yet").font(.caption).foregroundColor(.secondary)
                Spacer()
                Text("Closing this window leaves rewriting running.").font(.caption).foregroundColor(.secondary)
            }
        }
        .padding(18)
        .onChange(of: model.status?.profile) { _ in selectedDomain = nil; selectedRule = nil }
        .sheet(item: $ruleSession) { session in
            RewriteRuleEditor(presentation: presentation, draft: session.draft, originalID: session.originalID, profile: session.profile)
        }
        .sheet(isPresented: Binding(get: { domainDraft != nil }, set: { if !$0 { domainDraft = nil } })) {
            if let draft = domainDraft {
                RewriteDomainEditor(presentation: presentation, draft: draft) { domainDraft = nil }
            }
        }
    }
    static func time(_ date: Date) -> String {
        let formatter = DateFormatter(); formatter.dateFormat = "yyyy-MM-dd HH:mm"; return formatter.string(from: date)
    }
    private var statusPage: some View {
        VStack(alignment: .leading, spacing: 14) {
            ScrollView { Text(model.status?.detail ?? "Status unavailable. Install matching Nagi CLI and engine versions, then refresh.").textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
            Text("Certificate pinning, client certificates, ECH, unsupported protocols and applications with separate trust stores can limit interception. Local preview does not verify live coverage.").foregroundColor(.secondary)
            HStack {
                Button("Validate Stored Rules") { run(["validate"], profile: model.status?.profile) }
                Button("Disable Rewrite") { run(["disable"]) }
                Button("Import Rules…") { pickInput(command: "import") }
                Button("Export Sensitive Rules…") { saveExport(certificate: false) }
            }.disabled(!model.canOperate)
        }.padding()
    }
    private var domainsPage: some View {
        VStack(alignment: .leading) {
            Text("Authorize only domains you control or have permission to inspect. Exact domains match only that host. *.service.example matches subdomains and excludes service.example.").foregroundColor(.secondary)
            List(selection: $selectedDomain) {
                ForEach(model.domains, id: \.id) { item in HStack { Text(item.id).bold(); Text(item.pattern); Spacer(); Text(item.pattern.hasPrefix("*.") ? "Explicit wildcard · excludes apex" : "Exact domain").foregroundColor(.secondary) }.tag(item.id) }
            }
            HStack {
                Button("Add Domain…") { if let profile = model.status?.profile { domainDraft = RewriteDomainDraft(profile: profile) } }
                Button("Edit…") {
                    if let item = model.domains.first(where: { $0.id == selectedDomain }), let profile = model.status?.profile {
                        domainDraft = RewriteDomainDraft(id: item.id, pattern: item.pattern, originalID: item.id, profile: profile)
                    }
                }.disabled(selectedDomain == nil)
                Button("Remove…") {
                    guard let id = selectedDomain, model.domains.contains(where: { $0.id == id }), let profile = model.status?.profile,
                        confirm("Remove Domain Consent?", "Remove consent for \(id). The CLI rejects removal while rules still reference it.", action: "Remove Consent") else { return }
                    run(["domains", "remove", id], profile: profile)
                }.disabled(selectedDomain == nil)
            }.disabled(!model.canManage)
        }.padding()
    }
    private var rulesPage: some View {
        VStack(alignment: .leading) {
            Text("All matching enabled rules run in this order against the original request. Editing explicitly reads sensitive operation and destination-consent values into memory.").foregroundColor(.secondary)
            List(selection: $selectedRule) {
                ForEach(Array(model.rules.enumerated()), id: \.element.id) { index, item in
                    VStack(alignment: .leading, spacing: 3) {
                        Text("\(index + 1). \(item.id) · \(item.enabled ? "Enabled" : "Disabled")").bold()
                        Text("\(item.domain) · \(item.methods.joined(separator: ", ")) · \(item.path) · on error: \(item.onError)")
                        Text(item.kinds.joined(separator: " → ")).font(.caption).foregroundColor(.secondary)
                    }.tag(item.id)
                }
            }
            HStack {
                Button("Add Rule…") { if let profile = model.status?.profile { ruleSession = RewriteEditorSession(draft: RewriteRuleDraft(), originalID: nil, profile: profile) } }
                Button("Edit Sensitive Values…") { editRule() }.disabled(selectedRule == nil || loadingEditor)
                Button(selectedSummary?.enabled == true ? "Disable Rule" : "Enable Rule") {
                    guard let item = selectedSummary else { return }
                    run(["rules", item.enabled ? "disable" : "enable", item.id], profile: model.status?.profile)
                }.disabled(selectedSummary == nil)
                Button("Remove…") {
                    guard let item = selectedSummary, confirm("Remove Rule?", "Remove \(item.id) from this profile.", action: "Remove Rule") else { return }
                    run(["rules", "remove", item.id], profile: model.status?.profile)
                }.disabled(selectedSummary == nil)
                Spacer()
                Button("Move Up") { moveRule(-1) }.disabled(!canMove(-1))
                Button("Move Down") { moveRule(1) }.disabled(!canMove(1))
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
            Text("The per-user CA private key stays private. Generation, public export, trust installation and trust removal are separate explicit operations. Disabling rewrite retains the CA and trust; removing trust retains the key.")
            if let ca = model.caStatus {
                Text("CA state: \(ca["state"]?.string ?? "unknown")\nTrust: \(ca["trust"]?.string ?? "unknown")\nFingerprint: \(ca["fingerprint"]?.string ?? "unknown")\nValid from: \(certificateTime(ca["not_before"]?.string))\nValid until: \(certificateTime(ca["not_after"]?.string))").textSelection(.enabled)
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
                    if confirm("Remove Nagi CA Trust?", "The CLI first tears down rewriting, retains enabled intent and the CA private key, then removes login keychain trust. Re-enable explicitly after restoring trust.", action: "Remove Trust") { run(["ca", "trust", "remove"]) }
                }
            }
            Text("OS trust and active service state must be confirmed by the CLI. Unknown means unverified; neither a certificate file nor saved intent proves trust or interception.").foregroundColor(.secondary)
            Spacer()
        }.padding().disabled(!model.canOperate)
    }
    private func certificateTime(_ raw: String?) -> String {
        guard model.caStatus?["state"]?.string == "generated", let raw, !raw.hasPrefix("0001-"), let date = ISO8601DateFormatter().date(from: raw) else { return "unknown" }; return Self.time(date)
    }
    private func toggle() {
        if model.status?.intent == true { run(["disable"]); return }
        guard model.canEnable, confirm("Enable Request Rewrite?", "Intercept only explicitly authorized domains using the current profile's rules and trusted local CA. Existing connections may be interrupted when rules activate.", action: "Enable Rewrite") else { return }
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

@MainActor private func confirm(_ title: String, _ detail: String, action: String) -> Bool {
    let alert = NSAlert(); alert.messageText = title; alert.informativeText = detail; alert.alertStyle = .warning
    alert.addButton(withTitle: "Cancel"); alert.addButton(withTitle: action)
    return alert.runModal() == .alertSecondButtonReturn
}

@MainActor private struct RewriteDomainEditor: View {
    @ObservedObject var presentation: RewritePresentation
    @State var draft: RewriteDomainDraft
    let close: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(draft.originalID == nil ? "Authorize Domain" : "Edit Domain Consent").font(.title2)
            Text("Profile: \(draft.profile). Only authorize domains you may inspect. Wildcards exclude the apex.")
            TextField("Consent ID", text: $draft.id).disabled(draft.originalID != nil)
            TextField("api.service.example or *.service.example", text: $draft.pattern)
            if let error = presentation.model.error { Text(error).foregroundColor(.red) }
            HStack {
                Button("Cancel", action: close).keyboardShortcut(.cancelAction)
                Button("Refresh Current State") { presentation.model.refresh() }
                Spacer()
                Button("Submit Consent") {
                    let input = RewriteJSON.object(["id": .string(draft.id), "pattern": .string(draft.pattern)]).data
                    let args = draft.originalID.map { ["domains", "update", $0, "-"] } ?? ["domains", "add", "-"]
                    Task { if await presentation.model.perform(args, input: input, profile: draft.profile) { close() } }
                }.disabled(draft.id.isEmpty || draft.pattern.isEmpty).keyboardShortcut(.defaultAction)
            }
        }.padding(24).frame(width: 560).disabled(presentation.model.operationInProgress)
    }
}
