import XCTest
@testable import NagiApp

final class SourceSettingsStateTests: XCTestCase {
    func testUpdateOffIgnoresInvalidDisabledIntervalAndCanReturnToSavedValue() {
        var session = SourceSettingsSession()
        session.loadUpdate(seconds: 3600)
        XCTAssertFalse(session.canSaveUpdate)
        session.update.automatic = false
        session.update.interval.selectedIndex = session.update.interval.presets.count
        session.update.interval.customText = "invalid"
        XCTAssertEqual(session.update.value, 0)
        XCTAssertTrue(session.canSaveUpdate)
        session.loadUpdate(seconds: 0)
        XCTAssertFalse(session.dirty)
        session.update.automatic = true
        XCTAssertEqual(session.update.value, 3600)
        XCTAssertTrue(session.canSaveUpdate)
    }

    func testPresetAndCustomValuesCompareBySavedSeconds() {
        var session = SourceSettingsSession()
        session.loadUpdate(seconds: 3600)
        session.update.interval.selectedIndex = session.update.interval.presets.count
        session.update.interval.customText = "3600"
        XCTAssertFalse(session.dirty)
        XCTAssertFalse(session.canSaveUpdate)
        session.update.interval.customText = "7200"
        XCTAssertTrue(session.canSaveUpdate)
        session.update.interval.customText = "31536001"
        XCTAssertTrue(session.dirty)
        XCTAssertFalse(session.canSaveUpdate)
    }

    func testInheritedIntervalOnlySavesAfterChangingPolicy() {
        var session = SourceSettingsSession()
        session.loadInterval(seconds: 600, inherited: true)
        XCTAssertFalse(session.canSaveInterval)
        session.interval.interval.selectedIndex = 0
        XCTAssertFalse(session.dirty)
        session.interval.inherited = false
        XCTAssertEqual(session.interval.value, 300)
        XCTAssertTrue(session.canSaveInterval)
        session.interval.inherited = true
        XCTAssertNil(session.interval.value)
        XCTAssertFalse(session.dirty)
    }

    func testCustomIntervalCanRestoreInheritedEvenWithInvalidDisabledText() {
        var session = SourceSettingsSession()
        session.loadInterval(seconds: 600, inherited: false)
        session.interval.inherited = true
        session.interval.interval.selectedIndex = session.interval.interval.presets.count
        session.interval.interval.customText = ""
        XCTAssertNil(session.interval.value)
        XCTAssertTrue(session.canSaveInterval)
        session.interval.inherited = false
        XCTAssertFalse(session.canSaveInterval)
        XCTAssertTrue(session.needsDiscardConfirmation)
    }

    func testProviderWithoutInheritedURLCannotSubmitReset() {
        var session = SourceSettingsSession()
        session.loadURL(inherited: true, canReset: false)
        XCTAssertFalse(session.urlInherited)
        XCTAssertTrue(session.urlText.isEmpty)
        XCTAssertFalse(session.dirty)
        XCTAssertFalse(session.canSaveURL)
        session.urlInherited = true
        XCTAssertFalse(session.canSaveURL)
        session.urlInherited = false
        session.urlText = "https://example.invalid/check"
        XCTAssertTrue(session.canSaveURL)
    }

    func testReplacementURLStartsBlankAndRejectsUnsafeOrIncompleteValues() {
        var session = SourceSettingsSession()
        session.loadURL(inherited: false, canReset: true)
        XCTAssertEqual(session.urlText, "")
        XCTAssertFalse(session.canSaveURL)
        for value in [" ", "ftp://example.invalid", "https://user:secret@example.invalid/check", "https://example.invalid/#fragment", "https://example.invalid/a b"] {
            session.urlText = value
            XCTAssertFalse(session.canSaveURL, value)
        }
        session.urlText = "https://example.invalid/check?token=placeholder"
        XCTAssertTrue(session.canSaveURL)
        session.urlInherited = true
        XCTAssertTrue(session.canSaveURL)
    }

