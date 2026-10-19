import AppKit

/// Frame sizes include window chrome; visibleFrame excludes the menu bar and Dock.
struct NativeWindowLayout {
    let preferredSize: NSSize
    let minimumSize: NSSize

    func initialFrame(in screen: NSRect) -> NSRect {
        let size = NSSize(width: min(preferredSize.width, screen.width * 0.9),
                          height: min(preferredSize.height, screen.height * 0.9))
        return NSRect(x: screen.midX - size.width / 2, y: screen.midY - size.height / 2,
                      width: size.width, height: size.height)
    }

    func minimumFrameSize(in screen: NSRect) -> NSSize {
        NSSize(width: min(minimumSize.width, preferredSize.width, screen.width * 0.8),
               height: min(minimumSize.height, preferredSize.height, screen.height * 0.8))
    }

    @MainActor func apply(to window: NSWindow) {
        guard let screen = window.screen?.visibleFrame ?? NSScreen.main?.visibleFrame else {
            window.minSize = minimumSize
            window.center()
            return
        }
        if let native = window as? NativeWindow { native.minimumFrameSize = minimumFrameSize(in: screen) }
        window.minSize = minimumFrameSize(in: screen)
        window.setFrame(initialFrame(in: screen), display: false)
    }
}

/// SwiftUI can reset contentMinSize while realizing a hosting view or its
/// accessibility tree. Keep the AppKit-owned minimum independent of that hint.
@MainActor class NativeWindow: NSWindow {
    var minimumFrameSize: NSSize = .zero {
        didSet { super.minSize = minimumFrameSize }
    }
    override var minSize: NSSize {
        get {
            NSSize(width: max(super.minSize.width, minimumFrameSize.width),
                   height: max(super.minSize.height, minimumFrameSize.height))
        }
        set {
            super.minSize = NSSize(width: max(newValue.width, minimumFrameSize.width),
                                   height: max(newValue.height, minimumFrameSize.height))
        }
    }
    override var contentMinSize: NSSize {
        get { super.contentMinSize }
        set {
            let minimum = contentRect(forFrameRect: NSRect(origin: .zero, size: minimumFrameSize)).size
            super.contentMinSize = NSSize(width: max(newValue.width, minimum.width),
                                          height: max(newValue.height, minimum.height))
        }
    }
}
