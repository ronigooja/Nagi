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
            Text(originalID == nil ? "Create Request Rule" : "Edit Sensitive Rule Values").font(.title2.bold())
            Text("Profile: \(profile). Draft changes stay in memory until Submit. Secret values are sent through CLI stdin.").foregroundColor(.secondary)
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    HStack {
                        TextField("Rule ID", text: $draft.id).disabled(originalID != nil)
                        Toggle("Enabled rule", isOn: $draft.enabled)
                    }
                    GroupBox("Match the original request") {
                        VStack(alignment: .leading) {
                            TextField("Authorized exact domain or explicit wildcard", text: $draft.domain)
                            TextField("HTTP methods, e.g. GET, POST", text: $draft.methods)
                            TextField("Escaped path prefix, e.g. /v1/", text: $draft.path)
                            Picker("On transformation error", selection: $draft.onError) {
                                Text("Block (default)").tag("block")
                                Text("Forward complete original request").tag("forward_original")
                            }
                            Text("Forward original discards all uncommitted edits. It cannot bypass authorization, framing, trust or upstream failures.").font(.caption).foregroundColor(.secondary)
                        }.padding(6)
                    }
                    GroupBox("Ordered operations") {
                        VStack(alignment: .leading, spacing: 12) {
                            ForEach(draft.operations.indices, id: \.self) { index in
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack {
                                        Text("Operation \(index + 1)").bold()
                                        Picker("Type", selection: $draft.operations[index].kind) {
                                            ForEach(RewriteOperationDraft.kinds, id: \.self) { Text($0).tag($0) }
                                        }.labelsHidden()
                                        Button("↑") { draft.operations.swapAt(index, index - 1) }.disabled(index == 0).accessibilityLabel("Move operation up")
                                        Button("↓") { draft.operations.swapAt(index, index + 1) }.disabled(index + 1 == draft.operations.count).accessibilityLabel("Move operation down")
                                        Button("Remove") { draft.operations.remove(at: index) }
                                    }
                                    if draft.operations[index].kind.hasPrefix("header_") {
                                        TextField("Header name", text: $draft.operations[index].name)
                                    }
                                    if draft.operations[index].kind == "url_set" {
                                        Picker("URL component", selection: $draft.operations[index].component) {
                                            ForEach(["scheme", "host", "port", "path", "query"], id: \.self) { Text($0).tag($0) }
                                        }
                                    }
                                    if draft.operations[index].kind.hasPrefix("json_") {
                                        TextField("JSON Pointer (RFC 6901); empty means whole document for set", text: $draft.operations[index].pointer)
                                    }
                                    if draft.operations[index].kind == "json_set" {
                                        Text("JSON value (numbers retain their exact digits)").font(.caption)
                                        TextEditor(text: $draft.operations[index].value).font(.system(.body, design: .monospaced)).frame(minHeight: 70)
                                    } else if ["header_set", "url_set"].contains(draft.operations[index].kind) {
                                        TextField("Value", text: $draft.operations[index].value)
                                    }
                                }.padding(8).background(Color.secondary.opacity(0.07)).cornerRadius(6)
                            }
                            Button("Add Operation") { draft.operations.append(RewriteOperationDraft()) }
                            Text("Header values, URL queries, Authorization and Cookie may be sensitive. Host and framing headers are managed by the CLI. JSON operations require supported JSON content and are limited to 8 MiB.").font(.caption).foregroundColor(.secondary)
                        }.padding(6)
                    }
                    GroupBox("Explicit destination consent") {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("Any final scheme, host or port change requires explicit destination consent here AND independently authorized destination domains. Path/query-only edits need no destination entry.").font(.caption).foregroundColor(.secondary)
                            ForEach(draft.consent.indices, id: \.self) { index in
                                VStack(alignment: .leading) {
                                    HStack { Text("Destination \(index + 1)").bold(); Spacer(); Button("Remove") { draft.consent.remove(at: index) } }
                                    TextField("Authorized destination domain", text: $draft.consent[index].domain)
                                    HStack {
                                        TextField("Schemes, e.g. https", text: $draft.consent[index].schemes)
                                        TextField("Explicit ports, e.g. 443", text: $draft.consent[index].ports)
                                    }
                                }
                            }
                            Button("Add Destination Consent") { draft.consent.append(RewriteConsentDraft()) }
                        }.padding(6)
                    }
                }
            }
            if let error = localError ?? presentation.model.error { Text(error).foregroundColor(.red).fixedSize(horizontal: false, vertical: true) }
            HStack {
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Refresh Current State") { presentation.model.refresh() }
                Spacer()
                Button("Submit Rule") { submit() }.keyboardShortcut(.defaultAction)
            }
        }.padding(22).frame(width: 760, height: 700)
            .disabled(presentation.model.operationInProgress)
            .interactiveDismissDisabled(presentation.model.operationInProgress)
    }
    private func submit() {
        do {
            let bytes = try draft.json(); localError = nil
            let arguments = originalID.map { ["rules", "update", $0, "-"] } ?? ["rules", "add", "-"]
            Task { if await presentation.model.perform(arguments, input: bytes, profile: profile) { dismiss() } }
        } catch { localError = error.localizedDescription }
    }
}

