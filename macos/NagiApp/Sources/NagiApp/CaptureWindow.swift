import AppKit
import SwiftUI

@MainActor final class CapturePresentation: ObservableObject {
    let model: CaptureModel
    @Published var revision = 0
    init(_ model: CaptureModel) { self.model = model; model.onChange = { [weak self] in self?.revision &+= 1 } }
}
@MainActor final class CaptureWindowController: NSWindowController, NSWindowDelegate {
    let model: CaptureModel
    private var observers: [NSObjectProtocol] = []
    private var draftWindow: CaptureDraftWindowController?
    init(model: CaptureModel) {
        self.model = model
        let window = ConnectionsWindow(contentRect: NSRect(x: 0, y: 0, width: 1240, height: 820), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = "Nagi Capture"; window.minSize = NSSize(width: 980, height: 650)
        window.isReleasedWhenClosed = false; window.delegate = self
        window.onVisibilityChange = { [weak self] in self?.updateVisibility() }
        window.contentView = NSHostingView(rootView: CaptureView(presentation: CapturePresentation(model)))
        window.center()
        model.onCreateRule = { [weak self] context in self?.createRule(context) }
        for name in [NSApplication.didHideNotification, NSApplication.didUnhideNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in Task { @MainActor in self?.updateVisibility() } })
        }
    }
    private func createRule(_ context: CaptureSelectionContext) {
        if let draftWindow { draftWindow.present(); return }
        guard let run = model.draftRun else { return }
        let controller = CaptureDraftWindowController(model: CaptureDraftModel(context: context, run: run))
        draftWindow = controller
        controller.onClose = { [weak self] in self?.draftWindow = nil }
        controller.present()
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    deinit { observers.forEach { NotificationCenter.default.removeObserver($0) } }
    func present() {
        guard let window else { return }
        if window.isMiniaturized || (window as? ConnectionsWindow)?.minimizing == true { window.deminiaturize(nil) }
        showWindow(nil); NSApp.activate(ignoringOtherApps: true); window.makeKeyAndOrderFront(nil); updateVisibility()
    }
    private func updateVisibility() { model.setVisible((window as? ConnectionsWindow)?.readingPaused == false && window?.isVisible == true && window?.isMiniaturized == false && !NSApp.isHidden) }
    func windowWillClose(_ notification: Notification) { model.closeWindow() }
    func windowWillMiniaturize(_ notification: Notification) { (window as? ConnectionsWindow)?.beginMiniaturization() }
    func windowDidMiniaturize(_ notification: Notification) { updateVisibility() }
    func windowDidDeminiaturize(_ notification: Notification) { (window as? ConnectionsWindow)?.finishDeminiaturization(); updateVisibility() }
    func windowDidChangeOcclusionState(_ notification: Notification) { updateVisibility() }
}

