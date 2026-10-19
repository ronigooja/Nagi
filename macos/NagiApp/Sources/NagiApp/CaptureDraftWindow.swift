import AppKit
import SwiftUI

@MainActor final class CaptureDraftWindowController: NSWindowController, NSWindowDelegate {
    let model: CaptureDraftModel
    var onClose: (() -> Void)?
    init(model: CaptureDraftModel) {
        self.model = model
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 820), styleMask: [.titled, .closable, .resizable, .miniaturizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = "Create Rule from Capture"
        window.minSize = NSSize(width: 760, height: 650)
        window.isReleasedWhenClosed = false; window.delegate = self
        window.contentView = NSHostingView(rootView: CaptureDraftView(model: model, close: { [weak self] in self?.close() }))
        window.center()
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    func present() { showWindow(nil); NSApp.activate(ignoringOtherApps: true); window?.makeKeyAndOrderFront(nil) }
    func windowShouldClose(_ sender: NSWindow) -> Bool { !model.saving }
    func windowWillClose(_ notification: Notification) { model.cancel(); onClose?() }
}

@MainActor struct CaptureDraftView: View {
    @ObservedObject var model: CaptureDraftModel
    let close: () -> Void
    @State private var previewTab = "Before / After"
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Create Rewrite Rule").font(.title2.bold())
            Text("Profile: \(model.profile ?? "Loading…") · original record \(model.context.record.id)").textSelection(.enabled)
            if let version = model.baseRuleVersion { Text("Base rule version: \(version)").font(.caption).textSelection(.enabled) }
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    ruleForm
                    matchForm
                    operationForm
                    consentForm
                    previewForm
                }.padding(.trailing, 8)
            }.disabled(model.working || model.saved || model.baseRuleVersion == nil)
            if let error = model.error { Text(error).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            if let notice = model.notice { Text(notice).foregroundColor(.secondary).fixedSize(horizontal: false, vertical: true) }
            HStack {
                Button(model.saved ? "Close" : "Cancel") { close() }.keyboardShortcut(.cancelAction).disabled(model.saving)
                if model.working { ProgressView().controlSize(.small) }
                Spacer()
                Button("Offline Preview") { Task { await model.preview() } }.disabled(!model.ready || !model.canPreview)
                    .accessibilityLabel("Preview captured original through the complete ordered rule chain")
                Button(model.enable ? "Save Enabled Rule" : "Save Disabled Rule") { Task { await model.save() } }
                    .disabled(!model.ready).accessibilityLabel("Save capture rule atomically at insertion position")
            }
        }.padding(20).frame(minWidth: 720, minHeight: 600)
            .task { await model.prepare() }
    }
    private var ruleForm: some View {
        GroupBox("Rule and insertion") {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    TextField("Rule ID", text: $model.rule.id).accessibilityLabel("Capture rule ID")
                    Picker("Stage", selection: $model.rule.stage) {
                        ForEach(model.context.record.kind == "websocket" ? [model.context.record.stage ?? "websocket_client"] : ["request", "response"], id: \.self) { Text($0).tag($0) }
                    }.accessibilityLabel("Capture rule stage")
                }
                HStack {
                    TextField("Insertion position (zero-based)", text: $model.position).accessibilityLabel("Zero-based rule insertion position")
                    Toggle("Enable this rule when saved", isOn: $model.enable).accessibilityLabel("Enable inserted rule")
                }
                Text("Global rewrite intent is unchanged. Preview temporarily enables the candidate without saving.").font(.caption).foregroundColor(.secondary)
                if !model.orderedRules.isEmpty {
                    Text(model.orderedRules.enumerated().map { "\($0.offset): \($0.element)" }.joined(separator: "\n")).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                }
                HStack {
                    Picker("On error", selection: $model.rule.onError) { Text("Block").tag("block"); Text("Forward original").tag("forward_original") }
                    Picker("Range handling", selection: $model.rule.rangeMode) { Text("Fragment (preserve length)").tag("fragment"); Text("Full representation").tag("full") }
                }
            }.padding(6)
        }
    }
    private var matchForm: some View {
        GroupBox("Original metadata match · CLI-generated defaults") {
            VStack(alignment: .leading, spacing: 7) {
                TextField("Domain or explicit wildcard", text: $model.rule.domain)
                HStack { TextField("Methods", text: $model.rule.methods); TextField("Escaped path prefix", text: $model.rule.path) }
                TextField("Exact path / Go RE2 expression", text: $model.rule.pathRegex)
                predicate("Query values", text: $model.rule.query)
                predicate("Request headers", text: $model.rule.headers)
                if model.rule.stage != "request" {
                    HStack { TextField("Original status codes", text: $model.rule.statusCodes); TextField("Content types", text: $model.rule.contentTypes) }
                    predicate("Response / handshake headers", text: $model.rule.responseHeaders)
                }
                if model.rule.stage.hasPrefix("websocket_") { TextField("Message types: text, binary", text: $model.rule.messageTypes) }
            }.padding(6)
        }
    }
    private func predicate(_ label: String, text: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label + " · JSON object of string arrays; blank means any").font(.caption)
            TextEditor(text: text).font(.system(.caption, design: .monospaced)).frame(height: 48).accessibilityLabel(label)
        }
    }
    private var operationForm: some View {
        GroupBox("Ordered operations") {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(model.rule.operations.indices, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text("Operation \(index + 1)").bold(); Spacer()
                            Button("↑") { model.rule.operations.swapAt(index, index - 1) }.disabled(index == 0).accessibilityLabel("Move capture operation \(index + 1) up")
                            Button("↓") { model.rule.operations.swapAt(index, index + 1) }.disabled(index + 1 == model.rule.operations.count).accessibilityLabel("Move capture operation \(index + 1) down")
                            Button("Remove") { model.rule.operations.remove(at: index) }.accessibilityLabel("Remove capture operation \(index + 1)")
                        }
                        RewriteOperationEditor(draft: $model.rule.operations[index], stage: model.rule.stage)
                    }.padding(8).background(Color.secondary.opacity(0.06)).cornerRadius(6)
                }
                Button("Add Operation") { model.rule.operations.append(RewriteOperationDraft()) }.accessibilityLabel("Add capture rule operation")
                Text("Header set / append / delete, trailer set / delete, URL components, method, status, bodies, text / regex, JSON pointers and saved scripts use the existing CLI validators.").font(.caption).foregroundColor(.secondary)
            }.padding(6)
        }
    }
    private var consentForm: some View {
        GroupBox("Explicit destination consent") {
            VStack(alignment: .leading, spacing: 6) {
                Text("Request scheme, host or port changes require explicit consent for the replacement destination.").font(.caption).foregroundColor(.secondary)
                ForEach(model.rule.consent.indices, id: \.self) { index in
                    HStack {
                        TextField("Domain", text: $model.rule.consent[index].domain)
                        TextField("Schemes", text: $model.rule.consent[index].schemes)
                        TextField("Ports", text: $model.rule.consent[index].ports)
                        Button("Remove") { model.rule.consent.remove(at: index) }
                    }
                }
                Button("Add Destination Consent") { model.rule.consent.append(RewriteConsentDraft()) }.disabled(model.rule.stage != "request")
            }.padding(6)
        }
    }
    private var previewForm: some View {
        GroupBox("Offline original-content preview · no network request") {
            VStack(alignment: .leading, spacing: 8) {
                Text("Original content and credentials are displayed directly. The CLI runs the complete ordered chain with this insertion.").font(.caption).foregroundColor(.secondary)
                Picker("Preview display", selection: $previewTab) { ForEach(["Before / After", "Diff", "Raw Result"], id: \.self) { Text($0).tag($0) } }.pickerStyle(.segmented)
                if previewTab == "Before / After" {
                    HStack(alignment: .top) { previewText("Original", model.beforeText); previewText("After ordered chain", model.afterText) }
                } else { previewText(previewTab, previewTab == "Diff" ? model.diffText : (model.previewResult?.text ?? "No preview result.")) }
                if !model.canPreview { Text("Complete original content unavailable. Metadata operations may still be saved.").foregroundColor(.secondary) }
            }.padding(6)
        }
    }
    private func previewText(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption.bold())
            ScrollView([.horizontal, .vertical]) { Text(value).font(.system(.caption, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
                .frame(minHeight: 180, maxHeight: 260).accessibilityLabel(title + " capture preview")
        }.frame(maxWidth: .infinity)
    }
}
