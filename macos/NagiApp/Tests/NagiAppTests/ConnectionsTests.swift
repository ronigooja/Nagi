import Foundation
import XCTest
@testable import NagiApp

final class ConnectionsTests: XCTestCase {
    private func decode(_ json: String) throws -> [Connection] {
        try JSONDecoder().decode(ConnectionList.self, from: Data(json.utf8)).connections
    }

    func testConnectionDecodingMissingFieldsIPv6StringPortsAndExactCounters() throws {
        let records = try decode("""
        {"connections":[{"id":"missing"},{"id":"full","metadata":{"host":"example.org","destinationIP":"2001:db8::1","destinationPort":"443","sourceIP":"::1","sourcePort":"1234","network":"tcp","type":"Mixed","process":"Browser","processPath":"/Applications/Browser.app/Browser"},"chains":["Node A","Proxy"],"rule":"DomainSuffix","rulePayload":"example.org","upload":9223372036854775807,"download":0}]}
        """)
        let empty = records[0]
        for field in [empty.destination, empty.destinationIP, empty.source, empty.process,
                      empty.processPath, empty.network, empty.inbound, empty.chain,
                      empty.rule, empty.rulePayload] {
            XCTAssertEqual(field, "unknown")
        }
        XCTAssertNil(empty.upload)
        XCTAssertNil(empty.download)
        let full = records[1]
        XCTAssertEqual(full.destination, "example.org:443")
        XCTAssertEqual(full.destinationIP, "[2001:db8::1]:443")
        XCTAssertEqual(full.source, "[::1]:1234")
        XCTAssertEqual(full.network, "TCP")
        XCTAssertEqual(full.upload, Int64.max)
        XCTAssertEqual(full.download, 0)
        XCTAssertEqual(full.chain, "Node A → Proxy")
        XCTAssertEqual(try decode("{\"connections\":[]}"), [])
        XCTAssertNil(JSONValue.numberValue(Double(Int64.max)).int64)
    }

    func testTechnicalPlaceholdersAndActualNamesRemainDistinct() throws {
        let records = try decode("""
        {"connections":[
          {"id":"empty","metadata":{"process":"","processPath":"","host":""}},
          {"id":"named","metadata":{"process":"Unknown","processPath":"/opt/未知/none"},"chains":["DIRECT","running"],"rule":"MATCH","rulePayload":"none"}
        ]}
        """)
        XCTAssertEqual(records[0].process, "unknown")
        XCTAssertEqual(records[0].processPath, "unknown")
        XCTAssertEqual(records[0].destination, "unknown")
        XCTAssertEqual(records[1].process, "Unknown")
        XCTAssertEqual(records[1].processPath, "/opt/未知/none")
        XCTAssertEqual(records[1].chain, "DIRECT → running")
        XCTAssertEqual(records[1].rulePayload, "none")
        XCTAssertTrue(records[0].matches("unknown"))
        XCTAssertTrue(records[1].matches("/opt/未知"))
    }

    func testSearchAndSortingUseStableIDsAndNumericCounters() throws {
        let records = try decode("""
        {"connections":[
          {"id":"z","metadata":{"host":"same.example","destinationPort":80,"process":"Browser","destinationIP":"203.0.113.2"},"chains":["Tokyo"],"upload":2,"download":10},
          {"id":"a","metadata":{"host":"same.example","destinationPort":"80","processPath":"/opt/bin/browser"},"upload":10,"download":2},
          {"id":"b","metadata":{"host":"other.example"},"upload":1}
        ]}
        """)
        func rows(_ query: String = "", column: ConnectionSortColumn = .destination, ascending: Bool = true) throws -> [String] {
            try connectionRows(records, query: query, column: column, ascending: ascending).map(\.id)
        }
        XCTAssertEqual(try rows(), ["b", "a", "z"])
        XCTAssertEqual(try rows(column: .destination, ascending: false), ["a", "z", "b"])
        XCTAssertEqual(try rows(column: .upload), ["b", "z", "a"])
        XCTAssertEqual(try rows("SAME.EXAMPLE"), ["a", "z"])
        XCTAssertEqual(try rows("203.0.113.2"), ["z"])
        XCTAssertEqual(try rows("Tokyo"), ["z"])
        XCTAssertEqual(try rows("/opt/bin"), ["a"])
        XCTAssertEqual(try rows("z"), ["z"])
    }

