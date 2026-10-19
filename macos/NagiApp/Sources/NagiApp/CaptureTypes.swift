import Foundation

struct CaptureScope: Codable, Equatable, Sendable {
    var domains: [String] = []
    var processPaths: [String] = []
    var appPaths: [String] = []
    enum CodingKeys: String, CodingKey { case domains; case processPaths = "process_paths", appPaths = "app_paths" }
}
struct CaptureStatus: Codable, Sendable {
    let sessionID: String
    let revision: UInt64
    let state, profile, backend, owner: String
    let scope: CaptureScope
    let bodyLimit, records: Int
    let bytes: Int64
    let dropped, truncated: UInt64
    let readOnly: Bool
    enum CodingKeys: String, CodingKey {
        case revision, state, profile, backend, owner, scope, records, bytes, dropped, truncated
        case sessionID = "session_id", bodyLimit = "body_limit", readOnly = "read_only"
    }
}
struct CaptureIdentity: Codable, Sendable {
    let sessionID: String
    let revision: UInt64
    let uid: UInt32
    let process, processPath, source, destination: String
    enum CodingKeys: String, CodingKey { case revision, uid, process, source, destination; case sessionID = "session_id", processPath = "process_path" }
}
struct CaptureSnapshot: Codable, Sendable {
    let scheme, host, method, path, query: String?
    let port, status: Int?
    let messageType: String?
    let headers: [String: [String]]?
    let trailers: [String: [String]]?
    let bodyState: String
    let bodySize, captured: Int64
    enum CodingKeys: String, CodingKey {
        case scheme, host, method, path, query, port, status, headers, trailers, captured
        case messageType = "message_type", bodyState = "body_state", bodySize = "body_size"
    }
    var url: String { (scheme.map { $0 + "://" } ?? "") + (host ?? "") + (port.map { ":\($0)" } ?? "") + (path ?? "") + ((query?.isEmpty == false) ? "?" + query! : "") }
    var headerText: String { Self.fields(headers) }
    static func fields(_ fields: [String: [String]]?) -> String {
        (fields ?? [:]).keys.sorted().flatMap { key in (fields?[key] ?? []).map { key + ": " + $0 } }.joined(separator: "\n")
    }
}
struct CaptureRecord: Codable, Identifiable, Sendable {
    let id, sessionID, kind, generation, ruleVersion, started, outcome: String
    let parentID, stage, error: String?
    let sequence: UInt64?
    let identity: CaptureIdentity
    let durationMS: Int64
    let ruleIDs: [String]?
    let closeCode: Int?
    let snapshots: [String: CaptureSnapshot]
    enum CodingKeys: String, CodingKey {
        case id, kind, generation, started, outcome, stage, error, sequence, identity, snapshots
        case sessionID = "session_id", parentID = "parent_id", ruleVersion = "rule_version", durationMS = "duration_ms", ruleIDs = "rule_ids", closeCode = "close_code"
    }
    var original: CaptureSnapshot? { snapshots["request_original"] ?? snapshots["message_original"] ?? snapshots["response_original"] }
    var title: String { [original?.method, original?.url].compactMap { $0 }.joined(separator: " ") }
}
struct CaptureBodyChunk: Decodable, Sendable {
    let data: Data
    let offset, nextOffset: Int64
    let eof: Bool
    let state: String
    enum CodingKeys: String, CodingKey { case data, offset, eof, state; case nextOffset = "next_offset" }
}
struct CaptureReply: Decodable, Sendable {
    let sensitive: Bool
    let status: CaptureStatus?
    let records: [CaptureRecord]?
    let record: CaptureRecord?
    let body: CaptureBodyChunk?
    let nextCursor, error: String?
    enum CodingKeys: String, CodingKey { case sensitive, status, records, record, body, error; case nextCursor = "next_cursor" }
}
struct CaptureApplication: Decodable, Identifiable, Sendable {
    let process, processPath, appPath: String
    var id: String { processPath + appPath }
    enum CodingKeys: String, CodingKey { case process; case processPath = "process_path", appPath = "app_path" }
}
struct CaptureSelectionContext: Sendable {
    let record: CaptureRecord
    let snapshotName: String
    let body: Data
    var field: CaptureFieldSelection? = nil
}
struct CaptureFieldSelection: Sendable {
    let kind: String
    var name: String? = nil
    var pointer: String? = nil
    var selectedText: String? = nil
    var value: String? = nil
    var stage: String? = nil
}

/// Formatting and potentially large body work stay outside the main actor.
enum CaptureBodyFormat: String, CaseIterable { case raw = "Raw", json = "JSON", binary = "Hex", diff = "Before / After" }
func captureBodyText(_ data: Data, format: CaptureBodyFormat, before: Data? = nil) -> String {
    switch format {
    case .raw: return String(data: data, encoding: .utf8) ?? "Binary body · \(data.count) bytes. Select Hex to inspect."
    case .json:
        guard let object = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]), let formatted = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys, .fragmentsAllowed]), let text = String(data: formatted, encoding: .utf8) else { return "Body is not valid JSON.\n" + (String(data: data, encoding: .utf8) ?? "") }
        return text
    case .binary:
        return stride(from: 0, to: data.count, by: 16).map { offset in String(format: "%08x  ", offset) + data[offset..<min(offset + 16, data.count)].map { String(format: "%02x", $0) }.joined(separator: " ") }.joined(separator: "\n")
    case .diff:
        guard let before else { return "The corresponding original snapshot is unavailable." }
        if before == data { return "No body changes.\n" + captureBodyText(data, format: .raw) }
        let old = captureBodyText(before, format: .raw).components(separatedBy: "\n")
        let new = captureBodyText(data, format: .raw).components(separatedBy: "\n")
        return old.map { "− " + $0 }.joined(separator: "\n") + "\n" + new.map { "+ " + $0 }.joined(separator: "\n")
    }
}
