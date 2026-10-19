import SwiftUI
import AppKit

@MainActor struct RewriteRuleEditor: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var presentation: RewritePresentation
    @State var draft: RewriteRuleDraft
    let originalID: String?
    let profile: String
    @State private var localError: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(originalID == nil ? "Create Rewrite Rule" : "Edit Sensitive Rule Values").font(.title2.bold())
            Text("Profile: \(profile). Drafts stay in memory until Submit; sensitive values travel through CLI stdin.").foregroundColor(.secondary)
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    HStack {
                        TextField("Rule ID", text: $draft.id).disabled(originalID != nil)
                        Toggle("Enabled rule", isOn: $draft.enabled)
                        Picker("Stage", selection: $draft.stage) { ForEach(RewriteRuleDraft.stages, id: \.self) { Text($0).tag($0) } }
                    }
                    matchEditor
                    GroupBox("Ordered operations") {
                        VStack(alignment: .leading, spacing: 12) {
                            ForEach(draft.operations.indices, id: \.self) { index in
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack {
                                        Text("Operation \(index + 1)").bold(); Spacer()
                                        Button("↑") { draft.operations.swapAt(index, index - 1) }.disabled(index == 0).accessibilityLabel("Move operation up")
                                        Button("↓") { draft.operations.swapAt(index, index + 1) }.disabled(index + 1 == draft.operations.count).accessibilityLabel("Move operation down")
                                        Button("Remove") { draft.operations.remove(at: index) }
                                    }
                                    RewriteOperationEditor(draft: $draft.operations[index], stage: draft.stage)
                                }.padding(8).background(Color.secondary.opacity(0.07)).cornerRadius(6)
                            }
                            Button("Add Operation") { draft.operations.append(RewriteOperationDraft()) }
                            Text("The CLI checks operations against the chosen stage and enforces body, trailer, framing and script limits. Values can contain credentials.").font(.caption).foregroundColor(.secondary)
                        }.padding(6)
                    }
                    consentEditor
                }
            }
            if let error = localError ?? presentation.model.error { Text(error).foregroundColor(.red).fixedSize(horizontal: false, vertical: true) }
            HStack {
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Refresh Current State") { presentation.model.refresh() }; Spacer()
                Button("Submit Rule") { submit() }.keyboardShortcut(.defaultAction)
            }
        }.padding(22).frame(width: 800, height: 740)
            .disabled(presentation.model.operationInProgress)
            .interactiveDismissDisabled(presentation.model.operationInProgress)
    }
    private var matchEditor: some View {
        GroupBox("Match original metadata · all supplied predicates apply") {
            VStack(alignment: .leading, spacing: 8) {
                TextField("Exact domain or explicit wildcard (wildcards exclude apex)", text: $draft.domain)
                TextField("HTTP methods; blank means any", text: $draft.methods)
                TextField("Escaped path prefix; blank means any", text: $draft.path)
                TextField("Path regular expression (Go/RE2)", text: $draft.pathRegex)
                RewritePredicateEditor(label: "Query", text: $draft.query)
                RewritePredicateEditor(label: "Request headers", text: $draft.headers)
                if draft.stage != "request" {
                    TextField("Original status codes, e.g. 200, 206", text: $draft.statusCodes)
                    TextField("Original content types, e.g. application/json", text: $draft.contentTypes)
                    RewritePredicateEditor(label: "Response / handshake headers", text: $draft.responseHeaders)
                }
                if draft.stage.hasPrefix("websocket_") { TextField("Message types: text, binary; blank means any", text: $draft.messageTypes) }
                Picker("Range body handling", selection: $draft.rangeMode) {
                    Text("Transform fragments (preserve length)").tag("fragment")
                    Text("Require full representation").tag("full")
                }
                Picker("On transformation error", selection: $draft.onError) {
                    Text("Block (default)").tag("block")
                    Text("Forward complete original message").tag("forward_original")
                }
                Text("Forward original discards uncommitted stage edits. The CLI applies protocol and failure-policy constraints.").font(.caption).foregroundColor(.secondary)
            }.padding(6)
        }
    }
    private var consentEditor: some View {
        GroupBox("Explicit destination consent · request stage") {
            VStack(alignment: .leading, spacing: 10) {
                Text("Scheme, host or port changes require explicit destination consent. Consent itself authorizes the replacement destination; it does not add an interception rule. Path/query-only edits need no destination entry.").font(.caption).foregroundColor(.secondary)
                ForEach(draft.consent.indices, id: \.self) { index in
                    VStack(alignment: .leading) {
                        HStack { Text("Destination \(index + 1)").bold(); Spacer(); Button("Remove") { draft.consent.remove(at: index) } }
                        TextField("Destination domain", text: $draft.consent[index].domain)
                        HStack {
                            TextField("Schemes, e.g. https", text: $draft.consent[index].schemes)
                            TextField("Explicit ports, e.g. 443", text: $draft.consent[index].ports)
                        }
                    }
                }
                Button("Add Destination Consent") { draft.consent.append(RewriteConsentDraft()) }.disabled(draft.stage != "request")
            }.padding(6)
        }
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
            Text("\(label) · JSON object of string arrays; blank disables predicate").font(.caption)
            TextEditor(text: $text).font(.system(.body, design: .monospaced)).frame(height: 55)
        }
    }
}