    private func fixture(_ body: String) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-connections-\(UUID().uuidString).py")
        try ("#!/usr/bin/env python3\n" + body + "\n").write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
        return url
    }

    func testOnlyConnectionStdoutHasEightMiBLimitAndLargeTypedListsLoad() async throws {
        let cli = try fixture("""
        import json,sys
        value = {"ok":True,"data":{"connections":[{"id":"large","metadata":{"processPath":"x"*1500000},"upload":9223372036854775807}]}}
        print(json.dumps(value))
        """)
        defer { try? FileManager.default.removeItem(at: cli) }
        let client = NagiCLIClient(executable: cli)
        let records = try await client.connections()
        XCTAssertEqual(records.count, 1)
        XCTAssertEqual(records[0].processPath.count, 1_500_000)
        XCTAssertEqual(records[0].upload, Int64.max)
        do {
            _ = try await client.run(["status"])
            XCTFail("ordinary command output limit was raised")
        } catch CLIError.invalidJSON(let message) { XCTAssertEqual(message, "CLI output exceeded 1 MiB.") }
    }

    func testConnectionOutputBoundaryAndStderrLimit() async throws {
        for (size, accepted) in [(8_388_608, true), (8_388_609, false)] {
            let cli = try fixture("""
            import json,sys
            body = json.dumps({"ok":True,"data":{"connections":[]}})
            sys.stdout.write(body+' '*(\(size)-len(body)))
            """)
            defer { try? FileManager.default.removeItem(at: cli) }
            do {
                let records = try await NagiCLIClient(executable: cli).connections()
                XCTAssertTrue(accepted)
                XCTAssertEqual(records, [])
            } catch CLIError.outputTooLarge(let limit) {
                XCTAssertFalse(accepted)
                XCTAssertEqual(limit, 8_388_608)
            }
        }
        let cli = try fixture("""
        import sys
        sys.stdout.write('{"ok":true,"data":{"connections":[]}}')
        sys.stderr.write('x'*1048577)
        """)
        defer { try? FileManager.default.removeItem(at: cli) }
        do {
            _ = try await NagiCLIClient(executable: cli).connections()
            XCTFail("connection stderr limit was raised")
        } catch CLIError.invalidJSON(let message) { XCTAssertTrue(message.contains("stderr exceeded 1 MiB")) }
    }
}

@MainActor private final class ConnectionRequests {
    private(set) var readCount = 0
    private(set) var commands: [[String]] = []
    private var reads: [Int: CheckedContinuation<[Connection], Error>] = [:]
    private var closeContinuation: CheckedContinuation<Void, Error>?
    func read() async throws -> [Connection] {
        // Match the CLI's pre-launch cancellation check. A cancelled task that
        // has not started fetching must not create an unresumable fixture read.
        try Task.checkCancellation()
        readCount += 1
        let id = readCount
        return try await withCheckedThrowingContinuation { reads[id] = $0 }
    }
    func execute(_ arguments: [String]) async throws {
        commands.append(arguments)
        try await withCheckedThrowingContinuation { closeContinuation = $0 }
    }
    func finishRead(_ id: Int, _ value: [Connection]) { reads.removeValue(forKey: id)?.resume(returning: value) }
    func failRead(_ id: Int, _ error: Error) { reads.removeValue(forKey: id)?.resume(throwing: error) }
    func finishClose(_ error: Error? = nil) {
        let pending = closeContinuation; closeContinuation = nil
        if let error { pending?.resume(throwing: error) }
        else { pending?.resume(returning: ()) }
    }
}

