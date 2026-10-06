# Terminal menu design and implementation plan

**Status:** accepted design; implementation not started.

This document defines the planned `nagi menu` terminal menu. It is the handoff document for future coding agents. The menu is a line-oriented, step-by-step interface for people who do not want to remember commands. It does not use a full-screen TUI, alternate screen buffer, terminal cursor movement, or a visual theme.

## Goals and boundaries

The menu must make common Nagi operations discoverable while remaining simple to maintain. It must work in ordinary terminals, preserve scrollback and copy/paste, and call the existing CLI command implementation. The menu owns navigation, input validation for menu choices, confirmations, pagination, and presentation. Existing command code remains the owner of profile persistence, mihomo control, process lifecycle, DNS, rules, proxy selection, traffic access, and recovery.

The public entry point is `nagi menu`. Existing commands, their JSON envelope, exit codes, and the macOS app integration contract remain unchanged. `--json menu` is invalid because the menu requires an interactive terminal. A non-terminal input or output must produce a usage error that points to the ordinary command or `--json` interface.

The first implementation should cover every existing command through the menu tree below. Long-running operations such as log following and traffic watching run as a temporary child flow and return to the parent page when interrupted.

## Menu tree

The root page displays a compact status summary followed by these sections:

```text
1. Runtime status
2. Proxies and groups
3. Profiles and subscriptions
4. DNS
5. Rules
6. Traffic access
7. Connections
8. Logs and traffic
9. Service and startup
10. Diagnostics and recovery
0. Exit
```

### Runtime status

```text
1. Show status                         status
2. Start mihomo                       start
3. Stop mihomo                        stop
4. Restart mihomo                     restart
5. Show current configuration         config show
6. Validate current configuration     config validate
7. Reload configuration                config reload
8. Change runtime mode                mode
9. Save persistent mode               mode save MODE
0. Back
```

The mode flow offers `rule`, `global`, and `direct`, then distinguishes a temporary change from `mode save MODE`.

### Proxies and groups

```text
1. List proxy groups                  proxy groups
2. Choose a group
3. Search nodes                       proxy search QUERY
4. Test one node                      proxy delay NODE
5. Test all nodes in a group          proxy delays GROUP
6. Restore saved selections           proxy restore
0. Back
```

Choosing a group loads `proxy show GROUP`, displays the current selection and availability status, and offers node selection, latency testing, and search. Names are selected by number from returned data, so users do not need to quote names containing spaces. The final action maps to `proxy select GROUP NODE`.

### Profiles and subscriptions

The profile submenu exposes `profile list`, `profile use`, `profile import`, `profile export`, `profile backup`, `profile restore`, `profile diff`, `profile override show`, `profile override set`, `profile override clear`, and `profile remove`. Profile and subscription names come from read-only list commands or completion candidates; the menu must not scan files independently.

The subscription submenu exposes `subscription list`, `subscription add`, `subscription update`, `subscription preview`, `subscription apply`, and `subscription remove`. Applying or removing a subscription requires confirmation. The update and preview flows show the command's added, removed, and changed node lists before offering apply.

File and URL inputs are explicit text prompts. URL values are never echoed in status summaries or error decorations. The existing output redaction remains the final safety boundary.

### DNS

```text
1. Show DNS status                   dns status
2. Check DNS configuration            dns check
3. Set direct DoH upstreams           dns set direct URL...
4. Set proxy DoH upstreams            dns set proxy NODE URL...
5. Add DNS exception                  dns exception add DOMAIN SERVER
6. Remove DNS exception               dns exception remove DOMAIN
7. Enable TUN DNS interception        dns tun on
8. Disable TUN DNS interception       dns tun off
9. Query a domain                     dns query DOMAIN [A|AAAA]
10. Flush DNS cache                   dns flush
0. Back
```

The menu must state that `dns check` covers configuration and the mihomo API only. It must not describe it as an operating-system DNS leak test.

### Rules

The rules submenu exposes `rules list`, `rules providers`, `rules custom`, `rules conflicts`, `rules connection ID`, `rules add`, `rules remove`, `rules enable`, `rules disable`, `rules import-local`, and `rules import-remote`. It obtains rule names and connection IDs from the corresponding read operations. Add, import, remove, enable, and disable operations confirm when they change persistent state.

### Traffic access

```text
1. System proxy status             system-proxy status
2. Enable system proxy             system-proxy enable
3. Disable system proxy            system-proxy disable
4. TUN status                      tun status
5. Enable TUN                      tun enable
6. Disable TUN                     tun disable
7. Listener ports                  ports status
8. LAN access status               lan status
9. Enable LAN access               lan enable [ADDRESS]
10. Disable LAN access             lan disable
0. Back
```

