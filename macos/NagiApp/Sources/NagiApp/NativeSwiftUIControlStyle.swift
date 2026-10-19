import AppKit
import SwiftUI

extension View {
    /// Native form sizing shared by capture and rewrite windows and sheets.
    func nativeFormControls() -> some View {
        controlSize(.regular)
            .textFieldStyle(.roundedBorder)
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
