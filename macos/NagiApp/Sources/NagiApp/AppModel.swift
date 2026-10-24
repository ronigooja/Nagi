import AppKit
import Foundation
import ServiceManagement

struct NodeDelay {
    let status: String
    let milliseconds: Int64?
    let measuredAt: String?
    let testOrigin: String?
    let reason: String?
    init(_ value: JSONValue? = nil) {
        status = value?["status"]?.string ?? "untested"
        milliseconds = value?["delay_ms"]?.int64
        measuredAt = value?["measured_at"]?.string
        testOrigin = value?["test_url"]?.string
        reason = (value?["reason"]?.displayString).flatMap { $0.isEmpty ? nil : $0 }
    }
    var label: String {
        if status == "ok", let milliseconds { return milliseconds == 0 ? "<1 ms" : "\(milliseconds) ms" }
        if status == "failed" { return L10n.text("app.ui.failed") }
        return reason == nil ? L10n.text("app.ui.untested") : L10n.text("app.ui.unknown")
    }
    var detail: String {
        let measuredTime = measuredAt.map { MenuAppDelegate.subscriptionTime($0) } ?? "unknown"
        let measurement = L10n.format("app.ui.latest_engine_measurement_nmeasured_at_ntest_origin", String(describing: label), String(describing: measuredTime), String(describing: testOrigin ?? "unknown"))
        return measurement + (reason.map { L10n.format("app.ui.nreason", String(describing: $0)) } ?? "")
    }
    static func parse(_ value: JSONValue?) -> [String: NodeDelay] {
        guard let value, case .objectValue(let entries) = value else { return [:] }
        return entries.mapValues { NodeDelay($0) }
    }
}

struct NodeInfo {
    let kind: String
    let status: String?
    var selectedDelay: NodeDelay? = nil
    var selectedBuiltin: String? = nil
    static func parse(_ value: JSONValue?) -> [String: NodeInfo] {
        guard case let .objectValue(entries)? = value else { return [:] }
        return entries.mapValues { NodeInfo(kind: $0["kind"]?.string ?? "unknown", status: $0["status"]?.string,
                                            selectedDelay: $0["selected_delay"].map { NodeDelay($0) },
                                            selectedBuiltin: $0["selected_builtin"]?.string) }
    }
    static func kind(_ name: String, in info: [String: NodeInfo]) -> String {
        if let node = info[name] { return node.kind }
        if ["DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"].contains(name) { return "builtin" }
        return info[name]?.kind ?? "unknown"
    }
}

struct ProxyGroup {
    let name: String
    let kind: String
    let selected: String
    let fixed: String?
    let selectedStatus: String?
    let nodes: [String]
    let hidden: Bool
    let icon: String?
    var nodeDelays: [String: NodeDelay] = [:]
    var nodeInfo: [String: NodeInfo] = [:]
    var testableNodes: [String] { nodes.filter { NodeInfo.kind($0, in: nodeInfo) == "node" } }
    var hasLatencyCandidates: Bool {
        nodes.contains { node in
            let kind = NodeInfo.kind(node, in: nodeInfo)
            let rejecting = ["REJECT", "REJECT-DROP"]
            return !(kind == "builtin" && rejecting.contains(node) ||
                     kind == "group" && rejecting.contains(nodeInfo[node]?.selectedBuiltin ?? ""))
        }
    }
    func nodeTitle(_ node: String) -> String {
        switch NodeInfo.kind(node, in: nodeInfo) {
        case "node": return "\(node) · \(nodeDelays[node]?.label ?? L10n.text("app.ui.untested"))"
        case "group": return "\(node) · \(nodeInfo[node]?.selectedBuiltin ?? nodeInfo[node]?.selectedDelay?.label ?? L10n.text("app.ui.unknown"))"
        default: break
        }
        return node
    }
    func nodeDetail(_ node: String) -> String? {
        NodeInfo.kind(node, in: nodeInfo) == "node" ? (nodeDelays[node] ?? NodeDelay()).detail : nil
    }
    var testCommand: [String] { ["proxy", "delays", name] }
    var testTitle: String { L10n.text("app.ui.test_current_group") }
    var testExplanation: String { automatic ? L10n.text("app.ui.tests_this_group_clears_its_fixed_choice") : L10n.text("app.ui.tests_all_nodes_in_this_group_preserves") }
    var selectable: Bool {
        ["selector", "urltest", "fallback"].contains(kind.lowercased())
    }
    var automatic: Bool { ["urltest", "fallback"].contains(kind.lowercased()) }
    var supportsHealthcheck: Bool { automatic || ["loadbalance", "load-balance"].contains(kind.lowercased()) }
    func selectCommand(_ node: String) -> [String]? {
        guard selectable, !hidden, nodes.contains(node) else { return nil }
        return ["proxy", "select", name, node]
    }
    var restoreAutomaticCommand: [String]? { automatic && !hidden ? ["proxy", "auto", name] : nil }
}
struct Subscription {
    let name: String
    let keyConfigured: Bool
    let scheduleSeconds: Int64
    let schedulerRunning: Bool
    let nextDue: String?
    let pauseReason: String?
    let lastFailureStage: String?
    let lastFailureReason: String?
    let expiresAt: String?
    let upload: Int64
    let download: Int64
    let total: Int64
}
struct ProxyProvider {
    let name: String; let type: String; let vehicleType: String; let nodes: [String]
    var nodeDelays: [String: NodeDelay] = [:]
    var updateCommand: [String] { ["proxy", "provider", "update", name] }
    var removeCommand: [String] { ["proxy", "provider", "remove", name] }
    var nodeInfo: [String: NodeInfo] = [:]
    var testableNodes: [String] { nodes.filter { NodeInfo.kind($0, in: nodeInfo) == "node" } }
    var healthcheckCommand: [String] { ["proxy", "provider", "healthcheck", name] }
}
enum TrafficDisplay: String, CaseIterable {
    case both, icon
    var title: String {
        switch self {
        case .both: return L10n.text("app.ui.traffic_rates")
        case .icon: return L10n.text("app.ui.icon_only")
        }
    }
}
enum EngineState { case starting, running, stopped, unavailable }

