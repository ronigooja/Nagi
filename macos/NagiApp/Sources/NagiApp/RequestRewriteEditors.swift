import SwiftUI
import AppKit

enum RewriteLabels {
    static func stage(_ value: String) -> String {
        switch value {
        case "request": return "Request"
        case "response": return "Response"
        case "exchange": return "Request & Response"
        case "websocket_client": return "WebSocket · Client → Server"
        case "websocket_server": return "WebSocket · Server → Client"
        default: return value
        }
    }
    static func operation(_ value: String) -> String {
        switch value {
        case "header_set": return "Set header"
        case "header_append": return "Append header"
        case "header_delete": return "Delete header"
        case "url_set": return "Change URL"
        case "method_set": return "Change method"
        case "status_set": return "Change status"
        case "json_set": return "Set JSON value"
        case "json_delete": return "Delete JSON value"
        case "body_set": return "Replace body"
        case "text_replace": return "Replace text"
        case "regex_replace": return "Replace regular expression"
        case "trailer_set": return "Set trailer"
        case "trailer_delete": return "Delete trailer"
        case "script": return "Run script"
        default: return value
        }
    }
}

private struct RewriteField: View {
    let label: String
    @Binding var text: String
    var prompt = ""
    init(_ label: String, text: Binding<String>, prompt: String = "") {
        self.label = label; _text = text; self.prompt = prompt
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.caption).foregroundColor(.secondary)
            TextField(prompt, text: $text).accessibilityLabel(label)
        }
    }
}

private struct RewriteHelp: View {
    let text: String
    @State private var showing = false
    var body: some View {
        Button { showing.toggle() } label: { Image(systemName: "questionmark.circle") }
            .accessibilityLabel("Help")
            .popover(isPresented: $showing) { Text(text).padding().frame(width: 360, alignment: .leading) }
    }
}