struct RewritePreviewDraft {
    var scheme = "https", host = "", port = "443", method = "GET", path = "/", query = "", headers = "", body = ""
    func data() throws -> Data {
        guard let number = Int(port), (1...65535).contains(number) else { throw RewriteFormError.invalid("Enter a port from 1 through 65535.") }
        var fields: [String: [RewriteJSON]] = [:]
        for line in headers.split(separator: "\n", omittingEmptySubsequences: true) {
            guard let separator = line.firstIndex(of: ":") else { throw RewriteFormError.invalid("Enter headers as Name: value, one per line.") }
            let name = String(line[..<separator]).trimmingCharacters(in: .whitespaces)
            let value = String(line[line.index(after: separator)...]).trimmingCharacters(in: .whitespaces)
            fields[name, default: []].append(.string(value))
        }
        let bytes = Data(body.utf8)
        guard bytes.count <= 8 * 1_048_576 else { throw RewriteFormError.invalid("The preview body exceeds 8 MiB.") }
        let input = RewriteJSON.object(["scheme": .string(scheme), "host": .string(host), "port": .scalar(String(number)), "method": .string(method),
            "path": .string(path), "query": .string(query), "headers": .object(fields.mapValues(RewriteJSON.array)), "body_base64": .string(bytes.base64EncodedString())]).data
        guard input.count <= 12 * 1_048_576 else { throw RewriteFormError.invalid("The preview fixture exceeds 12 MiB.") }
        return input
    }
}

@MainActor struct RewritePreviewView: View {
    @ObservedObject var presentation: RewritePresentation
    @State private var draft = RewritePreviewDraft()
    @State private var localError: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Explicit sensitive local preview. No DNS, network request or persistence. Output can contain credentials, URLs and request bodies; keep it private.").foregroundColor(.secondary)
            HStack {
                Picker("Scheme", selection: $draft.scheme) { Text("https").tag("https"); Text("http").tag("http") }.frame(width: 150)
                TextField("Authorized host", text: $draft.host)
                TextField("Port", text: $draft.port).frame(width: 70)
                TextField("Method", text: $draft.method).frame(width: 90)
            }
            HStack { TextField("Escaped path", text: $draft.path); TextField("Raw query, without ?", text: $draft.query) }
            HStack(alignment: .top) {
                VStack(alignment: .leading) { Text("Headers · Name: value per line"); TextEditor(text: $draft.headers).font(.system(.body, design: .monospaced)) }
                VStack(alignment: .leading) { Text("UTF-8 body · use fixture file for binary input"); TextEditor(text: $draft.body).font(.system(.body, design: .monospaced)) }
            }.frame(minHeight: 90, maxHeight: 150)
            HStack {
                Button("Preview & Reveal Sensitive Result") {
                    do {
                        let data = try draft.data(); localError = nil
                        Task { _ = await presentation.model.perform(["preview", "-"], input: data, profile: presentation.model.status?.profile) }
                    } catch { localError = error.localizedDescription }
                }
                Button("Preview Fixture File & Reveal…") { pickFixture() }
                Spacer()
                Button("Clear Result") { presentation.model.clearPreview() }
            }.disabled(!presentation.model.canOperate || presentation.model.status?.profile == nil)
            if let error = localError { Text(error).foregroundColor(.red) }
            ScrollView {
                Text(presentation.model.preview ?? "No preview requested. Input remains a local draft until you submit it.")
                    .font(.system(.body, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(maxHeight: .infinity).padding(8).background(Color.secondary.opacity(0.06))
        }.padding()
    }
    private func pickFixture() {
        let panel = NSOpenPanel(); panel.canChooseDirectories = false; panel.allowsMultipleSelection = false
        panel.message = "Choose a private local request fixture. The CLI reads it; preview never sends a network request."
        if panel.runModal() == .OK, let file = panel.url {
            Task { _ = await presentation.model.perform(["preview", file.path], profile: presentation.model.status?.profile) }
        }
    }
}
