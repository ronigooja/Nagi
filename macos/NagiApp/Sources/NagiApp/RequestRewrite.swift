import Foundation

/// A lossless JSON syntax tree keeps large numeric tokens intact while editing
/// an explicitly exported rule. Validation and matching remain CLI operations.
indirect enum RewriteJSON: Equatable {
    case object([String: RewriteJSON]), array([RewriteJSON]), string(String), scalar(String)
    subscript(_ key: String) -> RewriteJSON? { if case .object(let fields) = self { return fields[key] }; return nil }
    var string: String? { if case .string(let text) = self { return text }; return nil }
    var array: [RewriteJSON] { if case .array(let items) = self { return items }; return [] }
    var text: String {
        switch self {
        case .object(let fields): return "{" + fields.keys.sorted().map { Self.string($0).text + ":" + fields[$0]!.text }.joined(separator: ",") + "}"
        case .array(let items): return "[" + items.map(\.text).joined(separator: ",") + "]"
        case .string(let value): return String(data: try! JSONEncoder().encode(value), encoding: .utf8)!
        case .scalar(let value): return value
        }
    }
    var data: Data { Data(text.utf8) }
    static func parse(_ data: Data) throws -> RewriteJSON {
        guard data.count <= 16 * 1_048_576, let source = String(data: data, encoding: .utf8) else { throw RewriteFormError.invalidJSON }
        var parser = Parser(bytes: Array(source.utf8))
        let value = try parser.value(depth: 0)
        parser.space()
        guard parser.index == parser.bytes.count else { throw RewriteFormError.invalidJSON }
        return value
    }
    private struct Parser {
        let bytes: [UInt8]; var index = 0
        mutating func space() { while index < bytes.count && [9, 10, 13, 32].contains(bytes[index]) { index += 1 } }
        mutating func take(_ byte: UInt8) -> Bool { space(); if index < bytes.count && bytes[index] == byte { index += 1; return true }; return false }
        mutating func value(depth: Int) throws -> RewriteJSON {
            guard depth < 128 else { throw RewriteFormError.invalidJSON }
            space(); guard index < bytes.count else { throw RewriteFormError.invalidJSON }
            if bytes[index] == 34 {
                let begin = index; index += 1
                while index < bytes.count {
                    let byte = bytes[index]; index += 1
                    if byte == 92 { guard index < bytes.count else { throw RewriteFormError.invalidJSON }; index += 1 }
                    else if byte == 34 {
                        guard let value = try? JSONDecoder().decode(String.self, from: Data(bytes[begin..<index])) else { throw RewriteFormError.invalidJSON }
                        return .string(value)
                    }
                }
                throw RewriteFormError.invalidJSON
            }
            if take(123) {
                var fields: [String: RewriteJSON] = [:]
                if take(125) { return .object(fields) }
                repeat {
                    guard case .string(let key) = try value(depth: depth + 1), fields[key] == nil, take(58) else { throw RewriteFormError.invalidJSON }
                    fields[key] = try value(depth: depth + 1)
                    if take(125) { return .object(fields) }
                } while take(44)
                throw RewriteFormError.invalidJSON
            }
            if take(91) {
                var items: [RewriteJSON] = []
                if take(93) { return .array(items) }
                repeat {
                    items.append(try value(depth: depth + 1))
                    if take(93) { return .array(items) }
                } while take(44)
                throw RewriteFormError.invalidJSON
            }
            let begin = index
            while index < bytes.count && ![9, 10, 13, 32, 44, 93, 125].contains(bytes[index]) { index += 1 }
            let token = String(decoding: bytes[begin..<index], as: UTF8.self)
            guard token == "true" || token == "false" || token == "null" || token.range(of: #"^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$"#, options: .regularExpression) != nil else { throw RewriteFormError.invalidJSON }
            return .scalar(token)
        }
    }
}

enum RewriteFormError: LocalizedError {
    case invalidJSON, invalid(String)
    var errorDescription: String? {
        switch self { case .invalidJSON: return "Enter one valid JSON value. Values have not been submitted."
        case .invalid(let text): return text }
    }
}

struct RewriteDomain: Equatable {
    let id: String; let pattern: String
    init?(_ value: JSONValue) {
        guard let id = value["id"]?.string, let pattern = value["pattern"]?.string else { return nil }
        self.id = id; self.pattern = pattern
    }
}
struct RewriteRuleSummary: Equatable {
    let id: String; let enabled: Bool; let domain: String; let methods: [String]; let path: String; let onError: String; let kinds: [String]
    init?(_ value: JSONValue) {
        guard let id = value["id"]?.string, let enabled = value["enabled"]?.bool else { return nil }
        self.id = id; self.enabled = enabled; domain = value["match"]?["domain"]?.string ?? "unknown"
        methods = value["match"]?["methods"]?.array.compactMap(\.string) ?? []
        path = value["match"]?["path_prefix"]?.string ?? "unknown"; onError = value["on_error"]?.string ?? "unknown"
        kinds = value["operations"]?.array.compactMap { $0.string ?? $0["op"]?.string } ?? []
    }
}
struct RewriteStatus {
    let value: JSONValue
    var profile: String? { value["profile"]?.string }
    var intent: Bool? { value["enabled_intent"]?.bool }
    var actual: String { value["actual"]?.string ?? "unavailable" }
    var tunRunning: Bool? { value["tun_running"]?.bool }
    var canEnable: Bool { tunRunning == true }
    var menuTitle: String { "Request Rewrite · \(actual.capitalized)" }
    var recovery: String { value["recovery"]?.string ?? value["recovery_command"]?.string ?? "Use the CLI for diagnostics and advanced recovery." }
    static func bool(_ value: Bool?) -> String { value.map { $0 ? "Yes" : "No" } ?? "unknown" }
    var detail: String {
        let ca = value["ca"]
        return """
        Profile: \(profile ?? "unknown")    Enabled intent: \(Self.bool(intent))
        Actual: \(actual)    TUN running: \(Self.bool(tunRunning))    Rewrite service running: \(Self.bool(value["service_running"]?.bool))
        Backend: \(value["backend"]?.string ?? "unknown")    Protocol: \(value["protocol_version"]?.int64.map(String.init) ?? "unknown")
        Configured version: \(value["configured_rule_version"]?.string ?? "unknown")
        Active version: \(value["active_rule_version"]?.string ?? "unknown")
        Generation: \(value["generation"]?.string ?? "unknown")    Engine epoch: \(value["engine_epoch"]?.string ?? "unknown")
        CA: \(ca?["state"]?.string ?? "unknown")    Trust: \(ca?["trust"]?.string ?? "unknown")
        Last error: \(value["last_error"]?.string ?? "none reported")
        \(recovery)
        """
    }
}

struct RewriteOperationDraft {
    static let kinds = ["header_set", "header_delete", "url_set", "json_set", "json_delete"]
    var kind = "header_set", name = "", component = "path", pointer = "", value = ""
    init() {}
    init(_ node: RewriteJSON) {
        kind = node["op"]?.string ?? "header_set"; name = node["name"]?.string ?? ""
        component = node["component"]?.string ?? "path"; pointer = node["pointer"]?.string ?? ""
        value = kind == "json_set" ? (node["value"]?.text ?? "null") : (node["value"]?.string ?? "")
    }
    func json() throws -> RewriteJSON {
        var fields: [String: RewriteJSON] = ["op": .string(kind)]
        switch kind {
        case "header_set", "header_delete": fields["name"] = .string(name)
        case "url_set": fields["component"] = .string(component)
        case "json_set", "json_delete": fields["pointer"] = .string(pointer)
        default: throw RewriteFormError.invalid("Choose a supported operation.")
        }
        if kind == "json_set" { fields["value"] = try RewriteJSON.parse(Data(value.utf8)) }
        else if kind == "header_set" || kind == "url_set" { fields["value"] = .string(value) }
        return .object(fields)
    }
}
struct RewriteConsentDraft {
    var domain = "", schemes = "https", ports = "443"
    init() {}
    init(_ node: RewriteJSON) {
        domain = node["domain"]?.string ?? ""; schemes = node["schemes"]?.array.compactMap(\.string).joined(separator: ", ") ?? ""
        ports = node["ports"]?.array.map(\.text).joined(separator: ", ") ?? ""
    }
    func json() throws -> RewriteJSON {
        let portStrings = rewriteTokens(ports)
        guard !portStrings.isEmpty, portStrings.allSatisfy({ Int($0).map { (1...65535).contains($0) } == true }) else { throw RewriteFormError.invalid("Destination consent needs explicit ports from 1 through 65535.") }
        return .object(["domain": .string(domain), "schemes": .array(rewriteTokens(schemes).map(RewriteJSON.string)), "ports": .array(portStrings.map { .scalar(String(Int($0)!)) })])
    }
}
func rewriteTokens(_ text: String) -> [String] { text.split(whereSeparator: { $0 == "," || $0.isWhitespace }).map(String.init) }
struct RewriteRuleDraft {
    var id = "", enabled = true, domain = "", methods = "GET", path = "/", onError = "block"
    var operations: [RewriteOperationDraft] = [], consent: [RewriteConsentDraft] = []
    init() {}
    init(_ node: RewriteJSON) {
        id = node["id"]?.string ?? ""; enabled = node["enabled"]?.text == "true"
        domain = node["match"]?["domain"]?.string ?? ""; methods = node["match"]?["methods"]?.array.compactMap(\.string).joined(separator: ", ") ?? ""
        path = node["match"]?["path_prefix"]?.string ?? "/"; onError = node["on_error"]?.string ?? "block"
        operations = node["operations"]?.array.map(RewriteOperationDraft.init) ?? []
        consent = node["redirect_consent"]?.array.map(RewriteConsentDraft.init) ?? []
    }
    func json() throws -> Data {
        guard !id.isEmpty, !domain.isEmpty, !operations.isEmpty else { throw RewriteFormError.invalid("Enter an ID, match domain, and at least one operation.") }
        let node = RewriteJSON.object(["id": .string(id), "enabled": .scalar(enabled ? "true" : "false"),
            "match": .object(["domain": .string(domain), "methods": .array(rewriteTokens(methods).map(RewriteJSON.string)), "path_prefix": .string(path)]),
            "on_error": .string(onError), "redirect_consent": .array(try consent.map { try $0.json() }), "operations": .array(try operations.map { try $0.json() })])
        let data = node.data
        guard data.count <= 262_144 else { throw RewriteFormError.invalid("The rule exceeds the CLI's 256 KiB input limit.") }
        return data
    }
}
