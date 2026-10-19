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
        let window = ConnectionsWindow(contentRect: NSRect(x: 0, y: 0, width: 1100, height: 740), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = "Nagi Capture"; window.minSize = NSSize(width: 980, height: 650)
        window.isReleasedWhenClosed = false; window.delegate = self
        window.onVisibilityChange = { [weak self] in self?.updateVisibility() }
        let content = NSHostingView(rootView: CaptureView(presentation: CapturePresentation(model)))
        // AppKit owns window sizing. SwiftUI's content-derived minimum/ideal
        // sizes otherwise allow capture controls and raw values to grow it.
        content.sizingOptions = []
        window.contentView = content
        if let screen = window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame {
            window.minSize = NSSize(width: min(980, screen.width * 0.8), height: min(650, screen.height * 0.8))
            window.setFrame(Self.initialFrame(in: screen), display: false)
        } else {
            window.center()
        }
        model.onCreateRule = { [weak self] context in self?.createRule(context) }
        observers.append(NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.refreshOnForeground() }
        })
        for name in [NSApplication.didHideNotification, NSApplication.didUnhideNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in Task { @MainActor in self?.updateVisibility() } })
        }
    }
    static func initialFrame(in screen: NSRect) -> NSRect {
        let size = NSSize(width: min(1100, screen.width * 0.9), height: min(760, screen.height * 0.9))
        return NSRect(x: screen.midX - size.width / 2, y: screen.midY - size.height / 2,
                      width: size.width, height: size.height)
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
    private func refreshOnForeground() {
        // Opening/restoring already starts a read; foreground notifications can
        // arrive together and should share that snapshot.
        if !model.isReading { model.refresh() }
    }
    func windowDidBecomeKey(_ notification: Notification) { refreshOnForeground() }
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
    @State private var scopePresented = false
    @State private var detailSection = CaptureDetailSection.overview
    private var model: CaptureModel { presentation.model }
    var body: some View {
        // Bound layout proposals to the actual window, including when a raw
        // URL/header has a much larger intrinsic size than the available space.
        GeometryReader { geometry in
            content.frame(width: geometry.size.width, height: geometry.size.height, alignment: .topLeading)
        }.nativeFormControls()
    }
    private var content: some View {
        VStack(alignment: .leading, spacing: 10) {
            ScrollView(.horizontal) { controls }.fixedSize(horizontal: false, vertical: true)
            if !model.prerequisitesReady, let message = model.prerequisiteMessage {
                HStack {
                    Text(message).font(.callout).foregroundColor(.orange).textSelection(.enabled)
                    if !model.checkingPrerequisites {
                        Button("Recheck") { model.refreshPrerequisites() }.disabled(!model.canOperate)
                    }
                }
            }
            ScrollView(.horizontal) { HStack {
                TextField("Search requests", text: $query)
                    .frame(minWidth: 220)
                    .help("Search original URLs, headers and bodies")
                Picker("Kind", selection: $kind) { Text("All").tag(""); Text("HTTP").tag("http"); Text("WebSocket").tag("websocket") }.frame(width: 170)
                TextField("Domain", text: $domain).frame(width: 160)
                TextField("Process path", text: $process).frame(width: 180)
                Toggle("Oldest first", isOn: $ascending)
            } }.fixedSize(horizontal: false, vertical: true)
                .onChange(of: query) { _ in filters() }.onChange(of: kind) { _ in filters() }
                .onChange(of: domain) { _ in filters() }.onChange(of: process) { _ in filters() }.onChange(of: ascending) { _ in filters() }
            HSplitView {
                recordList.frame(minWidth: 360, idealWidth: 440)
                details.frame(minWidth: 380)
            }
            HStack {
                Text("\(model.records.count) records")
                if model.stale { Text("Out of date").foregroundColor(.orange) }
                if let status = model.status {
                    Text(ByteCountFormatter.string(fromByteCount: status.bytes, countStyle: .file))
                    if status.dropped > 0 { Text("\(status.dropped) dropped").foregroundColor(.orange) }
                    if status.truncated > 0 { Text("\(status.truncated) truncated").foregroundColor(.orange) }
                }
                Spacer()
                // Keep the status row the same size between refreshes.
                ZStack {
                    if model.isReading || model.readingBody { ProgressView().controlSize(.small) }
                }.frame(width: 16, height: 16)
            }.font(.caption).foregroundColor(.secondary)
            if let error = model.error { Text(error).foregroundColor(.red).textSelection(.enabled) }
            if let status = model.status, let recovery = status.recovery {
                Text("Recorder: \(status.recorderState ?? "unknown"). \(recovery)").font(.callout).foregroundColor(.orange).textSelection(.enabled)
            }
        }.padding()
    }
    private var controls: some View {
        HStack {
            Button("Start") { Task { await model.start(scope: draftScope, bodyLimit: bodyLimit) } }
                .disabled(!model.canOperate || model.status?.canStartNewSession == false)
            Button(model.status?.state == "paused" ? "Resume" : "Pause") { Task { await model.mutate(model.status?.state == "paused" ? "resume" : "pause") } }
                .disabled(!model.canOperate || model.status?.canPauseOrResume != true)
            Button("Stop") { Task { await model.mutate("stop") } }.disabled(!model.canOperate || model.status?.canStop != true)
            Divider().frame(height: 16)
            Button { scopePresented.toggle() } label: {
                HStack(spacing: 4) {
                    Text("Scope: \(scopeSummary)")
                    Image(systemName: "chevron.down").font(.caption2)
                }
            }
            .popover(isPresented: $scopePresented, arrowEdge: .bottom) { scope }
            if model.status?.readOnly == true { Text("Read only").foregroundColor(.secondary) }
            else if let status = model.status {
                Text(status.state.capitalized).foregroundColor(status.state == "recording" ? .red : .secondary)
            }
            Spacer(minLength: 16)
            if !model.isRecording {
                Button { model.refresh() } label: { Image(systemName: "arrow.clockwise") }
                    .help("Refresh capture status and records").accessibilityLabel("Refresh").disabled(!model.canOperate)
            }
            Menu {
                Picker("Body limit", selection: bodyLimitSelection) {
                    Text("1 MiB").tag(1_048_576); Text("4 MiB").tag(4_194_304); Text("8 MiB").tag(8_388_608)
                }.disabled(!model.canEditScope)
                Divider()
                Button("Import…") { importFile() }.disabled(!model.canOperate || model.status?.canStartNewSession == false)
                Menu("Export") {
                    Button("Native Archive…") { exportFile("native") }
                    Button("HAR…") { exportFile("har") }
                }.disabled(!model.canOperate || model.status == nil)
                Divider()
                Button("Clear") { Task { await model.mutate("clear") } }.disabled(!model.canOperate || model.status == nil || model.status?.readOnly == true)
            } label: { Image(systemName: "ellipsis") }.help("More").accessibilityLabel("More")
        }
    }
    private var draftScope: CaptureScope {
        CaptureScope(domains: lines(domains), processPaths: lines(processes), appPaths: lines(apps))
    }
    private var displayedScope: CaptureScope { model.activeScope ?? draftScope }
    private var bodyLimitSelection: Binding<Int> {
        Binding(get: { model.activeScope == nil ? bodyLimit : (model.status?.bodyLimit ?? bodyLimit) },
                set: { if model.canEditScope { bodyLimit = $0 } })
    }
    private var scopeSummary: String {
        let value = displayedScope
        let parts = [(value.domains.count, "domain", "domains"),
                     (value.processPaths.count, "process", "processes"),
                     (value.appPaths.count, "application", "applications")]
            .filter { $0.0 > 0 }
            .map { "\($0.0) \($0.0 == 1 ? $0.1 : $0.2)" }
        return parts.isEmpty ? "All traffic" : parts.joined(separator: " · ")
    }
    private var scope: some View {
        VStack(alignment: .leading, spacing: 12) {
            scopeField("Domains", text: $domains, current: displayedScope.domains)
                .help("One domain pattern per line. Empty matches all domains. Different scope categories combine.")
            scopeField("Processes", text: $processes, current: displayedScope.processPaths)
                .help("One exact absolute process path per line")
            scopeField("Applications", text: $apps, current: displayedScope.appPaths)
                .help("One absolute .app path per line, including its helper processes")
            if model.activeScope == nil {
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
                }.disabled(!model.canEditScope)
            }
            if model.activeScope != nil {
                Text("Stop capture to change the scope.").font(.caption).foregroundColor(.secondary)
            }
            HStack {
                Spacer()
                Button("Done") { scopePresented = false }.keyboardShortcut(.defaultAction)
            }
        }.padding(16).frame(width: 420)
    }
    private func scopeField(_ title: String, text: Binding<String>, current: [String]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption).foregroundColor(.secondary)
            if model.activeScope != nil {
                ScrollView {
                    Text(current.isEmpty ? "All" : current.joined(separator: "\n"))
                        .frame(maxWidth: .infinity, alignment: .leading).textSelection(.enabled)
                }.frame(height: 52)
            } else {
                TextEditor(text: text).frame(height: 52).nativeTextSurface()
                    .disabled(!model.canEditScope)
            }
        }.font(.system(.caption, design: .monospaced))
    }
    private var recordList: some View {
        CaptureRecordTable(records: model.records, selection: Binding(get: { model.selectedID }, set: { if $0 != model.selectedID { model.select($0) } }))
    }
    private var details: some View {
        CaptureDetailView(presentation: presentation, section: $detailSection)
    }
    private func lines(_ value: String) -> [String] { value.components(separatedBy: .newlines).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty } }
    private func filters() { model.setFilters(query: query, kind: kind, domain: domain, processPath: process, ascending: ascending) }
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
        let scroll = NSScrollView(); scroll.hasVerticalScroller = true; scroll.hasHorizontalScroller = false
        scroll.autohidesScrollers = true
        let view = CaptureSelectableTextView(); view.isEditable = false; view.isSelectable = true
        view.font = .monospacedSystemFont(ofSize: 12, weight: .regular); view.autoresizingMask = [.width]
        view.isRichText = false; view.textContainerInset = NSSize(width: 8, height: 8)
        view.setAccessibilityLabel("Captured body")
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
