import XCTest
import AppKit
@testable import NagiApp

final class ConnectionsWindowTests: XCTestCase {
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
        let details = try XCTUnwrap(descendants(content, of: NSTextView.self).first)
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
        let details = try XCTUnwrap(descendants(content, of: NSTextView.self).first)
        XCTAssertTrue(details.string.contains("ID: b"))
        XCTAssertTrue(details.string.contains("/Applications/Browser.app/Contents/MacOS/Browser"))
    }
}
