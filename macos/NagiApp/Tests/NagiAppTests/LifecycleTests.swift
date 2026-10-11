import XCTest
@testable import NagiApp

final class LifecycleTests: XCTestCase {
    func testAutomaticGroupsAreSelectableAndFixedStateIsRepresentable() {
        let group = ProxyGroup(name: "auto", kind: "URLTest", selected: "a", fixed: "a", selectedStatus: "available", nodes: ["a"], hidden: false, icon: nil)
        XCTAssertTrue(group.selectable)
        XCTAssertEqual(group.fixed, "a")
    }

    @MainActor func testQuotaFormattingSaturatesOverflow() {
        let subscription = Subscription(name: "feed", keyConfigured: false, scheduleSeconds: 3600, schedulerRunning: true, nextDue: nil, pauseReason: nil, lastFailureStage: nil, lastFailureReason: nil, expiresAt: nil, upload: Int64.max, download: 1, total: Int64.max)
        XCTAssertTrue(MenuAppDelegate.quotaLabel(subscription).contains("9223372036854775807"))
    }
}

extension LifecycleTests {
    private func fixture(_ body: String) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-watch-\(UUID().uuidString).sh")
        try ("#!/bin/sh\n" + body + "\n").write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
        return url
    }

    func testDuplicateAndOwnedLongLivedHost() async throws {
        let duplicate = try fixture("printf '{\"ok\":false,\"error\":{\"code\":\"scheduler_' >&2; sleep 0.05; printf 'already_running\",\"message\":\"busy\"}}\\n' >&2; exit 1")
        let duplicateWatch = try SubscriptionWatch(executable: duplicate)
        if case .duplicate = await duplicateWatch.wait() {} else { XCTFail("duplicate host was not classified") }
        let long = try fixture("exec python3 -c 'import signal,time; print(\"{\\\"ok\\\":true,\\\"data\\\":{\\\"scheduler_running\\\":true}}\",flush=True); signal.pause()'")
        let owned = try SubscriptionWatch(executable: long)
        try await Task.sleep(nanoseconds: 100_000_000)
        owned.stop()
        if case .stopped = await owned.wait() {} else { XCTFail("user stop was not classified") }
    }

    func testAbnormalAndOversizedExitFail() async throws {
        let abnormal = try fixture("printf '{\"ok\":true,\"data\":{\"scheduler_running\":true}}\\n'; exit 7")
        let failed = try SubscriptionWatch(executable: abnormal)
        if case .failed = await failed.wait() {} else { XCTFail("abnormal exit was not reported") }
        let oversized = try fixture("python3 -c 'print(\"x\"*70000)'")
        let bounded = try SubscriptionWatch(executable: oversized)
        if case .failed = await bounded.wait() {} else { XCTFail("oversized frame was not bounded") }
    }
}
