import XCTest
import AppKit
@testable import NagiApp

@MainActor final class BrowserSettingsTests: XCTestCase {
    private func state(restricted: Bool = false, managed: Bool = false, forced: Bool = false,
                       canEnable: Bool = true, canRestore: Bool = false, canDisable: Bool = false) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: Data("""
        {"available":true,"current_value":null,"restricted":\(restricted),"managed":\(managed),"forced":\(forced),"can_enable":\(canEnable),"can_restore":\(canRestore),"can_disable":\(canDisable),"requires_restart":\(managed)}
        """.utf8))
    }

    func testEnableAndDisableUseCLIStateWithoutTouchingBrowserPreferences() async throws {
        var calls: [[String]] = []
        let model = BrowserSettingsModel(run: { args in
            calls.append(args)
            return try self.state(restricted: args.last == "enable", managed: args.last == "enable",
                                  canEnable: args.last != "enable", canRestore: args.last == "enable", canDisable: args.last == "enable")
        })
        XCTAssertFalse(model.canEnable)
        await model.refresh()
        XCTAssertTrue(model.canEnable)
        await model.enable()
        XCTAssertTrue(model.canDisable)
        await model.disable()
        XCTAssertTrue(model.canEnable)
        XCTAssertEqual(calls, [["browser", "chrome-webrtc", "status"], ["browser", "chrome-webrtc", "enable"], ["browser", "chrome-webrtc", "disable"]])
    }

    func testExistingExternalRestrictionCanBeDisabledButForcedPolicyIsReadOnly() async throws {
        var calls: [[String]] = []
        let model = BrowserSettingsModel(run: { args in
            calls.append(args)
            return try self.state(restricted: args.last != "disable", canEnable: args.last == "disable", canDisable: args.last != "disable")
        })
        await model.refresh()
        XCTAssertTrue(model.canDisable)
        await model.setRestricted(false)
        XCTAssertFalse(model.state?.restricted == true)
        XCTAssertEqual(calls.last, ["browser", "chrome-webrtc", "disable"])
        let forced = BrowserSettingsModel(run: { _ in
            try self.state(restricted: true, forced: true, canEnable: false, canDisable: false)
        })
        await forced.refresh()
        XCTAssertFalse(forced.canToggle)
    }

    func testFailedMutationReconcilesActualStateWithoutRepeatingWrite() async throws {
        var calls: [[String]] = []
        let model = BrowserSettingsModel(run: { args in
            calls.append(args)
            if args.last == "enable" { throw CLIError.command("conflict", "Chrome preference changed outside Nagi.") }
            return try self.state(restricted: calls.count > 1, canEnable: calls.count == 1, canDisable: calls.count > 1)
        })
        await model.refresh(); await model.enable()
        XCTAssertFalse(model.stale)
        XCTAssertTrue(model.state?.restricted == true)
        XCTAssertTrue(model.canDisable)
        XCTAssertTrue(model.error?.contains("Chrome preference changed outside Nagi.") == true)
        XCTAssertEqual(calls, [["browser", "chrome-webrtc", "status"], ["browser", "chrome-webrtc", "enable"], ["browser", "chrome-webrtc", "status"]])
    }

    func testFailedReconciliationDisablesFurtherWrites() async throws {
        var calls = 0
        let model = BrowserSettingsModel(run: { _ in
            calls += 1
            if calls > 1 { throw CLIError.command("failure", "Preferences unavailable") }
            return try self.state()
        })
        await model.refresh(); await model.enable()
        XCTAssertTrue(model.stale); XCTAssertFalse(model.canToggle)
        await model.enable()
        XCTAssertEqual(calls, 3)
    }

    func testInvalidCLIStateDoesNotEnableMutation() async throws {
        let model = BrowserSettingsModel(run: { _ in .objectValue([:]) })
        await model.refresh()
        XCTAssertTrue(model.stale); XCTAssertNotNil(model.error)
        XCTAssertFalse(model.canEnable); XCTAssertFalse(model.canDisable)
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

    func testSettingsMenuReplacesDisplayBrowserAndLoginEntries() {
        _ = NSApplication.shared
        let model = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/no-settings-test-cli")))
        let delegate = MenuAppDelegate(model: model)
        delegate.menuWillOpen(delegate.menu)
        let settings = delegate.menu.item(withTitle: L10n.text("app.settings.menu"))
        XCTAssertNotNil(settings); XCTAssertNil(settings?.submenu); XCTAssertNotNil(settings?.action)
        XCTAssertNil(delegate.menu.item(withTitle: L10n.text("app.ui.display")))
        XCTAssertNil(delegate.menu.item(withTitle: L10n.text("app.ui.login")))
        XCTAssertEqual(delegate.menu.items.filter { $0.title == L10n.text("app.settings.menu") }.count, 1)
        delegate.menuDidClose(delegate.menu)
    }
}
