# Rule-driven content rewrite

This document defines the current v2 rule document, deterministic transformations,
private engine protocol and transaction boundaries. Nagi owns content operations,
configuration, script execution, persistence and lifecycle. Mihomo owns TUN
capture, DNS, original identity, routing and upstream connections. Native macOS
trust, TUN and UI acceptance must be recorded separately from software fixtures;
see the [verification record](../development/request-rewrite-acceptance.md).
Command syntax is in the [CLI reference](../reference/cli.md#tun-request-rewrite).
[Decision 0011](../decisions/0011-rule-driven-content-rewrite.md) records the boundary.

## Traffic and ownership

```text
application -> mihomo TUN -> identified HTTP/TLS for intercept_domains
                                  |
                        authenticated handoff
                                  v
                  ordinary-UID Nagi rewrite child
                 request -> one upstream send -> response
                       | WebSocket: two message streams
                       v
                 authenticated engine reinjection
                       v
              mihomo original identity -> routing -> outbound
```

Rules provide interception scope. Every rule specifies an exact ASCII domain or
an explicit `*.service.example` pattern; the wildcard matches one or more labels
and excludes the apex. Normalize case and one trailing dot. Reject IP literals,
URLs, ports, Unicode names, malformed A-labels and public-suffix wildcards. No
independent domain authorization document exists.

`intercept_domains` is the sorted, unique union of enabled rules' domains across
all four stages. `target_domains` adds the `redirect_consent` domains of enabled
request rules. Each set has at most 256 entries. Target-only names do not cause
interception or QUIC blocking. HTTPS interception occurs by domain before method,
path or other HTTP predicates can be inspected; those predicates do not limit
TLS decryption itself.

Only actual TUN application TCP traffic enters this branch. Ordinary proxy
listeners, internal DNS, provider downloads, health checks, node connections and
reinjected traffic retain mihomo's normal paths. Rule, Global and Direct modes
all retain the branch and choose outbound routing after reinjection. Mihomo
blocks recognized QUIC only for interception domains to encourage TCP fallback.
Unknown UDP, ECH-hidden names, missing fake-IP mappings, certificate pinning and
client certificates limit coverage. There is no additional HTTP/3 implementation.

## Document and limits

A complete private JSON document has exactly these top-level fields:

```json
{
  "version": 2,
  "enabled": false,
  "rules": [{
    "id": "demo",
    "enabled": true,
    "stage": "response",
    "match": {"domain": "api.service.example", "methods": ["GET"], "path_prefix": "/v1/", "status_codes": [200]},
    "on_error": "block",
    "range_mode": "fragment",
    "redirect_consent": [],
    "operations": [{"op": "header_set", "name": "X-Demo", "value": "example"}]
  }],
  "scripts": []
}
```

Unknown fields, duplicate keys, trailing JSON, invalid UTF-8, incorrect types,
unsupported versions and malformed operation shapes fail ordinary validation.
There is one parser and one command tree. Both command names use that tree.
IDs contain 1–128 ASCII letters, digits, `_` or `-`.

| Resource | Limit |
| --- | --- |
| Canonical or input document | 1 MiB |
| Rules / operations per rule | 1024 / 32 |
| Script source | 64 KiB per script |
| Encoded or decoded body/message | 8 MiB at every conversion step |
| Headers / Trailers | 64 KiB / 16 KiB |
| Logical body conversion workspace | 64 MiB |
| Body read and stage conversion | 5 seconds combined |
| Preview input | 24 MiB, including both bodies' base64 expansion |
| WASM instance linear memory / execution | 128 MiB / 1 second |
| Parallel script instances | 2, with immediate resource failure when full |

The conversion workspace accounts for original bytes, working copies, codecs
and parsed values. It is an admission budget, not a Go heap or process RSS limit.
WASM linear memory is a separate guest limit. Host JSON input/output and script
source have separate size checks. Bodies never spill to disk. Private storage
uses owner-only directories and files, locks, no-follow opens and atomic writes.
CLI inputs, child startup frames, terminal drafts, App stdin/output and exports
allow the encoding overhead of legal maximum documents and fixtures.

## Rule matching

Required rule fields are `id`, `enabled`, `stage`, `match`, `on_error`,
`redirect_consent` and `operations`. Optional `range_mode` defaults to `fragment`.
`on_error` is `block` or `forward_original`. Stages are `request`, `response`,
`websocket_client` and `websocket_server`.

`match.domain` is required. Other predicates are optional:

| Field | Meaning |
| --- | --- |
| `methods` | Up to 16 uppercase HTTP method tokens. |
| `path_prefix` | Prefix of the original escaped path; at most 2048 bytes. |
| `path_regex` | RE2 expression over that escaped path; at most 8192 bytes. |
| `query` | Object mapping query names to nonempty arrays of candidate values. Any same-name parameter value can match. |
| `headers` | Original request headers, represented as names to candidate value arrays. |
| `status_codes` | Original response or WebSocket handshake status codes. Unavailable in request rules. |
| `content_types` | Media types, without parameters, from the original response/handshake. |
| `response_headers` | Original response/handshake headers and candidate values. |
| `message_types` | `text` and/or `binary`, for WebSocket stages only. |

Different predicates are ANDed; candidates within one predicate are ORed.
Header names are case-insensitive and values remain separate. Repeated
`Set-Cookie` values are never merged. All matching rules in a stage run in stored
order, using original request, original upstream response or original message
metadata for every match. Edits affect the current working message only. RE2
expressions are validated and compiled when the document is saved.

Ordinary summaries omit operation values, script source and redirect values and
redact query/header predicate values. Raw exports, source viewing and previews
are explicit sensitive operations and may expose credentials and full bodies.

## Operations

Operations have exactly the fields listed below, plus optional explicit
`message_type: "text" | "binary"` for WebSocket body operations or scripts.

| `op` | Additional fields | Scope |
| --- | --- | --- |
| `header_set`, `header_append` | `name`, string `value` | HTTP and ordinary WebSocket handshake headers |
| `header_delete` | `name` | Same |
| `url_set` | `component`, string `value` | Request; component is `scheme`, `host`, `port`, `path` or `query` |
| `method_set` | string `value` | Request; uppercase method, excluding CONNECT |
| `status_set` | integer `value` from 200 through 599 | Final HTTP response |
| `json_set` | `pointer`, JSON `value` | Body/message; RFC 6901 pointer, exact JSON numbers |
| `json_delete` | `pointer` | Body/message; existing non-root value |
| `body_set` | `encoding: "text" | "base64"`, string `value` | Complete body/message replacement |
| `text_replace` | `pattern`, `replacement`, boolean `all` | Literal first/all replacement |
| `regex_replace` | `pattern`, `replacement`, boolean `all` | RE2 first/all replacement with Go capture expansion, such as `${1}` |
| `trailer_set` | `name`, string `value` | HTTP Trailers |
| `trailer_delete` | `name` | HTTP Trailers |
| `script` | `script` ID | Any declared stage |

Header names must be valid tokens and values cannot contain controls. Header
values are at most 8192 bytes. Host, length, framing, encoding, connection,
upgrade, `Sec-WebSocket-*`, proxy authentication and private channel fields are adapter-owned and
cannot be edited. Trailers additionally reject authentication, routing,
representation and response-control fields. Script outputs use the same checks.

JSON requires a JSON media type, rejects duplicate keys and invalid UTF-8,
preserves arbitrary-precision number text, and applies strict pointer traversal.
Text uses declared charset, default UTF-8, with standard `x/text` encodings;
unrepresentable or non-lossless conversions fail. Base64 replacement operates
on bytes. Content encodings are identity, gzip, standard zlib `deflate`, Brotli
and Zstandard, with at most three layers. Decode in reverse order and encode in
forward order, checking input/output sizes at each layer.

Only operations requiring a body buffer it. Header-only HTTP/SSE transformations
preserve streaming. Body changes update length/encoding/framing and invalidate
ETag and representation digests in headers and existing trailers. Trailer invalidation
follows each successful body operation; a later explicit permitted trailer edit
uses its stored position in the operation sequence. HEAD, 204, 205 and 304 retain their protocol
body restrictions. A valid WebSocket handshake produces 101; rules cannot
manufacture protocol upgrade headers or a 101 status. Body and trailer edits
read to completion before commit. Trailer-only edits run at stream EOF;
HTTP/1.1 chunking and HTTP/2 trailing headers remain adapter responsibilities.

## Range

`fragment` retains Range/If-Range. Single `Content-Range` and
`multipart/byteranges` responses are transformed per fragment. Every fragment
must retain its original encoded byte length at stage commit; intermediate
operations may change length within the ordinary size limits. JSON operations
require each fragment to be a complete JSON value. Boundaries and Content-Range
remain consistent. Buffered scripts run once per fragment for each operation,
using the same current metadata baseline across that operation; their metadata
changes must agree across fragments. Multipart metadata shares the workspace
and has an additional 8 MiB admission limit.

`full` explicitly asks for the complete representation. Before sending, evaluate
original request predicates of candidate body rules and remove Range/If-Range
only when their mode is full. Conflicting candidate modes fail before any
upstream write, including candidates whose response predicates cannot yet be
known. Send once. If the server still returns partial content, transformation
fails under the owning rule's error policy; no second request is issued.

## Transactions, errors and lifecycle

One immutable snapshot owns request matching, request operations, final target
validation, one upstream send, original response matching and response operations.
Response errors never undo or resend the request. Local errors do not enter
response rules. Every executed request rule that contributes a target change
must permit the final domain, scheme and port in `redirect_consent` when the
final target differs from the original. The engine's target set is an additional
boundary. Scripts cannot expand either permission. URL authority, Host, TLS SNI
and engine target move together; mihomo reroutes with original source identity
and clears stale destination-derived metadata. HTTP redirects are returned to
the application and are not automatically followed.

Before commit, `block` returns a fixed sanitized HTTP error or closes the
WebSocket. `forward_original` discards all edits in that stage only when the
complete original is recoverable. Partial bodies or messages cannot be used as
fallback. TLS verification, authentication, final target permission and protocol
validation cannot be bypassed. After body commit, read/trailer failures terminate
the stream; sent bytes cannot be retracted. A WebSocket message is sent only
after its complete successful transformation.

Rule saves use the generation transaction. Prepare/probe a candidate, publish
atomically, revoke all old contexts and pools, and then retire the old child.
Connections cannot mix rule generations. Failed activation restores stored
configuration and confirms rollback; otherwise status is `uncertain`.

Enabled intent with zero enabled rules tears down the branch and child and is
`idle` after cleanup is confirmed. The first enabled rule requires actual TUN,
CA, trust and engine capabilities. Removing/disabling the last rule tears down
interception while preserving enabled intent. The overall disable clears intent
and interception. Missing runtime evidence remains unavailable/uncertain rather
than inferred success. CA generation, trust installation/removal and public
export are separate explicit operations.

## JavaScript contract

A script is `{id,body_access,source}`. `body_access` is `none` or `buffer`.
A referenced script cannot be removed until all referring rule operations are
removed. Header-only scripts receive no body and cannot return one. Buffered
scripts receive bounded decoded bytes as `message.body_base64`.

The synchronous entry point is `transform(input)`. Input is
`{stage,original,message}`. `original` contains read-only matching metadata:
request `{method,url,headers}`, response `{status,headers}` where available,
and original WebSocket message `{type}`. `message` contains current working
fields. Request fields are `method`, `url`, `headers`, `trailers`; response fields
are `status`, `headers`, `trailers`; WebSocket fields are `type`.

Return exactly `{action:"unchanged"}`, `{action:"block"}`, or
`{action:"replace",message:{...}}`. Replacement message fields are a checked
partial update to the current working message. `body_base64` is available only
to buffered scripts. Header maps contain arrays, preserving repeated values.
A type change requires an explicit `type` output or operation `message_type`.
Malformed output, oversized fields, invalid UTF-8 text and unauthorized targets
fail validation.

The original metadata graph is deeply frozen before user source runs, and the
`original` input property cannot be replaced.

QuickJS executes in a fresh wazero instance per call. No filesystem, environment,
process, network, downloaded module, real clock or random source is exposed.
Timeout/cancellation destroys the instance. The executable artifact, compiler,
source checksums and licenses are fixed by the
[reproducible guest build](../development/rewrite-script-guest.md). Ordinary
errors never print source, body or guest exception stacks.

## WebSocket adaptation

The HTTP/1.1 Upgrade path uses pinned `github.com/coder/websocket` at message
level. Each side negotiates its own handshake and no-context-takeover
permessage-deflate. The protocol library owns masking, fragmentation, ping/pong and close frames.
The adapter validates UTF-8 text in both edited and streaming directions,
retaining only an incomplete rune between streaming reads; invalid text closes
with status 1007 without completing an invalid message. The adapter preserves buffered first-frame data,
serializes messages in each direction, propagates backpressure and closes both
sides on cancellation, generation retirement or failure. Connections requiring
no message editing retain streaming forwarding; editable messages are bounded
to 8 MiB. Request handshake operations run normally, including buffered scripts over the
complete empty HTTP entity. The final handshake request must remain GET without
a body or trailers; violations are protocol failures and cannot use original
fallback. Extended CONNECT and HTTP/3 are outside this scope.

## Engine and private channel v2

All control, handoff, reinjection, heartbeat, probe and child schemas support
protocol version 2 only. Existing private Unix-socket authentication and helper
owner binding apply. No user CA private key reaches mihomo or the root helper.
Ephemeral channel certificates, two independent random 256-bit secrets, engine
epoch, generation and one-use context tokens authenticate transport and original
identity. Wire errors are fixed codes, never supplied values.

`GET /request-rewrite/capabilities` must advertise `versions:[2]`, `tun_only`,
`original_context`, `reinjection`, `quic_domain_block`, at least 1024 contexts,
64 streams per context, 60-second idle and 3600-second absolute context bounds.
Missing capabilities or another protocol version reject activation.

`PUT /request-rewrite/session` accepts exactly `version`, `generation`,
`previous_generation` (null initially), bound `owner_uid`, `handoff_addr`, public
`child_transport_cert`, `handoff_secret`, `reinjection_secret`, `intercept_domains`,
`target_domains`, `rule_version` and `block_authorized_quic:true`. The session
starts prepared and disabled. Loopback endpoints and public certificates are
validated. Response returns version, generation, engine_epoch, reinjection_addr,
engine_transport_cert and state. The engine never receives rule bodies/scripts.

`POST /request-rewrite/probe` verifies authenticated child/handoff/reinjection
readiness with no target DNS or outbound connection. `PUT /request-rewrite/branch`
publishes or removes a probed generation using `{version,generation,enabled}`.
A candidate must name the current previous generation and can coexist with one
active/blocked generation until publication. Publication closes old contexts
atomically without a forwarding gap. `DELETE /request-rewrite/session?generation=...`
refuses enabled sessions and removes only the named generation.

`GET /request-rewrite/status` returns version, state, branch_enabled, tun_running,
counts, sanitized last_error, and generation/engine_epoch/rule_version when
present. Engine states are absent, prepared, active, blocked and draining. Child
readiness loss retains a blocking branch until explicit fresh-generation
recovery. A mutation with unknown completion requires a status query and
confirmation; never blindly retry. Private helper schemas reject unknown fields,
duplicates, version/authentication/owner errors and oversized frames before mutation.

Inherited startup pipes carry configuration without argv/environment secrets.
Private child control authenticates peer UID and generation and returns status,
readiness and redacted counters. Ordinary counters cover stage hits, failures,
fallback, resource exhaustion, script timeout and abnormal WebSocket closure.
A successful original fallback still increments its failure category; only fixed
internal codes reach counters.
Raw traffic, source and arbitrary errors never enter these records.

## Preview and verification

Preview is local and sensitive, uses this same transformation library and script
executor, and performs no persistence, activation, DNS or upstream send. Its
`stage` is a single processing stage or `exchange` for request plus response.
Fixtures include the original request and, when required, response or WebSocket
message. Output includes matched rule IDs, per-stage outcomes and final messages.
The [CLI reference](../reference/cli.md#tun-request-rewrite) defines exact fixtures.
The [acceptance record](../development/request-rewrite-acceptance.md) distinguishes
unit/race/fuzz/integration checks from native platform and release evidence.
