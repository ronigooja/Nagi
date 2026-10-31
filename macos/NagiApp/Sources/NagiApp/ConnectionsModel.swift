import Foundation

enum ConnectionsState: Equatable {
    case idle, loading, fresh, stale, stopped, unavailable, tooLarge
}

/// Independent of AppModel's menu/settings polling. This model owns only CLI
/// requests and current snapshots, never mihomo networking or configuration.
@MainActor final class ConnectionsModel {
    var onChange: (() -> Void)?
    private(set) var connections: [Connection] = []
    private(set) var rows: [Connection] = []
    private(set) var rowsRevision: UInt64 = 0
    private(set) var state: ConnectionsState = .idle
    private(set) var lastUpdated: Date?
    private(set) var errorMessage: String?
    private(set) var operationMessage: String?
    private(set) var selectionDisappearMessage: String?
    private(set) var selectedID: String?
    private(set) var query = ""
    private(set) var sortColumn: ConnectionSortColumn = .appearance
    private(set) var sortAscending = true
    private(set) var operationInProgress = false
    private(set) var automaticRefreshPaused = false
    private(set) var frozen = false
    private(set) var filter: ConnectionFilter = .all
    private var records = ConnectionRecords()
    private var selectedDetail: Connection?
    private var latestSelectedDetail: Connection?
    private var publishTask: Task<Void, Never>?
    private var latestSampledAt: Date?
    private var lastPublished = Date.distantPast
    private var retryAttempt = 0
    private var retryAfter = Date.distantPast
    private var stream: ((@escaping (ConnectionWatchEnvelope) async -> Void) async throws -> Void)?
    var activeCount: Int { connections.filter { !$0.ended && !$0.failed }.count }
    var endedCount: Int { connections.filter(\.ended).count }
    var failedCount: Int { connections.filter(\.failed).count }
    var isRefreshing: Bool { readTask != nil }
    var selectedConnection: Connection? { selectedDetail }
    var canClose: Bool {
        !frozen && visible && !suspended && !operationBlocked && !operationInProgress && engineState == .running && state == .fresh
    }

    private let fetch: () async throws -> [Connection]
    private let execute: ([String]) async throws -> Void
    private let interval: UInt64
    private let now: () -> Date
    private var connectionsByID: [String: Connection] = [:]
    private var visible = false
    private var suspended = false
    private var operationBlocked = false
    private var engineState: EngineState = .starting
    private var generation: UInt64 = 0
    private var readID: UInt64 = 0
    private var projectionGeneration: UInt64 = 0
    private var immediateRefreshPending = false
    private var timerTask: Task<Void, Never>?
    private var readTask: Task<Void, Never>?
    private var projectionTask: Task<Void, Never>?
    private var operationTask: Task<Void, Never>?

    convenience init(cli: NagiCLIClient = NagiCLIClient()) {
        self.init(fetch: { try await cli.connections() }, execute: { _ = try await cli.run($0) })
        self.stream = { receive in try await cli.watchConnections(receive) }
    }
    /// Request injection allows deterministic lifecycle tests without an API,
    /// while integration tests use the real CLI Process cancellation path.
    init(fetch: @escaping () async throws -> [Connection], execute: @escaping ([String]) async throws -> Void,
         interval: UInt64 = 250_000_000, watch: ((@escaping (ConnectionWatchEnvelope) async -> Void) async throws -> Void)? = nil, now: @escaping () -> Date = Date.init) {
        self.fetch = fetch; self.execute = execute; self.interval = interval; self.now = now; self.stream = watch
    }
    init(fetch: @escaping () async throws -> [Connection], execute: @escaping ([String]) async throws -> Void,
         interval: UInt64 = 250_000_000, watch: @escaping (@escaping ([Connection]) async -> Void) async throws -> Void, now: @escaping () -> Date = Date.init) {
        self.fetch = fetch; self.execute = execute; self.interval = interval; self.now = now
        self.stream = { receive in try await watch { snapshot in await receive(ConnectionWatchEnvelope(connections: snapshot, failures: [], gaps: [])) } }
    }

