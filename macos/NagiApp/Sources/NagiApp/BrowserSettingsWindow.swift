import AppKit
import SwiftUI

@MainActor final class BrowserSettingsWindowController: NSWindowController, NSWindowDelegate {
    let model: BrowserSettingsModel
    static let layout = NativeWindowLayout(preferredSize: NSSize(width: 560, height: 350),
                                          minimumSize: NSSize(width: 400, height: 260))
    private var readTask: Task<Void, Never>?

    init(model: BrowserSettingsModel) {
        self.model = model
        let window = NativeWindow(contentRect: NSRect(x: 0, y: 0, width: 560, height: 350),
                                  styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = L10n.text("app.browser.title")
        window.isReleasedWhenClosed = false; window.delegate = self
        let content = NSHostingView(rootView: BrowserSettingsView(model: model, close: { [weak self] in self?.window?.performClose(nil) }))
        content.sizingOptions = []
        window.contentView = content
        Self.layout.apply(to: window)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func present() {
        guard let window else { return }
        showWindow(nil); NSApp.activate(ignoringOtherApps: true); window.makeKeyAndOrderFront(nil)
        refresh()
    }

    private func refresh() {
        guard readTask == nil, !model.busy else { return }
        readTask = Task { [weak self] in
            guard let self else { return }
            await self.model.refresh()
            self.readTask = nil
        }
    }

    func windowDidBecomeKey(_ notification: Notification) { refresh() }
    func windowShouldClose(_ sender: NSWindow) -> Bool { !model.isSaving }
}

@MainActor private struct BrowserSettingsView: View {
    @ObservedObject var model: BrowserSettingsModel
    let close: () -> Void
    @State private var showingHelp = false

    var body: some View {
        GeometryReader { geometry in
            VStack(alignment: .leading, spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack {
                            Text(verbatim: L10n.text("app.browser.chrome")).font(.headline)
                            Spacer()
                            if model.busy { ProgressView().controlSize(.small) }
                            Button { showingHelp.toggle() } label: { Image(systemName: "questionmark.circle") }
                                .accessibilityLabel(L10n.text("app.ui.help"))
                                .popover(isPresented: $showingHelp) {
                                    Text(verbatim: L10n.text("app.browser.help")).padding().frame(width: 350)
                                }
                        }
                        if let state = model.state, !model.stale {
                            HStack(alignment: .firstTextBaseline, spacing: 12) {
                                Text(verbatim: L10n.text("app.browser.current_policy")).foregroundColor(.secondary)
                                Text(verbatim: state.restricted ? L10n.text("app.browser.enabled") : L10n.text("app.browser.disabled"))
                            }
                            if !state.available {
                                Text(verbatim: L10n.text("app.browser.unavailable")).foregroundColor(.secondary)
                            } else if state.forced {
                                Text(verbatim: L10n.text("app.browser.forced")).foregroundColor(.secondary)
                            } else if state.restricted && !state.managed {
                                Text(verbatim: L10n.text("app.browser.external")).foregroundColor(.secondary)
                                Text(verbatim: L10n.text("app.browser.external_help")).font(.callout).foregroundColor(.secondary)
                            } else if state.managed {
                                Text(verbatim: L10n.text("app.browser.nagi_managed")).foregroundColor(.secondary)
                            }
                            if let policy = state.currentValue, !state.restricted {
                                Text(verbatim: L10n.format("app.browser.other_policy", policy)).font(.callout).textSelection(.enabled)
                            }
                        } else if model.isReading {
                            Text(verbatim: L10n.text("app.browser.loading")).foregroundColor(.secondary)
                        }
                        if let error = model.error {
                            Text(verbatim: error).foregroundColor(.red).textSelection(.enabled)
                        }
                        if model.saved {
                            Text(verbatim: L10n.text("app.browser.saved")).foregroundColor(.secondary)
                        } else {
                            Text(verbatim: L10n.text("app.browser.restart")).font(.callout).foregroundColor(.secondary)
                        }
                    }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
                }
                Divider()
                HStack {
                    Button(L10n.text("app.ui.refresh")) { Task { await model.refresh() } }.disabled(model.busy)
                    Spacer()
                    Button(L10n.text("app.ui.close"), action: close).keyboardShortcut(.cancelAction).disabled(model.isSaving)
                    if model.state?.canRestore == true {
                        Button(L10n.text("app.browser.restore")) { Task { await model.restore() } }.disabled(!model.canRestore)
                    } else if model.state?.restricted != true {
                        Button(L10n.text("app.browser.enable")) { Task { await model.enable() } }.disabled(!model.canEnable)
                    }
                }.padding(16)
            }.frame(width: geometry.size.width, height: geometry.size.height, alignment: .topLeading)
        }.nativeFormControls()
    }
}
