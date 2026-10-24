import XCTest
import AppKit
@testable import NagiApp

final class LocalizationCoverageTests: XCTestCase {
    private func bundle(_ language: String) throws -> Bundle {
        try XCTUnwrap(Bundle(url: NagiResources.bundle.bundleURL.appendingPathComponent(language.lowercased() + ".lproj")))
    }
    private func strings(_ table: String, _ language: String) throws -> [String: String] {
        let url = try XCTUnwrap(try bundle(language).url(forResource: table, withExtension: "strings"))
        return try XCTUnwrap(NSDictionary(contentsOf: url) as? [String: String])
    }
    private func placeholders(_ value: String) -> [String] {
        let pattern = #"%(?:[0-9]+\$)?(?:@|lld)"#
        let expression = try! NSRegularExpression(pattern: pattern)
        let text = value as NSString
        return expression.matches(in: value, range: NSRange(location: 0, length: text.length)).map { text.substring(with: $0.range) }.sorted()
    }

    func testEveryNativeResourceHasMatchingPlaceholdersAndEverySourceKeyExists() throws {
        let english = try strings("Localizable", "en"), chinese = try strings("Localizable", "zh-Hans")
        XCTAssertEqual(Set(english.keys), Set(chinese.keys))
        for (key, value) in english {
            XCTAssertEqual(placeholders(value), placeholders(try XCTUnwrap(chinese[key])), key)
        }
        let source = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().appendingPathComponent("Sources/NagiApp")
        let pluralURL = try XCTUnwrap(try bundle("en").url(forResource: "Localizable", withExtension: "stringsdict"))
        let plurals = try XCTUnwrap(NSDictionary(contentsOf: pluralURL) as? [String: Any])
        let known = Set(english.keys).union(plurals.keys)
        let expression = try NSRegularExpression(pattern: #"L10n\.(?:text|format)\("([^"]+)""#)
        for url in try FileManager.default.contentsOfDirectory(at: source, includingPropertiesForKeys: nil) where url.pathExtension == "swift" {
            let text = try String(contentsOf: url) as NSString
            for match in expression.matches(in: text as String, range: NSRange(location: 0, length: text.length)) {
                XCTAssertTrue(known.contains(text.substring(with: match.range(at: 1))), url.lastPathComponent + ": " + text.substring(with: match.range))
            }
        }
    }

    func testChineseResourcesTranslateAllNagiProseWithExplicitProtocolExceptions() throws {
        let exceptions: [String: Set<String>] = [
            "Localizable": ["app.ui.http", "app.ui.nagi_2", "app.browser.chrome"], // HTTP/product plus placeholders only.
            "MessageCatalog": ["cli.error.validation.external", "cli.output.writerewrite.ca"] // External cause only; CA acronym.
        ]
        for table in ["Localizable", "MessageCatalog"] {
            let english = try strings(table, "en"), chinese = try strings(table, "zh-Hans")
            for (key, value) in english where value == chinese[key] {
                XCTAssertTrue(exceptions[table]?.contains(key) == true, key + ": untranslated Nagi prose")
            }
        }
    }

    func testAllRegisteredCLIAndBackendDescriptorsRenderWithExactNamedSignatures() throws {
        let english = try strings("MessageCatalog", "en"), chinese = try strings("MessageCatalog", "zh-Hans")
        XCTAssertEqual(Set(english.keys), Set(chinese.keys))
        XCTAssertEqual(Set(english.keys), Set(NativeMessageCatalog.signatures.keys))
        for (id, signature) in NativeMessageCatalog.signatures {
            var args: [String: MessageArgument] = [:]
            for (name, kind) in signature {
                switch kind {
                case .string: args[name] = .string("fictitious-value%@中文")
                case .number: args[name] = .number(12.5)
                case .boolean: args[name] = .bool(true)
                }
            }
            for language in ["en", "zh-Hans"] {
                let rendered = MessageDescriptor(id: id, args: args).render(original: "UNRENDERED", bundle: try bundle(language))
                XCTAssertNotEqual(rendered, "UNRENDERED", id)
                XCTAssertFalse(rendered.contains("%1$@"), id)
            }
            var extra = args; extra["unexpected"] = .string("extra")
            XCTAssertEqual(MessageDescriptor(id: id, args: extra).render(original: "ORIGINAL"), "ORIGINAL", id)
            if let name = signature.keys.first {
                var missing = args; missing.removeValue(forKey: name)
                XCTAssertEqual(MessageDescriptor(id: id, args: missing).render(original: "ORIGINAL"), "ORIGINAL", id)
                var invalid = args; invalid[name] = .null
                XCTAssertEqual(MessageDescriptor(id: id, args: invalid).render(original: "ORIGINAL"), "ORIGINAL", id)
            }
            XCTAssertEqual(placeholders(try XCTUnwrap(english[id])), placeholders(try XCTUnwrap(chinese[id])), id)
        }
    }

    func testNativePluralCountersAndProtocolLabelsInBothLanguages() throws {
        let english = try bundle("en"), chinese = try bundle("zh-Hans")
        XCTAssertEqual(L10n.format("app.capture.records_count", Int64(1), bundle: english), "1 record")
        XCTAssertEqual(L10n.format("app.capture.records_count", Int64(2), bundle: english), "2 records")
        XCTAssertEqual(L10n.format("app.capture.records_count", Int64(2), bundle: chinese), "2 条记录")
        XCTAssertEqual(L10n.format("app.connections.count", Int64(1), Int64(1), bundle: english), "1 / 1 connection")
        XCTAssertEqual(L10n.format("app.connections.count", Int64(1), Int64(2), bundle: chinese), "1 / 2 个连接")
        XCTAssertEqual(L10n.protocolLabel("request_actual", bundle: chinese), "实际请求")
        XCTAssertEqual(L10n.protocolLabel("recording", bundle: chinese), "记录中")
        XCTAssertEqual(L10n.protocolLabel("rule", bundle: chinese), "规则")
        XCTAssertEqual(L10n.protocolLabel("unsupported-new-state", bundle: chinese), "unsupported-new-state")
        XCTAssertEqual(CaptureBodyFormat.raw.rawValue, "Raw")
        XCTAssertEqual(CaptureDetailSection.overview.rawValue, "overview")
    }

    func testBrowserSettingsResourcesExplainScopeAndRestartInBothLanguages() throws {
        let expected: [String: (menu: String, enable: String)] = [
            "en": ("Browser / WebRTC…", "Disable Non-Proxied UDP"),
            "zh-Hans": ("浏览器 / WebRTC…", "禁用非代理 UDP")
        ]
        for (language, text) in expected {
            let resources = try bundle(language)
            XCTAssertEqual(L10n.text("app.browser.menu", bundle: resources), text.menu)
            XCTAssertEqual(L10n.text("app.browser.enable", bundle: resources), text.enable)
            for key in ["app.browser.help", "app.browser.restart", "app.browser.forced", "app.browser.saved"] {
                XCTAssertNotEqual(L10n.text(key, bundle: resources), key)
            }
            for key in ["app.browser.load_failed", "app.browser.save_failed", "app.browser.other_policy"] {
                let rendered = L10n.format(key, "fictitious-policy", bundle: resources)
                XCTAssertTrue(rendered.contains("fictitious-policy"))
                XCTAssertFalse(rendered.contains("%1$@"))
            }
        }
    }

    func testOriginalNamesAndCapturedBodiesAreNeverLocalizationKeys() throws {
        let raw = "Start\n运行中\napp.ui.start\nAuthorization: Bearer fictitious-example%@"
        XCTAssertEqual(captureBodyText(Data(raw.utf8), format: .raw), raw)
        XCTAssertEqual(JSONValue.stringValue(raw).displayString, raw)
        XCTAssertEqual(JSONValue.stringValue("Running").string, "Running")
        let value = try JSONDecoder().decode(CLIEnvelope<JSONValue>.self, from: Data(#"{"ok":true,"data":{"profile":"Running","node":"DIRECT","message":"English fallback"},"messages":{"/data/message":{"id":"cli.language.duplicate"}}}"#.utf8))
        XCTAssertEqual(value.data?["profile"]?.displayString, "Running")
        XCTAssertEqual(value.data?["node"]?.displayString, "DIRECT")
        XCTAssertEqual(value.data?["message"]?.string, "English fallback")
        XCTAssertEqual(value.data?["message"]?.displayString, L10n.text("cli.language.duplicate"))
    }

    @MainActor func testTranslatedSettingsFieldsAndLongMenuContentFitInBothAppearances() async throws {
        _ = NSApplication.shared
        func descendants<T: NSView>(_ view: NSView, _ type: T.Type) -> [T] {
            ((view as? T).map { [$0] } ?? []) + view.subviews.flatMap { descendants($0, type) }
        }
        let longName = String(repeating: "Fictitious配置方案%@", count: 30)
        for appearanceName in [NSAppearance.Name.aqua, .darkAqua] {
            let model = AppModel(runCommand: { _ in
                try JSONDecoder().decode(JSONValue.self, from: Data(#"{"interval_seconds":450,"source":"profile","url_source":"profile","can_reset_url":true,"inherited_interval_seconds":450,"inherited_interval_status":"known","inherited_test_url":"https://service.example/placeholder","inherited_url_status":"known"}"#.utf8))
            })
            let controller = SourceSettingsPanelController(model: model, kind: "provider", name: longName, page: .latency)
            let window = try XCTUnwrap(controller.window)
            defer { window.close() }
            window.appearance = NSAppearance(named: appearanceName)
            await controller.loadSettings()
            let root = try XCTUnwrap(window.contentView)
            root.layoutSubtreeIfNeeded()
            XCTAssertTrue(window.title.contains(longName))
            let fields = descendants(root, NSTextField.self)
            XCTAssertTrue(fields.contains { !$0.isEnabled && $0.stringValue == L10n.format("app.ui.seconds", "450") })
            XCTAssertTrue(fields.contains { !$0.isEnabled && $0.stringValue == "https://service.example/placeholder" })
            let close = try XCTUnwrap(descendants(root, NSButton.self).first { $0.title == L10n.text("app.ui.close") })
            XCTAssertTrue(root.bounds.insetBy(dx: -1, dy: -1).contains(close.convert(close.bounds, to: root)))
            let menu = MenuAppDelegate.makeStatusMenuItem(state: .running, errors: [longName], notice: nil, tunEnabled: false, tunAdapter: nil)
            let item = try XCTUnwrap(menu.submenu?.items.first)
            XCTAssertEqual(item.title, longName)
            XCTAssertNotNil(item.view)
        }
    }

    @MainActor func testNativeMenuAndDialogsUseSelectedAppBundleWithRawNames() {
        _ = NSApplication.shared
        let name = "Running · fictitious中文%@"
        let alert = MenuAppDelegate.profileDeletionConfirmation(name)
        XCTAssertTrue(alert.messageText.contains(name))
        XCTAssertEqual(alert.buttons.last?.title, L10n.text("app.ui.cancel"))
        let item = MenuAppDelegate.makeStatusMenuItem(state: .running, errors: [], notice: nil, tunEnabled: false, tunAdapter: nil)
        XCTAssertTrue(item.title.contains(L10n.protocolLabel("running")))
        XCTAssertEqual(ConnectionsWindowController.closeAllConfirmation().buttons.last?.title, L10n.text("app.ui.cancel"))
    }
}
