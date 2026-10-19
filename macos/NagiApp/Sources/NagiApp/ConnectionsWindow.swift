import AppKit

/// Explicit ordering hooks complement delegate notifications, which AppKit
/// can defer while minimizing and does not reliably send for orderOut.
@MainActor final class ConnectionsWindow: NSWindow {
    var onVisibilityChange: (() -> Void)?
    private(set) var orderedOut = true
    private(set) var minimizing = false
    private var miniaturizationFallback: Task<Void, Never>?
    var readingPaused: Bool { orderedOut || minimizing }

    func beginMiniaturization() {
        guard !minimizing else { return }
        minimizing = true
        onVisibilityChange?()
        // Accessory apps have no Dock tile. AppKit can decline to miniaturize
        // their windows; hide after a short grace period instead of leaving
        // a visible window with its connection reading suspended.
        miniaturizationFallback = Task { [weak self] in
            do { try await Task.sleep(nanoseconds: 500_000_000) } catch { return }
            guard let self, self.minimizing else { return }
            self.miniaturizationFallback = nil
            if !self.isMiniaturized && self.isVisible { self.orderOut(nil) }
        }
    }
    func finishDeminiaturization() {
        miniaturizationFallback?.cancel(); miniaturizationFallback = nil
        minimizing = false
        onVisibilityChange?()
    }
    override func miniaturize(_ sender: Any?) {
        beginMiniaturization()
        super.miniaturize(sender)
    }
    override func deminiaturize(_ sender: Any?) {
        super.deminiaturize(sender)
        finishDeminiaturization()
    }
    override func orderOut(_ sender: Any?) {
        miniaturizationFallback?.cancel(); miniaturizationFallback = nil
        orderedOut = true
        onVisibilityChange?()
        super.orderOut(sender)
    }
    override func orderFront(_ sender: Any?) {
        orderedOut = false
        super.orderFront(sender)
        onVisibilityChange?()
    }
    override func makeKeyAndOrderFront(_ sender: Any?) {
        orderedOut = false
        super.makeKeyAndOrderFront(sender)
        onVisibilityChange?()
    }
    override func close() {
        miniaturizationFallback?.cancel(); miniaturizationFallback = nil
        orderedOut = true
        onVisibilityChange?()
        super.close()
    }
}

