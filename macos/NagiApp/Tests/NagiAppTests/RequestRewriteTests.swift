import XCTest
import Foundation
@testable import NagiApp

final class RequestRewriteDocumentTests: XCTestCase {
    func testLosslessRuleRoundTripRetainsLargeNumberAndExactOperationFields() throws {
        let bytes = Data(#"{"id":"example","enabled":true,"stage":"request","range_mode":"fragment","match":{"domain":"api.service.example","methods":["POST"],"path_prefix":"/v1/"},"on_error":"block","redirect_consent":[{"domain":"mirror.service.example","schemes":["https"],"ports":[443]}],"operations":[{"op":"json_set","pointer":"/number","value":9007199254740993123456789},{"op":"json_delete","pointer":"/obsolete"},{"op":"header_set","name":"Authorization","value":"fictitious-placeholder"},{"op":"url_set","component":"host","value":"mirror.service.example"}]}"#.utf8)
        let original = try RewriteJSON.parse(bytes)
        let draft = RewriteRuleDraft(original)
        let result = try RewriteJSON.parse(draft.json())
        XCTAssertEqual(result, original)
        XCTAssertEqual(draft.operations[0].value, "9007199254740993123456789")
        XCTAssertNil(result["operations"]?.array[1]["value"])
        XCTAssertNil(result["operations"]?.array[1]["name"])
    }
    func testJSONSyntaxRejectsDuplicatesTrailingValuesMalformedNumbersAndEscapes() {
        for fixture in [#"{"a":1,"a":2}"#, "true false", "01", "1.", #""\z""#, "[1,]", "{\"a\":1,}", String(repeating: "[", count: 130) + "0" + String(repeating: "]", count: 130)] {
            XCTAssertThrowsError(try RewriteJSON.parse(Data(fixture.utf8)), fixture)
        }
    }
    func testStringsAndJSONNullAreDistinctAndEscapedWithoutArguments() throws {
        var operation = RewriteOperationDraft(); operation.kind = "header_set"; operation.name = "X-Demo"; operation.value = "a\"b\nexample"
        XCTAssertEqual(try RewriteJSON.parse(operation.json().data)["value"]?.string, operation.value)
        operation.kind = "json_set"; operation.pointer = "/null"; operation.value = "null"
        XCTAssertEqual(try operation.json()["value"], .scalar("null"))
    }
    func testPreviewFormUsesCompleteFixtureAndRepeatedHeadersAndBase64() throws {
        var draft = RewritePreviewDraft(); draft.host = "api.service.example"; draft.headers = "X-Demo: one\nX-Demo: two"; draft.body = "π"
        let fixture = try RewriteJSON.parse(draft.data())
        XCTAssertEqual(fixture["stage"]?.string, "request")
        let request = fixture["request"]
        XCTAssertEqual(request?["headers"]?["X-Demo"]?.array, [.string("one"), .string("two")])
        XCTAssertEqual(request?["body_base64"]?.string, Data("π".utf8).base64EncodedString())
        if case .object(let fields)? = request { XCTAssertEqual(Set(fields.keys), Set(["scheme", "host", "port", "method", "path", "query", "headers", "body_base64"])) }
        else { XCTFail("Expected request object") }
        draft.port = "0"; XCTAssertThrowsError(try draft.data())
    }
    func testUnknownRuntimeEvidenceNeverEnablesFeature() throws {
        let value = try JSONDecoder().decode(JSONValue.self, from: Data(#"{"profile":"work","enabled_intent":true,"actual":"unavailable","tun_running":null,"service_running":null,"protocol_version":null}"#.utf8))
        let status = RewriteStatus(value: value)
        XCTAssertFalse(status.canEnable); XCTAssertTrue(status.detail.contains("TUN running: unknown"))
        XCTAssertTrue(status.detail.contains("Protocol: unknown"))
    }
}

@MainActor final class RequestRewriteModelTests: XCTestCase {
    private func json(_ text: String) throws -> JSONValue { try JSONDecoder().decode(JSONValue.self, from: Data(text.utf8)) }
    private func ready(_ arguments: [String]) throws -> JSONValue {
        if arguments == ["status"] { return try json(#"{"profile":"work","enabled_intent":false,"actual":"disabled","tun_running":true}"#) }
        if arguments.first == "scripts" { return try json(#"{"scripts":[{"id":"api","body_access":"none"}]}"#) }
        return try json(#"{"rules":[{"id":"rule","enabled":true,"stage":"request","range_mode":"fragment","match":{"domain":"api.service.example","methods":["GET"],"path_prefix":"/"},"on_error":"block","operations":["header_set"]}]}"#)
    }
    private func eventually(_ predicate: () -> Bool) async throws {
        for _ in 0..<200 { if predicate() { return }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTFail("Condition did not become true")
    }
    func testSingleReadSlotCancellationAndReopenRejectObsoleteResult() async throws {
        var calls = 0, cancelled = 0
        let model = RequestRewriteModel(read: { args, _ in
            calls += 1
            if calls == 1 {
                do { try await Task.sleep(nanoseconds: 10_000_000_000) }
                catch { cancelled += 1; throw error }
            }
            return try self.ready(args)
        }, submit: { _, _ in try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true)
        try await eventually { calls == 1 }
        for _ in 0..<10 { model.refresh() }
        XCTAssertEqual(calls, 1)
        model.setVisible(false); model.setVisible(true)
        try await eventually { model.canManage }
        XCTAssertEqual(cancelled, 1); XCTAssertEqual(model.rules.map(\.id), ["rule"])
        model.setVisible(false); await model.waitForCancelledRead()
    }
    func testFailureDisablesStaleSelectionButRecoveryDisableRemainsAvailable() async throws {
        var fail = false, mutations: [[String]] = []
        let model = RequestRewriteModel(read: { args, _ in
            if fail { throw CLIError.command("request_rewrite_error", "rewrite_state_uncertain") }
            return try self.ready(args)
        }, submit: { args, _ in mutations.append(args); return try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true); try await eventually { model.canManage }
        fail = true; model.refresh(); try await eventually { model.stale && !model.isReading }
        XCTAssertFalse(model.canManage); XCTAssertFalse(model.canEnable); XCTAssertTrue(model.canOperate)
        let removed = await model.perform(["rules", "remove", "rule"], profile: "work")
        XCTAssertFalse(removed); XCTAssertTrue(mutations.isEmpty)
        let disabled = await model.perform(["disable"])
        XCTAssertTrue(disabled); XCTAssertEqual(mutations, [["disable"]])
        model.setVisible(false); await model.waitForCancelledRead()
    }
    func testFailedMutationDispatchesOnceRetainsInputAndProfile() async throws {
        var submitted: [(args: [String], input: Data?)] = []
        let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { args, data in
            submitted.append((args, data)); throw CLIError.command("request_rewrite_error", "rewrite_invalid")
        }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true); try await eventually { model.canManage }
        let input = Data(#"{"id":"new","body_access":"none"}"#.utf8)
        let success = await model.perform(["scripts", "add", "-"], input: input, profile: "work")
        XCTAssertFalse(success); XCTAssertEqual(submitted.count, 1)
        XCTAssertEqual(submitted[0].args, ["scripts", "add", "-", "--profile", "work"])
        XCTAssertEqual(submitted[0].input, input); XCTAssertTrue(model.error?.contains("not retried") == true)
        model.setVisible(false); await model.waitForCancelledRead()
    }
    func testSensitiveEditReadsCancelAndDrainOnSleepSettingsAndClose() async throws {
        for gate in ["sleep", "settings", "close"] {
            var entered = false, cancelled = false
            let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { _, _ in try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
            model.exportRead = { args in
                XCTAssertEqual(args, ["export", "-", "--profile", "work"])
                entered = true
                do { try await Task.sleep(nanoseconds: 10_000_000_000) } catch { cancelled = true; throw error }
                return Data()
            }
            model.setVisible(true); try await eventually { model.canManage }
            let task = Task { try await model.sensitiveRule(id: "rule", profile: "work") }
            try await eventually { entered }
            switch gate { case "sleep": model.setSuspended(true); case "settings": model.setBlocked(true); default: model.setVisible(false) }
            await model.waitForCancelledRead()
            do { _ = try await task.value; XCTFail("Expected cancellation") } catch is CancellationError { } catch { XCTFail("Unexpected error: \(error)") }
            XCTAssertTrue(cancelled); XCTAssertFalse(model.sensitiveReadInProgress)
            model.setVisible(false)
        }
    }
}

@MainActor extension RequestRewriteModelTests {
    func testStatusOnlyProfileChangeCannotReusePreviousProfileLists() async throws {
        var profile = "work", submitted = 0
        let model = RequestRewriteModel(read: { args, _ in
            if args == ["status"] { return try self.json("{\"profile\":\"\(profile)\",\"enabled_intent\":false,\"actual\":\"disabled\",\"tun_running\":true}") }
            return try self.ready(args)
        }, submit: { _, _ in submitted += 1; return try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true); try await eventually { model.canManage }
        profile = "personal"
        model.refreshStatus(); try await eventually { model.status?.profile == "personal" && !model.isReading }
        XCTAssertFalse(model.canManage); XCTAssertTrue(model.rules.isEmpty); XCTAssertTrue(model.scripts.isEmpty)
        let result = await model.perform(["scripts", "remove", "api"], profile: "personal")
        XCTAssertFalse(result); XCTAssertEqual(submitted, 0)
        model.setVisible(false); await model.waitForCancelledRead()
    }
    func testClosingUnsubmittedDraftDoesNotDispatchMutation() async throws {
        var submitted = 0
        let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { _, _ in submitted += 1; return try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true); try await eventually { model.canManage }
        var draft = RewriteRuleDraft(); draft.id = "unsaved"; draft.domain = "api.service.example"
        draft.operations.append(RewriteOperationDraft())
        _ = try draft.json()
        model.setVisible(false); await model.waitForCancelledRead()
        XCTAssertEqual(submitted, 0)
    }
    func testSubmittedOperationSurvivesWindowCloseAndIsNeverRepeated() async throws {
        var submitted = 0
        var finish: CheckedContinuation<Void, Never>?
        let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { _, _ in
            submitted += 1
            await withCheckedContinuation { finish = $0 }
            return try self.json("{}")
        }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true); try await eventually { model.canManage }
        let task = Task { await model.perform(["disable"]) }
        try await eventually { submitted == 1 }
        model.setVisible(false)
        XCTAssertTrue(model.operationInProgress)
        finish?.resume()
        let success = await task.value
        XCTAssertTrue(success); XCTAssertEqual(submitted, 1)
        await model.waitForCancelledRead()
    }
}

@MainActor extension RequestRewriteModelTests {
    func testFileExportBlocksDuplicateSubmissionAndIsAwaitable() async throws {
        var exports = 0, mutations = 0
        var release: CheckedContinuation<Void, Never>?
        let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { _, _ in mutations += 1; return try self.json("{}") }, export: { args in
            XCTAssertEqual(args, ["export", "/private/fictitious-export.json", "--profile", "work"])
            exports += 1
            await withCheckedContinuation { release = $0 }
            return Data()
        }, interval: 60_000_000_000)
        let task = Task { try await model.exportFile(["export", "/private/fictitious-export.json", "--profile", "work"]) }
        try await eventually { exports == 1 }
        XCTAssertTrue(model.operationInProgress)
        let duplicate = await model.perform(["disable"])
        XCTAssertFalse(duplicate); XCTAssertEqual(mutations, 0)
        model.setSuspended(true)
        XCTAssertTrue(model.operationInProgress)
        release?.resume()
        try await task.value
        XCTAssertEqual(exports, 1); XCTAssertFalse(model.operationInProgress)
    }
    func testOnlyOneSensitiveEditExportCanStart() async throws {
        var exports = 0
        var release: CheckedContinuation<Data, Error>?
        let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { _, _ in try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.exportRead = { _ in exports += 1; return try await withCheckedThrowingContinuation { release = $0 } }
        model.setVisible(true); try await eventually { model.canManage }
        let first = Task { try await model.sensitiveRule(id: "rule", profile: "work") }
        try await eventually { exports == 1 }
        do { _ = try await model.sensitiveRule(id: "rule", profile: "work"); XCTFail("Duplicate export") } catch { }
        XCTAssertEqual(exports, 1)
        release?.resume(throwing: CancellationError())
        _ = try? await first.value
        model.setVisible(false); await model.waitForCancelledRead()
    }
}

extension RequestRewriteDocumentTests {
    func testV2RuleRetainsEveryMatchAndOperationField() throws {
        let bytes = Data(#"""
        {"id":"response","enabled":true,"stage":"websocket_server","range_mode":"full","match":{"domain":"*.service.example","methods":["GET"],"path_prefix":"/v2/","path_regex":"^/v2/","query":{"mode":["demo"]},"headers":{"X-Request":["one"]},"status_codes":[101],"content_types":["application/json"],"response_headers":{"X-Response":["two"]},"message_types":["text","binary"]},"on_error":"block","redirect_consent":[],"operations":[{"op":"body_set","value":"AA==","encoding":"base64","message_type":"binary"},{"op":"text_replace","pattern":"before","replacement":"after","all":true},{"op":"regex_replace","pattern":"a+","replacement":"b","all":false},{"op":"script","script":"example"}]}
        """#.utf8)
        let node = try RewriteJSON.parse(bytes)
        XCTAssertEqual(try RewriteJSON.parse(RewriteRuleDraft(node).json()), node)
        var operation = RewriteOperationDraft(); operation.kind = "status_set"; operation.value = "204"
        XCTAssertEqual(try operation.json(), .object(["op": .string("status_set"), "value": .scalar("204")]))
        operation.kind = "header_append"; operation.name = "X-Demo"; operation.value = "another"
        XCTAssertEqual(try operation.json()["value"]?.string, "another")
        operation.kind = "trailer_delete"
        XCTAssertNil(try operation.json()["value"])
        XCTAssertTrue(RewriteOperationDraft.kinds.contains("method_set"))
        XCTAssertTrue(RewriteOperationDraft.kinds.contains("trailer_set"))
    }
    func testScriptSourceIsSensitiveAndLimitedByDecodedUTF8Bytes() throws {
        var draft = RewriteScriptDraft(); draft.id = "example"; draft.bodyAccess = "buffer"
        let node = try RewriteJSON.parse(draft.json())
        XCTAssertEqual(node["body_access"]?.string, "buffer")
        XCTAssertEqual(RewriteScriptDraft(node).source, draft.source)
        draft.source = String(repeating: "π", count: 32_769)
        XCTAssertThrowsError(try draft.json())
    }
    func testPreviewResponseExchangeAndWebSocketEnvelopes() throws {
        for stage in ["response", "exchange", "websocket_client", "websocket_server"] {
            var draft = RewritePreviewDraft(); draft.stage = stage; draft.host = "api.service.example"
            draft.trailers = "X-Request-End: yes"; draft.responseTrailers = "X-Response-End: yes"
            draft.responseStatus = stage.hasPrefix("websocket_") ? "101" : "200"
            draft.responseBodyEncoding = "base64"; draft.responseBody = "AP+A"
            draft.messageType = "binary"; draft.messageBodyEncoding = "base64"; draft.messageBody = "AP8="
            let envelope = try RewriteJSON.parse(draft.data())
            XCTAssertEqual(envelope["stage"]?.string, stage)
            XCTAssertEqual(envelope["response"]?["body_base64"]?.string, "AP+A")
            XCTAssertEqual(envelope["request"]?["trailers"]?["X-Request-End"]?.array, [.string("yes")])
            if stage.hasPrefix("websocket_") { XCTAssertEqual(envelope["message"]?["body_base64"]?.string, "AP8=") }
            else { XCTAssertNil(envelope["message"]) }
        }
    }
    func testPreviewFormAcceptsTwoMaximumBodiesAndRejectsOversize() throws {
        var draft = RewritePreviewDraft(); draft.host = "api.service.example"; draft.stage = "exchange"
        draft.body = String(repeating: "a", count: 8 * 1_048_576)
        draft.responseBody = String(repeating: "b", count: 8 * 1_048_576)
        XCTAssertLessThanOrEqual(try draft.data().count, 24 * 1_048_576)
        draft.responseBody.append("b")
        XCTAssertThrowsError(try draft.data())
    }
}

@MainActor extension RequestRewriteModelTests {
    func testSensitiveScriptReadsRawSourceWithoutOrdinaryListDisclosure() async throws {
        let model = RequestRewriteModel(read: { args, _ in try self.ready(args) }, submit: { _, _ in try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.exportRead = { args in
            XCTAssertEqual(args, ["export", "-", "--profile", "work"])
            return Data(#"{"version":2,"enabled":false,"rules":[],"scripts":[{"id":"api","body_access":"none","source":"function transform(x){return x}"}]}"#.utf8)
        }
        model.setVisible(true); try await eventually { model.canManage }
        XCTAssertEqual(model.scripts.map(\.id), ["api"])
        let draft = try await model.sensitiveScript(id: "api", profile: "work")
        XCTAssertEqual(draft.source, "function transform(x){return x}")
        model.setVisible(false); await model.waitForCancelledRead()
    }
}

@MainActor extension RequestRewriteModelTests {
    func testEmptyEnabledRuleSetCanSaveIdleIntentWithoutTUN() async throws {
        let model = RequestRewriteModel(read: { args, _ in
            if args == ["status"] { return try self.json(#"{"profile":"work","enabled_intent":false,"actual":"disabled","tun_running":false,"config_version":2}"#) }
            return try self.json(args.first == "scripts" ? #"{"scripts":[]}"# : #"{"rules":[]}"#)
        }, submit: { _, _ in try self.json("{}") }, export: { _ in Data() }, interval: 60_000_000_000)
        model.setVisible(true); try await eventually { model.canManage }
        XCTAssertTrue(model.canEnable)
        model.setVisible(false); await model.waitForCancelledRead()
        XCTAssertFalse(model.canEnable)
    }
}
