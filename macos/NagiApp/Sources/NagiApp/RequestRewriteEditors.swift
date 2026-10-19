import SwiftUI
import AppKit

@MainActor enum RewriteEditorLayout {
    static func size(width: CGFloat, height: CGFloat, in screen: NSRect? = nil) -> NSSize {
        guard let screen = screen ?? NSScreen.main?.visibleFrame else { return NSSize(width: width, height: height) }
        return NSSize(width: min(width, screen.width * 0.85), height: min(height, screen.height * 0.85))
    }
}

enum RewriteLabels {
    static func stage(_ value: String) -> String {
        switch value {
        case "request": return L10n.text("app.ui.request")
        case "response": return L10n.text("app.ui.response")
        case "exchange": return L10n.text("app.ui.request_response")
        case "websocket_client": return L10n.text("app.ui.websocket_client_server")
        case "websocket_server": return L10n.text("app.ui.websocket_server_client")
        default: return value
        }
    }
    static func operation(_ value: String) -> String {
        switch value {
        case "header_set": return L10n.text("app.ui.set_header")
        case "header_append": return L10n.text("app.ui.append_header")
        case "header_delete": return L10n.text("app.ui.delete_header")
        case "url_set": return L10n.text("app.ui.change_url")
        case "method_set": return L10n.text("app.ui.change_method")
        case "status_set": return L10n.text("app.ui.change_status")
        case "json_set": return L10n.text("app.ui.set_json_value")
        case "json_delete": return L10n.text("app.ui.delete_json_value")
        case "body_set": return L10n.text("app.ui.replace_body")
        case "text_replace": return L10n.text("app.ui.replace_text")
        case "regex_replace": return L10n.text("app.ui.replace_regular_expression")
        case "trailer_set": return L10n.text("app.ui.set_trailer")
        case "trailer_delete": return L10n.text("app.ui.delete_trailer")
        case "script": return L10n.text("app.ui.run_script")
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
            Text(verbatim: label).font(.caption).foregroundColor(.secondary)
            TextField(prompt, text: $text).accessibilityLabel(label)
        }
    }
}

