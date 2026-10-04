# Nagi Architecture

This document describes the implemented system as of October 2026. Nagi is a macOS and Linux CLI that manages a locally installed mihomo process. A macOS SwiftUI app covers common operations by invoking the CLI. Future release work is marked separately below.

## System boundary

```text
macOS SwiftUI app
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

Nagi manages lifecycle, configuration, subscriptions, proxy selection, and status. mihomo implements proxy protocols, DNS, rules, and connections. The app does not read configuration files, use the socket, or call mihomo directly. Its fixed command path and supported screens are documented in [the app README](macos/NagiApp/README.md).

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
| `internal/diagnostic` | Read-only inspection of paths, executable, selected profile, PID, and control API. |
| `internal/service` | Per-user launchd and systemd service installation. |
| `internal/traffic` | OS proxy settings, restore journal, and engine exit watcher. |
| `internal/tui` | Terminal dashboard state, rendering, and key handling. |

`engine` does not make REST requests. `control` does not start processes. The CLI selects the active profile, constructs the engine and control clients, and connects profile reloads to the mihomo configuration API. See the [runtime design](docs/design/runtime.md) for state and recovery details.

DNS policy is stored in user-owned profile overrides. The CLI validates a generated effective profile before activation and uses the private control API for DNS queries and cache clearing. TUN DNS interception is a mihomo configuration option; Nagi does not itself manage operating-system DNS servers or verify system-wide route capture. See the [DNS reference](docs/reference/cli.md#dns-policy-and-leak-checks).

## Interfaces and data

The CLI is the sole full-featured entry point. All commands accept `--json`; JSON envelopes and error codes form the app integration interface. The precise commands and output rules are in the [CLI reference](docs/reference/cli.md). The CLI also provides read-only diagnostics, generated shell completion, proxy search and latency tests, saved proxy selections, connection closure, and runtime mode changes. Runtime mode changes use the control API and do not modify stored profiles. The app polls status using short-lived commands. An event stream is not implemented.

Nagi uses a private Unix Socket by default. Its default profile does not expose a TCP controller. Profiles, subscription metadata, cache, runtime files, and logs occupy separate per-user directories. The path rules and write behavior are in the [runtime design](docs/design/runtime.md). Supported YAML and proxy URI subscriptions can be explicitly converted and applied as profiles; the [CLI reference](docs/reference/cli.md) defines formats and merge behavior.

## Services, builds, and validation

`nagi service install` creates a user launchd Agent on macOS or a `systemd --user` unit on Linux. The service invokes the CLI; the app never invokes a service manager. Locked local and fetched builds are described in [Build and test](docs/development/build-and-test.md).

Go tests cover runtime paths, lifecycle state, profile writes and rollback, subscriptions, proxy client behavior, and the Unix Socket client. [Build and test](docs/development/build-and-test.md) describes a manual runtime integration check with a built mihomo executable for start, status, configuration validation, proxy groups, connections, and stop. CI runs a locked build and unit tests on Linux and macOS, but does not run that manual check. The release script cross compiles four OS and architecture targets; on macOS it can combine and optionally sign and notarize the CLI and mihomo binaries. The macOS app currently has no verified build in the Linux development environment. App packaging and CI release publication have not been implemented.

## Long-term constraints

Nagi keeps a pinned mihomo source commit, uses a Unix Socket for local control, and keeps mihomo core changes in the separate mihomo repository. The CLI owns business behavior and the macOS app uses only its JSON interface. These constraints apply to future features as well as current code.
