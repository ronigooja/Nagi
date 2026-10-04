import SwiftUI

@main struct NagiApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("Nagi") {
            ContentView(model: model)
                .frame(minWidth: 760, minHeight: 520)
                .task { await model.pollStatus() }
        }
    }
}

private enum Page: String, CaseIterable, Identifiable {
    case dashboard = "Dashboard"
    case proxies = "Proxies"
    case subscriptions = "Subscriptions"
    case settings = "Settings"
    var id: String { rawValue }
    var icon: String {
        switch self {
        case .dashboard: return "speedometer"
        case .proxies: return "point.3.connected.trianglepath.dotted"
        case .subscriptions: return "arrow.triangle.2.circlepath"
        case .settings: return "gearshape"
        }
    }
}

private struct ContentView: View {
    @ObservedObject var model: AppModel
    @State private var page: Page? = .dashboard

    var body: some View {
        NavigationSplitView {
            List(Page.allCases, selection: $page) { item in
                Label(item.rawValue, systemImage: item.icon).tag(item)
            }
            .navigationTitle("Nagi")
            .listStyle(.sidebar)
        } detail: {
            VStack(spacing: 0) {
                if let error = model.errorMessage {
                    HStack(alignment: .top) {
                        Image(systemName: "exclamationmark.triangle.fill")
                        Text(error).frame(maxWidth: .infinity, alignment: .leading)
                        Button("Dismiss") { model.errorMessage = nil }
                    }
                    .padding(10)
                    .background(.orange.opacity(0.18))
                }
                switch page ?? .dashboard {
                case .dashboard: DashboardView(model: model)
                case .proxies: ProxiesView(model: model)
                case .subscriptions: SubscriptionsView(model: model)
                case .settings: SettingsView(model: model)
                }
            }
            .task(id: page) {
                switch page ?? .dashboard {
                case .dashboard: await model.refreshDashboard()
                case .proxies: await model.refreshProxies()
                case .subscriptions: await model.refreshSubscriptions()
                case .settings: break
                }
            }
        }
    }
}

private struct DashboardView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                HStack {
                    Text("Dashboard").font(.largeTitle.bold())
                    Spacer()
                    Button("Refresh") { Task { await model.refreshDashboard() } }
                }
                GroupBox("Runtime") {
                    VStack(alignment: .leading, spacing: 12) {
                        Label(model.running ? "Running" : "Stopped", systemImage: model.running ? "checkmark.circle.fill" : "stop.circle")
                            .foregroundColor(model.running ? .green : .secondary)
                        if let pid = model.pid { LabeledContent("PID", value: String(pid)) }
                        if let version = model.version { LabeledContent("mihomo", value: version) }
                        if let port = model.mixedPort { LabeledContent("Mixed port", value: String(port)) }
                        HStack {
                            Button("Start") { Task { await model.start() } }.disabled(model.busy || model.running)
                            Button("Stop") { Task { await model.stop() } }.disabled(model.busy || !model.running)
                            Button("Restart") { Task { await model.restart() } }.disabled(model.busy || !model.running)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }
                GroupBox("Profile") {
                    HStack {
                        Text(model.currentProfile ?? "No profile selected")
                        Spacer()
                        Menu("Switch profile") {
                            ForEach(model.profiles, id: \.self) { profile in
                                Button(profile) { Task { await model.useProfile(profile) } }
                            }
                        }.disabled(model.busy || model.profiles.isEmpty)
                    }.padding(8)
                }
                GroupBox("Connections") {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text("Active connections: \(model.connections.count)")
                            Spacer()
                            Button("Refresh") { Task { await model.refreshConnections() } }
                        }
                        ForEach(model.connections.prefix(20)) { connection in
                            HStack {
                                Text(connection.host).lineLimit(1)
                                Spacer()
                                Text(connection.network).foregroundStyle(.secondary)
                            }
                        }
                    }.padding(8)
                }
                GroupBox("Recent logs") {
                    VStack(alignment: .leading, spacing: 4) {
                        if model.logs.isEmpty { Text("No logs available").foregroundStyle(.secondary) }
                        ForEach(model.logs.suffix(50).indices, id: \.self) { index in
                            Text(model.logs[index]).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }
            }.padding(24)
        }
    }
}

private struct ProxiesView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Proxies").font(.largeTitle.bold())
                Spacer()
                Button("Refresh") { Task { await model.refreshProxies() } }
            }
            if model.groups.isEmpty { emptyState("No proxy groups", "Start mihomo and refresh to see proxy groups.") }
            else {
                List(model.groups) { group in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(group.name).font(.headline)
                            Text("Current: \(group.selected)").foregroundStyle(.secondary)
                        }
                        Spacer()
                        Menu("Select node") {
                            ForEach(group.nodes, id: \.self) { node in
                                Button(node) { Task { await model.select(node, in: group.name) } }
                            }
                        }.disabled(model.busy || group.nodes.isEmpty)
                    }.padding(.vertical, 5)
                }
            }
        }.padding(24)
    }
}

private struct SubscriptionsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Subscriptions").font(.largeTitle.bold())
                Spacer()
                Button("Refresh list") { Task { await model.refreshSubscriptions() } }
            }
            if model.subscriptions.isEmpty { emptyState("No subscriptions", "Add subscriptions with the Nagi CLI.") }
            else {
                List(model.subscriptions) { subscription in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(subscription.name).font(.headline)
                            if let updated = subscription.updatedAt { Text("Updated: \(updated)").foregroundStyle(.secondary) }
                        }
                        Spacer()
                        Button("Update") { Task { await model.update(subscription.name) } }.disabled(model.busy)
                    }.padding(.vertical, 5)
                }
            }
        }.padding(24)
    }
}

private func emptyState(_ title: String, _ description: String) -> some View {
    VStack(spacing: 8) {
        Text(title).font(.title2)
        Text(description).foregroundStyle(.secondary)
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
}

private struct SettingsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        Form {
            Section("CLI") {
                LabeledContent("Executable", value: NagiCLIClient.executable.path)
                Text("The app invokes this executable with --json for all operations.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Section("Status") {
                Picker("Refresh interval", selection: $model.statusPollSeconds) {
                    Text("2 seconds").tag(2)
                    Text("5 seconds").tag(5)
                    Text("10 seconds").tag(10)
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Settings")
    }
}