@MainActor extension ConnectionsTests {
    private func waitUntil(_ condition: @escaping () -> Bool, file: StaticString = #filePath, line: UInt = #line) async {
        for _ in 0..<600 {
            if condition() { return }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertTrue(condition(), file: file, line: line)
    }
    private func snapshot(_ ids: [String]) throws -> [Connection] {
        let objects = ids.map { ["id": $0, "metadata": ["host": "\($0).example"]] as [String: Any] }
        return try JSONDecoder().decode(ConnectionList.self, from: JSONSerialization.data(withJSONObject: ["connections": objects])).connections
    }
    private func model(_ requests: ConnectionRequests, interval: UInt64 = 10_000_000_000) -> ConnectionsModel {
        ConnectionsModel(fetch: { try await requests.read() }, execute: { try await requests.execute($0) }, interval: interval)
    }
    private func activate(_ model: ConnectionsModel) { model.setEngineState(.running); model.setVisible(true) }
    private func settle(_ model: ConnectionsModel, requests: ConnectionRequests, records: [Connection]) async {
        await waitUntil { requests.readCount >= 1 }
        requests.finishRead(requests.readCount, records)
        await waitUntil { model.state == .fresh && !model.isRefreshing }
    }

    func testSlowRequestsDoNotOverlapAndCancelledOldResponseCannotReplaceNewState() async throws {
        let requests = ConnectionRequests(), model = model(requests, interval: 20_000_000)
        activate(model)
        await waitUntil { requests.readCount == 1 }
        model.refresh(); model.refresh()
        try await Task.sleep(nanoseconds: 80_000_000)
        XCTAssertEqual(requests.readCount, 1)
        model.setVisible(false); model.setVisible(true)
        try await Task.sleep(nanoseconds: 40_000_000)
        XCTAssertEqual(requests.readCount, 1, "cancelled read must retain its slot until the process finishes")
        requests.finishRead(1, try snapshot(["old"]))
        await waitUntil { requests.readCount == 2 }
        XCTAssertEqual(model.connections, [])
        requests.finishRead(2, try snapshot(["new"]))
        await waitUntil { model.rows.map(\.id) == ["new"] }
        model.setVisible(false)
        // A timer can begin a third request between publication and this call.
        if model.isRefreshing { requests.finishRead(requests.readCount, []) }
        await model.waitForCancelledRead()
    }

    func testSelectionFollowsIDThroughSortFilterAndDisappearance() async throws {
        let requests = ConnectionRequests(), model = model(requests)
        activate(model)
        await settle(model, requests: requests, records: try snapshot(["z", "a"]))
        model.select(id: "z")
        model.setSort(column: .destination, ascending: false)
        await waitUntil { model.rows.map(\.id) == ["z", "a"] }
        XCTAssertEqual(model.selectedConnection?.id, "z")
        model.setQuery("a.example")
        await waitUntil { model.rows.map(\.id) == ["a"] }
        XCTAssertEqual(model.selectedConnection?.id, "z", "filtering does not select a different connection")
        model.refresh()
        await waitUntil { requests.readCount == 2 }
        requests.finishRead(2, try snapshot(["a"]))
        await waitUntil { model.selectedConnection?.ended == true && !model.isRefreshing }
        XCTAssertEqual(model.selectedConnection?.id, "z")
        XCTAssertNil(model.selectionDisappearMessage)
        model.setVisible(false)
    }

    func testReadFailureKeepsStaleDataAndTooLargePausesUntilManualRetry() async throws {
        let requests = ConnectionRequests(), model = model(requests, interval: 20_000_000)
        activate(model)
        await settle(model, requests: requests, records: try snapshot(["a"]))
        await waitUntil { requests.readCount == 2 }
        requests.failRead(2, CLIError.command("mihomo_api_error", "API unavailable"))
        await waitUntil { model.state == .stale }
        XCTAssertEqual(model.connections.map(\.id), ["a"])
        XCTAssertFalse(model.canClose)
        XCTAssertTrue(model.errorMessage?.contains("stale") == true)
        await waitUntil { requests.readCount == 3 }
        requests.failRead(3, CLIError.command("response_too_large", "large"))
        await waitUntil { model.automaticRefreshPaused }
        try await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(requests.readCount, 3)
        XCTAssertEqual(model.state, .tooLarge)
        XCTAssertTrue(model.errorMessage?.contains("4 MiB") == true)
        model.refresh()
        await waitUntil { requests.readCount == 4 }
        requests.failRead(4, CLIError.outputTooLarge(8_388_608))
        await waitUntil { model.automaticRefreshPaused && !model.isRefreshing }
        XCTAssertTrue(model.errorMessage?.contains("8 MiB") == true)
        model.refresh()
        await waitUntil { requests.readCount == 5 }
        requests.finishRead(5, try snapshot(["b"]))
        await waitUntil { model.state == .fresh }
        XCTAssertTrue(model.canClose)
        XCTAssertNil(model.errorMessage)
        model.setVisible(false)
        if model.isRefreshing { requests.finishRead(requests.readCount, []) }
        await model.waitForCancelledRead()
    }

    func testStoppedEngineClearsSnapshotAndSuspensionDrainsThenRefreshes() async throws {
        let requests = ConnectionRequests(), model = model(requests)
        activate(model)
        await settle(model, requests: requests, records: try snapshot(["a"]))
        model.select(id: "a")
        model.refresh()
        await waitUntil { requests.readCount == 2 }
        model.setSuspended(true)
        model.setSuspended(false)
        XCTAssertEqual(requests.readCount, 2)
        requests.finishRead(2, try snapshot(["old"]))
        await waitUntil { requests.readCount == 3 }
        model.setEngineState(.stopped)
        XCTAssertEqual(model.state, .stopped)
        XCTAssertEqual(model.connections, [])
        XCTAssertEqual(model.rows, [])
        XCTAssertNil(model.selectedID)
        requests.finishRead(3, try snapshot(["old"]))
        await model.waitForCancelledRead()
        XCTAssertEqual(model.connections, [])
        model.setEngineState(.running)
        await waitUntil { requests.readCount == 4 }
        requests.finishRead(4, [])
        await waitUntil { model.state == .fresh }
        model.setVisible(false)
    }

    func testCloseCapturesIDWaitsForCancelledReadAndSubmitsOnlyOnce() async throws {
        let requests = ConnectionRequests(), model = model(requests)
        activate(model)
        await settle(model, requests: requests, records: try snapshot(["a", "b"]))
        model.select(id: "a")
        model.refresh()
        await waitUntil { requests.readCount == 2 }
        model.close(id: "a")
        model.select(id: "b")
        model.close(id: "b"); model.closeAll(); model.refresh()
        try await Task.sleep(nanoseconds: 40_000_000)
        XCTAssertEqual(requests.commands, [])
        XCTAssertTrue(model.operationInProgress)
        requests.finishRead(2, try snapshot(["outdated"]))
        await waitUntil { requests.commands.count == 1 }
        XCTAssertEqual(requests.commands, [["connections", "close", "a"]])
        XCTAssertEqual(model.connections.map(\.id), ["a", "b"])
        requests.finishClose()
        await waitUntil { requests.readCount == 3 }
        XCTAssertTrue(model.operationMessage?.contains("accepted") == true)
        XCTAssertFalse(model.operationInProgress)
        requests.finishRead(3, try snapshot(["b"]))
        await waitUntil { model.state == .fresh }
        model.setQuery("no-match")
        await waitUntil { model.rows.isEmpty }
        model.closeAll()
        await waitUntil { requests.commands.count == 2 }
        XCTAssertEqual(requests.commands[1], ["connections", "close-all"])
        requests.finishClose(CLIError.timeout)
        await waitUntil { requests.readCount == 4 }
        XCTAssertTrue(model.operationMessage?.contains("not retried") == true)
        XCTAssertEqual(requests.commands.count, 2)
        requests.finishRead(4, [])
        await waitUntil { !model.isRefreshing }
        model.setVisible(false)
    }

    func testSettingsWaitSubmittedCloseAndQuitFailureResumesVisibleReads() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-coordination-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let log = directory.appendingPathComponent("commands")
        let failure = directory.appendingPathComponent("fail-quit")
        let cli = try fixture("""
        import json,sys,os
        command = ' '.join(sys.argv[1:])
        with open('\(log.path)','a') as file: file.write(command+'\\n')
        if command == '--json quit' and os.path.exists('\(failure.path)'):
            print(json.dumps({"ok":False,"error":{"code":"quit_failed","message":"cleanup failed"}}),file=sys.stderr)
            sys.exit(1)
        print(json.dumps({"ok":True,"data":{"mode":"direct"}}))
        """)
        defer { try? FileManager.default.removeItem(at: cli) }
        func commands() -> [String] { ((try? String(contentsOf: log)) ?? "").split(separator: "\n").map(String.init) }
        let requests = ConnectionRequests(), connections = model(requests)
        let app = AppModel(cli: NagiCLIClient(executable: cli), connections: connections)
        activate(connections)
        await settle(connections, requests: requests, records: try snapshot(["a"]))
        connections.close(id: "a")
        await waitUntil { requests.commands.count == 1 }
        app.perform(["mode", "direct"])
        XCTAssertTrue(app.busy)
        try await Task.sleep(nanoseconds: 50_000_000)
        XCTAssertEqual(commands(), [], "a setting must wait for an already submitted close")
        XCTAssertEqual(requests.readCount, 1, "busy settings must pause connection polling")
        requests.finishClose()
        await waitUntil { !app.busy && requests.readCount == 2 }
        XCTAssertEqual(commands(), ["--json mode direct"])
        requests.finishRead(2, try snapshot(["b"]))
        await waitUntil { connections.state == .fresh && !connections.isRefreshing }
        connections.refresh()
        await waitUntil { requests.readCount == 3 }
        try "fail".write(to: failure, atomically: true, encoding: .utf8)
        var quitResult: Bool?
        app.quit { quitResult = $0 }
        try await Task.sleep(nanoseconds: 50_000_000)
        XCTAssertEqual(commands(), ["--json mode direct"], "quit must drain its cancelled read before cleanup")
        requests.finishRead(3, try snapshot(["old"]))
        await waitUntil { quitResult == false && requests.readCount == 4 }
        XCTAssertTrue(app.quitErrorMessage?.contains("cleanup failed") == true)
        XCTAssertEqual(connections.connections.filter { !$0.ended }.map(\.id), ["b"])
        XCTAssertTrue(connections.connections.contains { $0.id == "a" && $0.ended })
        requests.finishRead(4, try snapshot(["c"]))
        await waitUntil { connections.state == .fresh && !connections.isRefreshing }
        try FileManager.default.removeItem(at: failure)
        quitResult = nil
        app.quit { quitResult = $0 }
        await waitUntil { quitResult == true }
        try await Task.sleep(nanoseconds: 40_000_000)
        XCTAssertEqual(requests.readCount, 4, "successful quit must not briefly resume polling")
        XCTAssertEqual(commands(), ["--json mode direct", "--json quit", "--json quit"])
        connections.setVisible(false)
    }

