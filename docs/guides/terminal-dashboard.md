# Terminal dashboard

This guide explains how to use Nagi's interactive terminal dashboard. It requires a terminal on standard input and output. Run `nagi tui` after installing the CLI; a running mihomo instance is optional for opening the dashboard, but proxy and mode actions need one.

The first screen shows the engine state, selected profile, runtime mode, DNS status, system proxy status, TUN status, proxy groups, nodes, and saved subscriptions. `unknown` or `unavailable` means Nagi could not verify the value. An empty proxy list suggests starting mihomo or checking its control API with `nagi doctor`. Refresh with `r` after an external change.

| Key | Action |
| --- | --- |
| `q` | Exit. |
| `r` | Refresh the displayed state. |
| `n`, `N` | Move to the next or previous proxy group. |
| `j`, `k` | Move down or up the visible node list. |
| `/` | Type a case-insensitive node search and press Enter. |
| `c` | Clear the node search. |
| `s`, `d` | Select the highlighted node or test its latency. |
| `[`, `]` | Move to the previous or next subscription. |
| `u`, `a` | Download the selected subscription or apply its cached configuration. |
| `m` | Cycle the runtime mode through rule, global, and direct. |
| `p`, `t` | Toggle system proxy or TUN when the corresponding CLI control is available. |
| `l` | Show or hide the last 20 log lines. |

Downloading a subscription only updates its cache. Applying it validates and selects the cached YAML profile, and reloads a running engine. Errors appear in the dashboard; refresh after resolving them. The dashboard does not start mihomo automatically. Use `nagi start` if needed.

The dashboard uses the same operations as the CLI. For scripts and the macOS app, continue using `nagi --json` commands. `nagi --json tui` is a usage error because the dashboard requires a terminal.