The menu must preserve the CLI distinction between configured TUN state, adapter state, and verified traffic interception. It must not claim that an enabled adapter proves all traffic is captured.

### Connections

The connections submenu exposes `connections list`, `connections show ID`, `connections close ID`, and `connections close-all`. IDs are selected from a fresh list whenever possible. Closing all connections requires an explicit `yes` confirmation.

### Logs and traffic

The submenu exposes `logs [lines]`, `logs follow`, and `traffic watch`. Follow and watch flows print their existing human output directly. `Ctrl-C` stops the stream and returns to the parent menu without treating cancellation as an operation failure.

### Service and startup

The submenu exposes `service status`, `service install`, `service uninstall`, `startup status`, `startup enable`, `startup disable`, `startup check`, and `recover`. Service removal, startup disabling, and stale-state recovery require confirmation when they may affect a running process or future login behavior.

### Diagnostics and recovery

The submenu exposes `doctor`, `diagnostics`, `diagnostics export FILE`, `version`, and all `privileged-helper status|install|uninstall|enable|disable` operations. Diagnostic reports remain read-only and redacted. Helper installation and removal require confirmation and are shown only on platforms where the existing command supports them.

## Interaction contract

Every page follows the same line-oriented contract:

```text
<page title>
<current context, if any>

1. <action>
2. <action>
b. Back
r. Refresh
h. Show command help
q. Quit menu

Choice:
```

`b` returns to the parent page. `r` reloads data from the command layer. `h` prints the existing command help for the selected page or action. `q` exits cleanly from every page. A blank line repeats the page. An invalid choice prints one short correction and does not lose page state.

Lists use numeric selection and pagination. The default page size is 10. `n` and `p` move between pages, and `s` starts a search when the resource supports a search command. Empty lists explain the likely cause and provide the next command, for example “No proxy groups returned; start mihomo and run `nagi proxy groups`.”

The menu asks for confirmation only for destructive or broad changes: stopping or restarting mihomo, applying or removing subscriptions, deleting profiles or rules, disabling traffic access, closing all connections, changing service or startup state, and managing the privileged helper. Confirmation accepts only `yes`; every other response cancels and returns to the page.

After a successful mutation, the menu prints the command result and a concise next step. After a failure, it prints the existing human error, preserves the current page, and refreshes data before the next selection. The menu must not invent rollback claims or hide an uncertain engine state.

## Technical design

Use the Go standard library for menu I/O: `bufio.Reader`, `io.Writer`, `fmt`, `context`, and `os/signal`. Terminal detection may use the small `golang.org/x/term` package as an explicit dependency, or an equivalent existing platform helper if one is added during implementation. Do not introduce a TUI framework.

Keep the menu in `internal/menu/` with these responsibilities:

```text
internal/menu/
├── menu.go          root loop, page stack, cancellation
├── page.go          page, item, state, and result types
├── input.go         line input, numeric choices, confirmation, pagination
├── render.go        headings, status summaries, errors, and results
├── runner.go        command.Run adapter and JSON envelope decoding
├── status.go        runtime status and mode pages
├── proxy.go         groups, nodes, search, and latency pages
├── profile.go       profiles and subscriptions
├── dns.go           DNS pages
├── rules.go         rules pages
├── traffic.go       system proxy, TUN, ports, LAN, connections, logs
└── diagnostics.go   service, startup, diagnostics, and helper pages
```

The menu layer must call the existing command implementation through a small adapter. It may invoke `command.Run` in-process with `--json`, or use a narrow exported runner interface if the current package boundaries require it. It must not call `internal/control`, `internal/profile`, `internal/engine`, or OS settings packages directly.

The adapter should expose a result shape similar to:

```go
type Result struct {
    Data  any
    Code  int
    Error *CLIError
}

type Runner interface {
    Run(ctx context.Context, args []string) Result
}
```

JSON decoding belongs in the adapter. Page code receives typed or small view models and maps them to menu items. A page action contains command construction and confirmation metadata, but no persistence or mihomo logic.

The root menu obtains its status summary with read-only commands. If status cannot be read, it still shows the menu and reports the failure near the summary; it must not prevent recovery commands such as `stop`, `recover`, `doctor`, or `version` from being selected.

## Implementation phases

The work should be split into independent worktrees with narrow file ownership. Agents must not edit the same files concurrently.

### Phase 1: contract and skeleton

