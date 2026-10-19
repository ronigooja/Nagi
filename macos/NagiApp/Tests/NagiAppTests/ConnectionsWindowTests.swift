import XCTest
import AppKit
@testable import NagiApp

final class ConnectionsWindowTests: XCTestCase {
    @MainActor func testWindowFitsScreenAndKeepsActionsVisibleAtSmallSizes() async throws {
        _ = NSApplication.shared
        let model = ConnectionsModel(fetch: { [] }, execute: { _ in })
        model.setEngineState(.stopped)
        let controller = ConnectionsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        let screen = try XCTUnwrap(window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame)
        let initial = window.frame
        XCTAssertEqual(initial, ConnectionsWindowController.layout.initialFrame(in: screen))
        XCTAssertEqual(window.minSize, ConnectionsWindowController.layout.minimumFrameSize(in: screen))
        controller.present()
        let content = try XCTUnwrap(window.contentView)
        // Exercise small-screen minimums even when the test host has a large display.
        let smallScreen = NSRect(x: 0, y: 0, width: 960, height: 600)
        let minimum = ConnectionsWindowController.layout.minimumFrameSize(in: smallScreen)
        (window as? NativeWindow)?.minimumFrameSize = minimum
        window.minSize = minimum
        for size in [minimum, initial.size, minimum] {
            window.setFrame(NSRect(origin: initial.origin, size: size), display: true)
            content.layoutSubtreeIfNeeded()
            try await Task.sleep(nanoseconds: 20_000_000)
            XCTAssertEqual(window.frame.size, size)
            for button in descendants(content, of: NSButton.self) where !button.isHidden {
                XCTAssertTrue(content.bounds.insetBy(dx: -1, dy: -1).contains(button.convert(button.bounds, to: content)), button.title)
            }
            let details = try XCTUnwrap(descendants(content, of: NSTextView.self).first { $0.identifier?.rawValue == "connection-details" })
            let detailScroll = try XCTUnwrap(details.enclosingScrollView)
            XCTAssertGreaterThanOrEqual(detailScroll.frame.height, 60)
            XCTAssertTrue(content.bounds.insetBy(dx: -1, dy: -1).contains(detailScroll.convert(detailScroll.bounds, to: content)))
        }
        window.close()
        controller.present()
        content.layoutSubtreeIfNeeded()
        XCTAssertEqual(window.frame.size, minimum)
    }

    @MainActor private func descendants<T: NSView>(_ root: NSView, of type: T.Type) -> [T] {
        ((root as? T).map { [$0] } ?? []) + root.subviews.flatMap { descendants($0, of: type) }
    }

    @MainActor func testResizableWindowReusesInstanceAfterClose() throws {
        _ = NSApplication.shared
        let model = ConnectionsModel(fetch: { [] }, execute: { _ in })
        model.setEngineState(.stopped)
        let controller = ConnectionsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        XCTAssertTrue(window.styleMask.contains(.resizable))
        XCTAssertTrue(window.styleMask.contains(.miniaturizable))
        XCTAssertFalse(window.isReleasedWhenClosed)
        controller.present()
        controller.present()
        XCTAssertTrue(controller.window === window)
        window.close()
        controller.present()
        XCTAssertTrue(controller.window === window)
        let content = try XCTUnwrap(window.contentView)
        let table = try XCTUnwrap(descendants(content, of: NSTableView.self).first)
        XCTAssertEqual(table.tableColumns.count, 6)
        XCTAssertTrue(table.tableColumns[4].title.contains("cumulative"))
        XCTAssertTrue(table.tableColumns[5].title.contains("cumulative"))
        XCTAssertEqual(descendants(content, of: NSSearchField.self).count, 1)
        let details = try XCTUnwrap(descendants(content, of: NSTextView.self).first { $0.identifier?.rawValue == "connection-details" })
        XCTAssertFalse(details.isEditable)
        XCTAssertTrue(details.isSelectable)
        window.close()
    }

    @MainActor func testCloseAllConfirmationExplainsFullScopeAndReconnect() {
        let alert = ConnectionsWindowController.closeAllConfirmation()
        XCTAssertEqual(alert.buttons.map(\.title), ["Close All Connections", "Cancel"])
        XCTAssertTrue(alert.informativeText.contains("all current mihomo connections"))
        XCTAssertTrue(alert.informativeText.contains("outside this search"))
        XCTAssertTrue(alert.informativeText.contains("reconnect automatically"))
    }

