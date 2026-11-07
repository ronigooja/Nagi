import AppKit
import SwiftUI

@MainActor final class SettingsWindowController: NSWindowController, NSWindowDelegate {
    let model: SettingsModel
    static let layout = NativeWindowLayout(preferredSize: NSSize(width: 540, height: 450),
                                          minimumSize: NSSize(width: 380, height: 280))
    private var readTask: Task<Void, Never>?

    init(model: SettingsModel) {
        self.model = model
        let window = NativeWindow(contentRect: NSRect(x: 0, y: 0, width: 540, height: 450),
                                  styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        super.init(window: window)
        window.title = L10n.text("app.settings.title")
        window.isReleasedWhenClosed = false; window.delegate = self
        let content = NSHostingView(rootView: SettingsView(model: model, browser: model.browser, lan: model.lan))
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
        guard readTask == nil, !model.busy, !model.browser.busy, !model.lan.busy else { return }
        readTask = Task { [weak self] in
            guard let self else { return }
            await self.model.refresh()
            self.readTask = nil
        }
    }

    func windowDidBecomeKey(_ notification: Notification) { refresh() }
    func windowShouldClose(_ sender: NSWindow) -> Bool { !model.busy && !model.browser.isSaving && !model.lan.isSaving }
}

@MainActor private struct SettingsView: View {
    @ObservedObject var model: SettingsModel
    @ObservedObject var browser: BrowserSettingsModel
    @ObservedObject var lan: LANSettingsModel
    private var busy: Bool { model.busy || browser.busy || lan.busy }

    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    GroupBox {
                        Picker(L10n.text("app.ui.display"), selection: Binding(get: { model.display }, set: { model.setDisplay($0) })) {
                            ForEach(TrafficDisplay.allCases, id: \.self) { option in
                                Text(verbatim: option.title).tag(option)
                            }
                        }.pickerStyle(.radioGroup).labelsHidden().disabled(busy)
                            .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                    } label: { Text(verbatim: L10n.text("app.ui.display")) }
                    GroupBox {
                        VStack(alignment: .leading, spacing: 8) {
                            HStack {
                                if lan.stale {
                                    Text(verbatim: L10n.text("app.lan.unknown"))
                                        .foregroundColor(.secondary)
                                } else {
                                    Toggle(L10n.text("app.lan.allow"), isOn: Binding(
                                        get: { lan.state?.enabled == true },
                                        set: { value in Task { await lan.setEnabled(value) } }))
                                        .disabled(model.busy || browser.busy || !lan.canToggle ||
                                                  (lan.state?.enabled == false && !lan.validAddress))
                                }
                                if lan.busy { ProgressView().controlSize(.small) }
                            }
                            Text(verbatim: L10n.format("app.lan.port", lan.portText))
                            HStack {
                                TextField(L10n.text("app.lan.bind_address"), text: $lan.bindAddress,
                                          onEditingChanged: { lan.setEditing($0) },
                                          onCommit: { applyLANAddress() })
                                    .disabled(model.busy || browser.busy || !lan.canToggle)
                                    .accessibilityLabel(L10n.text("app.lan.bind_address"))
                                Button(L10n.text("app.lan.apply")) { applyLANAddress() }
                                    .disabled(model.busy || browser.busy || !lan.canApplyAddress)
                            }
                            if !lan.validAddress { errorText(L10n.text("app.lan.invalid_address")) }
                            if lan.exposesAllInterfaces {
                                Text(verbatim: L10n.text("app.lan.exposure_warning"))
                                    .font(.callout).foregroundColor(.secondary)
                            }
                            if let error = lan.error { errorText(error) }
                        }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
                    } label: { Text(verbatim: L10n.text("app.lan.title")) }
                    GroupBox {
                        VStack(alignment: .leading, spacing: 8) {
                            HStack {
                                Toggle(L10n.text("app.settings.chrome_toggle"), isOn: Binding(
                                    get: { browser.state?.restricted == true },
                                    set: { value in Task { await browser.setRestricted(value) } }))
                                    .disabled(model.busy || lan.busy || !browser.canToggle)
                                if browser.busy { ProgressView().controlSize(.small) }
                            }
                            Text(verbatim: L10n.text("app.settings.chrome_restart"))
                                .font(.callout).foregroundColor(.secondary)
                            if browser.state?.forced == true {
                                Text(verbatim: L10n.text("app.browser.forced")).font(.callout).foregroundColor(.secondary)
                            }
                            if let error = browser.error { errorText(error) }
                        }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
                    } label: { Text(verbatim: L10n.text("app.browser.chrome")) }
                    GroupBox {
                        VStack(alignment: .leading, spacing: 10) {
                            Toggle(L10n.text("app.ui.open_menu_bar_app_at_login"), isOn: Binding(
                                get: { model.appLogin }, set: { value in Task { await model.setAppLogin(value) } }))
                                .disabled(busy)
                            Toggle(L10n.text("app.ui.run_engine_at_login"), isOn: Binding(
                                get: { model.engineLogin == true }, set: { value in Task { await model.setEngineLogin(value) } }))
                                .disabled(busy || model.engineLogin == nil)
                            if model.busy { ProgressView().controlSize(.small) }
                            if let error = model.loginError { errorText(error) }
                        }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
                    } label: { Text(verbatim: L10n.text("app.ui.login")) }
                }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
            }.frame(width: geometry.size.width, height: geometry.size.height, alignment: .topLeading)
        }.nativeFormControls()
    }

    private func applyLANAddress() {
        guard !model.busy && !browser.busy && lan.canApplyAddress else { return }
        Task { await lan.applyAddress() }
    }

    private func errorText(_ value: String) -> some View {
        Text(verbatim: value).font(.callout).foregroundColor(.red).textSelection(.enabled)
    }
}
