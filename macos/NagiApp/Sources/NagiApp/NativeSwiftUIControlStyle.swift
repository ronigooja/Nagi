import AppKit
import SwiftUI

extension View {
    /// Native form sizing shared by capture and rewrite windows and sheets.
    func nativeFormControls() -> some View {
        controlSize(.regular)
            .textFieldStyle(.roundedBorder)
            .disclosureGroupStyle(NativeDisclosureGroupStyle())
    }

    /// Keep multiline input and captured text surfaces consistent with native colors.
    func nativeTextSurface() -> some View {
        background(Color(nsColor: .textBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay {
                RoundedRectangle(cornerRadius: 4)
                    .strokeBorder(Color(nsColor: .separatorColor))
                    .allowsHitTesting(false)
            }
    }
}

/// Let the arrow and title toggle a disclosure without extending its hit area
/// into the remaining header space or the editable content below it.
private struct NativeDisclosureGroupStyle: DisclosureGroupStyle {
    func makeBody(configuration: Configuration) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                configuration.isExpanded.toggle()
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: configuration.isExpanded ? "chevron.down" : "chevron.right")
                        .font(.caption).foregroundColor(.secondary).frame(width: 12)
                        .accessibilityHidden(true)
                    configuration.label
                }
                .padding(.vertical, 4)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(configuration.isExpanded ? L10n.text("app.ui.expanded") : L10n.text("app.ui.collapsed"))
            if configuration.isExpanded {
                configuration.content.padding(.leading, 18)
            }
        }
    }
}
