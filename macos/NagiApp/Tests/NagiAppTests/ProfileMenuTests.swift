import XCTest
import AppKit
@testable import NagiApp

@MainActor final class ProfileMenuTests: XCTestCase {
    @MainActor private final class Commands {
        var calls: [[String]] = []
        var current: String? = "one"
        var profiles = ["default", "one", "two", "three"]

        func run(_ arguments: [String]) async throws -> JSONValue {
            calls.append(arguments)
            let json: String
            let selected = current.map { "\"\($0)\"" } ?? "null"
            switch arguments {
            case ["status"]: json = "{\"running\":false,\"profile\":\(selected)}"
            case ["profile", "list"]:
                let names = String(data: try JSONEncoder().encode(profiles), encoding: .utf8)!
                json = "{\"current\":\(selected),\"profiles\":\(names)}"
            case ["subscription", "list"]: json = "{\"subscriptions\":[]}"
            default:
                if arguments.prefix(2) == ["profile", "remove"], let name = arguments.last {
                    profiles.removeAll { $0 == name }
                }
                json = "{\"installed\":false,\"default_override_applied\":false}"
            }
            return try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        }

        func model() -> AppModel {
            AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/nagi-profile-menu-test-no-cli")),
                     runCommand: { try await self.run($0) })
        }
    }

    private func waitForCompletion(_ model: AppModel) async {
        for _ in 0..<200 {
            if !model.busy { return }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTFail("Profile operation did not finish")
    }

    func testProfileSubmenusKeepSelectionAndBindActionsToTheirOwnNames() async throws {
        _ = NSApplication.shared
        let commands = Commands(), model = commands.model()
        await model.refresh()
        let delegate = MenuAppDelegate(model: model)
        let menu = delegate.makeProfilesMenu()
        XCTAssertEqual(menu.items.map(\.title), commands.profiles)
        XCTAssertEqual(menu.item(withTitle: "one")?.state, .on)
        XCTAssertEqual(menu.item(withTitle: "two")?.state, .off)
        let current = try XCTUnwrap(menu.item(withTitle: "one")?.submenu)
        XCTAssertEqual(current.items.map(\.title), ["Use", "Delete… (Switch profiles first)"])
        XCTAssertFalse(current.items[1].isEnabled)
        let defaults = try XCTUnwrap(menu.item(withTitle: "default")?.submenu)
        XCTAssertEqual(defaults.items.map(\.title), ["Use Default Profile", "Apply Local Default Settings to Current Profile"])
        for name in ["two", "three"] {
            let actions = try XCTUnwrap(menu.item(withTitle: name)?.submenu)
            XCTAssertEqual(actions.items.map(\.title), ["Use", "Delete…"])
            XCTAssertEqual(actions.items[0].representedObject as? [String], ["profile", "use", name])
            XCTAssertEqual(actions.items[1].representedObject as? String, name)
            XCTAssertEqual(actions.items[1].action, #selector(MenuAppDelegate.deleteProfile(_:)))
            XCTAssertTrue(actions.items[1].target === delegate)
            XCTAssertTrue(actions.items[1].isEnabled)
        }
    }

    func testConfirmationDefaultsToCancelAndExplainsCleanup() {
        _ = NSApplication.shared
        let alert = MenuAppDelegate.profileDeletionConfirmation("two")
        XCTAssertEqual(alert.messageText, "Delete profile “two”?")
        XCTAssertEqual(alert.buttons.map(\.title), ["Delete Profile", "Cancel"])
        XCTAssertEqual(alert.buttons[0].keyEquivalent, "")
        XCTAssertEqual(alert.buttons[1].keyEquivalent, "\r")
        XCTAssertTrue(alert.window.defaultButtonCell === alert.buttons[1].cell)
        XCTAssertTrue(alert.window.initialFirstResponder === alert.buttons[1])
        XCTAssertNil(alert.accessoryView)
        for effect in ["configuration", "backups", "local overrides and settings", "saved proxy choices",
                       "subscription associations", "Subscription records and cached downloads are kept"] {
            XCTAssertTrue(alert.informativeText.contains(effect), effect)
        }
    }

    func testCancelDispatchesNothingAndConfirmDeletesOnlyChosenProfileOnce() async throws {
        _ = NSApplication.shared
        let commands = Commands(), model = commands.model()
        await model.refresh()
        let delegate = MenuAppDelegate(model: model)
        let item = try XCTUnwrap(delegate.makeProfilesMenu().item(withTitle: "three")?.submenu?.items.last)
        var confirmations = 0
        delegate.confirmProfileDeletion = { alert in
            confirmations += 1
            XCTAssertEqual(alert.messageText, "Delete profile “three”?")
            return .alertSecondButtonReturn
        }
        let callsBefore = commands.calls
        delegate.deleteProfile(item)
        await Task.yield()
        XCTAssertEqual(commands.calls, callsBefore)
        XCTAssertEqual(confirmations, 1)
        delegate.confirmProfileDeletion = { _ in confirmations += 1; return .alertFirstButtonReturn }
        delegate.deleteProfile(item)
        await waitForCompletion(model)
        XCTAssertEqual(confirmations, 2)
        XCTAssertEqual(commands.calls.filter { $0.prefix(2) == ["profile", "remove"] }, [["profile", "remove", "three"]])
        XCTAssertEqual(model.profiles, ["default", "one", "two"])
    }

    func testProtectedOrStaleTargetsDoNotEvenOpenConfirmation() async {
        _ = NSApplication.shared
        let commands = Commands(), model = commands.model()
        await model.refresh()
        let delegate = MenuAppDelegate(model: model)
        delegate.confirmProfileDeletion = { _ in XCTFail("Protected profile prompted for deletion"); return .alertFirstButtonReturn }
        let callsBefore = commands.calls
        for name in ["one", "default", "missing"] {
            let item = NSMenuItem()
            item.representedObject = name
            delegate.deleteProfile(item)
        }
        XCTAssertEqual(commands.calls, callsBefore)
        commands.current = nil
        _ = await model.refreshMenuData()
        let menu = delegate.makeProfilesMenu()
        for name in ["one", "two", "three"] {
            let item = menu.item(withTitle: name)!.submenu!.items[1]
            XCTAssertFalse(item.isEnabled)
            delegate.deleteProfile(item)
        }
        XCTAssertFalse(commands.calls.contains { $0.prefix(2) == ["profile", "remove"] })
    }
}
