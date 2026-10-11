import XCTest
import AppKit
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
        defer { try? FileManager.default.removeItem(at: duplicate) }
        let duplicateWatch = try SubscriptionWatch(executable: duplicate)
        if case .duplicate = await duplicateWatch.wait() {} else { XCTFail("duplicate host was not classified") }
        let long = try fixture("exec python3 -c 'import signal,time; print(\"{\\\"ok\\\":true,\\\"data\\\":{\\\"scheduler_running\\\":true}}\",flush=True); signal.pause()'")
        defer { try? FileManager.default.removeItem(at: long) }
        let owned = try SubscriptionWatch(executable: long)
        try await Task.sleep(nanoseconds: 100_000_000)
        owned.stop()
        if case .stopped = await owned.wait() {} else { XCTFail("user stop was not classified") }
    }

    func testAbnormalAndOversizedExitFail() async throws {
        let abnormal = try fixture("printf '{\"ok\":true,\"data\":{\"scheduler_running\":true}}\\n'; exit 7")
        defer { try? FileManager.default.removeItem(at: abnormal) }
        let failed = try SubscriptionWatch(executable: abnormal)
        if case .failed = await failed.wait() {} else { XCTFail("abnormal exit was not reported") }
        let oversized = try fixture("python3 -c 'print(\"x\"*70000)'")
        defer { try? FileManager.default.removeItem(at: oversized) }
        let bounded = try SubscriptionWatch(executable: oversized)
        if case .failed = await bounded.wait() {} else { XCTFail("oversized frame was not bounded") }
    }

    func testManyFramesAndFragmentedDuplicate() async throws {
        let many = try fixture("python3 -c 'import json; print(json.dumps({\"ok\":True,\"data\":{\"scheduler_running\":True}})); [print(json.dumps({\"ok\":True,\"data\":{\"event\":\"x\"*900}})) for _ in range(90)]'")
        defer { try? FileManager.default.removeItem(at: many) }
        let watch = try SubscriptionWatch(executable: many)
        if case .failed(let error) = await watch.wait() {
            XCTAssertTrue(error.localizedDescription.contains("unexpectedly"), "\(error)")
        } else { XCTFail("unexpected watch result") }
        let split = try fixture("printf '{\"ok\":false,\"error\":{\"code\":\"scheduler_' >&2; sleep 0.05; printf 'already_running\",\"message\":\"busy\"}}\\n' >&2; exit 1")
        defer { try? FileManager.default.removeItem(at: split) }
        let duplicate = try SubscriptionWatch(executable: split)
        if case .duplicate = await duplicate.wait() {} else { XCTFail("fragmented duplicate lost") }
    }

    func testOversizedFrameTerminatesPromptly() async throws {
        let script = try fixture("exec python3 -u -c 'import sys,signal,time; signal.signal(signal.SIGTERM, lambda *_: sys.exit(0)); print(\"x\"*70000,flush=True); time.sleep(30)'")
        defer { try? FileManager.default.removeItem(at: script) }
        let watch = try SubscriptionWatch(executable: script)
        let start = Date()
        if case .failed(let error) = await watch.wait() {
            XCTAssertTrue(error.localizedDescription.contains("64 KiB"))
        } else { XCTFail("large frame was accepted") }
        XCTAssertLessThan(Date().timeIntervalSince(start), 4)
    }
}

