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
    private(set) var sortColumn: ConnectionSortColumn = .destination
    private(set) var sortAscending = true
    private(set) var operationInProgress = false
    private(set) var automaticRefreshPaused = false
    var isRefreshing: Bool { readTask != nil }
    var selectedConnection: Connection? { selectedID.flatMap { connectionsByID[$0] } }
    var canClose: Bool {
        visible && !suspended && !operationBlocked && !operationInProgress && engineState == .running && state == .fresh
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
    }
    /// Request injection allows deterministic lifecycle tests without an API,
    /// while integration tests use the real CLI Process cancellation path.
    init(fetch: @escaping () async throws -> [Connection], execute: @escaping ([String]) async throws -> Void,
         interval: UInt64 = 1_000_000_000, now: @escaping () -> Date = Date.init) {
        self.fetch = fetch; self.execute = execute; self.interval = interval; self.now = now
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
            connections = []; connectionsByID = [:]; rows = []; rowsRevision &+= 1
            if selectedID != nil { selectionDisappearMessage = "The selected connection is no longer active." }
            selectedID = nil; lastUpdated = nil; errorMessage = nil
            automaticRefreshPaused = false; state = .stopped
        } else if value != .running {
            cancelRead()
            state = connections.isEmpty ? .unavailable : .stale
            errorMessage = value == .starting ? "Waiting for the engine to start." : "Engine status is unavailable. Retry Start or check Nagi status."
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
        selectionDisappearMessage = nil
        notify()
    }

    /// An in-flight request is shared by manual and automatic refresh. Ticks
    /// are skipped; they never create a backlog of CLI invocations.
    func refresh() {
        guard active && !operationInProgress else { return }
        guard readTask == nil else { return }
        automaticRefreshPaused = false
        requestRead()
    }

    /// Quit suspends new requests first, then awaits the real CLI termination.
    func waitForCancelledRead() async { await readTask?.value }

    func close(id: String) {
        guard canClose, !id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              connectionsByID[id] != nil else { return }
        performClose(["connections", "close", id])
    }
    func closeAll() {
        guard canClose else { return }
        performClose(["connections", "close-all"])
    }

    private var active: Bool { visible && !suspended && !operationBlocked && engineState == .running }
    private func updateActivity() {
        if active {
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
                if !self.automaticRefreshPaused && !self.operationInProgress { self.requestRead() }
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
        let indexing = Task.detached(priority: .userInitiated) {
            var result: [String: Connection] = [:]
            for connection in snapshot {
                try Task.checkCancellation()
                result[connection.id] = connection
            }
            return result
        }
        let index = try await withTaskCancellationHandler {
            try await indexing.value
        } onCancel: { indexing.cancel() }
        // Query changes while a large snapshot is being prepared must not
        // publish rows for an old search or sorting choice.
        var preparedRows: [Connection] = []
        while !Task.isCancelled && readGeneration == generation && active {
            let query = self.query, column = sortColumn, ascending = sortAscending
            let projection = projectionGeneration
            preparedRows = try await sortedRows(snapshot, query: query, column: column, ascending: ascending)
            if projection == projectionGeneration { break }
        }
        guard !Task.isCancelled, readGeneration == generation, active else { return }
        projectionGeneration &+= 1; projectionTask?.cancel(); projectionTask = nil
        connections = snapshot
        connectionsByID = index
        rows = preparedRows; rowsRevision &+= 1
        if let selectedID, connectionsByID[selectedID] == nil {
            self.selectedID = nil
            selectionDisappearMessage = "The selected connection is no longer active."
        }
        lastUpdated = now(); errorMessage = nil; automaticRefreshPaused = false; state = .fresh
        notify()
    }
    private func failRead(_ error: Error) {
        if case CLIError.command("response_too_large", _) = error {
            state = .tooLarge; automaticRefreshPaused = true
            errorMessage = "The connection list is too large to load (mihomo response exceeded 4 MiB). Automatic refresh is paused; use Refresh to retry."
        } else if case CLIError.outputTooLarge(let limit) = error, limit == 8_388_608 {
            state = .tooLarge; automaticRefreshPaused = true
            errorMessage = "The connection list is too large to load (CLI output exceeded 8 MiB). Automatic refresh is paused; use Refresh to retry."
        } else {
            state = .stale
            errorMessage = "Connection list unavailable: \(error.localizedDescription). Displayed data is stale."
        }
    }
    private func projectRows() {
        projectionGeneration &+= 1
        let generation = projectionGeneration, snapshot = connections
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
                self?.operationMessage = "Close request accepted. Applications may reconnect automatically."
            } catch {
                self?.operationMessage = "Close request failed: \(error.localizedDescription). The request was not retried."
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
