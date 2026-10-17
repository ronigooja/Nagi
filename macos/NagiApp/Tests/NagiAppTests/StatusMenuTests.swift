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

    func testErrorsTakePriorityAndSubmenuContainsOnlyMessages() throws {
        _ = NSApplication.shared
        let item = MenuAppDelegate.makeStatusMenuItem(
            state: .stopped, errors: ["Quit failed", "Status read failed"],
            notice: "Profile deleted.", tunEnabled: true, tunAdapter: "down")
        XCTAssertEqual(item.title, "Nagi · Error \u{FFFC}")
        let details = try XCTUnwrap(item.submenu)
        XCTAssertEqual(details.items.map(\.title), [
            "Quit failed", "Status read failed",
            "TUN adapter: down · Check logs in CLI", "Profile deleted."
        ])
    }

    func testTUNWarningAloneIsAnErrorAndHealthyOrDisabledTUNIsNot() throws {
        _ = NSApplication.shared
        for adapter: String? in [nil, "down", "absent"] {
            let item = MenuAppDelegate.makeStatusMenuItem(
                state: .running, errors: [], notice: nil, tunEnabled: true, tunAdapter: adapter)
            XCTAssertEqual(item.title, "Nagi · Error \u{FFFC}")
            XCTAssertEqual(item.submenu?.items.last?.title,
                           "TUN adapter: \(adapter ?? "unknown") · Check logs in CLI")
        }
        for (enabled, adapter) in [(true, "up"), (false, "down")] {
            let item = MenuAppDelegate.makeStatusMenuItem(
                state: .running, errors: [], notice: nil, tunEnabled: enabled, tunAdapter: adapter)
            XCTAssertEqual(item.title, "Nagi · Running")
            XCTAssertNil(item.submenu)
        }
    }

    func testSummarySymbolsShareSizeAndBaselineAndFollowText() throws {
        _ = NSApplication.shared
        let error = MenuAppDelegate.makeStatusMenuItem(
            state: .running, errors: ["Failed"], notice: nil, tunEnabled: false, tunAdapter: nil)
        let info = MenuAppDelegate.makeStatusMenuItem(
            state: .running, errors: [], notice: "Saved", tunEnabled: false, tunAdapter: nil)
        var bounds: [NSRect] = []
        for item in [error, info] {
            let title = try XCTUnwrap(item.attributedTitle)
            XCTAssertEqual(title.string, item.title)
            XCTAssertTrue(title.string.hasSuffix(" \u{FFFC}"))
            let attachment = try XCTUnwrap(title.attribute(.attachment, at: title.length - 1,
                                                           effectiveRange: nil) as? NSTextAttachment)
            XCTAssertNotNil(attachment.image)
            XCTAssertEqual(attachment.image?.size, NSSize(width: 14, height: 14))
            XCTAssertEqual(attachment.bounds.size, NSSize(width: 14, height: 14))
            bounds.append(attachment.bounds)
        }
        XCTAssertEqual(bounds[0], bounds[1])
    }

    func testInfoDetailsWrapAndKeepFullTextIncludingExplicitLineBreaks() throws {
        _ = NSApplication.shared
        let short = MenuAppDelegate.makeStatusMenuItem(
            state: .running, errors: [], notice: "Saved.", tunEnabled: false, tunAdapter: nil)
        let text = String(repeating: "Long diagnostic message with details. ", count: 12)
            + "\nAdditional information."
        let long = MenuAppDelegate.makeStatusMenuItem(
            state: .running, errors: [], notice: text, tunEnabled: false, tunAdapter: nil)
        XCTAssertEqual(long.title, "Nagi · Info \u{FFFC}")
        XCTAssertEqual(long.submenu?.items.map(\.title), [text])
        let detail = try XCTUnwrap(long.submenu?.items.last)
        let view = try XCTUnwrap(detail.view)
        let label = try XCTUnwrap(view.subviews.first as? NSTextField)
        XCTAssertEqual(label.stringValue, text)
        XCTAssertEqual(detail.toolTip, label.stringValue)
        XCTAssertEqual(label.maximumNumberOfLines, 0)
        XCTAssertEqual(label.lineBreakMode, .byWordWrapping)
        XCTAssertEqual(view.frame.width, 320)
        let shortView = try XCTUnwrap(short.submenu?.items.last?.view)
        XCTAssertGreaterThan(view.frame.height, shortView.frame.height)
        XCTAssertFalse(detail.isEnabled)
    }
}
