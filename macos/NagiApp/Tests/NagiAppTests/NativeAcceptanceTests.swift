import AppKit
import XCTest
@testable import NagiApp

/// Automated native interactions with private models; no installed CLI or runtime.
@MainActor final class NativeAcceptanceTests: XCTestCase {
    private var chinese: Bool {
        (UserDefaults.standard.stringArray(forKey: "AppleLanguages")?.first ?? "en").hasPrefix("zh")
    }

    private func descendants<T: NSView>(_ root: NSView, _ type: T.Type) -> [T] {
        ((root as? T).map { [$0] } ?? []) + root.subviews.flatMap { descendants($0, type) }
    }

    func testProviderAndSubscriptionFormsRetryWithNativeClickAndKeepRawNames() async throws {
        _ = NSApplication.shared
        let name = String(repeating: "Running · fictitious中文%@ ", count: 20)
        for kind in ["provider", "subscription"] {
            for appearance in [NSAppearance.Name.aqua, .darkAqua] {
                var reads = 0
                let model = AppModel(runCommand: { _ in
                    reads += 1
                    if reads == 1 { throw CLIError.command("fixture", "Fictitious read failure") }
                    if kind == "subscription" {
                        return .objectValue(["subscriptions": .arrayValue([.objectValue([
                            "name": .stringValue(name), "schedule_seconds": .numberValue(3600)
                        ])])])
                    }
                    return .objectValue(["interval_seconds": .numberValue(3600)])
                })
                let controller = SourceSettingsPanelController(model: model, kind: kind, name: name, page: .update)
                let window = try XCTUnwrap(controller.window)
                defer { window.close() }
                window.appearance = NSAppearance(named: appearance)
                window.makeKeyAndOrderFront(nil)
                await controller.loadSettings()
                let root = try XCTUnwrap(window.contentView)
                root.layoutSubtreeIfNeeded()
                XCTAssertTrue(window.title.hasSuffix(name))
                let retry = try XCTUnwrap(descendants(root, NSButton.self).first { $0.title == (chinese ? "重新加载" : "Retry Loading") })
                XCTAssertTrue(retry.isEnabled)
                XCTAssertFalse(retry.isHidden)
                retry.performClick(nil)
                for _ in 0..<100 {
                    if reads == 2 && retry.isHidden { break }
                    try await Task.sleep(nanoseconds: 10_000_000)
                }
                XCTAssertEqual(reads, 2)
                XCTAssertTrue(retry.isHidden)
                XCTAssertEqual(descendants(root, NSPopUpButton.self).first?.titleOfSelectedItem, chinese ? "1 小时" : "1 hour")
                for _ in 0..<5 {
                    root.layoutSubtreeIfNeeded()
                    try await Task.sleep(nanoseconds: 20_000_000)
                }
                let close = try XCTUnwrap(descendants(root, NSButton.self).first { $0.title == (chinese ? "关闭" : "Close") })
                XCTAssertEqual(close.keyEquivalent, "\u{1b}")
                XCTAssertTrue(root.bounds.insetBy(dx: -1, dy: -1).contains(close.convert(close.bounds, to: root)))
                XCTAssertNotNil(descendants(root, NSScrollView.self).first?.documentView)
                try NativeAccessibilityFixture.snapshot(root, name: "\(kind)-update-\(appearance.rawValue)")
                XCTAssertEqual(reads, 2)
            }
        }
    }

