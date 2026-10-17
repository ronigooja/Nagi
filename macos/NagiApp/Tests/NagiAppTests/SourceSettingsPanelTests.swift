import AppKit
import XCTest
@testable import NagiApp

@MainActor final class SourceSettingsPanelTests: XCTestCase {
    private func descendants<T: NSView>(_ root: NSView, of type: T.Type) -> [T] {
        ((root as? T).map { [$0] } ?? []) + root.subviews.flatMap { descendants($0, of: type) }
    }

    // Construct the actual AppKit tree: state-only tests cannot catch an
    // NSMenu range exception raised before the window is shown.
    func testBothPagesConstructAndLoadForBothSourceKinds() async throws {
        _ = NSApplication.shared
        for kind in ["provider", "subscription"] {
            for page in [SourceSettingsPanelController.Page.update, .latency] {
                var calls: [[String]] = []
                let model = AppModel(runCommand: { arguments in
                    calls.append(arguments)
                    return try JSONDecoder().decode(JSONValue.self, from: Data("""
                    {"interval_seconds":3600,"source":"override","source_interval_seconds":21600,
                     "can_reset":true,"url_source":"override","can_reset_url":false,
                     "test_url":"https://example.invalid/check?token=placeholder","enabled":false,"profile":"Jimiao",
                     "subscriptions":[{"name":"work","schedule_seconds":3600}]}
                    """.utf8))
                })
                let controller = SourceSettingsPanelController(model: model, kind: kind, name: "work", page: page)
                let window = try XCTUnwrap(controller.window)
                await controller.loadSettings()
                let root = try XCTUnwrap(window.contentView)
                root.layoutSubtreeIfNeeded()
                let choices = descendants(root, of: NSPopUpButton.self)
                XCTAssertEqual(choices.count, page == .update ? 1 : 3)
                XCTAssertTrue(choices.allSatisfy { $0.indexOfSelectedItem >= 0 && $0.indexOfSelectedItem < $0.numberOfItems })
                let buttons = descendants(root, of: NSButton.self)
                XCTAssertTrue(buttons.contains { $0.title == "Close" && $0.isEnabled })
                let labels = descendants(root, of: NSTextField.self)
                XCTAssertFalse(labels.contains { !$0.isHidden && $0.stringValue.hasPrefix("Settings could not be loaded") })
                XCTAssertEqual(calls.count, 1)
                XCTAssertFalse(model.busy)
                if page == .latency {
                    if kind == "provider" {
                        XCTAssertTrue(labels.contains { $0.stringValue == "Own checks disabled" })
                    }
                    XCTAssertFalse(labels.contains { $0.stringValue.hasPrefix("Profile:") })
                    XCTAssertTrue(labels.contains { $0.isEditable && $0.stringValue == "https://example.invalid/check?token=placeholder" })
                    XCTAssertTrue(buttons.contains { $0.title == "Save Test URL" && !$0.isEnabled })
                    XCTAssertEqual(calls.first, ["proxy", "healthcheck", "show", kind, "work", "--full-url"])
                } else {
                    XCTAssertEqual(choices.first?.titleOfSelectedItem, "1 hour")
                    XCTAssertTrue(buttons.contains { $0.title == "Save" && !$0.isEnabled })
                }
            }
        }
    }

    func testInheritedValuesAppearInDisabledFieldsIncludingCustomSeconds() async throws {
        _ = NSApplication.shared
        let model = AppModel(runCommand: { _ in
            return try JSONDecoder().decode(JSONValue.self, from: Data("""
            {"interval_seconds":0,"source":"profile","url_source":"profile","profile":"work",
             "can_reset_url":true,"inherited_interval_seconds":450,"inherited_interval_status":"known",
             "inherited_test_url":"https://source.invalid/path?status=204","inherited_url_status":"known"}
            """.utf8))
        })
        let controller = SourceSettingsPanelController(model: model, kind: "subscription", name: "work", page: .latency)
        await controller.loadSettings()
        let root = try XCTUnwrap(controller.window?.contentView)
        let fields = descendants(root, of: NSTextField.self)
        XCTAssertTrue(fields.contains { $0.stringValue == "450 seconds" && !$0.isHidden && !$0.isEnabled })
        XCTAssertTrue(fields.contains { $0.stringValue == "https://source.invalid/path?status=204" && !$0.isHidden && !$0.isEnabled })
        let buttons = descendants(root, of: NSButton.self)
        XCTAssertTrue(buttons.contains { $0.title == "Save Interval" && !$0.isEnabled })
        XCTAssertTrue(buttons.contains { $0.title == "Save Test URL" && !$0.isEnabled })
    }

    func testMixedInheritedValuesShowPlaceholdersInsteadOfInventingValues() async throws {
        _ = NSApplication.shared
        let model = AppModel(runCommand: { _ in
            return try JSONDecoder().decode(JSONValue.self, from: Data("""
            {"interval_seconds":0,"source":"profile","url_source":"profile","profile":"work",
             "can_reset_url":true,"inherited_interval_status":"mixed","inherited_url_status":"mixed"}
            """.utf8))
        })
        let controller = SourceSettingsPanelController(model: model, kind: "subscription", name: "work", page: .latency)
        await controller.loadSettings()
        let root = try XCTUnwrap(controller.window?.contentView)
        let fields = descendants(root, of: NSTextField.self).filter { $0.placeholderString == "Varies by group or provider" }
        XCTAssertEqual(fields.count, 2)
        XCTAssertTrue(fields.allSatisfy { $0.stringValue.isEmpty && !$0.isHidden && !$0.isEnabled })
    }

    func testInitialReadFailureCanRetryInSameWindow() async throws {
        _ = NSApplication.shared
        var attempts = 0
        let model = AppModel(runCommand: { _ in
            attempts += 1
            if attempts == 1 { throw CLIError.command("read", "Read failed") }
            return .objectValue(["interval_seconds": .numberValue(3600)])
        })
        let controller = SourceSettingsPanelController(model: model, kind: "provider", name: "work", page: .update)
        let window = try XCTUnwrap(controller.window)
        await controller.loadSettings()
        let root = try XCTUnwrap(window.contentView)
        XCTAssertTrue(descendants(root, of: NSButton.self).contains { $0.title == "Retry Loading" && !$0.isHidden })
        await controller.loadSettings()
        XCTAssertTrue(controller.window === window)
        XCTAssertEqual(attempts, 2)
        XCTAssertEqual(descendants(root, of: NSPopUpButton.self).first?.titleOfSelectedItem, "1 hour")
    }
}
