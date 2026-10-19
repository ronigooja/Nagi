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
            case "websocket_client": direction = "Client → Server"
            case "websocket_server": direction = "Server → Client"
            default: direction = "Message"
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
        if failed { return "Failed" }
        return record.kind == "http" ? "Pending" : "—"
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
            return "HTTP \(code) · \(summary.record.outcome)" + (summary.failed ? " · Capture failed" : "")
        }
        if summary.failed { return "Capture outcome: \(summary.record.outcome) · \(summary.record.error ?? "Body collection failed")" }
        return summary.record.kind == "http" ? "No HTTP response status has been captured." : "WebSocket messages have no HTTP status."
    }
    var body: some View {
        Text(summary.statusText)
            .font(.system(.caption, design: .monospaced).weight(.medium))
            .foregroundColor(color).lineLimit(1)
            .help(explanation)
            .accessibilityLabel(summary.statusCode.map { "HTTP status \($0)" } ?? summary.statusText)
    }
}

@MainActor struct CaptureRecordTable: View {
    let records: [CaptureRecord]
    @Binding var selection: String?
    var body: some View {
        let byID = Dictionary(records.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
        Table(records, selection: $selection) {
            TableColumn("Method") { record in
                Text(CaptureRecordSummary(record: record).method)
                    .font(.system(.caption, design: .monospaced).weight(.semibold))
                    .lineLimit(1)
                    .help(record.kind == "websocket" ? record.stage ?? "WebSocket" : record.original?.method ?? "HTTP")
            }.width(52)
            TableColumn("Request") { record in
                let summary = CaptureRecordSummary(record: record, handshake: record.parentID.flatMap { byID[$0] })
                VStack(alignment: .leading, spacing: 3) {
                    Text(summary.host).font(.callout.weight(.medium)).lineLimit(1).truncationMode(.middle)
                    Text(summary.path).font(.caption).foregroundColor(.secondary).lineLimit(1).truncationMode(.middle)
                }
                .frame(maxWidth: .infinity, alignment: .leading).frame(height: 40)
                .help(summary.url.isEmpty ? summary.path : summary.url + (record.kind == "websocket" ? "\n" + summary.path : ""))
                .contextMenu {
                    Button("Copy URL") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(summary.url, forType: .string)
                    }.disabled(summary.url.isEmpty)
                }
            }.width(min: 100, ideal: 200)
            TableColumn("Status") { record in
                CaptureHTTPStatusView(summary: CaptureRecordSummary(record: record))
                    .frame(maxWidth: .infinity, alignment: .trailing)
            }.width(54)
            TableColumn("Time") { record in
                Text(CaptureRecordSummary(record: record).duration)
                    .font(.system(.caption, design: .monospaced)).monospacedDigit()
                    .lineLimit(1).frame(maxWidth: .infinity, alignment: .trailing)
                    .help("Recorded duration in milliseconds")
            }.width(66)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .accessibilityLabel("Captured requests")
    }
}
