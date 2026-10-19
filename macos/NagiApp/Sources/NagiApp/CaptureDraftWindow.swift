import AppKit
import Combine
import SwiftUI

@MainActor final class CaptureDraftWindowController: NSWindowController, NSWindowDelegate {
    let model: CaptureDraftModel
    var onClose: (() -> Void)?
    private var titleObservation: AnyCancellable?
    init(model: CaptureDraftModel) {
        self.model = model
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 840, height: 740), styleMask: [.titled, .closable, .resizable, .miniaturizable], backing: .buffered, defer: false)
        super.init(window: window)
        titleObservation = model.$profile.sink { [weak window] profile in
            window?.title = profile.map { "Create Rule — \($0)" } ?? "Create Rule"
        }
        window.minSize = NSSize(width: 760, height: 600)
        window.isReleasedWhenClosed = false; window.delegate = self
        let content = NSHostingView(rootView: CaptureDraftView(model: model, close: { [weak self] in self?.close() }))
        content.sizingOptions = []
        window.contentView = content
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
    @State private var showingPreview = false
    @State private var showingHelp = false
    @State private var showingMatchOptions = false
    @State private var showingRuleOptions = false
    @State private var showingDestinations = false
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    ruleForm
                    matchForm
                    operationForm
                    if model.rule.stage == "request" || !model.rule.consent.isEmpty { consentForm }
                    ruleOptions
                }.padding(2)
            }.disabled(model.working || model.saved || model.baseRuleVersion == nil)
            if let error = model.error { Text(error).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            if model.saved {
                Text("Rule saved to \(model.profile ?? "profile").").foregroundColor(.secondary)
            } else if let notice = model.notice {
                Text(notice).foregroundColor(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            Divider()
            actions
        }.padding(20)
            .disclosureGroupStyle(CaptureDraftDisclosureStyle())
            .task { await model.prepare() }
            .onChange(of: needsDestinationConsent) { needed in
                if needed { showingDestinations = true }
            }
            .sheet(isPresented: $showingPreview) { CaptureDraftPreviewView(model: model) }
    }
    private var ruleForm: some View {
        GroupBox("Rule") {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 10) {
                GridRow {
                    Text("Rule name").gridColumnAlignment(.trailing)
                    TextField("Name", text: $model.rule.id).labelsHidden()
                        .accessibilityLabel("Rule name").help("Unique rule identifier within the destination profile.")
                }
                GridRow {
                    Text("Apply to")
                    Picker("Apply to", selection: $model.rule.stage) {
                        ForEach(model.context.record.kind == "websocket" ? [model.context.record.stage ?? "websocket_client"] : ["request", "response"], id: \.self) {
                            Text(stageName($0)).tag($0)
                        }
                    }.labelsHidden()
                }
                GridRow {
                    Text("Rule order")
                    Picker("Rule order", selection: $model.position) {
                        ForEach(model.orderedRules.indices, id: \.self) { index in
                            Text("Before \(model.orderedRules[index])").tag(String(index))
                        }
                        Text("At end").tag(String(model.orderedRules.count))
                    }.labelsHidden()
                }
            }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
        }
    }
    private func stageName(_ stage: String) -> String {
        switch stage {
        case "request": return "Request"
        case "response": return "Response"
        case "websocket_client": return "WebSocket · Client → Server"
        case "websocket_server": return "WebSocket · Server → Client"
        default: return stage
        }
    }
    private var ruleOptions: some View {
        DisclosureGroup("Advanced Settings", isExpanded: $showingRuleOptions) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 10) {
                GridRow {
                    Text("If an operation fails").gridColumnAlignment(.trailing)
                    Picker("If an operation fails", selection: $model.rule.onError) {
                        Text("Block").tag("block"); Text("Forward original").tag("forward_original")
                    }.labelsHidden()
                }
                GridRow {
                    Text("Partial content")
                    Picker("Partial content", selection: $model.rule.rangeMode) {
                        Text("Preserve fragment length").tag("fragment"); Text("Use full representation").tag("full")
                    }.labelsHidden().help("Controls how rewrites handle HTTP range responses.")
                }
            }.padding(.top, 10)
        }.padding(8)
    }
    private var matchForm: some View {
        GroupBox("Match Conditions") {
            VStack(alignment: .leading, spacing: 12) {
                CaptureDraftField("Domain", text: $model.rule.domain, prompt: "api.example.com")
                    .help("A domain or explicit wildcard, such as *.example.com.")
                HStack(alignment: .top, spacing: 12) {
                    CaptureDraftField("Methods", text: $model.rule.methods, prompt: "Any").frame(width: 160)
                        .help("Comma-separated HTTP methods. Leave blank to match any method.")
                    CaptureDraftField("Path prefix", text: $model.rule.path, prompt: "Any")
                        .help("Matches the escaped URL path prefix.")
                }
                CaptureDraftField("Path expression", text: $model.rule.pathRegex, prompt: "Any")
                    .help("Go RE2 regular expression matched against the escaped URL path. Combined with the path prefix when both are set.")
                DisclosureGroup(isExpanded: $showingMatchOptions) {
                    VStack(alignment: .leading, spacing: 10) {
                        predicate("Query parameters", text: $model.rule.query)
                        predicate("Request headers", text: $model.rule.headers)
                        if model.rule.stage != "request" {
                            HStack(alignment: .top, spacing: 12) {
                                CaptureDraftField("Status codes", text: $model.rule.statusCodes, prompt: "Any")
                                    .help("Comma-separated original HTTP status codes, from 100 through 599.")
                                CaptureDraftField("Content types", text: $model.rule.contentTypes, prompt: "Any")
                                    .help("Comma-separated content types.")
                            }
                            predicate("Response / handshake headers", text: $model.rule.responseHeaders)
                        }
                        if model.rule.stage.hasPrefix("websocket_") {
                            CaptureDraftField("Message types", text: $model.rule.messageTypes, prompt: "Any")
                                .help("Comma-separated types: text, binary.")
                        }
                    }.padding(.top, 10)
                } label: {
                    HStack {
                        Text("More Conditions")
                        if activeMatchOptions > 0 { Text("\(activeMatchOptions) active").foregroundColor(.secondary) }
                    }
                }
            }.padding(8)
        }
    }
    private var activeMatchOptions: Int {
        var values = [model.rule.query, model.rule.headers]
        if model.rule.stage != "request" { values += [model.rule.statusCodes, model.rule.contentTypes, model.rule.responseHeaders] }
        if model.rule.stage.hasPrefix("websocket_") { values.append(model.rule.messageTypes) }
        return values.filter { !["", "{}"].contains($0.trimmingCharacters(in: .whitespacesAndNewlines)) }.count
    }
    private func predicate(_ label: String, text: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.caption).foregroundColor(.secondary)
            TextEditor(text: text).font(.system(.body, design: .monospaced)).frame(height: 64)
                .overlay(RoundedRectangle(cornerRadius: 4).stroke(Color.secondary.opacity(0.25)))
                .accessibilityLabel(label)
        }.help("JSON object mapping names to arrays of values, for example {\"X-Example\":[\"value\"]}. Leave blank to match any value.")
    }
    private var operationForm: some View {
        GroupBox("Operations") {
            VStack(alignment: .leading, spacing: 12) {
                ForEach(model.rule.operations.indices, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 10) {
                        HStack {
                            Text("\(index + 1)").font(.caption.monospacedDigit()).foregroundColor(.secondary)
                            Spacer()
                            Button { model.rule.operations.swapAt(index, index - 1) } label: { Image(systemName: "arrow.up") }
                                .disabled(index == 0).accessibilityLabel("Move capture operation \(index + 1) up").help("Move up")
                            Button { model.rule.operations.swapAt(index, index + 1) } label: { Image(systemName: "arrow.down") }
                                .disabled(index + 1 == model.rule.operations.count).accessibilityLabel("Move capture operation \(index + 1) down").help("Move down")
                            Button("Remove") { model.rule.operations.remove(at: index) }.accessibilityLabel("Remove capture operation \(index + 1)")
                        }
                        CaptureOperationEditor(draft: $model.rule.operations[index], stage: model.rule.stage)
                    }.padding(8).background(Color.secondary.opacity(0.06)).cornerRadius(6)
                }
                if model.rule.operations.isEmpty { Text("No operations").foregroundColor(.secondary) }
                Button("Add Operation") { model.rule.operations.append(RewriteOperationDraft()) }.accessibilityLabel("Add capture rule operation")
            }.padding(8)
        }
    }
    private var needsDestinationConsent: Bool {
        model.rule.stage == "request" && model.rule.operations.contains {
            $0.kind == "url_set" && ["scheme", "host", "port"].contains($0.component)
        }
    }
    private var consentForm: some View {
        DisclosureGroup(isExpanded: $showingDestinations) {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(model.rule.consent.indices, id: \.self) { index in
                    HStack(alignment: .bottom, spacing: 10) {
                        CaptureDraftField("Domain", text: $model.rule.consent[index].domain, prompt: "api.example.com")
                        CaptureDraftField("Schemes", text: $model.rule.consent[index].schemes, prompt: "https").frame(width: 110)
                        CaptureDraftField("Ports", text: $model.rule.consent[index].ports, prompt: "443").frame(width: 90)
                        Button("Remove") { model.rule.consent.remove(at: index) }.accessibilityLabel("Remove allowed destination \(index + 1)")
                    }
                }
                Button("Add Allowed Destination") { model.rule.consent.append(RewriteConsentDraft()) }.disabled(model.rule.stage != "request")
            }.padding(.top, 10)
        } label: {
            HStack {
                Text("Allowed Destinations")
                if !model.rule.consent.isEmpty { Text("\(model.rule.consent.count)").foregroundColor(.secondary) }
                else if needsDestinationConsent { Text("Required").foregroundColor(.secondary) }
            }
        }.padding(8).help("Changing a request's scheme, host or port requires explicit authorization for the replacement destination.")
    }
    private var actions: some View {
        HStack(spacing: 12) {
            Button(model.saved ? "Close" : "Cancel") { close() }.keyboardShortcut(.cancelAction).disabled(model.saving)
            Button { showingHelp.toggle() } label: { Image(systemName: "questionmark.circle") }
                .buttonStyle(.plain).accessibilityLabel("Help with capture rules")
                .popover(isPresented: $showingHelp) { helpContent }
            if model.working { ProgressView().controlSize(.small) }
            Spacer()
            if !model.saved {
                Toggle("Enable on save", isOn: $model.enable).disabled(!model.ready)
                Button("Preview…") { showingPreview = true }.disabled(!model.ready || !model.canPreview)
                    .help("Preview the complete rule chain locally without saving or sending a network request.")
                Button("Save Rule") { Task { await model.save() } }.keyboardShortcut(.defaultAction)
                    .disabled(!model.ready || model.rule.operations.isEmpty)
            }
        }
    }
    private var helpContent: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Capture Rules").font(.headline)
            Text("The profile in the window title is the save destination. Saving adds this rule at the selected position. Enable on save controls this rule; global rewrite activation is unchanged.")
            Text("Match conditions use original captured metadata. More Conditions shows how many additional filters are active. Hover over fields for accepted formats.")
            Text("Preview temporarily enables the candidate and runs the complete ordered rule chain locally, without saving or sending a network request. Original content, credentials and bodies appear directly; keep them private.")
        }.font(.callout).padding(16).frame(width: 380)
    }
}

