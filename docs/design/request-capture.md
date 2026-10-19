# Request capture and quick rewrite rules

Status: implemented with software verification and native Linux/macOS TUN/helper
checks. Interactive App, sleep/wake, sustained resource and release acceptance
remain separately pending. This document
owns the current capture protocol, responsibilities and failure behavior. See the
[verification record](../development/request-rewrite-acceptance.md#capture-verification)
for evidence and outstanding platform checks.
The existing [rewrite design](request-rewrite.md) owns transformation semantics.

## Ownership and authorization

Starting capture authorizes viewing, searching, previewing and exporting the
original content within that session. Capture-specific output includes complete
URLs, credentials and bodies without redaction or an additional reveal action.
Ordinary logs, errors and diagnostics never automatically include captured data.
Mihomo owns TUN, process identity, classification, routing and upstream sockets.
Nagi owns bounded content recording, sessions, archives, drafts and interaction.

## Engine extension

Keep rewrite protocol and rule document version 2. Capabilities add
`capture_protocol:1`. Session preparation optionally includes `capture`:
`{session_id,revision,state,scope:{domains,process_paths,app_paths}}`. State is
`recording`, `paused` or `stopped`; empty scope arrays mean all applications.
Different populated scope categories are ANDed, entries within a category ORed.
Process paths match exactly; app paths match a directory boundary below an
outermost macOS `.app` bundle, including helpers. Unknown identity does not match
a process/app restriction. Domain patterns retain the existing rewrite syntax.

`PUT /request-rewrite/capture` takes
`{version:2,generation,previous_revision,capture}` and returns status. Revision
starts at 1 and increases by exactly one. Duplicate identical updates succeed;
stale or conflicting updates fail. This never replaces a rule generation.
Prepared sessions may have empty interception/target arrays only with capture.
An existing rewrite domain is always intercepted independently of capture scope.
Capture-only contexts may open their original authority, not arbitrary targets.

Authenticated handoff optionally includes `Nagi-Capture-Metadata`: unpadded
base64url JSON `{session_id,revision,uid,process,process_path,source,destination}`.
The child accepts it only with the negotiated capture extension. Identity comes
from the original engine metadata, never from a child-supplied override.
The child configuration may include capture policy, recorder socket and body
limit; inherited startup pipes carry it. Engine failures use fixed codes.

## Recorder contract

The Go types in `internal/capture/types.go` are the shared recorder contract.
The independently running ordinary-UID recorder binds `capture.sock` under the
private runtime directory. `POST /call` accepts `capture.Request`, returns
`capture.Reply`, authenticates kernel peer UID and rejects malformed inputs.
`capture.Call(ctx,socket,request)` is the client and
`capture.Serve(ctx,socket,ownerUID)` is the server entry point.

Actions: `start`, `status`, `pause`, `resume`, `stop`, `clear`, `list`, `show`,
`body`, `event`, `import`. Every mutation except initial start names the session.
All records are original content. Lists/show omit body bytes, not header/query
values. Body reads return at most 256 KiB; offsets are decoded-byte offsets.
Cursor tokens are opaque and invalidated by clear. Lists support original-content
search, stable IDs and bounded pages. Imported sessions are stopped/read-only.

Events are `record` (upsert metadata), `body` (append bytes to one snapshot),
`finish` (final snapshot state/trailers), or `complete` (record outcome). Snapshot
names are `request_original`, `request_actual`, `response_original`,
`response_actual`, `message_original`, `message_actual`. Kind is `http` or
`websocket`; messages reference their HTTP handshake using `parent_id`.
Only active-session events are admitted; paused/stopped sessions reject new data.
Errors do not echo request values. Limits: 1,000 records, 64 MiB aggregate content
and event buffers, default 1 MiB per snapshot, configurable to 8 MiB. Truncation,
failure, collection cutoff and empty complete bodies remain distinguishable.

## CLI and drafts

Capture commands: `check`, `start [FILE|-]`, `status`, `pause SESSION`, `resume SESSION`,
`stop SESSION`, `apps`, `list [FILE|-]`, `show RECORD`,
`body RECORD SNAPSHOT OFFSET [LIMIT]`, `clear SESSION`,
`export FILE [native|har]`, `import FILE|-`,
`fixture RECORD STAGE`, `draft RECORD FILE|-`.
Start/list JSON options match shared types. Output uses the existing JSON
envelope. Exports are raw bytes, reject `--json`, use owner-only exclusive files;
`-` writes stdout. Aliases share the same implementation. Commands never start
TUN or install trust implicitly. Dedicated capture commands expose raw values.

`rewrite draft preview FILE|-` and `rewrite draft save FILE|-` consume
`{base_rule_version,position,rule,fixture?,enable}`. Position is a zero-based
insertion index. Preview enables the candidate in an ephemeral snapshot, runs the
complete chain against original fixture data and never sends network requests.
Save checks the current version inside the existing store transaction, inserts
the rule at that position and uses the existing activation/rollback mechanism.
No intermediate append/reorder/enable transactions are permitted.

## Lifecycle and delivery

Closing the capture window stops only its own recording and retains history.
CLI recordings survive window closure. Profile/backend changes and TUN/engine
exit stop capture; restart never resumes it. Existing streams drain after
collection stops. Rule generation changes retain recorder history. Records are
memory-only unless explicitly exported. Archives are versioned streaming NDJSON;
HAR uses named extensions for WebSocket data. Imported archives are offline.

The implemented source includes HTTP/HTTPS, both WebSocket directions,
stream-safe collection, the existing rewrite operation set, macOS/CLI/menu
interfaces and native/HAR archives. Native Linux/macOS TUN and authenticated helper checks have passed for the
scenarios in the verification record. Interactive App, sleep/wake and sustained
resource checks remain delivery gates; software fixtures cannot establish them.


## Observation and failure recovery

The CLI reports capture status from observed recorder, engine and child state,
including generation, engine epoch, selected profile, backend and actual TUN
availability. A recorder state alone is insufficient proof of active collection.
A mismatch reports `state:uncertain`, retains `recorder_state` and provides a
session-specific stop recovery command. Pause/resume reject uncertain sessions.
Stop tears down an uncertain generation and retains history; rewriting may need
explicit reactivation afterward. A coherent pause/resume/stop changes only capture
policy, preserving the rule generation and open streams. Startup failure tears
down partial activation rather than leaving collection claimed active.

Scope inputs use rewrite domain validation, bounded/deduplicated arrays and clean
absolute process/application paths. Application paths must identify the outermost
`.app` bundle. Malformed, ambiguous or duplicate JSON fields fail before state
changes. Archives validate before replacing retained stopped history; failed and
truncated imports preserve the previous session.

## Interaction and sensitive presentation

The CLI owns the sequential readiness query used when opening the terminal
capture page or native capture window. `capture check` stops at the first unmet
prerequisite without inspecting later stages. It reuses activation prerequisite
validation and does not need or start a recorder. Both interfaces display the
first unmet prerequisite without setup actions, preserving offline history/archive
access. The App omits the success message when ready. It checks again on explicit
**Recheck** beside the unmet-prerequisite message or window reopening, rather
than on manual data refresh or automatic record polling; canceled
checks cannot publish stale results.
Start retains its independent validation. The exact check order and response
contract belong to the [CLI reference](../reference/cli.md#original-request-capture-and-capture-derived-drafts).

The native App capture window owns a distinct session token. Closing it stops
only its own session; hiding, minimizing or sleeping cancels reads without
resuming collection on return. Capture mutations share the existing App operation
gate with settings and quit. The list and snapshot views display original
content, including URLs, headers, trailers and bodies. Body views support text,
JSON, hex and original/actual comparison. Context selections feed a local quick
rule draft through the CLI; operation validation and saving remain CLI work.

The capture toolbar contains session controls, a scope summary and a More menu.
While recording, status, records and selected snapshot details refresh every
second; slow reads skip timer ticks. Paused, stopped, uncertain and empty sessions
have no record-polling timer and show **Refresh** for manual synchronization,
including changes made through the CLI. Refresh reads data without rechecking
prerequisites. Opening or restoring a visible window, returning the App or window
to the foreground, and waking synchronize once without resuming collection.
Session mutations and filter changes also synchronize immediately. Observing a
recording session starts polling; observing any other state stops it.

The window title identifies the page; the content does not repeat a page title
or profile name. Scope opens a popover for domain patterns, exact
process paths and application paths. With no active session, edits stay in the
window's draft and Start submits them directly. Recording, paused and uncertain
sessions show the observed session scope in the popover without editing; Stop
must complete before scope editing becomes available again. This also applies
to sessions started through the CLI. Scope is separate from the search and
filters applied to retained records. More contains the next-session body limit,
import, export and clear. Generic operation/export success notices are omitted;
session state and records show results, while errors and recovery remain visible.
The footer shows record count, formatted byte size and nonzero loss counters,
with a fixed-size refresh indicator slot to avoid shifting content.

The record browser uses a four-column native table: **Method**, **Request**,
**Status** and **Time**. Request cells have two bounded lines for the original
host and path/query; tooltips and Copy URL retain the complete original URL.
Method, status and timing stay in aligned columns. HTTP status comes from
`response_actual`, falling back to `response_original` when necessary. A missing
status is **Pending**, or **Failed** when capture reports a failure; rewrite
outcomes are never interpreted as HTTP status codes. A captured code remains
visible even when subsequent body collection fails; the overview retains error
and collection details. Status codes use color as a secondary cue: green for
2xx, orange for 4xx, red for 5xx and neutral for other classes. Zero recorded
duration displays an em dash. WebSocket messages use WS direction markers and
sequence numbers; the related handshake supplies their host and URL when
available, but its HTTP status is never applied to a message. Missing handshake
metadata falls back to the captured destination. Selection uses stable record
IDs and table updates do not intentionally reset scrolling.

The resizable split gives the table an initial width around 440 points. Details
keep a compact request summary and snapshot selector above **Overview**,
**Headers** and **Body** sections. Overview and headers scroll independently;
full URLs, process/network identity, minute-precision timestamps, matched rules,
record IDs, collection state and related WebSocket navigation live in Overview.
Headers separates field names and complete values, including trailers. Selecting
Before / After on Body also makes Headers show original and actual headers for
an actual snapshot when its original exists. Body uses the remaining section
height for wrapped, selectable text with format and copy controls above it.
The JSON pointer rule action opens a popover instead of consuming a permanent
row. The active detail section survives record refresh and selection changes;
changing records closes the pointer popover and clears its input. Field and body
context menus continue to derive drafts through the CLI.

AppKit owns the capture window size. The SwiftUI host does not derive window
minimum, maximum or ideal sizes from capture content; layout is bounded by the
current content area. The initial outer frame, including its title bar, is at
most 1100 × 760 points and 90% of the screen's visible width and height. The
normal 980 × 650 minimum is capped at 80% of the screen's visible width and
height, leaving room to shrink the window even on smaller screens.
Action and filter rows scroll horizontally when needed. Reopening an existing
window preserves its current size. Native regression tests cover small screens, long URLs/headers, repeated
shrinking, table selection/scroll retention and detail-section body space. The
table/detail layout update is source-reviewed on Linux; execution of its Swift
tests and interactive layout acceptance remain unverified on that host.

The terminal menu is under Rewrite & Capture → Request Capture. It
uses existing command runners for status, scoped start, application discovery,
record listing/show, decoded-offset body chunks, session controls, fixtures,
archives and versioned drafts. Resource lists use the standard ten-entry numeric
pagination and an explicit next-batch action for recorder cursors. Original
metadata/body views bypass ordinary redaction and styles and pause until Enter;
ordinary page context and errors retain their existing presentation boundary.
There is no reveal operation or extra consent after an explicit capture action.

Quick-rule operation forms build a selection; `capture draft` supplies the base
rule version, original match and optional complete fixture. The menu binds preview
and save to the selected profile, retains the versioned candidate after failure
and never retries a mutation automatically. Complete JSON editing covers advanced
predicates, every operation, insertion order and policy fields. EOF discards an
unfinished form. Advanced private-file and batch flows remain ordinary CLI
operations. User steps belong in the [capture guide](../guides/request-capture.md)
and exact syntax belongs in the
[CLI reference](../reference/cli.md#original-request-capture-and-capture-derived-drafts).