    func testUnappliedSubscriptionCanSetIntervalWithoutInheritedSeconds() {
        var session = SourceSettingsSession()
        session.loadInterval(seconds: nil, inherited: true)
        session.loadURL(inherited: true, canReset: true)
        XCTAssertFalse(session.dirty)
        session.interval.inherited = false
        XCTAssertFalse(session.canSaveInterval)
        session.interval.interval.selectedIndex = 1
        XCTAssertEqual(session.interval.value, 600)
        XCTAssertTrue(session.canSaveInterval)
        session.urlInherited = false
        session.urlText = "https://example.invalid/check"
        XCTAssertTrue(session.canSaveURL)
    }

    func testSuccessfulIntervalAndFailedURLKeepIndependentState() {
        var session = SourceSettingsSession()
        session.loadInterval(seconds: 300, inherited: false)
        session.loadURL(inherited: true, canReset: true)
        session.interval.interval.selectedIndex = 1
        session.urlInherited = false
        session.urlText = "https://example.invalid/check"
        XCTAssertTrue(session.begin(.interval))
        session.saveSucceeded(.interval)
        session.refreshSucceeded(.interval)
        XCTAssertFalse(session.intervalChanged)
        XCTAssertTrue(session.urlChanged)
        XCTAssertEqual(session.urlText, "https://example.invalid/check")
        XCTAssertTrue(session.begin(.url))
        session.failed(.url)
        XCTAssertFalse(session.intervalChanged)
        XCTAssertTrue(session.canSaveURL)
        XCTAssertEqual(session.urlText, "https://example.invalid/check")
        XCTAssertTrue(session.needsDiscardConfirmation)
    }

    func testSavedReadFailureBlocksResubmissionUntilReadRetrySucceeds() {
        var session = SourceSettingsSession()
        session.loadInterval(seconds: 300, inherited: false)
        session.loadURL(inherited: true, canReset: true)
        session.interval.interval.selectedIndex = 1
        session.urlInherited = false
        session.urlText = "https://example.invalid/unsaved"
        XCTAssertTrue(session.begin(.interval))
        session.saveSucceeded(.interval)
        session.failed(.interval)
        XCTAssertTrue(session.pendingRefresh.contains(.interval))
        XCTAssertFalse(session.intervalChanged)
        XCTAssertFalse(session.canSaveInterval)
        XCTAssertTrue(session.canSaveURL)
        XCTAssertEqual(session.intervalBaseline?.value, 600)
        XCTAssertTrue(session.begin(.interval))
        session.refreshSucceeded(.interval)
        XCTAssertTrue(session.pendingRefresh.isEmpty)
        XCTAssertFalse(session.canSaveInterval)
        XCTAssertEqual(session.urlText, "https://example.invalid/unsaved")
        XCTAssertTrue(session.canSaveURL)
    }

    func testInFlightSaveRejectsSecondOperationAndPreventsClose() {
        var session = SourceSettingsSession()
        session.loadUpdate(seconds: 3600)
        session.update.automatic = false
        XCTAssertTrue(session.needsDiscardConfirmation)
        XCTAssertTrue(session.begin(.update))
        XCTAssertFalse(session.begin(.update))
        XCTAssertFalse(session.begin(.url))
        XCTAssertFalse(session.canSaveUpdate)
        XCTAssertFalse(session.canClose)
        XCTAssertFalse(session.needsDiscardConfirmation)
        session.failed(.update)
        XCTAssertTrue(session.canClose)
        XCTAssertTrue(session.needsDiscardConfirmation)
        XCTAssertTrue(session.canSaveUpdate)
        XCTAssertTrue(session.begin(.update))
        session.saveSucceeded(.update)
        session.refreshSucceeded(.update)
        XCTAssertTrue(session.canClose)
        XCTAssertFalse(session.needsDiscardConfirmation)
        XCTAssertFalse(session.dirty)
    }
}
