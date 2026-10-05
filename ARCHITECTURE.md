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

Nagi manages lifecycle, configuration, subscriptions, proxy selection, and status. mihomo implements proxy protocols, DNS, rules, and connections. The app does not read configuration files, use the socket, or call mihomo directly. Its fixed command path and menu controls are documented in [the app README](macos/NagiApp/README.md).

The mihomo source belongs in a separate repository. Nagi records the exact source commit in [`engine.lock`](engine.lock); [`scripts/build.sh`](scripts/build.sh) rejects a local checkout at any other commit. The current lock points to the available fork's `main` branch. A dedicated `nagi/meta` branch and upstream `Meta` synchronization workflow remain future repository maintenance work. Nagi contains no mihomo source modifications. The pinning rationale is in [decision 0001](docs/decisions/0001-pin-mihomo-commit.md).

## Modules and dependencies

The executable entry point is `cmd/nagi`. `internal/command` handles help, checks invocation syntax before reading runtime configuration, and assembles calls; `internal/output` owns the CLI response envelope and terminal formatting of feature results. `internal/tui` renders the interactive dashboard; its command adapter invokes the same feature operations as the CLI. Feature code is split by responsibility:

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
| `internal/traffic` | OS proxy settings, restore journal, and engine exit watcher. |
| `internal/tui` | Terminal dashboard state, rendering, and key handling. |

`engine` does not make REST requests. `control` does not start processes. The CLI selects the active profile, constructs the engine and control clients, and connects profile reloads to the mihomo configuration API. See the [runtime design](docs/design/runtime.md) for state and recovery details.

On macOS, an optional root LaunchDaemon helper can own mihomo for TUN access. The CLI still owns user operations and selects the backend explicitly. The helper authenticates the CLI over a Unix socket, snapshots validated configuration, and mediates a restricted set of mihomo controller calls. Its controller socket and log remain root-private. The user-mode backend remains the default. The [CLI reference](docs/reference/cli.md#traffic-access) defines installation and switching.

DNS policy is stored in user-owned profile overrides. The CLI validates a generated effective profile before activation and uses the private control API for DNS queries and cache clearing. TUN DNS interception is a mihomo configuration option. `dns check` inspects the selected configuration and queries mihomo through its private control API. It does not verify operating-system routes, resolver capture, or external DNS leaks. See the [DNS reference](docs/reference/cli.md#dns-policy-and-checks).

## Interfaces and data

All commands accept `--json`; JSON envelopes and error codes form the app integration interface. The precise commands and output rules are in the [CLI reference](docs/reference/cli.md). The CLI also provides read-only diagnostics, generated shell completion, proxy search and latency tests, saved proxy selections, connection inspection and closure, temporary and persistent runtime mode changes, log following, and validated configuration reloads. The app polls status using short-lived commands and reads traffic from the CLI's one-second NDJSON traffic stream.

Nagi uses a private Unix Socket by default. Its default profile does not expose a TCP controller. Profiles, subscription metadata, cache, runtime files, and logs occupy separate per-user directories. The path rules and write behavior are in the [runtime design](docs/design/runtime.md). Supported mihomo YAML subscriptions can be applied as profiles; the [CLI reference](docs/reference/cli.md) defines the format and merge behavior.

## Services, builds, and validation

`nagi service install` creates a user launchd Agent on macOS or a `systemd --user` unit on Linux. The service invokes the CLI; the app never invokes a service manager. Locked local and fetched builds are described in [Build and test](docs/development/build-and-test.md).

Go tests cover runtime paths, lifecycle state, profile writes and rollback, subscriptions, proxy client behavior, and the Unix Socket client. [Build and test](docs/development/build-and-test.md) describes a manual runtime integration check with a built mihomo executable for start, status, configuration validation, proxy groups, connections, and stop. CI runs a locked build and unit tests on Linux and macOS, but does not run that manual check. The release script cross compiles four OS and architecture targets; on macOS it can combine and optionally sign and notarize the CLI and mihomo binaries. A separate [app packaging script](macos/NagiApp/README.md) creates a menu-only bundle. The macOS app currently has no verified build in the Linux development environment. CI release publication has not been implemented.

## Long-term constraints

Nagi keeps a pinned mihomo source commit, uses a Unix Socket for local control, and keeps mihomo core changes in the separate mihomo repository. The macOS app uses the CLI's JSON interface. These constraints apply to future features as well as current code.
