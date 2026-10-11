# Terminal menu design

**Status:** implemented.

This document describes `nagi menu` responsibilities, navigation, input lifecycle, and failure handling. The [CLI reference](../reference/cli.md#terminal-menu) defines keys, effects, terminal requirements, and exit codes.

## Responsibility and command boundary

The menu is a line-oriented interface that preserves terminal scrollback. It owns input, forms, navigation, resource filtering and pagination, and presentation. Existing CLI commands own business operations, validation, persistence, and activation. The menu does not read profile files, call mihomo, or change OS settings directly. The CLI remains the complete interface for advanced arguments and automation.

`internal/command` rejects `--json menu` and nonterminal input/output before starting a session. The menu runner invokes ordinary commands with `--json` and decodes their existing result envelope. The menu presentation adapter accepts command arguments and result data, renders readable state and bounded lists, and preserves raw configuration and logs. Ordinary CLI output, JSON fields, syntax, and exit codes are unchanged.

Most commands run in process. The session shows the executing action and then its result or error. An ordinary operation already submitted can finish; input cancellation does not imply cancellation or rollback of the business operation. Log following and traffic watching use a child Nagi process with a cancelable context. Ctrl-C cancels that stream and returns to the same page. User-mode mihomo runs in a separate process group, so canceling a stream does not stop it.

## Page inventory

The home page shows mihomo state and the selected profile. Failed reads show unknown/unavailable state without hiding controls.

| Home category | Flows |
| --- | --- |
| Runtime controls | Status, start, stop, restart, configuration show/validate/reload, runtime and persistent mode, session quit. |
| Proxy groups | Groups and current nodes, direct node switching, filtering, details, node/group latency, automatic selection, saved selection restore, and redacted provider update/healthcheck actions. Hidden native groups are omitted while icon metadata is retained for labels. |
| Subscriptions | List/add, detail metadata, cache download, change summaries and paginated names, apply, remove, schedule interval, encryption-key set/clear, pause/failure state, quota and expiry. |
| Local profiles | List/import, use, export, backup/restore, diff, override show/set/clear, remove. |
| Traffic access | TUN, listener ports, LAN status and changes; macOS system proxy controls. |
| Diagnostics | Doctor, redacted diagnostics/export, version, shell completion. |
| Advanced functions | DNS, rules, connections, logs/traffic, service/startup, and supported privileged-helper operations. |

Every existing menu operation retains an entry. The menu does not expose every command argument variant; for example custom latency URLs and timeouts remain available through ordinary commands. DNS checks inspect configuration and mihomo's API, without claiming system-wide DNS leak verification.

## Input and page state

A session owns a single buffered reader. Terminal reads wait for canonical input using platform readiness polling in the same goroutine, checking already buffered data first. There are no per-prompt reading goroutines left behind after cancellation. The session receives interrupt signals while active; completed commands and streams discard their consumed interrupt signals before returning to input.

Each stack frame keeps its page, latest view, page index, filter, and redraw state. A view separates numbered resources (`Items`) from fixed letter operations (`Actions`). Resource names drive case-insensitive filtering, independently of labels containing status metadata. Fixed actions do not participate in filtering or pagination. Reserved keys are never assigned to actions.

A view reloads on entry, refresh, operation completion, and return from a child. Filter and page survive reload; page indices clamp to the last valid page. Changing a filter resets the index. Blank and invalid choices leave the rendered page intact and repeat the prompt. Navigation and operations trigger rendering. Help describes the current page's entries and navigation.

An item can open a child, dispatch existing command arguments, run a custom flow, or return a navigation result. Navigation results distinguish staying and refreshing, opening a detail page, and returning to the parent. They take effect only after a successful flow.

Text prompts accept `b` and `q` as literal content. Ctrl-C cancels the current form and returns to its page; during page selection it returns to the parent, or exits at home. EOF exits. Forms validate fields as entered and retain other values. After a failed command, users can edit a field and explicitly resubmit. There is no automatic retry of mutations and no second confirmation; operation effects are displayed beside entries.

## Proxy flow

Selecting Proxies at home loads groups directly, including current nodes. Selecting a group opens its node list. Selecting a node dispatches `proxy select GROUP NODE` once, then refreshes the current marker while staying in the list. Names containing spaces remain one argument. The ordinary path takes three selections from home.

The provider entry lists redacted names and node counts. Opening a provider and every later refresh call `proxy provider show NAME`; update and healthcheck actions use their corresponding CLI commands. A failed provider read clears selectable nodes while leaving those actions available for recovery.

Details, latency tests, restoring saved choices, and returning an automatic group to automatic selection are fixed actions. Ordinary nodes, filtered nodes, and latency-ranked nodes all switch with the same numeric action. A group page retains its last measurements and ordering across selections and refreshes, pruning records for nodes absent from the refreshed group. Automatic groups show fixed or automatic state. Latency actions disclose possible release of a fixed choice and preserve underlying errors about partial completion or unknown state.

## Subscription and profile flows

Successful subscription creation opens its detail page. Cache download stays there and never implicitly applies, starts mihomo, or enables traffic access. Details show available update time, quota, expiry, and cache-versus-target change counts. Missing metadata remains explicitly unknown. Complete added/removed/changed names are shown only on request in paginated lists.

Download results compare the new cache with the previous cache. Apply previews compare the cache with the target profile. These baselines are labeled separately. Apply is always present: a preview failure shows its reason but cannot block a valid cache from repairing a damaged target profile. Existing command validation and activation remain authoritative. The apply entry explains configuration replacement and clearing of local overrides and saved selections.

Successful removal returns to the parent list; failure stays in detail. Profile imports and other multi-field flows reuse retained-value forms. Local profile operations keep their existing CLI effects.

## Failure handling and verification

Failed reads replace old resources, preventing selection of stale targets. Views can retain fixed recovery actions and state unknown/unavailable explicitly. Command failures display existing messages and known resulting state; the menu never infers successful saving, activation, or rollback. URLs in errors, page context, and list labels are redacted. Raw requested configuration and log content retain their ordinary command content.

Action failures leave the menu usable and do not determine its exit status. Ordinary exit is `0`; unrecoverable input errors return `1`. Terminal/usage rejection returns `2` as defined in the reference.

Framework and feature tests inject runners to verify dispatch, filtering, pagination, retained forms, navigation, bounded results, redaction, and stale-resource invalidation. Ordinary command tests remain authoritative for persistence, activation, and mihomo effects. Real PTY checks cover repeated interruption, buffered input, EOF, and stream return while mihomo remains running. Darwin cross-compilation checks compilation only; Linux PTY results do not establish macOS runtime acceptance.

The October 2026 verification passed the Go and menu tests, Linux PTY journeys (three-selection proxy switching with spaces, repeated form cancellation, literal text keys, both streams, EOF, and exit without stopping mihomo), and Darwin source compilation. The final locked mihomo source was separately built and exercised through focused engine regression and isolated Nagi runtime checks. Full mihomo package testing remains limited by the documented listener/inbound timeout and offline nested dependencies. macOS menu-bar interactive acceptance still requires a macOS host.
