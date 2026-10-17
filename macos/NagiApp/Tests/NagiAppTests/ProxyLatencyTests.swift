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
        XCTAssertEqual(delays["fast"]?.label, "<1 ms")
        XCTAssertEqual(delays["bad"]?.label, "Failed")
        XCTAssertEqual(delays["new"]?.label, "Untested")
        XCTAssertEqual(NodeDelay().label, "Untested")
        XCTAssertTrue(delays["fast"]!.detail.contains("Measured at: \(MenuAppDelegate.subscriptionTime("2026-10-16T10:00:00Z"))"))
        XCTAssertFalse(delays["fast"]!.detail.contains("2026-10-16T10:00:00Z"))
        XCTAssertTrue(delays["fast"]!.detail.contains("Test origin: https://example.com"))
    }

    func testUnavailableHistoryShowsUnknownWithItsReason() throws {
        for reason in ["ambiguous node ownership", "history unavailable"] {
            let delay = NodeDelay(try decode("{\"status\":\"untested\",\"reason\":\"\(reason)\"}"))
            XCTAssertEqual(delay.label, "Unknown")
            XCTAssertTrue(delay.detail.contains("Reason: \(reason)"))
            XCTAssertFalse(delay.detail.contains("Untested"))
        }
        let untested = NodeDelay(try decode("{\"status\":\"untested\",\"reason\":\"\"}"))
        XCTAssertEqual(untested.label, "Untested")
    }

    func testMenusKeepSelectionSeparateFromTestsAndDiscloseAutomaticReset() throws {
        _ = NSApplication.shared
        let model = AppModel()
        let delegate = MenuAppDelegate(model: model)
        for kind in ["Selector", "URLTest", "Fallback"] {
            let group = ProxyGroup(name: "group", kind: kind, selected: "one", fixed: "one", selectedStatus: nil,
                                   nodes: ["one", "two"], hidden: false, icon: nil, nodeInfo: ["one": NodeInfo(kind: "node", status: nil), "two": NodeInfo(kind: "node", status: nil)])
            let menu = delegate.makeGroupMenu(group)
            let choice = try XCTUnwrap(menu.item(withTitle: "one · Untested"))
            XCTAssertEqual(choice.representedObject as? [String], ["proxy", "select", "group", "one"])
            XCTAssertEqual(choice.state, .on)
            let tests = try XCTUnwrap(menu.item(withTitle: "Test Single Node")?.submenu)
            XCTAssertEqual(tests.items[0].representedObject as? [String], ["proxy", "test", "group", "group", "one"])
            let batch = try XCTUnwrap(menu.item(withTitle: group.testTitle))
            XCTAssertEqual(batch.representedObject as? [String], ["proxy", "delays", "group"])
            if group.automatic {
                XCTAssertTrue(batch.title.contains("Restores Automatic Selection"))
                XCTAssertTrue(batch.toolTip!.contains("fixed choice and saved choice"))
                XCTAssertNil(menu.item(withTitle: "Automatic Latency Settings…"))
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
            XCTAssertNil(menu.item(withTitle: "Automatic Latency Settings…"))
            XCTAssertEqual(group.testTitle, "Test Current Group")
            XCTAssertFalse(group.testExplanation.contains("clears"))
        }
    }

    func testExplicitTestsRefreshMeasurementsAfterSuccessAndFailure() async throws {
        var calls: [[String]] = []
        var fail = false
        let model = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/nagi-latency-no-cli")), runCommand: { arguments in
            calls.append(arguments)
            if arguments.prefix(2) == ["proxy", "test"], fail { throw CLIError.command("test", "Test failed") }
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
            model.perform(["proxy", "test", "group", "group", "node"])
            for _ in 0..<200 {
                if !model.busy { break }
                try? await Task.sleep(nanoseconds: 5_000_000)
            }
            XCTAssertFalse(model.busy)
            XCTAssertEqual(calls.filter { $0 == ["proxy", "test", "group", "group", "node"] }.count, 1)
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
    func testGroupAndBuiltinRowsKeepNamesAndSelectionWithoutMeasurements() throws {
        _ = NSApplication.shared
        let delegate = MenuAppDelegate(model: AppModel())
        let failed = NodeDelay(try decode("{\"status\":\"failed\"}"))
        let nodes = ["Child", "Balance", "DIRECT", "REJECT", "Fast", "Unknown"]
        let info = ["Child": NodeInfo(kind: "group", status: "unavailable", selectedDelay: failed),
                    "Balance": NodeInfo(kind: "group", status: nil), "Fast": NodeInfo(kind: "node", status: nil)]
        let group = ProxyGroup(name: "Parent", kind: "Selector", selected: "DIRECT", fixed: nil, selectedStatus: nil,
                               nodes: nodes, hidden: false, icon: nil,
                               nodeDelays: Dictionary(uniqueKeysWithValues: nodes.map { ($0, failed) }), nodeInfo: info)
        let menu = delegate.makeGroupMenu(group)
        for name in ["DIRECT", "REJECT", "Unknown"] {
            let item = try XCTUnwrap(menu.item(withTitle: name))
            XCTAssertNil(item.toolTip)
            XCTAssertEqual(item.state, name == "DIRECT" ? .on : .off)
        }
        XCTAssertNotNil(menu.item(withTitle: "Child · Failed"))
        XCTAssertNotNil(menu.item(withTitle: "Balance · Unknown"))
        let tests = try XCTUnwrap(menu.item(withTitle: "Test Single Node")?.submenu)
        XCTAssertEqual(tests.items.count, 1)
        XCTAssertEqual(tests.items[0].representedObject as? [String], ["proxy", "test", "group", "Parent", "Fast"])
        XCTAssertNil(menu.item(withTitle: "Automatic Latency Settings…"))
    }

    func testNestedGroupShowsSelectedLeafDelayWithoutNodeName() throws {
        let info = NodeInfo.parse(try decode("""
        {"Child":{"kind":"group","status":"available","selected_delay":{"status":"ok","delay_ms":42}},
         "Idle":{"kind":"group","selected_delay":{"status":"untested"}},
         "DirectGroup":{"kind":"group","selected_builtin":"DIRECT"},
         "RejectGroup":{"kind":"group","selected_builtin":"REJECT"}}
        """))
        let group = ProxyGroup(name: "Parent", kind: "Selector", selected: "Child", fixed: nil, selectedStatus: nil,
                               nodes: ["Child", "Idle", "DirectGroup", "RejectGroup"], hidden: false, icon: nil, nodeInfo: info)
        XCTAssertEqual(group.nodeTitle("Child"), "Child · 42 ms")
        XCTAssertEqual(group.nodeTitle("Idle"), "Idle · Untested")
        XCTAssertEqual(group.nodeTitle("DirectGroup"), "DirectGroup · DIRECT")
        XCTAssertEqual(group.nodeTitle("RejectGroup"), "RejectGroup · REJECT")
    }

    func testRejectOnlyGroupCannotStartLatencyTest() throws {
        _ = NSApplication.shared
        let delegate = MenuAppDelegate(model: AppModel())
        let rejected = ProxyGroup(name: "Only", kind: "Selector", selected: "REJECT", fixed: nil, selectedStatus: nil,
                                  nodes: ["REJECT"], hidden: false, icon: nil,
                                  nodeInfo: ["REJECT": NodeInfo(kind: "builtin", status: nil)])
        let test = try XCTUnwrap(delegate.makeGroupMenu(rejected).item(withTitle: rejected.testTitle))
        XCTAssertFalse(rejected.hasLatencyCandidates)
        XCTAssertFalse(test.isEnabled)
        XCTAssertEqual(test.toolTip, "Reject actions have no latency to test.")

        let nested = ProxyGroup(name: "Parent", kind: "Selector", selected: "Child", fixed: nil, selectedStatus: nil,
                                nodes: ["Child"], hidden: false, icon: nil,
                                nodeInfo: ["Child": NodeInfo(kind: "group", status: nil, selectedBuiltin: "REJECT")])
        XCTAssertFalse(nested.hasLatencyCandidates)
        XCTAssertFalse(try XCTUnwrap(delegate.makeGroupMenu(nested).item(withTitle: nested.testTitle)).isEnabled)

        let mixed = ProxyGroup(name: "Mixed", kind: "Selector", selected: "REJECT", fixed: nil, selectedStatus: nil,
                               nodes: ["REJECT", "Node"], hidden: false, icon: nil,
                               nodeInfo: ["REJECT": NodeInfo(kind: "builtin", status: nil), "Node": NodeInfo(kind: "node", status: nil)])
        XCTAssertTrue(mixed.hasLatencyCandidates)
        XCTAssertTrue(try XCTUnwrap(delegate.makeGroupMenu(mixed).item(withTitle: mixed.testTitle)).isEnabled)

        let providerNode = ProxyGroup(name: "Provider", kind: "Selector", selected: "REJECT", fixed: nil, selectedStatus: nil,
                                      nodes: ["REJECT"], hidden: false, icon: nil,
                                      nodeInfo: ["REJECT": NodeInfo(kind: "node", status: nil)])
        XCTAssertTrue(providerNode.hasLatencyCandidates)
        XCTAssertTrue(try XCTUnwrap(delegate.makeGroupMenu(providerNode).item(withTitle: providerNode.testTitle)).isEnabled)
    }

    func testSelectingRefreshesActualFixedChoiceAndNestedAvailability() async throws {
        var calls: [[String]] = []
        let model = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/nagi-latency-no-cli")), runCommand: { arguments in
            calls.append(arguments)
            let json: String
            switch arguments {
            case ["status"]: json = "{\"running\":true,\"profile\":\"one\"}"
            case ["proxy", "groups"]:
                json = """
                {"groups":[{"name":"Parent","type":"URLTest","now":"Child","fixed":"Child","all":["Child"],
                "node_info":{"Child":{"kind":"group","status":"unavailable","selected_delay":{"status":"failed"}}}}]}
                """
            default: json = "{}"
            }
            return try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        })
        await model.refreshMenuData()
        calls.removeAll()
        model.perform(["proxy", "select", "Parent", "Child"])
        for _ in 0..<200 {
            if !model.busy { break }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertFalse(model.busy)
        XCTAssertTrue(calls.contains(["proxy", "groups"]))
        XCTAssertEqual(model.groups.first?.fixed, "Child")
        XCTAssertEqual(model.groups.first?.nodeTitle("Child"), "Child · Failed")
        model.pauseForSleep()
    }

    func testIntervalMutationUsesSubscriptionContextAndReleasesBusyAfterFailure() async throws {
        var calls: [[String]] = []
        let model = AppModel(runCommand: { arguments in
            calls.append(arguments)
            throw CLIError.command("activation", "Activation failed")
        })
        let failure = await model.saveHealthcheckSettings(kind: "subscription", name: "feed", seconds: 120)
        XCTAssertNotNil(failure)
        XCTAssertEqual(calls.first, ["proxy", "healthcheck", "set", "subscription", "feed", "120"])
        XCTAssertFalse(model.busy)
    }

    func testProviderNodeNamedDirectUsesExplicitIdentityAndContext() {
        let provider = ProxyProvider(name: "Feed", type: "http", vehicleType: "HTTP", nodes: ["DIRECT"], nodeInfo: ["DIRECT": NodeInfo(kind: "node", status: nil)])
        XCTAssertEqual(provider.testableNodes, ["DIRECT"])
        XCTAssertEqual(NodeInfo.kind("DIRECT", in: provider.nodeInfo), "node")
        XCTAssertEqual(NodeInfo.kind("DIRECT", in: [:]), "builtin")
    }

}
