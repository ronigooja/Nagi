import AppKit
import SwiftUI

enum CaptureDetailSection: String, CaseIterable {
    case overview = "Overview", headers = "Headers", body = "Body"
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
                    Text("Select a request").font(.headline)
                }.foregroundColor(.secondary).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .onChange(of: model.selectedID) { _ in jsonPointer = ""; jsonRulePresented = false }
    }
    private func detail(_ record: CaptureRecord) -> some View {
        let summary = CaptureRecordSummary(record: record, handshake: record.parentID.flatMap { id in model.records.first { $0.id == id } })
        return VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 8) {
                    Text(summary.method).font(.system(.callout, design: .monospaced).weight(.semibold))
                    Text(summary.host).font(.headline).lineLimit(1).truncationMode(.middle)
                    Spacer(minLength: 0)
                    CaptureHTTPStatusView(summary: summary)
                }
                Text(record.kind == "websocket" ? summary.path : summary.url)
                    .font(.callout).foregroundColor(.secondary).lineLimit(2).truncationMode(.middle)
                    .help(summary.url).textSelection(.enabled)
                    .contextMenu {
                        Button("Copy URL") { copy(summary.url) }.disabled(summary.url.isEmpty)
                        Button("Create Rule from URL…") { model.createRule(field: CaptureFieldSelection(kind: "url", value: summary.url, stage: record.stage)) }
                            .disabled(!canCreateRule || summary.url.isEmpty)
                    }
            }
            Picker("Snapshot", selection: Binding(get: { model.selectedSnapshotName }, set: { model.selectSnapshot($0) })) {
                ForEach(record.snapshots.keys.sorted(), id: \.self) {
                    Text($0.replacingOccurrences(of: "_", with: " ").capitalized).tag($0)
                }
            }
            Picker("Details", selection: $section) {
                ForEach(CaptureDetailSection.allCases, id: \.self) { Text($0.rawValue).tag($0) }
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
                    Button("JSON Pointer…") { jsonRulePresented = true }
                        .disabled(!canCreateRule)
                        .popover(isPresented: $jsonRulePresented, arrowEdge: .bottom) { jsonRule(record) }
                }
                Spacer(minLength: 0)
                Button("Create Rule…") { model.createRule() }.disabled(!canCreateRule)
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
                        field("Method", value: method, selection: CaptureFieldSelection(kind: "method", value: method, stage: record.stage))
                    }
                    if let status = snapshot.status {
                        field("HTTP status", value: String(status), selection: CaptureFieldSelection(kind: "status", value: String(status), stage: record.stage))
                    }
                    if let query = snapshot.query, !query.isEmpty {
                        field("Query", value: query, selection: CaptureFieldSelection(kind: "query", value: query, stage: record.stage))
                    }
                    field("Body collection", value: bodyDescription(snapshot))
                    if let type = snapshot.messageType { field("Message type", value: type) }
                }
                Divider()
                field("Process", value: record.identity.process)
                field("Process path", value: record.identity.processPath)
                field("Source", value: record.identity.source)
                field("Destination", value: record.identity.destination)
                field("Started", value: captureMinute(record.started))
                field("Duration", value: summary.duration)
                field("Outcome", value: record.outcome.isEmpty ? "Pending" : record.outcome)
                if let error = record.error, !error.isEmpty { field("Error", value: error) }
                if let code = record.closeCode { field("WebSocket close code", value: String(code)) }
                field("Matched rules", value: record.ruleIDs.flatMap { $0.isEmpty ? nil : $0.joined(separator: ", ") } ?? "None")
                field("Record ID", value: record.id)
                relatedMessages(record)
            }.frame(minWidth: 0, maxWidth: .infinity, alignment: .leading).padding(.trailing, 8)
        }
    }
    private func field(_ title: String, value: String, selection: CaptureFieldSelection? = nil) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption).foregroundColor(.secondary)
            Text(value.isEmpty ? "—" : value)
                .font(.system(.callout, design: .monospaced))
                .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                .contextMenu {
                    Button("Copy") { copy(value) }
                    if let selection {
                        Button("Create Rule from \(title)…") { model.createRule(field: selection) }.disabled(!canCreateRule || value.isEmpty)
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
                Text("WebSocket messages").font(.headline)
                ForEach(messages) { message in
                    Button(CaptureRecordSummary(record: message).path) { model.select(message.id) }
                }
            }
        } else if let parent = record.parentID {
            Divider()
            field("HTTP handshake", value: parent)
            Button("Show HTTP Handshake") { model.select(parent) }
                .disabled(!model.records.contains { $0.id == parent })
        }
    }
    private func headers(_ record: CaptureRecord) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let snapshot = record.snapshots[model.selectedSnapshotName] {
                    if model.format == .diff, model.selectedSnapshotName.hasSuffix("_actual"),
                       let original = record.snapshots[model.selectedSnapshotName.replacingOccurrences(of: "_actual", with: "_original")] {
                        Text("Original headers").font(.headline)
                        headerRows(original.headers, kind: "header", stage: record.stage, allowsRule: false)
                        Divider()
                        Text("Actual headers").font(.headline)
                    } else { Text("Headers").font(.headline) }
                    headerRows(snapshot.headers, kind: "header", stage: record.stage)
                    if snapshot.trailers?.isEmpty == false {
                        Divider()
                        Text("Trailers").font(.headline)
                        headerRows(snapshot.trailers, kind: "trailer", stage: record.stage)
                    }
                } else { Text("No snapshot selected.").foregroundColor(.secondary) }
            }.frame(minWidth: 0, maxWidth: .infinity, alignment: .leading).padding(.trailing, 8)
        }
    }
    @ViewBuilder private func headerRows(_ fields: [String: [String]]?, kind: String, stage: String?, allowsRule: Bool = true) -> some View {
        if let fields, !fields.isEmpty {
            let keys = fields.keys.sorted()
            ForEach(keys, id: \.self) { key in
                let value = (fields[key] ?? []).joined(separator: "\n")
                VStack(alignment: .leading, spacing: 4) {
                    Text(key).font(.system(.caption, design: .monospaced).weight(.semibold)).foregroundColor(.secondary)
                    Text(value).font(.system(.callout, design: .monospaced))
                        .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                }
                .contextMenu {
                    Button("Copy Header") { copy(key + ": " + value) }
                    Button("Copy Value") { copy(value) }
                    if allowsRule {
                        Button("Create Rule from \(key)…") { model.createRule(field: CaptureFieldSelection(kind: kind, name: key, value: value, stage: stage)) }
                            .disabled(!canCreateRule)
                    }
                }
                if key != keys.last { Divider() }
            }
        } else { Text("No headers.").font(.callout).foregroundColor(.secondary) }
    }
    private func bodyContent(_ record: CaptureRecord) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let snapshot = record.snapshots[model.selectedSnapshotName] {
                Text(bodyDescription(snapshot)).font(.caption).foregroundColor(.secondary)
            }
            ViewThatFits(in: .horizontal) {
                HStack { bodyFormat; copyBody }
                VStack(alignment: .leading) { bodyFormat; copyBody }
            }
            CaptureBodyTextView(text: model.bodyText, createRule: { selected in
                model.createRule(field: CaptureFieldSelection(kind: "text", selectedText: selected, value: selected, stage: record.stage))
            })
            .frame(minHeight: 80, maxHeight: .infinity)
            .background(Color(nsColor: .textBackgroundColor))
            .overlay(RoundedRectangle(cornerRadius: 4).stroke(Color.secondary.opacity(0.2)))
        }
    }
    private var bodyFormat: some View {
        Picker("Body format", selection: Binding(get: { model.format }, set: { model.setFormat($0) })) {
            ForEach(CaptureBodyFormat.allCases, id: \.self) { Text($0.rawValue).tag($0) }
        }.labelsHidden().pickerStyle(.segmented)
    }
    private var copyBody: some View {
        Button("Copy Body") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setData(model.selectedBody, forType: NSPasteboard.PasteboardType("public.data"))
            if let text = String(data: model.selectedBody, encoding: .utf8) { NSPasteboard.general.setString(text, forType: .string) }
        }.disabled(model.readingBody)
    }
    private func jsonRule(_ record: CaptureRecord) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("JSON Pointer").font(.headline)
            TextField("JSON pointer, e.g. /data/name", text: $jsonPointer)
            HStack {
                Button("Cancel") { jsonRulePresented = false }
                Spacer()
                Button("Create Rule…") {
                    model.createRule(field: CaptureFieldSelection(kind: "json", pointer: jsonPointer, stage: record.stage))
                    jsonRulePresented = false
                }.disabled(jsonPointer.isEmpty || !canCreateRule)
            }
        }.padding(16).frame(width: 320)
    }
    private func bodyDescription(_ snapshot: CaptureSnapshot) -> String {
        "\(snapshot.bodyState) · \(snapshot.captured) captured / \(snapshot.bodySize) bytes"
    }
    private func copy(_ text: String) {
        NSPasteboard.general.clearContents(); NSPasteboard.general.setString(text, forType: .string)
    }
}