@MainActor extension LifecycleTests {
    private func waitUntil(_ condition: @escaping () -> Bool) async -> Bool {
        for _ in 0..<100 {
            if condition() { return true }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        return condition()
    }

    private func expectUntil(_ condition: @escaping () -> Bool, file: StaticString = #filePath, line: UInt = #line) async {
        let success = await waitUntil(condition)
        XCTAssertTrue(success, file: file, line: line)
    }

    private func modelFixture() throws -> (URL, URL, URL) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-model-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let script = directory.appendingPathComponent("nagi")
        let log = directory.appendingPathComponent("events")
        let mode = directory.appendingPathComponent("mode")
        let body = """
        #!/bin/sh
        printf '%s\\n' "$*" >> '\(log.path)'
        if [ "$*" = '--json subscription watch' ]; then
          case "$(cat '\(mode.path)' 2>/dev/null)" in
            duplicate) printf '{"ok":false,"error":{"code":"scheduler_already_running","message":"busy"}}\\n' >&2; exit 1 ;;
            fail) printf '{"ok":true,"data":{"scheduler_running":true}}\\n'; exit 7 ;;
          esac
          exec python3 -u -c 'import signal,sys,time; p=sys.argv[1]; signal.signal(signal.SIGTERM,lambda *_: (open(p,"a").write("watch-stopped\\n"),sys.exit(0))); open(p,"a").write("watch-owned\\n"); print("{\\"ok\\":true,\\"data\\":{\\"scheduler_running\\":true}}",flush=True); signal.pause()' '\(log.path)'
        fi
        if [ "$*" = '--json quit' ] && [ -f '\(directory.appendingPathComponent("quitfail").path)' ]; then
          printf '{"ok":false,"error":{"code":"quit_failed","message":"cleanup failed"}}\\n' >&2; exit 1
        fi
        case "$*" in
          '--json status') printf '{"ok":true,"data":{"running":false}}\\n' ;;
          '--json profile list') printf '{"ok":true,"data":{"profiles":[]}}\\n' ;;
          '--json subscription list') printf '{"ok":true,"data":{"subscriptions":[],"scheduler_running":false}}\\n' ;;
          '--json service status') printf '{"ok":true,"data":{"installed":false}}\\n' ;;
          *) printf '{"ok":true,"data":{}}\\n' ;;
        esac
        """
        try body.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        return (directory, script, log)
    }

    private func lines(_ url: URL) -> [String] {
        ((try? String(contentsOf: url)) ?? "").split(separator: "\n").map(String.init)
    }

    func testHostRetrySleepWakeAndQuitOwnership() async throws {
        let (dir, cli, log) = try modelFixture()
        defer { try? FileManager.default.removeItem(at: dir) }
        let model = AppModel(cli: NagiCLIClient(executable: cli), watchDelay: { _, _ in 40_000_000 })
        try "duplicate".write(to: dir.appendingPathComponent("mode"), atomically: true, encoding: .utf8)
        let external = Process()
        external.executableURL = URL(fileURLWithPath: "/bin/sleep")
        external.arguments = ["30"]
        try external.run()
        defer { if external.isRunning { external.terminate() }; external.waitUntilExit() }
        model.launch()
        await expectUntil { self.lines(log).filter { $0 == "--json subscription watch" }.count >= 2 }
        XCTAssertTrue(external.isRunning)
        XCTAssertFalse(lines(log).contains("watch-stopped"))

        try "fail".write(to: dir.appendingPathComponent("mode"), atomically: true, encoding: .utf8)
        await expectUntil { model.errorMessage?.contains("scheduler unavailable") == true || model.errorMessage?.contains("scheduler") == true }
        await expectUntil { self.lines(log).filter { $0 == "--json subscription watch" }.count >= 3 }
        try "normal".write(to: dir.appendingPathComponent("mode"), atomically: true, encoding: .utf8)
        await expectUntil { self.lines(log).contains("watch-owned") }
        model.pauseForSleep()
        await expectUntil { self.lines(log).contains("watch-stopped") }
        let before = lines(log).filter { $0 == "--json subscription watch" }.count
        try? await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertEqual(lines(log).filter { $0 == "--json subscription watch" }.count, before)
        model.resumeAfterWake()
        await expectUntil { self.lines(log).filter { $0 == "--json subscription watch" }.count > before }
        let succeeded = await withCheckedContinuation { continuation in model.quit { continuation.resume(returning: $0) } }
        XCTAssertTrue(succeeded)
        await expectUntil { self.lines(log).filter { $0 == "watch-stopped" }.count >= 2 }
    }

    func testFailedQuitKeepsOwnedWatchUntilSuccessfulRetry() async throws {
        let (dir, cli, log) = try modelFixture()
        defer { try? FileManager.default.removeItem(at: dir) }
        let model = AppModel(cli: NagiCLIClient(executable: cli), watchDelay: { _, _ in 40_000_000 })
        model.launch()
        await expectUntil { self.lines(log).contains("--json subscription watch") }
        try "fail".write(to: dir.appendingPathComponent("quitfail"), atomically: true, encoding: .utf8)
        let failed = await withCheckedContinuation { continuation in model.quit { continuation.resume(returning: $0) } }
        XCTAssertFalse(failed)
        XCTAssertTrue(model.quitErrorMessage?.contains("cleanup failed") == true)
        XCTAssertFalse(lines(log).contains("watch-stopped"))
        try FileManager.default.removeItem(at: dir.appendingPathComponent("quitfail"))
        let succeeded = await withCheckedContinuation { continuation in model.quit { continuation.resume(returning: $0) } }
        XCTAssertTrue(succeeded)
        await expectUntil { self.lines(log).contains("watch-stopped") }
    }