@MainActor struct RewriteRuleEditor: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var presentation: RewritePresentation
    @State var draft: RewriteRuleDraft
    let originalID: String?
    let profile: String
    @State private var localError: String?
    @State private var showingConditions = false
    @State private var showingOptions = false
    @State private var showingDestinations = false
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    GroupBox("Rule") {
                        VStack(alignment: .leading, spacing: 10) {
                            RewriteField("Rule name", text: $draft.id).disabled(originalID != nil)
                            HStack {
                                Picker("Apply to", selection: $draft.stage) { ForEach(RewriteRuleDraft.stages, id: \.self) { Text(RewriteLabels.stage($0)).tag($0) } }
                                Toggle("Enabled", isOn: $draft.enabled)
                            }
                        }.padding(6)
                    }
                    matchEditor
                    GroupBox("Operations") {
                        VStack(alignment: .leading, spacing: 12) {
                            ForEach(draft.operations.indices, id: \.self) { index in
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack {
                                        Text("Operation \(index + 1)").bold(); Spacer()
                                        Button { draft.operations.swapAt(index, index - 1) } label: { Image(systemName: "arrow.up") }
                                            .disabled(index == 0).help("Move operation up").accessibilityLabel("Move operation up")
                                        Button { draft.operations.swapAt(index, index + 1) } label: { Image(systemName: "arrow.down") }
                                            .disabled(index + 1 == draft.operations.count).help("Move operation down").accessibilityLabel("Move operation down")
                                        Button("Remove") { draft.operations.remove(at: index) }
                                    }
                                    RewriteOperationEditor(draft: $draft.operations[index], stage: draft.stage)
                                }.padding(8).background(Color.secondary.opacity(0.07)).cornerRadius(6)
                            }
                            Button("Add Operation") { draft.operations.append(RewriteOperationDraft()) }
                        }.padding(6)
                    }
                    if draft.stage == "request" || !draft.consent.isEmpty { consentEditor }
                    optionsEditor
                }
            }
            if let error = localError ?? presentation.model.error { Text(error).foregroundColor(.red).fixedSize(horizontal: false, vertical: true) }
            HStack {
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Refresh") { presentation.model.refresh() }
                RewriteHelp(text: "All match conditions apply to the original message. A wildcard domain excludes its apex. Operations run in list order. Changing the URL scheme, host or port requires an allowed destination; path and query edits do not. Cancel discards the draft.")
                Spacer()
                Button("Save Rule") { submit() }.keyboardShortcut(.defaultAction).help("Save to profile \(profile)")
            }
        }.padding(22).frame(width: 800, height: 740)
            .nativeFormControls()
            .disabled(presentation.model.operationInProgress)
            .interactiveDismissDisabled(presentation.model.operationInProgress)
            .onAppear {
                showingConditions = [draft.pathRegex, draft.query, draft.headers, draft.statusCodes, draft.contentTypes, draft.responseHeaders, draft.messageTypes].contains { !$0.isEmpty }
                showingOptions = draft.rangeMode != "fragment" || draft.onError != "block"
                showingDestinations = !draft.consent.isEmpty || needsDestination
            }
            .onChange(of: needsDestination) { needed in if needed { showingDestinations = true } }
    }
    private var needsDestination: Bool {
        draft.stage == "request" && draft.operations.contains { $0.kind == "url_set" && ["scheme", "host", "port"].contains($0.component) }
    }
    private var matchEditor: some View {
        GroupBox("Match Conditions") {
            VStack(alignment: .leading, spacing: 8) {
                RewriteField("Domain", text: $draft.domain, prompt: "api.service.example or *.service.example")
                HStack(alignment: .top) {
                    RewriteField("Methods", text: $draft.methods, prompt: "Any").frame(width: 160)
                    RewriteField("Path prefix", text: $draft.path, prompt: "Any")
                }
                DisclosureGroup("Additional Conditions", isExpanded: $showingConditions) {
                    VStack(alignment: .leading, spacing: 10) {
                        RewriteField("Path expression", text: $draft.pathRegex, prompt: "Any").help("Go/RE2 expression over the escaped path.")
                        RewritePredicateEditor(label: "Query parameters", text: $draft.query)
                        RewritePredicateEditor(label: "Request headers", text: $draft.headers)
                        if draft.stage != "request" {
                            RewriteField("Status codes", text: $draft.statusCodes, prompt: "Any")
                            RewriteField("Content types", text: $draft.contentTypes, prompt: "Any")
                            RewritePredicateEditor(label: "Response / handshake headers", text: $draft.responseHeaders)
                        }
                        if draft.stage.hasPrefix("websocket_") { RewriteField("Message types", text: $draft.messageTypes, prompt: "text, binary") }
                    }.padding(.top, 8)
                }
            }.padding(6)
        }
    }
    private var optionsEditor: some View {
        DisclosureGroup("Advanced Settings", isExpanded: $showingOptions) {
            VStack(alignment: .leading, spacing: 10) {
                Picker("Partial content", selection: $draft.rangeMode) {
                    Text("Preserve fragment length").tag("fragment")
                    Text("Use full representation").tag("full")
                }
                Picker("If an operation fails", selection: $draft.onError) {
                    Text("Block").tag("block")
                    Text("Forward original").tag("forward_original")
                }.help("Forward original discards uncommitted changes for this stage.")
            }.padding(.top, 8)
        }.padding(6)
    }
    private var consentEditor: some View {
        DisclosureGroup("Allowed Destinations", isExpanded: $showingDestinations) {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(draft.consent.indices, id: \.self) { index in
                    VStack(alignment: .leading) {
                        HStack { Text("Destination \(index + 1)").bold(); Spacer(); Button("Remove") { draft.consent.remove(at: index) } }
                        RewriteField("Domain", text: $draft.consent[index].domain)
                        HStack {
                            RewriteField("Schemes", text: $draft.consent[index].schemes, prompt: "https")
                            RewriteField("Ports", text: $draft.consent[index].ports, prompt: "443")
                        }
                    }
                }
                Button("Add Destination") { draft.consent.append(RewriteConsentDraft()) }.disabled(draft.stage != "request")
            }.padding(6)
        }.padding(6)
    }
    private func submit() {
        do {
            let bytes = try draft.json(); localError = nil
            let arguments = originalID.map { ["rules", "update", $0, "-"] } ?? ["rules", "add", "-"]
            Task { if await presentation.model.perform(arguments, input: bytes, profile: profile) { dismiss() } }
        } catch { localError = error.localizedDescription }
    }
}

