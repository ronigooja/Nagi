# Nagi Feature Roadmap

This document audits the daily-use capability targets. The [CLI reference](../reference/cli.md) is authoritative for implemented commands and their limits. Items explicitly labeled remaining below are not implemented.

## TUI

The terminal dashboard is implemented through `nagi tui`; the [guide](../guides/terminal-dashboard.md) defines keys and current behavior. It covers:

- current engine status, selected profile, proxy mode, and DNS status;
- proxy group and node browsing;
- node search, filtering, latency tests, and selection;
- system proxy and TUN controls;
- subscription update and apply actions;
- logs, errors, loading states, and empty states;
- refresh, navigation, and exit key bindings.

The existing CLI remains the interface for scripts, automation, and the macOS app.

## Subscription management

Implemented CLI behavior covers the actions below within the formats and preservation limits in the [CLI reference](../reference/cli.md). The TUI can update and apply a selected subscription.

- Add, remove, update, and apply subscriptions.
- Support common subscription formats in addition to complete mihomo YAML.
- Show changes before applying an update, including added, removed, and changed nodes.
- Show refresh time, expiry time, and traffic usage when the subscription provides them.
- Keep the previous working configuration when an update fails.
- Preserve user node selections, rules, and local settings after updates.

## Profiles and local configuration

The CLI supports source profile import, export, switch, remove, validation, backup, restore, line and structural differences with conflict warnings, and separate local YAML overrides. See the [CLI reference](../reference/cli.md#local-profile-files-and-overrides) for the implemented contract.

- Compare YAML paths, named proxies and groups, ordering, and important configuration hazards before applying another version.

## Proxy groups and nodes

The CLI implements the items below as defined in the [CLI reference](../reference/cli.md); the TUI also browses groups and nodes, searches and tests nodes, and selects them.

- List groups, nodes, and the current selection.
- Search nodes and provide dynamic completion.
- Test multiple nodes and sort results by latency.
- Report clearly when a selected node is unavailable or removed.
- Persist proxy-group selections where possible.
- Show connection details, including the selected proxy chain.

## Traffic access

Implemented CLI commands and scope are in the [CLI reference](../reference/cli.md#traffic-access). TUN status checks an explicitly named OS adapter when mihomo exposes one, but an up adapter does not prove traffic capture. The remaining platform and interception limits are documented there and in the [traffic access guide](../guides/traffic-access.md).

- Enable, disable, and report the system proxy.
- Enable, disable, and report TUN mode.
- Show HTTP, HTTPS, and SOCKS port status.
- Provide an explicit LAN access switch and listening address.
- Explain which traffic is intercepted and which traffic is not.
- Restore system network settings after exit or an abnormal shutdown.

## DNS and leak prevention

The CLI now creates default profiles with IPv4/IPv6 mihomo DNS and DoH. Selected profiles can configure multiple custom DoH upstreams, direct or proxy-node routing, explicit domain exceptions, TUN DNS hijack, cache flushing, and mihomo DNS queries. Failure of all configured upstreams is reported by mihomo without a configured plaintext fallback. See the [DNS CLI reference](../reference/cli.md#dns-policy-and-leak-checks).

Remaining: verify TUN route ownership and system resolver behavior on supported macOS and Linux installations, and add an external DNS leak test that distinguishes applications captured by TUN from those using another network path. The current `dns check` does not make either claim.

## Rules

Implemented CLI commands and persistence behavior are in the [CLI reference](../reference/cli.md#rules) and [rules guide](../guides/rules.md).

- View active rules and rule providers.
- Add, remove, enable, and disable custom rules.
- Import local and remote rule sets.
- Route matches to direct, a proxy group, or reject.
- Report rule order and conflicts.
- Show the rule matched by a connection.
- Preserve custom rules across subscription updates.

## Runtime control

The CLI implements the runtime-control items below as defined in the [CLI reference](../reference/cli.md). The TUI also cycles the runtime mode.

- Switch between `rule`, `global`, and `direct` modes.
- Distinguish temporary runtime changes from persistent settings.
- List, inspect, and close active connections.
- Close all active connections.
- Follow logs in real time.
- Reload the running configuration safely.

## Startup and background operation

Implemented lifecycle, startup, and stale-state behavior is documented in the [CLI reference](../reference/cli.md#startup-and-background-operation) and [startup guide](../guides/startup.md).

- Start, stop, and restart the engine.
- Start automatically at login.
- Show service status in addition to installing and uninstalling the service.
- Prevent duplicate starts and distinguish stale state from a live engine.

- Recover after an unexpected engine exit while the login service is enabled.
- Recheck process, control API, and managed system proxy state periodically, including after network changes, sleep, and wake. Reassert only system proxy settings that match the saved baseline; report conflicting or new macOS network services.

Remaining: the periodic check does not verify OS route ownership, resolver capture, or restored connectivity. A live process whose control API is unreachable needs manual inspection.

## Diagnostics and security

Implemented checks, redaction, and kill-switch capability boundaries are documented in the [CLI reference](../reference/cli.md#diagnostics-and-security) and [diagnostics guide](../guides/diagnostics.md).

- Check the mihomo executable, configuration, ports, Unix Socket, DNS, and proxy connectivity.
- Distinguish a live process from a reachable control API and a working proxy path.
- Export a redacted diagnostic report.
- Protect configuration files and subscription credentials.
- Check Unix Socket and configuration-file permissions.
- Warn when a control interface is exposed beyond the local machine.
- Provide a kill switch that blocks direct traffic when proxy or DNS protection fails.

## CLI and integration contract

Implemented CLI envelope, generated shell completion, local-name candidates, and error handling are specified in the [CLI reference](../reference/cli.md) and [CLI usability rules](cli-usability.md).

- Expose core operations through CLI commands as well as the TUI.
- Keep the `--json` interface stable for scripts and the macOS app.
- Provide shell completion for supported commands and local resources.
- Keep errors, exit codes, recovery steps, and sensitive-data handling consistent.

## Scope guidance

The TUI, subscription flow, node selection, traffic access, DNS protection, rules, persistence, startup recovery, and diagnostics form the core daily-use scope. Notifications, multi-instance management, scheduled benchmarking, complex network scenes, and detailed traffic graphs can be considered after this scope is complete.
