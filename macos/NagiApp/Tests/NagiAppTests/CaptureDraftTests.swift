import XCTest
import AppKit
@testable import NagiApp

final class CaptureDraftIntentTests: XCTestCase {
    static func context(snapshot: String = "request_original", field: CaptureFieldSelection? = nil, body: String = "{\"a/b\":{\"~number\":9007199254740993123456789}}", websocket: Bool = false) throws -> CaptureSelectionContext {
        let record = try JSONDecoder().decode(CaptureRecord.self, from: Data("""
        {"id":"record-example","session_id":"session-example","kind":"\(websocket ? "websocket" : "http")","generation":"generation-example","rule_version":"captured-version","started":"2026-10-18T00:00:00Z","outcome":"complete",\(websocket ? "\"stage\":\"websocket_server\",\"parent_id\":\"handshake-example\"," : "")"identity":{"session_id":"session-example","revision":1,"uid":501,"process":"Example","process_path":"/Applications/Example.app/Example","source":"127.0.0.1","destination":"api.service.example"},"duration_ms":1,"snapshots":{"request_original":{"scheme":"https","host":"api.service.example","port":443,"method":"POST","path":"/v1/a%2Fb","query":"token=fictitious-example","headers":{"Authorization":["Bearer fictitious-example"],"X-Repeated":["one","two"]},"trailers":{"X-Trailer":["fictitious-trailer"]},"body_state":"complete","body_size":0,"captured":0},"response_original":{"status":201,"headers":{},"body_state":"complete","body_size":0,"captured":0}}}
        """.utf8))
        return CaptureSelectionContext(record: record, snapshotName: snapshot, body: Data(body.utf8), field: field)
    }
    func testRawCredentialsRepeatedHeaderAndTrailerMapping() throws {
        let auth = CaptureDraftIntent(try Self.context(field: CaptureFieldSelection(kind: "header", name: "Authorization", value: "ignored")))
        XCTAssertEqual(auth.rule.operations[0].value, "Bearer fictitious-example")
        XCTAssertFalse(auth.rule.enabled)
        let repeated = CaptureDraftIntent(try Self.context(field: CaptureFieldSelection(kind: "header", name: "X-Repeated")))
        XCTAssertEqual(repeated.rule.operations.map(\.kind), ["header_set", "header_append"])
        XCTAssertEqual(repeated.rule.operations.map(\.value), ["one", "two"])
        let trailer = CaptureDraftIntent(try Self.context(field: CaptureFieldSelection(kind: "trailer", name: "X-Trailer")))
        XCTAssertEqual(trailer.rule.operations[0].kind, "trailer_set")
        XCTAssertEqual(trailer.rule.operations[0].value, "fictitious-trailer")
    }
    func testPreciseJSONPointerRawNumberAndEscapes() throws {
        let context = try Self.context(field: CaptureFieldSelection(kind: "json", pointer: "/a~1b/~0number"))
        let draft = CaptureDraftIntent(context)
        XCTAssertEqual(draft.rule.operations[0].value, "9007199254740993123456789")
        let data = try RewriteJSON.parse(draft.data())
        XCTAssertEqual(data["operations"]?.array[0]["value"], .scalar("9007199254740993123456789"))
        XCTAssertNil(CaptureDraftIntent.jsonValue(context.body, pointer: "/a~2b"))
        XCTAssertNil(CaptureDraftIntent.jsonValue(Data("[1]".utf8), pointer: "/00"))
        XCTAssertNil(CaptureDraftIntent.jsonValue(Data("[1]".utf8), pointer: "/+0"))
    }
    func testURLQueryMethodStatusAndTextMapToEditableOperations() throws {
        let cases: [(CaptureFieldSelection, String, String)] = [
            (.init(kind: "url"), "url_set", "/v1/a%2Fb"),
            (.init(kind: "query", value: "token=fictitious-example"), "url_set", "token=fictitious-example"),
            (.init(kind: "method", value: "POST"), "method_set", "POST"),
            (.init(kind: "status", value: "201"), "status_set", "201")]
        for (field, kind, value) in cases {
            let operation = CaptureDraftIntent(try Self.context(field: field)).rule.operations[0]
            XCTAssertEqual(operation.kind, kind); XCTAssertEqual(operation.value, value)
        }
        let text = CaptureDraftIntent(try Self.context(field: CaptureFieldSelection(kind: "text", selectedText: "π secret-example"))).rule.operations[0]
        XCTAssertEqual(text.kind, "text_replace"); XCTAssertEqual(text.pattern, "π secret-example"); XCTAssertEqual(text.replacement, text.pattern)
        XCTAssertEqual(CaptureDraftIntent(try Self.context(snapshot: "response_actual")).rule.stage, "response")
        XCTAssertEqual(CaptureDraftIntent(try Self.context(snapshot: "response_actual", field: CaptureFieldSelection(kind: "url"))).rule.stage, "request")
        XCTAssertEqual(CaptureDraftIntent(try Self.context(websocket: true)).rule.stage, "websocket_server")
    }
}

