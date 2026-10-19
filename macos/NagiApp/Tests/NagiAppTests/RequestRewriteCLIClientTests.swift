import XCTest
import Foundation
@testable import NagiApp

final class RequestRewriteCLIClientTests: XCTestCase {
    private func fixture(_ source: String) throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-rewrite-cli-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        let script = directory.appendingPathComponent("fixture.sh")
        try ("#!/bin/sh\n" + source + "\n").write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        return script
    }
    func testSensitiveInputUsesOnlyStdinAndClosesPipeForEOF() async throws {
        let script = try fixture(#"""
        test "$#" = 5 && test "$1" = --json && test "$2" = request-rewrite && test "$3" = scripts && test "$4" = add && test "$5" = - || exit 9
        exec /usr/bin/python3 -c 'import sys,json; v=json.load(sys.stdin); assert v == {"id":"example","body_access":"none","source":"function transform(x){return x}"}; print("{\"ok\":true,\"data\":{\"saved\":true}}")'
        """#)
        let data = Data(#"{"id":"example","body_access":"none","source":"function transform(x){return x}"}"#.utf8)
        let result = try await NagiCLIClient(executable: script, timeout: 3).rewrite(["scripts", "add", "-"], input: data)
        XCTAssertEqual(result["saved"]?.bool, true)
    }
    func testRawExportNeverAddsJSONFlagAndPreservesExactBytes() async throws {
        let script = try fixture(#"""
        test "$#" = 5 && test "$1" = request-rewrite && test "$2" = export && test "$3" = - && test "$4" = --profile && test "$5" = work || exit 9
        printf '{"large":9007199254740993}\n'
        """#)
        let output = try await NagiCLIClient(executable: script).rewriteExport(["export", "-", "--profile", "work"])
        XCTAssertEqual(output, Data("{\"large\":9007199254740993}\n".utf8))
    }
    func testMalformedErrorDoesNotRevealSensitiveStderr() async throws {
        let script = try fixture("printf 'Authorization: fictitious-secret-placeholder' >&2; exit 1")
        do { _ = try await NagiCLIClient(executable: script).rewrite(["status"]); XCTFail("Expected error") }
        catch { XCTAssertFalse(error.localizedDescription.contains("fictitious-secret-placeholder")) }
        let invalidSuccess = try fixture("printf 'invalid'; printf 'fictitious-secret-placeholder' >&2")
        do { _ = try await NagiCLIClient(executable: invalidSuccess).rewrite(["status"]); XCTFail("Expected invalid JSON") }
        catch { XCTAssertFalse(error.localizedDescription.contains("fictitious-secret-placeholder")) }
    }
    func testStructuredErrorRetainsSanitizedRecoveryMessage() async throws {
        let script = try fixture(#"printf '{"ok":false,"error":{"code":"request_rewrite_error","message":"rewrite_capability_missing; update the engine"}}' >&2; exit 1"#)
        do { _ = try await NagiCLIClient(executable: script).rewrite(["enable"]); XCTFail("Expected error") }
        catch CLIError.command(let code, let message) { XCTAssertEqual(code, "request_rewrite_error"); XCTAssertTrue(message.contains("rewrite_capability_missing")) }
    }
    func testRefusedLargeInputTimesOutAndKillsTermIgnoringChild() async throws {
        let script = try fixture(#"exec /usr/bin/python3 -c 'import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(60)'"#)
        let started = Date()
        do {
            _ = try await NagiCLIClient(executable: script, timeout: 0.1).rewrite(["preview", "-"], input: Data(repeating: 65, count: 2_097_152))
            XCTFail("Expected timeout")
        } catch CLIError.timeout { }
        XCTAssertLessThan(Date().timeIntervalSince(started), 5)
    }
    func testEarlyExitDuringLargeInputDoesNotRaiseSIGPIPE() async throws {
        let script = try fixture(#"printf '{"ok":false,"error":{"code":"request_rewrite_error","message":"rewrite_invalid"}}' >&2; exit 1"#)
        do {
            _ = try await NagiCLIClient(executable: script, timeout: 2).rewrite(["preview", "-"], input: Data(repeating: 65, count: 2_097_152))
            XCTFail("Expected error")
        } catch CLIError.command(let code, _) { XCTAssertEqual(code, "request_rewrite_error") }
    }
    func testConcurrentLargeStdoutStderrDrainWithoutDeadlock() async throws {
        let script = try fixture(#"""
        exec /usr/bin/python3 -c 'import os,threading; t=threading.Thread(target=lambda:os.write(2,b"x"*1100000)); t.start(); os.write(1,b"x"*17000000); t.join()'
        """#)
        do { _ = try await NagiCLIClient(executable: script, timeout: 3).rewrite(["status"]); XCTFail("Expected limit error") }
        catch CLIError.outputTooLarge { }
    }
    func testCancellationStopsRefusedInputAndWaitsForChildExit() async throws {
        let script = try fixture(#"exec /usr/bin/python3 -c 'import time; time.sleep(60)'"#)
        let task = Task { try await NagiCLIClient(executable: script).rewrite(["preview", "-"], input: Data(repeating: 65, count: 2_097_152)) }
        try await Task.sleep(nanoseconds: 100_000_000)
        task.cancel()
        let started = Date()
        do { _ = try await task.value; XCTFail("Expected cancellation") } catch is CancellationError { }
        XCTAssertLessThan(Date().timeIntervalSince(started), 5)
    }
}

extension RequestRewriteCLIClientTests {
    func testRewritePreviewHasScopedLargeInputAndOutputLimits() async throws {
        let script = try fixture(#"""
        exec /usr/bin/python3 -c 'import sys,json; data=sys.stdin.buffer.read(); assert len(data)==13*1048576; print(json.dumps({"ok":True,"data":{"sensitive":True,"body":"x"*(17*1048576)}}))'
        """#)
        let client = NagiCLIClient(executable: script, timeout: 15)
        let result = try await client.rewrite(["preview", "-"], input: Data(repeating: 65, count: 13 * 1_048_576))
        XCTAssertEqual(result["body"]?.string?.count, 17 * 1_048_576)
        do { _ = try await client.rewrite(["rules", "add", "-"], input: Data(repeating: 65, count: 1_048_577)); XCTFail("Expected document input bound") }
        catch CLIError.outputTooLarge(let limit) { XCTAssertEqual(limit, 1_048_576) }
        XCTAssertEqual(NagiCLIClient.rewritePreviewInputLimit, 24 * 1_048_576)
        XCTAssertEqual(NagiCLIClient.rewritePreviewOutputLimit, 72 * 1_048_576 + 65_536)
    }
    func testRawExportAcceptsEscapedDocumentExpansion() async throws {
        let script = try fixture(#"exec /usr/bin/python3 -c 'import sys; sys.stdout.write("x"*(6*1048576))'"#)
        let output = try await NagiCLIClient(executable: script, timeout: 10).rewriteExport(["export", "-"])
        XCTAssertEqual(output.count, 6 * 1_048_576)
    }
}
