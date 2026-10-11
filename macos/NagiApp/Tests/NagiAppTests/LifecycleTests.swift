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
