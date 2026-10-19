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
        let names: [String]
        switch id {
        case "cli.language.invalid": names = ["value"]
        case "cli.language.missing", "cli.language.duplicate": names = []
        default: return original
        }
        guard Set(args.keys) == Set(names) else { return original }
        var arguments: [CVarArg] = []
        for name in names {
            guard case .string(let value)? = args[name] else { return original }
            arguments.append(value as NSString)
        }
        let format = L10n.text(id, bundle: bundle)
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
