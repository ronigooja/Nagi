import AppKit
import XCTest
@testable import NagiApp

@MainActor final class MenuAvailabilityTests: XCTestCase {
    private struct Fixture {
        let directory: URL
        let script: URL
        var hold: URL { directory.appendingPathComponent("hold") }
        var installed: URL { directory.appendingPathComponent("installed") }
        var failure: URL { directory.appendingPathComponent("failure") }
        var log: URL { directory.appendingPathComponent("events") }
    }

    private func fixture() throws -> Fixture {
        _ = NSApplication.shared
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-menu-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let fixture = Fixture(directory: directory, script: directory.appendingPathComponent("nagi"))
        let script = """
        #!/bin/sh
        printf '%s\\n' "$*" >> '\(fixture.log.path)'
        case "$*" in
          '--json status') printf '{"ok":true,"data":{"running":false}}\\n' ;;
          '--json profile list') printf '{"ok":true,"data":{"profiles":["default","work"],"current":"work"}}\\n' ;;
          '--json subscription list') printf '{"ok":true,"data":{"subscriptions":[]}}\\n' ;;
          '--json service status')
            if [ -f '\(fixture.installed.path)' ]; then value=true; else value=false; fi
            printf '{"ok":true,"data":{"installed":%s}}\\n' "$value" ;;
          '--json service install')
            while [ -f '\(fixture.hold.path)' ]; do sleep 0.02; done
            if [ -f '\(fixture.failure.path)' ]; then
              printf '{"ok":false,"error":{"code":"fixture_failure","message":"Service install failed"}}\\n' >&2
              exit 1
            fi
            touch '\(fixture.installed.path)'
            printf '{"ok":true,"data":{"installed":true}}\\n' ;;
          *) printf '{"ok":true,"data":{}}\\n' ;;
        esac
        """
        try script.write(to: fixture.script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: fixture.script.path)
        return fixture
    }

    private func item(_ title: String, in menu: NSMenu) -> NSMenuItem? {
        for candidate in menu.items {
            if candidate.title == title { return candidate }
            if let submenu = candidate.submenu, let found = item(title, in: submenu) { return found }
        }
        return nil
    }

