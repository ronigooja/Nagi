import XCTest
@testable import NagiApp

@MainActor final class LANSettingsTests: XCTestCase {
    private func state(_ enabled: Bool = false, _ address: String = "127.0.0.1") -> JSONValue {
        .objectValue(["enabled": .boolValue(enabled), "bind_address": .stringValue(address)])
    }

    func testPortUsesActualMixedListenerAndUnknownStates() async {
        var port: JSONValue? = .numberValue(17890)
        let model = LANSettingsModel(run: { _ in
            var value: [String: JSONValue] = ["enabled": .boolValue(false), "bind_address": .stringValue("127.0.0.1")]
            value["mixed_port"] = port
            return .objectValue(value)
        })
        await model.refresh()
        XCTAssertEqual(model.portText, "17890")
        port = .numberValue(0)
        await model.refresh()
        XCTAssertEqual(model.portText, L10n.text("app.lan.port_disabled"))
        port = nil
        await model.refresh()
        XCTAssertEqual(model.portText, L10n.text("app.lan.port_unknown"))
    }

    func testCLIEnableAddressChangesAndDisableUseConfirmedRuntimeState() async {
        var calls: [[String]] = []
        let model = LANSettingsModel(run: { args in
            calls.append(args)
            return self.state(args[1] == "enable", args.count == 3 ? args[2] : args[1] == "enable" ? "*" : "127.0.0.1")
        })
        XCTAssertFalse(model.canToggle)
        XCTAssertFalse(model.canApplyAddress)
        await model.refresh()
        model.bindAddress = " "
        await model.setEnabled(true)
        XCTAssertEqual(model.state?.enabled, true); XCTAssertEqual(model.bindAddress, "*")
        model.bindAddress = "192.0.2.10"
        await model.applyAddress()
        XCTAssertEqual(model.state?.bindAddress, "192.0.2.10")
        model.bindAddress = "2001:db8::10"
        await model.applyAddress()
        XCTAssertEqual(model.state?.bindAddress, "2001:db8::10")
        await model.setEnabled(false)
        XCTAssertEqual(model.state?.enabled, false)
        XCTAssertEqual(model.state?.bindAddress, "127.0.0.1")
        XCTAssertEqual(calls, [["lan", "status"], ["lan", "enable"], ["lan", "enable", "192.0.2.10"],
                               ["lan", "enable", "2001:db8::10"], ["lan", "disable"]])
    }

    func testInvalidAddressesCannotEnableAndDisabledEditsDoNotWrite() async {
        var calls = 0
        let model = LANSettingsModel(run: { _ in calls += 1; return self.state() })
        await model.refresh()
        for address in ["example.invalid", "192.0.2.10:7890", "[::1]", "999.0.0.1"] {
            model.bindAddress = address
            XCTAssertFalse(model.validAddress)
            await model.setEnabled(true)
        }
        for address in ["", "*", "127.0.0.1", "::1", "2001:db8::1"] {
            model.bindAddress = address
            XCTAssertTrue(model.validAddress)
            XCTAssertFalse(model.canApplyAddress)
            await model.applyAddress()
        }
        XCTAssertEqual(calls, 1)
    }

    func testRefreshPreservesDirtyOrFocusedDraftButReplacesCleanValue() async {
        var actual = "192.0.2.10"
        let model = LANSettingsModel(run: { _ in self.state(true, actual) })
        await model.refresh()
        XCTAssertEqual(model.bindAddress, actual)
        model.setEditing(true)
        actual = "192.0.2.11"
        await model.refresh()
        XCTAssertEqual(model.bindAddress, "192.0.2.10")
        model.setEditing(false)
        model.bindAddress = "2001:db8::1"
        await model.refresh()
        XCTAssertEqual(model.bindAddress, "2001:db8::1")
        XCTAssertEqual(model.state?.bindAddress, actual)
    }

    func testFailedMutationReconcilesAndRetainsErrorWithoutRetrying() async {
        var calls: [[String]] = []
        let model = LANSettingsModel(run: { args in
            calls.append(args)
            if args[1] == "enable" { throw CLIError.command("timeout", "Listener readback timed out") }
            return self.state(calls.count > 1, calls.count > 1 ? "*" : "127.0.0.1")
        })
        await model.refresh(); await model.setEnabled(true)
        XCTAssertFalse(model.stale); XCTAssertEqual(model.state?.enabled, true)
        XCTAssertTrue(model.error?.contains("Listener readback timed out") == true)
        XCTAssertEqual(calls, [["lan", "status"], ["lan", "enable"], ["lan", "status"]])
    }

    func testEditingFocusChangesAndRefreshNeverSubmitAddressDraft() async {
        var calls: [[String]] = []
        let model = LANSettingsModel(run: { args in
            calls.append(args)
            return self.state(true, "192.0.2.10")
        })
        await model.refresh()
        XCTAssertFalse(model.canApplyAddress)
        model.setEditing(true)
        model.bindAddress = "*"
        XCTAssertTrue(model.canApplyAddress)
        model.setEditing(false)
        await model.refresh()
        XCTAssertEqual(model.bindAddress, "*")
        XCTAssertEqual(model.state?.bindAddress, "192.0.2.10")
        XCTAssertEqual(calls, [["lan", "status"], ["lan", "status"]])
        model.bindAddress = "invalid"
        XCTAssertFalse(model.canApplyAddress)
        model.bindAddress = "192.0.2.10"
        XCTAssertFalse(model.canApplyAddress)
    }

