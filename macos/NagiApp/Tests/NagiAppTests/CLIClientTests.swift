import XCTest
import Foundation
@testable import NagiApp

final class CLIClientTests: XCTestCase {
    private func fixture(_ body: String) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-cli-\(UUID().uuidString).sh")
        try ("#!/bin/sh\n" + body + "\n").write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
        addTeardownBlock { try? FileManager.default.removeItem(at: url) }
        return url
    }

    private func waitForMarker(_ url: URL) async throws {
        let deadline = Date().addingTimeInterval(3)
        while !FileManager.default.fileExists(atPath: url.path), Date() < deadline {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertTrue(FileManager.default.fileExists(atPath: url.path), "CLI did not reach readiness")
    }

    func testImmediateExitDrainsFragmentedOutputAndPreservesArguments() async throws {
        let script = try fixture(#"""
        test "$1" = '--json' && test "$2" = 'status' && test "$3" = 'a b' || exit 9
        printf '{"ok":'; printf 'true,"data":{"state":'; printf '"running"}}'
        printf 'first diagnostic ' >&2; printf 'second diagnostic' >&2
        """#)
        // Repeated fast exits exercise exit notifications arriving before wait.
        for _ in 0..<20 {
            let response = try await NagiCLIClient(executable: script).run(["status", "a b"])
            XCTAssertEqual(response["state"]?.string, "running")
        }
    }

    func testFragmentedStderrEnvelopePreservesCommandError() async throws {
        let script = try fixture(#"""
        printf '{"ok":false,"error":{"code":"not_' >&2
        printf 'running","message":"engine stopped"}}' >&2
        exit 7
        """#)
        do { _ = try await NagiCLIClient(executable: script).run(["status"]); XCTFail("Expected command error") }
        catch CLIError.command(let code, let message) {
            XCTAssertEqual(code, "not_running"); XCTAssertEqual(message, "engine stopped")
        }
    }

    func testNonzeroExitWithSuccessEnvelopeIsCommandFailure() async throws {
        let script = try fixture(#"printf '{"ok":true,"data":{}}'; exit 9"#)
        do { _ = try await NagiCLIClient(executable: script).run([]); XCTFail("Expected exit error") }
        catch CLIError.command(let code, let message) {
            XCTAssertEqual(code, "command_failed"); XCTAssertEqual(message, "The CLI exited with status 9.")
        }
    }

    func testInvalidJSONUsesDrainedStderrDiagnostic() async throws {
        let script = try fixture("printf 'invalid'; printf 'diagnostic ' >&2; printf 'complete\\n' >&2")
        do { _ = try await NagiCLIClient(executable: script).run([]); XCTFail("Expected JSON error") }
        catch CLIError.invalidJSON(let message) { XCTAssertEqual(message, "diagnostic complete") }
    }

    func testMissingDataIsRejected() async throws {
        let script = try fixture(#"printf '{"ok":true}'"#)
        do { _ = try await NagiCLIClient(executable: script).run([]); XCTFail("Expected omitted data error") }
        catch CLIError.invalidJSON(let message) { XCTAssertEqual(message, "The CLI omitted the data field.") }
    }

    func testOversizedStdoutAndStderrAreDrainedWithoutDeadlock() async throws {
        for redirect in ["", " >&2"] {
            let script = try fixture("/usr/bin/head -c 1100000 /dev/zero\(redirect)")
            do { _ = try await NagiCLIClient(executable: script, timeout: 3).run([]); XCTFail("Expected output limit error") }
            catch CLIError.invalidJSON(let message) { XCTAssertEqual(message, "CLI output exceeded 1 MiB.") }
        }
    }

    func testAlreadyCancelledTaskDoesNotLaunchCLI() async throws {
        let script = try fixture("printf 'started' > \"$0.started\"; printf '{\"ok\":true,\"data\":{}}'")
        let marker = URL(fileURLWithPath: script.path + ".started")
        addTeardownBlock { try? FileManager.default.removeItem(at: marker) }
        let task = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            return try await NagiCLIClient(executable: script).run([])
        }
        do { _ = try await task.value; XCTFail("Expected cancellation") }
        catch is CancellationError { }
        XCTAssertFalse(FileManager.default.fileExists(atPath: marker.path))
    }

    func testCancellationTerminatesChildIgnoringSIGTERM() async throws {
        let script = try fixture("trap '' TERM; printf 'ready' > \"$0.ready\"; exec /bin/sleep 30")
        let marker = URL(fileURLWithPath: script.path + ".ready")
        addTeardownBlock { try? FileManager.default.removeItem(at: marker) }
        let task = Task { try await NagiCLIClient(executable: script).run([]) }
        try await waitForMarker(marker)
        let start = Date()
        task.cancel()
        do { _ = try await task.value; XCTFail("Expected cancellation") }
        catch is CancellationError { }
        XCTAssertLessThan(Date().timeIntervalSince(start), 3)
    }

    func testTimeoutRemainsTimeoutWhenCancellationArrivesDuringKillGrace() async throws {
        let script = try fixture("trap '' TERM; printf 'ready' > \"$0.ready\"; exec /bin/sleep 30")
        let marker = URL(fileURLWithPath: script.path + ".ready")
        addTeardownBlock { try? FileManager.default.removeItem(at: marker) }
        let start = Date()
        let task = Task { try await NagiCLIClient(executable: script, timeout: 0.1).run([]) }
        try await waitForMarker(marker)
        try await Task.sleep(nanoseconds: 300_000_000)
        task.cancel()
        do { _ = try await task.value; XCTFail("Expected timeout") }
        catch CLIError.timeout { }
        XCTAssertLessThan(Date().timeIntervalSince(start), 3)
    }

    func testExitedChildDoesNotWaitForDescendantHeldPipes() async throws {
        let script = try fixture(#"""
        /bin/sleep 3 &
        printf '{"ok":true,"data":{"finished":true}}'
        """#)
        let start = Date()
        let response = try await NagiCLIClient(executable: script).run([])
        XCTAssertEqual(response["finished"]?.bool, true)
        XCTAssertLessThan(Date().timeIntervalSince(start), 2)
    }

    func testMissingAndUnlaunchableExecutableErrors() async throws {
        let missing = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        do { _ = try await NagiCLIClient(executable: missing).run([]); XCTFail("Expected missing CLI") }
        catch CLIError.missingCLI(let path) { XCTAssertEqual(path, missing.path) }
        let script = try fixture("exit 0")
        try "#!/nonexistent/nagi-test-interpreter\n".write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        do { _ = try await NagiCLIClient(executable: script).run([]); XCTFail("Expected launch failure") }
        catch CLIError.launch { }
    }
}
