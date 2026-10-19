import XCTest
import AppKit
@testable import NagiApp

@MainActor final class MenuTrackingTests: XCTestCase {
    @MainActor private final class Commands {
        var calls: [[String]] = []
        var providerName = "example"

        func run(_ arguments: [String]) throws -> JSONValue {
            calls.append(arguments)
            let json: String
            switch arguments {
            case ["status"]: json = #"{"running":true,"profile":"default"}"#
            case ["profile", "list"]: json = #"{"current":"default","profiles":["default"]}"#
            case ["subscription", "list"]: json = #"{"subscriptions":[]}"#
            case ["proxy", "groups"]: json = #"{"groups":[]}"#
            case ["proxy", "providers"]:
                json = #"{"providers":[{"name":"\#(providerName)","type":"Proxy","vehicle_type":"HTTP","nodes":["example-node"]}]}"#
            default: json = "{}"
            }
            return try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        }

        func model() -> AppModel {
            AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/nagi-menu-tracking-test-no-cli")),
                     runCommand: { try self.run($0) })
        }
    }

    func testLeavingLatencySubmenuKeepsProviderSettingsAndRootItemsAlive() async throws {
        _ = NSApplication.shared
        let commands = Commands(), model = commands.model()
        await model.refresh()
        let delegate = MenuAppDelegate(model: model), root = delegate.menu
        delegate.menuWillOpen(root)
        let summary = try XCTUnwrap(root.items.first)
        let providers = try XCTUnwrap(root.item(withTitle: "Proxy Providers")?.submenu)
        let actions = try XCTUnwrap(providers.item(withTitle: "example")?.submenu)
        let tests = try XCTUnwrap(actions.item(withTitle: "Test Latency")?.submenu)
        let settings = try XCTUnwrap(actions.item(withTitle: "Update Settings…"))

        // Exercise nested lifecycle callbacks defensively, even if AppKit only
        // delivers root callbacks on a particular macOS release.
        for submenu in [providers, actions, tests] {
            delegate.menuWillOpen(submenu)
            delegate.menuNeedsUpdate(submenu)
        }
        let callsBefore = commands.calls
        await Task.yield()
        XCTAssertEqual(commands.calls, callsBefore)

        // Complete a root-menu refresh while the provider menu is open.
        commands.providerName = "updated-example"
        delegate.menuNeedsUpdate(root)
        for _ in 0..<50 { await Task.yield() }
        XCTAssertGreaterThan(commands.calls.filter { $0 == ["status"] }.count, 1)
        XCTAssertEqual(model.providers.first?.name, "updated-example")
        delegate.menuDidClose(tests)
        delegate.menuNeedsUpdate(root)
        XCTAssertTrue(root.items.first === summary)
        XCTAssertTrue(root.item(withTitle: "Proxy Providers")?.submenu === providers)
        XCTAssertTrue(actions.item(withTitle: "Update Settings…") === settings)
        XCTAssertTrue(settings.menu === actions)
        XCTAssertEqual(settings.representedObject as? String, "example")
        XCTAssertNotNil(settings.action)
        XCTAssertTrue(settings.target === delegate)

        delegate.menuDidClose(actions)
        delegate.menuDidClose(providers)
        delegate.menuDidClose(root)
        XCTAssertTrue(root.items.first === summary)
        delegate.menuWillOpen(root)
        XCTAssertFalse(root.items.first === summary)
        let updatedProviders = try XCTUnwrap(root.item(withTitle: "Proxy Providers")?.submenu)
        XCTAssertNil(updatedProviders.item(withTitle: "example"))
        XCTAssertNotNil(updatedProviders.item(withTitle: "updated-example"))
        delegate.menuDidClose(root)
    }

    func testRepeatedRootOpenCallbackDoesNotReplaceTrackedItems() {
        _ = NSApplication.shared
        let delegate = MenuAppDelegate(model: Commands().model()), root = delegate.menu
        delegate.menuWillOpen(root)
        let summary = root.items.first
        delegate.menuWillOpen(root)
        XCTAssertTrue(root.items.first === summary)
        delegate.menuDidClose(root)
    }
}