    func testProxyAndProviderCommandsUseInjectedCLI() async throws {
        let (dir, cli, log) = try modelFixture()
        defer { try? FileManager.default.removeItem(at: dir) }
        let model = AppModel(cli: NagiCLIClient(executable: cli))
        for kind in ["Selector", "URLTest", "Fallback"] {
            let group = ProxyGroup(name: "Group \(kind)", kind: kind, selected: "Node A", fixed: nil,
                                   selectedStatus: nil, nodes: ["Node A"], hidden: false, icon: nil)
            guard let command = group.selectCommand("Node A") else { return XCTFail("missing select command") }
            model.perform(command)
            await expectUntil { !model.busy }
            XCTAssertTrue(lines(log).contains("--json proxy select Group \(kind) Node A"))
            if kind != "Selector" {
                guard let restore = group.restoreAutomaticCommand else { return XCTFail("missing restore command") }
                model.perform(restore)
                await expectUntil { !model.busy }
                XCTAssertTrue(lines(log).contains("--json proxy auto Group \(kind)"))
            } else { XCTAssertNil(group.restoreAutomaticCommand) }
        }
        let hidden = ProxyGroup(name: "hidden", kind: "Selector", selected: "", fixed: nil,
                                selectedStatus: nil, nodes: ["Node A"], hidden: true, icon: nil)
        XCTAssertNil(hidden.selectCommand("Node A"))
        let provider = ProxyProvider(name: "work", type: "HTTP", vehicleType: "HTTP", nodes: [])
        for command in [provider.updateCommand, provider.healthcheckCommand] {
            model.perform(command)
            await expectUntil { !model.busy }
            XCTAssertTrue(lines(log).contains("--json \(command.joined(separator: " "))"))
        }
    }

    func testUnexpectedExitRetriesWithBoundedBackoffAndSleepCancelsRetry() async throws {
        let (dir, cli, log) = try modelFixture()
        defer { try? FileManager.default.removeItem(at: dir) }
        try "fail".write(to: dir.appendingPathComponent("mode"), atomically: true, encoding: .utf8)
        var attempts: [Int] = []
        let model = AppModel(cli: NagiCLIClient(executable: cli), watchDelay: { attempt, duplicate in
            if !duplicate { attempts.append(attempt) }
            return 30_000_000
        })
        model.launch()
        await expectUntil { self.lines(log).filter { $0 == "--json subscription watch" }.count >= 7 }
        XCTAssertTrue(attempts.contains(5))
        XCTAssertTrue(attempts.allSatisfy { (1...5).contains($0) })
        XCTAssertTrue(model.errorMessage?.contains("scheduler") == true)
        model.pauseForSleep()
        let count = lines(log).filter { $0 == "--json subscription watch" }.count
        try? await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertEqual(lines(log).filter { $0 == "--json subscription watch" }.count, count)
    }

    func testIconURLAcceptance() {
        XCTAssertNotNil(GroupIconLoader.allowedURL("https://example.org/icon.png"))
        XCTAssertNotNil(GroupIconLoader.allowedURL("http://example.org/icon.png"))
        for value in ["file:///tmp/private", "/tmp/private", "data:image/png;base64,AA", "https://user:password@example.org/icon", "ftp://example.org/icon"] {
            XCTAssertNil(GroupIconLoader.allowedURL(value), value)
        }
        let start = URL(string: "http://example.org/icon")!
        XCTAssertTrue(GroupIconLoader.allowedRedirect(from: start, to: URL(string: "http://other.example/icon")!))
        XCTAssertFalse(GroupIconLoader.allowedRedirect(from: start, to: URL(string: "https://example.org/icon")!))
        XCTAssertFalse(GroupIconLoader.allowedRedirect(from: start, to: URL(string: "http://user@example.org/icon")!))
    }

