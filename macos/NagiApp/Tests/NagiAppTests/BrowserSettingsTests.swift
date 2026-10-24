import XCTest
import AppKit
@testable import NagiApp

@MainActor final class BrowserSettingsTests: XCTestCase {
    private func state(restricted: Bool = false, managed: Bool = false, forced: Bool = false,
                       canEnable: Bool = true, canRestore: Bool = false) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: Data("""
        {"available":true,"current_value":null,"restricted":\(restricted),"managed":\(managed),"forced":\(forced),"can_enable":\(canEnable),"can_restore":\(canRestore),"requires_restart":\(managed)}
        """.utf8))
    }

    func testEnableAndRestoreUseCLIStateWithoutTouchingBrowserPreferences() async throws {
        var calls: [[String]] = []
        let model = BrowserSettingsModel(run: { args in
            calls.append(args)
            return try self.state(restricted: args.last == "enable", managed: args.last == "enable",
                                  canEnable: args.last != "enable", canRestore: args.last == "enable")
        })
        XCTAssertFalse(model.canEnable)
        await model.refresh()
        XCTAssertTrue(model.canEnable)
        await model.enable()
        XCTAssertTrue(model.canRestore)
        XCTAssertTrue(model.saved)
        await model.restore()
        XCTAssertTrue(model.canEnable)
        XCTAssertEqual(calls, [["browser", "chrome-webrtc", "status"], ["browser", "chrome-webrtc", "enable"], ["browser", "chrome-webrtc", "restore"]])
    }

    func testExistingExternalRestrictionAndForcedPolicyAreReadOnly() async throws {
        for forced in [false, true] {
            var calls: [[String]] = []
            let model = BrowserSettingsModel(run: { args in
                calls.append(args)
                return try self.state(restricted: true, forced: forced, canEnable: false)
            })
            await model.refresh()
            await model.enable(); await model.restore()
            XCTAssertFalse(model.canEnable); XCTAssertFalse(model.canRestore)
            XCTAssertEqual(calls.count, 1)
        }
    }

    func testFailedReadOrMutationRequiresRefreshBeforeFurtherWrites() async throws {
        var calls: [[String]] = []
        var fail = false
        let model = BrowserSettingsModel(run: { args in
            calls.append(args)
            if fail { throw CLIError.command("conflict", "Chrome preference changed outside Nagi.") }
            return try self.state()
        })
        await model.refresh(); fail = true
        await model.enable()
        XCTAssertTrue(model.stale); XCTAssertNotNil(model.error)
        XCTAssertFalse(model.saved); XCTAssertFalse(model.canEnable)
        await model.enable()
        XCTAssertEqual(calls.count, 2)
        await model.refresh()
        XCTAssertTrue(model.stale); XCTAssertFalse(model.canEnable)
        fail = false; await model.refresh()
        XCTAssertTrue(model.canEnable); XCTAssertNil(model.error)
    }

    func testInvalidCLIStateDoesNotEnableMutation() async throws {
        let model = BrowserSettingsModel(run: { _ in .objectValue([:]) })
        await model.refresh()
        XCTAssertTrue(model.stale); XCTAssertNotNil(model.error)
        XCTAssertFalse(model.canEnable); XCTAssertFalse(model.canRestore)
    }

    func testSubmittedBrowserOperationBlocksConcurrentSettingsAndQuitWaits() async throws {
        var mutationStarted = false, release = false
        var calls: [[String]] = []
        let app = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/no-browser-settings-test-cli")), runCommand: { args in
            calls.append(args)
            if args.last == "enable" {
                mutationStarted = true
                while !release { try? await Task.sleep(nanoseconds: 2_000_000) }
                return try self.state(restricted: true, managed: true, canEnable: false, canRestore: true)
            }
            return try self.state()
        })
        await app.browserSettings.refresh()
        let task = Task { await app.browserSettings.enable() }
        for _ in 0..<100 where !mutationStarted { try await Task.sleep(nanoseconds: 2_000_000) }
        XCTAssertTrue(mutationStarted); XCTAssertTrue(app.busy)
        app.perform(["mode", "direct"])
        var quitCompleted = false
        app.quit { _ in quitCompleted = true }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertFalse(quitCompleted)
        XCTAssertFalse(calls.contains(["quit"]))
        XCTAssertFalse(calls.contains(["mode", "direct"]))
        release = true; await task.value
        for _ in 0..<100 where !quitCompleted { try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(quitCompleted); XCTAssertTrue(calls.contains(["quit"]))
    }

    func testNativeWindowKeepsFrameOnReopenAndUsesScreenBounds() async throws {
        _ = NSApplication.shared
        let model = BrowserSettingsModel(run: { _ in try self.state() })
        let controller = BrowserSettingsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        let screen = try XCTUnwrap(window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame)
        XCTAssertEqual(window.frame, BrowserSettingsWindowController.layout.initialFrame(in: screen))
        XCTAssertEqual(window.minSize, BrowserSettingsWindowController.layout.minimumFrameSize(in: screen))
        let small = NSRect(x: -600, y: 40, width: 500, height: 330)
        XCTAssertTrue(small.contains(BrowserSettingsWindowController.layout.initialFrame(in: small)))
        let resized = NSSize(width: 480, height: 300)
        window.setFrame(NSRect(origin: window.frame.origin, size: resized), display: false)
        window.close(); controller.present()
        XCTAssertTrue(controller.window === window)
        XCTAssertEqual(window.frame.size, resized)
        window.close()
    }

    func testMenuAddsBrowserEntryWithoutReorganizingDisplayAndLogin() {
        _ = NSApplication.shared
        let model = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/no-browser-settings-test-cli")))
        let delegate = MenuAppDelegate(model: model)
        delegate.menuWillOpen(delegate.menu)
        let titles = delegate.menu.items.map(\.title)
        let display = titles.firstIndex(of: L10n.text("app.ui.display"))
        let browser = titles.firstIndex(of: L10n.text("app.browser.menu"))
        let login = titles.firstIndex(of: L10n.text("app.ui.login"))
        XCTAssertNotNil(display); XCTAssertNotNil(browser); XCTAssertNotNil(login)
        if let display, let browser, let login { XCTAssertLessThan(display, browser); XCTAssertLessThan(browser, login) }
        XCTAssertNil(delegate.menu.item(withTitle: L10n.text("app.browser.menu"))?.submenu)
        XCTAssertNotNil(delegate.menu.item(withTitle: L10n.text("app.browser.menu"))?.action)
        delegate.menuDidClose(delegate.menu)
    }
}
