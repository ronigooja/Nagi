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

struct RewriteScriptSummary: Equatable {
    let id: String; let bodyAccess: String
    init?(_ value: JSONValue) {
        guard let id = value["id"]?.string, let access = value["body_access"]?.string else { return nil }
        self.id = id; bodyAccess = access
    }
}
struct RewriteRuleSummary: Equatable {
    let id: String; let enabled: Bool; let stage: String; let domain: String; let methods: [String]; let path: String; let onError: String; let kinds: [String]
    init?(_ value: JSONValue) {
        guard let id = value["id"]?.string, let enabled = value["enabled"]?.bool, let stage = value["stage"]?.string else { return nil }
        self.id = id; self.enabled = enabled; self.stage = stage; domain = value["match"]?["domain"]?.string ?? "unknown"
        methods = value["match"]?["methods"]?.array.compactMap(\.string) ?? []
        path = value["match"]?["path_prefix"]?.string ?? "Any path"; onError = value["on_error"]?.string ?? "unknown"
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
        Config schema: \(value["config_version"]?.int64.map(String.init) ?? "unknown")
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
    static let kinds = ["header_set", "header_append", "header_delete", "url_set", "method_set", "status_set", "json_set", "json_delete", "body_set", "text_replace", "regex_replace", "trailer_set", "trailer_delete", "script"]
    var kind = "header_set", name = "", component = "path", pointer = "", value = ""
    var pattern = "", replacement = "", all = false, encoding = "text", script = "", messageType = ""
    init() {}
    init(_ node: RewriteJSON) {
        kind = node["op"]?.string ?? "header_set"; name = node["name"]?.string ?? ""
        component = node["component"]?.string ?? "path"; pointer = node["pointer"]?.string ?? ""
        value = ["json_set", "status_set"].contains(kind) ? (node["value"]?.text ?? "null") : (node["value"]?.string ?? "")
        pattern = node["pattern"]?.string ?? ""; replacement = node["replacement"]?.string ?? ""
        all = node["all"]?.text == "true"; encoding = node["encoding"]?.string ?? "text"
        script = node["script"]?.string ?? ""; messageType = node["message_type"]?.string ?? ""
    }
    func json() throws -> RewriteJSON {
        var fields: [String: RewriteJSON] = ["op": .string(kind)]
        switch kind {
        case "header_set", "header_append", "header_delete", "trailer_set", "trailer_delete": fields["name"] = .string(name)
        case "url_set": fields["component"] = .string(component)
        case "json_set", "json_delete": fields["pointer"] = .string(pointer)
        case "body_set": fields["encoding"] = .string(encoding)
        case "text_replace", "regex_replace":
            fields["pattern"] = .string(pattern); fields["replacement"] = .string(replacement); fields["all"] = .scalar(all ? "true" : "false")
        case "script": fields["script"] = .string(script)
        case "method_set", "status_set": break
        default: throw RewriteFormError.invalid("Choose a supported operation.")
        }
        if kind == "json_set" { fields["value"] = try RewriteJSON.parse(Data(value.utf8)) }
        else if kind == "status_set" {
            guard let status = Int(value), (200...599).contains(status) else { throw RewriteFormError.invalid("Enter a response status from 200 through 599.") }
            fields["value"] = .scalar(String(status))
        } else if ["header_set", "header_append", "trailer_set", "url_set", "method_set", "body_set"].contains(kind) { fields["value"] = .string(value) }
        if !messageType.isEmpty { fields["message_type"] = .string(messageType) }
        return .object(fields)
    }
}
struct RewriteScriptDraft {
    var id = "", bodyAccess = "none", source = "function transform(input) { return {action: 'unchanged'}; }"
    init() {}
    init(_ node: RewriteJSON) {
        id = node["id"]?.string ?? ""; bodyAccess = node["body_access"]?.string ?? "none"; source = node["source"]?.string ?? ""
    }
    func json() throws -> Data {
        guard !id.isEmpty, !source.isEmpty else { throw RewriteFormError.invalid("Enter a script ID and source.") }
        guard source.utf8.count <= 65_536 else { throw RewriteFormError.invalid("The script source exceeds 64 KiB.") }
        return RewriteJSON.object(["id": .string(id), "body_access": .string(bodyAccess), "source": .string(source)]).data
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
    static let stages = ["request", "response", "websocket_client", "websocket_server"]
    var id = "", enabled = true, stage = "request", domain = "", methods = "GET", path = "/", onError = "block", rangeMode = "fragment"
    var pathRegex = "", query = "", headers = "", statusCodes = "", contentTypes = "", responseHeaders = "", messageTypes = ""
    var operations: [RewriteOperationDraft] = [], consent: [RewriteConsentDraft] = []
    init() {}
    init(_ node: RewriteJSON) {
        id = node["id"]?.string ?? ""; enabled = node["enabled"]?.text == "true"; stage = node["stage"]?.string ?? "request"
        domain = node["match"]?["domain"]?.string ?? ""; methods = node["match"]?["methods"]?.array.compactMap(\.string).joined(separator: ", ") ?? ""
        path = node["match"]?["path_prefix"]?.string ?? ""; onError = node["on_error"]?.string ?? "block"
        rangeMode = node["range_mode"]?.string ?? "fragment"; pathRegex = node["match"]?["path_regex"]?.string ?? ""
        query = node["match"]?["query"]?.text ?? ""; headers = node["match"]?["headers"]?.text ?? ""
        statusCodes = node["match"]?["status_codes"]?.array.map(\.text).joined(separator: ", ") ?? ""
        contentTypes = node["match"]?["content_types"]?.array.compactMap(\.string).joined(separator: ", ") ?? ""
        responseHeaders = node["match"]?["response_headers"]?.text ?? ""
        messageTypes = node["match"]?["message_types"]?.array.compactMap(\.string).joined(separator: ", ") ?? ""
        operations = node["operations"]?.array.map(RewriteOperationDraft.init) ?? []
        consent = node["redirect_consent"]?.array.map(RewriteConsentDraft.init) ?? []
    }
    func json() throws -> Data {
        guard !id.isEmpty, !domain.isEmpty, !operations.isEmpty else { throw RewriteFormError.invalid("Enter an ID, match domain, and at least one operation.") }
        var match: [String: RewriteJSON] = ["domain": .string(domain)]
        for (key,text) in [("methods",methods),("content_types",contentTypes),("message_types",messageTypes)] {
            let values = rewriteTokens(text)
            if !values.isEmpty { match[key] = .array(values.map(RewriteJSON.string)) }
        }
        if !path.isEmpty { match["path_prefix"] = .string(path) }
        if !pathRegex.isEmpty { match["path_regex"] = .string(pathRegex) }
        for (key,text) in [("query",query),("headers",headers),("response_headers",responseHeaders)] where !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            let value = try RewriteJSON.parse(Data(text.utf8))
            guard case .object(let pairs) = value, pairs.values.allSatisfy({ node in
                if case .array(let values) = node { return values.allSatisfy { $0.string != nil } }; return false
            }) else { throw RewriteFormError.invalid("Match predicates require a JSON object mapping names to string arrays.") }
            match[key] = value
        }
        let codes = rewriteTokens(statusCodes)
        if !codes.isEmpty {
            guard codes.allSatisfy({ Int($0).map { (100...599).contains($0) } == true }) else { throw RewriteFormError.invalid("Enter status codes from 100 through 599.") }
            match["status_codes"] = .array(codes.map { .scalar(String(Int($0)!)) })
        }
        let node = RewriteJSON.object(["id": .string(id), "enabled": .scalar(enabled ? "true" : "false"), "stage": .string(stage), "range_mode": .string(rangeMode),
            "match": .object(match), "on_error": .string(onError), "redirect_consent": .array(try consent.map { try $0.json() }), "operations": .array(try operations.map { try $0.json() })])
        let data = node.data
        guard data.count <= 1_048_576 else { throw RewriteFormError.invalid("The rule exceeds the CLI's 1 MiB document input limit.") }
        return data
    }
}
