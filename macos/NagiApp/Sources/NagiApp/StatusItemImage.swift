import AppKit

/// Template artwork shared by the icon-only and traffic-rate presentations.
@MainActor enum StatusItemImage {
    private static let symbol: NSImage? = {
        // SwiftPM's generated accessor also embeds a build-directory fallback.
        // Resolve the relocated resource bundle first when running inside Nagi.app.
        let packagedURL = Bundle.main.resourceURL?.appendingPathComponent("NagiApp_NagiApp.bundle")
        let resources = packagedURL.flatMap { Bundle(url: $0) } ?? Bundle.module
        guard let url = resources.url(forResource: "NagiMenuTemplate", withExtension: "pdf"),
              let image = NSImage(contentsOf: url) else { return nil }
        image.size = NSSize(width: 22, height: 18)
        image.isTemplate = true
        return image
    }()

    static func make(rates: String?, opacity: CGFloat) -> NSImage? {
        guard let symbol else { return nil }
        let image: NSImage
        if let rates {
            let paragraph = NSMutableParagraphStyle()
            paragraph.alignment = .right
            let attributes: [NSAttributedString.Key: Any] = [
                .font: NSFont.systemFont(ofSize: 9, weight: .regular),
                .foregroundColor: NSColor.black, .paragraphStyle: paragraph
            ]
            let rows = rates.split(separator: "\n").map(String.init)
            let rateX = symbol.size.width + 2
            // Preserve the original item width independently of the displayed font.
            let widthAttributes: [NSAttributedString.Key: Any] = [
                .font: NSFont.monospacedSystemFont(ofSize: 9, weight: .regular), .kern: -1.2
            ]
            let rateWidth = ("999 KB/s" as NSString).size(withAttributes: widthAttributes).width
            let size = NSSize(width: rateX + rateWidth, height: 22)
            image = NSImage(size: size, flipped: false) { _ in
                symbol.draw(in: NSRect(x: 0, y: (size.height - 18) / 2, width: 22, height: 18),
                    from: .zero, operation: .sourceOver, fraction: opacity)
                for (index, row) in rows.prefix(2).enumerated() {
                    let rate = row.trimmingCharacters(in: .whitespaces) as NSString
                    let y: CGFloat = index == 0 ? 10 : 1
                    rate.draw(in: NSRect(x: rateX, y: y, width: rateWidth, height: 11),
                        withAttributes: attributes)
                }
                return true
            }
        } else {
            image = NSImage(size: symbol.size, flipped: false) { rect in
                symbol.draw(in: rect, from: .zero, operation: .sourceOver, fraction: opacity)
                return true
            }
        }
        // AppKit colors both the symbol and rate text for the menu-bar appearance.
        image.isTemplate = true
        return image
    }
}
