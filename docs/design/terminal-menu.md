# Terminal menu design

**Status:** implemented.

This document describes the current `nagi menu` page structure, command boundary, and failure behavior. The [CLI reference](../reference/cli.md#terminal-menu) defines its user-visible keys, terminal requirements, and exit codes.

## Responsibility and boundary

The menu is a line-oriented terminal interface that preserves scrollback. It owns page navigation, numeric selection, input prompts, confirmation, pagination, and display. `internal/command` still owns operations and validates command arguments. The menu does not read profile files, call mihomo, or change OS settings directly.

The `internal/command` entry point checks syntax, rejects `--json menu`, and requires terminal input and output before creating a menu session. The menu invokes ordinary commands through a runner that supplies `--json` and decodes their success or failure envelope. Most commands run in process. `logs follow` and `traffic watch` run in a child Nagi process with an interruptible context. Ctrl-C cancels the child stream and returns to the menu page. The menu prints stream text directly. The user-mode mihomo process starts in a separate process group, so the terminal's Ctrl-C does not signal it when a stream is canceled.

The macOS app continues to invoke ordinary CLI commands with JSON. The menu is not a separate business API. The command syntax, data fields, errors, and operation effects are defined in the [CLI reference](../reference/cli.md).

## Page structure

The root page reads `status` for a short mihomo and selected-profile summary. If that read fails, it shows the error and continues to offer its ten categories. All ten fit on the first page with the current page size.

| Root category | Implemented menu flows |
| --- | --- |
| Runtime status | Status, start, stop, restart, configuration show/validate/reload, temporary and saved routing mode, full `quit`. |
| Proxies and groups | Group list and detail, numbered node selection, name search, node and group latency checks, saved selection restore. |
| Profiles and subscriptions | Profile list/import and per-profile use, export, backup, restore, diff, override show/set/clear, and removal; subscription list/add and per-subscription update, preview, apply, and removal. |
| DNS | Status, configuration/API check, direct or proxy DoH upstreams, exception add/remove, TUN DNS on/off, query, and cache flush. |
| Rules | Active rules/providers, custom rules, conflict report, matched connection rule, add/remove/enable/disable, local and remote imports. |
| Traffic access | System proxy, TUN, listener ports, and LAN status or changes. |
| Connections | List, show one, close one, or close all. |
| Logs and traffic | Recent logs, follow logs, and watch traffic. |
| Service and startup | Service install/uninstall/status, startup status/enable/disable/check, and stale-marker recovery. |
| Diagnostics and recovery | Doctor, redacted diagnostics and export, version, macOS privileged-helper operations, and a shell completion page for Bash/Zsh/Fish scripts and candidate discovery. |

The menu does not currently expose every argument variant of every command. The CLI remains the complete interface for advanced configuration and automation. For example, the proxy latency menu uses the command's default URL and timeout; users can supply custom values with `nagi proxy delay` or `nagi proxy delays`.

Profile, subscription, proxy group, node, rule, and connection choices come from existing read commands. Dynamic items are selected by number, so names with spaces remain one argument to the underlying command. Inputs for a new name, file, URL, domain, address, or rule value are prompted as text and then validated by the command layer. Subscription lists omit URLs. The detail page shows the cached preview's added, removed, and changed node names and offers Apply only when the preview read succeeds. A failed preview leaves Update, Preview, and Remove available. Update prints the downloaded byte count and cache changes; URL-shaped node names in previews and updates are replaced with `[redacted URL]`. The menu states that `dns check` covers configuration and mihomo's API, and that TUN settings or an up adapter do not prove route or DNS interception.

## Navigation and state

Each page loads a view when opened, after an action, on `r`, or when returning to it with `b`. The view contains context text, numbered items, and an optional empty-state explanation. Lists display ten items per page; `n` and `p` change the visible slice, and numbering restarts at `1` on each slice. `b` pops the current page; `q` leaves from any page. A blank choice repeats the page, while an invalid choice prints a correction and retains the page. `h` runs the ordinary help command associated with the page. Text prompts accept `b` to cancel that flow and `q` to leave the menu.

Broad or destructive actions such as stopping mihomo, restoring a profile backup, applying or removing a subscription, removing a rule, disabling traffic access, and closing all connections require the literal answer `yes`. Another answer cancels the action. The menu keeps `quit` separate from leaving the menu: `quit` stops this session's login monitor and mihomo after confirmation, while `q` only closes the interactive interface.

On a command failure, the runner returns the CLI error message. The page displays it and remains available, then refreshes its read data. A failed page load replaces the prior view, clearing stale numbered choices; it displays an error and, when there are no items, an unavailable-data message. The user can retry with `r` or return with `b`. URL-shaped text in menu errors is replaced with `[redacted URL]`. A failed menu action does not become the menu process's exit status. Unexpected input errors exit `1`; ordinary exit and prompt interruption exit `0`. The exact terminal and usage contract is in the [CLI reference](../reference/cli.md#terminal-menu).

## Implementation and verification

`internal/menu/menu.go` owns the session loop, page stack, root registration, terminal check, and interruption. `input.go` owns prompts, confirmations, and numeric selection; `render.go` prints pages and command results; `runner.go` adapts command JSON envelopes and child streams. Feature files (`status.go`, `proxy.go`, `profile.go`, `dns.go`, `rules.go`, `traffic.go`, and `diagnostics.go`) build page views and command arguments. The diagnostics page includes the service, startup, helper, and completion flows. Privileged-helper actions are registered there only on macOS.

Menu tests use an injected runner and input/output buffers to check command mapping, navigation, pagination, confirmation, failed refreshes, subscription preview gating, and URL redaction without a live mihomo process. Command tests cover terminal rejection and help. A real-terminal check started user-mode mihomo from the menu, canceled both `logs follow` and `traffic watch` with Ctrl-C, confirmed the menu returned to the Logs and traffic page and mihomo was still running, then stopped it from the menu. Ordinary command tests remain authoritative for profile, mihomo, and OS operation effects.