private struct RewriteHelp: View {
    let text: String
    @State private var showing = false
    var body: some View {
        Button { showing.toggle() } label: { Image(systemName: "questionmark.circle") }
            .accessibilityLabel(L10n.text("app.ui.help"))
            .popover(isPresented: $showing) { Text(verbatim: text).padding().frame(width: 360, alignment: .leading) }
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
        let size = RewriteEditorLayout.size(width: 800, height: 740)
        VStack(alignment: .leading, spacing: 12) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    GroupBox(L10n.text("app.ui.rule")) {
                        VStack(alignment: .leading, spacing: 10) {
                            RewriteField(L10n.text("app.ui.rule_name"), text: $draft.id).disabled(originalID != nil)
                            HStack {
                                Picker(L10n.text("app.ui.apply_to"), selection: $draft.stage) { ForEach(RewriteRuleDraft.stages, id: \.self) { Text(verbatim: RewriteLabels.stage($0)).tag($0) } }
                                Toggle(L10n.text("app.ui.enabled"), isOn: $draft.enabled)
                            }
                        }.padding(6)
                    }
                    matchEditor
                    GroupBox(L10n.text("app.ui.operations")) {
                        VStack(alignment: .leading, spacing: 12) {
                            ForEach(draft.operations.indices, id: \.self) { index in
                                VStack(alignment: .leading, spacing: 6) {
                                    HStack {
                                        Text(verbatim: L10n.format("app.ui.operation", String(describing: index + 1))).bold(); Spacer()
                                        Button { draft.operations.swapAt(index, index - 1) } label: { Image(systemName: "arrow.up") }
                                            .disabled(index == 0).help(L10n.text("app.ui.move_operation_up")).accessibilityLabel(L10n.text("app.ui.move_operation_up"))
                                        Button { draft.operations.swapAt(index, index + 1) } label: { Image(systemName: "arrow.down") }
                                            .disabled(index + 1 == draft.operations.count).help(L10n.text("app.ui.move_operation_down")).accessibilityLabel(L10n.text("app.ui.move_operation_down"))
                                        Button(L10n.text("app.ui.remove")) { draft.operations.remove(at: index) }
                                    }
                                    RewriteOperationEditor(draft: $draft.operations[index], stage: draft.stage)
                                }.padding(8).background(Color.secondary.opacity(0.07)).cornerRadius(6)
                            }
                            Button(L10n.text("app.ui.add_operation")) { draft.operations.append(RewriteOperationDraft()) }
                        }.padding(6)
                    }
                    if draft.stage == "request" || !draft.consent.isEmpty { consentEditor }
                    optionsEditor
                }
            }
            if let error = localError ?? presentation.model.error { Text(verbatim: error).foregroundColor(.red).fixedSize(horizontal: false, vertical: true) }
            HStack {
                Button(L10n.text("app.ui.cancel")) { dismiss() }.keyboardShortcut(.cancelAction)
                RewriteHelp(text: L10n.text("app.ui.all_match_conditions_apply_to_the_original"))
                Spacer()
                Button(L10n.text("app.ui.save_rule")) { submit() }.keyboardShortcut(.defaultAction).help(L10n.format("app.ui.save_to_profile", String(describing: profile)))
            }
        }.padding(22).frame(width: size.width, height: size.height)
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
        GroupBox(L10n.text("app.ui.match_conditions")) {
            VStack(alignment: .leading, spacing: 8) {
                RewriteField(L10n.text("app.ui.domain"), text: $draft.domain, prompt: L10n.text("app.rewrite.domain_example"))
                HStack(alignment: .top) {
                    RewriteField(L10n.text("app.ui.methods"), text: $draft.methods, prompt: L10n.text("app.ui.any")).frame(width: 160)
                    RewriteField(L10n.text("app.ui.path_prefix"), text: $draft.path, prompt: L10n.text("app.ui.any"))
                }
                DisclosureGroup(L10n.text("app.ui.additional_conditions"), isExpanded: $showingConditions) {
                    VStack(alignment: .leading, spacing: 10) {
                        RewriteField(L10n.text("app.ui.path_expression"), text: $draft.pathRegex, prompt: L10n.text("app.ui.any")).help(L10n.text("app.ui.go_re2_expression_over_the_escaped_path"))
                        RewritePredicateEditor(label: L10n.text("app.ui.query_parameters"), text: $draft.query)
                        RewritePredicateEditor(label: L10n.text("app.ui.request_headers"), text: $draft.headers)
                        if draft.stage != "request" {
                            RewriteField(L10n.text("app.ui.status_codes"), text: $draft.statusCodes, prompt: L10n.text("app.ui.any"))
                            RewriteField(L10n.text("app.ui.content_types"), text: $draft.contentTypes, prompt: L10n.text("app.ui.any"))
                            RewritePredicateEditor(label: L10n.text("app.ui.response_handshake_headers"), text: $draft.responseHeaders)
                        }
                        if draft.stage.hasPrefix("websocket_") { RewriteField(L10n.text("app.ui.message_types"), text: $draft.messageTypes, prompt: "text, binary") }
                    }.padding(.top, 8)
                }
            }.padding(6)
        }
    }
    private var optionsEditor: some View {
        DisclosureGroup(L10n.text("app.ui.advanced_settings"), isExpanded: $showingOptions) {
            VStack(alignment: .leading, spacing: 10) {
                Picker(L10n.text("app.ui.partial_content"), selection: $draft.rangeMode) {
                    Text(verbatim: L10n.text("app.ui.preserve_fragment_length")).tag("fragment")
                    Text(verbatim: L10n.text("app.ui.use_full_representation")).tag("full")
                }
                Picker(L10n.text("app.ui.if_an_operation_fails"), selection: $draft.onError) {
                    Text(verbatim: L10n.text("app.ui.block")).tag("block")
                    Text(verbatim: L10n.text("app.ui.forward_original")).tag("forward_original")
                }.help(L10n.text("app.ui.forward_original_discards_uncommitted_changes_for_this"))
            }.padding(.top, 8)
        }.padding(6)
    }
    private var consentEditor: some View {
        DisclosureGroup(L10n.text("app.ui.allowed_destinations"), isExpanded: $showingDestinations) {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(draft.consent.indices, id: \.self) { index in
                    VStack(alignment: .leading) {
                        HStack { Text(verbatim: L10n.format("app.ui.destination", String(describing: index + 1))).bold(); Spacer(); Button(L10n.text("app.ui.remove")) { draft.consent.remove(at: index) } }
                        RewriteField(L10n.text("app.ui.domain"), text: $draft.consent[index].domain)
                        HStack {
                            RewriteField(L10n.text("app.ui.schemes"), text: $draft.consent[index].schemes, prompt: "https")
                            RewriteField(L10n.text("app.ui.ports"), text: $draft.consent[index].ports, prompt: "443")
                        }
                    }
                }
                Button(L10n.text("app.ui.add_destination")) { draft.consent.append(RewriteConsentDraft()) }.disabled(draft.stage != "request")
            }.padding(6)
        }.padding(6)
    }
    private func submit() {
        do {
            let bytes = try draft.json(); localError = nil
            let arguments = originalID.map { ["rules", "update", $0, "-"] } ?? ["rules", "add", "-"]
            Task {
                if await presentation.model.perform(arguments, input: bytes, profile: profile) { dismiss() }
                else {
                    localError = presentation.model.error
                    presentation.model.refresh()
                }
            }
        } catch { localError = error.localizedDescription }
    }
}

