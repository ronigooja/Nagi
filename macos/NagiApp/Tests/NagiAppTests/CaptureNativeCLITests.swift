import XCTest
@testable import NagiApp

/// Explicit opt-in: the caller supplies an isolated live recorder and real CLI.
/// This exercises native models and subprocess transport; it does not claim
/// interactive accessibility, OS routes, or application trust by itself.
@MainActor final class CaptureNativeCLITests: XCTestCase {
    func testCapturedFieldPreviewSaveThroughActualCLI() async throws {
        let env = ProcessInfo.processInfo.environment
        guard let executable = env["NAGI_CAPTURE_ACCEPTANCE_CLI"],
              let recordID = env["NAGI_CAPTURE_ACCEPTANCE_RECORD"] else {
            throw XCTSkip("Requires an explicitly prepared isolated live CLI capture")
        }
        let client = NagiCLIClient(executable: URL(fileURLWithPath: executable))
        let reply = try await client.capture(["show", recordID])
        let record = try XCTUnwrap(reply.record)
        let credentials = try XCTUnwrap(record.snapshots["request_original"]?.headers?["Authorization"]?.first)
        XCTAssertEqual(credentials, "Bearer fictitious-token")
        let body = try await client.capture(["body", recordID, "request_original", "0"])
        let context = CaptureSelectionContext(record: record, snapshotName: "request_original",
            body: try XCTUnwrap(body.body).data,
            field: CaptureFieldSelection(kind: "header", name: "Authorization"))
        let model = CaptureDraftModel(context: context) { args, input in
            try await client.rewrite(args, input: input)
        }
        await model.prepare()
        XCTAssertTrue(model.ready, model.error ?? "Draft did not prepare")
        XCTAssertEqual(model.rule.operations.first?.value, credentials)
        XCTAssertTrue(model.beforeText.contains(credentials))
        model.rule.id = "native-swift-capture-draft"
        model.rule.operations[0].name = "X-Rewritten"
        model.rule.operations[0].value = "native-swift-draft"
        model.enable = true
        await model.preview()
        XCTAssertNotNil(model.previewResult, model.error ?? "Preview failed")
        XCTAssertTrue(model.afterText.contains("native-swift-draft"))
        await model.save()
        XCTAssertTrue(model.saved, model.error ?? "Save failed")
        let saved = try await client.rewriteExport(["export", "-"])
        XCTAssertTrue(String(decoding: saved, as: UTF8.self).contains("native-swift-draft"))
        if env["NAGI_CAPTURE_ACCEPTANCE_ACTIVATE"] == "1" {
            _ = try await client.rewrite(["enable"])
        }
    }
}
