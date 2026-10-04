# Nagi Feature Roadmap

This document records planned capabilities for making Nagi a complete daily-use proxy client. The items below are proposals and are not implemented capabilities unless they are also listed in the [CLI reference](../reference/cli.md) and verified in the current code.

## TUI

The terminal user interface is a core user-facing feature. It should provide:

- current engine status, selected profile, proxy mode, and DNS status;
- proxy group and node browsing;
- node search, filtering, latency tests, and selection;
- system proxy and TUN controls;
- subscription update and apply actions;
- logs, errors, loading states, and empty states;
- refresh, navigation, and exit key bindings.

The existing CLI remains the interface for scripts, automation, and the macOS app.

## Subscription management

- Add, remove, update, and apply subscriptions.
- Support common subscription formats in addition to complete mihomo YAML.
- Show changes before applying an update, including added, removed, and changed nodes.
- Show refresh time, expiry time, and traffic usage when the subscription provides them.
- Keep the previous working configuration when an update fails.
- Preserve user node selections, rules, and local settings after updates.

## Profiles and local configuration

The CLI now supports source profile import, export, switch, remove, validation, backup, restore, line differences, and separate local YAML overrides. See the [CLI reference](../reference/cli.md#local-profile-files-and-overrides) for the implemented contract.

- Add richer structural differences and conflict warnings for configuration versions.

## Proxy groups and nodes

- List groups, nodes, and the current selection.
- Search nodes and provide dynamic completion.
- Test multiple nodes and sort results by latency.
- Report clearly when a selected node is unavailable or removed.
- Persist proxy-group selections where possible.
- Show connection details, including the selected proxy chain.

## Traffic access

- Enable, disable, and report the system proxy.
- Enable, disable, and report TUN mode.
- Show HTTP, HTTPS, and SOCKS port status.
- Provide an explicit LAN access switch and listening address.
- Explain which traffic is intercepted and which traffic is not.
- Restore system network settings after exit or an abnormal shutdown.

## DNS and leak prevention

- Enable mihomo DNS by default.
- Use DoH by default.
- Allow custom DoH upstreams.
- Support multiple upstreams and failure handling.
- Make the DoH routing policy explicit: direct or through the proxy.
- Do not silently fall back to plaintext system DNS.
- Handle IPv4 and IPv6 consistently.
- Intercept DNS through TUN or an equivalent supported mechanism.
- Clear the DNS cache.
- Provide DNS query diagnostics and leak checks.
- Support explicit exceptions for LAN and corporate domains.

## Rules

- View active rules and rule providers.
- Add, remove, enable, and disable custom rules.
- Import local and remote rule sets.
- Route matches to direct, a proxy group, or reject.
- Report rule order and conflicts.
- Show the rule matched by a connection.
- Preserve custom rules across subscription updates.

## Runtime control

- Switch between `rule`, `global`, and `direct` modes.
- Distinguish temporary runtime changes from persistent settings.
- List, inspect, and close active connections.
- Close all active connections.
- Follow logs in real time.
- Reload the running configuration safely.

## Startup and background operation

- Start, stop, and restart the engine.
- Start automatically at login.
- Show service status in addition to installing and uninstalling the service.
- Recover from unexpected engine exits.
- Recheck or restore state after network changes, sleep, and wake.
- Prevent duplicate starts and distinguish stale state from a live engine.

## Diagnostics and security

- Check the mihomo executable, configuration, ports, Unix Socket, DNS, and proxy connectivity.
- Distinguish a live process from a reachable control API and a working proxy path.
- Export a redacted diagnostic report.
- Protect configuration files and subscription credentials.
- Check Unix Socket and configuration-file permissions.
- Warn when a control interface is exposed beyond the local machine.
- Provide a kill switch that blocks direct traffic when proxy or DNS protection fails.

## CLI and integration contract

- Expose core operations through CLI commands as well as the TUI.
- Keep the `--json` interface stable for scripts and the macOS app.
- Provide shell completion for supported commands and local resources.
- Keep errors, exit codes, recovery steps, and sensitive-data handling consistent.

## Scope guidance

The TUI, subscription flow, node selection, traffic access, DNS protection, rules, persistence, startup recovery, and diagnostics form the core daily-use scope. Notifications, multi-instance management, scheduled benchmarking, complex network scenes, and detailed traffic graphs can be considered after this scope is complete.
