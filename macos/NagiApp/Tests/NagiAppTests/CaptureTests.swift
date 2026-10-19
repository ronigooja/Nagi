import AppKit
import XCTest
@testable import NagiApp

final class CaptureTests: XCTestCase {
    private func reply(owner: String = "cli", state: String = "recording", records: Bool = false) throws -> CaptureReply {
        let status: [String: Any] = ["session_id": "session", "revision": 1, "state": state, "profile": "default", "backend": "user", "owner": owner, "scope": ["domains": [], "process_paths": [], "app_paths": []], "body_limit": 1048576, "records": 1, "bytes": 0, "dropped": 0, "truncated": 0, "read_only": false]
        var value: [String: Any] = ["sensitive": true, "status": status]
        if records { value["records"] = [try JSONSerialization.jsonObject(with: recordData)] }
        return try JSONDecoder().decode(CaptureReply.self, from: JSONSerialization.data(withJSONObject: value))
    }
    private var recordData: Data { Data(#"{"id":"record","session_id":"session","kind":"http","generation":"g","rule_version":"v","started":"2026-10-18T12:34:56.123Z","duration_ms":12,"rule_ids":[],"outcome":"streaming","identity":{"session_id":"session","revision":1,"uid":501,"process":"Browser","process_path":"/Applications/Fictional.app/Browser","source":"127.0.0.1:1234","destination":"192.0.2.1:443"},"snapshots":{"request_original":{"scheme":"https","host":"example.invalid","path":"/test","query":"token=fictitious","method":"GET","headers":{"Authorization":["Bearer fictitious"],"Cookie":["session=fictitious"]},"body_state":"collecting","body_size":0,"captured":0}}}"#.utf8) }

    func testRawMetadataAndPartialBodyStateDecode() throws {
        let record = try JSONDecoder().decode(CaptureRecord.self, from: recordData)
        XCTAssertEqual(record.original?.url, "https://example.invalid/test?token=fictitious")
        XCTAssertTrue(record.original!.headerText.contains("Authorization: Bearer fictitious"))
        XCTAssertTrue(record.original!.headerText.contains("Cookie: session=fictitious"))
        XCTAssertEqual(record.original?.bodyState, "collecting")
        XCTAssertEqual(captureMinute(record.started), captureMinute("2026-10-18T12:34:00Z"))
        XCTAssertFalse(captureMinute(record.started).contains(":56"))
    }
    func testBodyFormatsPreserveRawAndBinaryBytes() {
        let original = Data("{\"token\":\"fictitious\"}".utf8)
        XCTAssertEqual(captureBodyText(original, format: .raw), String(data: original, encoding: .utf8))
        XCTAssertTrue(captureBodyText(original, format: .json).contains("fictitious"))
        XCTAssertTrue(captureBodyText(Data([0, 255, 128]), format: .binary).contains("00 ff 80"))
        XCTAssertTrue(captureBodyText(original, format: .diff, before: original).hasPrefix("No body changes."))
        XCTAssertTrue(captureBodyText(Data("new".utf8), format: .diff, before: Data("old".utf8)).contains("− old\n+ new"))
    }
    @MainActor func testVisibilityNeverStartsAndForeignSessionSurvivesClose() async throws {
        var actions: [String] = []
        let status = try reply()
        let model = CaptureModel(read: { args, _ in actions.append(args[0]); return status }, submit: { args, _ in actions.append(args[0]); return status }, export: { _ in }, interval: 9_000_000_000)
        model.setVisible(true)
        await wait { !model.isReading }
        XCTAssertEqual(model.status?.owner, "cli")
        model.closeWindow()
        await model.stopOwnedSession()
        XCTAssertFalse(actions.contains("start"))
        XCTAssertFalse(actions.contains("stop"))
        let count = actions.count
        model.refresh()
        try await Task.sleep(nanoseconds: 20_000_000)
        XCTAssertEqual(count, actions.count)
    }
    @MainActor func testOwnedSessionStopsAndResumingVisibilityOnlyReads() async throws {
        var actions: [String] = []
        var current: CaptureReply! = try reply()
        let model = CaptureModel(read: { args, _ in actions.append(args[0]); return current }, submit: { args, _ in actions.append(args[0]); return current }, export: { _ in }, interval: 9_000_000_000)
        current = try reply(owner: model.owner)
        model.setVisible(true); await wait { !model.isReading }
        model.setVisible(false)
        await model.stopOwnedSession()
        XCTAssertEqual(actions.filter { $0 == "stop" }.count, 1)
        model.setVisible(true); await wait { !model.isReading }
        XCTAssertFalse(actions.contains("start")); XCTAssertFalse(actions.contains("resume"))
        model.setVisible(false)
    }
    @MainActor func testScopeSentThroughInputAndBlockedMutationRejected() async throws {
        var input: Data?, submitted: [String] = []
        let status = try reply()
        let model = CaptureModel(read: { _, _ in status }, submit: { args, data in submitted = args; input = data; return status }, export: { _ in })
        model.setBlocked(true)
        await model.start(scope: CaptureScope(), bodyLimit: 1048576)
        XCTAssertTrue(submitted.isEmpty)
        model.setBlocked(false)
        await model.start(scope: CaptureScope(domains: ["example.invalid"], processPaths: ["/opt/fictitious"], appPaths: []), bodyLimit: 8388608)
        XCTAssertEqual(submitted, ["start", "-"])
        let options = try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(input)) as? [String: Any])
        XCTAssertEqual(options["owner"] as? String, model.owner)
        XCTAssertEqual(options["body_limit"] as? Int, 8388608)
        XCTAssertEqual((options["scope"] as? [String: Any])?["domains"] as? [String], ["example.invalid"])
    }
    @MainActor func testBodyChunksAndFieldSelectionHook() async throws {
        let status = try reply(records: true)
        let record = try JSONDecoder().decode(CaptureRecord.self, from: recordData)
        var offsets: [String] = []
        let model = CaptureModel(read: { args, _ in
            if args[0] == "show" { return try JSONDecoder().decode(CaptureReply.self, from: JSONSerialization.data(withJSONObject: ["sensitive": true, "record": try JSONSerialization.jsonObject(with: self.recordData)])) }
            if args[0] == "body" {
                offsets.append(args[3]); let first = args[3] == "0"
                return try JSONDecoder().decode(CaptureReply.self, from: JSONSerialization.data(withJSONObject: ["sensitive": true, "body": ["data": Data((first ? "raw " : "secret").utf8).base64EncodedString(), "offset": first ? 0 : 4, "next_offset": first ? 4 : 10, "eof": !first, "state": "complete"]]))
            }
            return status
        }, submit: { _, _ in status }, export: { _ in }, interval: 9_000_000_000)
        model.setVisible(true); await wait { !model.isReading }
        model.select(record.id); await wait { !model.readingBody }
        XCTAssertEqual(offsets, ["0", "4"])
        XCTAssertEqual(model.selectedBody, Data("raw secret".utf8))
        var context: CaptureSelectionContext?
        model.onCreateRule = { context = $0 }
        model.createRule(field: CaptureFieldSelection(kind: "header", name: "Authorization", value: "Bearer fictitious"))
        XCTAssertEqual(context?.record.id, record.id)
        XCTAssertEqual(context?.field?.name, "Authorization")
        XCTAssertEqual(context?.body, Data("raw secret".utf8))
        model.setVisible(false)
    }
    @MainActor func testHiddenReadCancellationDoesNotPublishLateResponse() async throws {
        let status = try reply()
        var began = false
        let model = CaptureModel(read: { _, _ in began = true; try? await Task.sleep(nanoseconds: 70_000_000); return status }, submit: { _, _ in status }, export: { _ in })
        model.setVisible(true); await wait { began }
        model.setVisible(false)
        await model.waitForCancelledRead()
        XCTAssertNil(model.status)
        XCTAssertTrue(model.stale)
    }
    @MainActor private func wait(_ ready: () -> Bool) async {
        for _ in 0..<300 { if ready() { return }; try? await Task.sleep(nanoseconds: 5_000_000) }
        XCTFail("Timed out waiting for capture model")
    }
}