    private func waitUntil(_ condition: () -> Bool) async -> Bool {
        for _ in 0..<200 {
            if condition() { return true }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        return condition()
    }

    func testBusyMenuBlocksOpeningAndReopensAfterOperation() async throws {
        let fixture = try fixture()
        defer { try? FileManager.default.removeItem(at: fixture.directory) }
        let model = AppModel(cli: NagiCLIClient(executable: fixture.script))
        let delegate = MenuAppDelegate(model: model)
        await model.refresh()
        XCTAssertTrue(delegate.menuShouldOpen(delegate.menu))
        try Data().write(to: fixture.hold)
        model.perform(["service", "install"])
        XCTAssertTrue(model.busy)
        XCTAssertFalse(delegate.menuShouldOpen(delegate.menu))
        XCTAssertTrue(delegate.menu.items.first?.title.contains("Operation in progress") == true)
        XCTAssertFalse(delegate.menu.autoenablesItems)
        for title in ["Retry Start", "Run engine at login", "Open menu bar app at login", "work", "Add Provider…", "Add and Apply Subscription…", "Quit Nagi"] {
            XCTAssertFalse(try XCTUnwrap(item(title, in: delegate.menu)).isEnabled, title)
        }
        let login = try XCTUnwrap(item("Run engine at login", in: delegate.menu))
        XCTAssertFalse(try XCTUnwrap(login.menu).autoenablesItems)
        // Dispatch directly as a stale AppKit event would; disabled UI alone
        // must not be the serialization boundary.
        _ = delegate.perform(NSSelectorFromString("runCommand:"), with: login)
        try FileManager.default.removeItem(at: fixture.hold)
        let completed = await waitUntil { !model.busy }
        XCTAssertTrue(completed)
        XCTAssertTrue(delegate.menuShouldOpen(delegate.menu))
        XCTAssertEqual(try String(contentsOf: fixture.log).components(separatedBy: "--json service install").count - 1, 1)
        delegate.menuWillOpen(delegate.menu)
        let refreshedLogin = try XCTUnwrap(item("Run engine at login", in: delegate.menu))
        XCTAssertTrue(refreshedLogin.isEnabled)
        XCTAssertEqual(refreshedLogin.representedObject as? [String], ["service", "uninstall"])
        delegate.menuDidClose(delegate.menu)
    }

    func testBusyMenuReopensAfterCommandFailure() async throws {
        let fixture = try fixture()
        defer { try? FileManager.default.removeItem(at: fixture.directory) }
        let model = AppModel(cli: NagiCLIClient(executable: fixture.script))
        let delegate = MenuAppDelegate(model: model)
        await model.refresh()
        try Data().write(to: fixture.failure)
        try Data().write(to: fixture.hold)
        model.perform(["service", "install"])
        XCTAssertFalse(delegate.menuShouldOpen(delegate.menu))
        try FileManager.default.removeItem(at: fixture.hold)
        let completed = await waitUntil { !model.busy }
        XCTAssertTrue(completed)
        XCTAssertTrue(delegate.menuShouldOpen(delegate.menu))
        XCTAssertTrue(model.errorMessage?.contains("Service install failed") == true)
        delegate.menuWillOpen(delegate.menu)
        let login = try XCTUnwrap(item("Run engine at login", in: delegate.menu))
        XCTAssertTrue(login.isEnabled)
        XCTAssertEqual(login.representedObject as? [String], ["service", "install"])
        delegate.menuDidClose(delegate.menu)
    }

    func testCompletionDefersTreeReplacementAndRefreshesCommandsOnClose() async throws {
        let fixture = try fixture()
        defer { try? FileManager.default.removeItem(at: fixture.directory) }
        let model = AppModel(cli: NagiCLIClient(executable: fixture.script))
        let delegate = MenuAppDelegate(model: model)
        await model.refresh()
        delegate.menuWillOpen(delegate.menu)
        let oldStatus = try XCTUnwrap(delegate.menu.items.first)
        let oldLogin = try XCTUnwrap(item("Run engine at login", in: delegate.menu))
        XCTAssertTrue(oldLogin.isEnabled)
        let previousDisplay = model.display
        model.display = previousDisplay == .icon ? .both : .icon
        XCTAssertTrue(oldLogin.isEnabled)
        XCTAssertTrue(delegate.menu.items.first === oldStatus)
        model.display = previousDisplay
        try Data().write(to: fixture.hold)
        model.perform(["service", "install"])
        XCTAssertTrue(delegate.menu.items.first === oldStatus)
        XCTAssertFalse(oldLogin.isEnabled)
        XCTAssertFalse(delegate.menuShouldOpen(delegate.menu))
        try FileManager.default.removeItem(at: fixture.hold)
        let completed = await waitUntil { !model.busy }
        XCTAssertTrue(completed)
        XCTAssertTrue(delegate.menu.items.first === oldStatus)
        XCTAssertFalse(oldStatus.title.contains("Operation in progress"))
        XCTAssertFalse(oldLogin.isEnabled)
        XCTAssertEqual(oldLogin.representedObject as? [String], ["service", "install"])
        _ = delegate.perform(NSSelectorFromString("runCommand:"), with: oldLogin)
        XCTAssertFalse(model.busy, "A completed operation must not revive an old tracked command")
        delegate.menuDidClose(delegate.menu)
        let newLogin = try XCTUnwrap(item("Run engine at login", in: delegate.menu))
        XCTAssertFalse(newLogin === oldLogin)
        XCTAssertTrue(newLogin.isEnabled)
        XCTAssertEqual(newLogin.state, .on)
        XCTAssertEqual(newLogin.representedObject as? [String], ["service", "uninstall"])
        delegate.menuWillOpen(delegate.menu)
        XCTAssertTrue(try XCTUnwrap(item("Run engine at login", in: delegate.menu)).isEnabled)
        delegate.menuDidClose(delegate.menu)
    }

    func testExternalConfigurationRefreshInvalidatesTrackedMutation() async throws {
        let fixture = try fixture()
        defer { try? FileManager.default.removeItem(at: fixture.directory) }
        let model = AppModel(cli: NagiCLIClient(executable: fixture.script))
        let delegate = MenuAppDelegate(model: model)
        await model.refresh()
        delegate.menuWillOpen(delegate.menu)
        let oldLogin = try XCTUnwrap(item("Run engine at login", in: delegate.menu))
        try Data().write(to: fixture.installed)
        await model.refreshMenuData()
        XCTAssertFalse(model.busy)
        XCTAssertTrue(item("Run engine at login", in: delegate.menu) === oldLogin)
        XCTAssertFalse(oldLogin.isEnabled)
        delegate.menuDidClose(delegate.menu)
        XCTAssertEqual(try XCTUnwrap(item("Run engine at login", in: delegate.menu)).representedObject as? [String], ["service", "uninstall"])
    }
}
