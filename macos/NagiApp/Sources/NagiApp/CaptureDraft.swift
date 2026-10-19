import Foundation

/// Only selection intent is mapped here. The CLI owns matching defaults,
/// validation, fixture construction, ordered execution and atomic persistence.
struct CaptureDraftIntent {
    var rule: RewriteRuleDraft
    init(_ context: CaptureSelectionContext) {
        var rule = RewriteRuleDraft()
        rule.id = "capture-" + UUID().uuidString.lowercased()
        rule.enabled = false
        rule.stage = context.record.stage ?? (context.snapshotName.hasPrefix("response_") ? "response" : "request")
        if ["url", "query", "method"].contains(context.field?.kind ?? "") { rule.stage = "request" }
        if context.field?.kind == "status" { rule.stage = "response" }
        rule.operations = Self.operations(context)
        self.rule = rule
    }
    static func operations(_ context: CaptureSelectionContext) -> [RewriteOperationDraft] {
        let field = context.field
        var operation = RewriteOperationDraft()
        switch field?.kind {
        case "header", "trailer":
            let trailer = field?.kind == "trailer"
            operation.kind = trailer ? "trailer_set" : "header_set"
            operation.name = field?.name ?? ""
            let snapshot = context.record.snapshots[context.snapshotName]
            let values = (trailer ? snapshot?.trailers : snapshot?.headers)?[operation.name] ?? [field?.value ?? ""]
            operation.value = values.first ?? ""
            if !trailer, values.count > 1 {
                return values.enumerated().map { index, value in
                    var item = operation; item.kind = index == 0 ? "header_set" : "header_append"; item.value = value; return item
                }
            }
        case "url":
            operation.kind = "url_set"; operation.component = "path"
            operation.value = context.record.snapshots[context.snapshotName]?.path ?? context.record.original?.path ?? "/"
        case "query": operation.kind = "url_set"; operation.component = "query"; operation.value = field?.value ?? ""
        case "method": operation.kind = "method_set"; operation.value = field?.value ?? "GET"
        case "status": operation.kind = "status_set"; operation.value = field?.value ?? "200"
        case "json":
            operation.kind = "json_set"; operation.pointer = field?.pointer ?? ""
            operation.value = Self.jsonValue(context.body, pointer: operation.pointer)?.text ?? "null"
        case "text":
            operation.kind = "text_replace"; operation.pattern = field?.selectedText ?? field?.value ?? ""
            operation.replacement = operation.pattern
        default:
            operation.kind = "body_set"
            if let text = String(data: context.body, encoding: .utf8) { operation.value = text }
            else { operation.value = context.body.base64EncodedString(); operation.encoding = "base64" }
        }
        return [operation]
    }
    static func jsonValue(_ bytes: Data, pointer: String) -> RewriteJSON? {
        guard var node = try? RewriteJSON.parse(bytes) else { return nil }
        if pointer.isEmpty { return node }
        guard pointer.hasPrefix("/") else { return nil }
        for raw in pointer.dropFirst().components(separatedBy: "/") {
            // Reject malformed escapes instead of selecting a different key.
            guard raw.range(of: "~(?![01])", options: .regularExpression) == nil else { return nil }
            let key = raw.replacingOccurrences(of: "~1", with: "/").replacingOccurrences(of: "~0", with: "~")
            switch node {
            case .object(let fields): guard let next = fields[key] else { return nil }; node = next
            case .array(let items):
                guard !key.isEmpty, key.utf8.allSatisfy({ (48...57).contains($0) }), key == "0" || key.first != "0", let index = Int(key), index < items.count else { return nil }; node = items[index]
            default: return nil
            }
        }
        return node
    }
    func data() throws -> Data {
        try RewriteJSON.object([
            "id": .string(rule.id), "stage": .string(rule.stage), "operations": .array(rule.operations.map { try $0.json() }),
            "on_error": .string(rule.onError), "range_mode": .string(rule.rangeMode), "enable": .scalar("false")
        ]).data
    }
}

