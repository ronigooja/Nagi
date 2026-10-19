import XCTest
@testable import NagiApp

final class LocalizationTests: XCTestCase {
    private func bundle(_ language: String) throws -> Bundle {
        try XCTUnwrap(NagiResources.bundle.url(forResource: language.lowercased(), withExtension: "lproj").flatMap(Bundle.init(url:)))
    }

    func testSharedContractAndCatalogParity() throws {
        let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let bytes = try Data(contentsOf: root.appendingPathComponent("docs/design/localization-contract.json"))
        let fixture = try XCTUnwrap(JSONSerialization.jsonObject(with: bytes) as? [String: Any])
        let ids = try XCTUnwrap(fixture["initial_ids"] as? [String: [String: Any]])
        for (id, spec) in ids {
            let signature = try XCTUnwrap(spec["args"] as? [String: String])
            let descriptor = MessageDescriptor(id: id, args: signature.mapValues { _ in .string("example") })
            for language in ["en", "zh-Hans"] {
                var expected = try XCTUnwrap(spec[language] as? String)
                for name in signature.keys { expected = expected.replacingOccurrences(of: "{\(name)}", with: "example") }
                XCTAssertEqual(descriptor.render(original: "ORIGINAL", bundle: try bundle(language)), expected)
            }
        }
        let invalid = try XCTUnwrap(fixture["invalid_descriptors"] as? [[String: Any]])
        for item in invalid {
            guard let raw = item["descriptor"] else { continue }
            let data = try JSONSerialization.data(withJSONObject: raw)
            if let descriptor = try? JSONDecoder().decode(MessageDescriptor.self, from: data) {
                XCTAssertEqual(descriptor.render(original: "ORIGINAL"), "ORIGINAL")
            }
        }
        var keys: Set<String>?
        for language in ["en", "zh-Hans"] {
            let url = try XCTUnwrap(try bundle(language).url(forResource: "Localizable", withExtension: "strings"))
            let dictionary = try XCTUnwrap(NSDictionary(contentsOf: url) as? [String: String])
            if let keys { XCTAssertEqual(Set(dictionary.keys), keys) }
            keys = Set(dictionary.keys)
        }
    }