    @MainActor func testNativeTableSelectionFollowsIDAfterSort() async throws {
        _ = NSApplication.shared
        let snapshot = Data("""
        {"connections":[{"id":"a","metadata":{"host":"alpha.example","destinationPort":"443"},"upload":12,"download":15},{"id":"b","metadata":{"host":"beta.example","destinationPort":"443","processPath":"/Applications/Browser.app/Contents/MacOS/Browser"},"upload":20,"download":30}]}
        """.utf8)
        let connections = try JSONDecoder().decode(ConnectionList.self, from: snapshot).connections
        let model = ConnectionsModel(fetch: { connections }, execute: { _ in })
        model.setEngineState(.running)
        let controller = ConnectionsWindowController(model: model)
        controller.present()
        defer { controller.window?.close() }
        for _ in 0..<100 {
            if model.rows.count == 2 { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTAssertEqual(model.rows.count, 2)
        let content = try XCTUnwrap(controller.window?.contentView)
        let table = try XCTUnwrap(descendants(content, of: NSTableView.self).first)
        table.selectRowIndexes(IndexSet(integer: 1), byExtendingSelection: false)
        XCTAssertEqual(model.selectedID, "b")
        table.sortDescriptors = [NSSortDescriptor(key: ConnectionSortColumn.destination.rawValue, ascending: false)]
        for _ in 0..<100 {
            if model.rows.first?.id == "b" && table.selectedRow == 0 { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTAssertEqual(model.rows.first?.id, "b")
        XCTAssertEqual(table.selectedRow, 0)
        XCTAssertEqual(model.selectedID, "b")
        let details = try XCTUnwrap(descendants(content, of: NSTextView.self).first { $0.identifier?.rawValue == "connection-details" })
        XCTAssertTrue(details.string.contains("ID: b"))
        XCTAssertTrue(details.string.contains("/Applications/Browser.app/Contents/MacOS/Browser"))
    }

    @MainActor func testRefreshAtTopKeepsTableAtTopWhenFirstRowChanges() async throws {
        _ = NSApplication.shared
        let initialRows = (0..<80).map { index in
            let id = index == 0 ? "a" : index == 1 ? "b" : "initial-(index)"
            let host = index == 0 ? "alpha.example" : index == 1 ? "beta.example" : "initial-(index).example"
            return "{\"id\":\"\(id)\",\"metadata\":{\"host\":\"\(host)\",\"destinationPort\":\"443\"}}"
        }.joined(separator: ",")
        var snapshot = try JSONDecoder().decode(ConnectionList.self, from: Data("{\"connections\":[\(initialRows)]}".utf8)).connections
        let model = ConnectionsModel(fetch: { snapshot }, execute: { _ in }, interval: 60_000_000_000)
        model.setEngineState(.running)
        let controller = ConnectionsWindowController(model: model)
        controller.present()
        defer { controller.window?.close() }
        for _ in 0..<100 {
            if model.rows.count == 2 { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        let scroll = try XCTUnwrap(descendants(controller.window!.contentView!, of: NSScrollView.self).first)
        let table = try XCTUnwrap(descendants(controller.window!.contentView!, of: NSTableView.self).first)
        table.selectRowIndexes(IndexSet(integer: 1), byExtendingSelection: false)
        scroll.contentView.scroll(to: NSPoint(x: 0, y: 0))
        scroll.reflectScrolledClipView(scroll.contentView)
        let refreshedRows = (0..<80).map { index in
            let id = index == 0 ? "c" : index == 1 ? "b" : "refreshed-(index)"
            let host = index == 0 ? "aardvark.example" : index == 1 ? "beta.example" : "refreshed-(index).example"
            return "{\"id\":\"\(id)\",\"metadata\":{\"host\":\"\(host)\",\"destinationPort\":\"443\"}}"
        }.joined(separator: ",")
        snapshot = try JSONDecoder().decode(ConnectionList.self, from: Data("{\"connections\":[\(refreshedRows)]}".utf8)).connections
        for _ in 0..<100 where model.isRefreshing {
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        model.refresh()
        for _ in 0..<100 {
            if model.rows.first?.id == "c" { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        try await Task.sleep(nanoseconds: 50_000_000)
        let expectedTop = -(table.headerView?.frame.height ?? 0)
        XCTAssertEqual(scroll.contentView.bounds.origin.y, expectedTop, accuracy: 0.5)
    }

    @MainActor func testRestoringDuringMinimizeGracePeriodDoesNotHideRestoredWindow() async throws {
        _ = NSApplication.shared
        let model = ConnectionsModel(fetch: { [] }, execute: { _ in })
        model.setEngineState(.stopped)
        let controller = ConnectionsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        controller.present()
        defer { window.close() }
        window.miniaturize(nil)
        controller.present()
        try await Task.sleep(nanoseconds: 700_000_000)
        XCTAssertTrue(window.isVisible)
        XCTAssertFalse((window as? ConnectionsWindow)?.readingPaused == true)
    }

    @MainActor private func eventually(_ stage: String, window: NSWindow, probe: WindowReadProbe,
        file: StaticString = #filePath, line: UInt = #line, _ condition: () async -> Bool) async throws {
        for _ in 0..<200 {
            if await condition() {
                print("Connections window \(stage): visible=\(window.isVisible), miniaturized=\(window.isMiniaturized), appHidden=\(NSApp.isHidden), readingPaused=\((window as? ConnectionsWindow)?.readingPaused == true), starts=\(await probe.starts), cancellations=\(await probe.cancellations)")
                return
            }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        let message = "\(stage): visible=\(window.isVisible), miniaturized=\(window.isMiniaturized), appHidden=\(NSApp.isHidden), readingPaused=\((window as? ConnectionsWindow)?.readingPaused == true), starts=\(await probe.starts), cancellations=\(await probe.cancellations)"
        XCTFail("Window lifecycle did not update reading within four seconds: \(message)", file: file, line: line)
        throw WindowLifecycleTimeout(stage: stage)
    }

    @MainActor func testNativeWindowLifecycleCancelsAndImmediatelyResumesReading() async throws {
        _ = NSApplication.shared
        let probe = WindowReadProbe()
        // A long cadence means each restored read must be immediate, rather
        // than a timer tick. The slow read also exposes actual cancellation.
        let model = ConnectionsModel(fetch: { try await probe.read() }, execute: { _ in }, interval: 60_000_000_000)
        model.setEngineState(.running)
        let controller = ConnectionsWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        controller.present()
        defer { window.close() }
        try await eventually("open starts read", window: window, probe: probe) { await probe.starts == 1 }

        window.miniaturize(nil)
        try await eventually("minimize cancels read", window: window, probe: probe) { await probe.cancellations == 1 }
        try await eventually("minimize succeeds or hides instead of freezing a visible window", window: window, probe: probe) {
            window.isMiniaturized || !window.isVisible
        }
        let afterMinimize = await probe.starts
        XCTAssertEqual(afterMinimize, 1)
        if !window.isMiniaturized {
            XCTAssertFalse(window.isVisible, "A declined native minimization must hide the paused window.")
        }
        controller.present()
        try await eventually("present after minimize resumes read", window: window, probe: probe) { await probe.starts == 2 }

        window.orderOut(nil)
        try await eventually("orderOut cancels read", window: window, probe: probe) { await probe.cancellations == 2 }
        try await Task.sleep(nanoseconds: 200_000_000)
        let afterHide = await probe.starts
        XCTAssertEqual(afterHide, 2)
        controller.present()
        try await eventually("present after orderOut resumes read", window: window, probe: probe) { await probe.starts == 3 }

        window.close()
        try await eventually("close cancels read", window: window, probe: probe) { await probe.cancellations == 3 }
        try await Task.sleep(nanoseconds: 200_000_000)
        let afterClose = await probe.starts
        XCTAssertEqual(afterClose, 3)
        controller.present()
        try await eventually("present after close resumes read", window: window, probe: probe) { await probe.starts == 4 }
        window.close()
        try await eventually("final close cancels read", window: window, probe: probe) { await probe.cancellations == 4 }
    }

}


private actor WindowReadProbe {
    private(set) var starts = 0
    private(set) var cancellations = 0

    func read() async throws -> [Connection] {
        starts += 1
        do {
            try await Task.sleep(nanoseconds: 60_000_000_000)
            return []
        } catch {
            cancellations += 1
            throw error
        }
    }
}

private struct WindowLifecycleTimeout: Error {
    let stage: String
}