private struct RewritePredicateEditor: View {
    let label: String
    @Binding var text: String
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label).font(.caption).foregroundColor(.secondary)
            TextEditor(text: $text).font(.system(.body, design: .monospaced)).frame(height: 55)
                .nativeTextSurface()
                .accessibilityLabel(label).help("JSON object mapping names to string arrays, for example {\"X-Demo\":[\"example\"]}. Leave blank to match any value.")
        }
    }
}

struct RewriteOperationEditor: View {
    @Binding var draft: RewriteOperationDraft
    let stage: String
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Picker("Operation", selection: $draft.kind) { ForEach(RewriteOperationDraft.kinds, id: \.self) { Text(RewriteLabels.operation($0)).tag($0) } }
            if draft.kind.hasPrefix("header_") || draft.kind.hasPrefix("trailer_") { RewriteField(draft.kind.hasPrefix("header_") ? "Header name" : "Trailer name", text: $draft.name) }
            if draft.kind == "url_set" {
                Picker("URL component", selection: $draft.component) { ForEach(["scheme", "host", "port", "path", "query"], id: \.self) { Text($0).tag($0) } }
            }
            if draft.kind.hasPrefix("json_") { RewriteField("JSON Pointer", text: $draft.pointer).help("RFC 6901 path to the JSON value; leave blank to select the root.") }
            if ["json_set", "body_set"].contains(draft.kind) {
                Text(draft.kind == "json_set" ? "JSON value" : "Replacement body").font(.caption)
                TextEditor(text: $draft.value).font(.system(.body, design: .monospaced)).frame(minHeight: 70)
                    .nativeTextSurface()
                    .accessibilityLabel(draft.kind == "json_set" ? "JSON value" : "Replacement body")
            } else if ["header_set", "header_append", "trailer_set", "url_set", "method_set", "status_set"].contains(draft.kind) {
                RewriteField(draft.kind == "status_set" ? "Status (200–599)" : "Value", text: $draft.value)
            }
            if draft.kind == "body_set" {
                Picker("Body encoding", selection: $draft.encoding) { Text("UTF-8").tag("text"); Text("Base64").tag("base64") }
            }
            if ["text_replace", "regex_replace"].contains(draft.kind) {
                RewriteField(draft.kind == "regex_replace" ? "Go/RE2 pattern" : "Find", text: $draft.pattern)
                RewriteField("Replace with", text: $draft.replacement)
                Toggle("Replace all occurrences", isOn: $draft.all)
            }
            if draft.kind == "script" { RewriteField("Script name", text: $draft.script) }
            if stage.hasPrefix("websocket_") && ["body_set", "json_set", "json_delete", "text_replace", "regex_replace", "script"].contains(draft.kind) {
                Picker("Message type after operation", selection: $draft.messageType) {
                    Text("Preserve type").tag(""); Text("Text").tag("text"); Text("Binary").tag("binary")
                }
            }
        }
    }
}

@MainActor struct RewriteScriptEditor: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var presentation: RewritePresentation
    @State var draft: RewriteScriptDraft
    let originalID: String?
    let profile: String
    @State private var localError: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            RewriteField("Script name", text: $draft.id).disabled(originalID != nil)
            Picker("Body access", selection: $draft.bodyAccess) { Text("Metadata only").tag("none"); Text("Buffered body (up to 8 MiB)").tag("buffer") }
            Text("JavaScript").font(.caption).foregroundColor(.secondary)
            TextEditor(text: $draft.source).font(.system(.body, design: .monospaced))
                .nativeTextSurface()
                .accessibilityLabel("JavaScript source")
            if let error = localError ?? presentation.model.error { Text(error).foregroundColor(.red) }
            HStack {
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Refresh") { presentation.model.refresh() }
                RewriteHelp(text: "Define synchronous transform(input). Return {action: 'unchanged'}, {action: 'block'}, or {action: 'replace', message: {...}}. Scripts run only through a rule's Run script operation, without network or file access. Source limit: 64 KiB.")
                Spacer()
                Button("Save Script") {
                    do {
                        let bytes = try draft.json(); localError = nil
                        let args = originalID.map { ["scripts", "update", $0, "-"] } ?? ["scripts", "add", "-"]
                        Task { if await presentation.model.perform(args, input: bytes, profile: profile) { dismiss() } }
                    } catch { localError = error.localizedDescription }
                }.keyboardShortcut(.defaultAction).help("Save to profile \(profile)")
            }
        }.padding(22).frame(width: 780, height: 620)
            .nativeFormControls()
            .disabled(presentation.model.operationInProgress)
            .interactiveDismissDisabled(presentation.model.operationInProgress)
    }
}

