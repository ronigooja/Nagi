import AppKit

/// Keep native controls at the same system size without overriding their shapes.
@MainActor enum NativeControlStyle {
    static func configure(_ control: NSControl) {
        control.controlSize = .regular
        control.font = .systemFont(ofSize: NSFont.systemFontSize)
        if let popup = control as? NSPopUpButton {
            popup.bezelStyle = .rounded
        } else if let button = control as? NSButton {
            button.bezelStyle = .rounded
        } else if let field = control as? NSTextField {
            field.isBezeled = true
            field.bezelStyle = .roundedBezel
        }
    }

    /// Alert accessories share their existing label and input column widths.
    static func form(_ fields: [(String, NSTextField)]) -> NSStackView {
        let form = NSStackView()
        form.orientation = .vertical
        form.alignment = .leading
        form.spacing = 10
        for (title, field) in fields {
            configure(field)
            let label = NSTextField(labelWithString: title)
            label.font = .systemFont(ofSize: NSFont.systemFontSize)
            let row = NSStackView(views: [label, field])
            row.orientation = .horizontal
            row.alignment = .firstBaseline
            row.spacing = 5
            label.widthAnchor.constraint(equalToConstant: 85).isActive = true
            field.widthAnchor.constraint(equalToConstant: 310).isActive = true
            form.addArrangedSubview(row)
        }
        form.setFrameSize(form.fittingSize)
        form.layoutSubtreeIfNeeded()
        return form
    }
}