    func testRealCLIChildrenAreCancelledForVisibilitySleepSettingsAndEngineStop() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-read-cancel-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let log = directory.appendingPathComponent("events")
        let cli = try fixture("""
        import signal,sys,time,os
        def record(event):
            with open('\(log.path)','a') as file: file.write(event+' '+str(os.getpid())+'\\n')
        def stop(*_):
            record('stopped')
            sys.exit(0)
        signal.signal(signal.SIGTERM,stop)
        record('started')
        time.sleep(30)
        print('{"ok":true,"data":{"connections":[]}}')
        """)
        defer { try? FileManager.default.removeItem(at: cli) }
        func events() -> [String] { ((try? String(contentsOf: log)) ?? "").split(separator: "\n").map(String.init) }
        let model = ConnectionsModel(cli: NagiCLIClient(executable: cli))
        activate(model)
        let suspend: [() -> Void] = [
            { model.setVisible(false) },
            { model.setSuspended(true) },
            { model.setOperationBlocked(true) },
            { model.setEngineState(.stopped) }
        ]
        let resume: [() -> Void] = [
            { model.setVisible(true) },
            { model.setSuspended(false) },
            { model.setOperationBlocked(false) }
        ]
        for index in suspend.indices {
            await waitUntil { events().filter { $0.hasPrefix("started") }.count == index + 1 }
            let began = Date()
            suspend[index]()
            await model.waitForCancelledRead()
            XCTAssertLessThan(Date().timeIntervalSince(began), 3)
            XCTAssertEqual(events().filter { $0.hasPrefix("stopped") }.count, index + 1)
            XCTAssertFalse(model.isRefreshing)
            if index < resume.count { resume[index]() }
        }
        XCTAssertEqual(events().count, 8)
        model.setVisible(false)
    }
}

