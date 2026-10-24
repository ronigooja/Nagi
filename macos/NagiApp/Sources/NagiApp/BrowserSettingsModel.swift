import Combine
import Foundation

/// Presentation state only: the CLI owns Chrome preferences and restore journals.
struct BrowserWebRTCState {
    let available: Bool
    let currentValue: String?
    let restricted: Bool
    let managed: Bool
    let forced: Bool
    let canEnable: Bool
    let canDisable: Bool
    let canRestore: Bool
    let requiresRestart: Bool

    init(_ value: JSONValue) throws {
        guard let available = value["available"]?.bool,
              let restricted = value["restricted"]?.bool,
              let managed = value["managed"]?.bool,
              let forced = value["forced"]?.bool,
              let canEnable = value["can_enable"]?.bool,
              let canRestore = value["can_restore"]?.bool,
              let canDisable = value["can_disable"]?.bool else {
            throw CLIError.invalidJSON(L10n.text("app.browser.invalid_status"))
        }
        self.available = available; currentValue = value["current_value"]?.string
        self.restricted = restricted; self.managed = managed; self.forced = forced
        self.canEnable = canEnable; self.canRestore = canRestore; self.canDisable = canDisable
        requiresRestart = value["requires_restart"]?.bool ?? false
    }
}

@MainActor final class BrowserSettingsModel: ObservableObject {
    @Published private(set) var state: BrowserWebRTCState?
    @Published private(set) var isReading = false
    @Published private(set) var isSaving = false
    @Published private(set) var error: String?
    @Published private(set) var stale = true
    private let run: ([String]) async throws -> JSONValue

    init(cli: NagiCLIClient = NagiCLIClient(), run: (([String]) async throws -> JSONValue)? = nil) {
        self.run = run ?? { try await cli.run($0) }
    }

    var busy: Bool { isReading || isSaving }
    var canEnable: Bool { !busy && !stale && state?.available == true && state?.forced == false && state?.canEnable == true }
    var canDisable: Bool { !busy && !stale && state?.available == true && state?.forced == false && state?.canDisable == true }
    var canToggle: Bool { state?.restricted == true ? canDisable : canEnable }

    func refresh() async {
        guard !busy else { return }
        isReading = true; error = nil
        defer { isReading = false }
        do {
            state = try BrowserWebRTCState(await run(["browser", "chrome-webrtc", "status"]))
            stale = false
        } catch {
            stale = true
            self.error = L10n.format("app.browser.load_failed", error.localizedDescription)
        }
    }

    func enable() async { await apply("enable", allowed: canEnable) }
    func disable() async { await apply("disable", allowed: canDisable) }
    func setRestricted(_ restricted: Bool) async {
        if restricted { await enable() } else { await disable() }
    }

    private func apply(_ action: String, allowed: Bool) async {
        guard allowed else { return }
        isSaving = true; error = nil
        defer { isSaving = false }
        do {
            state = try BrowserWebRTCState(await run(["browser", "chrome-webrtc", action]))
            stale = false
        } catch {
            let failure = L10n.format("app.browser.save_failed", error.localizedDescription)
            // A failed command may have changed the preference. Reconcile the
            // actual value without repeating the write or hiding its error.
            stale = true
            do {
                state = try BrowserWebRTCState(await run(["browser", "chrome-webrtc", "status"]))
                stale = false
            } catch { }
            self.error = failure
        }
    }
}