private struct CaptureDraftDisclosureStyle: DisclosureGroupStyle {
    func makeBody(configuration: Configuration) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                configuration.isExpanded.toggle()
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: configuration.isExpanded ? "chevron.down" : "chevron.right")
                        .font(.caption).foregroundColor(.secondary).frame(width: 12)
                        .accessibilityHidden(true)
                    configuration.label
                }
                .padding(.vertical, 4)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(configuration.isExpanded ? "Expanded" : "Collapsed")
            if configuration.isExpanded {
                configuration.content.padding(.leading, 18)
            }
        }
    }
}

private struct CaptureDraftField: View {
    let label: String
    @Binding var text: String
    var prompt: String
    init(_ label: String, text: Binding<String>, prompt: String = "") {
        self.label = label; _text = text; self.prompt = prompt
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.caption).foregroundColor(.secondary)
            TextField(prompt, text: $text).accessibilityLabel(label)
        }.frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct CaptureOperationEditor: View {
    @Binding var draft: RewriteOperationDraft
    let stage: String
    private let names = [
        "header_set": "Set Header", "header_append": "Append Header", "header_delete": "Delete Header",
        "url_set": "Change URL", "method_set": "Change Method", "status_set": "Change Status Code",
        "json_set": "Set JSON Value", "json_delete": "Delete JSON Value", "body_set": "Replace Body",
        "text_replace": "Replace Text", "regex_replace": "Replace with Regular Expression",
        "trailer_set": "Set Trailer", "trailer_delete": "Delete Trailer", "script": "Run Script"
    ]
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker("Action", selection: $draft.kind) {
                ForEach(RewriteOperationDraft.kinds, id: \.self) { Text(names[$0] ?? $0).tag($0) }
            }
            if draft.kind.hasPrefix("header_") || draft.kind.hasPrefix("trailer_") {
                CaptureDraftField(draft.kind.hasPrefix("header_") ? "Header name" : "Trailer name", text: $draft.name)
            }
            if draft.kind == "url_set" {
                Picker("URL component", selection: $draft.component) {
                    ForEach(["scheme", "host", "port", "path", "query"], id: \.self) { Text($0.capitalized).tag($0) }
                }
            }
            if draft.kind.hasPrefix("json_") {
                CaptureDraftField("JSON pointer", text: $draft.pointer).help("RFC 6901 pointer, such as /items/0/name. An empty pointer selects the root value.")
            }
            if ["json_set", "body_set"].contains(draft.kind) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(draft.kind == "json_set" ? "JSON value" : "Replacement body").font(.caption).foregroundColor(.secondary)
                    TextEditor(text: $draft.value).font(.system(.body, design: .monospaced)).frame(height: 120)
                        .accessibilityLabel(draft.kind == "json_set" ? "JSON value" : "Replacement body")
                }
            } else if ["header_set", "header_append", "trailer_set", "url_set", "method_set", "status_set"].contains(draft.kind) {
                CaptureDraftField(draft.kind == "status_set" ? "Status code" : "Value", text: $draft.value)
                    .help(draft.kind == "status_set" ? "HTTP status code from 200 through 599." : "Replacement value.")
            }
            if draft.kind == "body_set" {
                Picker("Body encoding", selection: $draft.encoding) { Text("UTF-8").tag("text"); Text("Base64").tag("base64") }
            }
            if ["text_replace", "regex_replace"].contains(draft.kind) {
                CaptureDraftField(draft.kind == "regex_replace" ? "Regular expression" : "Find", text: $draft.pattern)
                    .help(draft.kind == "regex_replace" ? "Go RE2 regular expression." : "Literal text to replace.")
                CaptureDraftField("Replace with", text: $draft.replacement)
                Toggle("Replace all occurrences", isOn: $draft.all)
            }
            if draft.kind == "script" { CaptureDraftField("Saved script", text: $draft.script) }
            if stage.hasPrefix("websocket_") && ["body_set", "json_set", "json_delete", "text_replace", "regex_replace", "script"].contains(draft.kind) {
                Picker("Result message type", selection: $draft.messageType) {
                    Text("Preserve type").tag(""); Text("Text").tag("text"); Text("Binary").tag("binary")
                }
            }
        }
    }
}