private struct RewritePredicateEditor: View {
    let label: String
    @Binding var text: String
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: label).font(.caption).foregroundColor(.secondary)
            TextEditor(text: $text).font(.system(.body, design: .monospaced)).frame(height: 55)
                .nativeTextSurface()
                .accessibilityLabel(label).help(L10n.text("app.ui.json_object_mapping_names_to_string_arrays"))
        }
    }
}

struct RewriteOperationEditor: View {
    @Binding var draft: RewriteOperationDraft
    let stage: String
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Picker(L10n.text("app.ui.operation_2"), selection: $draft.kind) { ForEach(RewriteOperationDraft.kinds, id: \.self) { Text(verbatim: RewriteLabels.operation($0)).tag($0) } }
            if draft.kind.hasPrefix("header_") || draft.kind.hasPrefix("trailer_") { RewriteField(draft.kind.hasPrefix("header_") ? L10n.text("app.ui.header_name") : L10n.text("app.ui.trailer_name"), text: $draft.name) }
            if draft.kind == "url_set" {
                Picker(L10n.text("app.ui.url_component"), selection: $draft.component) { ForEach(["scheme", "host", "port", "path", "query"], id: \.self) { Text(verbatim: L10n.protocolLabel($0)).tag($0) } }
            }
            if draft.kind.hasPrefix("json_") { RewriteField(L10n.text("app.ui.json_pointer"), text: $draft.pointer).help(L10n.text("app.ui.rfc_6901_path_to_the_json_value")) }
            if ["json_set", "body_set"].contains(draft.kind) {
                Text(verbatim: draft.kind == "json_set" ? L10n.text("app.ui.json_value") : L10n.text("app.ui.replacement_body")).font(.caption)
                TextEditor(text: $draft.value).font(.system(.body, design: .monospaced)).frame(minHeight: 70)
                    .nativeTextSurface()
                    .accessibilityLabel(draft.kind == "json_set" ? L10n.text("app.ui.json_value") : L10n.text("app.ui.replacement_body"))
            } else if ["header_set", "header_append", "trailer_set", "url_set", "method_set", "status_set"].contains(draft.kind) {
                RewriteField(draft.kind == "status_set" ? L10n.text("app.ui.status_200_599") : L10n.text("app.ui.value"), text: $draft.value)
            }
            if draft.kind == "body_set" {
                Picker(L10n.text("app.ui.body_encoding"), selection: $draft.encoding) { Text(verbatim: "UTF-8").tag("text"); Text(verbatim: "Base64").tag("base64") }
            }
            if ["text_replace", "regex_replace"].contains(draft.kind) {
                RewriteField(draft.kind == "regex_replace" ? L10n.text("app.ui.go_re2_pattern") : L10n.text("app.ui.find"), text: $draft.pattern)
                RewriteField(L10n.text("app.ui.replace_with"), text: $draft.replacement)
                Toggle(L10n.text("app.ui.replace_all_occurrences"), isOn: $draft.all)
            }
            if draft.kind == "script" { RewriteField(L10n.text("app.ui.script_name"), text: $draft.script) }
            if stage.hasPrefix("websocket_") && ["body_set", "json_set", "json_delete", "text_replace", "regex_replace", "script"].contains(draft.kind) {
                Picker(L10n.text("app.ui.message_type_after_operation"), selection: $draft.messageType) {
                    Text(verbatim: L10n.text("app.ui.preserve_type")).tag(""); Text(verbatim: L10n.text("app.ui.text")).tag("text"); Text(verbatim: L10n.text("app.ui.binary")).tag("binary")
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
        let size = RewriteEditorLayout.size(width: 780, height: 620)
        VStack(alignment: .leading, spacing: 12) {
            RewriteField(L10n.text("app.ui.script_name"), text: $draft.id).disabled(originalID != nil)
            Picker(L10n.text("app.ui.body_access"), selection: $draft.bodyAccess) { Text(verbatim: L10n.text("app.ui.metadata_only")).tag("none"); Text(verbatim: L10n.text("app.ui.buffered_body_up_to_8_mib")).tag("buffer") }
            Text(verbatim: "JavaScript").font(.caption).foregroundColor(.secondary)
            TextEditor(text: $draft.source).font(.system(.body, design: .monospaced))
                .nativeTextSurface()
                .accessibilityLabel(L10n.text("app.ui.javascript_source"))
            if let error = localError ?? presentation.model.error { Text(verbatim: error).foregroundColor(.red) }
            HStack {
                Button(L10n.text("app.ui.cancel")) { dismiss() }.keyboardShortcut(.cancelAction)
                RewriteHelp(text: L10n.text("app.ui.define_synchronous_transform_input_return_action_unchanged"))
                Spacer()
                Button(L10n.text("app.ui.save_script")) {
                    do {
                        let bytes = try draft.json(); localError = nil
                        let args = originalID.map { ["scripts", "update", $0, "-"] } ?? ["scripts", "add", "-"]
                        Task {
                            if await presentation.model.perform(args, input: bytes, profile: profile) { dismiss() }
                            else {
                                localError = presentation.model.error
                                presentation.model.refresh()
                            }
                        }
                    } catch { localError = error.localizedDescription }
                }.keyboardShortcut(.defaultAction).help(L10n.format("app.ui.save_to_profile", String(describing: profile)))
            }
        }.padding(22).frame(width: size.width, height: size.height)
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
            guard let separator = line.firstIndex(of: ":") else { throw RewriteFormError.invalid(L10n.text("app.ui.enter_headers_or_trailers_as_name_value")) }
            let name = String(line[..<separator]).trimmingCharacters(in: .whitespaces)
            let value = String(line[line.index(after: separator)...]).trimmingCharacters(in: .whitespaces)
            fields[name, default: []].append(.string(value))
        }
        return .object(fields.mapValues(RewriteJSON.array))
    }
    private func bodyJSON(_ text: String, encoding: String) throws -> RewriteJSON {
        guard text.utf8.count <= 12 * 1_048_576 else { throw RewriteFormError.invalid(L10n.text("app.ui.the_encoded_preview_body_exceeds_12_mib")) }
        let bytes: Data
        if encoding == "base64" {
            guard let decoded = Data(base64Encoded: text) else { throw RewriteFormError.invalid(L10n.text("app.ui.enter_valid_base64_body_data")) }; bytes = decoded
        } else { bytes = Data(text.utf8) }
        guard bytes.count <= 8 * 1_048_576 else { throw RewriteFormError.invalid(L10n.text("app.ui.the_decoded_preview_body_exceeds_8_mib")) }
        return .string(bytes.base64EncodedString())
    }
    func data() throws -> Data {
        guard let number = Int(port), (1...65535).contains(number) else { throw RewriteFormError.invalid(L10n.text("app.ui.enter_a_port_from_1_through_65535")) }
        var request: [String: RewriteJSON] = ["scheme": .string(scheme), "host": .string(host), "port": .scalar(String(number)), "method": .string(method),
            "path": .string(path), "query": .string(query), "headers": try headerJSON(headers), "body_base64": try bodyJSON(body, encoding: bodyEncoding)]
        if !trailers.isEmpty { request["trailers"] = try headerJSON(trailers) }
        var envelope: [String: RewriteJSON] = ["stage": .string(stage), "request": .object(request)]
        if stage != "request" {
            guard let status = Int(responseStatus), (100...599).contains(status) else { throw RewriteFormError.invalid(L10n.text("app.ui.enter_a_response_status_from_100_through")) }
            var response: [String: RewriteJSON] = ["status": .scalar(String(status)), "headers": try headerJSON(responseHeaders), "body_base64": try bodyJSON(responseBody, encoding: responseBodyEncoding)]
            if !responseTrailers.isEmpty { response["trailers"] = try headerJSON(responseTrailers) }
            envelope["response"] = .object(response)
        }
        if stage.hasPrefix("websocket_") {
            envelope["message"] = .object(["type": .string(messageType), "body_base64": try bodyJSON(messageBody, encoding: messageBodyEncoding)])
        }
        let input = RewriteJSON.object(envelope).data
        guard input.count <= 24 * 1_048_576 else { throw RewriteFormError.invalid(L10n.text("app.ui.the_preview_envelope_exceeds_24_mib")) }
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
                    Picker(L10n.text("app.ui.apply_to"), selection: $draft.stage) { ForEach(RewritePreviewDraft.stages, id: \.self) { Text(verbatim: RewriteLabels.stage($0)).tag($0) } }
                        .onChange(of: draft.stage) { stage in if stage.hasPrefix("websocket_") { draft.responseStatus = "101" } }
                    requestForm
                    if draft.stage != "request" { responseForm }
                    if draft.stage.hasPrefix("websocket_") {
                        GroupBox(L10n.text("app.ui.websocket_message")) {
                            VStack(alignment: .leading) {
                                Picker(L10n.text("app.ui.type"), selection: $draft.messageType) { Text(verbatim: L10n.text("app.ui.text")).tag("text"); Text(verbatim: L10n.text("app.ui.binary")).tag("binary") }
                                RewritePreviewBodyEditor(bodyText: $draft.messageBody, encoding: $draft.messageBodyEncoding)
                            }.padding(6)
                        }
                    }
                }
            }.frame(minHeight: 200, idealHeight: 320, maxHeight: 360)
            HStack {
                Button(L10n.text("app.ui.run_preview")) {
                    do {
                        let data = try draft.data(); localError = nil
                        Task { _ = await presentation.model.perform(["preview", "-"], input: data, profile: presentation.model.status?.profile) }
                    } catch { localError = error.localizedDescription }
                }.disabled(!presentation.model.canOperate || presentation.model.status?.profile == nil)
                Button(L10n.text("app.ui.preview_file")) { pickFixture() }
                    .disabled(!presentation.model.canOperate || presentation.model.status?.profile == nil)
                RewriteHelp(text: L10n.text("app.ui.preview_applies_saved_rules_to_the_original"))
                Spacer()
                Button(L10n.text("app.ui.clear_result")) { presentation.model.clearPreview() }.disabled(presentation.model.preview == nil)
            }
            if let error = localError { Text(verbatim: error).foregroundColor(.red) }
            ScrollView {
                Text(verbatim: presentation.model.preview ?? L10n.text("app.ui.no_preview_result"))
                    .font(.system(.body, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(maxHeight: .infinity).padding(8).background(Color.secondary.opacity(0.06))
        }.padding().nativeFormControls()
    }
    private var requestForm: some View {
        GroupBox(L10n.text("app.ui.original_request")) {
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .bottom) {
                    Picker(L10n.text("app.ui.scheme"), selection: $draft.scheme) { Text(verbatim: "https").tag("https"); Text(verbatim: "http").tag("http") }.frame(width: 150)
                    RewriteField(L10n.text("app.ui.host"), text: $draft.host)
                    RewriteField(L10n.text("app.ui.port"), text: $draft.port).frame(width: 70)
                    RewriteField(L10n.text("app.ui.method"), text: $draft.method).frame(width: 90)
                }
                HStack { RewriteField(L10n.text("app.ui.path"), text: $draft.path); RewriteField(L10n.text("app.ui.query"), text: $draft.query).help(L10n.text("app.ui.raw_query_without_the_leading_question_mark")) }
                HStack {
                    RewritePreviewHeaderEditor(label: L10n.text("app.ui.headers"), text: $draft.headers)
                    RewritePreviewHeaderEditor(label: L10n.text("app.ui.trailers"), text: $draft.trailers)
                }
                RewritePreviewBodyEditor(bodyText: $draft.body, encoding: $draft.bodyEncoding)
            }.padding(6)
        }
    }
    private var responseForm: some View {
        GroupBox(draft.stage.hasPrefix("websocket_") ? L10n.text("app.ui.original_handshake_response_status_101") : L10n.text("app.ui.original_response")) {
            VStack(alignment: .leading, spacing: 6) {
                RewriteField(L10n.text("app.ui.status"), text: $draft.responseStatus)
                HStack {
                    RewritePreviewHeaderEditor(label: L10n.text("app.ui.headers"), text: $draft.responseHeaders)
                    RewritePreviewHeaderEditor(label: L10n.text("app.ui.trailers"), text: $draft.responseTrailers)
                }
                RewritePreviewBodyEditor(bodyText: $draft.responseBody, encoding: $draft.responseBodyEncoding)
            }.padding(6)
        }
    }
    private func pickFixture() {
        let panel = NSOpenPanel(); panel.canChooseDirectories = false; panel.allowsMultipleSelection = false
        panel.message = L10n.text("app.ui.choose_a_private_preview_json_file")
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
            Text(verbatim: label).font(.caption).foregroundColor(.secondary)
            TextEditor(text: $text).font(.system(.body, design: .monospaced)).frame(height: 55)
                .nativeTextSurface()
                .accessibilityLabel(label).help(L10n.text("app.ui.one_name_value_pair_per_line"))
        }
    }
}
private struct RewritePreviewBodyEditor: View {
    @Binding var bodyText: String
    @Binding var encoding: String
    var body: some View {
        VStack(alignment: .leading) {
            Picker(L10n.text("app.ui.body_input"), selection: $encoding) { Text(verbatim: "UTF-8").tag("utf8"); Text(verbatim: L10n.text("app.ui.base64_binary")).tag("base64") }
            TextEditor(text: $bodyText).font(.system(.body, design: .monospaced)).frame(height: 65)
                .nativeTextSurface()
                .accessibilityLabel(L10n.text("app.ui.body"))
        }
    }
}
