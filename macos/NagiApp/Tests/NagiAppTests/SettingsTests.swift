import XCTest
import AppKit
@testable import NagiApp

@MainActor final class SettingsTests: XCTestCase {
    private func browser() -> BrowserSettingsModel {
        BrowserSettingsModel(run: { _ in
            try JSONDecoder().decode(JSONValue.self, from: Data(#"{"available":true,"current_value":null,"restricted":false,"managed":false,"forced":false,"can_enable":true,"can_disable":false,"can_restore":false}"#.utf8))
        })
    }

    func testDisplayAndLoginChangesSaveImmediately() async throws {
        var display = TrafficDisplay.icon, appLogin = false, engineLogin = false
        var writes: [String] = []
        let model = SettingsModel(browser: browser(), readDisplay: { display }, writeDisplay: { display = $0; writes.append("display") },
            readAppLogin: { appLogin }, writeAppLogin: { appLogin = $0; writes.append("app") },
            readEngineLogin: { engineLogin }, writeEngineLogin: { engineLogin = $0; writes.append("engine") })
        await model.refresh()
        XCTAssertEqual(model.display, .icon); XCTAssertFalse(model.appLogin); XCTAssertEqual(model.engineLogin, false)
        model.setDisplay(.both)
        await model.setAppLogin(true); await model.setEngineLogin(true)
        XCTAssertEqual(display, .both); XCTAssertTrue(model.appLogin); XCTAssertEqual(model.engineLogin, true)
        XCTAssertEqual(writes, ["display", "app", "engine"])
    }

    func testLoginFailuresReadActualValuesAndKeepSpecificError() async throws {
        var actualEngine = false
        let model = SettingsModel(browser: browser(), readDisplay: { .both }, writeDisplay: { _ in },
            readAppLogin: { false }, writeAppLogin: { _ in throw CLIError.command("denied", "Login permission denied") },
            readEngineLogin: { actualEngine }, writeEngineLogin: { _ in
                actualEngine = true
                throw CLIError.command("failed", "Service registration response unavailable")
            })
        await model.refresh(); await model.setAppLogin(true)
        XCTAssertFalse(model.appLogin)
        XCTAssertTrue(model.loginError?.contains("Login permission denied") == true)
        await model.setEngineLogin(true)
        XCTAssertEqual(model.engineLogin, true)
        XCTAssertTrue(model.loginError?.contains("Service registration response unavailable") == true)
    }

    func testMissingEngineStatusDisablesEngineChangeUntilAutomaticReload() async throws {
        var fail = true, writes = 0
        let model = SettingsModel(browser: browser(), readDisplay: { .both }, writeDisplay: { _ in },
            readAppLogin: { false }, writeAppLogin: { _ in }, readEngineLogin: {
                if fail { throw CLIError.command("read", "Engine login status unavailable") }
                return false
            }, writeEngineLogin: { _ in writes += 1 })
        await model.refresh()
        XCTAssertNil(model.engineLogin); XCTAssertNotNil(model.loginError)
        await model.setEngineLogin(true); XCTAssertEqual(writes, 0)
        fail = false; await model.refresh()
        XCTAssertEqual(model.engineLogin, false); XCTAssertNil(model.loginError)
    }

    func testEngineLoginUsesExistingCLICommandsWithoutStartingEngine() async throws {
        var installed = false, calls: [[String]] = []
        let app = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/no-settings-test-cli")), runCommand: { args in
            calls.append(args)
            if args == ["service", "install"] { installed = true }
            if args == ["service", "uninstall"] { installed = false }
            if args.first == "service" { return .objectValue(["installed": .boolValue(installed)]) }
            return try JSONDecoder().decode(JSONValue.self, from: Data(#"{"available":true,"current_value":null,"restricted":false,"managed":false,"forced":false,"can_enable":true,"can_disable":false,"can_restore":false}"#.utf8))
        })
        await app.settings.refresh()
        await app.settings.setEngineLogin(true)
        XCTAssertEqual(app.settings.engineLogin, true)
        XCTAssertEqual(app.serviceInstalled, true)
        await app.settings.setEngineLogin(false)
        XCTAssertEqual(app.settings.engineLogin, false)
        XCTAssertEqual(calls, [["browser", "chrome-webrtc", "status"], ["service", "status"],
                               ["service", "install"], ["service", "status"],
                               ["service", "uninstall"], ["service", "status"]])
    }

    func testNativeWindowReusesFrameAndHasNoActionButtonRow() async throws {
        _ = NSApplication.shared
        let model = SettingsModel(browser: browser(), readDisplay: { .both }, writeDisplay: { _ in },
            readAppLogin: { false }, writeAppLogin: { _ in }, readEngineLogin: { false }, writeEngineLogin: { _ in })
        let controller = SettingsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        let screen = try XCTUnwrap(window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame)
        XCTAssertEqual(window.frame, SettingsWindowController.layout.initialFrame(in: screen))
        XCTAssertEqual(window.minSize, SettingsWindowController.layout.minimumFrameSize(in: screen))
        let small = NSRect(x: -600, y: 40, width: 500, height: 330)
        XCTAssertTrue(small.contains(SettingsWindowController.layout.initialFrame(in: small)))
        let resized = NSSize(width: 480, height: 330)
        window.setFrame(NSRect(origin: window.frame.origin, size: resized), display: false)
        window.close(); controller.present()
        XCTAssertTrue(controller.window === window); XCTAssertEqual(window.frame.size, resized)
        try await Task.sleep(nanoseconds: 100_000_000)
        let content = try XCTUnwrap(window.contentView)
        let buttonTitles = descendants(content, of: NSButton.self).map(\.title)
        for key in ["app.ui.refresh", "app.ui.save", "app.ui.close"] {
            XCTAssertFalse(buttonTitles.contains(L10n.text(key)))
        }
        window.close()
    }

    @MainActor private func descendants<T: NSView>(_ root: NSView, of type: T.Type) -> [T] {
        ((root as? T).map { [$0] } ?? []) + root.subviews.flatMap { descendants($0, of: type) }
    }
}
