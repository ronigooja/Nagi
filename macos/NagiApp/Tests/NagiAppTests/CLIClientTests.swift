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

    func testOnlyAppUpdateUsesLongerPreparationTimeout() async throws {
        let script = try fixture(#"""
        exec /usr/bin/python3 -c 'import json,time; time.sleep(0.3); print(json.dumps({"ok":True,"data":{"scheduled":True}}))'
        """#)
        let client = NagiCLIClient(executable: script, timeout: 0.05, appUpdateTimeout: 2)
        let result = try await client.run(["update", "--app"])
        XCTAssertEqual(result["scheduled"]?.bool, true)
        for arguments in [["status"], ["update"], ["app", "update-error"]] {
            do { _ = try await client.run(arguments); XCTFail("Expected ordinary command timeout") }
            catch CLIError.timeout { }
        }
        do {
            _ = try await NagiCLIClient(executable: script, appUpdateTimeout: 0.05).run(["update", "--app"])
            XCTFail("Expected update preparation timeout")
        } catch CLIError.timeout { }
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
        // Observe the actual timeout signal instead of assuming that launch and
        // timeout have happened after a fixed sleep. Once this marker exists,
        // the client has classified timeout and is waiting for SIGKILL fallback.
        let script = try fixture(#"""
        trap 'printf timeout > "$0.timedout"; trap "" TERM' TERM
        printf ready > "$0.ready"
        while :; do :; done
        """#)
        let ready = URL(fileURLWithPath: script.path + ".ready")
        let timedOut = URL(fileURLWithPath: script.path + ".timedout")
        addTeardownBlock {
            try? FileManager.default.removeItem(at: ready)
            try? FileManager.default.removeItem(at: timedOut)
        }
        let task = Task { try await NagiCLIClient(executable: script, timeout: 2).run([]) }
        try await waitForMarker(ready)
        try await waitForMarker(timedOut)
        let cancellationStart = Date()
        task.cancel()
        do { _ = try await task.value; XCTFail("Expected timeout") }
        catch CLIError.timeout { }
        XCTAssertLessThan(Date().timeIntervalSince(cancellationStart), 2)
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

    func testSubscriptionWatchRepeatedImmediateExitWaits() async throws {
        let script = try fixture(#"""
        printf '{"ok":true,"data":{"scheduler_running":true}}\n'
        exit 7
        """#)
        for _ in 0..<20 {
            let watch = try SubscriptionWatch(executable: script)
            // The exit notification must be retained even before wait begins;
            // subsequent waits must observe the same completed process.
            for _ in 0..<2 {
                if case .failed(let error) = await watch.wait() {
                    XCTAssertTrue(error.localizedDescription.contains("unexpectedly"), "\(error)")
                } else { XCTFail("Expected unexpected scheduler exit") }
            }
        }
    }

    func testTrafficStreamRepeatedImmediateExitDrainsSample() async throws {
        let script = try fixture(#"""
        printf '{"ok":true,"data":{"upload":'; printf '123}}\n'
        """#)
        for _ in 0..<20 {
            let received = expectation(description: "final traffic sample")
            let stream = try TrafficStream(executable: script) { sample in
                XCTAssertEqual(sample["upload"]?.int64, 123)
                received.fulfill()
            }
            try await stream.wait()
            await fulfillment(of: [received], timeout: 2)
        }
    }

    func testTrafficStreamImmediateFailureDrainsStderrEnvelope() async throws {
        let script = try fixture(#"""
        printf '{"ok":false,"error":{"code":"stream_' >&2
        printf 'failed","message":"stream stopped"}}' >&2
        exit 7
        """#)
        for _ in 0..<20 {
            let stream = try TrafficStream(executable: script) { _ in XCTFail("Unexpected sample") }
            do { try await stream.wait(); XCTFail("Expected traffic error") }
            catch CLIError.command(let code, let message) {
                XCTAssertEqual(code, "stream_failed"); XCTAssertEqual(message, "stream stopped")
            }
        }
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
