import XCTest
import AppKit
@testable import NagiApp

@MainActor final class RequestRewriteWindowTests: XCTestCase {
    func testWindowIsIndependentReusableAndCloseNeverStopsService() async throws {
        _ = NSApplication.shared
        var commands: [[String]] = []
        let model = RequestRewriteModel(read: { args, _ in
            let text = args == ["status"] ? #"{"profile":"work","actual":"disabled","enabled_intent":false,"tun_running":false}"# : (args.first == "scripts" ? #"{"scripts":[]}"# : #"{"rules":[]}"#)
            return try JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))
        }, submit: { args, _ in
            commands.append(args)
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        }, export: { _ in Data() })
        let controller = RequestRewriteWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        XCTAssertEqual(window.title, "Nagi Request Rewrite")
        XCTAssertTrue(window.styleMask.contains(.resizable)); XCTAssertTrue(window.styleMask.contains(.miniaturizable))
        XCTAssertFalse(window.isReleasedWhenClosed)
        controller.present(); controller.present()
        XCTAssertTrue(controller.window === window)
        window.close(); await model.waitForCancelledRead()
        XCTAssertTrue(commands.isEmpty)
        controller.present(); XCTAssertTrue(controller.window === window)
        window.orderOut(nil); await model.waitForCancelledRead()
        XCTAssertTrue(model.stale)
        window.close()
    }
    func testRewriteMinuteTimestampOmitsSeconds() {
        let text = RequestRewriteView.time(Date(timeIntervalSince1970: 1_786_000_043))
        XCTAssertEqual(text.count, 16)
        XCTAssertEqual(text.filter { $0 == ":" }.count, 1)
    }
    func testWindowTitleTracksProfileRefreshAndRetainsLastProfileOnFailure() async throws {
        _ = NSApplication.shared
        var profile: String? = "work"
        var failRead = false
        let model = RequestRewriteModel(read: { args, _ in
            if failRead { throw CLIError.command("fixture", "read failed") }
            if args == ["status"] {
                return .objectValue(["profile": profile.map(JSONValue.stringValue) ?? .null,
                    "actual": .stringValue("disabled"), "enabled_intent": .boolValue(false)])
            }
            return .objectValue([args.first == "scripts" ? "scripts" : "rules": .arrayValue([])])
        }, submit: { _, _ in .objectValue([:]) }, export: { _ in Data() })
        let controller = RequestRewriteWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        // Drive the read slot directly so the test does not depend on window occlusion.
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertEqual(window.title, "Nagi Request Rewrite — work")

        profile = "personal"
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertEqual(window.title, "Nagi Request Rewrite — personal")

        failRead = true
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(model.stale)
        XCTAssertEqual(window.title, "Nagi Request Rewrite — personal")

        failRead = false; profile = nil
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertEqual(window.title, "Nagi Request Rewrite")
    }
    func testQuitWaitsForRewriteSubmitAndFailedQuitRestoresGate() async throws {
        var allowSubmit: CheckedContinuation<Void, Never>?
        var submitted = false, cleanup = 0, completion: Bool?
        let app = AppModel(runCommand: { args in
            if args == ["quit"] { cleanup += 1; throw CLIError.command("fixture", "cleanup failed") }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        app.requestRewrite.submit = { _, _ in
            submitted = true
            await withCheckedContinuation { allowSubmit = $0 }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        }
        let task = Task { await app.requestRewrite.perform(["disable"]) }
        for _ in 0..<100 { if submitted { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(submitted)
        app.quit { completion = $0 }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertEqual(cleanup, 0)
        allowSubmit?.resume()
        _ = await task.value
        for _ in 0..<100 { if completion != nil { break }; try await Task.sleep(nanoseconds: 10_000_000) }
        XCTAssertEqual(cleanup, 1); XCTAssertEqual(completion, false)
        XCTAssertTrue(app.requestRewrite.canOperate)
    }
}

@MainActor extension RequestRewriteWindowTests {
    func testSettingsWaitForSubmittedRewriteBeforeDispatch() async throws {
        var release: CheckedContinuation<Void, Never>?
        var submitted = false, settingsCommands = 0
        let app = AppModel(runCommand: { args in
            if args == ["fixture-setting"] { settingsCommands += 1 }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        app.requestRewrite.submit = { _, _ in
            submitted = true
            await withCheckedContinuation { release = $0 }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        }
        let operation = Task { await app.requestRewrite.perform(["disable"]) }
        for _ in 0..<100 { if submitted { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        let setting = Task { await app.saveSourceSetting(["fixture-setting"]) }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertTrue(app.busy); XCTAssertEqual(settingsCommands, 0)
        release?.resume(); _ = await operation.value
        _ = await setting.value
        XCTAssertEqual(settingsCommands, 1)
    }
}

@MainActor extension RequestRewriteWindowTests {
    func testActualRewriteTransportHoldsAppSettingsGateUntilChildCompletes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-rewrite-gate-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        defer { try? FileManager.default.removeItem(at: directory) }
        let cli = directory.appendingPathComponent("fixture.sh")
        try #"""
        #!/bin/sh
        exec /usr/bin/python3 -c 'import time; time.sleep(0.2); print("{\"ok\":true,\"data\":{\"saved\":true}}")'
        """#.write(to: cli, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
        let app = AppModel(cli: NagiCLIClient(executable: cli, timeout: 3))
        let operation = Task { await app.requestRewrite.perform(["disable"]) }
        for _ in 0..<100 { if app.busy { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(app.busy); XCTAssertTrue(app.requestRewrite.operationInProgress)
        do { _ = try await app.executeRewrite(["enable"], input: nil); XCTFail("Overlapping mutation") } catch { }
        let accepted = await operation.value
        XCTAssertTrue(accepted); XCTAssertFalse(app.busy)
        app.requestRewrite.setSuspended(true)
        await app.requestRewrite.waitForCancelledRead()
    }
}