    func testFailedAddressSaveRetainsDraftWithoutRetryOnFocusOrRefresh() async {
        var calls: [[String]] = []
        let model = LANSettingsModel(run: { args in
            calls.append(args)
            if args[1] == "enable" { throw CLIError.command("failure", "Address unavailable") }
            return self.state(true, "192.0.2.10")
        })
        await model.refresh()
        model.bindAddress = "2001:db8::10"
        await model.applyAddress()
        XCTAssertEqual(model.bindAddress, "2001:db8::10")
        XCTAssertEqual(model.state?.bindAddress, "192.0.2.10")
        XCTAssertTrue(model.error?.contains("Address unavailable") == true)
        XCTAssertTrue(model.canApplyAddress)
        model.setEditing(true); model.setEditing(false)
        await model.refresh()
        XCTAssertEqual(model.bindAddress, "2001:db8::10")
        XCTAssertEqual(calls, [["lan", "status"], ["lan", "enable", "2001:db8::10"],
                               ["lan", "status"], ["lan", "status"]])
    }

    func testFailedReadAndMalformedStateRemainUnknownAndPreventWrites() async {
        var calls = 0
        let model = LANSettingsModel(run: { _ in
            calls += 1
            if calls == 1 { return self.state() }
            if calls == 2 { throw CLIError.command("stopped", "Engine stopped") }
            return .objectValue([:])
        })
        await model.refresh(); await model.setEnabled(true)
        XCTAssertTrue(model.stale); XCTAssertFalse(model.canToggle); XCTAssertNotNil(model.error)
        XCTAssertEqual(model.state?.enabled, false)
        await model.setEnabled(true)
        XCTAssertEqual(calls, 3)
        let unknown = LANSettingsModel(run: { _ in .objectValue([:]) })
        await unknown.refresh()
        XCTAssertNil(unknown.state); XCTAssertTrue(unknown.stale); XCTAssertFalse(unknown.canToggle)
    }

    func testFailedDisableRetainsSpecificAddressAfterConfirmedDisable() async {
        var disabled = false
        let model = LANSettingsModel(run: { args in
            if args == ["lan", "disable"] {
                disabled = true
                throw CLIError.command("timeout", "Reply lost")
            }
            return self.state(!disabled, disabled ? "127.0.0.1" : "192.0.2.10")
        })
        await model.refresh()
        await model.setEnabled(false)
        XCTAssertEqual(model.state?.enabled, false)
        XCTAssertEqual(model.bindAddress, "192.0.2.10")
        XCTAssertNotNil(model.error)
    }

    func testSubmittedMutationIsNotOptimisticAndBlocksOtherSettingsAndQuit() async throws {
        var started = false, release = false
        var calls: [[String]] = []
        let app = AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/no-lan-test-cli")), runCommand: { args in
            calls.append(args)
            if args.prefix(2) == ["lan", "enable"] {
                started = true
                while !release { try? await Task.sleep(nanoseconds: 2_000_000) }
                throw CLIError.command("timeout", "Reply lost")
            }
            return self.state(started, started ? "*" : "127.0.0.1")
        })
        await app.lanSettings.refresh()
        let task = Task { await app.lanSettings.setEnabled(true) }
        for _ in 0..<100 where !started { try await Task.sleep(nanoseconds: 2_000_000) }
        XCTAssertTrue(started); XCTAssertTrue(app.busy)
        XCTAssertFalse(app.lanSettings.canApplyAddress)
        XCTAssertEqual(app.lanSettings.state?.enabled, false)
        app.perform(["mode", "direct"])
        var quitCompleted = false
        app.quit { _ in quitCompleted = true }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertFalse(quitCompleted); XCTAssertFalse(calls.contains(["mode", "direct"]))
        release = true; await task.value
        for _ in 0..<100 where !quitCompleted { try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(quitCompleted)
        XCTAssertEqual(app.lanSettings.state?.enabled, true)
        XCTAssertEqual(calls, [["lan", "status"], ["lan", "enable"], ["lan", "status"], ["quit"]])
    }

    func testLANMutationBlocksSettingsPageRefreshDisplayAndLoginChanges() async throws {
        var started = false, release = false, loginWrites = 0, displayWrites = 0, reads = 0
        let lan = LANSettingsModel(run: { args in
            if args[1] == "enable" {
                started = true
                while !release { try? await Task.sleep(nanoseconds: 2_000_000) }
            }
            return self.state(args[1] == "enable", "*")
        })
        let browser = BrowserSettingsModel(run: { _ in reads += 1; return .objectValue([:]) })
        let settings = SettingsModel(browser: browser, lan: lan, readDisplay: { .both },
            writeDisplay: { _ in displayWrites += 1 }, readAppLogin: { false },
            writeAppLogin: { _ in loginWrites += 1 }, readEngineLogin: { reads += 1; return false },
            writeEngineLogin: { _ in loginWrites += 1 })
        await lan.refresh()
        let task = Task { await lan.setEnabled(true) }
        for _ in 0..<100 where !started { try await Task.sleep(nanoseconds: 2_000_000) }
        await settings.refresh(); settings.setDisplay(.icon)
        await settings.setAppLogin(true); await settings.setEngineLogin(true)
        XCTAssertEqual(reads, 0); XCTAssertEqual(displayWrites, 0); XCTAssertEqual(loginWrites, 0)
        release = true; await task.value
    }
}
