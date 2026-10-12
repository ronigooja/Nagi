import XCTest
@testable import NagiApp

@MainActor final class RefreshTests: XCTestCase {
    @MainActor private final class Commands {
        var calls: [[String]] = []
        var date = Date(timeIntervalSince1970: 1_000)
        var profile = "one"
        var running = false
        var schedule: Int64 = 3600
        var failures: Set<String> = []
        var blocked: String?
        var continuation: CheckedContinuation<Void, Never>?

        func run(_ arguments: [String]) async throws -> JSONValue {
            calls.append(arguments)
            let command = arguments.joined(separator: " ")
            let capturedSchedule = schedule
            if command == blocked {
                blocked = nil
                await withCheckedContinuation { continuation = $0 }
            }
            if failures.contains(command) { throw CLIError.command("fixture", "Read failed") }
            let json: String
            switch command {
            case "status": json = "{\"running\":\(running),\"profile\":\"\(profile)\"}"
            case "profile list": json = "{\"current\":\"\(profile)\",\"profiles\":[\"one\",\"two\"]}"
            case "subscription list": json = "{\"subscriptions\":[{\"name\":\"feed\",\"schedule_seconds\":\(capturedSchedule)}]}"
            case "subscription schedule feed off": schedule = 0; json = "{}"
            case "profile use two": profile = "two"; json = "{\"current\":\"two\"}"
            default: json = "{\"default_override_applied\":false,\"installed\":false}"
            }
            return try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        }
        func model() -> AppModel {
            AppModel(cli: NagiCLIClient(executable: URL(fileURLWithPath: "/nagi-refresh-test-no-cli")), now: { self.date }, runCommand: { try await self.run($0) })
        }
        func count(_ command: String) -> Int { calls.filter { $0.joined(separator: " ") == command }.count }
        func release() { continuation?.resume(); continuation = nil }
    }

    private func waitUntil(_ condition: () -> Bool) async {
        for _ in 0..<200 {
            if condition() { return }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTFail("Condition did not become true")
    }

    func testConcurrentPeriodicAndMenuReadsAreCoalesced() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        commands.blocked = "status"
        let periodic = Task { await model.refresh() }
        await waitUntil { commands.continuation != nil }
        let menu = Task { await model.refreshMenuData() }
        await Task.yield()
        commands.release()
        await periodic.value
        _ = await menu.value
        XCTAssertEqual(commands.count("status"), 1)
        for command in ["profile list", "subscription list", "service status"] {
            XCTAssertEqual(commands.count(command), 1, command)
        }
    }

    func testMetadataTTLAndMenuAlwaysReadsFreshMetadata() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        await model.refresh()
        commands.date.addTimeInterval(5)
        await model.refresh()
        XCTAssertEqual(commands.count("status"), 2)
        XCTAssertEqual(commands.count("subscription list"), 1)
        XCTAssertEqual(commands.count("service status"), 1)
        commands.date.addTimeInterval(26)
        await model.refresh()
        XCTAssertEqual(commands.count("subscription list"), 2)
        XCTAssertEqual(commands.count("service status"), 1)
        _ = await model.refreshMenuData()
        XCTAssertEqual(commands.count("subscription list"), 3)
        XCTAssertEqual(commands.count("service status"), 2)
    }

    func testMutationDiscardsOlderSubscriptionResultAndForcesFreshRead() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        await model.refresh()
        commands.blocked = "subscription list"
        let stale = Task { await model.refreshMenuData() }
        await waitUntil { commands.continuation != nil }
        model.scheduleSubscription("feed", seconds: nil)
        await waitUntil { commands.count("subscription schedule feed off") == 1 }
        XCTAssertTrue(model.busy)
        commands.release()
        _ = await stale.value
        await waitUntil { !model.busy }
        XCTAssertEqual(model.subscriptions.first?.scheduleSeconds, 0)
        XCTAssertEqual(commands.count("subscription list"), 3)
    }

    func testTargetedSubscriptionActionsOnlyReadSubscriptionList() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        for command in [["subscription", "schedule", "feed", "off"], ["subscription", "key", "feed", "clear"]] {
            commands.calls = []
            model.perform(command)
            await waitUntil { !model.busy }
            XCTAssertEqual(commands.calls, [command, ["subscription", "list"]])
        }
    }

    func testAutomaticAndProviderActionsReadOnlyAffectedRuntimeData() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        commands.running = true
        await model.refresh()
        defer { model.pauseForSleep() }
        for action in [["proxy", "auto", "group"], ["proxy", "provider", "update", "feed"], ["proxy", "provider", "healthcheck", "feed"]] {
            commands.calls = []
            model.perform(action)
            await waitUntil { !model.busy }
            let reads = action[1] == "auto" ? [["proxy", "groups"]] : [["proxy", "groups"], ["proxy", "providers"]]
            XCTAssertEqual(commands.calls, [action] + reads)
        }
    }

    func testBusyMenuRequestIsDrainedAfterMutation() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        commands.blocked = "subscription schedule feed off"
        model.scheduleSubscription("feed", seconds: nil)
        await waitUntil { commands.continuation != nil }
        let refreshed = await model.refreshMenuData()
        XCTAssertFalse(refreshed)
        XCTAssertEqual(commands.count("subscription list"), 0)
        commands.release()
        await waitUntil { !model.busy }
        XCTAssertEqual(commands.count("subscription list"), 1)
        XCTAssertEqual(commands.count("profile list"), 1)
        XCTAssertEqual(commands.count("service status"), 1)
    }

    func testRetryStartDrainsQueuedStartupRefresh() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        model.retryStart()
        await waitUntil { commands.count("status") == 1 && commands.count("subscription list") == 1 }
        XCTAssertFalse(model.busy)
        XCTAssertEqual(model.state, .stopped)
    }

    func testProfileChangesKeepOverrideBindingCurrent() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        await model.refresh()
        commands.calls = []
        commands.profile = "two"
        await model.refresh()
        XCTAssertEqual(model.profile, "two")
        XCTAssertEqual(commands.count("profile override default-status two"), 1)
        XCTAssertEqual(commands.count("service status"), 0)
        commands.calls = []
        model.perform(["profile", "use", "two"])
        await waitUntil { !model.busy }
        XCTAssertEqual(commands.count("profile override default-status two"), 1)
        XCTAssertEqual(commands.count("subscription list"), 0)
    }

    func testFailedMetadataIsRetriedWithoutWaitingForTTL() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        commands.failures = ["subscription list"]
        await model.refresh()
        commands.failures = []
        await model.refresh()
        XCTAssertEqual(commands.count("subscription list"), 2)
        XCTAssertEqual(model.subscriptions.count, 1)
    }

    func testSleepRejectsLateReadAndSubsequentRefreshRecovers() async {
        let commands = Commands(), model: AppModel
        model = commands.model()
        commands.blocked = "subscription list"
        let stale = Task { await model.refreshMenuData() }
        await waitUntil { commands.continuation != nil }
        model.pauseForSleep()
        commands.release()
        _ = await stale.value
        XCTAssertTrue(model.subscriptions.isEmpty)
        // Wake starts the scheduler watch; sleeping again cancels its retry.
        model.resumeAfterWake()
        await model.refresh()
        XCTAssertEqual(model.subscriptions.count, 1)
        model.pauseForSleep()
    }
}
