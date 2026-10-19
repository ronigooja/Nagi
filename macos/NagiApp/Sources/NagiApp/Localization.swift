import Foundation

/// Installed bundles must resolve without SwiftPM's original build directory.
enum NagiResources {
    static let bundle = resolve(mainBundle: .main, developmentBundle: Bundle.module)

    static func resolve(mainBundle: Bundle, developmentBundle: @autoclosure () -> Bundle) -> Bundle {
        if let url = mainBundle.resourceURL?.appendingPathComponent("NagiApp_NagiApp.bundle"),
           let installed = Bundle(url: url) { return installed }
        return developmentBundle()
    }
}

/// Shared by AppKit and SwiftUI. Pass rendered strings to Text(verbatim:) when
/// the source includes user data; protocol identifiers are never localization keys.
enum L10n {
    static func text(_ key: String, bundle: Bundle = NagiResources.bundle) -> String {
        bundle.localizedString(forKey: key, value: key, table: "Localizable")
    }

    static func format(_ key: String, _ arguments: CVarArg..., bundle: Bundle = NagiResources.bundle) -> String {
        String(format: text(key, bundle: bundle), locale: formattingLocale(bundle), arguments: arguments)
    }

    /// Only protocol fields use this mapping. Never pass user names or captured content.
    static func protocolLabel(_ value: String, bundle: Bundle = NagiResources.bundle) -> String {
        guard let key = protocolKeys[value] ?? protocolKeys[value.lowercased()] else { return value }
        return text(key, bundle: bundle)
    }
    private static let protocolKeys: [String: String] = [
        "selector": "app.protocol.selector",
        "urltest": "app.protocol.urltest",
        "fallback": "app.protocol.fallback",
        "loadbalance": "app.protocol.loadbalance",
        "load-balance": "app.protocol.load-balance",
        "relay": "app.protocol.relay",
        "upgraded": "app.protocol.upgraded",
        "client_to_server": "app.protocol.client_to_server",
        "server_to_client": "app.protocol.server_to_client",

        "running": "app.protocol.running",
        "starting": "app.protocol.starting",
        "stopped": "app.protocol.stopped",
        "unavailable": "app.protocol.unavailable",
        "unknown": "app.protocol.unknown",
        "available": "app.protocol.available",
        "enabled": "app.protocol.enabled",
        "disabled": "app.protocol.disabled",
        "idle": "app.protocol.idle",
        "active": "app.protocol.active",
        "inactive": "app.protocol.inactive",
        "error": "app.protocol.error",
        "failed": "app.protocol.failed",
        "warning": "app.protocol.warning",
        "pending": "app.protocol.pending",
        "ready": "app.protocol.ready",
        "recording": "app.protocol.recording",
        "paused": "app.protocol.paused",
        "uncertain": "app.protocol.uncertain",
        "complete": "app.protocol.complete",
        "collecting": "app.protocol.collecting",
        "truncated": "app.protocol.truncated",
        "cutoff": "app.protocol.cutoff",
        "empty": "app.protocol.empty",
        "blocked": "app.protocol.blocked",
        "unchanged": "app.protocol.unchanged",
        "rewritten": "app.protocol.rewritten",
        "forwarded": "app.protocol.forwarded",
        "forward_original": "app.protocol.forward_original",
        "generated": "app.protocol.generated",
        "missing": "app.protocol.missing",
        "trusted": "app.protocol.trusted",
        "untrusted": "app.protocol.untrusted",
        "installed": "app.protocol.installed",
        "not_installed": "app.protocol.not_installed",
        "up": "app.protocol.up",
        "down": "app.protocol.down",
        "absent": "app.protocol.absent",
        "rule": "app.protocol.rule",
        "global": "app.protocol.global",
        "direct": "app.protocol.direct",
        "request": "app.protocol.request",
        "response": "app.protocol.response",
        "exchange": "app.protocol.exchange",
        "websocket_client": "app.protocol.websocket_client",
        "websocket_server": "app.protocol.websocket_server",
        "request_original": "app.protocol.request_original",
        "request_actual": "app.protocol.request_actual",
        "response_original": "app.protocol.response_original",
        "response_actual": "app.protocol.response_actual",
        "message_original": "app.protocol.message_original",
        "message_actual": "app.protocol.message_actual",
        "text": "app.protocol.text",
        "binary": "app.protocol.binary",
        "scheme": "app.protocol.scheme",
        "host": "app.protocol.host",
        "port": "app.protocol.port",
        "path": "app.protocol.path",
        "query": "app.protocol.query",
        "download": "app.protocol.download",
        "apply": "app.protocol.apply",
        "validate": "app.protocol.validate",
        "decode": "app.protocol.decode",
        "not_applied": "app.protocol.not_applied",
        "selected_profile_changed": "app.protocol.selected_profile_changed",
        "manual_profile": "app.protocol.manual_profile",
        "profile_changed": "app.protocol.profile_changed",
        "subscription_changed": "app.protocol.subscription_changed",
        "body_limit": "app.protocol.body_limit",
        "disconnected": "app.protocol.disconnected",
        "finished": "app.protocol.finished",
        "closed": "app.protocol.closed",
        "open": "app.protocol.open",
        "not_required": "app.protocol.not_required",
        "expired": "app.protocol.expired",
        "invalid": "app.protocol.invalid",
        "none": "app.protocol.none",
        "automatic": "app.protocol.automatic",
        "fixed": "app.protocol.fixed",
        "unsupported": "app.protocol.unsupported",
        "stale": "app.protocol.stale",
        "recovery_required": "app.protocol.recovery_required",
        "not_ready": "app.protocol.not_ready",
        "starting_recorder": "app.protocol.starting_recorder",
        "recovering": "app.protocol.recovering",
        "ok": "app.protocol.ok",
        "untested": "app.protocol.untested",
    ]