    func setVisible(_ value: Bool) {
        guard visible != value else { return }
        visible = value
        updateActivity()
        notify()
    }
    func setSuspended(_ value: Bool) {
        guard suspended != value else { return }
        suspended = value
        updateActivity()
        notify()
    }
    func setOperationBlocked(_ value: Bool) {
        guard operationBlocked != value else { return }
        operationBlocked = value
        if value && state == .fresh { state = .stale }
        updateActivity()
        notify()
    }
    func setEngineState(_ value: EngineState) {
        guard engineState != value else { return }
        engineState = value
        if value == .stopped {
            cancelRead()
            projectionGeneration &+= 1; projectionTask?.cancel(); projectionTask = nil
            records = ConnectionRecords(); selectedDetail = nil; latestSelectedDetail = nil; frozen = false
            publishTask?.cancel(); publishTask = nil
            connections = []; connectionsByID = [:]; rows = []; rowsRevision &+= 1
            if selectedID != nil { selectionDisappearMessage = L10n.text("app.ui.the_selected_connection_is_no_longer_active") }
            selectedID = nil; lastUpdated = nil; latestSampledAt = nil; errorMessage = nil
            automaticRefreshPaused = false; state = .stopped
        } else if value != .running {
            cancelRead()
            state = connections.isEmpty ? .unavailable : .stale
            errorMessage = value == .starting ? L10n.text("app.ui.waiting_for_the_engine_to_start") : L10n.text("app.ui.engine_status_is_unavailable_retry_start_or")
        } else {
            if state == .stopped || state == .unavailable || state == .idle {
                state = .idle; errorMessage = nil
            }
        }
        updateActivity()
        notify()
    }

    func setQuery(_ value: String) {
        guard query != value else { return }
        query = value; projectRows()
    }
    func setSort(column: ConnectionSortColumn, ascending: Bool) {
        guard sortColumn != column || sortAscending != ascending else { return }
        sortColumn = column; sortAscending = ascending; projectRows()
    }
    func select(id: String?) {
        selectedID = id.flatMap { connectionsByID[$0] == nil ? nil : $0 }
        selectedDetail = selectedID.flatMap { connectionsByID[$0] }
        latestSelectedDetail = selectedID.flatMap { records.active[$0] ?? records.ended[$0]?.connection ?? selectedDetail }
        selectionDisappearMessage = nil
        notify()
    }

    /// An in-flight request is shared by manual and automatic refresh. Ticks
    /// are skipped; they never create a backlog of CLI invocations.
    func refresh() {
        guard active && !operationInProgress && !frozen else { return }
        automaticRefreshPaused = false; retryAttempt = 0; retryAfter = .distantPast
        if readTask != nil {
            guard stream != nil else { return }
            immediateRefreshPending = true; cancelRead(); return
        }
        requestRead()
    }

    /// Quit suspends new requests first, then awaits the real CLI termination.
    func waitForCancelledRead() async { await readTask?.value }

    func close(id: String) {
        guard canClose, !id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              connectionsByID[id]?.ended == false, records.active[id] != nil else { return }
        performClose(["connections", "close", id])
    }
    func closeAll() {
        guard canClose else { return }
        performClose(["connections", "close-all"])
    }

