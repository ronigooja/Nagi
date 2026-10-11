# Terminal menu design

**Status:** implemented.

This document describes `nagi menu` responsibilities, navigation, input lifecycle, and failure handling. The [CLI reference](../reference/cli.md#terminal-menu) defines numbers, effects, terminal requirements, and exit codes.

## Responsibility and command boundary

The menu is a line-oriented interface that preserves terminal scrollback. It owns input, forms, navigation, resource pagination, and presentation. Existing CLI commands own business operations, validation, persistence, and activation. The menu does not read profile files, call mihomo, or change OS settings directly. The CLI remains the complete interface for advanced arguments and automation.

`internal/command` rejects `--json menu` and nonterminal input/output before starting a session. The menu runner invokes ordinary commands with `--json` and decodes their existing result envelope. The menu presentation adapter accepts command arguments and result data, renders readable state and bounded lists, and preserves raw configuration and logs. Ordinary CLI output, JSON fields, syntax, and exit codes are unchanged.

Most commands run in process. The session shows the result or error without echoing internal command arguments. An ordinary operation already submitted can finish; input cancellation does not imply cancellation or rollback of the business operation. Log following and traffic watching use a child Nagi process with a cancelable context. Ctrl-C cancels that stream and returns to the same page. User-mode mihomo runs in a separate process group, so canceling a stream does not stop it.

## Menu presentation

The session owns a semantic style renderer for menu UI text. It detects terminal styling once from its output terminal and environment; the [CLI reference](../reference/cli.md#terminal-menu) defines the enable conditions and style meanings. Plain output retains the same wording, numeric mapping, and selected markers. `Item.Selected` carries actual node or profile selection state, independently of label text; rendering does not infer selection from names or status strings.

The renderer redacts menu labels, context, impacts, and feedback before applying styles. Feedback retains its success, warning, or error class while queued, then renders after the reloaded page and before its prompt. Partial completion and input validation use the warning class; command and read failures use the error class. Each styled span resets immediately, including prompts, so user input and subsequent output do not inherit menu styles.

Requested configuration, overrides, profile comparisons, recent logs, and child log or traffic streams bypass menu styling. The Enter-to-return prompt is menu UI and resets before reading input. Styling changes no ordinary command output, JSON contracts, business operations, or mihomo behavior.

## Page inventory

The home page shows mihomo state and the selected profile. Failed reads show unknown/unavailable state without hiding controls.

The [CLI reference page inventory](../reference/cli.md#terminal-menu) defines the eight home entries and their operation order. Common actions lead each page; deletion is last. Current Configuration, Backup & Restore, Local Overrides, DNS, routing rules, and login startup group their related operations in dedicated pages. Completion and cross-group search are available through ordinary commands only.

The menu does not expose every command argument variant; custom latency URLs and timeouts remain available through ordinary commands. DNS checks inspect configuration and mihomo's API without claiming system-wide DNS leak verification.

## Input and page state

A session owns a single buffered reader. Terminal reads wait for canonical input using platform readiness polling in the same goroutine, checking already buffered data first. There are no per-prompt reading goroutines left behind after cancellation. The session receives interrupt signals while active; completed commands and streams discard their consumed interrupt signals before returning to input.

Each stack frame keeps its page, latest view, page index, and redraw state. A view separates resources (`Items`) from operations (`Actions`) internally so only resources paginate. `numberedChoices` produces the shared mapping for both rendering and dispatch: up to ten resources, all actions, then available pagination entries. Selectors use the same mapping. There is no letter-key allocation or filter state.

A view reloads on entry, operation completion, and return from a child. Pagination renders the existing snapshot without reading again. Page indices clamp to the last valid page after reload. Blank and invalid choices leave the rendered page intact and repeat the prompt. Zero returns, cancels a selector, or exits at home. Help, refresh, filtering, and search have no menu entries.

The session queues command results until the refreshed page has rendered, then prints them immediately before the next prompt. Failed forms flush feedback after their numeric editing/submission choices. Raw configuration, overrides, profile comparisons, and recent logs are shown before an Enter-to-return prompt; only then does the page reload. Streams write directly until cancellation.

An item can open a child, dispatch existing command arguments, run a custom flow, or return a navigation result. Navigation results distinguish staying and refreshing, opening a detail page, and returning to the parent. They take effect only after a successful flow.

Text prompts accept `0`, `b`, and `q` as literal content. Ctrl-C cancels the current form and returns to its page; during page selection it returns to the parent, or exits at home. EOF exits. Forms validate fields as entered and retain other values. After a failed command, users can edit a field and explicitly resubmit using numbered entries, or cancel with zero. There is no automatic retry of mutations and no second confirmation; operation effects are displayed beside entries.

## Proxy flow

Selecting Select Node at home loads groups directly, including current nodes. Selecting a group opens its node list. Selecting a node dispatches `proxy select GROUP NODE` once, then refreshes the current marker while staying in the list. Names containing spaces remain one argument. The ordinary path takes three selections from home.

If the group read fails, Select Node queries the existing `status` command, clears resource choices, and adds a Start / Stop child page beside the existing fixed actions. Recovery guidance distinguishes a stopped engine, an unexpected exit with stale records, a running process, and unknown status. A known stopped engine shows its status-specific recovery guidance without the generic read-error line; other failures preserve the original group-read error. The menu does not infer that a running process has a reachable controller. The shared page runner uses a page's `ReadErrorHint` for failed reads when provided, otherwise its generic unavailable message. Returning from Start / Stop triggers the ordinary group reload; no lifecycle command or retry runs automatically.

The provider entry lists redacted names and node counts. Opening a provider and every read after an operation call `proxy provider show NAME`; update and healthcheck actions use their corresponding CLI commands. A failed provider read clears selectable nodes while leaving those actions available for recovery.

The provider list always offers Add provider, including when empty or unreadable. Its shared form collects the provider name, HTTP(S) URL, existing or new group name, and update interval, then calls `proxy provider add`. Submission errors retain the form values for explicit editing and retry. The form explains that creation updates the selected profile and reloads a running engine, while a new select group leaves routing rules unchanged. Configuration persistence and activation remain CLI responsibilities; the menu does not require a successful live group read to add a provider while stopped.

Details, latency tests, restoring saved choices, and returning an automatic group to automatic selection are fixed actions. Ordinary nodes and latency-ranked nodes all switch with the same numeric action. A group page retains its last measurements and ordering across selections and refreshes, pruning records for nodes absent from the refreshed group. Automatic groups show fixed or automatic state. Latency actions disclose possible release of a fixed choice and preserve underlying errors about partial completion or unknown state.

## Subscription and profile flows

Add and Apply subscription saves the record, downloads its cache, and applies it through the existing commands in that order, then opens its detail page. Update and Apply downloads and then applies an existing subscription. A failed download prevents apply; a newly saved record remains available in the detail page. A failed apply keeps the new cache. Creation forms retry only the add stage; later failures never resubmit add. Encrypted feeds can be retried by setting an identity file in Encryption Settings and choosing Update and Apply.

Details show available update time, quota, and expiry, with missing metadata explicitly unknown. Subscription time and byte formatting belongs to the menu presentation adapter; the [CLI reference](../reference/cli.md#terminal-menu) defines its display rules. Quota comes from the latest provider response metadata rather than live mihomo traffic. Daily pages do not request previews or display change counts or lists; ordinary subscription preview, update, and apply commands remain available.

The combined action discloses that applying replaces the target source configuration and clears local overrides and saved selections. Existing command validation and the apply transaction remain authoritative. The flow does not implicitly start mihomo or enable traffic access. Feedback states successful completion or the failed stage and its known resulting state, without dumping command metadata.

Successful removal returns to the parent list; failure stays in detail. Profile imports and other multi-field flows reuse retained-value forms. Local profile operations keep their existing CLI effects.

## Failure handling and verification

Failed reads replace old resources, preventing selection of stale targets. Views can retain fixed recovery actions and state unknown/unavailable explicitly. Command failures display existing messages and known resulting state; the menu never infers successful saving, activation, or rollback. URLs in errors, page context, and list labels are redacted. Raw requested configuration and log content retain their ordinary command content.

Action failures leave the menu usable and do not determine its exit status. Ordinary exit is `0`; unrecoverable input errors return `1`. Terminal/usage rejection returns `2` as defined in the reference.

Framework and feature tests inject runners to verify dispatch, numeric navigation, pagination, retained forms, navigation, bounded results, redaction, and stale-resource invalidation. Ordinary command tests remain authoritative for persistence, activation, and mihomo effects. Real PTY checks cover repeated interruption, buffered input, EOF, and stream return while mihomo remains running. Darwin cross-compilation checks compilation only; Linux PTY results do not establish macOS runtime acceptance.

The October 2026 verification passed the Go and menu tests, Linux PTY journeys (three-selection proxy switching with spaces, repeated form cancellation, literal text including zero, both streams, EOF, and exit without stopping mihomo), and Darwin source compilation. The final locked mihomo source was separately built and exercised through focused engine regression and isolated Nagi runtime checks. Full mihomo package testing remains limited by the documented listener/inbound timeout and offline nested dependencies. macOS menu-bar interactive acceptance still requires a macOS host.