Create the `internal/menu` package, define injectable input/output, page stack behavior, terminal checks, common navigation, confirmation, pagination, and the `nagi menu` dispatch path. Add unit tests using `strings.Reader` and `bytes.Buffer`. Do not implement feature pages yet beyond a placeholder root page.

Acceptance checks: `nagi menu` is rejected for redirected input; `q`, `b`, blank input, invalid input, and `Ctrl-C` behave consistently; existing command tests and JSON output remain unchanged.

### Phase 2: command runner and runtime pages

Implement the JSON runner adapter and pages for status, lifecycle, config validation/show/reload, and mode. Reuse existing output decoding and error codes. Add integration-style tests with a fake runner so pages can be tested without mihomo.

Acceptance checks: start, stop, restart, reload, temporary mode, persistent mode, and read-only status map to the exact existing command arguments and display success and failure states.

### Phase 3: proxy and profile pages

Implement proxy groups, group details, numeric node selection, search, latency actions, saved selection restore, profile list/use/import/export/backup/restore/diff/override/remove, and subscription operations. Add pagination and confirmation flows.

Acceptance checks: names containing spaces are passed as one argument; selected and unavailable nodes are represented from returned fields; subscription preview appears before apply; URLs are not included in ordinary menu summaries.

### Phase 4: DNS, rules, and traffic pages

Implement the DNS, rules, system proxy, TUN, ports, LAN, connections, logs, and traffic pages. Long-running streams must use context cancellation and return to the page on interrupt.

Acceptance checks: every menu action maps to a documented command; destructive actions require confirmation; DNS scope and TUN limitations are stated accurately; stream cancellation returns status zero and does not print a false failure.

### Phase 5: service, diagnostics, polish, and documentation

Implement service/startup/recovery and diagnostic/helper pages. Add root status formatting, consistent empty/error messages, command-help display, platform gating, and end-to-end scripted menu journeys. Update the CLI reference, CLI usability rules, README navigation, and relevant guides after behavior is settled.

Acceptance checks: `go test ./...`, representative interactive journeys with isolated runtime directories, help output, redirected-input behavior, JSON compatibility, and manual link/command review all pass.

## Parallel worktree plan

The parent agent should create one worktree per stream and merge in this order:

1. **menu-core:** Phase 1 skeleton and common input/navigation. Owns `internal/menu/menu.go`, `page.go`, `input.go`, `render.go`, and command dispatch changes.
2. **menu-runner:** Phase 2 runner adapter and runtime pages. Owns `internal/menu/runner.go` and `status.go`; depends on the core interfaces but can develop against a temporary local interface.
3. **menu-proxy-profile:** Phase 3 proxy/profile/subscription pages. Owns `proxy.go` and `profile.go`; depends on runner and page interfaces.
4. **menu-operations:** Phase 4 DNS/rules/traffic pages. Owns `dns.go`, `rules.go`, and `traffic.go`; depends on runner and page interfaces.
5. **menu-diagnostics-docs:** Phase 5 diagnostic pages, integration tests, and documentation updates. Owns `diagnostics.go`, menu-specific tests, and docs changes after the command behavior is fixed.

If only four agents are available, combine `menu-runner` with `menu-core`, then merge operations before diagnostics and documentation. The parent agent resolves interface conflicts and performs the final integration test; feature agents should not edit `internal/command/help.go` or `docs/reference/cli.md` unless a verified command contract change is required.

## Verification matrix

Each page group must verify:

| Area | Required checks |
| --- | --- |
| Navigation | valid choice, invalid choice, blank input, back, refresh, help, quit |
| Data | normal list, empty list, stale item, names with spaces, pagination |
| Mutation | exact command arguments, confirmation, success result, failure recovery |
| Runtime | stopped engine, running engine, unreachable API, corrupt selection where recovery allows it |
| Safety | URL redaction, no secret echo, no false rollback or activation claim |
| Automation | redirected input rejection, unchanged `--json` output and exit codes |
| Streams | Ctrl-C cancellation, clean return to parent page, no spurious error |

Tests should focus on menu behavior and command mapping. Existing command tests remain authoritative for business operations and should not be duplicated by menu tests.

## Documentation updates after implementation

When the implementation is merged, update `docs/reference/cli.md` with the `menu` command, terminal requirements, navigation keys, noninteractive behavior, and exit codes. Update `docs/development/cli-usability.md` with the menu interaction contract and testing rules. Add a short entry to `README.md` and link this design document from the architecture or CLI documentation as appropriate. Do not describe menu actions as a second business API; the existing commands remain authoritative.