    func testIconFetchUsesImageSizeRedirectAndTimeLimits() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-icon-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 1, pixelsHigh: 1,
                                      bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                      colorSpaceName: .deviceRGB, bytesPerRow: 4, bitsPerPixel: 32)!
        bitmap.setColor(.red, atX: 0, y: 0)
        let png = dir.appendingPathComponent("icon.png")
        try bitmap.representation(using: .png, properties: [:])!.write(to: png)
        let serverScript = dir.appendingPathComponent("server.py")
        try """
        import http.server,sys,time
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == '/slow':
                    time.sleep(10)
                    return
                if self.path == '/redirect':
                    self.send_response(302)
                    self.send_header('Location','https://example.invalid/icon.png')
                    self.end_headers()
                    return
                if self.path == '/large':
                    self.send_response(200)
                    self.send_header('Content-Length','300000')
                    self.end_headers()
                    return
                body = open(sys.argv[1],'rb').read()
                self.send_response(200)
                self.send_header('Content-Length',str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            def log_message(self,*args): pass
        server = http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
        print(server.server_port,flush=True)
        server.serve_forever()
        """.write(to: serverScript, atomically: true, encoding: .utf8)
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/python3")
        process.arguments = [serverScript.path, png.path]
        let output = Pipe()
        process.standardOutput = output
        process.standardError = Pipe()
        try process.run()
        output.fileHandleForWriting.closeFile()
        defer { if process.isRunning { process.terminate() }; process.waitUntilExit() }
        guard let port = Int(String(data: output.fileHandleForReading.availableData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "") else {
            return XCTFail("icon server did not start")
        }
        let loader = GroupIconLoader()
        func load(_ path: String) async -> NSImage? {
            await withCheckedContinuation { continuation in
                _ = loader.image(for: "http://127.0.0.1:\(port)/\(path)") { continuation.resume(returning: $0) }
            }
        }
        let valid = await load("icon")
        XCTAssertNotNil(valid)
        let oversized = await load("large")
        XCTAssertNil(oversized)
        let redirected = await load("redirect")
        XCTAssertNil(redirected)
        let started = Date()
        let timedOut = await load("slow")
        XCTAssertNil(timedOut)
        XCTAssertLessThan(Date().timeIntervalSince(started), 9)
    }
}

@MainActor extension LifecycleTests {
    private func providerFixture(running: Bool = false) throws -> (URL, URL, URL) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("nagi-provider-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let script = directory.appendingPathComponent("nagi")
        let log = directory.appendingPathComponent("events.jsonl")
        try """
        #!/usr/bin/python3
        import json, pathlib, sys, time
        root = pathlib.Path(__file__).parent
        args = sys.argv[1:]
        with (root / 'events.jsonl').open('a') as log:
            log.write(json.dumps(args) + '\\n')
        command = args[1:]
        if command[:3] == ['proxy', 'provider', 'add']:
            time.sleep(0.1)
            if (root / 'fail').exists():
                print(json.dumps({'ok':False,'error':{'code':'provider_exists','message':'Provider already exists: ' + command[4]}}))
                sys.exit(1)
            data = {'provider':command[3], 'group':command[5], 'profile':'personal', 'interval_seconds':int(command[6]), 'added':True, 'group_created':True}
        elif command == ['status']:
            data = {'running':\(running ? "True" : "False"), 'profile':'personal'}
        elif command == ['profile', 'list']:
            data = {'profiles':['personal'], 'current':'personal'}
        elif command == ['proxy', 'groups']:
            data = {'groups':[{'name':'Work Group','type':'Selector','now':'DIRECT','all':['DIRECT','Node A']}]}
        elif command == ['proxy', 'providers']:
            data = {'providers':[{'name':'work','type':'Proxy','vehicle_type':'HTTP','nodes':['Node A']}]}
        elif command == ['quit']:
            time.sleep(0.1)
            data = {}
        else:
            data = {}
        print(json.dumps({'ok':True,'data':data}))
        """.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        return (directory, script, log)
    }

    private func providerCalls(_ log: URL) -> [[String]] {
        lines(log).compactMap { line in
            guard let data = line.data(using: .utf8) else { return nil }
            return try? JSONDecoder().decode([String].self, from: data)
        }
    }

    func testProviderAddWhileStoppedDoesNotStartEngine() async throws {
        let (directory, cli, log) = try providerFixture()
        defer { try? FileManager.default.removeItem(at: directory) }
        let model = AppModel(cli: NagiCLIClient(executable: cli))
        await model.refresh()
        XCTAssertEqual(model.state, .stopped)
        let error = await model.addProvider("work", url: "https://example.invalid/feed?token=private", group: "Work Group")
        XCTAssertNil(error)
        XCTAssertEqual(providerCalls(log).filter { $0.prefix(4) == ["--json", "proxy", "provider", "add"] },
                       [["--json", "proxy", "provider", "add", "work", "https://example.invalid/feed?token=private", "Work Group", "3600"]])
        XCTAssertFalse(providerCalls(log).contains(["--json", "start"]))
        XCTAssertEqual(model.state, .stopped)
        XCTAssertEqual(model.profile, "personal")
        XCTAssertFalse(model.busy)
        XCTAssertFalse(model.applyingSetting)
        XCTAssertTrue(model.notice?.contains("Routing rules are unchanged") == true)
        XCTAssertFalse(model.notice?.contains("private") == true)
    }