extension CaptureTests {
    func testNullScopeArraysAndEmptyBodyRemainDistinctFromCollecting() throws {
        let scope = try JSONDecoder().decode(CaptureScope.self, from: Data(#"{"domains":null,"process_paths":null,"app_paths":null}"#.utf8))
        XCTAssertEqual(scope, CaptureScope())
        let empty = try JSONDecoder().decode(CaptureBodyChunk.self, from: Data(#"{"data":null,"offset":0,"next_offset":0,"eof":true,"state":"complete"}"#.utf8))
        let pending = try JSONDecoder().decode(CaptureBodyChunk.self, from: Data(#"{"data":null,"offset":0,"next_offset":0,"eof":false,"state":"collecting"}"#.utf8))
        XCTAssertTrue(empty.data.isEmpty); XCTAssertEqual(empty.state, "complete")
        XCTAssertFalse(pending.eof); XCTAssertEqual(pending.state, "collecting")
    }
    func testRealTransportUsesStdinAndCancelsChild() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-capture-transport-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appendingPathComponent("fixture.py")
        try #"""
        #!/usr/bin/python3
        import sys,json,time
        if sys.argv[4] == 'start':
            assert sys.argv[1:] == ['--json','rewrite','capture','start','-']
            assert json.load(sys.stdin)['scope']['domains'] == ['private.example.invalid']
            print(json.dumps({'ok':True,'data':{'sensitive':True}}))
        else:
            time.sleep(30)
        """#.write(to: file, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: file.path)
        let client = NagiCLIClient(executable: file, timeout: 40)
        let result = try await client.capture(["start", "-"], input: Data(#"{"scope":{"domains":["private.example.invalid"]}}"#.utf8))
        XCTAssertTrue(result.sensitive)
        let start = Date()
        let task = Task { try await client.capture(["status"]) }
        try await Task.sleep(nanoseconds: 80_000_000); task.cancel()
        do { _ = try await task.value; XCTFail("Expected cancellation") } catch is CancellationError { }
        XCTAssertLessThan(Date().timeIntervalSince(start), 3)
    }
    @MainActor func testWindowMinimizeHideAndCloseCancelWithoutStartingCapture() async throws {
        _ = NSApplication.shared
        let status = try reply()
        var starts = 0, cancelled = 0, mutations: [String] = []
        let model = CaptureModel(read: { _, _ in
            starts += 1
            do { try await Task.sleep(nanoseconds: 20_000_000_000) } catch { cancelled += 1; throw error }
            return status
        }, submit: { args, _ in mutations.append(args[0]); return status }, export: { _ in }, interval: 60_000_000_000)
        let controller = CaptureWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        XCTAssertEqual(window.title, "Nagi Capture")
        XCTAssertTrue(window.styleMask.contains(.resizable)); XCTAssertTrue(window.styleMask.contains(.miniaturizable))
        XCTAssertFalse(window.isReleasedWhenClosed)
        controller.present(); await wait { starts == 1 }
        window.miniaturize(nil); await wait { cancelled == 1 }
        controller.present(); await wait { starts == 2 }
        window.orderOut(nil); await wait { cancelled == 2 }
        controller.present(); await wait { starts == 3 }
        model.setSuspended(true); await wait { cancelled == 3 }
        model.setSuspended(false); await wait { starts == 4 }
        window.close(); await wait { cancelled == 4 }
        XCTAssertTrue(mutations.isEmpty)
    }
    @MainActor func testSettingsAndQuitWaitForSubmittedCapture() async throws {
        var release: CheckedContinuation<Void, Never>?, submitted = false, commands: [[String]] = [], completion: Bool?
        let app = AppModel(runCommand: { args in commands.append(args); return .objectValue([:]) })
        let status = try reply()
        app.capture.submit = { _, _ in submitted = true; await withCheckedContinuation { release = $0 }; return status }
        let operation = Task { await app.capture.perform(["start", "-"], input: Data("{}".utf8)) }
        await wait { submitted }
        app.quit { completion = $0 }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertFalse(commands.contains(["quit"]))
        release?.resume(); _ = await operation.value
        await wait { completion != nil }
        XCTAssertEqual(completion, true)
        XCTAssertTrue(commands.contains(["quit"]))
    }
}


extension CaptureTests {
    func testUncertainSessionRetainsStopRecoveryAndHistoryAccess() throws {
        var document = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(reply().status)) as? [String: Any])
        document["state"] = "uncertain"
        document["recorder_state"] = "recording"
        document["recovery"] = "run nagi rewrite capture stop session"
        let status = try JSONDecoder().decode(CaptureStatus.self, from: JSONSerialization.data(withJSONObject: document))
        XCTAssertEqual(status.recorderState, "recording")
        XCTAssertEqual(status.recovery, "run nagi rewrite capture stop session")
        XCTAssertTrue(status.canStop)
        XCTAssertFalse(status.canPauseOrResume)
        XCTAssertFalse(status.canStartNewSession)
        let stopped = try XCTUnwrap(reply(state: "stopped").status)
        XCTAssertTrue(stopped.canStartNewSession)
        XCTAssertFalse(stopped.canStop)
        XCTAssertNil(stopped.recovery)
    }
    @MainActor func testOwnedUncertainSessionStopsOnClose() async throws {
        var stops = 0
        var current = try reply()
        let model = CaptureModel(read: { _, _ in current }, submit: { args, _ in
            if args[0] == "stop" { stops += 1 }
            return current
        }, export: { _ in }, interval: 9_000_000_000)
        current = try reply(owner: model.owner, state: "uncertain", records: true)
        model.setVisible(true)
        await wait { !model.isReading }
        XCTAssertFalse(model.records.isEmpty)
        XCTAssertTrue(model.canOperate)
        model.setVisible(false)
        await model.stopOwnedSession()
        XCTAssertEqual(stops, 1)
    }
}