private struct RewriteOperationEditor: View {
    @Binding var draft: RewriteOperationDraft
    let stage: String
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Picker("Operation", selection: $draft.kind) { ForEach(RewriteOperationDraft.kinds, id: \.self) { Text($0).tag($0) } }
            if draft.kind.hasPrefix("header_") || draft.kind.hasPrefix("trailer_") { TextField("Header or trailer name", text: $draft.name) }
            if draft.kind == "url_set" {
                Picker("URL component", selection: $draft.component) { ForEach(["scheme", "host", "port", "path", "query"], id: \.self) { Text($0).tag($0) } }
            }
            if draft.kind.hasPrefix("json_") { TextField("JSON Pointer (RFC 6901)", text: $draft.pointer) }
            if ["json_set", "body_set"].contains(draft.kind) {
                Text(draft.kind == "json_set" ? "JSON value · exact numeric digits retained" : "Replacement body").font(.caption)
                TextEditor(text: $draft.value).font(.system(.body, design: .monospaced)).frame(minHeight: 70)
            } else if ["header_set", "header_append", "trailer_set", "url_set", "method_set", "status_set"].contains(draft.kind) {
                TextField(draft.kind == "status_set" ? "Status (200–599)" : "Value", text: $draft.value)
            }
            if draft.kind == "body_set" {
                Picker("Body encoding", selection: $draft.encoding) { Text("UTF-8").tag("text"); Text("Base64").tag("base64") }
            }
            if ["text_replace", "regex_replace"].contains(draft.kind) {
                TextField(draft.kind == "regex_replace" ? "Go/RE2 pattern" : "Literal text", text: $draft.pattern)
                TextField("Replacement", text: $draft.replacement)
                Toggle("Replace all occurrences", isOn: $draft.all)
            }
            if draft.kind == "script" { TextField("Saved script ID", text: $draft.script) }
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
            Text(originalID == nil ? "Create Transform Script" : "Edit Sensitive Script Source").font(.title2.bold())
            Text("Profile: \(profile). Source can contain credentials. Drafts remain in memory; Submit sends source through CLI stdin.").foregroundColor(.secondary)
            TextField("Script ID", text: $draft.id).disabled(originalID != nil)
            Picker("Body access", selection: $draft.bodyAccess) { Text("Metadata only").tag("none"); Text("Buffered body (up to 8 MiB)").tag("buffer") }
            Text("Define synchronous transform(input) and return {action: 'unchanged'}, {action: 'block'}, or {action: 'replace', message: {...}}. Execution is isolated with no network, files, clock or random; source limit 64 KiB.").font(.caption).foregroundColor(.secondary)
            TextEditor(text: $draft.source).font(.system(.body, design: .monospaced))
            if let error = localError ?? presentation.model.error { Text(error).foregroundColor(.red) }
            HStack {
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Refresh Current State") { presentation.model.refresh() }; Spacer()
                Button("Submit Script") {
                    do {
                        let bytes = try draft.json(); localError = nil
                        let args = originalID.map { ["scripts", "update", $0, "-"] } ?? ["scripts", "add", "-"]
                        Task { if await presentation.model.perform(args, input: bytes, profile: profile) { dismiss() } }
                    } catch { localError = error.localizedDescription }
                }.keyboardShortcut(.defaultAction)
            }
        }.padding(22).frame(width: 780, height: 620)
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
            Text("Explicit sensitive local preview. No DNS, network request or persistence. Output can contain credentials, URLs and bodies; keep it private.").foregroundColor(.secondary)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    Picker("Preview stage", selection: $draft.stage) { ForEach(RewritePreviewDraft.stages, id: \.self) { Text($0).tag($0) } }
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
            }.frame(maxHeight: 320)
            HStack {
                Button("Preview & Reveal Sensitive Result") {
                    do {
                        let data = try draft.data(); localError = nil
                        Task { _ = await presentation.model.perform(["preview", "-"], input: data, profile: presentation.model.status?.profile) }
                    } catch { localError = error.localizedDescription }
                }
                Button("Preview Fixture File & Reveal…") { pickFixture() }; Spacer()
                Button("Clear Result") { presentation.model.clearPreview() }
            }.disabled(!presentation.model.canOperate || presentation.model.status?.profile == nil)
            if let error = localError { Text(error).foregroundColor(.red) }
            ScrollView {
                Text(presentation.model.preview ?? "No preview requested. Input remains a local draft until you submit it.")
                    .font(.system(.body, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(maxHeight: .infinity).padding(8).background(Color.secondary.opacity(0.06))
        }.padding()
    }
    private var requestForm: some View {
        GroupBox("Original request") {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Picker("Scheme", selection: $draft.scheme) { Text("https").tag("https"); Text("http").tag("http") }.frame(width: 150)
                    TextField("Rule match host", text: $draft.host)
                    TextField("Port", text: $draft.port).frame(width: 70)
                    TextField("Method", text: $draft.method).frame(width: 90)
                }
                HStack { TextField("Escaped path", text: $draft.path); TextField("Raw query, without ?", text: $draft.query) }
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
                TextField("Status", text: $draft.responseStatus)
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
        panel.message = "Choose a private v2 preview envelope with stage and request, plus response/message when required. The CLI reads it without sending network requests."
        if panel.runModal() == .OK, let file = panel.url {
            Task { _ = await presentation.model.perform(["preview", file.path], profile: presentation.model.status?.profile) }
        }
    }
}
private struct RewritePreviewHeaderEditor: View {
    let label: String
    @Binding var text: String
    var body: some View { VStack(alignment: .leading) { Text("\(label) · Name: value per line").font(.caption); TextEditor(text: $text).font(.system(.body, design: .monospaced)).frame(height: 55) } }
}
private struct RewritePreviewBodyEditor: View {
    @Binding var bodyText: String
    @Binding var encoding: String
    var body: some View {
        VStack(alignment: .leading) {
            Picker("Body input", selection: $encoding) { Text("UTF-8").tag("utf8"); Text("Base64 binary").tag("base64") }
            TextEditor(text: $bodyText).font(.system(.body, design: .monospaced)).frame(height: 65)
        }
    }
}
