import Foundation

/// Session-only drafts and snapshots. CLI owns persistence, trust, validation
/// and activation. A failed submit never clears or automatically retries a draft.
@MainActor final class RequestRewriteModel {
    typealias Run = ([String], Data?) async throws -> JSONValue
    typealias Export = ([String]) async throws -> Data
    var onChange: (() -> Void)?
    var onStatusChange: (() -> Void)?
    var submit: Run
    var export: Export
    var exportRead: Export
    private let read: Run
    private(set) var status: RewriteStatus?
    private(set) var domains: [RewriteDomain] = []
    private(set) var rules: [RewriteRuleSummary] = []
    private(set) var error: String?
    private(set) var notice: String?
    private(set) var stale = true
    private(set) var updated: Date?
    private(set) var operationInProgress = false
    private(set) var preview: String?
    private(set) var caStatus: JSONValue?
    var canOperate: Bool { !blocked && !suspended && !operationInProgress }
    var canEnable: Bool { canOperate && !stale && status?.intent != nil && status?.canEnable == true }
    var canManage: Bool { canOperate && !stale && listsFresh && status?.profile != nil }
    private(set) var listsFresh = false
    private(set) var sensitiveReadInProgress = false
    private var sensitiveReadTask: Task<Data, Error>?
    var isReading: Bool { readTask != nil }
    private var visible = false, blocked = false, suspended = false
    private var generation: UInt64 = 0
    private var readTask: Task<Void, Never>?
    private var timer: Task<Void, Never>?
    private var pendingWindowRead = false
    private let interval: UInt64
    init(read: @escaping Run, submit: @escaping Run, export: @escaping Export, interval: UInt64 = 5_000_000_000) {
        self.read = read; self.submit = submit; self.export = export; self.exportRead = export; self.interval = interval
    }
    convenience init(cli: NagiCLIClient = NagiCLIClient()) {
        self.init(read: { try await cli.rewrite($0, input: $1) }, submit: { try await cli.rewrite($0, input: $1) }, export: { try await cli.rewriteExport($0) })
    }
    func setVisible(_ value: Bool) {
        guard visible != value else { return }
        visible = value
        if value { updateActivity() } else { pauseReads() }
        notify()
    }
    func setSuspended(_ value: Bool) { suspended = value; updateActivity(); notify() }
    func setBlocked(_ value: Bool) { blocked = value; updateActivity(); notify() }
    private func updateActivity() {
        if blocked || suspended { pauseReads(); return }
        if visible {
            if timer == nil {
                timer = Task { [weak self] in
                    while !Task.isCancelled {
                        guard let self else { return }
                        do { try await Task.sleep(nanoseconds: self.interval) } catch { return }
                        self.refresh()
                    }
                }
            }
            if readTask == nil { refresh() } else { pendingWindowRead = true }
        }
    }
    private func pauseReads() {
        generation &+= 1; readTask?.cancel(); sensitiveReadTask?.cancel(); timer?.cancel(); timer = nil
        pendingWindowRead = false; stale = true; listsFresh = false
    }
    func waitForCancelledRead() async {
        await readTask?.value
        _ = try? await sensitiveReadTask?.value
    }
    /// Menu refreshes share the same read slot; window reads include lists.
    func refreshStatus() { beginRead(includeLists: false) }
    func refresh() { guard visible else { return }; beginRead(includeLists: true) }
    private func beginRead(includeLists: Bool) {
        guard !blocked, !suspended, !operationInProgress, !sensitiveReadInProgress, readTask == nil else { return }
        let token = generation, read = self.read
        pendingWindowRead = false
        readTask = Task { [weak self] in
            do {
                let current = RewriteStatus(value: try await read(["status"], nil))
                var domainRows: [RewriteDomain]?, ruleRows: [RewriteRuleSummary]?
                if includeLists, let profile = current.profile {
                    let d = try await read(["domains", "list", "--profile", profile], nil)
                    let r = try await read(["rules", "list", "--profile", profile], nil)
                    guard case .arrayValue(let ds)? = d["domains"], case .arrayValue(let rs)? = r["rules"] else {
                        throw CLIError.invalidJSON("Request rewrite lists are unavailable.")
                    }
                    domainRows = ds.compactMap(RewriteDomain.init)
                    ruleRows = rs.compactMap(RewriteRuleSummary.init)
                    guard domainRows?.count == ds.count, ruleRows?.count == rs.count else {
                        throw CLIError.invalidJSON("Request rewrite lists contain unsupported entries.")
                    }
                }
                guard let self, token == self.generation, !Task.isCancelled else { self?.finishRead(); return }
                if self.status?.profile != current.profile { self.domains = []; self.rules = []; self.preview = nil; self.listsFresh = false }
                self.status = current
                self.caStatus = current.value["ca"]
                if let domainRows, let ruleRows { self.domains = domainRows; self.rules = ruleRows; self.listsFresh = true }
                self.stale = false; self.updated = Date(); self.error = nil
                self.onStatusChange?()
            } catch {
                if let self, token == self.generation, !Task.isCancelled {
                    self.stale = true; self.listsFresh = false
                    self.error = "Request Rewrite unavailable: \(error.localizedDescription) Upgrade the matching Nagi CLI and engine if this command or capability is unavailable."
                    self.onStatusChange?()
                }
            }
            self?.finishRead()
        }
        notify()
    }
    private func finishRead() {
        readTask = nil
        let resume = pendingWindowRead; pendingWindowRead = false
        notify()
        if resume && visible && !blocked && !suspended { refresh() }
    }
    /// The App settings gate executes once. Hiding/closing does not cancel a
    /// mutation already submitted, and never implies rollback.
    func perform(_ arguments: [String], input: Data? = nil, profile: String? = nil) async -> Bool {
        guard canOperate else { error = "Another operation is in progress."; notify(); return false }
        if ["domains", "rules"].contains(arguments.first ?? ""), !canManage {
            error = "Refresh the domain and rule lists before changing saved entries."; notify(); return false
        }
        operationInProgress = true; error = nil; notice = nil
        generation &+= 1; readTask?.cancel(); sensitiveReadTask?.cancel()
        let pending = readTask
        notify()
        await pending?.value
        _ = try? await sensitiveReadTask?.value
        do {
            let args = arguments + (profile.map { ["--profile", $0] } ?? [])
            let result = try await submit(args, input)
            if arguments == ["ca", "status"] { caStatus = result }
            if arguments.first == "preview" {
                // Only an explicitly requested preview reveals sensitive values.
                preview = Self.display(result)
                notice = "Sensitive local preview only. No network request was sent."
            } else { notice = "Operation completed. Refresh status to confirm current activation." }
            operationInProgress = false
            if visible { refresh() } else { refreshStatus() }
            notify(); return true
        } catch {
            self.error = error.localizedDescription + " The operation was not retried. Refresh status before resubmitting if runtime state is uncertain."
            operationInProgress = false; stale = true
            notify(); return false
        }
    }
    func clearPreview() { preview = nil; notify() }
    func sensitiveRule(id: String, profile: String) async throws -> RewriteRuleDraft {
        guard canManage, !sensitiveReadInProgress else { throw RewriteFormError.invalid("Refresh the list or wait for the current operation.") }
        generation &+= 1; readTask?.cancel()
        await readTask?.value
        guard canManage, !sensitiveReadInProgress else { throw CancellationError() }
        sensitiveReadInProgress = true
        let fetch = exportRead
        let task = Task { try await fetch(["export", "-", "--profile", profile]) }
        sensitiveReadTask = task
        notify()
        defer {
            sensitiveReadTask = nil; sensitiveReadInProgress = false; notify()
            if visible { refresh() }
        }
        let bytes = try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
        try Task.checkCancellation()
        guard !suspended, !blocked, visible else { throw CancellationError() }
        let document = try RewriteJSON.parse(bytes)
        guard let rule = document["rules"]?.array.first(where: { $0["id"]?.string == id }) else { throw RewriteFormError.invalid("The selected rule no longer exists. Refresh the list.") }
        return RewriteRuleDraft(rule)
    }
    func exportFile(_ arguments: [String]) async throws {
        guard canOperate else { throw RewriteFormError.invalid("Another operation is in progress.") }
        operationInProgress = true
        generation &+= 1; readTask?.cancel(); sensitiveReadTask?.cancel()
        await waitForCancelledRead()
        notify()
        defer { operationInProgress = false; notify(); if visible { refresh() } }
        _ = try await export(arguments)
        notice = "Export completed. Keep the exported file private."
    }
    static func display(_ value: JSONValue) -> String {
        switch value {
        case .objectValue(let fields): return "{" + fields.keys.sorted().map { RewriteJSON.string($0).text + ": " + display(fields[$0]!) }.joined(separator: ",\n") + "}"
        case .arrayValue(let items): return "[" + items.map(display).joined(separator: ", ") + "]"
        case .stringValue(let text): return RewriteJSON.string(text).text
        case .numberValue(let number): return String(number)
        case .boolValue(let bool): return bool ? "true" : "false"
        case .null: return "null"
        }
    }
    private func notify() { onChange?() }
}