@MainActor extension ConnectionsTests {
    func testSingleSnapshotHistoryRevivalExpiryAndBudgets() throws {
        let date = Date(timeIntervalSince1970: 1000)
        let brief = try snapshot(["brief"])
        var records = ConnectionRecords()
        records.merge(brief, at: date)
        records.merge([], at: date.addingTimeInterval(0.25))
        XCTAssertEqual(records.ended["brief"]?.connection.ended, true)
        records.interrupt()
        records.merge(brief, at: date.addingTimeInterval(1))
        XCTAssertTrue(records.ended.isEmpty)
        XCTAssertTrue(records.active["brief"]?.observationInterrupted == true)
        records.merge([], at: date.addingTimeInterval(2))
        records.prune(at: date.addingTimeInterval(301))
        XCTAssertEqual(records.ended.count, 1)
        records.prune(at: date.addingTimeInterval(302))
        XCTAssertTrue(records.ended.isEmpty)
        records.maximumCount = 1
        records.merge(try snapshot(["older"]), at: date)
        records.merge(try snapshot(["newer"]), at: date.addingTimeInterval(1))
        records.merge([], at: date.addingTimeInterval(2))
        XCTAssertEqual(Set(records.ended.keys), ["newer"])
        records.byteBudget = 1
        records.prune(at: date.addingTimeInterval(3))
        XCTAssertTrue(records.ended.isEmpty)
        records.merge(brief, at: date)
        XCTAssertEqual(records.active.count, 1, "history budgets must not truncate active snapshots")
    }

