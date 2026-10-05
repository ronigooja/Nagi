# Nagi macOS menu app

The macOS 13+ app lives only in the menu bar. It opens no main window and has no Dock icon. The native menu shows engine status, system proxy and TUN controls, runtime mode, selectable proxy groups, profiles, subscriptions, display choices, startup choices, Help, About, and Quit. It starts Nagi on launch, accepts an already running engine, and keeps the menu available if startup fails so Retry Start can be used. Quit runs the CLI cleanup command before the app terminates; a failed cleanup leaves the menu open with the CLI error.

Install the matching Nagi CLI as an executable at `/usr/local/bin/nagi`. Install the pinned mihomo executable beside the CLI, or provide `NAGI_MIHOMO_BIN` in the app's environment. The app invokes only `nagi --json` commands; it does not read Nagi configuration files or contact mihomo's socket or API. The [CLI reference](../../docs/reference/cli.md) defines commands and JSON fields.

When macOS denies ordinary-user TUN creation, [install and select the privileged CLI backend](../../docs/guides/traffic-access.md). The app uses the selected backend through the same CLI commands, so changing backends does not require rebuilding the app.

The status item displays mihomo's upload and download rates once a second from `nagi --json traffic watch`. Upload and download is the default display; Download only and Icon only are alternatives. The numeric display uses a monospaced font. A dash means the engine is stopped, the stream is unavailable, or the last sample is stale; a valid zero appears as `0 B/s`. The app reconnects the stream after interruption while the engine is running. Display choice is the only app data stored in UserDefaults.

While a menu command is applying a setting and refreshing the displayed state, the status item shows an animated template loading icon and temporarily detaches the menu so it cannot be opened. The animation stops when the command finishes or reports an error.

Subscription Update downloads and validates a cache. Apply cached copy is a separate explicit action that activates it as a profile. Proxy selection is offered only for mihomo Selector groups. A TUN setting can be on while its adapter is absent or down; the menu shows that warning. “Start Nagi at login” installs or uninstalls the CLI's login service; uninstalling removes its service definition and leaves profiles and settings intact. “Open app at login” uses macOS `SMAppService` and requires an installed app bundle.

Build and package on macOS with Xcode command line tools:

```sh
macos/NagiApp/package-app.sh
open macos/NagiApp/dist/Nagi.app
```

The packaging script creates `dist/Nagi.app` with `LSUIElement` set to true. Run the script from the repository root or by absolute path. SwiftPM alone builds an executable, not an app bundle with the menu-only activation setting. Install the built app in Applications before enabling its login item. The app is not signed or notarized by this script, and the repository release script does not package it.