    func testProviderAddRefreshesGroupsAndProvidersWhenRunning() async throws {
        let (directory, cli, log) = try providerFixture(running: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let model = AppModel(cli: NagiCLIClient(executable: cli))
        model.pauseForSleep()
        await model.refresh()
        let before = providerCalls(log).count
        let error = await model.addProvider("work", url: "http://example.invalid/feed", group: "Work Group", intervalSeconds: 60)
        XCTAssertNil(error)
        let calls = Array(providerCalls(log).dropFirst(before))
        XCTAssertEqual(calls.first, ["--json", "proxy", "provider", "add", "work", "http://example.invalid/feed", "Work Group", "60"])
        XCTAssertTrue(calls.contains(["--json", "proxy", "groups"]))
        XCTAssertTrue(calls.contains(["--json", "proxy", "providers"]))
        XCTAssertEqual(model.groups.first?.name, "Work Group")
        XCTAssertEqual(model.providers.first?.name, "work")
        XCTAssertNil(model.errorMessage)
    }

    func testProviderAddFailureRedactsURLAndCanRetry() async throws {
        let (directory, cli, log) = try providerFixture()
        defer { try? FileManager.default.removeItem(at: directory) }
        let failure = directory.appendingPathComponent("fail")
        try "fail".write(to: failure, atomically: true, encoding: .utf8)
        let model = AppModel(cli: NagiCLIClient(executable: cli))
        let url = "https://example.invalid/feed?token=private"
        var observedFailureAfterAnimationStopped = false
        model.onChange = { [weak model] in
            guard let model else { return }
            if model.errorMessage != nil && !model.busy && !model.applyingSetting {
                observedFailureAfterAnimationStopped = true
            }
        }
        let error = await model.addProvider("work", url: url, group: "Work Group")
        XCTAssertTrue(observedFailureAfterAnimationStopped)
        XCTAssertTrue(error?.contains("already exists") == true)
        XCTAssertFalse(error?.contains(url) == true)
        XCTAssertFalse(model.errorMessage?.contains("private") == true)
        XCTAssertNil(model.notice)
        XCTAssertFalse(model.busy)
        XCTAssertFalse(model.applyingSetting)
        try FileManager.default.removeItem(at: failure)
        let retried = await model.addProvider("work", url: url, group: "Work Group")
        XCTAssertNil(retried)
        XCTAssertNil(model.errorMessage)
        XCTAssertEqual(providerCalls(log).filter { $0.prefix(4) == ["--json", "proxy", "provider", "add"] }.count, 2)
    }

    func testProviderAddGuardsBusyQuitAndInvalidIntervals() async throws {
        let (directory, cli, log) = try providerFixture()
        defer { try? FileManager.default.removeItem(at: directory) }
        let model = AppModel(cli: NagiCLIClient(executable: cli))
        for interval in [Int64(0), -1, 31_536_001] {
            let error = await model.addProvider("work", url: "https://example.invalid/feed", group: "Work Group", intervalSeconds: interval)
            XCTAssertNotNil(error)
        }
        XCTAssertTrue(providerCalls(log).isEmpty)
        let first = Task { await model.addProvider("work", url: "https://example.invalid/feed", group: "Work Group") }
        await expectUntil { model.busy }
        XCTAssertTrue(model.applyingSetting)
        let blocked = await model.addProvider("second", url: "https://example.invalid/other", group: "Work Group")
        XCTAssertNotNil(blocked)
        let result = await first.value
        XCTAssertNil(result)
        model.quit { _ in }
        let quitting = await model.addProvider("third", url: "https://example.invalid/other", group: "Work Group")
        XCTAssertNotNil(quitting)
        await expectUntil { self.providerCalls(log).contains(["--json", "quit"]) && !model.busy }
        XCTAssertEqual(providerCalls(log).filter { $0.prefix(4) == ["--json", "proxy", "provider", "add"] }.count, 1)
    }
}
