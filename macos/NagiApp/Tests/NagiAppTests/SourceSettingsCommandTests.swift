import XCTest
@testable import NagiApp

@MainActor final class SourceSettingsCommandTests: XCTestCase {
    @MainActor private final class Commands {
        var calls: [[String]] = []
        var failures: Set<[String]> = []
        var blocked: [String]?
        var continuation: CheckedContinuation<Void, Never>?

        func run(_ arguments: [String]) async throws -> JSONValue {
            calls.append(arguments)
            if arguments == blocked {
                blocked = nil
                await withCheckedContinuation { continuation = $0 }
            }
            if failures.contains(arguments) { throw CLIError.command("fixture", "Read failed") }
            let json = arguments == ["status"] ? "{\"running\":false}" : "{}"
            return try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        }

        func model() -> AppModel {
            AppModel(runCommand: { try await self.run($0) })
        }
    }

    func testUpdateOffAndProviderOriginalUseSeparateCommands() async {
        let commands = Commands(), model = commands.model()
        let off = ["proxy", "provider", "interval", "set", "work", "off"]
        let reset = ["proxy", "provider", "interval", "reset", "work"]
        let offError = await model.saveSourceSetting(off)
        let resetError = await model.saveSourceSetting(reset)
        XCTAssertNil(offError)
        XCTAssertNil(resetError)
        XCTAssertEqual(commands.calls.filter { $0 == off || $0 == reset }, [off, reset])
        XCTAssertFalse(commands.calls.contains(["start"]))
    }

    func testUnappliedSubscriptionPolicyDoesNotDownloadOrApply() async {
        let commands = Commands(), model = commands.model()
        let interval = ["proxy", "healthcheck", "set", "subscription", "work", "600"]
        let url = ["proxy", "healthcheck", "url", "set", "subscription", "work", "https://example.invalid/check"]
        let intervalError = await model.saveHealthcheckSettings(kind: "subscription", name: "work", seconds: 600)
        let urlError = await model.saveSourceSetting(url)
        XCTAssertNil(intervalError)
        XCTAssertNil(urlError)
        XCTAssertTrue(commands.calls.contains(interval))
        XCTAssertTrue(commands.calls.contains(url))
        XCTAssertFalse(commands.calls.contains { $0.starts(with: ["subscription", "update"]) || $0.starts(with: ["subscription", "apply"]) || $0 == ["start"] })
    }

    func testInvalidLatencyIntervalDispatchesNoCommand() async {
        let commands = Commands(), model = commands.model()
        for seconds in [Int64(0), -1, 31_536_001] {
            let failure = await model.saveHealthcheckSettings(kind: "provider", name: "work", seconds: seconds)
            XCTAssertNotNil(failure)
        }
        XCTAssertTrue(commands.calls.isEmpty)
        XCTAssertFalse(model.busy)
        XCTAssertFalse(model.applyingSetting)
    }

    func testConcurrentSubmissionDispatchesMutationOnlyOnce() async {
        let commands = Commands(), model = commands.model()
        let command = ["proxy", "healthcheck", "url", "reset", "subscription", "work"]
        commands.blocked = command
        let first = Task { await model.saveSourceSetting(command) }
        for _ in 0..<200 {
            if commands.continuation != nil { break }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertNotNil(commands.continuation)
        XCTAssertTrue(model.busy)
        let second = await model.saveSourceSetting(command)
        XCTAssertNotNil(second)
        XCTAssertEqual(commands.calls.filter { $0 == command }.count, 1)
        commands.continuation?.resume()
        commands.continuation = nil
        let firstError = await first.value
        XCTAssertNil(firstError)
        XCTAssertFalse(model.busy)
        XCTAssertFalse(model.applyingSetting)
    }

    func testSavedMutationWithFailedRefreshRetriesReadWithoutRepeatingWrite() async {
        let commands = Commands(), model = commands.model()
        let write = ["proxy", "healthcheck", "url", "set", "subscription", "work", "https://example.invalid/check"]
        let read = ["proxy", "healthcheck", "show", "subscription", "work", "--full-url"]
        var session = SourceSettingsSession()
        session.loadURL(inherited: true, canReset: true)
        session.urlInherited = false
        session.urlText = "https://example.invalid/check"
        XCTAssertTrue(session.begin(.url))
        let failure = await model.saveSourceSetting(write)
        XCTAssertNil(failure)
        session.saveSucceeded(.url)
        commands.failures.insert(read)
        do {
            _ = try await model.healthcheckSettings(kind: "subscription", name: "work")
            XCTFail("Expected saved-value refresh to fail")
        } catch {
            session.failed(.url)
        }
        XCTAssertTrue(session.pendingRefresh.contains(.url))
        XCTAssertFalse(session.canSaveURL)
        XCTAssertFalse(session.urlChanged)
        commands.failures.remove(read)
        XCTAssertTrue(session.begin(.url))
        do {
            _ = try await model.healthcheckSettings(kind: "subscription", name: "work")
            session.refreshSucceeded(.url)
        } catch {
            XCTFail("Read retry failed: \(error)")
        }
        XCTAssertEqual(commands.calls.filter { $0 == write }.count, 1)
        XCTAssertEqual(commands.calls.filter { $0 == read }.count, 2)
        XCTAssertTrue(session.pendingRefresh.isEmpty)
        XCTAssertFalse(session.dirty)
    }

    func testFailedReadRetriesOnlyReadAndReleasesOperation() async {
        let commands = Commands(), model = commands.model()
        let read = ["proxy", "healthcheck", "show", "subscription", "work", "--full-url"]
        commands.failures.insert(read)
        do {
            _ = try await model.healthcheckSettings(kind: "subscription", name: "work")
            XCTFail("Expected settings read to fail")
        } catch { }
        XCTAssertFalse(model.busy)
        XCTAssertFalse(model.applyingSetting)
        commands.failures.remove(read)
        do {
            _ = try await model.healthcheckSettings(kind: "subscription", name: "work")
        } catch {
            XCTFail("Read retry failed: \(error)")
        }
        XCTAssertEqual(commands.calls, [read, read])
        XCTAssertFalse(model.busy)
    }
}