@MainActor final class CaptureDraftModel: ObservableObject {
    let context: CaptureSelectionContext
    let run: RequestRewriteModel.Run
    @Published var rule: RewriteRuleDraft { didSet { previewResult = nil } }
    @Published var position = "0" { didSet { previewResult = nil } }
    @Published var enable = false
    @Published private(set) var profile: String?
    @Published private(set) var baseRuleVersion: String?
    @Published private(set) var fixture: RewriteJSON?
    @Published private(set) var previewResult: RewriteJSON?
    @Published private(set) var error: String?
    @Published private(set) var notice: String?
    @Published private(set) var working = false
    @Published private(set) var saving = false
    @Published private(set) var saved = false
    @Published private(set) var orderedRules: [String] = []
    private var cancelled = false
    private var call: Task<JSONValue, Error>?
    var ready: Bool { baseRuleVersion != nil && !working && !cancelled && !saved }
    var canPreview: Bool { fixture != nil && (rule.stage != "response" || fixture?["response"] != nil) }
    init(context: CaptureSelectionContext, run: @escaping RequestRewriteModel.Run) {
        self.context = context; self.run = run; rule = CaptureDraftIntent(context).rule
    }
    private func execute(_ arguments: [String], _ input: Data? = nil) async throws -> JSONValue {
        try Task.checkCancellation()
        guard !cancelled else { throw CancellationError() }
        let run = self.run
        let task = Task { try await run(arguments, input) }; call = task
        defer { call = nil }
        let value = try await task.value
        try Task.checkCancellation()
        guard !cancelled else { throw CancellationError() }
        return value
    }
    /// Pin the selected profile and version before any editable rule is prepared.
    func prepare() async {
        guard !working, baseRuleVersion == nil, !cancelled else { return }
        working = true; error = nil
        defer { working = false }
        do {
            if context.field?.kind == "json", CaptureDraftIntent.jsonValue(context.body, pointer: context.field?.pointer ?? "") == nil {
                throw RewriteFormError.invalid("The selected JSON pointer does not resolve in the captured body. Choose a valid field before creating a rule.")
            }
            let status = try await execute(["status"])
            guard let name = status["profile"]?.string, let version = status["configured_rule_version"]?.string else {
                throw RewriteFormError.invalid("The selected profile and rule version are unavailable. Reopen after refreshing Rewrite status.")
            }
            profile = name
            let rows = try await execute(["rules", "list", "--profile", name])
            orderedRules = rows["rules"]?.array.compactMap { $0["id"]?.string } ?? []
            let intent = CaptureDraftIntent(context)
            let value = try await execute(["capture", "draft", context.record.id, "-"], try intent.data())
            guard value["base_rule_version"]?.string == version else {
                throw RewriteFormError.invalid("Rules changed while opening the draft. Close and reopen to use the new version.")
            }
            let after = try await execute(["status"])
            guard after["profile"]?.string == name, after["configured_rule_version"]?.string == version else {
                throw RewriteFormError.invalid("The selected profile or rules changed while opening the draft. Close and reopen.")
            }
            let node = try Self.syntax(value)
            guard let candidate = node["rule"], let index = value["position"]?.int64 else { throw CLIError.invalidJSON("The generated draft is incomplete.") }
            var draft = RewriteRuleDraft(candidate)
            // JSONValue transports CLI results as Doubles. Keep the original
            // lossless operation input, including exact JSON numeric tokens.
            draft.operations = intent.rule.operations
            rule = draft; position = String(index); enable = false
            baseRuleVersion = version; fixture = node["fixture"]
            await loadFixture()
        } catch is CancellationError { }
        catch { self.error = error.localizedDescription }
    }
    /// HTTP previews run request + response in order whenever both originals
    /// exist. An incomplete original is never replaced with invented empty data.
    private func loadFixture() async {
        fixture = nil; previewResult = nil
        let stage = context.record.kind == "websocket" ? rule.stage : (context.record.snapshots["response_original"] == nil && rule.stage == "request" ? "request" : "exchange")
        do { fixture = try Self.syntax(await execute(["capture", "fixture", context.record.id, stage])) }
        catch is CancellationError { }
        catch { notice = "Original content is incomplete or unavailable. Offline preview is unavailable; metadata operations can still be saved." }
    }
    func document() throws -> Data {
        guard let version = baseRuleVersion, let index = Int(position), index >= 0 else { throw RewriteFormError.invalid("Enter a zero-based insertion position.") }
        let ruleNode = try RewriteJSON.parse(rule.json())
        var fields: [String: RewriteJSON] = ["base_rule_version": .string(version), "position": .scalar(String(index)), "rule": ruleNode, "enable": .scalar(enable ? "true" : "false")]
        if let fixture { fields["fixture"] = fixture }
        return RewriteJSON.object(fields).data
    }
    func preview() async {
        guard ready, let profile else { return }
        guard canPreview else { error = "Original content is incomplete. Preview requires complete original bytes."; return }
        working = true; error = nil; previewResult = nil
        defer { working = false }
        do { previewResult = try Self.syntax(await execute(["draft", "preview", "-", "--profile", profile], try document())) }
        catch is CancellationError { }
        catch { self.error = error.localizedDescription + " Draft retained; no automatic retry." }
    }
    func save() async {
        guard ready, let profile else { return }
        working = true; saving = true; error = nil
        defer { working = false; saving = false }
        do {
            _ = try await execute(["draft", "save", "-", "--profile", profile], try document())
            saved = true; notice = "Rule saved to \(profile). Global rewrite intent is unchanged."
        } catch { self.error = error.localizedDescription + " Draft retained, including its original version. No automatic retry." }
    }
    func cancel() { guard !saving else { return }; cancelled = true; call?.cancel(); previewResult = nil }
    static func syntax(_ value: JSONValue) throws -> RewriteJSON {
        switch value {
        case .objectValue(let fields): return .object(try fields.mapValues(syntax))
        case .arrayValue(let items): return .array(try items.map(syntax))
        case .stringValue(let text): return .string(text)
        case .numberValue(let number):
            guard number.isFinite else { throw CLIError.invalidJSON("A draft contains an invalid numeric field.") }
            return .scalar(Int64(exactly: number).map(String.init) ?? String(number))
        case .boolValue(let value): return .scalar(value ? "true" : "false")
        case .null: return .scalar("null")
        }
    }
    var beforeText: String { fixture.map(Self.fixtureText) ?? "Complete original content unavailable." }
    var afterText: String { previewResult.map(Self.fixtureText) ?? "Run Offline Preview to see the full ordered result." }
    var diffText: String { captureBodyText(Data(afterText.utf8), format: .diff, before: Data(beforeText.utf8)) }
    static func fixtureText(_ node: RewriteJSON) -> String {
        ["request", "response", "message"].compactMap { key -> String? in
            guard let part = node[key] else { return nil }
            var text = key.uppercased() + "\n" + part.text
            if let encoded = part["body_base64"]?.string, let bytes = Data(base64Encoded: encoded) {
                text += "\nBODY (\(bytes.count) bytes)\n" + (String(data: bytes, encoding: .utf8) ?? captureBodyText(bytes, format: .binary))
            }
            return text
        }.joined(separator: "\n\n")
    }
}
