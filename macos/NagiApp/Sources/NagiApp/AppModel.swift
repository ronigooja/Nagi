import AppKit
import Foundation
import ServiceManagement

struct ProxyGroup {
    let name: String
    let kind: String
    let selected: String
    let fixed: String?
    let selectedStatus: String?
    let nodes: [String]
    let hidden: Bool
    let icon: String?
    var selectable: Bool {
        ["selector", "urltest", "fallback"].contains(kind.lowercased())
    }
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
struct ProxyProvider { let name: String; let type: String; let vehicleType: String; let nodes: [String] }
enum TrafficDisplay: String, CaseIterable {
    case both, icon
    var title: String {
        switch self {
        case .both: return "Traffic rates"
        case .icon: return "Icon only"
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
    }

    var onChange: (() -> Void)?
    private let cli = NagiCLIClient()
    private var refreshTask: Task<Void, Never>?
    private var trafficTask: Task<Void, Never>?
    private var trafficStream: TrafficStream?
    private var subscriptionWatch: SubscriptionWatch?
    private var subscriptionWatchTask: Task<Void, Never>?
    private var watchGeneration = 0
    private var watchRetry = 0
    private var trafficGeneration = 0
    private var settingsGeneration = 0
    private var lastSample = Date.distantPast
    private var streamStartedAt = Date.distantPast
    private var sleeping = false
    private var quitting = false
    private(set) var busy = false { didSet { notify() } }
    private(set) var applyingSetting = false { didSet { notify() } }
    private(set) var state: EngineState = .starting { didSet { notify() } }
    private(set) var errorMessage: String? { didSet { notify() } }
    private(set) var quitErrorMessage: String? { didSet { notify() } }
    private(set) var notice: String? { didSet { notify() } }
    private(set) var profile: String?
    private(set) var profiles: [String] = []
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
              let up = uploadBPS, let down = downloadBPS else { return "traffic unavailable" }
        return "upload \(Self.rate(up).trimmingCharacters(in: .whitespaces)), download \(Self.rate(down).trimmingCharacters(in: .whitespaces))"
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
            await startEngine()
            startSubscriptionWatch()
            busy = false
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(nanoseconds: 5_000_000_000)
            }
        }
    }
    func retryStart() {
        guard !busy && !quitting else { return }
        busy = true
        Task { await startEngine(); await refresh(); busy = false }
    }
    private func startEngine() async {
        state = .starting
        do { _ = try await cli.run(["start"]); errorMessage = nil }
        catch CLIError.command(let code, _) where code == "already_running" { errorMessage = nil }
        catch { state = .unavailable; errorMessage = "Start failed: \(error.localizedDescription)" }
    }
    func refresh() async {
        let generation = settingsGeneration
        do {
            let value = try await cli.run(["status"])
            guard generation == settingsGeneration else { return }
            state = value["running"]?.bool == true ? .running : .stopped
            profile = value["profile"]?.string
            if errorMessage?.hasPrefix("Status unavailable:") == true { errorMessage = nil }
            if state == .running { if trafficTask == nil && !sleeping { startTraffic() } }
            else { stopTraffic() }
        } catch {
            guard generation == settingsGeneration else { return }
            state = .unavailable; stopTraffic()
            errorMessage = "Status unavailable: \(error.localizedDescription)"
        }
        await refreshMenuData()
    }
    @discardableResult func refreshMenuData() async -> Bool {
        let generation = settingsGeneration
        await refreshProfiles()
        guard generation == settingsGeneration else { return false }
        let subscriptionsRefreshed = await refreshSubscriptions()
        guard generation == settingsGeneration else { return false }
        do {
            let service = try await cli.run(["service", "status"])
            guard generation == settingsGeneration else { return false }
            serviceInstalled = service["installed"]?.bool
        } catch {
            guard generation == settingsGeneration else { return false }
            serviceInstalled = nil
        }
        await refreshRuntimeSettings()
        guard generation == settingsGeneration else { return false }
        if state == .running {
            do {
                let value = try await cli.run(["system-proxy", "status"])
                guard generation == settingsGeneration else { return false }
                systemProxy = value["enabled"]?.bool
            } catch {
                guard generation == settingsGeneration else { return false }
                systemProxy = nil; errorMessage = error.localizedDescription
            }
        } else { systemProxy = nil }
        notify()
        return subscriptionsRefreshed
    }
    private func refreshProfiles() async {
        let generation = settingsGeneration
        do {
            let value = try await cli.run(["profile", "list"])
            guard generation == settingsGeneration else { return }
            profiles = value["profiles"]?.array.compactMap { $0.string ?? $0["name"]?.string } ?? []
            profile = value["current"]?.string
            if let problem = value["current_error"]?.string { errorMessage = "Profile selection unavailable: \(problem)" }
        } catch {
            guard generation == settingsGeneration else { return }
            errorMessage = error.localizedDescription
        }
    }
    @discardableResult private func refreshSubscriptions() async -> Bool {
        let generation = settingsGeneration
        do {
            let value = try await cli.run(["subscription", "list"])
            guard generation == settingsGeneration else { return false }
            schedulerRunning = value["scheduler_running"]?.bool == true
            subscriptions = value["subscriptions"]?.array.compactMap { item in
                guard let name = item["name"]?.string else { return nil }
                return Subscription(name: name,
                    keyConfigured: item["key_configured"]?.bool == true,
                    scheduleSeconds: max(0, item["schedule_seconds"]?.int64 ?? 3600),
                    schedulerRunning: schedulerRunning,
                    nextDue: item["next_due"]?.string,
                    pauseReason: item["pause_reason"]?.string,
                    lastFailureStage: item["last_failure_stage"]?.string,
                    lastFailureReason: item["last_failure_reason"]?.string,
                    expiresAt: item["expires_at"]?.string,
                    upload: item["upload"]?.int64 ?? 0,
                    download: item["download"]?.int64 ?? 0,
                    total: item["total"]?.int64 ?? 0)
            } ?? []
            return true
        } catch {
            guard generation == settingsGeneration else { return false }
            errorMessage = error.localizedDescription; return false
        }
    }
    private func refreshRuntimeSettings() async {
        let generation = settingsGeneration
        if state == .running {
            do {
                let value = try await cli.run(["proxy", "groups"])
                guard generation == settingsGeneration else { return }
                groups = value["groups"]?.array.compactMap { item in
                    guard let name = item["name"]?.string else { return nil }
                    return ProxyGroup(name: name, kind: item["type"]?.string ?? "", selected: item["now"]?.string ?? "",
                                      fixed: item["fixed"]?.string, selectedStatus: item["selected_status"]?.string,
                                      nodes: item["all"]?.array.compactMap(\.string) ?? [],
                                      hidden: item["hidden"]?.bool == true, icon: item["icon"]?.string)
                } ?? []
            } catch {
                guard generation == settingsGeneration else { return }
                groups = []; errorMessage = error.localizedDescription
            }
            do {
                let value = try await cli.run(["mode"])
                guard generation == settingsGeneration else { return }
                mode = value["mode"]?.string
            } catch {
                guard generation == settingsGeneration else { return }
                mode = nil; errorMessage = error.localizedDescription
            }
            do {
                let value = try await cli.run(["tun", "status"])
                guard generation == settingsGeneration else { return }
                tun = value["enabled"]?.bool; tunAdapter = value["adapter_status"]?.string
            } catch {
                guard generation == settingsGeneration else { return }
                tun = nil; tunAdapter = nil; errorMessage = error.localizedDescription
            }
            do {
                let value = try await cli.run(["proxy", "providers"])
                guard generation == settingsGeneration else { return }
                providersUnavailable = false
                providers = value["providers"]?.array.compactMap { item in
                    guard let name = item["name"]?.string else { return nil }
                    return ProxyProvider(name: name, type: item["type"]?.string ?? "", vehicleType: item["vehicle_type"]?.string ?? "", nodes: item["nodes"]?.array.compactMap(\.string) ?? [])
                } ?? []
            } catch {
                guard generation == settingsGeneration else { return }
                providers = []
                providersUnavailable = true
                errorMessage = "Proxy providers unavailable: \(error.localizedDescription)"
            }
        } else { groups = []; providers = []; providersUnavailable = false; mode = nil; tun = nil; tunAdapter = nil }
    }
    func perform(_ arguments: [String]) {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            do {
                let result = try await cli.run(arguments)
                settingsGeneration += 1
                errorMessage = nil
                if arguments.count >= 2 {
                    switch (arguments[0], arguments[1]) {
                    case ("system-proxy", _): systemProxy = result["enabled"]?.bool
                    case ("tun", _):
                        tun = result["enabled"]?.bool
                        tunAdapter = result["adapter_status"]?.string
                    case ("mode", _): mode = result["mode"]?.string
                    case ("proxy", "select"):
                        if arguments.count == 4 {
                            groups = groups.map { group in
                                guard group.name == arguments[2] else { return group }
                                return ProxyGroup(name: group.name, kind: group.kind, selected: arguments[3], fixed: group.fixed, selectedStatus: group.selectedStatus, nodes: group.nodes, hidden: group.hidden, icon: group.icon)
                            }
                        }
                    case ("service", "install"): serviceInstalled = true
                    case ("service", "uninstall"): serviceInstalled = false
                    case ("profile", "use"):
                        profile = result["current"]?.string
                        await refreshRuntimeSettings()
                    case ("profile", "override"):
                        await refreshRuntimeSettings()
                    default: await refreshMenuData()
                    }
                }
                notify()
            }
            catch {
                settingsGeneration += 1
                errorMessage = error.localizedDescription
            }
        }
    }

    func switchToDefaultProfile() {
        perform(ProfileCommands.useDefault)
    }

    func applyDefaultOverrideToCurrentProfile() {
        guard let profile, !profile.isEmpty else {
            errorMessage = "No current profile is selected."
            return
        }
        perform(ProfileCommands.applyDefaultOverride(for: profile))
    }
    func updateAndApplySubscription(_ name: String) {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            do {
                _ = try await cli.run(["subscription", "update", name])
            } catch {
                settingsGeneration += 1
                errorMessage = "Subscription update failed: \(error.localizedDescription)"
                return
            }
            do {
                let result = try await cli.run(["subscription", "apply", name])
                settingsGeneration += 1
                errorMessage = nil
                notice = "Subscription updated and applied."
                profile = result["profile"]?.string
                await refreshProfiles()
                await refreshRuntimeSettings()
                notify()
            } catch {
                settingsGeneration += 1
                errorMessage = "Subscription updated, but apply failed: \(error.localizedDescription)"
            }
        }
    }
    func addAndDownloadSubscription(_ name: String, url: String) {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            do {
                _ = try await cli.run(["subscription", "add", name, url])
                settingsGeneration += 1
                subscriptions.append(Subscription(name: name, keyConfigured: false, scheduleSeconds: 3600, schedulerRunning: false, nextDue: nil, pauseReason: nil, lastFailureStage: nil, lastFailureReason: nil, expiresAt: nil, upload: 0, download: 0, total: 0))
                subscriptions.sort { $0.name < $1.name }
            } catch {
                settingsGeneration += 1
                errorMessage = "Subscription add failed: \(error.localizedDescription)"
                return
            }
            do {
                _ = try await cli.run(["subscription", "update", name])
                settingsGeneration += 1
                errorMessage = nil
                notice = "Subscription saved and downloaded. Apply it to activate the cache."
            } catch {
                settingsGeneration += 1
                errorMessage = "Subscription saved, but download failed: \(error.localizedDescription). Retry the download from the CLI."
            }
        }
    }
    func applySubscription(_ name: String) {
        guard !busy && !quitting else { return }
        settingsGeneration += 1
        busy = true
        applyingSetting = true
        notice = nil
        Task {
            defer { applyingSetting = false; busy = false }
            do {
                let result = try await cli.run(["subscription", "apply", name])
                settingsGeneration += 1
                errorMessage = nil
                profile = result["profile"]?.string
                await refreshProfiles()
                await refreshRuntimeSettings()
                notice = "Cached subscription applied."
                notify()
            } catch {
                settingsGeneration += 1
                errorMessage = "Subscription apply failed: \(error.localizedDescription)"
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
            do {
                _ = try await cli.run(["subscription", "remove", name])
                settingsGeneration += 1
                errorMessage = nil
                subscriptions.removeAll { $0.name == name }
                notice = "Subscription removed."
            } catch {
                settingsGeneration += 1
                let listRefreshed = await refreshSubscriptions()
                let detail = error.localizedDescription
                if !listRefreshed || subscriptions.contains(where: { $0.name == name }) {
                    errorMessage = "Subscription removal failed: \(detail)"
                } else {
                    errorMessage = "Subscription record removed, but cache cleanup failed: \(detail)"
                }
            }
        }
    }
    func scheduleSubscription(_ name: String, seconds: Int64?) {
        var args = ["subscription", "schedule", name, "off"]
        if let seconds { args[3] = String(seconds) }
        perform(args)
    }
    func previewSubscription(_ name: String) async -> String {
        do {
            let value = try await cli.run(["subscription", "preview", name])
            let added = value["added"]?.array.compactMap(\.string) ?? []
            let removed = value["removed"]?.array.compactMap(\.string) ?? []
            let changed = value["changed"]?.array.compactMap(\.string) ?? []
            var lines = ["Compared cached subscription with profile \(name):",
                         "Added (\(added.count)): \(added.joined(separator: ", "))",
                         "Removed (\(removed.count)): \(removed.joined(separator: ", "))",
                         "Changed (\(changed.count)): \(changed.joined(separator: ", "))"]
            for change in value["changes"]?.array ?? [] {
                lines.append("\(change["path"]?.string ?? "unknown path") · \(change["kind"]?.string ?? "unknown change") · \(change["behavior"]?.string ?? "")")
            }
            if value["remote_nodes_compared"]?.bool == false { lines.append("Remote provider nodes were not compared.") }
            return lines.joined(separator: "\n")
        } catch { return "Preview unavailable: \(error.localizedDescription)\nApply remains available if the cached subscription is valid." }
    }
    func clearSubscriptionKey(_ name: String) { perform(["subscription", "key", name, "clear"]) }
    func setSubscriptionKey(_ name: String, file: String) { perform(["subscription", "key", name, file]) }
    func quit(completion: @escaping (Bool) -> Void) {
        guard !quitting else { return }
        quitting = true
        quitErrorMessage = nil
        Task {
            while busy { try? await Task.sleep(nanoseconds: 100_000_000) }
            busy = true
            do {
                _ = try await cli.run(["quit"])
                stopTraffic(); stopSubscriptionWatch(); refreshTask?.cancel()
                completion(true)
            } catch {
                quitErrorMessage = "Quit failed: \(error.localizedDescription)"
                completion(false)
            }
            busy = false; quitting = false
        }
    }
    func setAppLogin(_ enabled: Bool) {
        guard !quitting else { return }
        do {
            if enabled { try SMAppService.mainApp.register() }
            else { try SMAppService.mainApp.unregister() }
            errorMessage = nil
        } catch { errorMessage = "App login setting failed: \(error.localizedDescription)" }
        notify()
    }
    func pauseForSleep() { sleeping = true; stopTraffic(); stopSubscriptionWatch() }
    func resumeAfterWake() {
        sleeping = false
        startSubscriptionWatch()
        Task { await refresh() }
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
                            self.lastSample = Date(); self.notify()
                            if self.errorMessage?.hasPrefix("Traffic unavailable:") == true { self.errorMessage = nil }
                        }
                    }
                    if generation != trafficGeneration { stream.stop(); break }
                    trafficStream = stream
                    try await stream.wait()
                } catch {
                    if !Task.isCancelled && state == .running { errorMessage = "Traffic unavailable: \(error.localizedDescription)" }
                }
                if generation == trafficGeneration {
                    trafficStream = nil; uploadBPS = nil; downloadBPS = nil; notify()
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
        uploadBPS = nil; downloadBPS = nil; notify()
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
                    await self.refreshSubscriptions()
                    // A login service may own the host. Recheck later so a vanished host is replaced.
                    try? await Task.sleep(nanoseconds: 15_000_000_000)
                case .failed(let error):
                    self.errorMessage = "Subscription scheduler unavailable: \(error.localizedDescription)"
                    self.watchRetry = min(self.watchRetry + 1, 5)
                    let delay = UInt64(1 << self.watchRetry) * 1_000_000_000
                    try? await Task.sleep(nanoseconds: delay)
                }
                if generation == self.watchGeneration && !self.sleeping && !self.quitting { self.startSubscriptionWatch() }
            }
        } catch {
            errorMessage = "Subscription scheduler unavailable: \(error.localizedDescription)"
            watchRetry = min(watchRetry + 1, 5)
            let delay = UInt64(1 << watchRetry) * 1_000_000_000
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