/// A session-scoped native view of the connection model's current CLI snapshot.
@MainActor final class ConnectionsWindowController: NSWindowController, NSWindowDelegate,
    NSTableViewDataSource, NSTableViewDelegate, NSSearchFieldDelegate {
    private let model: ConnectionsModel
    private let table = NSTableView()
    private let scroll = NSScrollView()
    private let search = NSSearchField()
    private let countLabel = NSTextField(labelWithString: "0 / 0 connections")
    private let updatedLabel = NSTextField(labelWithString: "Not updated yet")
    private let statusLabel = NSTextField(wrappingLabelWithString: "")
    private let detail = NSTextView()
    private let refreshButton = NSButton(title: "Refresh", target: nil, action: nil)
    private let closeButton = NSButton(title: "Close Connection", target: nil, action: nil)
    private let closeAllButton = NSButton(title: "Close All Connections…", target: nil, action: nil)
    private var rows: [Connection] = []
    private var renderedRowsRevision: UInt64?
    private var topPinnedDuringRefresh = false
    private var updatingTable = false
    private var observers: [NSObjectProtocol] = []
    private static let timeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .none
        formatter.timeStyle = .short
        return formatter
    }()

    init(model: ConnectionsModel) {
        self.model = model
        let window = ConnectionsWindow(contentRect: NSRect(x: 0, y: 0, width: 1040, height: 700),
            styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = "Nagi Connections"
        window.minSize = NSSize(width: 800, height: 550)
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.onVisibilityChange = { [weak self] in self?.updateVisibility() }
        window.center()
        buildContent()
        model.onChange = { [weak self] in self?.render() }
        for name in [NSApplication.didHideNotification, NSApplication.didUnhideNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                Task { @MainActor in self?.updateVisibility() }
            })
        }
        render()
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func present() {
        guard let window else { return }
        if window.isMiniaturized || (window as? ConnectionsWindow)?.minimizing == true { window.deminiaturize(nil) }
        showWindow(nil)
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
        updateVisibility()
    }

    private func buildContent() {
        guard let content = window?.contentView else { return }
        NativeControlStyle.configure(search)
        for button in [refreshButton, closeButton, closeAllButton] { NativeControlStyle.configure(button) }
        search.placeholderString = "Search domain, IP, process, proxy chain, or ID"
        search.delegate = self
        search.sendsSearchStringImmediately = true
        search.setAccessibilityLabel("Search connections")
        refreshButton.target = self; refreshButton.action = #selector(refresh)
        closeButton.target = self; closeButton.action = #selector(closeSelected)
        closeAllButton.target = self; closeAllButton.action = #selector(closeAll)
        countLabel.font = .monospacedDigitSystemFont(ofSize: NSFont.smallSystemFontSize, weight: .regular)
        updatedLabel.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        updatedLabel.textColor = .secondaryLabelColor

        let toolbar = NSStackView(views: [search, refreshButton, countLabel])
        toolbar.orientation = .horizontal; toolbar.spacing = 12
        toolbar.alignment = .firstBaseline
        search.setContentHuggingPriority(.defaultLow, for: .horizontal)
        countLabel.setContentCompressionResistancePriority(.required, for: .horizontal)
        refreshButton.setContentCompressionResistancePriority(.required, for: .horizontal)

        table.delegate = self; table.dataSource = self
        table.usesAlternatingRowBackgroundColors = true
        table.allowsMultipleSelection = false
        table.allowsEmptySelection = true
        table.rowHeight = 25
        table.columnAutoresizingStyle = .lastColumnOnlyAutoresizingStyle
        table.setAccessibilityLabel("Active connections")
        let columns: [(ConnectionSortColumn, String, CGFloat)] = [
            (.destination, "Destination", 250), (.process, "Process", 150), (.network, "Protocol", 75),
            (.chains, "Proxy Chain", 235), (.upload, "Uploaded (cumulative)", 140),
            (.download, "Downloaded (cumulative)", 150)
        ]
        for (key, title, width) in columns {
            let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier(key.rawValue))
            column.title = title; column.width = width; column.minWidth = 65
            column.sortDescriptorPrototype = NSSortDescriptor(key: key.rawValue, ascending: true)
            table.addTableColumn(column)
        }
        table.sortDescriptors = [NSSortDescriptor(key: ConnectionSortColumn.destination.rawValue, ascending: true)]
        scroll.documentView = table
        scroll.hasVerticalScroller = true; scroll.hasHorizontalScroller = true
        scroll.autohidesScrollers = true
        scroll.borderType = .bezelBorder
        scroll.setContentHuggingPriority(.defaultLow, for: .vertical)

        detail.identifier = NSUserInterfaceItemIdentifier("connection-details")
        detail.frame = NSRect(x: 0, y: 0, width: 900, height: 170)
        detail.minSize = .zero
        detail.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        detail.isEditable = false; detail.isSelectable = true
        detail.isRichText = false; detail.drawsBackground = false
        detail.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        detail.textContainerInset = NSSize(width: 8, height: 8)
        detail.textContainer?.widthTracksTextView = true
        detail.isVerticallyResizable = true; detail.isHorizontallyResizable = false
        detail.autoresizingMask = [.width]
        detail.setAccessibilityLabel("Connection details")
        let detailScroll = NSScrollView()
        detailScroll.documentView = detail; detailScroll.hasVerticalScroller = true
        detailScroll.borderType = .bezelBorder
        detailScroll.heightAnchor.constraint(equalToConstant: 170).isActive = true
        let detailsTitle = NSTextField(labelWithString: "Connection Details")
        detailsTitle.font = .systemFont(ofSize: NSFont.smallSystemFontSize, weight: .semibold)
        let actions = NSStackView(views: [closeButton, closeAllButton, NSView(), updatedLabel])
        actions.orientation = .horizontal; actions.spacing = 12
        actions.alignment = .firstBaseline
        statusLabel.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        statusLabel.maximumNumberOfLines = 3
        let sampling = NSTextField(labelWithString: "Snapshots refresh once a second. Connections shorter than one second may not appear.")
        sampling.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        sampling.textColor = .secondaryLabelColor
        let stack = NSStackView(views: [toolbar, scroll, detailsTitle, detailScroll, actions, statusLabel, sampling])
        stack.orientation = .vertical; stack.alignment = .leading; stack.spacing = 10
        stack.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 16),
            stack.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -16),
            stack.topAnchor.constraint(equalTo: content.topAnchor, constant: 16),
            stack.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -16),
            scroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 140)
        ])
        for view in [toolbar, scroll, detailScroll, actions, statusLabel] {
            view.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
        }
    }

    private func render() {
        updatingTable = true
        let firstRowTop = rows.isEmpty ? table.bounds.minY : table.rect(ofRow: 0).minY
        let currentlyAtTop = table.visibleRect.minY <= firstRowTop + 0.5
        if model.isRefreshing && currentlyAtTop { topPinnedDuringRefresh = true }
        var wasAtTop = false
        if renderedRowsRevision != model.rowsRevision {
            // Anchor scrolling to an ID so inserted and reordered rows do not move the user's view.
            let origin = scroll.contentView.bounds.origin
            // Scroller values can be stale while AppKit lays out a reloaded
            // table. The table's visible rect is in document coordinates and
            // gives the actual top boundary.
            wasAtTop = topPinnedDuringRefresh || currentlyAtTop
            let topRow = table.row(at: origin)
            let anchorID = rows.indices.contains(topRow) ? rows[topRow].id : nil
            let offset = topRow >= 0 ? origin.y - table.rect(ofRow: topRow).minY : 0
            rows = model.rows
            renderedRowsRevision = model.rowsRevision
            topPinnedDuringRefresh = false
            table.reloadData()
            // A refresh can change which row is first. When the user was at
            // the top, keep that boundary instead of anchoring the previous
            // first row after it has moved or disappeared.
            table.layoutSubtreeIfNeeded()
            let maximum = max(0, table.bounds.height - scroll.contentView.bounds.height)
            let minimum = topClipOriginY()
            let y: CGFloat
            if wasAtTop {
                y = topClipOriginY()
            } else if let anchorID, let index = rows.firstIndex(where: { $0.id == anchorID }) {
                y = table.rect(ofRow: index).minY + offset
            } else { y = origin.y }
            scroll.contentView.scroll(to: NSPoint(x: origin.x, y: min(maximum, max(minimum, y))))
            scroll.reflectScrolledClipView(scroll.contentView)
        }
        if let selectedID = model.selectedID, let index = rows.firstIndex(where: { $0.id == selectedID }) {
            if table.selectedRow != index { table.selectRowIndexes(IndexSet(integer: index), byExtendingSelection: false) }
        } else if table.selectedRow != -1 { table.deselectAll(nil) }
        if wasAtTop {
            let revision = renderedRowsRevision
            let originX = scroll.contentView.bounds.origin.x
            // Selecting a row can make AppKit scroll that row into view. Do
            // this after selection synchronization so a top-pinned list
            // cannot be pulled down and hide its first row.
            scroll.contentView.scroll(to: NSPoint(x: originX, y: topClipOriginY()))
            scroll.reflectScrolledClipView(scroll.contentView)
            DispatchQueue.main.async { [weak self] in
                guard let self, self.renderedRowsRevision == revision else { return }
                self.scroll.contentView.scroll(to: NSPoint(x: originX, y: self.topClipOriginY()))
                self.scroll.reflectScrolledClipView(self.scroll.contentView)
            }
        }
        updatingTable = false
        countLabel.stringValue = "\(rows.count) / \(model.connections.count) connections"
        updatedLabel.stringValue = model.lastUpdated.map { "Updated \(Self.timeFormatter.string(from: $0))" } ?? "Not updated yet"
        refreshButton.isEnabled = !model.isRefreshing && !model.operationInProgress
        closeButton.isEnabled = model.canClose && model.selectedConnection != nil
        closeAllButton.isEnabled = model.canClose && !model.connections.isEmpty
        renderDetails()
        let stateMessage: String
        switch model.state {
        case .idle: stateMessage = "Open the window to load active connections."
        case .loading: stateMessage = "Loading connections…"
        case .fresh: stateMessage = model.connections.isEmpty ? "No active connections." : ""
        case .stale: stateMessage = "Displayed connections are stale. Close actions are unavailable until a refresh succeeds."
        case .stopped: stateMessage = "Engine stopped. Use Retry Start from the Nagi menu to load connections."
        case .unavailable: stateMessage = "Engine status unavailable. Waiting for status to recover."
        case .tooLarge: stateMessage = "Connection list too large to load. Automatic refresh is paused; use Refresh to retry."
        }
        statusLabel.stringValue = [stateMessage, model.errorMessage, model.operationMessage]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
        statusLabel.textColor = (model.state == .stale || model.state == .tooLarge || model.errorMessage != nil)
            ? .systemRed : .secondaryLabelColor
    }

    private func topClipOriginY() -> CGFloat {
        table.bounds.minY - (table.headerView?.frame.height ?? 0)
    }

    private func renderDetails() {
        guard let connection = model.selectedConnection else {
            detail.string = model.selectionDisappearMessage ?? "Select a connection to inspect its details."
            return
        }
        detail.string = """
        Source: \(connection.source)
        Destination: \(connection.destination)
        Destination IP: \(connection.destinationIP)
        Process: \(connection.process)
        Process Path: \(connection.processPath)
        Protocol: \(connection.network)    Inbound: \(connection.inbound)
        Rule: \(connection.rule)    Rule Payload: \(connection.rulePayload)
        Proxy Chain: \(connection.chain)
        Uploaded (cumulative): \(Self.bytes(connection.upload))    Downloaded (cumulative): \(Self.bytes(connection.download))
        ID: \(connection.id)
        """
    }

    private static func bytes(_ value: Int64?) -> String {
        guard let value else { return "unknown" }
        return MenuAppDelegate.subscriptionBytes(value)
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }
    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        guard rows.indices.contains(row), let column = tableColumn,
              let key = ConnectionSortColumn(rawValue: column.identifier.rawValue) else { return nil }
        let cell = (tableView.makeView(withIdentifier: column.identifier, owner: self) as? NSTableCellView) ?? NSTableCellView()
        if cell.textField == nil {
            cell.identifier = column.identifier
            let label = NSTextField(labelWithString: "")
            label.translatesAutoresizingMaskIntoConstraints = false
            label.lineBreakMode = .byTruncatingTail
            cell.addSubview(label); cell.textField = label
            NSLayoutConstraint.activate([
                label.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 6),
                label.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -6),
                label.centerYAnchor.constraint(equalTo: cell.centerYAnchor)
            ])
        }
        let connection = rows[row]
        let text: String
        switch key {
        case .destination: text = connection.destination
        case .process: text = connection.process
        case .network: text = connection.network
        case .chains: text = connection.chain
        case .upload: text = Self.bytes(connection.upload)
        case .download: text = Self.bytes(connection.download)
        }
        cell.textField?.stringValue = text
        cell.textField?.font = key == .upload || key == .download
            ? .monospacedDigitSystemFont(ofSize: NSFont.systemFontSize, weight: .regular) : .systemFont(ofSize: NSFont.systemFontSize)
        cell.textField?.alignment = key == .upload || key == .download ? .right : .left
        cell.toolTip = text
        return cell
    }
    func tableViewSelectionDidChange(_ notification: Notification) {
        guard !updatingTable else { return }
        let row = table.selectedRow
        model.select(id: rows.indices.contains(row) ? rows[row].id : nil)
    }
    func tableView(_ tableView: NSTableView, sortDescriptorsDidChange oldDescriptors: [NSSortDescriptor]) {
        guard let descriptor = tableView.sortDescriptors.first, let key = descriptor.key,
              let column = ConnectionSortColumn(rawValue: key) else { return }
        model.setSort(column: column, ascending: descriptor.ascending)
    }
    func controlTextDidChange(_ obj: Notification) { model.setQuery(search.stringValue) }
    @objc private func refresh() { model.refresh() }
    @objc private func closeSelected() {
        guard model.canClose, let id = model.selectedConnection?.id else { return }
        model.close(id: id)
    }
    @objc private func closeAll() {
        guard model.canClose, let window else { return }
        let alert = Self.closeAllConfirmation()
        alert.beginSheetModal(for: window) { [weak self] response in
            guard response == .alertFirstButtonReturn else { return }
            self?.model.closeAll()
        }
    }

    static func closeAllConfirmation() -> NSAlert {
        let alert = NSAlert()
        alert.messageText = "Close All Connections?"
        alert.informativeText = "This closes all current mihomo connections, including connections outside this search. Applications may reconnect automatically."
        alert.alertStyle = .warning
        alert.addButton(withTitle: "Close All Connections")
        alert.addButton(withTitle: "Cancel")
        return alert
    }
    private func updateVisibility() {
        let readingPaused = (window as? ConnectionsWindow)?.readingPaused == true
        model.setVisible(!readingPaused && window?.isVisible == true && window?.isMiniaturized == false && !NSApp.isHidden)
    }
    func windowWillClose(_ notification: Notification) { model.setVisible(false) }
    func windowWillMiniaturize(_ notification: Notification) {
        (window as? ConnectionsWindow)?.beginMiniaturization()
    }
    func windowDidMiniaturize(_ notification: Notification) { updateVisibility() }
    func windowDidDeminiaturize(_ notification: Notification) {
        (window as? ConnectionsWindow)?.finishDeminiaturization()
        updateVisibility()
    }
    func windowDidChangeOcclusionState(_ notification: Notification) { updateVisibility() }
}