struct RewritePreviewDraft {
    static let stages = ["request", "response", "exchange", "websocket_client", "websocket_server"]
    var stage = "request", scheme = "https", host = "", port = "443", method = "GET", path = "/", query = "", headers = "", trailers = "", body = "", bodyEncoding = "utf8"
    var responseStatus = "200", responseHeaders = "", responseTrailers = "", responseBody = "", responseBodyEncoding = "utf8"
    var messageType = "text", messageBody = "", messageBodyEncoding = "utf8"
    private func headerJSON(_ text: String) throws -> RewriteJSON {
        var fields: [String: [RewriteJSON]] = [:]
        for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
            guard let separator = line.firstIndex(of: ":") else { throw RewriteFormError.invalid("Enter headers or trailers as Name: value, one per line.") }
            let name = String(line[..<separator]).trimmingCharacters(in: .whitespaces)
            let value = String(line[line.index(after: separator)...]).trimmingCharacters(in: .whitespaces)
            fields[name, default: []].append(.string(value))
        }
        return .object(fields.mapValues(RewriteJSON.array))
    }
    private func bodyJSON(_ text: String, encoding: String) throws -> RewriteJSON {
        guard text.utf8.count <= 12 * 1_048_576 else { throw RewriteFormError.invalid("The encoded preview body exceeds 12 MiB.") }
        let bytes: Data
        if encoding == "base64" {
            guard let decoded = Data(base64Encoded: text) else { throw RewriteFormError.invalid("Enter valid base64 body data.") }; bytes = decoded
        } else { bytes = Data(text.utf8) }
        guard bytes.count <= 8 * 1_048_576 else { throw RewriteFormError.invalid("The decoded preview body exceeds 8 MiB.") }
        return .string(bytes.base64EncodedString())
    }
    func data() throws -> Data {
        guard let number = Int(port), (1...65535).contains(number) else { throw RewriteFormError.invalid("Enter a port from 1 through 65535.") }
        var request: [String: RewriteJSON] = ["scheme": .string(scheme), "host": .string(host), "port": .scalar(String(number)), "method": .string(method),
            "path": .string(path), "query": .string(query), "headers": try headerJSON(headers), "body_base64": try bodyJSON(body, encoding: bodyEncoding)]
        if !trailers.isEmpty { request["trailers"] = try headerJSON(trailers) }
        var envelope: [String: RewriteJSON] = ["stage": .string(stage), "request": .object(request)]
        if stage != "request" {
            guard let status = Int(responseStatus), (100...599).contains(status) else { throw RewriteFormError.invalid("Enter a response status from 100 through 599.") }
            var response: [String: RewriteJSON] = ["status": .scalar(String(status)), "headers": try headerJSON(responseHeaders), "body_base64": try bodyJSON(responseBody, encoding: responseBodyEncoding)]
            if !responseTrailers.isEmpty { response["trailers"] = try headerJSON(responseTrailers) }
            envelope["response"] = .object(response)
        }
        if stage.hasPrefix("websocket_") {
            envelope["message"] = .object(["type": .string(messageType), "body_base64": try bodyJSON(messageBody, encoding: messageBodyEncoding)])
        }
        let input = RewriteJSON.object(envelope).data
        guard input.count <= 24 * 1_048_576 else { throw RewriteFormError.invalid("The preview envelope exceeds 24 MiB.") }
        return input
    }
}

