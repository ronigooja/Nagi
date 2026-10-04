import Foundation
import SwiftUI

struct ProxyGroup: Identifiable {
    let name: String
    let selected: String
    let nodes: [String]
    var id: String { name }
}

struct Subscription: Identifiable {
    let name: String
    let updatedAt: String?
    var id: String { name }
}

struct Connection: Identifiable {
    let id: String
    let host: String
    let network: String
    let uploaded: Int?
    let downloaded: Int?
}

@MainActor final class AppModel: ObservableObject {
    @Published var running = false
    @Published var pid: Int?
    @Published var version: String?
    @Published var mixedPort: Int?
    @Published var profiles: [String] = []
    @Published var currentProfile: String?
    @Published var groups: [ProxyGroup] = []
    @Published var subscriptions: [Subscription] = []
    @Published var connections: [Connection] = []
    @Published var logs: [String] = []
    @Published var errorMessage: String?
    @Published var busy = false
    @Published var statusPollSeconds: Int = UserDefaults.standard.integer(forKey: "statusPollSeconds") == 0
        ? 5 : UserDefaults.standard.integer(forKey: "statusPollSeconds") {
        didSet { UserDefaults.standard.set(statusPollSeconds, forKey: "statusPollSeconds") }
    }

    private let cli = NagiCLIClient()

    func pollStatus() async {
        while !Task.isCancelled {
            await refreshStatus()
            try? await Task.sleep(nanoseconds: UInt64(max(2, statusPollSeconds)) * 1_000_000_000)
        }
    }

    func refreshStatus() async {
        do {
            let value = try await cli.run(["status"])
            running = value["running"]?.bool ?? false
            pid = value["pid"]?.integer
            version = value["version"]?.string
            mixedPort = value["mixed_port"]?.integer
        } catch {
            running = false
            errorMessage = error.localizedDescription
        }
    }

    func refreshDashboard() async {
        await refreshStatus()
        do {
            let value = try await cli.run(["profile", "list"])
            profiles = value["profiles"]?.array.compactMap { $0.string ?? $0["name"]?.string } ?? []
            currentProfile = value["current"]?.string
        } catch { errorMessage = error.localizedDescription }
        do {
            let value = try await cli.run(["logs"])
            logs = value["lines"]?.array.compactMap(\.string) ?? []
        } catch { errorMessage = error.localizedDescription }
        if running { await refreshConnections() } else { connections = [] }
    }

    func refreshProxies() async {
        do {
            let value = try await cli.run(["proxy", "groups"])
            groups = value["groups"]?.array.compactMap { item in
                guard let name = item["name"]?.string else { return nil }
                return ProxyGroup(name: name, selected: item["now"]?.string ?? "", nodes: item["all"]?.array.compactMap(\.string) ?? [])
            } ?? []
            errorMessage = nil
        } catch { errorMessage = error.localizedDescription }
    }

    func refreshSubscriptions() async {
        do {
            let value = try await cli.run(["subscription", "list"])
            subscriptions = value["subscriptions"]?.array.compactMap { item in
                guard let name = item["name"]?.string else { return nil }
                return Subscription(name: name, updatedAt: item["updated_at"]?.string)
            } ?? []
            errorMessage = nil
        } catch { errorMessage = error.localizedDescription }
    }

    func refreshConnections() async {
        do {
            let value = try await cli.run(["connections", "list"])
            connections = value["connections"]?.array.enumerated().map { index, item in
                let metadata = item["metadata"]
                return Connection(
                    id: item["id"]?.string ?? String(index),
                    host: metadata?["host"]?.string ?? item["host"]?.string ?? "Unknown",
                    network: metadata?["network"]?.string ?? item["network"]?.string ?? "",
                    uploaded: item["upload"]?.integer,
                    downloaded: item["download"]?.integer
                )
            } ?? []
            errorMessage = nil
        } catch { errorMessage = error.localizedDescription }
    }

    func start() async { await perform(["start"]) { await self.refreshDashboard() } }
    func stop() async { await perform(["stop"]) { await self.refreshDashboard() } }
    func restart() async { await perform(["restart"]) { await self.refreshDashboard() } }
    func useProfile(_ name: String) async { await perform(["profile", "use", name]) { await self.refreshDashboard() } }
    func select(_ node: String, in group: String) async { await perform(["proxy", "select", group, node]) { await self.refreshProxies() } }
    func update(_ name: String) async { await perform(["subscription", "update", name]) { await self.refreshSubscriptions() } }

    private func perform(_ arguments: [String], then refresh: @escaping @MainActor () async -> Void) async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        do {
            _ = try await cli.run(arguments)
            errorMessage = nil
            await refresh()
        } catch { errorMessage = error.localizedDescription }
    }
}
