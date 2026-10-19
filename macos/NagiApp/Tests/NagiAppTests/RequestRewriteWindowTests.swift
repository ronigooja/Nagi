import XCTest
import AppKit
import SwiftUI
@testable import NagiApp

@MainActor final class RequestRewriteWindowTests: XCTestCase {
    func testInitialFrameAndEditorsFitSmallAndOffsetScreens() {
        for screen in [NSRect(x: 0, y: 40, width: 1280, height: 720),
                       NSRect(x: -1024, y: 200, width: 1024, height: 640),
                       NSRect(x: 0, y: 0, width: 1920, height: 1080)] {
            let frame = RequestRewriteWindowController.initialFrame(in: screen)
            XCTAssertTrue(screen.contains(frame))
            XCTAssertLessThan(frame.width, screen.width)
            XCTAssertLessThan(frame.height, screen.height)
            XCTAssertEqual(frame.midX, screen.midX, accuracy: 1)
            XCTAssertEqual(frame.midY, screen.midY, accuracy: 1)
            for preferred in [NSSize(width: 800, height: 740), NSSize(width: 780, height: 620)] {
                let size = RewriteEditorLayout.size(width: preferred.width, height: preferred.height, in: screen)
                XCTAssertLessThan(size.width, screen.width)
                XCTAssertLessThan(size.height, screen.height)
                XCTAssertLessThanOrEqual(size.width, preferred.width)
                XCTAssertLessThanOrEqual(size.height, preferred.height)
            }
        }
    }

