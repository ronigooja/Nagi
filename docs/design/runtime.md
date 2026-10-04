# Runtime and configuration design

This document describes the implemented process and configuration flow. The CLI is the only control layer used by the macOS app.

On macOS, data lives under `~/Library/Application Support/Nagi`: `config/profiles`, `config/settings.yaml`, `config/subscriptions.yaml`, `runtime/mihomo.sock`, `runtime/mihomo.pid`, `runtime/mihomo.lock`, `runtime/logs/mihomo.log`, and `cache/subscriptions`. On Linux, Nagi uses `nagi` under `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, and `XDG_STATE_HOME`. It uses `XDG_RUNTIME_DIR/nagi` for the socket and PID when available, otherwise a runtime directory under its state directory.

`start` validates the selected profile with `mihomo -t`, takes an exclusive runtime lock, launches mihomo with `-ext-ctl-unix`, writes its PID, and waits for the socket. The runtime directory has mode `0700` and the socket has mode `0600`. No TCP controller is configured by Nagi's default profile. `stop` sends SIGTERM and uses SIGKILL after its timeout. Unexpected exits leave a stale PID marker in `status`; recent logs remain available. Nagi does not automatically restart mihomo.

The CLI's control client connects to mihomo's REST API through the Unix Socket for version, configuration, proxy, and connection requests. Profile selection and writes use temporary files, atomic replacement, and a most recent backup. The profile is validated before activation. If reload fails, Nagi restores the previous profile or selection. Subscription URLs are stored in `subscriptions.yaml` with restricted permissions and excluded from list output; refresh writes a separate cache file. Cached subscription data is not automatically imported into the active profile.

The macOS app invokes a fixed `/usr/local/bin/nagi` path through `Process` with `--json`. It does not read runtime files or contact mihomo itself. The app polls status; it has no events stream.
