# UI design rule

Apply this rule when adding or changing Nagi's native macOS interface or interactive terminal menu.

Make operations self-explanatory through clear naming, layout, and interaction. Do not fill interfaces with purpose explanations or tutorials by default; provide necessary help on demand.

Terminal menu actions should identify the operation and its target. The Tailscale
path page includes an explicit **Refresh path status** action for rereading its
current engine snapshot; see the [terminal menu design](../design/terminal-menu.md#node-and-chain-flows).

## Local output and sensitive content

These presentation rules also apply to ordinary CLI output and errors. Local
interfaces serve the user operating their own system; they are not assumed to
be reports intended for sharing.

- Preserve node names, domains, paths, relevant URL details, and actionable error
  causes needed to operate or troubleshoot the system. Do not blanket-mask URLs
  or replace useful failures with generic messages solely because the output is
  visible locally.
- Lists and summaries need not include passwords, private keys, authentication
  tokens, or complete request bodies. Omit or mask actual credential values
  without hiding the surrounding operational context.
- Preserve original content when the user explicitly views configuration,
  logs, capture records, or exports their data. Explain sensitivity where it
  helps the user decide whether to share that content.
- Redact diagnostic reports intended for sharing by default. Keep this separate
  from local operation and explicit raw exports; do not add reveal steps or
  confirmations to routine local use merely to enforce sharing precautions.

The CLI reference documents the implemented local formatter and the stricter
URL handling used for shareable diagnostic reports. The local formatter masks
URL userinfo and values in query strings or fragments whose names identify
common credentials; it is not a general secret detector, so known sensitive
summary fields must still be omitted at their source. Shareable diagnostics
retain at most a URL origin.

## Native controls

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