@MainActor final class AppModel {
    // Keep profile action command names in one place.
    private enum ProfileCommands {
        static let useDefault = ["profile", "use", "default"]
        static func applyDefaultOverride(for profile: String) -> [String] {
            ["profile", "override", "apply-default", profile]
        }
        static func restoreDefaultOverride(for profile: String) -> [String] {
            ["profile", "override", "restore-default", profile]
        }
    }

    var onChange: (() -> Void)?
    var onTrafficChange: (() -> Void)?
    private let cli: NagiCLIClient
    let connections: ConnectionsModel
    lazy var settings: SettingsModel = {
        SettingsModel(browser: browserSettings,
            readDisplay: { [weak self] in self?.display ?? .both },
            writeDisplay: { [weak self] in self?.setSettingsDisplay($0) },
            readAppLogin: { [weak self] in self?.appLoginEnabled ?? false },
            writeAppLogin: { [weak self] enabled in
                guard let self else { throw CancellationError() }
                try await self.changeSettingsAppLogin(enabled)
            },
            readEngineLogin: { [weak self] in
                guard let self else { throw CancellationError() }
                return try await self.readSettingsEngineLogin()
            },
            writeEngineLogin: { [weak self] enabled in
                guard let self else { throw CancellationError() }
                try await self.changeSettingsEngineLogin(enabled)
            })
    }()
    lazy var browserSettings: BrowserSettingsModel = {
        BrowserSettingsModel(run: { [weak self] arguments in
            guard let self else { throw CancellationError() }
            return try await self.executeBrowserSettings(arguments)
        })
    }()
    lazy var requestRewrite: RequestRewriteModel = {
        let rewrite = RequestRewriteModel(cli: cli)
        rewrite.submit = { [weak self] arguments, input in
            guard let self else { throw CancellationError() }
            return try await self.executeRewrite(arguments, input: input)
        }
        rewrite.export = { [weak self] arguments in
            guard let self else { throw CancellationError() }
            return try await self.exportRewrite(arguments)
        }
        rewrite.onStatusChange = { [weak self] in self?.notify() }
        return rewrite
    }()
    lazy var capture: CaptureModel = {
        let capture = CaptureModel(cli: cli)
        capture.submit = { [weak self] arguments, input in
            guard let self else { throw CancellationError() }
            guard !self.busy && !self.quitting else { throw RewriteFormError.invalid(L10n.text("app.ui.another_operation_is_in_progress")) }
            self.busy = true
            defer { self.busy = false }
            await self.waitForConnectionsOperation(skipCaptureOperation: true)
            return try await self.cli.capture(arguments, input: input)
        }
        capture.export = { [weak self] arguments in
            guard let self else { throw CancellationError() }
            guard !self.busy && !self.quitting else { throw RewriteFormError.invalid(L10n.text("app.ui.another_operation_is_in_progress")) }
            self.busy = true
            defer { self.busy = false }
            await self.waitForConnectionsOperation(skipCaptureOperation: true)
            try await self.cli.captureExport(arguments)
        }
        capture.draftRun = { [weak self] arguments, input in
            guard let self else { throw CancellationError() }
            return try await self.executeRewrite(arguments, input: input)
        }
        capture.onStatusChange = { [weak self] in self?.notify() }
        return capture
    }()
    private let watchDelay: (Int, Bool) -> UInt64
    private var refreshTask: Task<Void, Never>?
    private struct RefreshScope: OptionSet {
        let rawValue: Int
        static let status = Self(rawValue: 1 << 0)
        static let profiles = Self(rawValue: 1 << 1)
        static let subscriptions = Self(rawValue: 1 << 2)
        static let service = Self(rawValue: 1 << 3)
        static let groups = Self(rawValue: 1 << 4)
        static let providers = Self(rawValue: 1 << 5)
        static let settings = Self(rawValue: 1 << 6)
        static let runtime: Self = [.groups, .providers, .settings]
        static let menu: Self = [.status, .profiles, .subscriptions, .service, .runtime]
    }
    private var refreshWorker: Task<Bool, Never>?
    private var pendingRefresh: RefreshScope = []
    private var activeRefresh: RefreshScope = []
    private var activeGeneration = -1
    private var pendingForced = false
    private var metadataReadAt: [Int: Date] = [:]
    private let now: () -> Date
    private let runCommand: ([String]) async throws -> JSONValue
    private var trafficTask: Task<Void, Never>?
    private var trafficStream: TrafficStream?
    private var subscriptionWatch: SubscriptionWatch?
    private var subscriptionWatchTask: Task<Void, Never>?
    private var watchGeneration = 0
    private var watchRetry = 0
    init(cli: NagiCLIClient = NagiCLIClient(), connections: ConnectionsModel? = nil, watchDelay: @escaping (Int, Bool) -> UInt64 = { attempt, duplicate in
        duplicate ? 15_000_000_000 : UInt64(1 << attempt) * 1_000_000_000
    }, now: @escaping () -> Date = Date.init,
         runCommand: (([String]) async throws -> JSONValue)? = nil) {
        self.cli = cli
        self.connections = connections ?? ConnectionsModel(cli: cli)
        self.watchDelay = watchDelay
        self.now = now
        self.runCommand = runCommand ?? { arguments in try await cli.run(arguments) }
    }
    private var trafficGeneration = 0
    private var settingsGeneration = 0
    private var lastSample = Date.distantPast
    private var streamStartedAt = Date.distantPast
    private var sleeping = false { didSet { connections.setSuspended(sleeping || quitting); requestRewrite.setSuspended(sleeping || quitting); capture.setSuspended(sleeping || quitting) } }
    private var quitting = false {
        didSet {
            connections.setSuspended(sleeping || quitting)
            requestRewrite.setSuspended(sleeping || quitting)
            capture.setSuspended(sleeping || quitting)
            connections.setOperationBlocked(busy || quitting)
            requestRewrite.setBlocked(busy || quitting)
            capture.setBlocked(busy || quitting)
        }
    }
    private(set) var busy = false { didSet {
        connections.setOperationBlocked(busy || quitting)
        requestRewrite.setBlocked(busy || quitting)
        capture.setBlocked(busy || quitting)
        notify()
        if !busy, !pendingRefresh.isEmpty, !sleeping, !quitting {
            Task { _ = await requestRefresh([], force: false) }
        }
    } }
    private(set) var applyingSetting = false { didSet { notify() } }
    private(set) var state: EngineState = .starting { didSet { connections.setEngineState(state); notify() } }
    // A detached update failure survives ordinary launch and polling results.
    private var updateErrorMessage: String?
    private enum ErrorOrigin { case operation, status, traffic }
    private var errorOrigin: ErrorOrigin = .operation
    private var operationErrorMessage: String?
    private(set) var errorMessage: String? {
        get { updateErrorMessage ?? operationErrorMessage }
        set { operationErrorMessage = newValue; errorOrigin = .operation; notify() }
    }
    private(set) var quitErrorMessage: String? { didSet { notify() } }
    private(set) var notice: String? { didSet { notify() } }
    private(set) var profile: String?
    private(set) var profiles: [String] = []
    private(set) var defaultOverrideApplied: Bool?
    private(set) var dnsProxyRequired = false
    private(set) var dnsProxyGroups: [String] = []
    private(set) var dnsProxyGroup: String = ""
    private(set) var groups: [ProxyGroup] = []
    private(set) var subscriptions: [Subscription] = []
    private(set) var providers: [ProxyProvider] = []
    private(set) var providersUnavailable = false
    private(set) var schedulerRunning = false
    private(set) var mode: String?
    private(set) var systemProxy: Bool?
    private(set) var tun: Bool?
    private(set) var tunAdapter: String?
    private(set) var serviceInstalled: Bool?
    private(set) var uploadBPS: Int64?
    private(set) var downloadBPS: Int64?
    var display: TrafficDisplay = TrafficDisplay(rawValue: UserDefaults.standard.string(forKey: "trafficDisplay") ?? "both") ?? .both {
        didSet { UserDefaults.standard.set(display.rawValue, forKey: "trafficDisplay"); notify() }
    }
    var appLoginEnabled: Bool { SMAppService.mainApp.status == .enabled }
    var statusTitle: String {
        guard display != .icon else { return "" }
        let fresh = state == .running && Date().timeIntervalSince(lastSample) < 3.5
        let upload = Self.rate(fresh ? (uploadBPS ?? 0) : 0)
        let download = Self.rate(fresh ? (downloadBPS ?? 0) : 0)
        return "\(upload)\n\(download)"
    }
    var accessibilityTraffic: String {
        guard state == .running, Date().timeIntervalSince(lastSample) < 3.5,
              let up = uploadBPS, let down = downloadBPS else { return L10n.text("app.ui.traffic_unavailable") }
        return L10n.format("app.ui.upload_download", String(describing: Self.rate(up).trimmingCharacters(in: .whitespaces)), String(describing: Self.rate(down).trimmingCharacters(in: .whitespaces)))
    }
    private static func rate(_ bytes: Int64) -> String {
        let value = Double(max(0, bytes))
        let unit: String
        let scaled: Double
        if value >= 1_000_000_000 { unit = "GB/s"; scaled = value / 1_000_000_000 }
        else if value >= 1_000_000 { unit = "MB/s"; scaled = value / 1_000_000 }
        else if value >= 1_000 { unit = "KB/s"; scaled = value / 1_000 }
        else { unit = "B/s"; scaled = value }
        let number = scaled < 10 && unit != "B/s" ? String(format: "%.1f", scaled) : String(format: "%.0f", scaled)
        let label = "\(number) \(unit)"
        return String(repeating: " ", count: max(0, 10 - label.count)) + label
    }
    private func notify() { onChange?() }

