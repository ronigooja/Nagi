import Combine
import Foundation

/// Window state only. AppModel owns the existing display/login operations and
/// serializes CLI mutations with the rest of the App.
@MainActor final class SettingsModel: ObservableObject {
    let browser: BrowserSettingsModel
    @Published private(set) var display: TrafficDisplay = .both
    @Published private(set) var appLogin = false
    @Published private(set) var engineLogin: Bool?
    @Published private(set) var busy = false
    @Published private(set) var loginError: String?
    private let readDisplay: () -> TrafficDisplay
    private let writeDisplay: (TrafficDisplay) -> Void
    private let readAppLogin: () -> Bool
    private let writeAppLogin: (Bool) async throws -> Void
    private let readEngineLogin: () async throws -> Bool
    private let writeEngineLogin: (Bool) async throws -> Void

    init(browser: BrowserSettingsModel, readDisplay: @escaping () -> TrafficDisplay,
         writeDisplay: @escaping (TrafficDisplay) -> Void, readAppLogin: @escaping () -> Bool,
         writeAppLogin: @escaping (Bool) async throws -> Void,
         readEngineLogin: @escaping () async throws -> Bool,
         writeEngineLogin: @escaping (Bool) async throws -> Void) {
        self.browser = browser; self.readDisplay = readDisplay; self.writeDisplay = writeDisplay
        self.readAppLogin = readAppLogin; self.writeAppLogin = writeAppLogin
        self.readEngineLogin = readEngineLogin; self.writeEngineLogin = writeEngineLogin
    }

    func refresh() async {
        guard !busy && !browser.busy else { return }
        busy = true; defer { busy = false }
        display = readDisplay(); appLogin = readAppLogin()
        await browser.refresh()
        do { engineLogin = try await readEngineLogin(); loginError = nil }
        catch {
            engineLogin = nil
            loginError = L10n.format("app.settings.login_failed", error.localizedDescription)
        }
    }

    func setDisplay(_ value: TrafficDisplay) {
        guard !busy && !browser.busy else { return }
        writeDisplay(value); display = readDisplay()
    }

    func setAppLogin(_ enabled: Bool) async {
        guard !busy && !browser.busy, enabled != appLogin else { return }
        busy = true; loginError = nil
        defer { appLogin = readAppLogin(); busy = false }
        do { try await writeAppLogin(enabled) }
        catch { loginError = L10n.format("app.settings.login_failed", error.localizedDescription) }
    }

    func setEngineLogin(_ enabled: Bool) async {
        guard !busy && !browser.busy, engineLogin != nil, enabled != engineLogin else { return }
        busy = true; loginError = nil
        defer { busy = false }
        do {
            try await writeEngineLogin(enabled)
            engineLogin = try await readEngineLogin()
        } catch {
            let failure = L10n.format("app.settings.login_failed", error.localizedDescription)
            engineLogin = try? await readEngineLogin()
            loginError = failure
        }
    }
}
