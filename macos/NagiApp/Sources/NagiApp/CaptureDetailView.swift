import AppKit
import SwiftUI

enum CaptureDetailSection: String, CaseIterable {
    case overview, headers, body
    var title: String {
        switch self { case .overview: return L10n.text("app.ui.overview"); case .headers: return L10n.text("app.ui.headers"); case .body: return L10n.text("app.ui.body") }
    }
}

@MainActor struct CaptureDetailView: View {
    @ObservedObject var presentation: CapturePresentation
    @Binding var section: CaptureDetailSection
    @State private var jsonRulePresented = false
    @State private var jsonPointer = ""
    private var model: CaptureModel { presentation.model }
    private var canCreateRule: Bool { model.onCreateRule != nil && !model.readingBody }
    var body: some View {
        Group {
            if let record = model.selectedRecord {
                detail(record)
            } else {
                VStack(spacing: 8) {
                    Image(systemName: "doc.text.magnifyingglass").font(.largeTitle)
                    Text(verbatim: L10n.text("app.ui.select_a_request")).font(.headline)
                }.foregroundColor(.secondary).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .nativeFormControls()
        .onChange(of: model.selectedID) { _ in jsonPointer = ""; jsonRulePresented = false }
    }
    private func detail(_ record: CaptureRecord) -> some View {
        let summary = CaptureRecordSummary(record: record, handshake: record.parentID.flatMap { id in model.records.first { $0.id == id } })
        return VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 8) {
                    Text(verbatim: summary.method).font(.system(.callout, design: .monospaced).weight(.semibold))
                    Text(verbatim: summary.host).font(.headline).lineLimit(1).truncationMode(.middle)
                    Spacer(minLength: 0)
                    CaptureHTTPStatusView(summary: summary)
                }
                Text(verbatim: record.kind == "websocket" ? summary.path : summary.url)
                    .font(.callout).foregroundColor(.secondary).lineLimit(2).truncationMode(.middle)
                    .help(summary.url).textSelection(.enabled)
                    .contextMenu {
                        Button(L10n.text("app.ui.copy_url")) { copy(summary.url) }.disabled(summary.url.isEmpty)
                        Button(L10n.text("app.ui.create_rule_from_url")) { model.createRule(field: CaptureFieldSelection(kind: "url", value: summary.url, stage: record.stage)) }
                            .disabled(!canCreateRule || summary.url.isEmpty)
                    }
            }
            Picker(L10n.text("app.ui.snapshot"), selection: Binding(get: { model.selectedSnapshotName }, set: { model.selectSnapshot($0) })) {
                ForEach(record.snapshots.keys.sorted(), id: \.self) {
                    Text(verbatim: L10n.protocolLabel($0)).tag($0)
                }
            }
            Picker(L10n.text("app.ui.details"), selection: $section) {
                ForEach(CaptureDetailSection.allCases, id: \.self) { Text(verbatim: $0.title).tag($0) }
            }.pickerStyle(.segmented).labelsHidden()
            Group {
                switch section {
                case .overview: overview(record, summary: summary)
                case .headers: headers(record)
                case .body: bodyContent(record)
                }
            }.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            Divider()
            HStack {
                if section == .body {
                    Button(L10n.text("app.ui.json_pointer_2")) { jsonRulePresented = true }
                        .disabled(!canCreateRule)
                        .popover(isPresented: $jsonRulePresented, arrowEdge: .bottom) { jsonRule(record) }
                }
                Spacer(minLength: 0)
                Button(L10n.text("app.ui.create_rule_3")) { model.createRule() }.disabled(!canCreateRule)
            }
        }
        .padding(12).frame(minWidth: 0, maxWidth: .infinity, maxHeight: .infinity)
    }
    private func overview(_ record: CaptureRecord, summary: CaptureRecordSummary) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                field("URL", value: summary.url, selection: CaptureFieldSelection(kind: "url", value: summary.url, stage: record.stage))
                if let snapshot = record.snapshots[model.selectedSnapshotName] {
                    if let method = snapshot.method {
                        field(L10n.text("app.ui.method"), value: method, selection: CaptureFieldSelection(kind: "method", value: method, stage: record.stage))
                    }
                    if let status = snapshot.status {
                        field(L10n.text("app.capture.http_status_label"), value: String(status), selection: CaptureFieldSelection(kind: "status", value: String(status), stage: record.stage))
                    }
                    if let query = snapshot.query, !query.isEmpty {
                        field(L10n.text("app.ui.query"), value: query, selection: CaptureFieldSelection(kind: "query", value: query, stage: record.stage))
                    }
                    field(L10n.text("app.ui.body_collection"), value: bodyDescription(snapshot))
                    if let type = snapshot.messageType { field(L10n.text("app.ui.message_type"), value: L10n.protocolLabel(type)) }
                }
                Divider()
                field(L10n.text("app.ui.process"), value: record.identity.process)
                field(L10n.text("app.ui.process_path"), value: record.identity.processPath)
                field(L10n.text("app.ui.source"), value: record.identity.source)
                field(L10n.text("app.ui.destination_2"), value: record.identity.destination)
                field(L10n.text("app.ui.started"), value: captureMinute(record.started))
                field(L10n.text("app.ui.duration"), value: summary.duration)
                field(L10n.text("app.ui.outcome"), value: record.outcome.isEmpty ? L10n.text("app.ui.pending") : L10n.protocolLabel(record.outcome))
                if let error = record.error, !error.isEmpty { field(L10n.text("app.ui.error"), value: error) }
                if let code = record.closeCode { field(L10n.text("app.ui.websocket_close_code"), value: String(code)) }
                field(L10n.text("app.ui.matched_rules"), value: record.ruleIDs.flatMap { $0.isEmpty ? nil : $0.joined(separator: ", ") } ?? L10n.text("app.ui.none"))
                field(L10n.text("app.ui.record_id"), value: record.id)
                relatedMessages(record)
            }.frame(minWidth: 0, maxWidth: .infinity, alignment: .leading).padding(.trailing, 8)
        }
    }
    private func field(_ title: String, value: String, selection: CaptureFieldSelection? = nil) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: title).font(.caption).foregroundColor(.secondary)
            Text(verbatim: value.isEmpty ? "—" : value)
                .font(.system(.callout, design: .monospaced))
                .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                .contextMenu {
                    Button(L10n.text("app.ui.copy")) { copy(value) }
                    if let selection {
                        Button(L10n.format("app.ui.create_rule_from", String(describing: title))) { model.createRule(field: selection) }.disabled(!canCreateRule || value.isEmpty)
                    }
                }
        }
    }
    @ViewBuilder private func relatedMessages(_ record: CaptureRecord) -> some View {
        if record.kind == "http" {
            let messages = model.records.filter { $0.parentID == record.id }.sorted {
                if $0.sequence == $1.sequence { return $0.id < $1.id }
                return ($0.sequence ?? 0) < ($1.sequence ?? 0)
            }
            if !messages.isEmpty {
                Divider()
                Text(verbatim: L10n.text("app.ui.websocket_messages")).font(.headline)
                ForEach(messages) { message in
                    Button(CaptureRecordSummary(record: message).path) { model.select(message.id) }
                }
            }
        } else if let parent = record.parentID {
            Divider()
            field("HTTP handshake", value: parent)
            Button(L10n.text("app.ui.show_http_handshake")) { model.select(parent) }
                .disabled(!model.records.contains { $0.id == parent })
        }
    }
    private func headers(_ record: CaptureRecord) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let snapshot = record.snapshots[model.selectedSnapshotName] {
                    if model.format == .diff, model.selectedSnapshotName.hasSuffix("_actual"),
                       let original = record.snapshots[model.selectedSnapshotName.replacingOccurrences(of: "_actual", with: "_original")] {
                        Text(verbatim: L10n.text("app.ui.original_headers")).font(.headline)
                        headerRows(original.headers, kind: "header", stage: record.stage, allowsRule: false)
                        Divider()
                        Text(verbatim: L10n.text("app.ui.actual_headers")).font(.headline)
                    } else { Text(verbatim: L10n.text("app.ui.headers")).font(.headline) }
                    headerRows(snapshot.headers, kind: "header", stage: record.stage)
                    if snapshot.trailers?.isEmpty == false {
                        Divider()
                        Text(verbatim: L10n.text("app.ui.trailers")).font(.headline)
                        headerRows(snapshot.trailers, kind: "trailer", stage: record.stage)
                    }
                } else { Text(verbatim: L10n.text("app.ui.no_snapshot_selected")).foregroundColor(.secondary) }
            }.frame(minWidth: 0, maxWidth: .infinity, alignment: .leading).padding(.trailing, 8)
        }
    }
    @ViewBuilder private func headerRows(_ fields: [String: [String]]?, kind: String, stage: String?, allowsRule: Bool = true) -> some View {
        if let fields, !fields.isEmpty {
            let keys = fields.keys.sorted()
            ForEach(keys, id: \.self) { key in
                let value = (fields[key] ?? []).joined(separator: "\n")
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: key).font(.system(.caption, design: .monospaced).weight(.semibold)).foregroundColor(.secondary)
                    Text(verbatim: value).font(.system(.callout, design: .monospaced))
                        .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                }
                .contextMenu {
                    Button(L10n.text("app.ui.copy_header")) { copy(key + ": " + value) }
                    Button(L10n.text("app.ui.copy_value")) { copy(value) }
                    if allowsRule {
                        Button(L10n.format("app.ui.create_rule_from", String(describing: key))) { model.createRule(field: CaptureFieldSelection(kind: kind, name: key, value: value, stage: stage)) }
                            .disabled(!canCreateRule)
                    }
                }
                if key != keys.last { Divider() }
            }
        } else { Text(verbatim: L10n.text("app.ui.no_headers")).font(.callout).foregroundColor(.secondary) }
    }
    private func bodyContent(_ record: CaptureRecord) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let snapshot = record.snapshots[model.selectedSnapshotName] {
                Text(verbatim: bodyDescription(snapshot)).font(.caption).foregroundColor(.secondary)
            }
            ViewThatFits(in: .horizontal) {
                HStack { bodyFormat; copyBody }
                VStack(alignment: .leading) { bodyFormat; copyBody }
            }
            CaptureBodyTextView(text: model.bodyText, createRule: { selected in
                model.createRule(field: CaptureFieldSelection(kind: "text", selectedText: selected, value: selected, stage: record.stage))
            })
            .frame(minHeight: 80, maxHeight: .infinity)
            .nativeTextSurface()
        }
    }
    private var bodyFormat: some View {
        Picker(L10n.text("app.ui.body_format"), selection: Binding(get: { model.format }, set: { model.setFormat($0) })) {
            ForEach(CaptureBodyFormat.allCases, id: \.self) { Text(verbatim: $0.title).tag($0) }
        }.labelsHidden().pickerStyle(.segmented)
    }
    private var copyBody: some View {
        Button(L10n.text("app.ui.copy_body")) {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setData(model.selectedBody, forType: NSPasteboard.PasteboardType("public.data"))
            if let text = String(data: model.selectedBody, encoding: .utf8) { NSPasteboard.general.setString(text, forType: .string) }
        }.disabled(model.readingBody)
    }
    private func jsonRule(_ record: CaptureRecord) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(verbatim: L10n.text("app.ui.json_pointer")).font(.headline)
            TextField("JSON pointer, e.g. /data/name", text: $jsonPointer)
            HStack {
                Button(L10n.text("app.ui.cancel")) { jsonRulePresented = false }
                Spacer()
                Button(L10n.text("app.ui.create_rule_3")) {
                    model.createRule(field: CaptureFieldSelection(kind: "json", pointer: jsonPointer, stage: record.stage))
                    jsonRulePresented = false
                }.disabled(jsonPointer.isEmpty || !canCreateRule)
            }
        }.padding(16).frame(width: 320)
    }
    private func bodyDescription(_ snapshot: CaptureSnapshot) -> String {
        L10n.format("app.ui.captured_bytes", String(describing: L10n.protocolLabel(snapshot.bodyState)), String(describing: snapshot.captured), String(describing: snapshot.bodySize))
    }
    private func copy(_ text: String) {
        NSPasteboard.general.clearContents(); NSPasteboard.general.setString(text, forType: .string)
    }
}
