import AppKit
import SwiftUI

/// Display captured metadata without interpreting a rewrite outcome as HTTP status.
struct CaptureRecordSummary {
    let record: CaptureRecord
    var handshake: CaptureRecord? = nil
    var request: CaptureSnapshot? { record.kind == "websocket" ? handshake?.original : record.original }
    var host: String {
        guard let host = request?.host, !host.isEmpty else { return record.identity.destination }
        return host
    }
    var method: String {
        guard record.kind == "websocket" else { return request?.method ?? "HTTP" }
        switch record.stage {
        case "websocket_client": return "WS ↑"
        case "websocket_server": return "WS ↓"
        default: return "WS"
        }
    }
    var path: String {
        if record.kind == "websocket" {
            let direction: String
            switch record.stage {
            case "websocket_client": direction = L10n.text("app.ui.client_server")
            case "websocket_server": direction = L10n.text("app.ui.server_client")
            default: direction = L10n.text("app.ui.message")
            }
            return direction + (record.sequence.map { " #\($0)" } ?? "")
        }
        let capturedPath = request?.path ?? ""
        let path = capturedPath.isEmpty ? "/" : capturedPath
        let query = request?.query ?? ""
        return path + (query.isEmpty ? "" : "?" + query)
    }
    var url: String { request?.url ?? "" }
    var statusCode: Int? {
        guard record.kind == "http" else { return nil }
        for name in ["response_actual", "response_original"] {
            if let status = record.snapshots[name]?.status, (100...999).contains(status) { return status }
        }
        return nil
    }
    var failed: Bool {
        record.error?.isEmpty == false || ["failed", "blocked"].contains(record.outcome)
            || record.snapshots.values.contains { $0.bodyState == "failed" }
    }
    var statusText: String {
        if let statusCode { return String(statusCode) }
        if failed { return L10n.text("app.ui.failed") }
        return record.kind == "http" ? L10n.text("app.ui.pending") : "—"
    }
    var duration: String { record.durationMS > 0 ? "\(record.durationMS) ms" : "—" }
}

struct CaptureHTTPStatusView: View {
    let summary: CaptureRecordSummary
    private var color: Color {
        if let code = summary.statusCode {
            switch code {
            case 200...299: return .green
            case 400...499: return .orange
            case 500...599: return .red
            default: return .secondary
            }
        }
        return summary.failed ? .red : .secondary
    }
    private var explanation: String {
        if let code = summary.statusCode {
            return L10n.format("app.ui.http", String(describing: code), String(describing: L10n.protocolLabel(summary.record.outcome))) + (summary.failed ? L10n.text("app.ui.capture_failed") : "")
        }
        if summary.failed { return L10n.format("app.ui.capture_outcome", String(describing: L10n.protocolLabel(summary.record.outcome)), String(describing: summary.record.error ?? L10n.text("app.ui.body_collection_failed"))) }
        return summary.record.kind == "http" ? L10n.text("app.ui.no_http_response_status_has_been_captured") : L10n.text("app.ui.websocket_messages_have_no_http_status")
    }
    var body: some View {
        Text(verbatim: summary.statusText)
            .font(.system(.caption, design: .monospaced).weight(.medium))
            .foregroundColor(color).lineLimit(1)
            .help(explanation)
            .accessibilityLabel(summary.statusCode.map { L10n.format("app.ui.http_status", String(describing: $0)) } ?? summary.statusText)
    }
}

@MainActor struct CaptureRecordTable: View {
    let records: [CaptureRecord]
    @Binding var selection: String?
    var body: some View {
        let byID = Dictionary(records.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        Table(records, selection: $selection) {
            TableColumn(L10n.text("app.ui.method")) { record in
                Text(verbatim: CaptureRecordSummary(record: record).method)
                    .font(.system(.caption, design: .monospaced).weight(.semibold))
                    .lineLimit(1)
                    .help(record.kind == "websocket" ? record.stage ?? "WebSocket" : record.original?.method ?? "HTTP")
            }.width(52)
            TableColumn(L10n.text("app.ui.request")) { record in
                let summary = CaptureRecordSummary(record: record, handshake: record.parentID.flatMap { byID[$0] })
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: summary.host).font(.callout.weight(.medium)).lineLimit(1).truncationMode(.middle)
                    Text(verbatim: summary.path).font(.caption).foregroundColor(.secondary).lineLimit(1).truncationMode(.middle)
                }
                .frame(maxWidth: .infinity, alignment: .leading).frame(height: 40)
                .help(summary.url.isEmpty ? summary.path : summary.url + (record.kind == "websocket" ? "\n" + summary.path : ""))
                .contextMenu {
                    Button(L10n.text("app.ui.copy_url")) {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(summary.url, forType: .string)
                    }.disabled(summary.url.isEmpty)
                }
            }.width(min: 100, ideal: 200)
            TableColumn(L10n.text("app.ui.status")) { record in
                CaptureHTTPStatusView(summary: CaptureRecordSummary(record: record))
                    .frame(maxWidth: .infinity, alignment: .trailing)
            }.width(54)
            TableColumn(L10n.text("app.ui.time")) { record in
                Text(verbatim: CaptureRecordSummary(record: record).duration)
                    .font(.system(.caption, design: .monospaced)).monospacedDigit()
                    .lineLimit(1).frame(maxWidth: .infinity, alignment: .trailing)
                    .help(L10n.text("app.ui.recorded_duration_in_milliseconds"))
            }.width(66)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .accessibilityLabel(L10n.text("app.ui.captured_requests"))
    }
}
