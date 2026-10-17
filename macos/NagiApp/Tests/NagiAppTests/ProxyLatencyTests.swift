import XCTest
import AppKit
@testable import NagiApp

@MainActor final class ProxyLatencyTests: XCTestCase {
    private func decode(_ text: String) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))
    }

    func testMeasurementsKeepFailureAndMissingDistinctAndShowContext() throws {
        let delays = NodeDelay.parse(try decode("""
        {"fast":{"status":"ok","delay_ms":0,"measured_at":"2026-10-16T10:00:00Z","test_url":"https://example.com"},
         "bad":{"status":"failed","measured_at":"2026-10-16T10:00:01Z"},"new":{"status":"untested"}}
        """))
        XCTAssertEqual(delays["fast"]?.label, "0 ms")
        XCTAssertEqual(delays["bad"]?.label, "Failed")
        XCTAssertEqual(delays["new"]?.label, "Untested")
        XCTAssertEqual(NodeDelay().label, "Untested")
        XCTAssertTrue(delays["fast"]!.detail.contains("2026-10-16T10:00:00Z"))
        XCTAssertTrue(delays["fast"]!.detail.contains("Test origin: https://example.com"))
    }

    func testMenusKeepSelectionSeparateFromTestsAndDiscloseAutomaticReset() throws {
        _ = NSApplication.shared
        let model = AppModel()
        let delegate = MenuAppDelegate(model: model)
        for kind in ["Selector", "URLTest", "Fallback"] {
            let group = ProxyGroup(name: "group", kind: kind, selected: "one", fixed: "one", selectedStatus: nil,
                                   nodes: ["one", "two"], hidden: false, icon: nil)
            let menu = delegate.makeGroupMenu(group)
            let choice = try XCTUnwrap(menu.item(withTitle: "one · Untested"))
            XCTAssertEqual(choice.representedObject as? [String], ["proxy", "select", "group", "one"])
            XCTAssertEqual(choice.state, .on)
            let tests = try XCTUnwrap(menu.item(withTitle: "Test Single Node")?.submenu)
            XCTAssertEqual(tests.items[0].representedObject as? [String], ["proxy", "delay", "one"])
            let batch = try XCTUnwrap(menu.item(withTitle: group.testTitle))
            XCTAssertEqual(batch.representedObject as? [String], ["proxy", "delays", "group"])
            if group.automatic {
                XCTAssertTrue(batch.title.contains("Restores Automatic Selection"))
                XCTAssertTrue(batch.toolTip!.contains("fixed choice and saved choice"))
                XCTAssertNotNil(menu.item(withTitle: "Healthcheck Settings…"))
            }
        }
        XCTAssertFalse(model.busy) // Constructing/opening menus dispatches no test.
    }

    func testLoadBalanceHealthcheckSettingsKeepMembersReadOnly() throws {
        _ = NSApplication.shared
        let delegate = MenuAppDelegate(model: AppModel())
        for kind in ["LoadBalance", "load-balance"] {
            let group = ProxyGroup(name: "balanced", kind: kind, selected: "one", fixed: nil, selectedStatus: nil,
                                   nodes: ["one"], hidden: false, icon: nil)
            XCTAssertTrue(group.supportsHealthcheck)
            XCTAssertFalse(group.automatic)
            XCTAssertFalse(group.selectable)
            XCTAssertNil(group.restoreAutomaticCommand)
            let menu = delegate.makeGroupMenu(group)
            let member = try XCTUnwrap(menu.item(withTitle: group.nodeTitle("one")))
            XCTAssertFalse(member.isEnabled)
            XCTAssertNil(member.representedObject)
            let settings = try XCTUnwrap(menu.item(withTitle: "Healthcheck Settings…"))
            XCTAssertEqual(settings.representedObject as? [String], ["group", "balanced"])
            XCTAssertEqual(group.testTitle, "Test Current Group")
            XCTAssertFalse(group.testExplanation.contains("clears"))
        }
    }

    func testExplicitTestsRefreshMeasurementsAfterSuccessAndFailure() async throws {
        var calls: [[String]] = []
        var fail = false
        let model = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/nagi-latency-no-cli")), runCommand: { arguments in
            calls.append(arguments)
            if arguments.prefix(2) == ["proxy", "delay"], fail { throw CLIError.command("test", "Test failed") }
            let json: String
            switch arguments {
            case ["status"]: json = "{\"running\":true,\"profile\":\"one\"}"
            case ["proxy", "groups"]:
                json = "{\"groups\":[{\"name\":\"group\",\"type\":\"Selector\",\"all\":[\"node\"],\"node_delays\":{\"node\":{\"status\":\"ok\",\"delay_ms\":12}}}]}"
            case ["proxy", "providers"]:
                json = "{\"providers\":[{\"name\":\"provider\",\"nodes\":[\"node\"],\"node_delays\":{\"node\":{\"status\":\"failed\"}}}]}"
            default: json = "{}"
            }
            return try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        })
        await model.refreshMenuData()
        XCTAssertFalse(calls.contains { $0.prefix(2) == ["proxy", "delay"] || $0.prefix(2) == ["proxy", "delays"] })
        XCTAssertEqual(model.groups.first?.nodeDelays["node"]?.label, "12 ms")
        XCTAssertEqual(model.providers.first?.nodeDelays["node"]?.label, "Failed")
        for shouldFail in [false, true] {
            fail = shouldFail
            calls.removeAll()
            model.perform(["proxy", "delay", "node"])
            for _ in 0..<200 {
                if !model.busy { break }
                try? await Task.sleep(nanoseconds: 5_000_000)
            }
            XCTAssertFalse(model.busy)
            XCTAssertEqual(calls.filter { $0 == ["proxy", "delay", "node"] }.count, 1)
            XCTAssertTrue(calls.contains(["proxy", "groups"]))
            XCTAssertTrue(calls.contains(["proxy", "providers"]))
            if shouldFail { XCTAssertTrue(model.errorMessage?.contains("Test failed") == true) }
        }
        model.pauseForSleep()
    }

    func testHealthcheckReadIsExplicitAndReleasesBusyAfterFailure() async throws {
        var calls: [[String]] = []
        let model = AppModel(runCommand: { arguments in
            calls.append(arguments)
            throw CLIError.command("unavailable", "Settings unavailable")
        })
        do {
            _ = try await model.healthcheckSettings(kind: "provider", name: "feed")
            XCTFail("Expected failure")
        } catch {}
        XCTAssertEqual(calls, [["proxy", "healthcheck", "show", "provider", "feed"]])
        XCTAssertFalse(model.busy)
    }
}