    static func formattingLocale(_ bundle: Bundle) -> Locale {
        // Plural rules follow the selected App language, even when the user's
        // region uses another language. Keep the region for numeric formatting.
        let language = bundle.bundleURL.pathExtension == "lproj"
            ? bundle.bundleURL.deletingPathExtension().lastPathComponent
            : (bundle.preferredLocalizations.first ?? "en")
        return Locale(identifier: language + "_" + (Locale.current.region?.identifier ?? "US"))
    }
}

/// JSON scalar arguments preserve their type until validated against a known ID.
enum MessageArgument: Decodable, Equatable, Sendable {
    case string(String), number(Double), bool(Bool), null
    init(from decoder: Decoder) throws {
        let value = try decoder.singleValueContainer()
        if value.decodeNil() { self = .null }
        else if let v = try? value.decode(Bool.self) { self = .bool(v) }
        else if let v = try? value.decode(String.self) { self = .string(v) }
        else { self = .number(try value.decode(Double.self)) }
    }
}

struct MessageDescriptor: Decodable, Equatable, Sendable {
    let id: String
    let args: [String: MessageArgument]
    enum CodingKeys: String, CodingKey { case id, args }
    init(id: String, args: [String: MessageArgument] = [:]) { self.id = id; self.args = args }
    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        args = values.contains(.args) ? try values.decode([String: MessageArgument].self, forKey: .args) : [:]
    }

    /// Only registered IDs with exact named argument signatures may format.
    func render(original: String, bundle: Bundle = NagiResources.bundle) -> String {
        guard let signature = NativeMessageCatalog.signatures[id], Set(args.keys) == Set(signature.keys) else { return original }
        var arguments: [CVarArg] = []
        for name in signature.keys.sorted() {
            let value: String
            switch (signature[name], args[name]) {
            case (.string?, .string(let string)?): value = string
            case (.number?, .number(let number)?) where number.isFinite: value = String(number)
            case (.boolean?, .bool(let boolean)?): value = boolean ? "true" : "false"
            default: return original
            }
            arguments.append(value as NSString)
        }
        let format = bundle.localizedString(forKey: id, value: id, table: "MessageCatalog")
        guard format != id else { return original }
        return String(format: format, locale: L10n.formattingLocale(bundle), arguments: arguments)
    }
}

/// A malformed optional record must never invalidate an otherwise valid response.
struct CLIMessages: Decodable, Sendable {
    let values: [String: MessageDescriptor]
    init(values: [String: MessageDescriptor] = [:]) { self.values = values }
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: PointerKey.self)
        var result: [String: MessageDescriptor] = [:]
        for key in container.allKeys where Self.isPointer(key.stringValue) {
            if let descriptor = try? container.decode(MessageDescriptor.self, forKey: key) {
                result[key.stringValue] = descriptor
            }
        }
        values = result
    }
    func display(_ original: String, at pointer: String, bundle: Bundle = NagiResources.bundle) -> String {
        values[pointer]?.render(original: original, bundle: bundle) ?? original
    }
    static func escape(_ component: String) -> String {
        component.replacingOccurrences(of: "~", with: "~0").replacingOccurrences(of: "/", with: "~1")
    }
    private static func isPointer(_ pointer: String) -> Bool {
        guard pointer.hasPrefix("/") else { return false }
        let characters = Array(pointer)
        for i in characters.indices where characters[i] == "~" {
            guard i + 1 < characters.count, characters[i + 1] == "0" || characters[i + 1] == "1" else { return false }
        }
        return true
    }
    private struct PointerKey: CodingKey {
        let stringValue: String
        var intValue: Int? { nil }
        init?(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { return nil }
    }
}

/// Checked-in named schemas are presentation data, shared by all response transports.
/// A missing catalog preserves original text; it cannot poison a CLI response.
enum NativeMessageCatalog {
    enum Kind: String, Decodable { case string, number, boolean }
    static let signatures: [String: [String: Kind]] = {
        guard let url = NagiResources.bundle.url(forResource: "MessageSignatures", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let signatures = try? JSONDecoder().decode([String: [String: Kind]].self, from: data) else { return [:] }
        return signatures
    }()
}
