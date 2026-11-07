import AppKit
import XCTest
@testable import NagiApp

@MainActor final class LANSettingsLayoutTests: XCTestCase {
    private func settings(enabled: Bool) -> SettingsModel {
        let browser = BrowserSettingsModel(run: { _ in
            try JSONDecoder().decode(JSONValue.self, from: Data(
                #"{"available":true,"current_value":null,"restricted":false,"managed":false,"forced":false,"can_enable":true,"can_disable":false,"can_restore":false}"#.utf8))
        })
        let lan = LANSettingsModel(run: { args in
            XCTAssertEqual(args, ["lan", "status"], "Layout fixtures must not change runtime settings")
            return .objectValue(["enabled": .boolValue(enabled), "bind_address": .stringValue(enabled ? "*" : "127.0.0.1")])
        })
        return SettingsModel(browser: browser, lan: lan, readDisplay: { .both }, writeDisplay: { _ in
            XCTFail("Layout fixtures must not write preferences")
        }, readAppLogin: { false }, writeAppLogin: { _ in
            XCTFail("Layout fixtures must not change login settings")
        }, readEngineLogin: { false }, writeEngineLogin: { _ in
            XCTFail("Layout fixtures must not change service settings")
        })
    }

    func testLANControlsAndFollowingSettingsAreScrollableInBothAppearancesAndWindowSizes() async throws {
        let restoreAccessibility = NativeAccessibilityFixture.enable()
        defer { restoreAccessibility() }
        let model = settings(enabled: true)
        await model.refresh()
        let controller = SettingsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        // Hosting directly avoids menu tracking, desktop AX permissions and CLI execution.
        window.orderFront(nil)
        let initial = window.frame
        let root = try XCTUnwrap(window.contentView)
        for appearance in [NSAppearance.Name.aqua, .darkAqua] {
            window.appearance = NSAppearance(named: appearance)
            for (name, size) in [("initial", initial.size), ("minimum", window.minSize)] {
                window.setFrame(NSRect(origin: initial.origin, size: size), display: true)
                try await settle(root)
                XCTAssertEqual(root.effectiveAppearance.bestMatch(from: [.aqua, .darkAqua]), appearance)
                XCTAssertEqual(window.frame.size, size)
                XCTAssertEqual(root.frame.size, window.contentLayoutRect.size)
                let scroll = try XCTUnwrap(descendants(root, of: NSScrollView.self).first)
                let document = try XCTUnwrap(scroll.documentView)
                XCTAssertGreaterThan(document.bounds.height, scroll.contentView.bounds.height)
                for key in ["app.lan.allow", "app.lan.bind_address", "app.lan.apply",
                            "app.lan.address_help", "app.lan.exposure_warning", "app.lan.runtime_only",
                            "app.ui.open_menu_bar_app_at_login", "app.ui.run_engine_at_login"] {
                    _ = try await reachableElement(key, in: root, scroll: scroll, window: window)
                    XCTAssertEqual(window.frame.size, size, "Scrolling must not grow the Settings window")
                }
                // Capture the actual LAN section, rather than an off-screen SwiftUI subtree.
                _ = try await reachableElement("app.lan.bind_address", in: root, scroll: scroll, window: window)
                try NativeAccessibilityFixture.snapshot(root, name: "lan-settings-\(name)-\(appearance.rawValue)")
            }
        }
    }

    func testDisabledApplyAndInvalidAddressRemainReachableAtMinimumSize() async throws {
        let restoreAccessibility = NativeAccessibilityFixture.enable()
        defer { restoreAccessibility() }
        let model = settings(enabled: false)
        await model.refresh()
        let controller = SettingsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        window.orderFront(nil)
        let root = try XCTUnwrap(window.contentView)
        for appearance in [NSAppearance.Name.aqua, .darkAqua] {
            window.appearance = NSAppearance(named: appearance)
            window.setFrame(NSRect(origin: window.frame.origin, size: window.minSize), display: true)
            try await settle(root)
            let scroll = try XCTUnwrap(descendants(root, of: NSScrollView.self).first)
            let apply = try await reachableElement("app.lan.apply", in: root, scroll: scroll, window: window)
            XCTAssertEqual(apply.value(forKey: "accessibilityEnabled") as? Bool, false)
            model.lan.bindAddress = "not-an-ip.invalid"
            try await settle(root)
            _ = try await reachableElement("app.lan.invalid_address", in: root, scroll: scroll, window: window)
            _ = try await reachableElement("app.lan.runtime_only", in: root, scroll: scroll, window: window)
            _ = try await reachableElement("app.lan.bind_address", in: root, scroll: scroll, window: window)
            let field = try XCTUnwrap(descendants(root, of: NSTextField.self).first {
                $0.isEditable && $0.accessibilityLabel() == L10n.text("app.lan.bind_address")
            })
            XCTAssertTrue(window.makeFirstResponder(field))
            try await settle(root)
            XCTAssertEqual(window.frame.size, window.minSize)
            try NativeAccessibilityFixture.snapshot(root, name: "lan-settings-invalid-focused-minimum-\(appearance.rawValue)")
            window.makeFirstResponder(nil)
        }
    }

    private func settle(_ root: NSView) async throws {
        for _ in 0..<5 {
            root.layoutSubtreeIfNeeded()
            try await Task.sleep(nanoseconds: 20_000_000)
        }
    }

    // SwiftUI labels and toggles may be virtual AX nodes, not NSView descendants.
    private func elements(in element: NSObject) -> [NSObject] {
        let children = element.responds(to: NSSelectorFromString("accessibilityChildren"))
            ? element.value(forKey: "accessibilityChildren") as? [NSObject] ?? [] : []
        return [element] + children.flatMap { elements(in: $0) }
    }

    private func reachableElement(_ key: String, in root: NSView, scroll: NSScrollView,
                                  window: NSWindow) async throws -> NSObject {
        let document = try XCTUnwrap(scroll.documentView)
        let clip = scroll.contentView
        let step = max(1, clip.bounds.height / 2)
        let maximum = max(document.bounds.minY, document.bounds.maxY - clip.bounds.height)
        let count = Int(ceil((maximum - document.bounds.minY) / step))
        for index in 0...count {
            let y = min(maximum, document.bounds.minY + CGFloat(index) * step)
            clip.scroll(to: NSPoint(x: document.bounds.minX, y: y))
            scroll.reflectScrolledClipView(clip)
            root.layoutSubtreeIfNeeded()
            try await Task.sleep(nanoseconds: 20_000_000)
            let viewport = window.convertToScreen(clip.convert(clip.bounds, to: nil))
                .intersection(window.convertToScreen(root.convert(root.bounds, to: nil)))
            for element in elements(in: root) where element.responds(to: NSSelectorFromString("accessibilityLabel")) {
                guard element.value(forKey: "accessibilityLabel") as? String == L10n.text(key),
                      element.responds(to: NSSelectorFromString("accessibilityFrame")),
                      let value = element.value(forKey: "accessibilityFrame") as? NSValue else { continue }
                let frame = value.rectValue
                if frame.width > 0 && frame.height > 0 && viewport.insetBy(dx: -1, dy: -1).contains(frame) {
                    return element
                }
            }
        }
        XCTFail("\(key) must be accessible and fully reachable by scrolling the actual Settings viewport")
        throw NSError(domain: "LANSettingsLayoutTests", code: 1)
    }

    private func descendants<T: NSView>(_ root: NSView, of type: T.Type) -> [T] {
        ((root as? T).map { [$0] } ?? []) + root.subviews.flatMap { descendants($0, of: type) }
    }
}
