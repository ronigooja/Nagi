import Foundation

@MainActor final class CaptureModel {
    typealias Run = ([String], Data?) async throws -> CaptureReply
    var onChange: (() -> Void)?
    var onStatusChange: (() -> Void)?
    var onCreateRule: ((CaptureSelectionContext) -> Void)?
    var draftRun: RequestRewriteModel.Run?
    var submit: Run
    var export: ([String]) async throws -> Void
    let read: Run
    private let discover: () async throws -> [CaptureApplication]
    let owner = "macos-" + UUID().uuidString
    private(set) var status: CaptureStatus?
    private var statusMessages = CLIMessages()
    var statusRecovery: String? {
        status?.recovery.map { statusMessages.display($0, at: "/data/status/recovery") }
    }
    private(set) var records: [CaptureRecord] = []
    private(set) var applications: [CaptureApplication] = []
    private(set) var selectedID: String?
    private(set) var selectedSnapshotName = "request_original"
    private(set) var selectedBody = Data()
    private(set) var bodyText = ""
    private(set) var error: String?
    private(set) var prerequisiteMessage: String?
    private(set) var prerequisitesReady = false
    private var pendingPrerequisiteCheck = false
    private var prerequisiteCheckInProgress = false
    private(set) var stale = true
    private(set) var operationInProgress = false
    private(set) var readingBody = false
    private(set) var format: CaptureBodyFormat = .raw
    var selectedRecord: CaptureRecord? { records.first { $0.id == selectedID } }
    var canOperate: Bool { !blocked && !suspended && !operationInProgress }
    var activeScope: CaptureScope? {
        guard let status, !status.canStartNewSession else { return nil }
        return status.scope
    }
    var canEditScope: Bool { canOperate && activeScope == nil }
    var ownsSession: Bool { status?.owner == owner }
    var isReading: Bool { readTask != nil }
    var isRecording: Bool { status?.state == "recording" }
    var checkingPrerequisites: Bool { pendingPrerequisiteCheck || prerequisiteCheckInProgress }
    private var visible = false, blocked = false, suspended = false
    private var generation: UInt64 = 0, detailGeneration: UInt64 = 0
    private var readTask: Task<Void, Never>?, detailTask: Task<Void, Never>?, timer: Task<Void, Never>?, applicationTask: Task<Void, Never>?
    private var pendingDetail = false
    private var pendingRead = false
    private var query = "", kind = "", domain = "", processPath = ""
    private var ascending = false
    private let interval: UInt64
    init(read: @escaping Run, submit: @escaping Run, export: @escaping ([String]) async throws -> Void,
         discover: @escaping () async throws -> [CaptureApplication] = { [] }, interval: UInt64 = 1_000_000_000) {
        self.read = read; self.submit = submit; self.export = export; self.discover = discover; self.interval = interval
    }
    convenience init(cli: NagiCLIClient = NagiCLIClient()) {
        self.init(read: { try await cli.capture($0, input: $1) }, submit: { try await cli.capture($0, input: $1) },
                  export: { try await cli.captureExport($0) }, discover: { try await cli.captureApps() })
        draftRun = { try await cli.rewrite($0, input: $1) }
    }
    func setVisible(_ value: Bool) {
        guard value != visible else { return }
        if value && !visible { pendingPrerequisiteCheck = true; prerequisiteMessage = L10n.text("app.ui.checking_capture_prerequisites"); prerequisitesReady = false }
        visible = value; updateActivity()
    }
    func refreshPrerequisites() {
        pendingPrerequisiteCheck = true; prerequisiteMessage = L10n.text("app.ui.checking_capture_prerequisites"); prerequisitesReady = false
        if readTask != nil { pendingRead = true } else { refresh() }
        notify()
    }
    func setSuspended(_ value: Bool) { suspended = value; updateActivity() }
    func setBlocked(_ value: Bool) { blocked = value; updateActivity() }
    private var active: Bool { visible && !blocked && !suspended && !operationInProgress }
    private func cancelReads() {
        if prerequisiteCheckInProgress { pendingPrerequisiteCheck = true }
        prerequisiteCheckInProgress = false
        generation &+= 1; detailGeneration &+= 1
        readTask?.cancel(); detailTask?.cancel(); applicationTask?.cancel(); timer?.cancel(); timer = nil
        pendingDetail = false
        stale = true; pendingRead = false
    }
    private func updateActivity() {
        if active {
            updatePolling()
            if readTask == nil { refresh() } else { pendingRead = true }
        } else { cancelReads() }
        notify()
    }
    private func updatePolling() {
        guard active && isRecording else { timer?.cancel(); timer = nil; return }
        guard timer == nil else { return }
        let interval = self.interval
        timer = Task { [weak self] in
            while !Task.isCancelled {
                do { try await Task.sleep(nanoseconds: interval) } catch { return }
                guard let self, self.active, self.isRecording else { return }
                // Slow reads skip ticks rather than accumulating refresh work.
                if self.readTask == nil { self.refresh() }
            }
        }
    }
    func waitForCancelledRead() async { await readTask?.value; await detailTask?.value; await applicationTask?.value }
    func setFilters(query: String, kind: String, domain: String, processPath: String, ascending: Bool) {
        self.query = query; self.kind = kind; self.domain = domain; self.processPath = processPath; self.ascending = ascending
        if prerequisiteCheckInProgress { pendingPrerequisiteCheck = true; prerequisiteCheckInProgress = false }
        generation &+= 1; readTask?.cancel(); pendingRead = active
        if readTask == nil { refresh() }
    }
    func refreshStatus() { beginRead(includeRecords: false) }
    func refresh() {
        guard active else { return }
        if readTask != nil { pendingRead = true } else { beginRead(includeRecords: true) }
    }
    private func beginRead(includeRecords: Bool) {
        guard !blocked, !suspended, !operationInProgress, readTask == nil else { return }
        let token = generation, read = self.read
        let checkPrerequisites = pendingPrerequisiteCheck
        pendingPrerequisiteCheck = false
        prerequisiteCheckInProgress = checkPrerequisites
        let query = self.query, kind = self.kind, domain = self.domain, processPath = self.processPath, ascending = self.ascending
        pendingRead = false
        readTask = Task { [weak self] in
            if checkPrerequisites {
                do {
                    let check = try await read(["check"], nil)
                    guard let self, !Task.isCancelled, token == self.generation else { self?.finishRead(); return }
                    self.prerequisiteMessage = check.readiness.map { check.display($0.message, at: "/data/readiness/message") } ?? L10n.text("app.ui.unable_to_check_capture_prerequisites_use_recheck")
                    self.prerequisitesReady = check.readiness?.ready == true
                } catch {
                    guard let self, !Task.isCancelled, token == self.generation else { self?.finishRead(); return }
                    self.prerequisiteMessage = L10n.text("app.ui.unable_to_check_capture_prerequisites_use_recheck")
                    self.prerequisitesReady = false
                }
                self?.prerequisiteCheckInProgress = false
                self?.notify()
            }
            do {
                let reply = try await read(["status"], nil)
                var records: [CaptureRecord]?
                if includeRecords, reply.status != nil {
                    var all: [CaptureRecord] = [], cursor = "", seenCursors: Set<String> = []
                    repeat {
                        let input: [String: Any] = ["query": query, "kind": kind, "domain": domain, "process_path": processPath, "cursor": cursor, "limit": 100]
                        let page = try await read(["list", "-"], try JSONSerialization.data(withJSONObject: input))
                        try Task.checkCancellation()
                        all += page.records ?? []; cursor = page.nextCursor ?? ""
                        guard all.count <= 1000, cursor.isEmpty || seenCursors.insert(cursor).inserted else { throw CLIError.invalidJSON(L10n.text("app.ui.capture_pagination_exceeded_its_bounds_refresh_to")) }
                    } while !cursor.isEmpty
                    records = await Task.detached(priority: .userInitiated) { all.sorted {
                        if $0.started == $1.started { return $0.id < $1.id }
                        return ascending ? $0.started < $1.started : $0.started > $1.started
                    } }.value
                }
                guard let self, !Task.isCancelled, token == self.generation else { self?.finishRead(); return }
                if self.status?.sessionID != reply.status?.sessionID { self.select(nil); self.records = [] }
                self.status = reply.status
                self.statusMessages = reply.messages
                if let records {
                    self.records = records
                    if let id = self.selectedID, !records.contains(where: { $0.id == id }) { self.select(nil) }
                    else if self.selectedID != nil { self.loadDetail() }
                }
                self.stale = false; self.error = nil; self.onStatusChange?()
            } catch {
                if let self, !Task.isCancelled, token == self.generation {
                    self.stale = true; self.error = L10n.format("app.ui.capture_unavailable", String(describing: error.localizedDescription)); self.onStatusChange?()
                }
            }
            self?.finishRead()
        }
        notify()
    }
    private func finishRead() {
        readTask = nil; let resume = pendingRead; pendingRead = false; updatePolling(); notify()
        if resume && active { refresh() }
    }
    func discoverApplications() {
        guard canOperate, applicationTask == nil else { return }
        applicationTask = Task { [weak self, discover] in
            defer { self?.applicationTask = nil }
            do { let apps = try await discover(); guard let self, self.active, !Task.isCancelled else { return }; self.applications = apps; self.notify() }
            catch { if !Task.isCancelled { self?.error = L10n.format("app.ui.application_list_unavailable", String(describing: error.localizedDescription)); self?.notify() } }
        }
    }
    func select(_ id: String?) {
        selectedID = id; selectedBody = Data(); bodyText = ""; detailGeneration &+= 1; detailTask?.cancel()
        if let record = selectedRecord, record.snapshots[selectedSnapshotName] == nil { selectedSnapshotName = record.snapshots.keys.sorted().first ?? "request_original" }
        loadDetail(); notify()
    }
    func selectSnapshot(_ name: String) { selectedSnapshotName = name; selectedBody = Data(); bodyText = ""; detailGeneration &+= 1; detailTask?.cancel(); loadDetail(); notify() }
    func setFormat(_ value: CaptureBodyFormat) { format = value; detailGeneration &+= 1; detailTask?.cancel(); loadDetail(); notify() }
    private func loadDetail() {
        guard active, let record = selectedRecord else { return }
        // Preserve the process slot until cancellation has drained all pipe readers.
        if detailTask != nil { pendingDetail = true; return }
        pendingDetail = false
        let token = detailGeneration, name = selectedSnapshotName, read = self.read, format = self.format
        readingBody = true
        detailTask = Task { [weak self] in
            do {
                let fresh = try await read(["show", record.id], nil).record ?? record
                let body = try await Self.readBody(read, id: record.id, snapshot: name)
                var before: Data?
                if format == .diff {
                    let original = name.replacingOccurrences(of: "_actual", with: "_original")
                    if fresh.snapshots[original] != nil { before = try await Self.readBody(read, id: record.id, snapshot: original) }
                }
                let text = await Task.detached(priority: .userInitiated) { captureBodyText(body, format: format, before: before) }.value
                guard let self, !Task.isCancelled, token == self.detailGeneration, self.selectedID == record.id, self.active else { self?.finishDetail(); return }
                if let index = self.records.firstIndex(where: { $0.id == record.id }) { self.records[index] = fresh }
                self.selectedBody = body; self.bodyText = text
            } catch {
                if let self, !Task.isCancelled, token == self.detailGeneration { self.error = L10n.format("app.ui.body_unavailable", String(describing: error.localizedDescription)) }
            }
            self?.finishDetail()
        }
    }
    private func finishDetail() {
        detailTask = nil; readingBody = false
        let resume = pendingDetail; pendingDetail = false
        if resume && active { loadDetail() }
        notify()
    }
    private static func readBody(_ read: Run, id: String, snapshot: String) async throws -> Data {
        var data = Data(), offset: Int64 = 0
        while true {
            try Task.checkCancellation()
            guard let chunk = try await read(["body", id, snapshot, String(offset), "262144"], nil).body else { throw CLIError.invalidJSON(L10n.text("app.ui.capture_body_response_is_missing")) }
            guard chunk.offset == offset, chunk.nextOffset >= offset, chunk.nextOffset - offset == chunk.data.count else { throw CLIError.invalidJSON(L10n.text("app.ui.capture_body_offsets_are_invalid")) }
            data.append(chunk.data)
            guard data.count <= 8 * 1_048_576 else { throw CLIError.outputTooLarge(8 * 1_048_576) }
            if chunk.eof || chunk.nextOffset == offset { return data }
            offset = chunk.nextOffset
        }
    }
    func start(scope: CaptureScope, bodyLimit: Int) async {
        let input: [String: Any] = ["owner": owner, "scope": ["domains": scope.domains, "process_paths": scope.processPaths, "app_paths": scope.appPaths], "body_limit": bodyLimit]
        _ = await perform(["start", "-"], input: try? JSONSerialization.data(withJSONObject: input))
    }
    func mutate(_ action: String) async { guard let id = status?.sessionID else { return }; _ = await perform([action, id]) }
    @discardableResult func perform(_ args: [String], input: Data? = nil) async -> Bool {
        guard canOperate else { return false }
        operationInProgress = true; cancelReads(); error = nil; notify()
        await waitForCancelledRead()
        do {
            let reply = try await submit(args, input)
            if reply.status != nil { status = reply.status; statusMessages = reply.messages }
            if args.first == "clear" || args.first == "import" || args.first == "start" { records = []; select(nil) }
            operationInProgress = false; updateActivity(); onStatusChange?(); return true
        } catch {
            self.error = error.localizedDescription + L10n.text("app.ui.refresh_before_retrying_if_the_result_is")
            operationInProgress = false; updateActivity(); return false
        }
    }
    func exportFile(_ file: String, format: String) async {
        guard canOperate else { return }
        operationInProgress = true; cancelReads(); await waitForCancelledRead(); notify()
        do { try await export([file, format]) }
        catch { self.error = error.localizedDescription }
        operationInProgress = false; updateActivity()
    }
    /// Lifecycle cleanup bypasses the UI gate, but only for the exact owned session.
    func stopOwnedSession(useGate: Bool = true) async {
        cancelReads(); await waitForCancelledRead()
        // A window close waits for the current settings mutation to release its
        // gate. Quit invokes the same ownership check after acquiring cleanup.
        while useGate && (blocked || operationInProgress) {
            if Task.isCancelled { return }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        guard ownsSession, let id = status?.sessionID, status?.state != "stopped" else { return }
        operationInProgress = true
        defer { operationInProgress = false; notify() }
        do {
            let reply = try await (useGate ? submit : read)(["stop", id], nil)
            if reply.status != nil { status = reply.status; statusMessages = reply.messages }
            onStatusChange?()
        }
        catch { self.error = L10n.format("app.ui.could_not_stop_recording", String(describing: error.localizedDescription)) }
        notify()
    }
    func closeWindow() { setVisible(false); Task { await stopOwnedSession() } }
    func createRule(field: CaptureFieldSelection? = nil) {
        guard let record = selectedRecord else { return }
        onCreateRule?(CaptureSelectionContext(record: record, snapshotName: selectedSnapshotName, body: selectedBody, field: field))
    }
    private func notify() { onChange?() }
}