@MainActor private struct CaptureDraftPreviewView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CaptureDraftModel
    @State private var previewTab = "Before / After"
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Preview display", selection: $previewTab) {
                ForEach(["Before / After", "Diff", "Raw Result"], id: \.self) { Text($0).tag($0) }
            }.pickerStyle(.segmented)
            Group {
                if previewTab == "Before / After" {
                    HStack(alignment: .top, spacing: 12) {
                        previewText("Original", model.beforeText)
                        Divider()
                        previewText("Result", model.previewResult == nil ? "No preview result." : model.afterText)
                    }
                } else {
                    previewText(previewTab, resultText)
                }
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
            if let error = model.error { Text(error).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            Divider()
            HStack {
                Text("Offline Preview").font(.caption).foregroundColor(.secondary)
                if model.working { ProgressView().controlSize(.small) }
                Spacer()
                Button("Update Preview") { Task { await model.preview() } }.disabled(!model.ready || !model.canPreview)
                Button("Done") { dismiss() }.keyboardShortcut(.cancelAction)
            }
        }.padding(20).frame(minWidth: 680, idealWidth: 800, minHeight: 460, idealHeight: 560)
            .task { await model.preview() }
    }
    private var resultText: String {
        guard let result = model.previewResult else { return "No preview result." }
        return previewTab == "Diff" ? model.diffText : result.text
    }
    private func previewText(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption.bold())
            ScrollView([.horizontal, .vertical]) {
                Text(value).font(.system(.caption, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(maxWidth: .infinity, maxHeight: .infinity).accessibilityLabel(title + " capture preview")
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
