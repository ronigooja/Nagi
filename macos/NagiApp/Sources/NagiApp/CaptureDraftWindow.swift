import AppKit
import Combine
import SwiftUI

@MainActor final class CaptureDraftWindowController: NSWindowController, NSWindowDelegate {
    let model: CaptureDraftModel
    var onClose: (() -> Void)?
    private var titleObservation: AnyCancellable?
    static let layout = NativeWindowLayout(preferredSize: NSSize(width: 840, height: 740),
                                          minimumSize: NSSize(width: 760, height: 600))
    init(model: CaptureDraftModel) {
        self.model = model
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 840, height: 740), styleMask: [.titled, .closable, .resizable, .miniaturizable], backing: .buffered, defer: false)
        super.init(window: window)
        titleObservation = model.$profile.sink { [weak window] profile in
            window?.title = profile.map { L10n.format("app.ui.create_rule", String(describing: $0)) } ?? L10n.text("app.ui.create_rule_2")
        }
        window.isReleasedWhenClosed = false; window.delegate = self
        let content = NSHostingView(rootView: CaptureDraftView(model: model, close: { [weak self] in self?.close() }))
        content.sizingOptions = []
        window.contentView = content
        Self.layout.apply(to: window)
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
            if let error = model.error { Text(verbatim: error).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            if model.saved {
                Text(verbatim: L10n.format("app.ui.rule_saved_to", String(describing: model.profile ?? L10n.text("app.profile.fallback_name")))).foregroundColor(.secondary)
            } else if let notice = model.notice {
                Text(verbatim: notice).foregroundColor(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            Divider()
            actions
        }.padding(20)
            .nativeFormControls()
            .task { await model.prepare() }
            .onChange(of: needsDestinationConsent) { needed in
                if needed { showingDestinations = true }
            }
            .sheet(isPresented: $showingPreview) { CaptureDraftPreviewView(model: model) }
    }
    private var ruleForm: some View {
        GroupBox(L10n.text("app.ui.rule")) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 10) {
                GridRow {
                    Text(verbatim: L10n.text("app.ui.rule_name")).gridColumnAlignment(.trailing)
                    TextField(L10n.text("app.ui.name"), text: $model.rule.id).labelsHidden()
                        .accessibilityLabel(L10n.text("app.ui.rule_name")).help(L10n.text("app.ui.unique_rule_identifier_within_the_destination_profile"))
                }
                GridRow {
                    Text(verbatim: L10n.text("app.ui.apply_to"))
                    Picker(L10n.text("app.ui.apply_to"), selection: $model.rule.stage) {
                        ForEach(model.context.record.kind == "websocket" ? [model.context.record.stage ?? "websocket_client"] : ["request", "response"], id: \.self) {
                            Text(verbatim: stageName($0)).tag($0)
                        }
                    }.labelsHidden()
                }
                GridRow {
                    Text(verbatim: L10n.text("app.ui.rule_order"))
                    Picker(L10n.text("app.ui.rule_order"), selection: $model.position) {
                        ForEach(model.orderedRules.indices, id: \.self) { index in
                            Text(verbatim: L10n.format("app.ui.before", String(describing: model.orderedRules[index]))).tag(String(index))
                        }
                        Text(verbatim: L10n.text("app.ui.at_end")).tag(String(model.orderedRules.count))
                    }.labelsHidden()
                }
            }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
        }
    }
    private func stageName(_ stage: String) -> String {
        switch stage {
        case "request": return L10n.text("app.ui.request")
        case "response": return L10n.text("app.ui.response")
        case "websocket_client": return L10n.text("app.ui.websocket_client_server")
        case "websocket_server": return L10n.text("app.ui.websocket_server_client")
        default: return stage
        }
    }
    private var ruleOptions: some View {
        DisclosureGroup(L10n.text("app.ui.advanced_settings"), isExpanded: $showingRuleOptions) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 10) {
                GridRow {
                    Text(verbatim: L10n.text("app.ui.if_an_operation_fails")).gridColumnAlignment(.trailing)
                    Picker(L10n.text("app.ui.if_an_operation_fails"), selection: $model.rule.onError) {
                        Text(verbatim: L10n.text("app.ui.block")).tag("block"); Text(verbatim: L10n.text("app.ui.forward_original")).tag("forward_original")
                    }.labelsHidden()
                }
                GridRow {
                    Text(verbatim: L10n.text("app.ui.partial_content"))
                    Picker(L10n.text("app.ui.partial_content"), selection: $model.rule.rangeMode) {
                        Text(verbatim: L10n.text("app.ui.preserve_fragment_length")).tag("fragment"); Text(verbatim: L10n.text("app.ui.use_full_representation")).tag("full")
                    }.labelsHidden().help(L10n.text("app.ui.controls_how_rewrites_handle_http_range_responses"))
                }
            }.padding(.top, 10)
        }.padding(8)
    }
    private var matchForm: some View {
        GroupBox(L10n.text("app.ui.match_conditions")) {
            VStack(alignment: .leading, spacing: 12) {
                CaptureDraftField(L10n.text("app.ui.domain"), text: $model.rule.domain, prompt: "api.example.com")
                    .help(L10n.text("app.ui.a_domain_or_explicit_wildcard_such_as"))
                HStack(alignment: .top, spacing: 12) {
                    CaptureDraftField(L10n.text("app.ui.methods"), text: $model.rule.methods, prompt: L10n.text("app.ui.any")).frame(width: 160)
                        .help(L10n.text("app.ui.comma_separated_http_methods_leave_blank_to"))
                    CaptureDraftField(L10n.text("app.ui.path_prefix"), text: $model.rule.path, prompt: L10n.text("app.ui.any"))
                        .help(L10n.text("app.ui.matches_the_escaped_url_path_prefix"))
                }
                CaptureDraftField(L10n.text("app.ui.path_expression"), text: $model.rule.pathRegex, prompt: L10n.text("app.ui.any"))
                    .help(L10n.text("app.ui.go_re2_regular_expression_matched_against_the"))
                DisclosureGroup(isExpanded: $showingMatchOptions) {
                    VStack(alignment: .leading, spacing: 10) {
                        predicate(L10n.text("app.ui.query_parameters"), text: $model.rule.query)
                        predicate(L10n.text("app.ui.request_headers"), text: $model.rule.headers)
                        if model.rule.stage != "request" {
                            HStack(alignment: .top, spacing: 12) {
                                CaptureDraftField(L10n.text("app.ui.status_codes"), text: $model.rule.statusCodes, prompt: L10n.text("app.ui.any"))
                                    .help(L10n.text("app.ui.comma_separated_original_http_status_codes_from"))
                                CaptureDraftField(L10n.text("app.ui.content_types"), text: $model.rule.contentTypes, prompt: L10n.text("app.ui.any"))
                                    .help(L10n.text("app.ui.comma_separated_content_types"))
                            }
                            predicate(L10n.text("app.ui.response_handshake_headers"), text: $model.rule.responseHeaders)
                        }
                        if model.rule.stage.hasPrefix("websocket_") {
                            CaptureDraftField(L10n.text("app.ui.message_types"), text: $model.rule.messageTypes, prompt: L10n.text("app.ui.any"))
                                .help(L10n.text("app.ui.comma_separated_types_text_binary"))
                        }
                    }.padding(.top, 10)
                } label: {
                    HStack {
                        Text(verbatim: L10n.text("app.ui.more_conditions"))
                        if activeMatchOptions > 0 { Text(verbatim: L10n.format("app.ui.active", String(describing: activeMatchOptions))).foregroundColor(.secondary) }
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
            Text(verbatim: label).font(.caption).foregroundColor(.secondary)
            TextEditor(text: text).font(.system(.body, design: .monospaced)).frame(height: 64)
                .nativeTextSurface()
                .accessibilityLabel(label)
        }.help(L10n.text("app.ui.json_object_mapping_names_to_arrays_of"))
    }
    private var operationForm: some View {
        GroupBox(L10n.text("app.ui.operations")) {
            VStack(alignment: .leading, spacing: 12) {
                ForEach(model.rule.operations.indices, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 10) {
                        HStack {
                            Text(verbatim: "\(index + 1)").font(.caption.monospacedDigit()).foregroundColor(.secondary)
                            Spacer()
                            Button { model.rule.operations.swapAt(index, index - 1) } label: { Image(systemName: "arrow.up") }
                                .disabled(index == 0).accessibilityLabel(L10n.format("app.ui.move_capture_operation_up", String(describing: index + 1))).help(L10n.text("app.ui.move_up"))
                            Button { model.rule.operations.swapAt(index, index + 1) } label: { Image(systemName: "arrow.down") }
                                .disabled(index + 1 == model.rule.operations.count).accessibilityLabel(L10n.format("app.ui.move_capture_operation_down", String(describing: index + 1))).help(L10n.text("app.ui.move_down"))
                            Button(L10n.text("app.ui.remove")) { model.rule.operations.remove(at: index) }.accessibilityLabel(L10n.format("app.ui.remove_capture_operation", String(describing: index + 1)))
                        }
                        CaptureOperationEditor(draft: $model.rule.operations[index], stage: model.rule.stage)
                    }.padding(8).background(Color.secondary.opacity(0.06)).cornerRadius(6)
                }
                if model.rule.operations.isEmpty { Text(verbatim: L10n.text("app.ui.no_operations")).foregroundColor(.secondary) }
                Button(L10n.text("app.ui.add_operation")) { model.rule.operations.append(RewriteOperationDraft()) }.accessibilityLabel(L10n.text("app.ui.add_capture_rule_operation"))
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
                        CaptureDraftField(L10n.text("app.ui.domain"), text: $model.rule.consent[index].domain, prompt: "api.example.com")
                        CaptureDraftField(L10n.text("app.ui.schemes"), text: $model.rule.consent[index].schemes, prompt: "https").frame(width: 110)
                        CaptureDraftField(L10n.text("app.ui.ports"), text: $model.rule.consent[index].ports, prompt: "443").frame(width: 90)
                        Button(L10n.text("app.ui.remove")) { model.rule.consent.remove(at: index) }.accessibilityLabel(L10n.format("app.ui.remove_allowed_destination", String(describing: index + 1)))
                    }
                }
                Button(L10n.text("app.ui.add_allowed_destination")) { model.rule.consent.append(RewriteConsentDraft()) }.disabled(model.rule.stage != "request")
            }.padding(.top, 10)
        } label: {
            HStack {
                Text(verbatim: L10n.text("app.ui.allowed_destinations"))
                if !model.rule.consent.isEmpty { Text(verbatim: "\(model.rule.consent.count)").foregroundColor(.secondary) }
                else if needsDestinationConsent { Text(verbatim: L10n.text("app.ui.required")).foregroundColor(.secondary) }
            }
        }.padding(8).help(L10n.text("app.ui.changing_a_request_s_scheme_host_or"))
    }
    private var actions: some View {
        HStack(spacing: 12) {
            Button(model.saved ? L10n.text("app.ui.close") : L10n.text("app.ui.cancel")) { close() }.keyboardShortcut(.cancelAction).disabled(model.saving)
            Button { showingHelp.toggle() } label: { Image(systemName: "questionmark.circle") }
                .buttonStyle(.plain).accessibilityLabel(L10n.text("app.ui.help_with_capture_rules"))
                .popover(isPresented: $showingHelp) { helpContent }
            if model.working { ProgressView().controlSize(.small) }
            Spacer()
            if !model.saved {
                Toggle(L10n.text("app.ui.enable_on_save"), isOn: $model.enable).disabled(!model.ready)
                Button(L10n.text("app.ui.preview")) { showingPreview = true }.disabled(!model.ready || !model.canPreview)
                    .help(L10n.text("app.ui.preview_the_complete_rule_chain_locally_without"))
                Button(L10n.text("app.ui.save_rule")) { Task { await model.save() } }.keyboardShortcut(.defaultAction)
                    .disabled(!model.ready || model.rule.operations.isEmpty)
            }
        }
    }
    private var helpContent: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(verbatim: L10n.text("app.ui.capture_rules")).font(.headline)
            Text(verbatim: L10n.text("app.ui.the_profile_in_the_window_title_is"))
            Text(verbatim: L10n.text("app.ui.match_conditions_use_original_captured_metadata_more"))
            Text(verbatim: L10n.text("app.ui.preview_temporarily_enables_the_candidate_and_runs"))
        }.font(.callout).padding(16).frame(width: 380)
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
            Text(verbatim: label).font(.caption).foregroundColor(.secondary)
            TextField(prompt, text: $text).accessibilityLabel(label)
        }.frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct CaptureOperationEditor: View {
    @Binding var draft: RewriteOperationDraft
    let stage: String
    private let names = [
        "header_set": L10n.text("app.ui.set_header_2"), "header_append": L10n.text("app.ui.append_header_2"), "header_delete": L10n.text("app.ui.delete_header_2"),
        "url_set": L10n.text("app.ui.change_url"), "method_set": L10n.text("app.ui.change_method_2"), "status_set": L10n.text("app.ui.change_status_code"),
        "json_set": L10n.text("app.ui.set_json_value_2"), "json_delete": L10n.text("app.ui.delete_json_value_2"), "body_set": L10n.text("app.ui.replace_body_2"),
        "text_replace": L10n.text("app.ui.replace_text_2"), "regex_replace": L10n.text("app.ui.replace_with_regular_expression"),
        "trailer_set": L10n.text("app.ui.set_trailer_2"), "trailer_delete": L10n.text("app.ui.delete_trailer_2"), "script": L10n.text("app.ui.run_script_2")
    ]
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker(L10n.text("app.operation.action"), selection: $draft.kind) {
                ForEach(RewriteOperationDraft.kinds, id: \.self) { Text(verbatim: names[$0] ?? $0).tag($0) }
            }
            if draft.kind.hasPrefix("header_") || draft.kind.hasPrefix("trailer_") {
                CaptureDraftField(draft.kind.hasPrefix("header_") ? L10n.text("app.ui.header_name") : L10n.text("app.ui.trailer_name"), text: $draft.name)
            }
            if draft.kind == "url_set" {
                Picker(L10n.text("app.ui.url_component"), selection: $draft.component) {
                    ForEach(["scheme", "host", "port", "path", "query"], id: \.self) { Text(verbatim: L10n.protocolLabel($0)).tag($0) }
                }
            }
            if draft.kind.hasPrefix("json_") {
                CaptureDraftField("JSON pointer", text: $draft.pointer).help(L10n.text("app.ui.rfc_6901_pointer_such_as_items_0"))
            }
            if ["json_set", "body_set"].contains(draft.kind) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: draft.kind == "json_set" ? L10n.text("app.ui.json_value") : L10n.text("app.ui.replacement_body")).font(.caption).foregroundColor(.secondary)
                    TextEditor(text: $draft.value).font(.system(.body, design: .monospaced)).frame(height: 120)
                        .nativeTextSurface()
                        .accessibilityLabel(draft.kind == "json_set" ? L10n.text("app.ui.json_value") : L10n.text("app.ui.replacement_body"))
                }
            } else if ["header_set", "header_append", "trailer_set", "url_set", "method_set", "status_set"].contains(draft.kind) {
                CaptureDraftField(draft.kind == "status_set" ? L10n.text("app.ui.status_code") : L10n.text("app.ui.value"), text: $draft.value)
                    .help(draft.kind == "status_set" ? L10n.text("app.ui.http_status_code_from_200_through_599") : L10n.text("app.ui.replacement_value"))
            }
            if draft.kind == "body_set" {
                Picker(L10n.text("app.ui.body_encoding"), selection: $draft.encoding) { Text(verbatim: "UTF-8").tag("text"); Text(verbatim: "Base64").tag("base64") }
            }
            if ["text_replace", "regex_replace"].contains(draft.kind) {
                CaptureDraftField(draft.kind == "regex_replace" ? L10n.text("app.operation.regular_expression") : L10n.text("app.ui.find"), text: $draft.pattern)
                    .help(draft.kind == "regex_replace" ? L10n.text("app.ui.go_re2_regular_expression") : L10n.text("app.ui.literal_text_to_replace"))
                CaptureDraftField(L10n.text("app.ui.replace_with"), text: $draft.replacement)
                Toggle(L10n.text("app.ui.replace_all_occurrences"), isOn: $draft.all)
            }
            if draft.kind == "script" { CaptureDraftField(L10n.text("app.ui.saved_script"), text: $draft.script) }
            if stage.hasPrefix("websocket_") && ["body_set", "json_set", "json_delete", "text_replace", "regex_replace", "script"].contains(draft.kind) {
                Picker(L10n.text("app.ui.result_message_type"), selection: $draft.messageType) {
                    Text(verbatim: L10n.text("app.ui.preserve_type")).tag(""); Text(verbatim: L10n.text("app.ui.text")).tag("text"); Text(verbatim: L10n.text("app.ui.binary")).tag("binary")
                }
            }
        }
    }
}

