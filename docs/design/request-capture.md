# Request capture and quick rewrite rules

Status: accepted implementation contract; implementation and platform acceptance
are in progress. This document owns the capture protocol and operation boundary.
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

Capture commands: `start [FILE|-]`, `status`, `pause SESSION`, `resume SESSION`,
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

HTTP/HTTPS, both WebSocket directions, stream-safe collection, all existing
operations, macOS/CLI/menu, archives, native TUN and helper acceptance are required
for completion. Fixtures cannot substitute for real platform acceptance.
