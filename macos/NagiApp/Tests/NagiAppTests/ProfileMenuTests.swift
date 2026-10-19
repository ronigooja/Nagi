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
            if arguments.count == 3 && arguments.prefix(2) == ["profile", "use"] {
                current = arguments[2]
            }
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

    func testProfilesSwitchDirectlyAndManagementKeepsDeletionSeparate() async throws {
        _ = NSApplication.shared
        let commands = Commands(), model = commands.model()
        await model.refresh()
        let delegate = MenuAppDelegate(model: model)
        let menu = delegate.makeProfilesMenu()
        XCTAssertEqual(Array(menu.items.prefix(commands.profiles.count)).map(\.title), commands.profiles)
        XCTAssertEqual(menu.item(withTitle: "one")?.state, .on)
        XCTAssertEqual(menu.item(withTitle: "two")?.state, .off)
        for name in commands.profiles {
            let item = try XCTUnwrap(menu.item(withTitle: name))
            XCTAssertNil(item.submenu)
            XCTAssertEqual(item.representedObject as? [String], ["profile", "use", name])
            XCTAssertTrue(item.target === delegate)
            XCTAssertTrue(item.isEnabled)
        }
        XCTAssertTrue(menu.items[commands.profiles.count].isSeparatorItem)
        let defaults = try XCTUnwrap(menu.item(withTitle: "Apply Default Runtime Settings to Current Profile"))
        XCTAssertNil(defaults.submenu)
        XCTAssertTrue(defaults.isEnabled)
        XCTAssertEqual(defaults.state, .off)
        let management = try XCTUnwrap(menu.item(withTitle: "Manage Profiles")?.submenu)
        XCTAssertEqual(management.items.map(\.title), ["Delete “one”… (Switch profiles first)", "Delete “two”…", "Delete “three”…"])
        XCTAssertFalse(management.items[0].isEnabled)
        XCTAssertFalse(management.items.contains { $0.representedObject as? String == "default" })
        for name in ["two", "three"] {
            let deletion = try XCTUnwrap(management.item(withTitle: "Delete “\(name)”…"))
            XCTAssertEqual(deletion.representedObject as? String, name)
            XCTAssertEqual(deletion.action, #selector(MenuAppDelegate.deleteProfile(_:)))
            XCTAssertTrue(deletion.target === delegate)
            XCTAssertTrue(deletion.isEnabled)
        }
        XCTAssertFalse(commands.calls.contains { $0.prefix(2) == ["profile", "use"] })
        for name in ["two", "default"] {
            let item = try XCTUnwrap(menu.item(withTitle: name))
            let action = try XCTUnwrap(item.action)
            XCTAssertTrue(NSApp.sendAction(action, to: item.target, from: item))
            await waitForCompletion(model)
            XCTAssertEqual(commands.calls.filter { $0 == ["profile", "use", name] }.count, 1)
            XCTAssertEqual(model.profile, name)
            XCTAssertEqual(delegate.makeProfilesMenu().item(withTitle: name)?.state, .on)
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
        let management = try XCTUnwrap(delegate.makeProfilesMenu().item(withTitle: "Manage Profiles")?.submenu)
        let item = try XCTUnwrap(management.item(withTitle: "Delete “three”…"))
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

    func testProtectedOrStaleTargetsDoNotEvenOpenConfirmation() async throws {
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
        let management = try XCTUnwrap(menu.item(withTitle: "Manage Profiles")?.submenu)
        for name in ["one", "two", "three"] {
            let item = management.item(withTitle: "Delete “\(name)”…")!
            XCTAssertFalse(item.isEnabled)
            delegate.deleteProfile(item)
        }
        XCTAssertFalse(commands.calls.contains { $0.prefix(2) == ["profile", "remove"] })
    }
}