    func testInstalledBundleWinsAfterRelocationWithoutDevelopmentFallback() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-relocated-\(UUID().uuidString)")
        addTeardownBlock { try? FileManager.default.removeItem(at: root) }
        let contents = root.appendingPathComponent("Nagi.app/Contents")
        let resources = contents.appendingPathComponent("Resources")
        try FileManager.default.createDirectory(at: resources, withIntermediateDirectories: true)
        try PropertyListSerialization.data(fromPropertyList: ["CFBundleIdentifier": "com.nagi.localization-test", "CFBundlePackageType": "APPL"], format: .xml, options: 0).write(to: contents.appendingPathComponent("Info.plist"))
        try FileManager.default.copyItem(at: NagiResources.bundle.bundleURL, to: resources.appendingPathComponent("NagiApp_NagiApp.bundle"))
        let main = try XCTUnwrap(Bundle(url: root.appendingPathComponent("Nagi.app")))
        func forbiddenFallback() -> Bundle { XCTFail("Installed App accessed build-directory fallback"); return .main }
        let installed = NagiResources.resolve(mainBundle: main, developmentBundle: forbiddenFallback())
        XCTAssertTrue(installed.bundleURL.path.hasPrefix(root.path))
        XCTAssertNotNil(installed.url(forResource: "NagiMenuTemplate", withExtension: "pdf"))
        let english = try XCTUnwrap(Bundle(url: installed.bundleURL.appendingPathComponent("en.lproj")))
        XCTAssertEqual(L10n.text("app.cancel", bundle: english), "Cancel")
        let chinese = try XCTUnwrap(Bundle(url: installed.bundleURL.appendingPathComponent("zh-hans.lproj")))
        XCTAssertEqual(L10n.text("app.ui.save_rule", bundle: english), "Save Rule")
        XCTAssertEqual(L10n.text("app.ui.save_rule", bundle: chinese), "保存规则")
        XCTAssertEqual(L10n.format("app.capture.records_count", Int64(2), bundle: english), "2 records")
        XCTAssertEqual(L10n.format("app.capture.records_count", Int64(2), bundle: chinese), "2 条记录")
        XCTAssertNotNil(installed.url(forResource: "MessageSignatures", withExtension: "json"))
        XCTAssertEqual(MessageDescriptor(id: "cli.language.duplicate").render(original: "original", bundle: english), "--lang may be specified only once.")
        XCTAssertEqual(MessageDescriptor(id: "cli.language.duplicate").render(original: "original", bundle: chinese), "--lang 只能指定一次。")
    }

    func testNativeCatalogueAndNamedArgumentsAcrossLanguages() throws {
        let message = MessageDescriptor(id: "cli.language.invalid", args: ["value": .string("fr")])
        XCTAssertEqual(message.render(original: "fallback", bundle: try bundle("en")), "Unsupported language fr; use en or zh-Hans.")
        XCTAssertEqual(message.render(original: "fallback", bundle: try bundle("zh-Hans")), "不支持语言 fr；请使用 en 或 zh-Hans。")
        XCTAssertEqual(MessageDescriptor(id: "unknown").render(original: "fallback"), "fallback")
        XCTAssertEqual(MessageDescriptor(id: message.id, args: ["value": .number(3)]).render(original: "fallback"), "fallback")
        XCTAssertEqual(MessageDescriptor(id: message.id, args: ["value": .string("fr"), "extra": .null]).render(original: "fallback"), "fallback")
    }

    func testNativePluralRules() throws {
        XCTAssertEqual(L10n.format("app.items.count", Int64(1), bundle: try bundle("en")), "1 item")
        XCTAssertEqual(L10n.format("app.items.count", Int64(2), bundle: try bundle("en")), "2 items")
        XCTAssertEqual(L10n.format("app.items.count", Int64(2), bundle: try bundle("zh-Hans")), "2 项")
    }

    func testOptionalMetadataCannotBreakValidResponse() throws {
        for metadata in ["null", "42", "[]", "{\"/data/message\":42}", "{\"/data/message\":{\"id\":\"x\",\"args\":{\"bad\":[]}}}"] {
            let data = Data("{\"ok\":true,\"data\":{\"message\":\"original\"},\"messages\":\(metadata)}".utf8)
            let envelope = try JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: data)
            XCTAssertEqual(envelope.data?["message"]?.string, "original")
            XCTAssertEqual(envelope.data?["message"]?.displayString, "original")
        }
    }

    func testPointersAreExactAndOriginalBusinessValuesStayAvailable() throws {
        let bytes = Data(#"{"ok":true,"data":{"a/b~c":["original"],"number":4},"messages":{"/data/a~1b~0c/0":{"id":"cli.language.missing"},"/data/number":{"id":"cli.language.missing"},"/data/absent":{"id":"cli.language.missing"},"/data/~2invalid":{"id":"cli.language.missing"},"/data/bad":{"id":4}}}"#.utf8)
        let envelope = try JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: bytes)
        let message = try XCTUnwrap(envelope.data?["a/b~c"]?.array.first)
        XCTAssertEqual(message.string, "original")
        XCTAssertNotEqual(message.displayString, "original")
        XCTAssertEqual(envelope.data?["number"]?.int64, 4)
        XCTAssertNil(envelope.messages.values["/data/~2invalid"])
        XCTAssertNil(envelope.messages.values["/data/bad"])
    }

    private func fixture(_ output: String, stderr: Bool = false, status: Int = 0) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-i18n-\(UUID().uuidString).sh")
        let quoted = output.replacingOccurrences(of: "'", with: "'\"'\"'")
        try ("#!/bin/sh\nprintf '%s\\n' '" + quoted + "'" + (stderr ? " >&2" : "") + "\nexit \(status)\n").write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
        addTeardownBlock { try? FileManager.default.removeItem(at: url) }
        return url
    }

    func testNormalRewriteCaptureAndTrafficTransportsRetainDescriptors() async throws {
        let json = #"{"ok":true,"data":{"message":"original"},"messages":{"/data/message":{"id":"cli.language.missing"}}}"#
        let executable = try fixture(json)
        let normal = try await NagiCLIClient(executable: executable).run(["status"])
        let rewrite = try await NagiCLIClient(executable: executable).rewrite(["status"])
        for result in [normal, rewrite] {
            XCTAssertEqual(result["message"]?.string, "original")
            XCTAssertNotEqual(result["message"]?.displayString, "original")
        }
        let sample = expectation(description: "localized stream sample")
        let stream = try NagiCLIClient(executable: executable).trafficStream { value in
            XCTAssertEqual(value["message"]?.string, "original")
            XCTAssertNotEqual(value["message"]?.displayString, "original")
            sample.fulfill()
        }
        try await stream.wait()
        await fulfillment(of: [sample], timeout: 3)
        let captureJSON = #"{"ok":true,"data":{"sensitive":true,"readiness":{"ready":false,"message":"original"}},"messages":{"/data/readiness/message":{"id":"cli.language.missing"}}}"#
        let capture = try await NagiCLIClient(executable: fixture(captureJSON)).capture(["status"])
        XCTAssertEqual(capture.readiness?.message, "original")
        XCTAssertNotEqual(capture.display("original", at: "/data/readiness/message"), "original")
    }

    func testAllFailureTransportsRetainOriginalMessageAndCode() async throws {
        let json = #"{"ok":false,"error":{"code":"scheduler_already_running","message":"original"},"messages":{"/error/message":{"id":"cli.language.missing"}}}"#
        let executable = try fixture(json, stderr: true, status: 1)
        let client = NagiCLIClient(executable: executable)
        for operation in [0, 1, 2] {
            do {
                if operation == 0 { _ = try await client.run(["status"]) }
                else if operation == 1 { _ = try await client.rewrite(["status"]) }
                else { _ = try await client.capture(["status"]) }
                XCTFail("Expected failure")
            } catch CLIError.localizedCommand(let code, let original, let descriptor) {
                XCTAssertEqual(code, "scheduler_already_running")
                XCTAssertEqual(original, "original")
                XCTAssertEqual(descriptor.id, "cli.language.missing")
            }
        }
        let traffic = try client.trafficStream { _ in XCTFail("Unexpected sample") }
        do { try await traffic.wait(); XCTFail("Expected failure") }
        catch CLIError.localizedCommand(let code, let original, _) {
            XCTAssertEqual(code, "scheduler_already_running"); XCTAssertEqual(original, "original")
        }
        let watch = try client.subscriptionWatch()
        guard case .duplicate = await watch.wait() else { return XCTFail("Localized scheduler code changed control flow") }
    }

    func testCaptureAndErrorMetadataPreserved() throws {
        let data = Data(#"{"ok":true,"data":{"sensitive":true,"readiness":{"ready":false,"message":"original"}},"messages":{"/data/readiness/message":{"id":"cli.language.missing"}}}"#.utf8)
        let capture = try XCTUnwrap(JSONDecoder().decode(CLIEnvelope<CaptureReply>.self, from: data).data)
        XCTAssertEqual(capture.readiness?.message, "original")
        XCTAssertNotEqual(capture.display("original", at: "/data/readiness/message"), "original")
        let errorData = Data(#"{"ok":false,"error":{"code":"invalid_argument","message":"original"},"messages":{"/error/message":{"id":"cli.language.invalid","args":{"value":"fr"}}}}"#.utf8)
        let error = try JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: errorData).commandError(fallbackCode: "fallback", fallbackMessage: "fallback")
        XCTAssertEqual(error.commandCode, "invalid_argument")
        guard case .localizedCommand(_, let original, _) = error else { return XCTFail("metadata lost") }
        XCTAssertEqual(original, "original")
    }
}
