# Nagi Architecture

This document describes the implemented system as of October 2026. Nagi is a macOS and Linux CLI that manages a locally installed mihomo process. A macOS menu bar app invokes the CLI. Future release work is marked separately below.

## System boundary

```text
macOS menu bar app
       | Process + JSON
       v
Nagi CLI (macOS / Linux)
       |       |       |
       |       |       +-- profile and subscription storage
       |       +---------- process, PID, logs, service manager
       +------------------ REST API over private Unix Socket
                                      |
                                      v
                                  mihomo Meta
```

An interactive terminal menu is another entry point into the CLI. It presents pages and invokes existing commands through their JSON results; it does not own proxy, profile, or lifecycle operations. See the [terminal menu design](docs/design/terminal-menu.md) for its responsibilities and failure behavior.

Nagi manages lifecycle, configuration, subscriptions, proxy selection, and status. Tailscale management requires the [matching pinned engine](docs/guides/tailscale.md#engine-compatibility). It configures Tailscale exits and node/provider front-proxy bindings; mihomo owns Tailscale identity, login, exit selection, and forwarding. mihomo implements proxy protocols, DNS, rules, and connections. The app does not read configuration files, use the socket, or call mihomo directly. Its fixed command path and menu controls are documented in [the app README](macos/NagiApp/README.md).

The mihomo source belongs in a separate repository. Nagi records the exact source commit in [`engine.lock`](engine.lock); [`scripts/build.sh`](scripts/build.sh) rejects a local checkout at any other commit. The current lock points to the available fork's `main` branch. A dedicated `nagi/meta` branch and upstream `Meta` synchronization workflow remain future repository maintenance work. Nagi contains no mihomo source modifications. The pinning rationale is in [decision 0001](docs/decisions/0001-pin-mihomo-commit.md).

## Modules and dependencies

The executable entry point is `cmd/nagi`. `internal/command` handles help, checks invocation syntax before reading runtime configuration, and assembles calls; `internal/output` owns the CLI response envelope and terminal formatting of feature results. Feature code is split by responsibility:

| Package | Responsibility |
| --- | --- |
| `internal/runtime`, `internal/platform` | Per-user paths, directory permissions, and OS process operations. |
| `internal/engine` | Validate, start, stop, restart, and inspect mihomo and its logs. |
| `internal/control` | mihomo REST requests through its Unix Socket. |
| `internal/profile` | Source profiles, local overrides, current selection, secure replacement, and rollback. |
| `internal/subscription` | Subscription records, fetching, and cache. |
| `internal/proxy` | Group, node, connection, and matched-rule operations through `control`. |
| `internal/rules` | Persistent custom rules, imported rule sets, effective overlays, and conflict reports. |
| `internal/diagnostic` | Read-only health, connectivity, permission, exposure, and redacted export checks. |
| `internal/service` | Per-user launchd and systemd service installation and status. |
| `internal/traffic` | OS proxy and macOS TUN DNS settings, restore journals, and engine exit recovery. |
| `internal/menu` | Terminal input, pages, command result presentation, and stream cancellation. |
| `internal/installation` | Verified release downloads, App management, maintenance sequencing, and detached App update launch. |

`engine` does not make REST requests. `control` does not start processes. The CLI selects the active profile, constructs the engine and control clients, and connects profile reloads to the mihomo configuration API. See the [runtime design](docs/design/runtime.md) for state and recovery details.

On macOS, an optional root LaunchDaemon helper can own mihomo for TUN access. The CLI still owns user operations and selects the backend explicitly. The helper authenticates the CLI over a Unix socket, snapshots validated configuration, and mediates a restricted set of mihomo controller calls. Its controller socket and log remain root-private. The user-mode backend remains the default. The [CLI reference](docs/reference/cli.md#traffic-access) defines installation and switching.

Tailscale state is persistent and separate from configuration snapshots. User-mode and privileged identities remain isolated; the helper rewrites root state paths and mihomo enforces restricted policy and adapter handoff. See the [runtime design](docs/design/runtime.md#tailscale-configuration-and-identity) and [state decision](docs/decisions/0006-privileged-tailscale-state.md).

DNS policy is stored in user-owned profile overrides. The CLI validates a generated effective profile before activation and uses the private control API for DNS queries and cache clearing. TUN DNS interception and optional local resolution of proxy destinations are mihomo capabilities. On macOS, Nagi also points network-service DNS settings at the running TUN's DNS endpoints and restores their prior settings when TUN or the engine stops. The privileged backend keeps this restore journal and performs network-service changes inside the authenticated helper. `dns check` inspects the selected configuration and queries mihomo through its private control API. It does not verify operating-system routes, resolver capture, or external DNS leaks. See the [DNS reference](docs/reference/cli.md#dns-policy-and-checks) and the [system DNS decision](docs/decisions/0004-macos-tun-dns.md).

## Interfaces and data

Ordinary commands accept `--json`; JSON envelopes and error codes form the app integration interface. The terminal-only `nagi menu` uses those commands internally and rejects `--json`. The precise commands and output rules are in the [CLI reference](docs/reference/cli.md). The CLI also provides read-only diagnostics, generated shell completion, proxy search and latency tests, saved proxy selections, connection inspection and closure, temporary and persistent runtime mode changes, log following, and validated configuration reloads. The app polls status using short-lived commands and reads traffic from the CLI's one-second NDJSON traffic stream. Its native Connections window uses an independent model to read connection snapshots and request closure through the CLI; see the [connections window design](docs/design/connections-window.md).

Nagi uses a private Unix Socket by default. Its default profile does not expose a TCP controller. Profiles, subscription metadata, cache, runtime files, and logs occupy separate per-user directories. The path rules and write behavior are in the [runtime design](docs/design/runtime.md). Supported mihomo YAML subscriptions can be applied as profiles; the [CLI reference](docs/reference/cli.md) defines the format and replacement behavior.

## Services, builds, and validation

Installed CLI maintenance reuses the transactional artifact installer embedded from `scripts/installer.sh`; whole updates use the verified release's installer. The CLI owns release selection, download verification, service preparation, and recovery. The macOS App requests a detached update through the CLI and exits through its ordinary quit flow; the updater runs the CLI maintenance operation and reopens the App after success. See the [maintenance design](docs/design/installation.md).

`nagi service install` creates a user launchd Agent on macOS or a `systemd --user` unit on Linux. The service invokes the CLI; the app never invokes a service manager. Its login monitor also hosts the per-user subscription scheduler; a foreground `subscription watch` uses the same singleton host lock. Locked local and fetched builds are described in [Build and test](docs/development/build-and-test.md).

Go tests cover runtime paths, lifecycle state, profile writes and rollback, subscriptions, proxy client behavior, and the Unix Socket client. [Build and test](docs/development/build-and-test.md) describes local unit and runtime integration checks. The release script cross compiles four OS and architecture targets, packages fixed-name archives plus `SHA256SUMS` for the GitHub online installer, and on macOS can combine and optionally sign and notarize the CLI and mihomo binaries. `make build app` invokes the [app packaging script](macos/NagiApp/README.md) on macOS to create a menu-only bundle in `bin/`; `make release` includes it in the matching archive when present. The macOS app has verified source compilation, native logic tests, and package generation on macOS; interactive menu-bar acceptance still requires a macOS host. A tag-triggered GitHub Actions workflow publishes release archives and checksums without signing the App.

## Long-term constraints

Nagi keeps a pinned mihomo source commit, uses a Unix Socket for local control, and keeps mihomo core changes in the separate mihomo repository. The macOS app uses the CLI's JSON interface. These constraints apply to future features as well as current code.
