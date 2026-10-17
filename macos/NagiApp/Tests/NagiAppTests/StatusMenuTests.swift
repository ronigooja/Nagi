import XCTest
import AppKit
@testable import NagiApp

@MainActor final class StatusMenuTests: XCTestCase {
    func testNoMessagesShowsEngineStateWithoutSubmenu() {
        _ = NSApplication.shared
        let cases: [(EngineState, String)] = [
            (.starting, "Starting…"), (.running, "Running"),
            (.stopped, "Stopped"), (.unavailable, "Status unavailable")
        ]
        for (state, title) in cases {
            let item = MenuAppDelegate.makeStatusMenuItem(
                state: state, errors: [], notice: nil, tunEnabled: false, tunAdapter: nil)
            XCTAssertEqual(item.title, "Nagi · \(title)")
            XCTAssertNil(item.submenu)
            XCTAssertFalse(item.isEnabled)
        }
    }

    func testErrorsTakePriorityAndRetainAllMessagesAndEngineState() throws {
        _ = NSApplication.shared
        let item = MenuAppDelegate.makeStatusMenuItem(
            state: .stopped, errors: ["Quit failed", "Status read failed"],
            notice: "Profile deleted.", tunEnabled: true, tunAdapter: "down")
        XCTAssertEqual(item.title, "Nagi · Error ⚠")
        let details = try XCTUnwrap(item.submenu)
        XCTAssertEqual(details.items.filter { !$0.isSeparatorItem }.map(\.title), [
            "Engine: Stopped", "⚠ Quit failed", "⚠ Status read failed",
            "⚠ TUN adapter: down · Check logs in CLI", "ⓘ Profile deleted."
        ])
    }

    func testTUNWarningAloneIsAnErrorAndHealthyOrDisabledTUNIsNot() throws {
        _ = NSApplication.shared
        for adapter: String? in [nil, "down", "absent"] {
            let item = MenuAppDelegate.makeStatusMenuItem(
                state: .running, errors: [], notice: nil, tunEnabled: true, tunAdapter: adapter)
            XCTAssertEqual(item.title, "Nagi · Error ⚠")
            XCTAssertEqual(item.submenu?.items.last?.title,
                           "⚠ TUN adapter: \(adapter ?? "unknown") · Check logs in CLI")
        }
        for (enabled, adapter) in [(true, "up"), (false, "down")] {
            let item = MenuAppDelegate.makeStatusMenuItem(
                state: .running, errors: [], notice: nil, tunEnabled: enabled, tunAdapter: adapter)
            XCTAssertEqual(item.title, "Nagi · Running")
            XCTAssertNil(item.submenu)
        }
    }

    func testInfoDetailsWrapAndKeepFullTextIncludingExplicitLineBreaks() throws {
        _ = NSApplication.shared
        let short = MenuAppDelegate.makeStatusMenuItem(
            state: .running, errors: [], notice: "Saved.", tunEnabled: false, tunAdapter: nil)
        let text = String(repeating: "Long diagnostic message with details. ", count: 12)
            + "\nAdditional information."
        let long = MenuAppDelegate.makeStatusMenuItem(
            state: .running, errors: [], notice: text, tunEnabled: false, tunAdapter: nil)
        XCTAssertEqual(long.title, "Nagi · Info ⓘ")
        XCTAssertEqual(long.submenu?.items.first?.title, "Engine: Running")
        let detail = try XCTUnwrap(long.submenu?.items.last)
        let view = try XCTUnwrap(detail.view)
        let label = try XCTUnwrap(view.subviews.first as? NSTextField)
        XCTAssertEqual(label.stringValue, "ⓘ \(text)")
        XCTAssertEqual(detail.toolTip, label.stringValue)
        XCTAssertEqual(label.maximumNumberOfLines, 0)
        XCTAssertEqual(label.lineBreakMode, .byWordWrapping)
        XCTAssertEqual(view.frame.width, 320)
        let shortView = try XCTUnwrap(short.submenu?.items.last?.view)
        XCTAssertGreaterThan(view.frame.height, shortView.frame.height)
        XCTAssertFalse(detail.isEnabled)
    }
}