    func testCaptureAndRewriteMinimumWindowsExposeActionsInBothAppearances() async throws {
        _ = NSApplication.shared
        let restoreAccessibility = NativeAccessibilityFixture.enable()
        defer { restoreAccessibility() }
        let reply = try JSONDecoder().decode(CaptureReply.self, from: Data(
            #"{"sensitive":true,"status":{"session_id":"fictitious-session","revision":1,"state":"paused","profile":"Running","backend":"user","owner":"cli","scope":{"domains":["api.example.invalid"],"process_paths":[],"app_paths":[]},"body_limit":1048576,"records":0,"bytes":0,"dropped":0,"truncated":0,"read_only":false},"records":[]}"#.utf8))
        XCTAssertEqual(L10n.format("app.capture.scope_domain_count", Int64(1)), chinese ? "1 个域名" : "1 domain")
        XCTAssertEqual(L10n.format("app.capture.scope_process_count", Int64(2)), chinese ? "2 个进程" : "2 processes")
        XCTAssertEqual(L10n.format("app.capture.scope_application_count", Int64(2)), chinese ? "2 个应用" : "2 applications")
        let capture = CaptureWindowController(model: CaptureModel(read: { _, _ in reply },
            submit: { _, _ in XCTFail("Fixture must not activate capture"); return reply }, export: { _ in }))
        let rewrite = RequestRewriteWindowController(model: RequestRewriteModel(read: { args, _ in
            if args == ["status"] {
                return .objectValue(["profile": .stringValue("Running"), "actual": .stringValue("disabled"), "enabled_intent": .boolValue(false)])
            }
            return .objectValue([args.first == "scripts" ? "scripts" : "rules": .arrayValue([])])
        }, submit: { _, _ in XCTFail("Fixture must not activate rewrite"); return .objectValue([:]) }, export: { _ in Data() }))
        defer { capture.window?.close(); rewrite.window?.close() }
        for (kind, controller) in [("capture", capture as NSWindowController), ("rewrite", rewrite as NSWindowController)] {
            let window = try XCTUnwrap(controller.window)
            let expectedMinimum = window.minSize
            XCTAssertGreaterThan(expectedMinimum.width, 600)
            XCTAssertGreaterThan(expectedMinimum.height, 400)
            if kind == "capture" { capture.present() } else { rewrite.present() }
            for appearance in [NSAppearance.Name.aqua, .darkAqua] {
                window.appearance = NSAppearance(named: appearance)
                window.setFrame(NSRect(origin: window.frame.origin, size: window.minSize), display: true)
                let root = try XCTUnwrap(window.contentView)
                for _ in 0..<10 {
                    root.layoutSubtreeIfNeeded()
                    try await Task.sleep(nanoseconds: 20_000_000)
                }
                XCTAssertEqual(window.minSize, expectedMinimum)
                XCTAssertEqual(window.frame.size, expectedMinimum)
                XCTAssertTrue(window.title.hasPrefix(kind == "capture"
                    ? (chinese ? "Nagi 请求捕获" : "Nagi Capture")
                    : (chinese ? "Nagi 请求改写" : "Nagi Request Rewrite")))
                let refresh = try XCTUnwrap(NativeAccessibilityFixture.buttons(in: root).first {
                    ($0.value(forKey: "accessibilityLabel") as? String) == (chinese ? "刷新" : "Refresh")
                })
                let frame = try XCTUnwrap(refresh.value(forKey: "accessibilityFrame") as? NSValue).rectValue
                let visible = window.convertToScreen(root.convert(root.bounds, to: nil))
                XCTAssertGreaterThan(frame.width, 0)
                XCTAssertTrue(visible.insetBy(dx: -1, dy: -1).contains(frame))
                XCTAssertFalse(descendants(root, NSScrollView.self).isEmpty)
                try NativeAccessibilityFixture.snapshot(root, name: "\(kind)-minimum-\(appearance.rawValue)")
            }
        }
    }

    func testConnectionsSearchReceivesKeyboardInputAndKeepsNativeAccessibility() async throws {
        _ = NSApplication.shared
        let model = ConnectionsModel(fetch: { [] }, execute: { _ in XCTFail("Fixture must not close a real connection") })
        model.setEngineState(.stopped)
        let controller = ConnectionsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        controller.present()
        let root = try XCTUnwrap(window.contentView)
        let search = try XCTUnwrap(descendants(root, NSSearchField.self).first)
        XCTAssertEqual(search.accessibilityLabel(), chinese ? "搜索连接" : "Search connections")
        XCTAssertTrue(window.makeFirstResponder(search))
        let editor = try XCTUnwrap(window.fieldEditor(false, for: search) as? NSTextView)
        XCTAssertTrue(window.firstResponder === editor)
        let event = try XCTUnwrap(NSEvent.keyEvent(with: .keyDown, location: .zero,
            modifierFlags: [], timestamp: 0, windowNumber: window.windowNumber, context: nil,
            characters: "fictitious中文", charactersIgnoringModifiers: "fictitious中文", isARepeat: false, keyCode: 3))
        window.sendEvent(event)
        XCTAssertEqual(search.stringValue, "fictitious中文")
        XCTAssertTrue(window.firstResponder === editor)
        let table = try XCTUnwrap(descendants(root, NSTableView.self).first)
        XCTAssertEqual(table.accessibilityLabel(), chinese ? "活动连接" : "Active connections")
        let buttons = descendants(root, NSButton.self)
        XCTAssertTrue(buttons.contains { $0.title == (chinese ? "关闭连接" : "Close Connection") && !$0.isEnabled })
        XCTAssertTrue(buttons.contains { $0.title == (chinese ? "关闭所有连接…" : "Close All Connections…") && !$0.isEnabled })
        try NativeAccessibilityFixture.snapshot(root, name: "connections-keyboard-focus")
    }
}