@MainActor private struct CaptureDraftPreviewView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CaptureDraftModel
    private enum PreviewTab: CaseIterable {
        case comparison, diff, raw
        var title: String {
            switch self { case .comparison: return L10n.text("app.ui.before_after"); case .diff: return L10n.text("app.ui.diff"); case .raw: return L10n.text("app.ui.raw_result") }
        }
    }
    @State private var previewTab = PreviewTab.comparison
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker(L10n.text("app.ui.preview_display"), selection: $previewTab) {
                ForEach(PreviewTab.allCases, id: \.self) { Text(verbatim: $0.title).tag($0) }
            }.pickerStyle(.segmented)
            Group {
                if previewTab == .comparison {
                    HStack(alignment: .top, spacing: 12) {
                        previewText(L10n.text("app.ui.original"), model.beforeText)
                        Divider()
                        previewText(L10n.text("app.ui.result"), model.previewResult == nil ? L10n.text("app.ui.no_preview_result_2") : model.afterText)
                    }
                } else {
                    previewText(previewTab.title, resultText)
                }
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
            if let error = model.error { Text(verbatim: error).foregroundColor(.red).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            Divider()
            HStack {
                Text(verbatim: L10n.text("app.ui.offline_preview")).font(.caption).foregroundColor(.secondary)
                if model.working { ProgressView().controlSize(.small) }
                Spacer()
                Button(L10n.text("app.ui.update_preview")) { Task { await model.preview() } }.disabled(!model.ready || !model.canPreview)
                Button(L10n.text("app.ui.done")) { dismiss() }.keyboardShortcut(.cancelAction)
            }
        }.padding(20).frame(minWidth: 680, idealWidth: 800, minHeight: 460, idealHeight: 560)
            .nativeFormControls()
            .task { await model.preview() }
    }
    private var resultText: String {
        guard let result = model.previewResult else { return L10n.text("app.ui.no_preview_result_2") }
        return previewTab == .diff ? model.diffText : result.text
    }
    private func previewText(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: title).font(.caption.bold())
            ScrollView([.horizontal, .vertical]) {
                Text(verbatim: value).font(.system(.caption, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(maxWidth: .infinity, maxHeight: .infinity).accessibilityLabel(title + L10n.text("app.ui.capture_preview"))
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
