# Capture original requests and create rewrite rules

This guide covers explicitly starting original-content capture, inspecting HTTP
and WebSocket traffic, keeping archives and creating local rewrite drafts. The
[CLI reference](../reference/cli.md#original-request-capture-and-capture-derived-drafts)
owns exact arguments and schemas; the [capture design](../design/request-capture.md)
explains ownership and failure handling. Source implementation and software tests
do not establish native TUN coverage; see the
[verification record](../development/request-rewrite-acceptance.md#capture-verification).

## Prepare traffic access

Use a source build with the matching engine from `engine.lock`. Start TUN using
the [traffic access guide](traffic-access.md), explicitly generate a rewrite CA
and establish certificate trust using the
[rewrite CA commands](../reference/cli.md#tun-request-rewrite). Applications with
their own certificate store need explicit trust there too. Capture start checks
these prerequisites and never enables TUN, generates a CA or installs trust.

Starting capture authorizes reading the original URLs, query strings,
Authorization and Cookie headers, trailers and bodies in that session. These
values are shown, searched and exported directly without a reveal step. Keep
terminal scrollback, clipboard contents and exported archives private. Ordinary
errors and diagnostics do not automatically include captured content.

## Start and inspect

On macOS, open **Capture…** in the menu bar app. Set domain patterns, exact
process paths or outermost `.app` paths, then choose **Start**. **Find
Applications** lists observed identities; a path may also be entered before the
application appears. Empty categories impose no restriction. Populated domain,
process and application categories combine with AND; alternatives within each
category combine with OR. Unknown process identity cannot match a process or
application restriction.

From the terminal, open `nagi menu`, then **Settings & Maintenance → Request Rewrite → Original
Capture → Start with scope**. All choices use numbers. The menu submits scope and
body limits to the existing CLI through stdin. It retains form values on failure;
resubmission is explicit. **Captured records** opens paginated record IDs, then
**Show original metadata** and **Read body chunk** display original content until
Enter. Chunk results include decoded-byte offsets, next offset, collection state
and lossless base64; UTF-8 chunks also show body text. Advanced original-content
search and list predicates are available through CLI input JSON.

For automation, the following fictitious scope keeps input out of command
arguments. Use a private file or disable shell history if the shell records
here-document input:

```sh
nagi rewrite capture start - <<'JSON'
{"owner":"cli","scope":{"domains":["api.fixture.example"],"process_paths":[],"app_paths":[]},"body_limit":1048576}
JSON
nagi rewrite capture status
nagi rewrite capture apps
nagi rewrite capture list
```

Make a new connection from the application after starting capture. Existing
connections may remain outside the new scope. HTTP exchanges include original
and actual request/response snapshots. WebSocket messages identify their original
HTTP handshake and direction. Inspect record IDs returned by the list:

```sh
nagi rewrite capture show RECORD_ID
nagi rewrite capture body RECORD_ID request_original 0
```

Body collection states distinguish complete empty content, in-progress collection,
cutoff, truncation and failure. The default limit is 1 MiB per snapshot, up to
8 MiB; the recorder retains at most 1,000 records and 64 MiB aggregate content
and event buffers. Check dropped and truncated counts before interpreting results.
A missing exchange does not prove that an application sent no traffic.

## Control and retain a session

Use **Pause**, **Resume**, **Stop** or **Clear** in the App or terminal menu, or
pass the returned session ID to the corresponding CLI operation. Pause and stop
retain recorded history. Clear removes this session's records and invalidates
list cursors. Status compares recorder, engine and rewrite child observations;
an `uncertain` result includes a recovery command. Stop the named session before
starting again; do not assume a paused recorder means interception has stopped.
Recovery may tear down the rewrite generation, requiring explicit rewrite enable
again if rewriting is wanted.

Closing the terminal menu or finishing a CLI command retains the session. Closing
the App capture window stops only the session that window started and retains
history; CLI-owned recording survives it. Profile/backend changes, TUN/engine
exit and quit stop capture. Restart never resumes it automatically. History is
memory-only and disappears when the recorder exits.

Export before exiting if history is needed:

```sh
nagi rewrite capture export capture.ndjson native
nagi rewrite capture export capture.har har
nagi rewrite capture import capture.ndjson
```

Choose a new destination; exports refuse existing files and create owner-only
files. Native archives support later import; HAR is an interchange export with
WebSocket extensions. Import validates a native archive before replacing stopped
history. Imported sessions are offline, stopped and read-only: they cannot be
resumed or cleared. A new explicit start can replace them. Import during recording
or pause fails. A partial or invalid archive leaves retained history unchanged.

## Create, preview and save a rule

In the terminal menu, open a record and choose **Create quick rule**. Set the
rule ID, request/response/WebSocket stage and whether the candidate should be
enabled on save. Add ordered operations using the existing rewrite operation
forms, then choose **Build versioned draft**. The CLI derives an exact original
host/method/path match. **View complete draft**, **Edit complete draft JSON**,
**Preview full chain** and **Save rule** are separate actions. JSON editing covers
all existing predicates, operations, destination consent, insertion position and
error/range policies. **Open versioned draft JSON** opens an externally prepared
draft. The CLI also accepts private JSON files for advanced and batch workflows.

For example, derive a request-header rule from an original HTTP record:

```sh
nagi --json rewrite capture draft RECORD_ID - <<'JSON'
{"stage":"request","id":"fixture-header","operations":[{"op":"header_set","name":"X-Demo","value":"fictitious"}],"enable":false}
JSON
```

The result's `data` object is the versioned draft. Supply that object, rather
than the surrounding success envelope, through stdin or a private file to
`nagi rewrite draft preview -` and `nagi rewrite draft save -`. Preview runs the
entire existing chain locally with the candidate temporarily enabled. It sends
no network requests and changes no saved state. Complete original bodies are
required for a preview fixture. An incomplete record can still derive a rule
from metadata; its draft has no fixture and cannot preview invented body bytes.
WebSocket drafts retrieve their original handshake automatically.

Save checks the base rule version inside the existing transaction, inserts at
the chosen position and uses normal activation/rollback. A conflict saves
nothing: rebuild against the current rules and review again. Save preserves the
global rewrite intent; saving an enabled candidate does not implicitly enable
rewriting or start TUN. Header/query predicates require an explicit match rather
than being inferred from secret values. Use the full existing operation set and
[rewrite semantics](../design/request-rewrite.md) for response and both WebSocket
directions as well.
