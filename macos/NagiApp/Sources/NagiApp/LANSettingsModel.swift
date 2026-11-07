import Combine
import Darwin
import Foundation

/// Runtime presentation only; the CLI owns listener changes and backend selection.
struct LANSettingsState {
    let enabled: Bool
    let bindAddress: String

    init(_ value: JSONValue) throws {
        guard let enabled = value["enabled"]?.bool,
              let address = value["bind_address"]?.string else {
            throw CLIError.invalidJSON(L10n.text("app.lan.invalid_status"))
        }
        self.enabled = enabled; bindAddress = address
    }
}

@MainActor final class LANSettingsModel: ObservableObject {
    @Published private(set) var state: LANSettingsState?
    @Published private(set) var isReading = false
    @Published private(set) var isSaving = false
    @Published private(set) var stale = true
    @Published private(set) var error: String?
    @Published var bindAddress = ""
    private var baseline = ""
    private var editing = false
    private let run: ([String]) async throws -> JSONValue

    init(cli: NagiCLIClient = NagiCLIClient(), run: (([String]) async throws -> JSONValue)? = nil) {
        self.run = run ?? { try await cli.run($0) }
    }

    var busy: Bool { isReading || isSaving }
    var canToggle: Bool { !busy && !stale && state != nil }
    var canApplyAddress: Bool { canToggle && state?.enabled == true && validAddress && bindAddress != baseline }
    var normalizedAddress: String { bindAddress.trimmingCharacters(in: .whitespacesAndNewlines) }
    var exposesAllInterfaces: Bool { ["", "*", "0.0.0.0", "::"].contains(normalizedAddress) }
    var validAddress: Bool {
        if normalizedAddress.isEmpty || normalizedAddress == "*" { return true }
        var ipv4 = in_addr(), ipv6 = in6_addr()
        return normalizedAddress.withCString {
            inet_pton(AF_INET, $0, &ipv4) == 1 || inet_pton(AF_INET6, $0, &ipv6) == 1
        }
    }

    func setEditing(_ value: Bool) { editing = value }

    func refresh() async {
        guard !busy else { return }
        isReading = true
        defer { isReading = false }
        do {
            accept(try LANSettingsState(await run(["lan", "status"])))
            error = nil
        } catch {
            stale = true
            self.error = L10n.format("app.lan.load_failed", error.localizedDescription)
        }
    }

    func setEnabled(_ enabled: Bool) async {
        guard canToggle, enabled != state?.enabled, !enabled || validAddress else { return }
        await apply(enabled ? enableArguments : ["lan", "disable"])
    }

    func applyAddress() async {
        guard canApplyAddress else { return }
        await apply(enableArguments)
    }

    private var enableArguments: [String] {
        normalizedAddress.isEmpty || normalizedAddress == "*" ? ["lan", "enable"] : ["lan", "enable", normalizedAddress]
    }

    private func accept(_ value: LANSettingsState, replaceDraft: Bool = false) {
        let preserve = !replaceDraft && (editing || bindAddress != baseline)
        state = value; stale = false
        let address = value.enabled ? value.bindAddress : "*"
        if !preserve { bindAddress = address }
        baseline = address
    }

    private func apply(_ arguments: [String]) async {
        isSaving = true; error = nil
        defer { isSaving = false }
        do {
            accept(try LANSettingsState(await run(arguments)), replaceDraft: true)
        } catch {
            let failure = L10n.format("app.lan.save_failed", error.localizedDescription)
            let draft = bindAddress
            stale = true
            // A failed PATCH/read can still have changed the listener. Never retry
            // the mutation; reconcile actual state while retaining the user's draft.
            do { accept(try LANSettingsState(await run(["lan", "status"]))) }
            catch { }
            bindAddress = draft
            self.error = failure
        }
    }
}