    func launch() {
        guard refreshTask == nil else { return }
        busy = true
        refreshTask = Task {
            await loadUpdateError()
            await startEngine()
            startSubscriptionWatch()
            busy = false
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(nanoseconds: 5_000_000_000)
            }
        }
    }
    private func loadUpdateError() async {
        // The CLI owns persistence and consumes the failure once for this launch.
        // An older CLI may not support this read; it must not prevent startup.
        guard let result = try? await runCommand(["app", "update-error"]),
              let message = result["error"]?.displayString, !message.isEmpty else { return }
        updateErrorMessage = message
        notify()
    }
    func retryStart() {
        guard !busy && !quitting else { return }
        busy = true
        Task { await waitForConnectionsOperation(); await startEngine(); await refresh(); busy = false }
    }
    private func startEngine() async {
        settingsGeneration += 1
        defer { settingsGeneration += 1 }
        state = .starting
        do { _ = try await runCommand(["start"]); errorMessage = nil }
        catch let error as CLIError where error.commandCode == "already_running" { errorMessage = nil }
        catch { state = .unavailable; errorMessage = L10n.format("app.ui.start_failed", String(describing: error.localizedDescription)) }
    }
    func refresh() async {
        var scope: RefreshScope = [.status, .groups, .settings]
        for (field, ttl) in [(RefreshScope.profiles, 30.0), (.subscriptions, 30.0), (.providers, 30.0), (.service, 60.0)] {
            if now().timeIntervalSince(metadataReadAt[field.rawValue] ?? .distantPast) >= ttl { scope.insert(field) }
        }
        _ = await requestRefresh(scope)
    }
    @discardableResult func refreshMenuData() async -> Bool {
        requestRewrite.refreshStatus()
        return await requestRefresh(.menu)
    }

    // One worker owns reads. Concurrent callers share its current pass; mutations
    // enqueue a new generation even when a pre-mutation pass is still finishing.
    @discardableResult private func requestRefresh(_ scope: RefreshScope, force: Bool = false) async -> Bool {
        guard !sleeping, !quitting else { return false }
        if activeGeneration == settingsGeneration && !force {
            pendingRefresh.formUnion(scope.subtracting(activeRefresh))
        } else { pendingRefresh.formUnion(scope) }
        pendingForced = pendingForced || force
        if busy && !pendingForced { return false }
        if let refreshWorker {
            let result = await refreshWorker.value
            if !pendingRefresh.isEmpty && !sleeping && !quitting && (!busy || pendingForced) {
                return await requestRefresh([])
            }
            return result
        }
        guard !pendingRefresh.isEmpty else { return true }
        let worker = Task { @MainActor in
            var succeeded = true
            while !pendingRefresh.isEmpty && !sleeping && !quitting && !Task.isCancelled {
                if busy && !pendingForced { break }
                let requested = pendingRefresh
                pendingRefresh = []
                pendingForced = false
                activeRefresh = requested
                activeGeneration = settingsGeneration
                let generation = settingsGeneration
                succeeded = await readRefresh(requested, generation: generation)
            }
            activeRefresh = []
            activeGeneration = -1
            refreshWorker = nil
            return succeeded
        }
        refreshWorker = worker
        return await worker.value
    }
    private func readRefresh(_ scope: RefreshScope, generation: Int) async -> Bool {
        func valid() -> Bool { generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting }
        var scope = scope
        var subscriptionsRefreshed = true
        if scope.contains(.status) {
            capture.refreshStatus()
            do {
                let value = try await runCommand(["status"])
                guard valid() else { return false }
                state = value["running"]?.bool == true ? .running : .stopped
                let currentProfile = value["profile"]?.string
                if currentProfile != profile {
                    scope.formUnion([.profiles, .runtime])
                    activeRefresh.formUnion([.profiles, .runtime])
                    metadataReadAt.removeValue(forKey: RefreshScope.profiles.rawValue)
                    metadataReadAt.removeValue(forKey: RefreshScope.providers.rawValue)
                }
                profile = currentProfile
                if errorOrigin == .status { errorMessage = nil }
                if state == .running { if trafficTask == nil { startTraffic() } }
                else { stopTraffic() }
            } catch {
                guard valid() else { return false }
                state = .unavailable; stopTraffic()
                errorMessage = L10n.format("app.ui.status_unavailable", String(describing: error.localizedDescription)); errorOrigin = .status
            }
        }
        if scope.contains(.profiles) {
            metadataReadAt.removeValue(forKey: RefreshScope.profiles.rawValue)
            if await refreshProfiles() { metadataReadAt[RefreshScope.profiles.rawValue] = now() }
        }
        guard valid() else { return false }
        if scope.contains(.subscriptions) {
            metadataReadAt.removeValue(forKey: RefreshScope.subscriptions.rawValue)
            subscriptionsRefreshed = await refreshSubscriptions()
            if subscriptionsRefreshed { metadataReadAt[RefreshScope.subscriptions.rawValue] = now() }
        }
        guard valid() else { return false }
        if scope.contains(.service) {
            metadataReadAt.removeValue(forKey: RefreshScope.service.rawValue)
            do {
                let value = try await runCommand(["service", "status"])
                guard valid() else { return false }
                serviceInstalled = value["installed"]?.bool
                metadataReadAt[RefreshScope.service.rawValue] = now()
            } catch {
                guard valid() else { return false }
                serviceInstalled = nil
            }
        }
        guard valid() else { return false }
        await refreshRuntimeSettings(scope)
        guard valid() else { return false }
        notify()
        return subscriptionsRefreshed
    }
    @discardableResult private func refreshProfiles() async -> Bool {
        let generation = settingsGeneration
        do {
            let value = try await runCommand(["profile", "list"])
            guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return false }
            profiles = value["profiles"]?.array.compactMap { $0.string ?? $0["name"]?.string } ?? []
            profile = value["current"]?.string
            if let profile, !profile.isEmpty {
                do {
                    let status = try await runCommand(["profile", "override", "default-status", profile])
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return false }
                    defaultOverrideApplied = status["default_override_applied"]?.bool
                    dnsProxyRequired = status["dns_proxy_required"]?.bool == true
                    dnsProxyGroups = status["dns_proxy_groups"]?.array.compactMap(\.string) ?? []
                    dnsProxyGroup = status["dns_proxy_group"]?.string ?? ""
                } catch {
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return false }
                    defaultOverrideApplied = nil
                    dnsProxyRequired = false; dnsProxyGroups = []; dnsProxyGroup = ""
                    return false
                }
            } else { defaultOverrideApplied = nil }
            if let problem = value["current_error"]?.displayString {
                errorMessage = L10n.format("app.ui.profile_selection_unavailable", String(describing: problem))
                return false
            }
            return true
        } catch {
            guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return false }
            defaultOverrideApplied = nil
            errorMessage = error.localizedDescription
            return false
        }
    }
    @discardableResult private func refreshSubscriptions() async -> Bool {
        let generation = settingsGeneration
        do {
            let value = try await runCommand(["subscription", "list"])
            guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return false }
            schedulerRunning = value["scheduler_running"]?.bool == true
            subscriptions = value["subscriptions"]?.array.compactMap { item in
                guard let name = item["name"]?.string else { return nil }
                return Subscription(name: name,
                    keyConfigured: item["key_configured"]?.bool == true,
                    scheduleSeconds: max(0, item["schedule_seconds"]?.int64 ?? 3600),
                    schedulerRunning: schedulerRunning,
                    nextDue: item["next_due"]?.string,
                    pauseReason: item["pause_reason"]?.displayString,
                    lastFailureStage: item["last_failure_stage"]?.string,
                    lastFailureReason: item["last_failure_reason"]?.displayString,
                    expiresAt: item["expires_at"]?.string,
                    upload: item["upload"]?.int64 ?? 0,
                    download: item["download"]?.int64 ?? 0,
                    total: item["total"]?.int64 ?? 0)
            } ?? []
            return true
        } catch {
            guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return false }
            errorMessage = error.localizedDescription; return false
        }
    }
    private func refreshRuntimeSettings(_ scope: RefreshScope = .runtime) async {
        let generation = settingsGeneration
        if state == .running {
            if scope.contains(.groups) {
                do {
                    let value = try await runCommand(["proxy", "groups"])
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    groups = value["groups"]?.array.compactMap { item in
                        guard let name = item["name"]?.string else { return nil }
                        return ProxyGroup(name: name, kind: item["type"]?.string ?? "", selected: item["now"]?.string ?? "",
                                          fixed: item["fixed"]?.string, selectedStatus: item["selected_status"]?.string,
                                          nodes: item["all"]?.array.compactMap(\.string) ?? [],
                                          hidden: item["hidden"]?.bool == true, icon: item["icon"]?.string, nodeDelays: NodeDelay.parse(item["node_delays"]), nodeInfo: NodeInfo.parse(item["node_info"]))
                    } ?? []
                } catch {
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    groups = []; errorMessage = error.localizedDescription
                }
            }
            if scope.contains(.settings) {
                do {
                    let value = try await runCommand(["mode"])
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    mode = value["mode"]?.string
                } catch {
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    mode = nil; errorMessage = error.localizedDescription
                }
                do {
                    let value = try await runCommand(["tun", "status"])
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    tun = value["enabled"]?.bool; tunAdapter = value["adapter_status"]?.string
                } catch {
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    tun = nil; tunAdapter = nil; errorMessage = error.localizedDescription
                }
                do {
                    let value = try await runCommand(["system-proxy", "status"])
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    systemProxy = value["enabled"]?.bool
                } catch {
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    systemProxy = nil; errorMessage = error.localizedDescription
                }
            }
            if scope.contains(.providers) {
                metadataReadAt.removeValue(forKey: RefreshScope.providers.rawValue)
                do {
                    let value = try await runCommand(["proxy", "providers"])
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    metadataReadAt[RefreshScope.providers.rawValue] = now()
                    providersUnavailable = false
                    providers = value["providers"]?.array.compactMap { item in
                        guard let name = item["name"]?.string else { return nil }
                        return ProxyProvider(name: name, type: item["type"]?.string ?? "", vehicleType: item["vehicle_type"]?.string ?? "", nodes: item["nodes"]?.array.compactMap(\.string) ?? [], nodeDelays: NodeDelay.parse(item["node_delays"]), nodeInfo: NodeInfo.parse(item["node_info"]))
                    } ?? []
                } catch {
                    guard generation == settingsGeneration && !Task.isCancelled && !sleeping && !quitting else { return }
                    providers = []
                    providersUnavailable = true
                    errorMessage = L10n.format("app.ui.proxy_providers_unavailable", String(describing: error.localizedDescription))
                }
            }
        } else {
            metadataReadAt.removeValue(forKey: RefreshScope.providers.rawValue)
            systemProxy = nil; groups = []; providers = []; providersUnavailable = false; mode = nil; tun = nil; tunAdapter = nil
        }
    }
    func perform(_ arguments: [String]) {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            await waitForConnectionsOperation()
            do {
                let result = try await runCommand(arguments)
                settingsGeneration += 1
                errorMessage = nil
                if arguments.count == 4, Array(arguments.prefix(3)) == ["proxy", "provider", "update"] {
                    notice = L10n.format("app.ui.provider_updated", String(describing: arguments[3]))
                }
                if arguments.count >= 2 {
                    switch (arguments[0], arguments[1]) {
                    case ("system-proxy", _): systemProxy = result["enabled"]?.bool
                    case ("tun", _):
                        tun = result["enabled"]?.bool
                        tunAdapter = result["adapter_status"]?.string
                    case ("mode", _): mode = result["mode"]?.string
                    case ("proxy", "select"):
                        await requestRefresh(.groups, force: true)
                    case ("service", "install"): serviceInstalled = true
                    case ("service", "uninstall"): serviceInstalled = false
                    case ("profile", "use"):
                        profile = result["current"]?.string
                        await requestRefresh([.profiles, .runtime], force: true)
                    case ("profile", "remove"):
                        notice = L10n.text("app.ui.profile_deleted")
                        await requestRefresh([.profiles, .subscriptions], force: true)
                    case ("profile", "override"):
                        await requestRefresh([.profiles, .runtime], force: true)
                    case ("proxy", "auto"):
                        await requestRefresh(.groups, force: true)
                    case ("proxy", "test"), ("proxy", "delay"), ("proxy", "delays"), ("proxy", "healthcheck"), ("proxy", "provider"):
                        await requestRefresh([.groups, .providers], force: true)
                    case ("subscription", "schedule"), ("subscription", "key"):
                        await requestRefresh(.subscriptions, force: true)
                    default: await requestRefresh(.menu, force: true)
                    }
                }
                notify()
            }
            catch {
                settingsGeneration += 1
                if arguments.first == "proxy", arguments.count > 1,
                   ["test", "delay", "delays", "provider", "healthcheck"].contains(arguments[1]) {
                    await requestRefresh([.groups, .providers], force: true)
                }
                errorMessage = error.localizedDescription
            }
        }
    }

    func healthcheckSettings(kind: String, name: String) async throws -> JSONValue {
        guard !busy && !quitting else { throw CLIError.command("busy", L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true
        defer { busy = false }
        await waitForConnectionsOperation()
        return try await runCommand(["proxy", "healthcheck", "show", kind, name, "--full-url"])
    }

    func saveHealthcheckSettings(kind: String, name: String, seconds: Int64?) async -> String? {
        guard !busy && !quitting else { return L10n.text("app.ui.another_operation_is_in_progress") }
        if let seconds, !(1...31_536_000).contains(seconds) { return L10n.text("app.ui.test_interval_must_be_1_31536000_seconds") }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        defer { applyingSetting = false; busy = false }
        await waitForConnectionsOperation()
        do {
            let arguments = ["proxy", "healthcheck", seconds == nil ? "reset" : "set", kind, name] + (seconds.map { [String($0)] } ?? [])
            _ = try await runCommand(arguments)
            settingsGeneration += 1
            await requestRefresh([.groups, .providers], force: true)
            errorMessage = nil
            return nil
        } catch {
            settingsGeneration += 1
            await requestRefresh([.groups, .providers], force: true)
            errorMessage = error.localizedDescription
            return error.localizedDescription
        }
    }

    func subscriptionUpdateSettings(_ name: String) async throws -> Int64 {
        guard !busy && !quitting else { throw CLIError.command("busy", L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true
        defer { busy = false }
        await waitForConnectionsOperation()
        let value = try await runCommand(["subscription", "list"])
        guard let item = value["subscriptions"]?.array.first(where: { $0["name"]?.string == name }),
              let seconds = item["schedule_seconds"]?.int64 else {
            throw CLIError.command("subscription_error", L10n.text("app.ui.subscription_schedule_is_unavailable"))
        }
        return seconds
    }

    func providerUpdateSettings(_ name: String) async throws -> JSONValue {
        guard !busy && !quitting else { throw CLIError.command("busy", L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true
        defer { busy = false }
        await waitForConnectionsOperation()
        return try await runCommand(["proxy", "provider", "interval", "show", name])
    }

    func saveSourceSetting(_ arguments: [String]) async -> String? {
        guard !busy && !quitting else { return L10n.text("app.ui.another_operation_is_in_progress") }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        defer { applyingSetting = false; busy = false }
        await waitForConnectionsOperation()
        do {
            _ = try await runCommand(arguments)
            settingsGeneration += 1
            await requestRefresh([.groups, .providers, .subscriptions], force: true)
            errorMessage = nil
            return nil
        } catch {
            settingsGeneration += 1
            await requestRefresh([.groups, .providers, .subscriptions], force: true)
            errorMessage = error.localizedDescription
            return error.localizedDescription
        }
    }

    // Configuration, validation, persistence, and activation remain CLI operations.
    func addProvider(_ name: String, url: String, group: String, intervalSeconds: Int64 = 3600, updateOff: Bool = false, testURL: String = "") async -> String? {
        guard !busy && !quitting else { return L10n.text("app.ui.another_operation_is_in_progress_try_again") }
        guard updateOff || (1...31_536_000).contains(intervalSeconds) else {
            return L10n.text("app.ui.update_interval_must_be_1_31536000_seconds")
        }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        defer { applyingSetting = false; busy = false }
        await waitForConnectionsOperation()
        do {
            let arguments = ["proxy", "provider", "add", name, url, group, updateOff ? "off" : String(intervalSeconds)] + (testURL.isEmpty ? [] : ["--test-url", testURL])
            let result = try await runCommand(arguments)
            settingsGeneration += 1
            errorMessage = nil
            profile = result["profile"]?.string ?? profile
            await requestRefresh([.groups, .providers], force: true)
            notice = result["group_created"]?.bool == true
                ? L10n.text("app.ui.provider_added_new_group_choices_are_in")
                : L10n.text("app.ui.provider_added_group_choices_are_in_select")
            notify()
            return nil
        } catch {
            settingsGeneration += 1
            // CLI errors are redacted; also protect the form URL against older CLI versions.
            let detail = error.localizedDescription.replacingOccurrences(of: url, with: "<redacted URL>")
                .replacingOccurrences(of: "(?i)https?://[^\\s<>\"']+", with: "<redacted URL>", options: .regularExpression)
            let message = L10n.format("app.ui.provider_add_failed", String(describing: detail))
            errorMessage = message
            return message
        }
    }

    var deletableProfiles: [String] {
        guard let profile, !profile.isEmpty else { return [] }
        return profiles.filter { $0 != profile && $0 != "default" }
    }

    func removeProfile(_ name: String) {
        guard !busy, deletableProfiles.contains(name) else { return }
        perform(["profile", "remove", name])
    }

    func switchToDefaultProfile() {
        perform(ProfileCommands.useDefault)
    }

    func updateNagi(_ readyToQuit: @escaping () -> Void) {
        guard !busy && !quitting else { return }
        updateErrorMessage = nil
        busy = true
        Task {
            await waitForConnectionsOperation()
            do {
                let result = try await runCommand(["update", "--app"])
                guard result["scheduled"]?.bool == true else {
                    throw CLIError.invalidJSON(L10n.text("app.ui.the_cli_did_not_schedule_an_update"))
                }
                errorMessage = nil
                readyToQuit()
                // Request termination before releasing busy. This lets
                // applicationShouldTerminate mark quitting first, so the
                // busy observer cannot start a queued refresh in the gap
                // between scheduling the updater and quitting the App.
                busy = false
            } catch {
                errorMessage = L10n.format("app.ui.update_could_not_start", String(describing: error.localizedDescription))
                busy = false
            }
        }
    }

    func applyDefaultOverrideToCurrentProfile(group: String? = nil) {
        guard let profile, !profile.isEmpty else {
            errorMessage = L10n.text("app.ui.no_current_profile_is_selected")
            return
        }
        guard let defaultOverrideApplied else {
            errorMessage = L10n.text("app.ui.default_runtime_settings_status_is_unavailable_try")
            return
        }
        if defaultOverrideApplied {
            perform(ProfileCommands.restoreDefaultOverride(for: profile))
            return
        }
        var command = ProfileCommands.applyDefaultOverride(for: profile)
        if let group, !group.isEmpty { command += ["--dns-proxy", group] }
        perform(command)
    }
    func updateAndApplySubscription(_ name: String) {
        saveDownloadAndApplySubscription(name, url: nil)
    }
    func addAndApplySubscription(_ name: String, url: String, schedule: String = "3600", testURL: String = "") {
        saveDownloadAndApplySubscription(name, url: url, schedule: schedule, testURL: testURL)
    }
    private func saveDownloadAndApplySubscription(_ name: String, url: String?, schedule: String = "3600", testURL: String = "") {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            await waitForConnectionsOperation()
            if let url {
                do {
                    var arguments = ["subscription", "add", name, url]
                    if schedule != "3600" { arguments += ["--schedule", schedule] }
                    if !testURL.isEmpty { arguments += ["--test-url", testURL] }
                    _ = try await runCommand(arguments)
                    settingsGeneration += 1
                    subscriptions.append(Subscription(name: name, keyConfigured: false, scheduleSeconds: Int64(schedule) ?? 0, schedulerRunning: false, nextDue: nil, pauseReason: nil, lastFailureStage: nil, lastFailureReason: nil, expiresAt: nil, upload: 0, download: 0, total: 0))
                    subscriptions.sort { $0.name < $1.name }
                } catch {
                    settingsGeneration += 1
                    errorMessage = L10n.format("app.ui.subscription_add_failed", String(describing: error.localizedDescription))
                    return
                }
            }
            do {
                _ = try await runCommand(["subscription", "update", name])
                settingsGeneration += 1
            } catch {
                settingsGeneration += 1
                await requestRefresh(.subscriptions, force: true)
                let state = url == nil ? L10n.text("app.ui.subscription_download_failed_apply_was_not_attempted") : L10n.text("app.ui.subscription_saved_but_download_failed_apply_was")
                errorMessage = "\(state): \(error.localizedDescription)"
                return
            }
            do {
                let result = try await runCommand(["subscription", "apply", name])
                settingsGeneration += 1
                errorMessage = nil
                profile = result["profile"]?.string
                await requestRefresh([.profiles, .runtime, .subscriptions], force: true)
                notice = L10n.text("app.ui.subscription_updated_and_applied")
                notify()
            } catch {
                settingsGeneration += 1
                await requestRefresh(.subscriptions, force: true)
                errorMessage = L10n.format("app.ui.subscription_downloaded_but_apply_failed_the_new", String(describing: error.localizedDescription))
            }
        }
    }
    func removeSubscription(_ name: String) {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            await waitForConnectionsOperation()
            do {
                _ = try await runCommand(["subscription", "remove", name])
                settingsGeneration += 1
                errorMessage = nil
                subscriptions.removeAll { $0.name == name }
                notice = L10n.text("app.ui.subscription_removed")
            } catch {
                settingsGeneration += 1
                let listRefreshed = await requestRefresh(.subscriptions, force: true)
                let detail = error.localizedDescription
                if !listRefreshed || subscriptions.contains(where: { $0.name == name }) {
                    errorMessage = L10n.format("app.ui.subscription_removal_failed", String(describing: detail))
                } else {
                    errorMessage = L10n.format("app.ui.subscription_record_removed_but_cache_cleanup_failed", String(describing: detail))
                }
            }
        }
    }
    func clearSubscriptionKey(_ name: String) { perform(["subscription", "key", name, "clear"]) }
    func setSubscriptionKey(_ name: String, file: String) { perform(["subscription", "key", name, file]) }
    private func executeBrowserSettings(_ arguments: [String]) async throws -> JSONValue {
        try await executeSettingsCommand(arguments)
    }
    private func executeSettingsCommand(_ arguments: [String]) async throws -> JSONValue {
        guard !busy && !quitting else { throw CLIError.command("busy", L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true
        let mutation = arguments.last != "status"
        if mutation { applyingSetting = true }
        defer { if mutation { applyingSetting = false }; busy = false }
        await waitForConnectionsOperation()
        return try await runCommand(arguments)
    }
    private func readSettingsEngineLogin() async throws -> Bool {
        let value = try await executeSettingsCommand(["service", "status"])
        guard let installed = value["installed"]?.bool else {
            throw CLIError.invalidJSON(L10n.text("app.ui.engine_service_status_unavailable"))
        }
        serviceInstalled = installed
        return installed
    }
    private func changeSettingsEngineLogin(_ enabled: Bool) async throws {
        _ = try await executeSettingsCommand(["service", enabled ? "install" : "uninstall"])
        serviceInstalled = enabled
    }
    private func setSettingsDisplay(_ value: TrafficDisplay) {
        guard !busy && !quitting else { return }
        display = value
    }
    private func changeSettingsAppLogin(_ enabled: Bool) async throws {
        guard !busy && !quitting else { throw CLIError.command("busy", L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true
        defer { busy = false }
        if enabled { try SMAppService.mainApp.register() }
        else { try await SMAppService.mainApp.unregister() }
        notify()
    }
    func executeRewrite(_ arguments: [String], input: Data?) async throws -> JSONValue {
        guard !busy && !quitting else { throw RewriteFormError.invalid(L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true; applyingSetting = true; settingsGeneration += 1
        defer { settingsGeneration += 1; applyingSetting = false; busy = false }
        await waitForConnectionsOperation(skipRewriteOperation: true)
        return try await cli.rewrite(arguments, input: input)
    }
    func exportRewrite(_ arguments: [String]) async throws -> Data {
        guard !busy && !quitting else { throw RewriteFormError.invalid(L10n.text("app.ui.another_operation_is_in_progress")) }
        busy = true
        defer { busy = false }
        await waitForConnectionsOperation(skipRewriteOperation: true)
        return try await cli.rewriteExport(arguments)
    }
    private func waitForConnectionsOperation(skipRewriteOperation: Bool = false, skipCaptureOperation: Bool = false) async {
        // busy is acquired synchronously before reaching this wait, so new
        // close requests cannot slip in while an earlier close drains.
        while connections.operationInProgress { try? await Task.sleep(nanoseconds: 20_000_000) }
        await connections.waitForCancelledRead()
        if !skipRewriteOperation {
            while requestRewrite.operationInProgress { try? await Task.sleep(nanoseconds: 20_000_000) }
        }
        await requestRewrite.waitForCancelledRead()
        if !skipCaptureOperation {
            while capture.operationInProgress { try? await Task.sleep(nanoseconds: 20_000_000) }
        }
        await capture.waitForCancelledRead()
    }
    func quit(completion: @escaping (Bool) -> Void) {
        guard !quitting else { return }
        quitting = true
        cancelRefresh()
        quitErrorMessage = nil
        Task {
            while busy || connections.operationInProgress || requestRewrite.operationInProgress || capture.operationInProgress { try? await Task.sleep(nanoseconds: 100_000_000) }
            await connections.waitForCancelledRead()
            await requestRewrite.waitForCancelledRead()
            await capture.stopOwnedSession(useGate: false)
            busy = true
            do {
                _ = try await runCommand(["quit"])
                stopTraffic(); stopSubscriptionWatch(); refreshTask?.cancel()
                busy = false
                completion(true)
            } catch {
                quitErrorMessage = L10n.format("app.ui.quit_failed", String(describing: error.localizedDescription))
                busy = false; quitting = false
                completion(false)
            }
        }
    }
    func setAppLogin(_ enabled: Bool) {
        guard !quitting else { return }
        do {
            if enabled { try SMAppService.mainApp.register() }
            else { try SMAppService.mainApp.unregister() }
            errorMessage = nil
        } catch { errorMessage = L10n.format("app.ui.app_login_setting_failed", String(describing: error.localizedDescription)) }
        notify()
    }
    private func cancelRefresh() {
        settingsGeneration += 1
        refreshWorker?.cancel()
        pendingRefresh = []
        pendingForced = false
    }
    func pauseForSleep() { sleeping = true; cancelRefresh(); stopTraffic(); stopSubscriptionWatch() }
    func resumeAfterWake() {
        sleeping = false
        startSubscriptionWatch()
        Task { await refreshMenuData() }
    }
    func ensureFreshTraffic() {
        guard state == .running, !sleeping, trafficTask != nil else { return }
        if Date().timeIntervalSince(max(lastSample, streamStartedAt)) > 8 {
            stopTraffic()
            startTraffic()
        }
    }
    private func startTraffic() {
        trafficGeneration += 1
        let generation = trafficGeneration
        streamStartedAt = Date()
        trafficTask = Task {
            while !Task.isCancelled && state == .running && generation == trafficGeneration {
                do {
                    let stream = try cli.trafficStream { [weak self] value in
                        Task { @MainActor in
                            guard let self, self.state == .running, generation == self.trafficGeneration else { return }
                            self.uploadBPS = value["upload_bps"]?.int64
                            self.downloadBPS = value["download_bps"]?.int64
                            self.lastSample = Date(); self.onTrafficChange?()
                            if self.errorOrigin == .traffic { self.errorMessage = nil }
                        }
                    }
                    if generation != trafficGeneration { stream.stop(); break }
                    trafficStream = stream
                    try await stream.wait()
                } catch {
                    if !Task.isCancelled && state == .running { errorMessage = L10n.format("app.ui.traffic_unavailable_2", String(describing: error.localizedDescription)); errorOrigin = .traffic }
                }
                if generation == trafficGeneration {
                    trafficStream = nil; uploadBPS = nil; downloadBPS = nil; onTrafficChange?()
                }
                if !Task.isCancelled { try? await Task.sleep(nanoseconds: 2_000_000_000) }
            }
            if generation == trafficGeneration { trafficTask = nil }
        }
    }
    private func stopTraffic() {
        trafficGeneration += 1
        trafficTask?.cancel(); trafficTask = nil
        trafficStream?.stop(); trafficStream = nil
        uploadBPS = nil; downloadBPS = nil; onTrafficChange?()
    }
    private func startSubscriptionWatch() {
        guard !sleeping, !quitting, subscriptionWatchTask == nil else { return }
        watchGeneration += 1
        let generation = watchGeneration
        do {
            let watch = try cli.subscriptionWatch(); subscriptionWatch = watch
            subscriptionWatchTask = Task { [weak self] in
                guard let self else { return }
                let result = await watch.wait()
                guard generation == self.watchGeneration, self.subscriptionWatch === watch else { return }
                self.subscriptionWatch = nil; self.subscriptionWatchTask = nil
                switch result {
                case .stopped: return
                case .duplicate:
                    self.watchRetry = 0
                    await self.requestRefresh(.subscriptions)
                    // A login service may own the host. Recheck later so a vanished host is replaced.
                    try? await Task.sleep(nanoseconds: self.watchDelay(self.watchRetry, true))
                case .failed(let error):
                    self.errorMessage = L10n.format("app.ui.subscription_scheduler_unavailable", String(describing: error.localizedDescription))
                    self.watchRetry = min(self.watchRetry + 1, 5)
                    try? await Task.sleep(nanoseconds: self.watchDelay(self.watchRetry, false))
                }
                if generation == self.watchGeneration && !self.sleeping && !self.quitting {
                    self.subscriptionWatchTask = nil
                    self.startSubscriptionWatch()
                }
            }
        } catch {
            errorMessage = L10n.format("app.ui.subscription_scheduler_unavailable", String(describing: error.localizedDescription))
            watchRetry = min(watchRetry + 1, 5)
            let delay = watchDelay(watchRetry, false)
            subscriptionWatchTask = Task { [weak self] in
                try? await Task.sleep(nanoseconds: delay)
                guard let self, generation == self.watchGeneration else { return }
                self.subscriptionWatchTask = nil
                self.startSubscriptionWatch()
            }
        }
    }
    private func stopSubscriptionWatch() {
        watchGeneration += 1
        subscriptionWatchTask?.cancel(); subscriptionWatchTask = nil
        subscriptionWatch?.stop(); subscriptionWatch = nil
    }
}
