import XCTest
import AppKit
@testable import NagiApp

@MainActor final class SourceSettingsTests: XCTestCase {
    func testFormValidationSupportsOffAndRejectsUnsafeTestURLs() {
        for value in ["off", "1", "31536000"] {
            XCTAssertTrue(MenuAppDelegate.validUpdateInterval(value))
        }
        for value in ["", "0", "-1", "31536001", "1.5"] {
            XCTAssertFalse(MenuAppDelegate.validUpdateInterval(value))
        }
        XCTAssertTrue(MenuAppDelegate.validTestURL("https://example.invalid/path?token=value"))
        for value in ["", "ftp://example.invalid", "https://user:secret@example.invalid", "https://example.invalid/#fragment"] {
            XCTAssertFalse(MenuAppDelegate.validTestURL(value))
        }
    }

    func testProviderCreationPassesOptionalTestURLWithoutStartingEngine() async throws {
        var calls: [[String]] = []
        let model = AppModel(runCommand: { arguments in
            calls.append(arguments)
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        let error = await model.addProvider("work", url: "https://feed.example.invalid/source", group: "Work Group", intervalSeconds: 7200, testURL: "https://check.example.invalid/path")
        XCTAssertNil(error)
        XCTAssertEqual(calls.first, ["proxy", "provider", "add", "work", "https://feed.example.invalid/source", "Work Group", "7200", "--test-url", "https://check.example.invalid/path"])
        XCTAssertFalse(calls.contains(["start"]))
        XCTAssertFalse(model.busy)
    }

    func testProviderCreationCanDisableAutomaticDownloads() async throws {
        var calls: [[String]] = []
        let model = AppModel(runCommand: { arguments in
            calls.append(arguments)
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        let error = await model.addProvider("work", url: "https://feed.example.invalid/source", group: "Work Group", updateOff: true)
        XCTAssertNil(error)
        XCTAssertEqual(calls.first, ["proxy", "provider", "add", "work", "https://feed.example.invalid/source", "Work Group", "off"])
    }

    func testSubscriptionCreationSavesOptionsBeforeDownloadAndApply() async throws {
        var calls: [[String]] = []
        let model = AppModel(runCommand: { arguments in
            calls.append(arguments)
            return try JSONDecoder().decode(JSONValue.self, from: Data("{}".utf8))
        })
        model.addAndApplySubscription("work", url: "https://feed.example.invalid/source", schedule: "off", testURL: "https://check.example.invalid/path")
        for _ in 0..<200 {
            if !model.busy { break }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        XCTAssertEqual(Array(calls.prefix(3)), [
            ["subscription", "add", "work", "https://feed.example.invalid/source", "--schedule", "off", "--test-url", "https://check.example.invalid/path"],
            ["subscription", "update", "work"], ["subscription", "apply", "work"]
        ])
        XCTAssertFalse(calls.contains(["start"]))
        XCTAssertFalse(model.busy)
    }

    func testSourceMutationDoesNotRetryFailureAndReleasesOperation() async throws {
        var calls: [[String]] = []
        let model = AppModel(runCommand: { arguments in
            calls.append(arguments)
            throw CLIError.command("activation", "Activation failed")
        })
        let command = ["proxy", "healthcheck", "url", "reset", "subscription", "work"]
        let failure = await model.saveSourceSetting(command)
        XCTAssertNotNil(failure)
        XCTAssertEqual(calls.filter { $0 == command }.count, 1)
        XCTAssertFalse(model.busy)
        XCTAssertFalse(model.applyingSetting)
        XCTAssertFalse(calls.contains(["proxy", "healthcheck", "reset", "subscription", "work"]))
    }
}
