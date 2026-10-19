import AppKit
import XCTest

/// In-process fixture access; this never grants desktop Accessibility permission.
@MainActor enum NativeAccessibilityFixture {
    static func enable() -> () -> Void {
        _ = NSApplication.shared
        let attribute = NSAccessibility.Attribute(rawValue: "AXEnhancedUserInterface")
        let previous = NSApp.accessibilityAttributeValue(attribute) ?? false
        NSApp.accessibilitySetValue(true, forAttribute: attribute)
        return { NSApp.accessibilitySetValue(previous, forAttribute: attribute) }
    }

    // SwiftUI virtual nodes implement these Objective-C selectors without
    // declaring NSAccessibilityProtocol conformance. Native controls use them too.
    static func buttons(in element: NSObject) -> [NSObject] {
        let role = element.responds(to: NSSelectorFromString("accessibilityRole"))
            ? element.value(forKey: "accessibilityRole") as? String : nil
        let own = role == NSAccessibility.Role.button.rawValue ? [element] : []
        let children = element.responds(to: NSSelectorFromString("accessibilityChildren"))
            ? element.value(forKey: "accessibilityChildren") as? [NSObject] ?? [] : []
        return own + children.flatMap { buttons(in: $0) }
    }

    static func snapshot(_ view: NSView, name: String) throws {
        guard let directory = ProcessInfo.processInfo.environment["NAGI_NATIVE_SCREENSHOTS"] else { return }
        let url = URL(fileURLWithPath: directory, isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        view.layoutSubtreeIfNeeded()
        let bitmap = try XCTUnwrap(view.bitmapImageRepForCachingDisplay(in: view.bounds))
        view.cacheDisplay(in: view.bounds, to: bitmap)
        // Composite the transparent native view over its selected window background.
        let composed = try XCTUnwrap(NSBitmapImageRep(bitmapDataPlanes: nil,
            pixelsWide: bitmap.pixelsWide, pixelsHigh: bitmap.pixelsHigh,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true,
            isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0))
        let context = try XCTUnwrap(NSGraphicsContext(bitmapImageRep: composed))
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        view.effectiveAppearance.performAsCurrentDrawingAppearance {
            NSColor.windowBackgroundColor.setFill()
            NSRect(x: 0, y: 0, width: composed.pixelsWide, height: composed.pixelsHigh).fill()
            bitmap.draw(in: NSRect(x: 0, y: 0, width: composed.pixelsWide, height: composed.pixelsHigh))
        }
        NSGraphicsContext.restoreGraphicsState()
        let data = try XCTUnwrap(composed.representation(using: .png, properties: [:]))
        let image = url.appendingPathComponent(name + ".png")
        try data.write(to: image)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: image.path)
    }
}