    private var active: Bool { visible && !suspended && !operationBlocked && engineState == .running }
    private func updateActivity() {
        if active {
            if !frozen && latestSampledAt != nil { publishLatest() }
            startTimer()
            if !automaticRefreshPaused && !operationInProgress {
                // A cancelled process still owns the request slot until it has
                // exited. Resume immediately once that particular task ends.
                if readTask == nil { requestRead() }
                else { immediateRefreshPending = true }
            }
        } else {
            if state == .fresh { state = .stale }
            timerTask?.cancel(); timerTask = nil
            immediateRefreshPending = false
            cancelRead()
        }
    }
    private func startTimer() {
        guard timerTask == nil else { return }
        let interval = self.interval
        timerTask = Task { [weak self] in
            while !Task.isCancelled {
                do { try await Task.sleep(nanoseconds: interval) } catch { return }
                guard let self, self.active else { return }
                if !self.frozen && self.records.ended.values.contains(where: { self.now().timeIntervalSince($0.disappeared) >= 300 }) {
                    self.schedulePublish()
                }
                if !self.automaticRefreshPaused && !self.operationInProgress && self.now() >= self.retryAfter { self.requestRead() }
            }
        }
    }
    private func requestRead() {
        guard active, !operationInProgress, readTask == nil else { return }
        immediateRefreshPending = false
        readID &+= 1
        let id = readID, generation = self.generation, fetch = self.fetch
        if lastUpdated == nil { state = .loading }
        readTask = Task { [weak self] in
            do {
                if let stream = self?.stream {
                    try await stream { [weak self] snapshot in
                        guard let self, !Task.isCancelled, generation == self.generation, self.active else { return }
                         try? await self.accept(snapshot, readGeneration: generation)
                    }
                    throw CLIError.invalidJSON(L10n.text("app.connections.stream_ended"))
                }
                let connections = try await fetch()
                guard let self, !Task.isCancelled, generation == self.generation, self.active else {
                    self?.finishRead(id: id); return
                }
                try await self.accept(connections, readGeneration: generation)
            } catch {
                if let self, !Task.isCancelled, generation == self.generation, self.active {
                    self.failRead(error)
                }
            }
            self?.finishRead(id: id)
        }
        notify()
    }
    private func cancelRead() {
        generation &+= 1
        records.interrupt()
        selectedDetail?.observationInterrupted = true
        latestSelectedDetail?.observationInterrupted = true
        publishTask?.cancel(); publishTask = nil
        if frozen { frozen = false; publishLatest() }
        readTask?.cancel()
        // Do not clear readTask here: cancellation signals ProcessHolder but
        // does not mean the child or pipe readers have finished yet.
    }
    private func finishRead(id: UInt64) {
        guard id == readID else { return }
        readTask = nil
        let resume = immediateRefreshPending
        immediateRefreshPending = false
        if resume && active && !automaticRefreshPaused && !operationInProgress { requestRead() }
        else { notify() }
    }
    private func accept(_ snapshot: [Connection], readGeneration: UInt64) async throws {
        try await accept(ConnectionWatchEnvelope(connections: snapshot, failures: [], gaps: []), readGeneration: readGeneration)
    }
    private func accept(_ envelope: ConnectionWatchEnvelope, readGeneration: UInt64) async throws {
        let current = records, date = now()
        let merging = Task.detached(priority: .userInitiated) {
            var result = current
            result.merge(envelope.connections, failures: envelope.failures, gaps: envelope.gaps, at: date)
            return result
        }
        let merged = await merging.value
        guard !Task.isCancelled, readGeneration == generation, active else { return }
        records = merged
        retryAttempt = 0; retryAfter = .distantPast
        latestSampledAt = date
        if let selectedID {
            if let value = merged.active[selectedID] ?? merged.ended[selectedID]?.connection { latestSelectedDetail = value }
            else { latestSelectedDetail?.ended = true }
        }
        errorMessage = nil; automaticRefreshPaused = false; state = .fresh
        schedulePublish()
    }
    func setFilter(_ value: ConnectionFilter) { filter = value; projectRows() }
    func toggleFreeze() {
        guard active else { return }
        if frozen { frozen = false; publishLatest() }
        else {
            publishTask?.cancel(); publishTask = nil
            publishLatest(); frozen = true; notify()
        }
    }
    private func schedulePublish() {
        guard !frozen, publishTask == nil else { return }
        let delay = max(0, 0.5 - now().timeIntervalSince(lastPublished))
        if delay == 0 { publishLatest(); return }
        publishTask = Task { [weak self] in
            do { try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000)) } catch { return }
            guard let self else { return }
            self.publishTask = nil
            if !self.frozen { self.publishLatest() }
        }
    }
    private func publishLatest() {
        records.prune(at: now())
        lastUpdated = latestSampledAt
        connections = records.values
        connectionsByID = Dictionary(uniqueKeysWithValues: connections.map { ($0.id, $0) })
        if let selectedID {
            selectedDetail = connectionsByID[selectedID] ?? latestSelectedDetail ?? selectedDetail
        }
        lastPublished = now()
        projectRows()
    }
    private func failRead(_ error: Error) {
        records.interrupt(); latestSelectedDetail?.observationInterrupted = true
        if !frozen { selectedDetail?.observationInterrupted = true }
        if (error as? CLIError)?.commandCode == "response_too_large" {
            state = .tooLarge; automaticRefreshPaused = true
            errorMessage = L10n.text("app.ui.the_connection_list_is_too_large_to")
        } else if case CLIError.outputTooLarge(let limit) = error, limit == 8_388_608 {
            state = .tooLarge; automaticRefreshPaused = true
            errorMessage = L10n.text("app.ui.the_connection_list_is_too_large_to_2")
        } else {
            let delays: [TimeInterval] = [1, 2, 4, 8, 10]
            retryAfter = now().addingTimeInterval(delays[min(retryAttempt, delays.count - 1)])
            retryAttempt += 1
            state = .stale
            errorMessage = L10n.format("app.ui.connection_list_unavailable_displayed_data_is_stale", String(describing: error.localizedDescription))
        }
    }
    private func projectRows() {
        projectionGeneration &+= 1
        let generation = projectionGeneration, snapshot = connections.filter {
            switch filter {
            case .all: return true
            case .active: return !$0.ended && !$0.failed
            case .ended: return $0.ended
            case .failed: return $0.failed
            }
        }
        let query = self.query, column = sortColumn, ascending = sortAscending
        projectionTask?.cancel()
        projectionTask = Task { [weak self] in
            guard let self else { return }
            let rows: [Connection]
            do { rows = try await self.sortedRows(snapshot, query: query, column: column, ascending: ascending) }
            catch { return }
            guard !Task.isCancelled, generation == self.projectionGeneration else { return }
            self.rows = rows; self.rowsRevision &+= 1; self.projectionTask = nil
            self.notify()
        }
    }
    private func sortedRows(_ snapshot: [Connection], query: String, column: ConnectionSortColumn, ascending: Bool) async throws -> [Connection] {
        let sorting = Task.detached(priority: .userInitiated) {
            try connectionRows(snapshot, query: query, column: column, ascending: ascending)
        }
        return try await withTaskCancellationHandler {
            try await sorting.value
        } onCancel: { sorting.cancel() }
    }
    private func performClose(_ arguments: [String]) {
        operationInProgress = true; operationMessage = nil
        immediateRefreshPending = false
        let pendingRead = readTask
        cancelRead()
        let execute = self.execute
        operationTask = Task { [weak self] in
            // Wait for termination and draining of the cancelled CLI read, not
            // just its cancellation flag, before submitting a DELETE command.
            await pendingRead?.value
            do {
                try await execute(arguments)
                self?.operationMessage = L10n.text("app.ui.close_request_accepted_applications_may_reconnect_automatically")
            } catch {
                self?.operationMessage = L10n.format("app.ui.close_request_failed_the_request_was_not", String(describing: error.localizedDescription))
            }
            guard let self else { return }
            self.operationInProgress = false; self.operationTask = nil
            // Only another successful snapshot can enable a new close action.
            if self.engineState == .running { self.state = .stale }
            self.notify()
            if self.active { self.refresh() }
        }
        notify()
    }
    private func notify() { onChange?() }
}
