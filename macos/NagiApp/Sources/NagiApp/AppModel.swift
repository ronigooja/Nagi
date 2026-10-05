import AppKit
import Foundation
import ServiceManagement

struct ProxyGroup {
    let name: String
    let kind: String
    let selected: String
    let nodes: [String]
    var selectable: Bool { kind.lowercased() == "selector" }
}
struct Subscription { let name: String; let updatedAt: String? }
enum TrafficDisplay: String, CaseIterable {
    case both, icon
    var title: String {
        switch self {
        case .both: return "Upload and download"
        case .icon: return "Icon only"
        }
    }
}
enum EngineState { case starting, running, stopped, unavailable }

@MainActor final class AppModel {
    var onChange: (() -> Void)?
    private let cli = NagiCLIClient()
    private var refreshTask: Task<Void, Never>?
    private var trafficTask: Task<Void, Never>?
    private var trafficStream: TrafficStream?
    private var trafficGeneration = 0
    private var lastSample = Date.distantPast
    private var streamStartedAt = Date.distantPast
    private var sleeping = false
    private var quitting = false
    private(set) var busy = false { didSet { notify() } }
    private(set) var state: EngineState = .starting { didSet { notify() } }
    private(set) var errorMessage: String? { didSet { notify() } }
    private(set) var quitErrorMessage: String? { didSet { notify() } }
    private(set) var notice: String? { didSet { notify() } }
    private(set) var profile: String?
    private(set) var profiles: [String] = []
    private(set) var groups: [ProxyGroup] = []
    private(set) var subscriptions: [Subscription] = []
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
        return "↑ \(upload)\n↓ \(download)"
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
        do {
            let value = try await cli.run(["status"])
            state = value["running"]?.bool == true ? .running : .stopped
            profile = value["profile"]?.string
            if errorMessage?.hasPrefix("Status unavailable:") == true { errorMessage = nil }
            if state == .running { if trafficTask == nil && !sleeping { startTraffic() } }
            else { stopTraffic() }
        } catch {
            state = .unavailable; stopTraffic()
            errorMessage = "Status unavailable: \(error.localizedDescription)"
        }
        await refreshMenuData()
    }
    func refreshMenuData() async {
        do {
            let value = try await cli.run(["profile", "list"])
            profiles = value["profiles"]?.array.compactMap { $0.string ?? $0["name"]?.string } ?? []
            profile = value["current"]?.string
            if let problem = value["current_error"]?.string { errorMessage = "Profile selection unavailable: \(problem)" }
        } catch { errorMessage = error.localizedDescription }
        do {
            let value = try await cli.run(["subscription", "list"])
            subscriptions = value["subscriptions"]?.array.compactMap { item in
                guard let name = item["name"]?.string else { return nil }
                return Subscription(name: name, updatedAt: item["updated_at"]?.string)
            } ?? []
        } catch { errorMessage = error.localizedDescription }
        do {
            let service = try await cli.run(["service", "status"])
            serviceInstalled = service["installed"]?.bool
        } catch { serviceInstalled = nil }
        if state == .running {
            do {
                let value = try await cli.run(["proxy", "groups"])
                groups = value["groups"]?.array.compactMap { item in
                    guard let name = item["name"]?.string else { return nil }
                    return ProxyGroup(name: name, kind: item["type"]?.string ?? "", selected: item["now"]?.string ?? "",
                                      nodes: item["all"]?.array.compactMap(\.string) ?? [])
                } ?? []
            } catch { groups = []; errorMessage = error.localizedDescription }
            do { mode = try await cli.run(["mode"])["mode"]?.string }
            catch { mode = nil; errorMessage = error.localizedDescription }
            do { systemProxy = try await cli.run(["system-proxy", "status"])["enabled"]?.bool }
            catch { systemProxy = nil; errorMessage = error.localizedDescription }
            do {
                let value = try await cli.run(["tun", "status"])
                tun = value["enabled"]?.bool; tunAdapter = value["adapter_status"]?.string
            } catch { tun = nil; tunAdapter = nil; errorMessage = error.localizedDescription }
        } else { groups = []; mode = nil; systemProxy = nil; tun = nil; tunAdapter = nil }
        notify()
    }
    func perform(_ arguments: [String], success: String? = nil) {
        guard !busy && !quitting else { return }
        busy = true
        Task {
            defer { busy = false }
            do { _ = try await cli.run(arguments); errorMessage = nil; notice = success; await refresh() }
            catch { errorMessage = error.localizedDescription }
        }
    }
    func quit(completion: @escaping (Bool) -> Void) {
        guard !quitting else { return }
        quitting = true
        quitErrorMessage = nil
        Task {
            while busy { try? await Task.sleep(nanoseconds: 100_000_000) }
            busy = true
            do {
                _ = try await cli.run(["quit"])
                stopTraffic(); refreshTask?.cancel()
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
    func pauseForSleep() { sleeping = true; stopTraffic() }
    func resumeAfterWake() {
        sleeping = false
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
}