    func testFreezeCollectsBriefConnectionAndRetainsEvictedSelection() async throws {
        let requests = ConnectionRequests()
        var date = Date(timeIntervalSince1970: 1000)
        let model = ConnectionsModel(fetch: { try await requests.read() }, execute: { try await requests.execute($0) },
                                     interval: 60_000_000_000, now: { date })
        activate(model)
        await settle(model, requests: requests, records: try snapshot(["selected"]))
        model.select(id: "selected")
        model.refresh()
        await waitUntil { requests.readCount == 2 }
        model.toggleFreeze()
        requests.finishRead(2, try snapshot(["brief"]))
        await waitUntil { !model.isRefreshing }
        XCTAssertEqual(model.connections.map(\.id), ["selected"])
        XCTAssertFalse(model.canClose)
        model.refresh(); model.closeAll(); model.close(id: "selected")
        XCTAssertEqual(requests.readCount, 2)
        XCTAssertTrue(requests.commands.isEmpty)
        // Automatic sampling continues while frozen; use the injected stream
        // below to verify successive samples without manual refresh.
        model.toggleFreeze()
        await waitUntil { model.connections.contains { $0.id == "brief" } }
        XCTAssertTrue(model.selectedConnection?.ended == true)
        date = date.addingTimeInterval(301)
        model.refresh()
        await waitUntil { requests.readCount == 3 }
        requests.finishRead(3, [])
        await waitUntil { !model.isRefreshing && !model.connections.contains { $0.id == "selected" } }
        XCTAssertEqual(model.selectedConnection?.id, "selected")
        XCTAssertTrue(model.selectedConnection?.ended == true)
        model.select(id: nil)
        XCTAssertNil(model.selectedConnection)
        model.setVisible(false)
    }