@MainActor struct CaptureView: View {
    @ObservedObject var presentation: CapturePresentation
    @State private var domains = ""
    @State private var processes = ""
    @State private var apps = ""
    @State private var query = ""
    @State private var kind = ""
    @State private var domain = ""
    @State private var process = ""
    @State private var ascending = false
    @State private var bodyLimit = 1_048_576
    @State private var archiveFormat = "native"
    @State private var scopeExpanded = true
    @State private var jsonPointer = ""
    private var model: CaptureModel { presentation.model }
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            controls
            DisclosureGroup("Capture Scope", isExpanded: $scopeExpanded) { scope }
            HStack {
                TextField("Search original URLs, headers and bodies", text: $query)
                Picker("Kind", selection: $kind) { Text("All").tag(""); Text("HTTP").tag("http"); Text("WebSocket").tag("websocket") }.frame(width: 170)
                TextField("Domain", text: $domain).frame(width: 160)
                TextField("Process path", text: $process).frame(width: 180)
                Toggle("Oldest first", isOn: $ascending)
            }.onChange(of: query) { _ in filters() }.onChange(of: kind) { _ in filters() }
                .onChange(of: domain) { _ in filters() }.onChange(of: process) { _ in filters() }.onChange(of: ascending) { _ in filters() }
            HSplitView {
                recordList.frame(minWidth: 300, idealWidth: 420)
                details.frame(minWidth: 440)
            }
            HStack {
                Text(model.stale ? "Displayed capture may be stale." : "\(model.records.count) matching records")
                if let status = model.status { Text("\(status.bytes) bytes · \(status.dropped) dropped · \(status.truncated) truncated") }
                Spacer()
                if model.isReading || model.readingBody { ProgressView().controlSize(.small) }
            }.font(.caption).foregroundColor(.secondary)
            if let error = model.error { Text(error).foregroundColor(.red).textSelection(.enabled) }
            if let status = model.status, let recovery = status.recovery {
                Text("Recorder: \(status.recorderState ?? "unknown"). \(recovery)").font(.callout).foregroundColor(.orange).textSelection(.enabled)
            }
            if let notice = model.notice { Text(notice).font(.caption).foregroundColor(.secondary) }
        }.padding()
    }
    private var controls: some View {
        HStack {
            Text("Capture").font(.title2.bold())
            Text(model.status.map { "\($0.state.capitalized) · \($0.profile)" } ?? "No session").foregroundColor(model.status?.state == "recording" ? .red : .secondary)
            if model.status?.readOnly == true { Text("Imported · Read only").foregroundColor(.secondary) }
            Spacer()
            Button("Start") { Task { await model.start(scope: CaptureScope(domains: lines(domains), processPaths: lines(processes), appPaths: lines(apps)), bodyLimit: bodyLimit) } }
                .disabled(!model.canOperate || model.status?.canStartNewSession == false)
            Button(model.status?.state == "paused" ? "Resume" : "Pause") { Task { await model.mutate(model.status?.state == "paused" ? "resume" : "pause") } }
                .disabled(!model.canOperate || model.status?.canPauseOrResume != true)
            Button("Stop") { Task { await model.mutate("stop") } }.disabled(!model.canOperate || model.status?.canStop != true)
            Button("Clear") { Task { await model.mutate("clear") } }.disabled(!model.canOperate || model.status == nil || model.status?.readOnly == true)
            Button("Refresh") { model.refresh() }.disabled(!model.canOperate)
            Button("Import…") { importFile() }.disabled(!model.canOperate || model.status?.canStartNewSession == false)
            Menu("Export…") {
                Button("Native Archive…") { exportFile("native") }
                Button("HAR…") { exportFile("har") }
            }.disabled(!model.canOperate || model.status == nil)
        }
    }
    private var scope: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Capture shows complete URLs, credentials and bodies. Empty scope includes all applications. Domain, process and application restrictions combine.").font(.callout).foregroundColor(.secondary)
            HStack(alignment: .top) {
                scopeField("Domains (one pattern per line)", text: $domains)
                scopeField("Process paths (one per line)", text: $processes)
                scopeField("Application paths (one .app per line)", text: $apps)
            }
            HStack {
                Button("Find Applications") { model.discoverApplications() }
                Menu("Add Application") {
                    ForEach(model.applications) { app in
                        Button(app.appPath.isEmpty ? app.processPath : app.appPath) {
                            if app.appPath.isEmpty { processes += (processes.isEmpty ? "" : "\n") + app.processPath }
                            else { apps += (apps.isEmpty ? "" : "\n") + app.appPath }
                        }
                    }
                }.disabled(model.applications.isEmpty)
                Picker("Body limit", selection: $bodyLimit) { Text("1 MiB").tag(1_048_576); Text("4 MiB").tag(4_194_304); Text("8 MiB").tag(8_388_608) }.frame(width: 210)
                Text("Scope applies when starting a new session.").font(.caption).foregroundColor(.secondary)
            }
        }.padding(.vertical, 6)
    }
    private func scopeField(_ title: String, text: Binding<String>) -> some View {
        VStack(alignment: .leading) { Text(title).font(.caption); TextEditor(text: text).font(.system(.caption, design: .monospaced)).frame(height: 52).border(Color.secondary.opacity(0.3)) }
    }
    private var recordList: some View {
        List(selection: Binding(get: { model.selectedID }, set: { model.select($0) })) {
            ForEach(model.records) { record in
                VStack(alignment: .leading, spacing: 4) {
                    HStack { Text(record.kind == "websocket" ? "WS \(record.stage ?? "") #\(record.sequence ?? 0)" : record.original?.method ?? "HTTP").bold(); Spacer(); Text(record.outcome) }
                    Text(record.title.isEmpty ? record.id : record.title).lineLimit(2)
                    Text("\(captureMinute(record.started)) · \(record.identity.process) · \(record.durationMS) ms").font(.caption).foregroundColor(.secondary)
                    if record.parentID != nil { Text("Handshake: \(record.parentID!)").font(.caption2).foregroundColor(.secondary) }
                }.padding(.leading, record.parentID == nil ? 0 : 14).tag(record.id)
            }
        }.listStyle(.inset)
    }
    @ViewBuilder private var details: some View {
        if let record = model.selectedRecord {
            VStack(alignment: .leading, spacing: 8) {
                Text(record.title.isEmpty ? record.id : record.title).bold().textSelection(.enabled)
                    .contextMenu { Button("Copy URL") { copy(record.original?.url ?? "") }; Button("Create Rule from URL…") { model.createRule(field: CaptureFieldSelection(kind: "url", value: record.original?.url, stage: record.stage)) } }
                Text("\(record.identity.processPath)\n\(record.identity.source) → \(record.identity.destination)\nRules: \((record.ruleIDs ?? []).joined(separator: ", "))\nOutcome: \(record.outcome)\(record.error.map { " · " + $0 } ?? "")\(record.closeCode.map { " · Close " + String($0) } ?? "")").font(.caption).textSelection(.enabled)
                if record.kind == "http" {
                    let messages = model.records.filter { $0.parentID == record.id }.sorted { ($0.sequence ?? 0) < ($1.sequence ?? 0) }
                    if !messages.isEmpty {
                        ScrollView(.horizontal) { HStack { Text("Messages").font(.caption); ForEach(messages) { message in Button("\(message.stage ?? "") #\(message.sequence ?? 0)") { model.select(message.id) } } } }.frame(height: 28)
                    }
                } else if let parent = record.parentID { Button("Show HTTP Handshake") { model.select(parent) } }
                Picker("Snapshot", selection: Binding(get: { model.selectedSnapshotName }, set: { model.selectSnapshot($0) })) {
                    ForEach(record.snapshots.keys.sorted(), id: \.self) { Text($0.replacingOccurrences(of: "_", with: " ").capitalized).tag($0) }
                }
                if let snapshot = record.snapshots[model.selectedSnapshotName] {
                    Text("Body: \(snapshot.bodyState) · \(snapshot.captured) captured / \(snapshot.bodySize) bytes\(snapshot.messageType.map { " · " + $0 } ?? "")").font(.caption).foregroundColor(.secondary)
                    if let method = snapshot.method { Text("Method: \(method)").contextMenu { Button("Create Rule from Method…") { model.createRule(field: CaptureFieldSelection(kind: "method", value: method, stage: record.stage)) } } }
                    if let query = snapshot.query, !query.isEmpty { Text("Query: \(query)").textSelection(.enabled).contextMenu { Button("Create Rule from Query…") { model.createRule(field: CaptureFieldSelection(kind: "query", value: query, stage: record.stage)) } } }
                    if let status = snapshot.status { Text("Status: \(status)").contextMenu { Button("Create Rule from Status…") { model.createRule(field: CaptureFieldSelection(kind: "status", value: String(status), stage: record.stage)) } } }
                    ScrollView {
                        VStack(alignment: .leading, spacing: 3) {
                            headerRows(snapshot.headers, kind: "header", stage: record.stage)
                            if snapshot.trailers?.isEmpty == false { Text("Trailers").bold(); headerRows(snapshot.trailers, kind: "trailer", stage: record.stage) }
                            if model.format == .diff, model.selectedSnapshotName.hasSuffix("_actual"), let original = record.snapshots[model.selectedSnapshotName.replacingOccurrences(of: "_actual", with: "_original")] {
                                Text("Original headers").bold(); Text(original.headerText).textSelection(.enabled)
                                Text("Actual headers").bold(); Text(snapshot.headerText).textSelection(.enabled)
                            }
                        }.frame(maxWidth: .infinity, alignment: .leading)
                    }.font(.system(.caption, design: .monospaced)).frame(maxHeight: 150)
                }
                HStack {
                    Picker("Body", selection: Binding(get: { model.format }, set: { model.setFormat($0) })) { ForEach(CaptureBodyFormat.allCases, id: \.self) { Text($0.rawValue).tag($0) } }.labelsHidden().pickerStyle(.segmented)
                    Button("Copy Body") { NSPasteboard.general.clearContents(); NSPasteboard.general.setData(model.selectedBody, forType: NSPasteboard.PasteboardType("public.data")); if let text = String(data: model.selectedBody, encoding: .utf8) { NSPasteboard.general.setString(text, forType: .string) } }
                }
                CaptureBodyTextView(text: model.bodyText, createRule: { selected in model.createRule(field: CaptureFieldSelection(kind: "text", selectedText: selected, value: selected, stage: record.stage)) })
                HStack {
                    TextField("JSON pointer, e.g. /data/name", text: $jsonPointer)
                    Button("Create JSON Rule…") { model.createRule(field: CaptureFieldSelection(kind: "json", pointer: jsonPointer, stage: record.stage)) }.disabled(jsonPointer.isEmpty)
                    Button("Create Rule…") { model.createRule() }
                }.disabled(model.onCreateRule == nil || model.readingBody)
            }.padding(8)
        } else { Text("Select a request or WebSocket message to inspect its content. After starting capture, create a new connection in the application.").foregroundColor(.secondary).frame(maxWidth: .infinity, maxHeight: .infinity) }
    }
    private func headerRows(_ fields: [String: [String]]?, kind: String, stage: String?) -> some View {
        ForEach((fields ?? [:]).keys.sorted(), id: \.self) { key in
            let value = (fields?[key] ?? []).joined(separator: "\n")
            Text(key + ": " + value).textSelection(.enabled).contextMenu {
                Button("Copy") { copy(key + ": " + value) }
                Button("Create Rule from \(key)…") { model.createRule(field: CaptureFieldSelection(kind: kind, name: key, value: value, stage: stage)) }.disabled(model.onCreateRule == nil)
            }
        }
    }
    private func lines(_ value: String) -> [String] { value.components(separatedBy: .newlines).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty } }
    private func filters() { model.setFilters(query: query, kind: kind, domain: domain, processPath: process, ascending: ascending) }
    private func copy(_ text: String) { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(text, forType: .string) }
    private func importFile() {
        let panel = NSOpenPanel(); panel.allowsMultipleSelection = false; panel.canChooseDirectories = false
        if panel.runModal() == .OK, let file = panel.url { Task { await model.perform(["import", file.path]) } }
    }
    private func exportFile(_ format: String) {
        let panel = NSSavePanel(); panel.nameFieldStringValue = format == "har" ? "capture.har" : "capture.ndjson"
        panel.message = "The archive includes original credentials, URLs and bodies. Keep it private."
        if panel.runModal() == .OK, let file = panel.url { Task { await model.exportFile(file.path, format: format) } }
    }
}