@MainActor final class CaptureDraftModelTests: XCTestCase {
    func testDraftWindowFitsScreenAndPreservesResizedFrame() async throws {
        _ = NSApplication.shared
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, input in
            try self.response(args, input)
        }
        await model.prepare()
        XCTAssertTrue(model.ready, model.error ?? "")
        let controller = CaptureDraftWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        let screen = try XCTUnwrap(window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame)
        let initial = window.frame
        XCTAssertEqual(initial, CaptureDraftWindowController.layout.initialFrame(in: screen))
        XCTAssertEqual(window.minSize, CaptureDraftWindowController.layout.minimumFrameSize(in: screen))
        controller.present()
        let content = try XCTUnwrap(window.contentView)
        let minimum = CaptureDraftWindowController.layout.minimumFrameSize(in: NSRect(x: 0, y: 0, width: 960, height: 600))
        window.minSize = minimum
        func buttons(in view: NSView) -> [NSButton] {
            ((view as? NSButton).map { [$0] } ?? []) + view.subviews.flatMap { buttons(in: $0) }
        }
        for size in [minimum, initial.size, minimum] {
            window.setFrame(NSRect(origin: initial.origin, size: size), display: true)
            for _ in 0..<5 {
                content.layoutSubtreeIfNeeded()
                try await Task.sleep(nanoseconds: 20_000_000)
            }
            XCTAssertEqual(window.frame.size, size)
            for title in ["Cancel", "Preview…", "Save Rule"] {
                let button = try XCTUnwrap(buttons(in: content).first { $0.title == title })
                XCTAssertTrue(content.bounds.insetBy(dx: -1, dy: -1).contains(button.convert(button.bounds, to: content)), title)
            }
        }
        // Ordering out keeps the draft session alive, unlike closing it.
        window.orderOut(nil)
        controller.present()
        content.layoutSubtreeIfNeeded()
        XCTAssertEqual(window.frame.size, minimum)
    }

    private func json(_ text: String) throws -> JSONValue { try JSONDecoder().decode(JSONValue.self, from: Data(text.utf8)) }
    private let fixture = #"{"stage":"exchange","request":{"scheme":"https","host":"api.service.example","port":443,"method":"POST","path":"/v1/a%2Fb","query":"token=fictitious-example","headers":{"Authorization":["Bearer fictitious-example"]},"body_base64":"b3JpZ2luYWw="},"response":{"status":201,"headers":{},"body_base64":""}}"#
    private func response(_ args: [String], _ input: Data?) throws -> JSONValue {
        if args == ["status"] { return try json(#"{"profile":"work","configured_rule_version":"version-at-open"}"#) }
        if args.first == "rules" { return try json(#"{"rules":[{"id":"first"},{"id":"last"}]}"#) }
        if args.prefix(2) == ["capture", "fixture"] { return try json(fixture) }
        if args.prefix(2) == ["capture", "draft"] {
            let selection = try RewriteJSON.parse(XCTUnwrap(input))
            return try json("""
            {"base_rule_version":"version-at-open","position":2,"enable":false,"rule":{"id":\(selection["id"]!.text),"stage":"request","enabled":false,"match":{"domain":"api.service.example","methods":["POST"],"path_regex":"^/v1/a%2Fb$"},"range_mode":"fragment","on_error":"block","redirect_consent":[],"operations":\(selection["operations"]!.text)}}
            """)
        }
        return try json(fixture)
    }
    func testPreparePinsVersionProfileAndUsesCompleteOriginalExchange() async throws {
        var calls: [[String]] = []
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context(field: CaptureFieldSelection(kind: "json", pointer: "/a~1b/~0number"))) { args, input in
            calls.append(args); return try self.response(args, input)
        }
        await model.prepare()
        XCTAssertTrue(model.ready, model.error ?? "")
        XCTAssertEqual(model.profile, "work"); XCTAssertEqual(model.baseRuleVersion, "version-at-open")
        XCTAssertFalse(model.enable); XCTAssertEqual(model.orderedRules, ["first", "last"])
        XCTAssertEqual(calls.last, ["capture", "fixture", "record-example", "exchange"])
        let doc = try RewriteJSON.parse(model.document())
        XCTAssertEqual(doc["rule"]?["operations"]?.array[0]["value"], .scalar("9007199254740993123456789"))
        XCTAssertEqual(doc["fixture"]?["request"]?["port"], .scalar("443"))
        XCTAssertEqual(doc["fixture"]?["response"]?["status"], .scalar("201"))
        XCTAssertTrue(model.beforeText.contains("Bearer fictitious-example")); XCTAssertTrue(model.beforeText.contains("original"))
    }
    func testPreviewSaveOrderEnableAndConflictRetainDraftWithoutRetry() async throws {
        var mutations: [([String], Data?)] = []
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, input in
            if args.first == "draft" {
                mutations.append((args, input))
                if args[1] == "save" { throw CLIError.command("request_rewrite_error", "rewrite_conflict") }
            }
            return try self.response(args, input)
        }
        await model.prepare()
        let base = model.baseRuleVersion
        model.rule.operations.append(RewriteOperationDraft())
        model.rule.operations.swapAt(0, 1); model.position = "1"; model.enable = true
        let input = try model.document()
        await model.preview()
        XCTAssertNotNil(model.previewResult)
        XCTAssertEqual(mutations[0].0, ["draft", "preview", "-", "--profile", "work"])
        model.rule.operations[0].name = "X-Edited"
        XCTAssertNil(model.previewResult)
        let edited = try model.document()
        XCTAssertNotEqual(input, edited)
        await model.save()
        XCTAssertFalse(model.saved); XCTAssertTrue(model.ready)
        XCTAssertEqual(model.baseRuleVersion, base); XCTAssertEqual(try model.document(), edited)
        XCTAssertEqual(mutations.count, 2)
        XCTAssertEqual(mutations[1].0, ["draft", "save", "-", "--profile", "work"])
        XCTAssertTrue(model.error?.contains("rewrite_conflict") == true)
    }
    func testIncompleteFixtureStillAllowsMetadataSaveWithoutInventingBody() async throws {
        var saved: Data?, previewCalls = 0
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context(field: CaptureFieldSelection(kind: "header", name: "Authorization"))) { args, input in
            if args.prefix(2) == ["capture", "fixture"] { throw CLIError.command("capture_error", "rewrite_invalid") }
            if args.prefix(2) == ["draft", "preview"] { previewCalls += 1 }
            if args.prefix(2) == ["draft", "save"] { saved = input }
            return try self.response(args, input)
        }
        await model.prepare()
        XCTAssertTrue(model.ready); XCTAssertNil(model.fixture)
        await model.preview(); XCTAssertEqual(previewCalls, 0)
        await model.save(); XCTAssertTrue(model.saved)
        XCTAssertNil(try RewriteJSON.parse(XCTUnwrap(saved))["fixture"])
    }
    func testCancelDrainsPreparationAndDoesNotSaveOrAcceptLateResult() async throws {
        var entered = false, cancelled = false, saves = 0
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, _ in
            if args.first == "draft" { saves += 1 }
            entered = true
            do { try await Task.sleep(nanoseconds: 10_000_000_000) } catch { cancelled = true; throw error }
            return try self.json(#"{"profile":"late","configured_rule_version":"late"}"#)
        }
        let preparation = Task { await model.prepare() }
        for _ in 0..<100 { if entered { break }; try await Task.sleep(nanoseconds: 1_000_000) }
        model.cancel(); await preparation.value
        XCTAssertTrue(cancelled); XCTAssertNil(model.baseRuleVersion); XCTAssertFalse(model.ready)
        await model.save(); XCTAssertEqual(saves, 0)
    }
    func testProfileChangeDuringPreparationRejectsDraft() async throws {
        var statuses = 0
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, input in
            if args == ["status"] { statuses += 1; if statuses == 2 { return try self.json(#"{"profile":"other","configured_rule_version":"version-at-open"}"#) } }
            return try self.response(args, input)
        }
        await model.prepare()
        XCTAssertFalse(model.ready); XCTAssertNil(model.baseRuleVersion)
        XCTAssertTrue(model.error?.contains("profile or rules changed") == true)
    }


    func testStageChangeWithoutResponseCannotPreviewWrongStage() async throws {
        var previews = 0
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, input in
            if args.prefix(2) == ["capture", "fixture"] {
                return try self.json(#"{"stage":"request","request":{"scheme":"https","host":"api.service.example","port":443,"method":"POST","path":"/","query":"","headers":{},"body_base64":""}}"#)
            }
            if args.prefix(2) == ["draft", "preview"] { previews += 1 }
            return try self.response(args, input)
        }
        await model.prepare(); XCTAssertTrue(model.canPreview)
        model.rule.stage = "response"
        XCTAssertFalse(model.canPreview)
        await model.preview(); XCTAssertEqual(previews, 0)
        model.rule.stage = "request"; XCTAssertTrue(model.canPreview)
    }
    func testSubmittedSaveBlocksClosingAndCompletesOnce() async throws {
        _ = NSApplication.shared
        var release: CheckedContinuation<Void, Never>?, saves = 0
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, input in
            if args.prefix(2) == ["draft", "save"] {
                saves += 1
                await withCheckedContinuation { release = $0 }
            }
            return try self.response(args, input)
        }
        await model.prepare()
        let controller = CaptureDraftWindowController(model: model)
        let save = Task { await model.save() }
        for _ in 0..<100 { if release != nil { break }; try await Task.sleep(nanoseconds: 1_000_000) }
        XCTAssertTrue(model.saving)
        XCTAssertFalse(controller.windowShouldClose(try XCTUnwrap(controller.window)))
        model.cancel()
        release?.resume(); await save.value
        XCTAssertTrue(model.saved); XCTAssertEqual(saves, 1)
        XCTAssertTrue(controller.windowShouldClose(try XCTUnwrap(controller.window)))
        controller.close()
    }
    func testCancelledPreviewRejectsLateResultAndNeverSaves() async throws {
        var entered = false, cancelled = false
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { args, input in
            if args.prefix(2) == ["draft", "preview"] {
                entered = true
                do { try await Task.sleep(nanoseconds: 10_000_000_000) } catch { cancelled = true; throw error }
            }
            return try self.response(args, input)
        }
        await model.prepare()
        let preview = Task { await model.preview() }
        for _ in 0..<100 { if entered { break }; try await Task.sleep(nanoseconds: 1_000_000) }
        model.cancel(); await preview.value
        XCTAssertTrue(cancelled); XCTAssertNil(model.previewResult); XCTAssertFalse(model.saved)
    }
    func testClosingDraftWindowDoesNotSubmit() throws {
        _ = NSApplication.shared
        var calls = 0
        let model = CaptureDraftModel(context: try CaptureDraftIntentTests.context()) { _, _ in calls += 1; return try self.json("{}") }
        let controller = CaptureDraftWindowController(model: model)
        controller.close()
        XCTAssertEqual(calls, 0); XCTAssertFalse(model.saved)
    }
}