@MainActor struct RewritePreviewView: View {
    @ObservedObject var presentation: RewritePresentation
    @State private var draft = RewritePreviewDraft()
    @State private var localError: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    Picker("Apply to", selection: $draft.stage) { ForEach(RewritePreviewDraft.stages, id: \.self) { Text(RewriteLabels.stage($0)).tag($0) } }
                        .onChange(of: draft.stage) { stage in if stage.hasPrefix("websocket_") { draft.responseStatus = "101" } }
                    requestForm
                    if draft.stage != "request" { responseForm }
                    if draft.stage.hasPrefix("websocket_") {
                        GroupBox("WebSocket message") {
                            VStack(alignment: .leading) {
                                Picker("Type", selection: $draft.messageType) { Text("Text").tag("text"); Text("Binary").tag("binary") }
                                RewritePreviewBodyEditor(bodyText: $draft.messageBody, encoding: $draft.messageBodyEncoding)
                            }.padding(6)
                        }
                    }
                }
            }.frame(minHeight: 200, idealHeight: 320, maxHeight: 360)
            HStack {
                Button("Run Preview") {
                    do {
                        let data = try draft.data(); localError = nil
                        Task { _ = await presentation.model.perform(["preview", "-"], input: data, profile: presentation.model.status?.profile) }
                    } catch { localError = error.localizedDescription }
                }.disabled(!presentation.model.canOperate || presentation.model.status?.profile == nil)
                Button("Preview File…") { pickFixture() }
                    .disabled(!presentation.model.canOperate || presentation.model.status?.profile == nil)
                RewriteHelp(text: "Preview applies saved rules to the original request, response or WebSocket message locally. It sends no network request and saves no changes. Headers and trailers use one Name: value pair per line. Choose Base64 for binary bodies.")
                Spacer()
                Button("Clear Result") { presentation.model.clearPreview() }.disabled(presentation.model.preview == nil)
            }
            if let error = localError { Text(error).foregroundColor(.red) }
            ScrollView {
                Text(presentation.model.preview ?? "No preview result")
                    .font(.system(.body, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(maxHeight: .infinity).padding(8).background(Color.secondary.opacity(0.06))
        }.padding().nativeFormControls()
    }
    private var requestForm: some View {
        GroupBox("Original request") {
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .bottom) {
                    Picker("Scheme", selection: $draft.scheme) { Text("https").tag("https"); Text("http").tag("http") }.frame(width: 150)
                    RewriteField("Host", text: $draft.host)
                    RewriteField("Port", text: $draft.port).frame(width: 70)
                    RewriteField("Method", text: $draft.method).frame(width: 90)
                }
                HStack { RewriteField("Path", text: $draft.path); RewriteField("Query", text: $draft.query).help("Raw query without the leading question mark.") }
                HStack {
                    RewritePreviewHeaderEditor(label: "Headers", text: $draft.headers)
                    RewritePreviewHeaderEditor(label: "Trailers", text: $draft.trailers)
                }
                RewritePreviewBodyEditor(bodyText: $draft.body, encoding: $draft.bodyEncoding)
            }.padding(6)
        }
    }
    private var responseForm: some View {
        GroupBox(draft.stage.hasPrefix("websocket_") ? "Original handshake response (status 101)" : "Original response") {
            VStack(alignment: .leading, spacing: 6) {
                RewriteField("Status", text: $draft.responseStatus)
                HStack {
                    RewritePreviewHeaderEditor(label: "Headers", text: $draft.responseHeaders)
                    RewritePreviewHeaderEditor(label: "Trailers", text: $draft.responseTrailers)
                }
                RewritePreviewBodyEditor(bodyText: $draft.responseBody, encoding: $draft.responseBodyEncoding)
            }.padding(6)
        }
    }
    private func pickFixture() {
        let panel = NSOpenPanel(); panel.canChooseDirectories = false; panel.allowsMultipleSelection = false
        panel.message = "Choose a private preview JSON file."
        if panel.runModal() == .OK, let file = panel.url {
            Task { _ = await presentation.model.perform(["preview", file.path], profile: presentation.model.status?.profile) }
        }
    }
}
private struct RewritePreviewHeaderEditor: View {
    let label: String
    @Binding var text: String
    var body: some View {
        VStack(alignment: .leading) {
            Text(label).font(.caption).foregroundColor(.secondary)
            TextEditor(text: $text).font(.system(.body, design: .monospaced)).frame(height: 55)
                .nativeTextSurface()
                .accessibilityLabel(label).help("One Name: value pair per line.")
        }
    }
}
private struct RewritePreviewBodyEditor: View {
    @Binding var bodyText: String
    @Binding var encoding: String
    var body: some View {
        VStack(alignment: .leading) {
            Picker("Body input", selection: $encoding) { Text("UTF-8").tag("utf8"); Text("Base64 binary").tag("base64") }
            TextEditor(text: $bodyText).font(.system(.body, design: .monospaced)).frame(height: 65)
                .nativeTextSurface()
                .accessibilityLabel("Body")
        }
    }
}