    func testStreamingFreezeMergesEveryIntermediateSample() async throws {
        let frames = try [snapshot(["initial"]), snapshot(["brief"]), snapshot([])]
        var emit: (([Connection]) async -> Void)?
        let model = ConnectionsModel(fetch: { [] }, execute: { _ in }, watch: { receive in
            emit = receive
            while !Task.isCancelled { try await Task.sleep(nanoseconds: 10_000_000) }
        })
        activate(model)
        await waitUntil { emit != nil }
        await emit?(frames[0])
        await waitUntil { model.connections.count == 1 }
        model.toggleFreeze()
        await emit?(frames[1])
        await emit?(frames[2])
        XCTAssertEqual(model.connections.map(\.id), ["initial"])
        model.toggleFreeze()
        await waitUntil { model.rows.count == 2 }
        XCTAssertTrue(model.connections.allSatisfy(\.ended))
        model.setFilter(.active)
        await waitUntil { model.rows.isEmpty }
        model.setFilter(.ended)
        await waitUntil { model.rows.count == 2 }
        model.setVisible(false)
        await model.waitForCancelledRead()
    }
}

@MainActor extension ConnectionsTests {
    func testConnectionStreamUsesPerLineLimitAndDeliversEverySample() async throws {
        let executable = try fixture("""
        import json,signal,sys,time
        signal.signal(signal.SIGTERM,lambda *_: sys.exit(0))
        for index in range(6):
            print(json.dumps({"ok":True,"data":{"connections":[{"id":str(index),"metadata":{"processPath":"x"*1500000}}]}}),flush=True)
        time.sleep(30)
        """)
        defer { try? FileManager.default.removeItem(at: executable) }
        var observed: [String] = []
        let task = Task {
            try await NagiCLIClient(executable: executable).watchConnections { snapshot in
                await MainActor.run { observed.append(contentsOf: snapshot.map(\.id)) }
            }
        }
        await waitUntil { observed.count == 6 }
        task.cancel()
        do { try await task.value; XCTFail("stream cancellation must be reported") }
        catch is CancellationError { }
        XCTAssertEqual(observed, ["0", "1", "2", "3", "4", "5"])
    }

    func testConnectionStreamOverflowPreservesSpecificError() async throws {
        let executable = try fixture("""
        import sys,time
        sys.stdout.write('x'*8388609)
        sys.stdout.flush()
        time.sleep(30)
        """)
        defer { try? FileManager.default.removeItem(at: executable) }
        do {
            try await NagiCLIClient(executable: executable).watchConnections { _ in }
            XCTFail("oversized sample accepted")
        } catch CLIError.outputTooLarge(let limit) { XCTAssertEqual(limit, 8_388_608) }
    }

    func testReconnectBackoffAndSuccessfulSampleReset() async throws {
        let requests = ConnectionRequests()
        var date = Date(timeIntervalSince1970: 1000)
        let model = ConnectionsModel(fetch: { try await requests.read() }, execute: { _ in },
                                     interval: 10_000_000, now: { date })
        activate(model)
        for (index, delay) in [1.0, 2, 4, 8, 10].enumerated() {
            await waitUntil { requests.readCount == index + 1 }
            requests.failRead(index + 1, CLIError.command("mihomo_api_error", "unavailable"))
            await waitUntil { !model.isRefreshing }
            date = date.addingTimeInterval(delay - 0.1)
            try await Task.sleep(nanoseconds: 30_000_000)
            XCTAssertEqual(requests.readCount, index + 1)
            date = date.addingTimeInterval(0.1)
        }
        await waitUntil { requests.readCount == 6 }
        requests.finishRead(6, try snapshot(["active"]))
        await waitUntil { requests.readCount == 7 }
        requests.failRead(7, CLIError.command("mihomo_api_error", "unavailable"))
        await waitUntil { !model.isRefreshing }
        XCTAssertFalse(model.connections.first?.ended ?? true)
        date = date.addingTimeInterval(1)
        await waitUntil { requests.readCount == 8 }
        model.setVisible(false)
        requests.finishRead(8, [])
        await model.waitForCancelledRead()
    }
}
