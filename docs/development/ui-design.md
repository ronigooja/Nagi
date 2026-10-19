# UI design rule

Apply this rule when adding or changing Nagi's native macOS interface or interactive terminal menu.

Make operations self-explanatory through clear naming, layout, and interaction. Do not fill interfaces with purpose explanations or tutorials by default; provide necessary help on demand.

For the macOS interface, use native controls with regular system sizing. Keep comparable inputs, pop-up selectors, and action buttons consistent within and across forms, using intrinsic control heights and aligned rows. Preserve native shape differences for search fields, checkboxes, help buttons, and other specialized controls; do not impose one corner radius on every control.

Use the shared AppKit and SwiftUI control-style helpers for new forms. Multiline SwiftUI text surfaces use the system text background and separator colors, a 4-point clipped corner radius, and a border that does not intercept input. Keep editor scrolling, focus, selection, accessibility labels, and keyboard actions intact. Grouped content containers may retain their separate 6-point radius.

SwiftUI forms use the shared disclosure style in `nativeFormControls()`: both the arrow and title toggle the section, while the remaining header space does not. Preserve keyboard activation, expanded/collapsed accessibility state, and independent interaction with the expanded content.

Check changed forms on macOS in light and dark appearances, including focused and disabled controls, multiline scrolling, and the minimum supported window size. Compilation and automated tests do not replace visual and interactive acceptance.