func captureMinute(_ raw: String) -> String {
    let parser = ISO8601DateFormatter(); parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    guard let date = parser.date(from: raw) ?? ISO8601DateFormatter().date(from: raw) else { return "unknown" }
    let formatter = DateFormatter(); formatter.dateFormat = "yyyy-MM-dd HH:mm"; return formatter.string(from: date)
}

@MainActor struct CaptureBodyTextView: NSViewRepresentable {
    let text: String
    let createRule: (String) -> Void
    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSScrollView(); scroll.hasVerticalScroller = true; scroll.hasHorizontalScroller = true
        let view = CaptureSelectableTextView(); view.isEditable = false; view.isSelectable = true
        view.font = .monospacedSystemFont(ofSize: 12, weight: .regular); view.autoresizingMask = [.width]
        view.isVerticallyResizable = true; view.isHorizontallyResizable = false
        view.textContainer?.widthTracksTextView = true
        scroll.documentView = view; return scroll
    }
    func updateNSView(_ scroll: NSScrollView, context: Context) {
        guard let view = scroll.documentView as? CaptureSelectableTextView else { return }
        view.createRule = createRule
        if view.string != text { let selection = view.selectedRange(); view.string = text; if NSMaxRange(selection) <= (text as NSString).length { view.setSelectedRange(selection) } }
    }
}
@MainActor final class CaptureSelectableTextView: NSTextView {
    var createRule: ((String) -> Void)?
    override func menu(for event: NSEvent) -> NSMenu? {
        let menu = super.menu(for: event) ?? NSMenu()
        if selectedRange().length > 0 {
            menu.addItem(.separator()); let item = NSMenuItem(title: "Create Rule from Selection…", action: #selector(ruleFromSelection), keyEquivalent: ""); item.target = self; menu.addItem(item)
        }
        return menu
    }
    @objc private func ruleFromSelection() { createRule?((string as NSString).substring(with: selectedRange())) }
}
