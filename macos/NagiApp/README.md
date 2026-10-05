# Nagi macOS app

This SwiftUI app provides runtime status, lifecycle controls, profile switching, proxy selection, subscription refresh, connections, recent logs, and app settings. It requires macOS 13 or later.

Install the Nagi CLI as an executable at `/usr/local/bin/nagi` before opening the app. Install the matching pinned mihomo executable at `/usr/local/bin/mihomo`, or set `NAGI_MIHOMO_BIN` for the app process. The CLI path is fixed. The app invokes only `nagi --json` commands through `Process`; it does not access mihomo, its API or socket, or Nagi configuration files directly.

On macOS with Xcode command line tools installed, build with:

```sh
cd macos/NagiApp
swift build -c release
swift run NagiApp
```

The app expects the CLI JSON envelope `{ "ok": true, "data": ... }` or `{ "ok": false, "error": { "code": "...", "message": "..." } }`. The Settings page controls only the app's status polling interval.