    func testLongContentAndTabChangesDoNotGrowWindowOrPreventShrinking() async throws {
        _ = NSApplication.shared
        let longValue = String(repeating: "fictitious", count: 1000)
        var failRead = false
        let model = RequestRewriteModel(read: { args, _ in
            if failRead { throw CLIError.command("fixture", longValue) }
            if args == ["status"] {
                return .objectValue(["profile": .stringValue("work"), "actual": .stringValue("disabled"),
                    "enabled_intent": .boolValue(false), "last_error": .stringValue(longValue),
                    "ca": .objectValue(["fingerprint": .stringValue(longValue)])])
            }
            if args.first == "scripts" { return .objectValue(["scripts": .arrayValue([])]) }
            return .objectValue(["rules": .arrayValue([.objectValue([
                "id": .stringValue("example"), "enabled": .boolValue(true), "stage": .stringValue("request"),
                "match": .objectValue(["domain": .stringValue("api.service.example"), "path_prefix": .stringValue("/" + longValue)])
            ])])])
        }, submit: { _, _ in .objectValue(["result": .stringValue(longValue)]) }, export: { _ in Data() })
        let controller = RequestRewriteWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        let screen = try XCTUnwrap(window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame)
        XCTAssertTrue(screen.contains(window.frame))
        controller.present()
        // AppKit may reposition a newly presented window to avoid other windows.
        // Content and tab changes must preserve the resulting user-visible frame.
        let initial = window.frame
        XCTAssertTrue(screen.contains(initial))
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(model.listsFresh)
        XCTAssertEqual(model.rules.first?.path, "/" + longValue)
        let previewed = await model.perform(["preview", "-"])
        XCTAssertTrue(previewed)
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        window.contentView?.layoutSubtreeIfNeeded()
        XCTAssertEqual(window.frame, initial)
        let host = try XCTUnwrap(window.contentView as? NSHostingView<RequestRewriteView>)
        XCTAssertTrue(host.sizingOptions.isEmpty)
        func findTabs(_ view: NSView) -> NSTabView? {
            if let tabs = view as? NSTabView { return tabs }
            return view.subviews.lazy.compactMap { findTabs($0) }.first
        }
        let tabs = try XCTUnwrap(findTabs(host))
        XCTAssertEqual(tabs.numberOfTabViewItems, 5)
        for index in 0..<tabs.numberOfTabViewItems {
            tabs.selectTabViewItem(at: index)
            for size in [window.minSize, initial.size, window.minSize] {
                window.setFrame(NSRect(origin: initial.origin, size: size), display: true)
                for _ in 0..<5 {
                    host.layoutSubtreeIfNeeded()
                    try await Task.sleep(nanoseconds: 20_000_000)
                }
                XCTAssertEqual(window.frame.width, size.width, accuracy: 1)
                XCTAssertEqual(window.frame.height, size.height, accuracy: 1)
            }
        }
        failRead = true
        model.refresh()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertNotNil(model.error)
        for _ in 0..<5 {
            host.layoutSubtreeIfNeeded()
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTAssertEqual(window.frame.size, window.minSize)
        window.close()
        await model.waitForCancelledRead()
        controller.present()
        XCTAssertEqual(window.frame.size, window.minSize)
    }

    func testWindowIsIndependentReusableAndCloseNeverStopsService() async throws {
        _ = NSApplication.shared
        var commands: [[String]] = []
        let model = RequestRewriteModel(read: { args, _ in
            let text = args == ["status"] ? #"{"profile":"work","actual":"disabled","enabled_intent":false,"tun_running":false}"# : (args.first == "scripts" ? #"{"scripts":[]}"# : #"{"rules":[]}"#)
            return try JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))
        }, submit: { args, _ in
            commands.append(args)
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        }, export: { _ in Data() })
        let controller = RequestRewriteWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        XCTAssertEqual(window.title, "Nagi Request Rewrite")
        XCTAssertTrue(window.styleMask.contains(.resizable)); XCTAssertTrue(window.styleMask.contains(.miniaturizable))
        XCTAssertFalse(window.isReleasedWhenClosed)
        controller.present(); controller.present()
        XCTAssertTrue(controller.window === window)
        window.close(); await model.waitForCancelledRead()
        XCTAssertTrue(commands.isEmpty)
        controller.present(); XCTAssertTrue(controller.window === window)
        window.orderOut(nil); await model.waitForCancelledRead()
        XCTAssertTrue(model.stale)
        window.close()
    }
    func testRewriteMinuteTimestampOmitsSeconds() {
        let text = RequestRewriteView.time(Date(timeIntervalSince1970: 1_786_000_043))
        XCTAssertEqual(text.count, 16)
        XCTAssertEqual(text.filter { $0 == ":" }.count, 1)
    }
    func testWindowTitleTracksProfileRefreshAndRetainsLastProfileOnFailure() async throws {
        _ = NSApplication.shared
        var profile: String? = "work"
        var failRead = false
        let model = RequestRewriteModel(read: { args, _ in
            if failRead { throw CLIError.command("fixture", "read failed") }
            if args == ["status"] {
                return .objectValue(["profile": profile.map(JSONValue.stringValue) ?? .null,
                    "actual": .stringValue("disabled"), "enabled_intent": .boolValue(false)])
            }
            return .objectValue([args.first == "scripts" ? "scripts" : "rules": .arrayValue([])])
        }, submit: { _, _ in .objectValue([:]) }, export: { _ in Data() })
        let controller = RequestRewriteWindowController(model: model)
        let window = try XCTUnwrap(controller.window)
        defer { window.close() }
        // Drive the read slot directly so the test does not depend on window occlusion.
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertEqual(window.title, "Nagi Request Rewrite · work")

        profile = "personal"
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertEqual(window.title, "Nagi Request Rewrite · personal")

        failRead = true
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(model.stale)
        XCTAssertEqual(window.title, "Nagi Request Rewrite · personal")

        failRead = false; profile = nil
        model.refreshStatus()
        for _ in 0..<100 { if !model.isReading { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertEqual(window.title, "Nagi Request Rewrite")
    }
    func testQuitWaitsForRewriteSubmitAndFailedQuitRestoresGate() async throws {
        var allowSubmit: CheckedContinuation<Void, Never>?
        var submitted = false, cleanup = 0, completion: Bool?
        let app = AppModel(runCommand: { args in
            if args == ["quit"] { cleanup += 1; throw CLIError.command("fixture", "cleanup failed") }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        app.requestRewrite.submit = { _, _ in
            submitted = true
            await withCheckedContinuation { allowSubmit = $0 }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        }
        let task = Task { await app.requestRewrite.perform(["disable"]) }
        for _ in 0..<100 { if submitted { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(submitted)
        app.quit { completion = $0 }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertEqual(cleanup, 0)
        allowSubmit?.resume()
        _ = await task.value
        for _ in 0..<100 { if completion != nil { break }; try await Task.sleep(nanoseconds: 10_000_000) }
        XCTAssertEqual(cleanup, 1); XCTAssertEqual(completion, false)
        XCTAssertTrue(app.requestRewrite.canOperate)
    }
}

@MainActor extension RequestRewriteWindowTests {
    func testSettingsWaitForSubmittedRewriteBeforeDispatch() async throws {
        var release: CheckedContinuation<Void, Never>?
        var submitted = false, settingsCommands = 0
        let app = AppModel(runCommand: { args in
            if args == ["fixture-setting"] { settingsCommands += 1 }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        app.requestRewrite.submit = { _, _ in
            submitted = true
            await withCheckedContinuation { release = $0 }
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        }
        let operation = Task { await app.requestRewrite.perform(["disable"]) }
        for _ in 0..<100 { if submitted { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        let setting = Task { await app.saveSourceSetting(["fixture-setting"]) }
        try await Task.sleep(nanoseconds: 30_000_000)
        XCTAssertTrue(app.busy); XCTAssertEqual(settingsCommands, 0)
        release?.resume(); _ = await operation.value
        _ = await setting.value
        XCTAssertEqual(settingsCommands, 1)
    }
}

@MainActor extension RequestRewriteWindowTests {
    func testActualRewriteTransportHoldsAppSettingsGateUntilChildCompletes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-rewrite-gate-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        defer { try? FileManager.default.removeItem(at: directory) }
        let cli = directory.appendingPathComponent("fixture.sh")
        try #"""
        #!/bin/sh
        exec /usr/bin/python3 -c 'import time; time.sleep(0.2); print("{\"ok\":true,\"data\":{\"saved\":true}}")'
        """#.write(to: cli, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
        let app = AppModel(cli: NagiCLIClient(executable: cli, timeout: 3))
        let operation = Task { await app.requestRewrite.perform(["disable"]) }
        for _ in 0..<100 { if app.busy { break }; try await Task.sleep(nanoseconds: 5_000_000) }
        XCTAssertTrue(app.busy); XCTAssertTrue(app.requestRewrite.operationInProgress)
        do { _ = try await app.executeRewrite(["enable"], input: nil); XCTFail("Overlapping mutation") } catch { }
        let accepted = await operation.value
        XCTAssertTrue(accepted); XCTAssertFalse(app.busy)
        app.requestRewrite.setSuspended(true)
        await app.requestRewrite.waitForCancelledRead()
    }
}
