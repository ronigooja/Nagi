# UI design rule

Apply this rule when adding or changing Nagi's native macOS interface or interactive terminal menu.

Make operations self-explanatory through clear naming, layout, and interaction. Do not fill interfaces with purpose explanations or tutorials by default; provide necessary help on demand.

For the macOS interface, use native controls with regular system sizing. Keep comparable inputs, pop-up selectors, and action buttons consistent within and across forms, using intrinsic control heights and aligned rows. Preserve native shape differences for search fields, checkboxes, help buttons, and other specialized controls; do not impose one corner radius on every control.

Use the shared AppKit and SwiftUI control-style helpers for new forms. Multiline SwiftUI text surfaces use the system text background and separator colors, a 4-point clipped corner radius, and a border that does not intercept input. Keep editor scrolling, focus, selection, accessibility labels, and keyboard actions intact. Grouped content containers may retain their separate 6-point radius.

SwiftUI forms use the shared disclosure style in `nativeFormControls()`: both the arrow and title toggle the section, while the remaining header space does not. Preserve keyboard activation, expanded/collapsed accessibility state, and independent interaction with the expanded content.

Check changed forms on macOS in light and dark appearances, including focused and disabled controls, multiline scrolling, and the minimum supported window size. Compilation and automated tests do not replace visual and interactive acceptance.

## Window sizing

For new or changed resizable macOS windows, AppKit owns the window frame. Use
`NativeWindowLayout` in `macos/NagiApp/Sources/NagiApp/NativeWindowLayout.swift`
to center the initial frame within the selected screen's `visibleFrame`, which
excludes the menu bar and Dock. Preferred sizes are upper bounds: cap the initial
frame at 90% of the visible width and height, and cap minimum sizes at 80% so the
window can still shrink on smaller screens. Frame sizes include title bars and
other window chrome. Apply the initial layout after installing the content, only
when creating the window; reopening an existing window preserves its size.

For an AppKit-managed window backed by SwiftUI, disable content-derived window
constraints with `NSHostingView.sizingOptions = []`. Bound dynamic root layouts
to the actual viewport when necessary; long values, expanded sections, tab
changes and errors must not enlarge the window. Sheets also need screen-aware
size bounds. Keep overflowing content scrollable and primary actions reachable.
Lowering a window's minimum size must include checking its internal constraints:
fixed detail panes and table height requirements must allow the smaller size.

Add regression coverage for small visible areas, screens with nonzero or negative
origins, first presentation, shrinking and enlarging, and reopening after resize.
Include long content and state changes when they affect layout. Record macOS
compilation, test and interactive acceptance gaps explicitly; checks performed
on another platform cannot establish that native layout works.
