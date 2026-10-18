# TUN request rewrite

Status: the Nagi domain library, profile-local storage and CA primitives described under **Implemented domain support** are implemented. The end-to-end feature, engine integration, persistent service, lifecycle, OS trust operations, commands and UI remain planned and unavailable. This document defines the frozen v1 contract for those remaining components. The pinned engine release has no request rewrite endpoints. Examples use fictitious `.example` domains and symbolic secrets that are not usable credentials. The rationale is in [decision 0010](../decisions/0010-tun-request-rewrite.md). The [CLI reference](../reference/cli.md) continues to describe implemented commands only.

## Ownership and scope

Mihomo owns TUN ingress, DNS, protocol and domain identification, source/process identity, routing, outbound protocols, and forwarding. Nagi owns explicit domain consent, request rules, request content transformation through goproxy, CA management, configuration persistence, process supervision, and operation failures. Request content transformation is a narrow proposed extension to Nagi's present management boundary; it does not introduce a second network engine or routing matcher.

A single mihomo instance branches authorized application TCP traffic after TUN identification and before outbound routing into an unprivileged, persistent internal Nagi CLI subprocess. That child uses goproxy as an HTTP parser and MITM library and sends every upstream connection back to the same engine. Rule, Global, and Direct modes change final outbound selection only: all three retain the rewrite branch. Ordinary HTTP/SOCKS listeners, internal DNS (including DoH), healthchecks/provider downloads, proxy-node connections, and already reinjected traffic skip it. Source profiles and ordinary routing rules do not enable or authorize rewriting.

Rewriting defaults to off and requires a verified, actually running TUN, not merely `tun.enable` in a file. Only HTTP request headers, URL components, and JSON request bodies are rewritten; responses and arbitrary byte streams are outside v1. There is no HTTP/3 engine. Identified QUIC for an authorized domain is blocked to encourage TCP fallback; unrelated UDP and QUIC pass through ordinary engine routing. Unknown domains, ECH-hidden names, missing fake-IP mappings, unsupported protocols, client certificate requirements, and certificate pinning limit coverage. Unknown traffic is forwarded normally, never claimed to have been rewritten. A recognized authorized HTTP/TLS request that fails interception is blocked by default; there is no silent direct or non-intercepting fallback.

## Verified baseline and dependency gate

The baseline Nagi commit is `50d3acd409727d2131e172bb0d4ab047cccf3a47`. Its `engine.lock` names `https://github.com/ronigooja/mihomo.git`, ref `main`, commit `9a449c5797f7c855067f15be69fa659d13f6ce88`. Inspect that object rather than a developer checkout's current HEAD. At that object:

- `listener/sing_tun/server.go` installs TUN inbound identity; `tunnel/tunnel.go:handleTCPConn` preprocesses fake-IP metadata, sniffs, then calls `resolveMetadata` and the selected adapter's `DialContext`.
- `resolveMetadata` currently performs some process lookup lazily during routing. T2 must capture that identity before handing traffic to the child, and must prevent reinjection from looking up the rewrite process instead.
- `constant/metadata.go` carries source, destination, inbound, UID, process/path, and sniffed hostname. `INNER` identifies internal traffic. These are engine-owned facts, not client-supplied claims.
- `hub/route/server.go` and `hub/route/configs.go` provide existing controller/config activation machinery, but no v1 request rewrite capability, context handoff, or authenticated reinjection. Existing sniffer/DNS support alone cannot implement this design. Existing HTTP/TLS sniffers extract Host/SNI, not complete verified protocol classification; TLS record fragmentation, HTTP method limitations, missing SNI and sniffer skip lists require a new bounded classifier independent of ordinary sniffer configuration. TUN DNS hijack runs before the generic tunnel. QUIC sniffing at this pin recognizes selected draft29/v1 Initial packets only; it is not HTTP/3 parsing or complete version coverage.

Freeze the planned direct dependency to `github.com/elazarl/goproxy v1.9.2`, tag object/commit `a05e9220be0b6fd84566bf0d5f5880be94f3db36` (verified from the tracked release). Its `go.mod` declares Go `1.24.0`, `golang.org/x/net v0.50.0`, and `golang.org/x/text v0.34.0`; therefore the future Nagi dependency change requires Go **1.24.0 or newer**, subject to all transitive module requirements at implementation. Available development Go is `1.26.4 linux/arm64`. T1 changes neither `go.mod` nor the existing README build requirement. T4 must update module locks, build/release checks, and authoritative build documentation together, verify module checksums from the release, and cross-compile supported targets. Do not substitute an unreleased checkout or copy untracked audit files.

Release source inspection is evidence of available hooks, not proof of Nagi compatibility:

| Release evidence | Required adapter and verification gate |
| --- | --- |
| `proxy.go:NewProxyHttpServer` uses `ProxyFromEnvironment`, a misleadingly named `tlsClientSkipVerify` whose value in `certs.go` is an empty (verifying) TLS config, and environment-derived `ConnectDial`. | Replace the transport, `ConnectDial`, and all dial paths explicitly. Set `Proxy: nil`, retain upstream TLS verification against system roots and hostname, and dial only authenticated engine reinjection. `certs.go` also has a separate MITM server `defaultTLSConfig` with `InsecureSkipVerify: true`; do not confuse it with the outbound transport. Never use the built-in CA. |
| `https.go` shares `ConnectMitm` for plaintext and TLS based on initial bytes; `ConnectHTTPMitm` is deprecated. | Carry explicit engine protocol classification, verify bytes against it, preserve buffered bytes, and reject mismatches rather than guess or force TLS on HTTP. |
| `http2.go` has per-stream hooks and copies `UserData`/`RoundTripper`; `AllowHTTP2` defaults false. | Keep HTTP/2 disabled until concurrent streams, authority changes, cancellation, flow control, rule snapshots, and error redaction pass adapter tests. Negotiate HTTP/1.1 initially; h2-only clients may fail. No universal HTTP/2 promise. |
| HTTP/2 paths use `context.Background()` and some `http.Error(...err.Error()...)`; HTTP/1 paths also log context. | Bound handshakes and reads, propagate original-close cancellation, and sanitize every response/error path. A logger wrapper alone cannot redact client error bodies; disable the path or adapt it before exposure. |
| `websocket.go` uses hijacking and relays buffered bytes; HTTP/2 removes hop-by-hop fields. | Validate HTTP/1 WebSocket handshake rewriting, buffered first frame, bidirectional closure and shutdown before claiming upgrade support. No frame rewriting, arbitrary Upgrade, h2c, or extended CONNECT in v1. Unsupported upgrades fail with a sanitized error on authorized traffic. |

## Pipeline and consent

```text
application -> mihomo TUN -> identify + capture original context
                              | unauthorized/unknown/internal -> ordinary routing
                              | authorized HTTP/TLS
                              v
                    authenticated CONNECT handoff
                              v
                    unprivileged rewrite child
                    parse -> authorize -> snapshot -> edit -> commit
                              |
                    authenticated CONNECT reinjection
                              v
                    mihomo restore context -> resolve new target -> route -> outbound
```

Authorization belongs to a separate per-profile rewrite document, never to routing rule syntax or a subscription. A domain is either exact (`api.service.example`) or an explicit subdomain wildcard (`*.service.example`). The wildcard matches one or more labels below that suffix and does not match `service.example`; authorize the apex separately. Normalize ASCII DNS names to lowercase with a single trailing dot removed; reject URLs, IP literals, ports, userinfo, control characters, empty labels, bare/public suffix wildcards, and implicit suffix matching. International names must be supplied as validated ASCII A-labels. No global `*` is accepted. Authorization is checked both at engine branch selection and against each HTTP request's original authority, including keep-alive and HTTP/2 coalescing. A new unauthorized authority inside an intercepted connection is blocked, not treated as consent.

Cross-domain or scheme/port changes require a per-rule explicit `redirect_consent` destination list in addition to destination domain authorization in the same profile. The new destination must satisfy both lists; a rule cannot expand them at runtime. Removing destination authorization invalidates referencing rules. For example changing `api.service.example` to `mirror.service.example` requires separately authorizing `mirror.service.example` and listing it in that rule's consent. Same-domain path/query changes need no redirect consent; any scheme or port change does. Validate each operation syntactically as it runs; validate redirect consent and destination authorization against the final committed target only. Intermediate URL values cannot cause DNS, dialing or any upstream write. If the final target differs from the original, it must satisfy `redirect_consent` of **every executed matching rule containing a scheme/host/port operation**, even if a later rule overwrites that operation or it was a no-op. Header/path/query/JSON-only rules do not participate. If the final target equals the original target, redirect consent is unnecessary for all rules. This conservative contributor set is frozen for v1. The destination port must be in consent's explicit port list; only `http` and `https` schemes are supported. No automatic following of upstream redirects: return the response to the client and authorize any subsequent request independently. A target change updates URL authority, `Host`, engine target and TLS SNI together, discards old target IP/DNS/sniff-derived routing fields (including destination GeoIP/ASN caches), and invokes mihomo routing afresh with original source identity.

## Frozen v1 engine control contract

These proposed endpoints live on the existing private controller, authenticated through its existing transport/helper boundary. They are not public TCP controller routes. The helper exposes only these exact methods and bounded schemas to its bound session UID. Unknown fields, duplicate keys, multiple JSON values, and version mismatches are rejected before mutation. Common engine error body is `{"error":{"code":"rewrite_invalid","field":"version","message":"Unsupported rewrite protocol"}}`; messages never echo supplied values. HTTP codes: 400 schema, 401 channel authentication, 403 policy/UID, 409 generation/state, 413 bound exceeded, 422 unsupported capability/runtime, 503 unavailable. Success returns JSON except successful CONNECT tunnels.

`GET /request-rewrite/capabilities` returns:

```json
{
  "versions": [1],
  "tun_only": true,
  "original_context": true,
  "reinjection": true,
  "quic_domain_block": true,
  "max_contexts": 1024,
  "max_streams_per_context": 64,
  "context_idle_seconds": 60,
  "context_max_seconds": 3600
}
```

A 404 or absent required boolean rejects enable with an upgrade instruction. No guessed compatibility by version string. `quic_domain_block` means authorized-domain recognition only, not comprehensive QUIC/ECH visibility. V1 requires all listed booleans and at least the listed bounds. Lower deployments may reject enable with `rewrite_limit`; do not silently change negotiated behavior.

`PUT /request-rewrite/session` prepares a disabled branch and returns its engine epoch and reinjection address. Example control body (all secrets below are symbolic placeholders):

```json
{
  "version": 1,
  "generation": "11111111111111111111111111111111",
  "previous_generation": null,
  "owner_uid": 1000,
  "handoff_addr": "127.0.0.1:43101",
  "child_transport_cert": "<base64-DER-ephemeral-child-certificate-placeholder>",
  "handoff_secret": "<random-32-byte-base64url-placeholder>",
  "reinjection_secret": "<different-random-32-byte-base64url-placeholder>",
  "domains": ["api.service.example", "mirror.service.example"],
  "rule_version": "sha256:<canonical-document-digest-placeholder>",
  "block_authorized_quic": true
}
```

`generation` and `engine_epoch` are fresh 128-bit lowercase hexadecimal IDs. Secrets are independently generated 256-bit random values encoded unpadded base64url. Session fields are required; `child_transport_cert` is the public ephemeral transport certificate described below, never a private key. The response includes the engine public transport certificate, delivered to the child over its startup pipe before probing. `owner_uid` must equal authenticated helper/session identity, never authorize arbitrary UID. Loopback IPv4 addresses only, ephemeral ports 1–65535; reject unspecified, public and name-based addresses. Engine binds its own `127.0.0.1:0` reinjection listener and returns `{"version":1,"generation":"11111111111111111111111111111111","engine_epoch":"22222222222222222222222222222222","reinjection_addr":"127.0.0.1:43102","engine_transport_cert":"<base64-DER-ephemeral-engine-certificate-placeholder>","state":"prepared"}`. Repeating an identical prepare for the same generation is idempotent. At most one active (or blocked/draining) session and one prepared candidate may coexist. `previous_generation` is required and must match the current active/blocked generation, or null if none exists. A new candidate can be prepared/probed while the old branch remains active or blocked; failed preparation never removes that branch. No second active generation is allowed. A candidate uses its own registered child, secrets and certificate; the coordinator may briefly own two children during handoff. Session/control JSON is limited to 256 KiB and never logged.

`POST /request-rewrite/probe` with `{"version":1,"generation":"11111111111111111111111111111111"}` exercises an authenticated synthetic handoff and reinjection without target DNS or outbound dialing; success `{"ready":true}`. A probe creates only a one-use probe context that cannot dial a destination. It verifies both process and channel readiness, not network health. The child must also report its loaded `rule_version` to the CLI.

`PUT /request-rewrite/branch` takes `{"version":1,"generation":"11111111111111111111111111111111","enabled":true}` and atomically publishes the prepared domain policy only when TUN is actually active and the probe succeeded. For a candidate, `enabled:true` atomically replaces the matching previous generation's policy and loaded rule version, revokes/closes **all** old contexts/pools and publishes the candidate without a forwarding gap. It requires a fresh successful probe and heartbeat for the candidate. Old in-flight requests may be interrupted; v1 does not migrate or continue old contexts. The old child is then stopped. `enabled:false` on the active generation atomically removes the branch and QUIC block and stops context allocation before draining. Returns the status object below. Changing a prepared session never enables it. A candidate cannot replace an unrelated active generation: both prepare and publish compare its required `previous_generation` (null on first enable) against the current active/blocked generation. Mutations compare generation, serialize with config activation/TUN lifecycle, and complete within 5 seconds or return a known/uncertain result; a timed-out mutation requires a status query, never blind retry.

`DELETE /request-rewrite/session?generation=11111111111111111111111111111111` refuses while enabled, then closes all contexts/listeners, zeroes secrets, and returns `{"closed":true}`. A prepared candidate may be deleted without disturbing the active/blocked branch. It is idempotent for that closed generation; stale generations cannot close a new session. `GET /request-rewrite/status` returns no endpoints, tokens or request details:

```json
{
  "version": 1,
  "generation": "11111111111111111111111111111111",
  "engine_epoch": "22222222222222222222222222222222",
  "state": "active",
  "branch_enabled": true,
  "tun_running": true,
  "rule_version": "sha256:<canonical-document-digest-placeholder>",
  "contexts": 1,
  "streams": 1,
  "last_error": null
}
```

Engine states are `absent`, `prepared`, `active`, `blocked`, `draining`. Absent status keeps version/state/branch/TUN/count/error fields and omits session identity/version fields. `last_error` is null or a fixed redacted code, never raw text. Child readiness loss moves `active -> blocked`: the branch stays installed and rejects recognized authorized targets while other traffic routes normally. Restoring service stages and atomically publishes a newly probed generation against the blocked generation; the old policy stays blocking until replacement succeeds. It does not reuse old contexts or secrets. Status adds optional `prepared_generation` when a candidate exists; the remaining fields always describe the active/blocked generation, or the prepared one if no active generation exists.

No user MITM CA/private key or arbitrary user path belongs in engine configuration. The helper accepts only the bound UID's registered per-run loopback addresses and public transport certificate, writes its own root-private session snapshot and injects it through its restricted control path. User YAML and subscriptions cannot supply transport endpoints, credentials, context or skip flags. Session creation is an authenticated runtime operation; generated YAML cannot turn it on. T2 supplies the restricted engine policy and exact schema; T5 must extend the helper's endpoint/schema allowlist before privileged activation is available. Root writes only its own private runtime snapshots and channels, and cannot read arbitrary files requested by the child. Secrets reach the child through inherited pipes or owner-only runtime files (directories `0700`, files `0600`, no-follow creation); root-owned copies use root-private paths. Never put secrets in argv, environment, URLs, profile exports, controller logs, or source YAML. Remove startup files after reading, zero on shutdown, and rotate after any engine/child restart.

## Frozen v1 data channels and context lifetime

Both loopback listeners first require TLS 1.3 with mutual certificate authentication before exchanging any HTTP/authentication/context/application bytes. This transport TLS is distinct from intercepted application TLS and the user CA. The child and engine each generate one ephemeral, self-signed P-256 transport certificate/key per generation, valid no longer than 24 hours, with both client/server EKUs and SAN `nagi-rewrite-child.internal` or `nagi-rewrite-engine.internal` respectively. Keys stay in the generating process. The coordinator exchanges their public DER certificates via authenticated control and private child pipes. Each peer trusts only the exact received certificate, checks its role SAN/validity and pins its SPKI; no system-root fallback or optional verification. Dial loopback addresses directly while validating that fixed role SAN. A port-reuse attacker cannot pass the handshake or receive credentials/plaintext. A child cert is usable only for its registered UID/generation. The persistent coordinator schedules a fresh staged generation before certificate expiry (at least 5 minutes beforehand), using the same probe/CAS cutover; if replacement fails or the certificate expires, recognized authorized traffic stays blocked until explicit recovery or successful replacement. Certificates are never accepted after expiry, and clock errors fail closed.

Inside transport TLS, both listeners accept HTTP/1.1 CONNECT, with a 16 KiB total header limit, 5-second header/handshake timeout and no pre-auth body parsing. The sole non-CONNECT exception is the heartbeat endpoint below. Across active and candidate listeners, at most 64 pending TLS/header handshakes may exist per engine/user run; excess accepts close immediately before allocating goroutines/context state. The 5-second deadline bounds the complete TLS-plus-header stage, not each stage separately. No goproxy handler sees a request before this gate. `Authorization: Bearer ...` authenticates the channel in constant time. Wrong secret or non-loopback peer is rejected and closed before issuing any context or reaching goproxy. A loopback socket is not authentication. Internal fields are parsed and consumed by a dedicated adapter; no internal header can become an origin request header.

Engine-to-child handoff example:

```http
CONNECT api.service.example:443 HTTP/1.1
Host: api.service.example:443
Authorization: Bearer <handoff-secret-placeholder>
Nagi-Rewrite-Version: 1
Nagi-Rewrite-Generation: 11111111111111111111111111111111
Nagi-Rewrite-Epoch: 22222222222222222222222222222222
Nagi-Rewrite-Context: <random-256-bit-context-placeholder>
Nagi-Rewrite-Protocol: tls

```

`Nagi-Rewrite-Protocol` is `http` or `tls`, determined by mihomo. All displayed CONNECT messages occur inside the mutually authenticated transport TLS. After `200 Connection Established`, the bytes are the original plaintext HTTP or original TLS stream respectively. Plaintext HTTP uses `ConnectMitm` with an explicit plaintext adapter; TLS uses the per-user CA after verification of the classification. The adapter preserves prefetched bytes and rejects a TLS/plaintext mismatch. Reject duplicate/internal unknown fields and inconsistent CONNECT authority/Host. Application-supplied `Nagi-Rewrite-*` and proxy credentials are stripped before origin delivery and cannot supply context. The handoff carries no process path or UID: the engine retains them privately. A probe handoff uses the same headers plus `Nagi-Rewrite-Probe: 1`, target `nagi-rewrite-probe.invalid:443`, and protocol `http`. The child echoes a synthetic upstream CONNECT with the same probe header/context. Engine accepts only that target for a probe context and returns 200 without DNS or dialing; child exchanges a fixed `NAGI-REWRITE-PROBE-1` payload which the engine echoes then closes. Probe-only contexts expire after 5 seconds and cannot open any real target. Ordinary contexts reject the probe header.

Each context binds an unpredictable 256-bit token to engine epoch, generation, owner UID, original connection and metadata. Capture original process/path, UID, source address/port, destination, TUN inbound identity, and domain attribution before the branch; unavailable process identity stays explicitly unknown. Reinjection may change only destination host/port and requires engine reauthorization against the session's destination domains. The protocol never accepts `SpecialProxy`, `SpecialRules` or equivalent outbound overrides; a target change clears destination IP/DNS mode/sniff hostname/GeoIP/ASN/remote-destination caches before native matching. Restore original inbound/source/process fields and mark an internal, unforgeable skip-branch flag, separate from metadata visible to normal listeners. Never accept a client-specified process, UID, inbound, outbound selection, or skip flag. Do not label the restored application connection `INNER`, which would change routing semantics. The engine freezes the process-lookup-completed state even if captured identity is unknown: reinjection never looks up the child. Preserve source GeoIP/ASN and clear only target-derived fields on a target change. Original target remains in private context for audit-free rollback; routing uses the new target.

Child-to-engine upstream open:

```http
CONNECT mirror.service.example:443 HTTP/1.1
Host: mirror.service.example:443
Authorization: Bearer <reinjection-secret-placeholder>
Nagi-Rewrite-Version: 1
Nagi-Rewrite-Generation: 11111111111111111111111111111111
Nagi-Rewrite-Epoch: 22222222222222222222222222222222
Nagi-Rewrite-Context: <same-context-placeholder>
Nagi-Rewrite-Open: 1

```

The open counter is a strictly increasing unsigned 64-bit integer per context, starting at 1. Engine atomically accepts only `last_open + 1` and consumes it even if outbound dialing fails; duplicate/out-of-order opens fail 409 and close. Concurrent streams serialize open allocation and acknowledgement. An ambiguous lost acknowledgement closes the context; no retry/replay of an upstream request. Authenticated possession of a context allows multiple sequential or concurrent opens only while its original connection lives and within limits, not replay across sessions. An unauthorized target returns 403; expired/closed/unknown contexts return the same 409 error without disclosing which exists. A `200` response switches to raw TCP bytes of the changed target. For HTTPS the child establishes and verifies application TLS over that tunnel using the changed SNI; for HTTP it writes plaintext. Engine performs DNS through its configured resolver and normal outbound adapters, including Direct mode. Child does no OS DNS and has no raw target dial or environment proxy fallback.

The child sends `POST /_nagi/request-rewrite/heartbeat` to the reinjection listener once per second inside transport TLS, authenticated with its reinjection Bearer secret. Body (maximum 4096 bytes) is `{ "version":1, "generation":"11111111111111111111111111111111", "engine_epoch":"22222222222222222222222222222222", "rule_version":"sha256:<canonical-document-digest-placeholder>", "ready":true, "sequence":1 }`; unknown/duplicate fields fail. Response is `{ "accepted":true }`. Only a matching loaded version refreshes readiness; 3 seconds without a valid heartbeat marks the branch blocked and closes derived sockets. Heartbeats cannot create contexts or authorize targets. The child reports false readiness on local fatal failures, and never receives the general root controller socket. Heartbeats carry required integer `sequence`, starting at 1 per generation and strictly increasing for every send attempt (gaps allowed); the engine accepts only a value greater than its last accepted sequence. Duplicate/older sequences reject without refreshing readiness. A lost acknowledgement does not require a replay: the next attempt uses a newer sequence. The example above uses the first sequence. Readiness deadlines use monotonic elapsed time, not wall-clock timestamps; `ready:false` blocks immediately. A prepared candidate expires after 30 seconds without successful publication and is deleted without affecting the old branch.

A context expires 60 seconds after all handoff/upstream activity stops, at an absolute age of 3600 seconds, or immediately when its original connection closes, whichever occurs first. Active streams refresh idle activity but cannot extend the absolute deadline. Closing the original cancels pending DNS/dials and closes all derived connections, HTTP/2 streams, pooled transports and body work. Engine observes handoff socket closure directly; child cancellation and explicit session deletion are additional cleanup, not the only cleanup. Engine exit revokes all context tokens. Resource bounds: 1024 contexts/session, 64 active streams/context, 4096 upstream sockets/session, 256 concurrent body transforms with a 64 MiB aggregate body-memory budget. Admission failure blocks the affected authorized request and reports `rewrite_limit`, without evicting another live context. A JSON transform reserves memory before reading; oversized requests do not defeat the aggregate bound.

Upstream connection pools are partitioned by `(engine_epoch, generation, context, scheme, hostname, port, TLS policy)`; no cross-context reuse, coalescing, or proxy-wide default transport. A target change obtains a fresh partition. HTTP/2 streams may share only their own context/target partition. Closing a context closes idle and busy pooled connections. Disable automatic transport retries and redirect following; never duplicate non-idempotent requests after upstream writes. Opaque context and auth data live in Go context/adapter state rather than request headers passed to origin. Internal header removal includes all `Nagi-Rewrite-*`, `Authorization` from CONNECT, `Proxy-Authorization`, and hop-by-hop proxy fields; the application's ordinary end-server `Authorization` survives unless an explicit rule removes it.

## Rule document and deterministic transformations

Domain-library persistence: `config/request-rewrite/PROFILE.json`, mode `0600`, directory `0700`, separate from `config/rules` and profile sources. Per-user CA is independent under `config/request-rewrite/ca/`. Profile removal transactionally removes its rewrite document, never the user's CA. Subscription apply/update retains the target profile's local rewrite document, never imports authorization/rules/intent from feeds and never auto-enables rewriting. Switching selects only that profile's rewrite intent.

`rule_version` is `sha256:` followed by 64 lowercase hexadecimal digits over the validated normalized JSON document: recursively sort object keys by UTF-8 byte order, retain array order and original JSON number tokens, remove insignificant whitespace, emit UTF-8 strings with only required JSON escapes (lowercase `\u00xx` for control characters), and no trailing newline. Normalize domain fields before hashing. This avoids float64 canonicalization; equal numeric values with different tokens may have different versions. Symbolic digest placeholders in examples deliberately do not satisfy the wire digest schema.

Document limit is 256 KiB, 256 authorized domains, 1024 rules and 32 operations/rule. Stable IDs follow existing profile-name character/length constraints and are unique within each kind. Arrays define order; unknown fields, duplicate JSON keys and unsupported schema versions fail. Example complete JSON (disabled intentionally):

```json
{
  "version": 1,
  "enabled": false,
  "domains": [
    {"id":"api","pattern":"api.service.example"},
    {"id":"mirror","pattern":"mirror.service.example"}
  ],
  "rules": [
    {
      "id":"edit-query",
      "enabled":true,
      "match":{"domain":"api.service.example","methods":["GET"],"path_prefix":"/v1/"},
      "on_error":"block",
      "redirect_consent":[],
      "operations":[
        {"op":"header_set","name":"X-Demo","value":"example"},
        {"op":"header_delete","name":"X-Obsolete"},
        {"op":"url_set","component":"query","value":"mode=example"}
      ]
    },
    {
      "id":"edit-json",
      "enabled":false,
      "match":{"domain":"api.service.example","methods":["POST"],"path_prefix":"/v1/"},
      "on_error":"block",
      "redirect_consent":[{"domain":"mirror.service.example","schemes":["https"],"ports":[443]}],
      "operations":[
        {"op":"json_set","pointer":"/options/demo","value":true},
        {"op":"json_delete","pointer":"/obsolete"},
        {"op":"url_set","component":"host","value":"mirror.service.example"}
      ]
    }
  ]
}
```

All top-level fields and rule fields shown above are required. Operation-specific fields are exact. Match `domain` is an exact or explicit wildcard contained within an authorized pattern; rules cannot widen consent. `methods` is a nonempty array of uppercase HTTP token methods, maximum 16, and `path_prefix` is a UTF-8 string beginning `/`, maximum 2048 bytes. It compares the original escaped URL path, with empty path treated as `/`, case sensitively, without percent decoding or path cleaning. Prefix matching is literal: `/v1/` does not match `/v10/`. Domain/method/path matching always uses the original request, never a prior rule's changes. Every matching enabled rule runs in stored order; operations act on the working result of all prior operations. No implicit stop-after-first-match.

The request captures one immutable validated rule document/version at header acceptance. Rule/domain updates stage a new generation and child snapshot; each old request retains its immutable version until completion or cancellation at cutover. Engine domain policy and child version are published together with generation fencing. V1 cutover revokes all old contexts before acknowledging activation, including contexts whose consent remains unchanged; clients reconnect to use the new generation. No request observes a partial rule document and no old context acquires expanded consent. Pre-validate the complete candidate, compile matchers, validate cross-domain targets and CA/runtime requirements, then atomically save and activate through the coordinator. If persistence or activation fails, restore previous persisted intent/document and previously confirmed runtime rule version/policy, staging a fresh generation when the old generation was already revoked; never resurrect old contexts/tokens; if runtime rollback cannot be confirmed, report `uncertain` and leave recognized authorized traffic blocked. Never report success from a write alone.

| Operation | Exact semantics and validation |
| --- | --- |
| `header_set` | Required `name`, `value` strings; ASCII token name, no CR/LF/control bytes, value at most 8192 bytes. Replace all values case insensitively with one value. |
| `header_delete` | Required `name` with the same ASCII-token validation as set; remove all values, absent is success. |
| `url_set` | Required `component` enum `scheme`, `host`, `port`, `path`, `query`, and string `value`. Scheme is `http`/`https`; host is exact normalized authorized domain; port is decimal 1–65535; path starts `/` and is a valid escaped path; query is valid percent-encoded raw query without leading `?`. Empty query removes it. Empty port resets to scheme default; other empty values fail. Path/query reject raw `#`, control bytes, malformed percent escapes and invalid UTF-8; raw query preserves order and duplicate keys (there is no query-map normalization). No fragment or userinfo. |
| `json_set` | Required `pointer` and arbitrary JSON `value`; replace an existing value or add an object member. All parents must exist. Arrays replace an existing index or append using `-`; numeric index equal to length is also append. Reject leading-zero/nondecimal/out-of-range array indices. Empty pointer replaces the complete document. |
| `json_delete` | Required `pointer`; delete existing object member or array element (shifting later indices). Missing path is an error. Empty pointer and `-` are invalid deletes. |

JSON Pointer follows RFC 6901 (`~0`, `~1` decoding); reject malformed escapes. Reject duplicate keys in input bodies to avoid ambiguous edits. Decode numbers as preserved numeric tokens (`json.Number` or equivalent), never float64: `9007199254740993` remains exact. Serialization may change whitespace, but cannot round unchanged numbers. Header operations cannot set/delete `Host`, `Content-Length`, `Transfer-Encoding`, `Content-Encoding`, `Connection`, `Upgrade`, `Trailer`, proxy authentication, or any `Nagi-Rewrite-*`; URL/body machinery owns those fields. `Authorization` and `Cookie` are permitted rule fields and are sensitive. Total request headers after edits must remain within 64 KiB. Reject request smuggling ambiguities, conflicting framing, malformed authority, and URL/Host mismatches before any upstream write.

Only a matching JSON operation buffers a body. Require `application/json` or `application/*+json` (optional UTF-8 charset only), supported `identity` or one `gzip` Content-Encoding, complete valid UTF-8 JSON and a valid gzip checksum. Accept exactly one gzip member with no trailing bytes; multiple members are rejected. Reject concatenated JSON values. Default decompressed limit is **8 MiB** and compressed input is also capped at 8 MiB; v1 exposes no tuning option. Unsupported/stacked encodings, malformed gzip, invalid JSON, excessive size, and body read timeout are operation errors. Non-JSON rules stream bodies without parsing, decompressing or imposing this JSON size limit. Reserve bounded rollback storage before consuming JSON; original bytes and metadata remain available until commit. In-memory originals/working copies count toward the 64 MiB aggregate bound; no body spill to disk in v1. Recompress edited gzip with gzip, retain identity without encoding, recalculate Content-Length and remove Transfer-Encoding and stale body integrity headers (`Content-MD5`, `Digest`, `Content-Digest`, `Repr-Digest`). Both the final serialized JSON and recompressed gzip, if any, must also fit 8 MiB; exceeding either is a transformation error before commit. User signatures may become invalid; v1 does not resign requests. Trailers on JSON transforms are unsupported and fail before forwarding.

Default `on_error` is semantically `block` (explicit in stored v1 documents): no upstream bytes are sent; return sanitized HTTP 502 once HTTP is available, or close a failed TLS tunnel. `forward_original` is an explicit per-rule alternative for transformation errors only: discard **all** uncommitted edits from all rules for this request, stop rule evaluation, and forward the exact original request semantically, including original compressed body/headers/target and original engine routing context. It does not bypass consent, malformed framing, channel authentication, resource admission, CA/TLS validation, or upstream failures. If size/read/time failure prevents recovering a complete original, block. For a fully captured original, its body closer belongs to the request transaction; forward_original creates a fresh reader over the original compressed bytes, transfers closer ownership once to the transport and closes discarded working bodies. Partial originals are never sent. Non-JSON streaming bodies pass through without a rollback buffer because no rule consumes/edits them; header/URL rollback happens before first write. If the complete original cannot be recovered, block. Commit happens once after all operations/consent/body checks, before any upstream write. No fallback after commit or partial send, and no retry that could duplicate a request. Local errors include only a stable code and rule ID; no body fragment, URL query, credential or parser value.

## Implemented domain support

`internal/requestrewrite` implements the version 1 document schema, bounded strict JSON decoding (including duplicate-key rejection), normalized domains, deterministic digest, immutable snapshots, ordered request transformation and explicit sensitive preview. It performs no DNS, network requests, TLS interception or engine routing. `Snapshot.Transform` returns a request for a future adapter to send once; it does not activate interception. Matching uses the original authority/method/escaped path, while operations accumulate on a private request copy. Non-JSON bodies retain their original stream. Only matching JSON operations consume a body, preserve numeric tokens and original compressed bytes, and enforce identity/single-member gzip and input/output limits. Returned buffered bodies retain their resource reservation until closed; adapter consumers must always close success and rollback bodies. V1 currently reserves the full 64 MiB workspace conservatively for each admitted JSON transaction, so concurrent JSON work may receive `rewrite_limit` before the 256-transform ceiling. Tree/depth admission also rejects excessive JSON structure before building an unbounded object tree. Non-JSON streaming does not acquire that reservation. This is bounded domain admission, not evidence that the complete runtime resource gates are implemented.

Domain normalization uses `golang.org/x/net` **v0.35.0** (`idna` and `publicsuffix`) with indirect `golang.org/x/text` **v0.22.0**. These releases declare Go 1.18 and align with the existing `x/sys` v0.30.0; Nagi's Go 1.20 minimum is unchanged at this stage. Unicode domain input is rejected; validated ASCII A-labels are accepted. The Public Suffix List rejects wildcard consent for both ICANN and private suffixes. No goproxy dependency is introduced by this domain implementation; the Go 1.24/dependency adapter gate above remains T4 work.

`Store` loads missing documents as disabled empty state, supports domain/rule edits and full reorder, and validates complete immutable candidates before saving. Portable imports must be disabled and preserve independently stored intent; exports always clear enabled intent. Read summaries omit operation values; `Document`, `ExportSensitive` and `PreviewSensitive` are explicit sensitive boundaries and must not be included in ordinary status or diagnostics. Preview accepts only the supplied bounded fixture and performs no forwarding or persistence.

All managed storage operations use no-follow directory/file descriptors, validate owner/private modes and regular single-link files, and use synchronized private temporary replacement. The caller must hold Nagi's configuration lock before `Store` takes its document lock. Edits use an expected-version comparison to reject concurrent changes. `Store.Save` without an `Activation` implementation saves disabled configuration only and refuses enabled intent. An injected lifecycle coordinator must confirm `Apply` and `Rollback`; activation failure restores prior bytes/absence and requests runtime rollback, while failure to confirm either restoration reports `rewrite_state_uncertain`. This interface provides no runtime implementation, durable cross-process activation journal or claimed runtime success. T5 must compose it inside the ordinary recoverable configuration transaction, honor cancellation/deadlines and generation fencing, and supply actual status. Profile removal invokes secure rewrite-document removal inside the existing configuration transaction. Source/profile writes and subscription apply remain independent and preserve this local document; recreation after profile removal starts without its former consent. The per-user CA survives profile deletion. Ordinary configuration snapshot/restore excludes exactly `request-rewrite/ca/` (and rejects a link at that boundary), so transactions neither copy private keys nor overwrite/delete independently managed CA state; profile documents, including `ca.json`, remain inside rollback.

`CAStore.Generate` explicitly creates one P-256 self-signed CA in an atomic private `config/request-rewrite/ca/root.pem` bundle containing certificate and PKCS#8 key, refusing an existing bundle. `Status` and `ExportPublic` read only the public PEM prefix and private-file metadata; neither reads/decodes the following private key. Their `generated` state describes valid public certificate/storage metadata, not OS trust or proof that a runtime child loaded the key. Trust remains `unknown`. `LoadForChild` is the sensitive child-only boundary that checks certificate/key matching, self-signature, CA constraints, curve and validity before runtime use. Only public PEM leaves `ExportPublic`; CA generation never installs trust. T5 owns OS trust assessment/install/remove and runtime prerequisite enforcement. Machine certificate validity fields use RFC 3339 JSON timestamps; eventual human presentation must use minute precision.

Domain tests cover ordered original matching, cumulative edits, final contributor consent, exact large numbers, pointer errors, gzip and bounds, complete rollback, streaming, admission, malformed framing, immutable versions, strict documents, PSL/IDNA, isolation, mutation/import behavior, activation rollback uncertainty, private path attacks, public-only CA export and subscription/profile cleanup. They do not establish live TUN, OS trust, adapter TLS, helper or UI behavior.

## Lifecycle, state and failure recovery

One coordinator shares the existing [runtime locking and recovery model](runtime.md) across CLI, menu, App, monitor, and helper. Lock order remains quit, configuration, lifecycle, engine; the rewrite session belongs inside that transaction and introduces no inverted lock. Child process ownership is per user/backend/engine epoch/generation, recorded in private PID/start identity state and supervised independently of a transient menu/window. The hidden command `nagi internal request-rewrite serve` is not a public operation and receives startup descriptors rather than secret arguments. Ordinary command invocations request changes through the coordinator; they never create uncoordinated children. One staged candidate child is permitted alongside the active child only during a coordinator-owned generation replacement.

The child hosts an owner-only Unix control socket beneath the existing private runtime directory, validates peer UID against its startup owner, and accepts only `GET /status`, `POST /drain`, and `POST /shutdown`. JSON bodies (maximum 4096 bytes) for POST contain exactly `version:1` and `generation`; status is read-only and returns `version`, `generation`, `rule_version`, `ready`, `contexts`, and redacted `last_error`. Drain stops admissions and returns status; shutdown closes all sockets and exits, with idempotent cleanup. This socket exposes no raw request/CA/engine forwarding operation. Startup uses a bounded private pipe record (maximum 512 KiB) containing the full validated rule snapshot, generation, owner UID, secrets and public transport trust, and a second record supplies engine epoch/address/certificate after session preparation. Child readiness is acknowledged only after both records are validated; records are consumed and descriptors closed. Transient CLI commands use the socket to manage the persistent child; child heartbeat and its internal coordinator continue after the spawning CLI exits. They do not treat normal startup-pipe EOF as shutdown. New rule documents use a fresh staged child instead of mutating a live child's snapshot.

Enable sequence: validate selected profile and immutable candidate; query capability and actual TUN; verify a locally generated CA/key and required trust state for HTTPS; start child with disabled forwarding; prepare engine session; load child's rule snapshot; probe handoff and reinjection; publish branch; verify generation/version/actual state; persist enabled intent within the recoverable transaction. Missing CA, untrusted CA, missing capability, stopped TUN, or failed probe leaves enable rejected and prior intent/runtime restored. Because v1 authorizes domains rather than a protocol-specific domain subset, every enabled session requires a valid trusted CA even if the current workload is plaintext HTTP. CA generation/trust is always an explicit operation, never an enable side effect. No external network test is an enable prerequisite. The readiness probe is strictly an internal no-DNS/no-outbound channel test, not connectivity or leak detection.

Disable sequence: remove engine branch and authorized QUIC block first and confirm removal; stop admitting contexts; drain existing work for at most **5 seconds**; forcibly close remaining contexts/pools; delete session; stop child; persist disabled intent. If branch removal cannot be confirmed, keep child available where possible, report uncertain state, and do not claim disabled. Force closure after the deadline is successful bounded cleanup, not original-forward fallback. The branch's removal allows new traffic to follow ordinary routing by explicit disable intent.

| Trigger | Required resulting state and action |
| --- | --- |
| TUN stop, engine stop, App Quit | Same coordinator removes branch, drains/closes child, then performs existing engine/TUN/DNS cleanup. Explicit rewrite intent is retained unless `request-rewrite disable` was requested. |
| Engine exits unexpectedly | Contexts/tokens disappear with engine. Stop orphan child; re-enable after engine recovery only for saved enabled intent with actual TUN and full readiness checks. |
| Child crashes or readiness heartbeat is lost | Within 3 seconds (1-second heartbeat), engine marks branch blocked and rejects recognized authorized targets. Unrelated traffic continues; no original/direct fallback. Restart through a new generation/probe or explicit disable. |
| Profile or user/privileged backend switch | Freeze new admissions, remove old branch, drain within 5 seconds and revoke old session; validate new profile/backend and start a new session only for its own enabled intent and actual TUN. No context/pool/secret reuse across backends. Roll back the entire switch on activation failure; unknown final state is explicit. |
| Rule/domain update | Validate before save; stage immutable child snapshot and engine domain policy; generation-fenced publish, close all old contexts at atomic generation replacement. Keep prior snapshot until commit/rollback is confirmed. No half-updated consent. |
| Ordinary engine config reload / TUN patch | Preserve the active session, branch and epoch while TUN remains active, applying current native routing to new reinjection opens. Do not create a temporary bypass during reload. If TUN is lost, atomically stop context admission and revoke the session before completing TUN shutdown; retained intent is revalidated on recovery. An implementation unable to preserve the branch must keep recognized authorized targets blocked until a new probed generation is published. |
| Reload times out | Query engine generation/version and existing `activation_id` where applicable. Restore disk and confirmed runtime if possible. Late/uncertain activation remains uncertain and blocked; never blindly retry or claim rollback. |
| CA trust removed/key unavailable | Reject enable; if already active, block new intercepted TLS and require explicit recovery/disable. Never generate a replacement root silently. |
| Menu/window closes | Child and engine continue. This is not App Quit. |

Proposed `request-rewrite status` data fields: `profile` (string or omitted with profile error), `enabled_intent` (boolean), `actual` (`disabled`, `starting`, `active`, `blocked`, `draining`, `unavailable`, `uncertain`), `tun_running` (boolean or null), `backend` (`user`/`privileged`), `service_running` (boolean or null), `protocol_version` (1 or null), `configured_rule_version`/`active_rule_version` (digest or null), `ca` (`missing`, `generated`, `trusted`, `untrusted`, `unknown`), `generation` (ID or null), and `last_error` (null or redacted code). Actual state comes from confirmed runtime, not saved intent or PID alone. Reads remain usable with invalid profile data; recovery/disable must remain accessible. Human timestamps use minute precision; machine timestamps, if added to state records, use RFC 3339 consistent with existing interfaces.

## CA, secrets and diagnostics

Generate one local CA per user through an explicit command using cryptographic randomness; private key `0600` beneath `0700` directories, never embedded in a binary, root configuration or export. Public certificate export excludes the key. Trust install/remove are separate explicit platform operations through the CLI, displaying their effects; do not imply generation installs trust. Disable retains CA and trust. Removal of trust does not delete the private key. V1 provides no CA rotation/replacement/delete command. Manual replacement is outside the supported v1 interface; a later version must define a separate explicit migration and trust-removal contract before offering rotation. The child alone reads the user key, verifies key/cert match and validity, and uses a bounded 256-entry leaf-certificate cache. No root helper request accepts arbitrary key paths or exports key material. macOS user trust and Linux trust mechanisms require separate platform verification; unsupported stores return actionable unsupported errors rather than claiming trusted.

Normal status/errors/logs contain only counts, stable codes, rule IDs and versions. Never log Cookie, Authorization, body, full requests, URL queries, internal secrets/context tokens or CA key. Replace goproxy's default verbose logging and error rendering, including transport/HTTP2/TLS paths. Explicit rule exports and request previews may contain sensitive header values, bodies and URLs; label that boundary before displaying or writing them, use private files, and never include them in diagnostics automatically. Preview operates on a supplied request only; it makes no network/DNS call and does not imply live interception coverage. Public CA export is distinct from private rule/request export. Ordinary diagnostics provide redacted readiness/coverage facts only.

## Planned CLI contract

All syntax here is reserved for implementation, not usable today. Follow [CLI usability rules](../development/cli-usability.md), reuse the existing `ok/data` success and `ok/error` stderr envelope, stdout success, and exit codes 0 success, 2 usage, 1 operation failure. New error code is `request_rewrite_error`; its sanitized message identifies a stable reason such as `rewrite_capability_missing`, `rewrite_tun_required`, `rewrite_ca_required`, `rewrite_invalid`, `rewrite_auth`, `rewrite_limit`, `rewrite_activation_failed`, or `rewrite_state_uncertain`, with a concrete recovery command. Do not add incompatible top-level envelope fields. Help and completion must work without valid configuration or mihomo.

`--profile NAME` applies to domain/rule/import/export/validate/preview operations (default selected profile); enable/disable target selected profile only and reject `--profile`. File input uses a regular private local file or `-` for stdin, at most 256 KiB for rule documents, 8 MiB for preview request bodies, and never a URL. Secret-bearing values are supplied inside input documents, never argv. Exports refuse existing paths and create `0600` files; `-` explicitly writes raw stdout. All edits return redacted status/version rather than echoing input. `--json` is supported except explicit raw exports (`--json` with raw export is usage error); reads/list/detail redact secret-bearing values. The explicit export command below is the only complete rule read.

| Proposed command | Input/result |
| --- | --- |
| `request-rewrite status` | Confirm intent, actual state, versions, CA and recovery fields above. |
| `request-rewrite enable`, `request-rewrite disable` | Coordinated lifecycle; return status after confirmed outcome. |
| `request-rewrite domains list [--profile NAME]` | `domains` array of IDs/patterns and profile. |
| `request-rewrite domains add FILE|- [--profile NAME]` | One `{id,pattern}` document; validate consent and save/activate transactionally. |
| `request-rewrite domains update ID FILE|- [--profile NAME]` | Replace same-ID entry; dangling rules reject rather than silently disable. |
| `request-rewrite domains remove ID [--profile NAME]` | Refuse referenced authorization; remove only after rules are changed. |
| `request-rewrite rules list [--profile NAME]`, `request-rewrite rules show ID [--profile NAME]` | Ordered IDs, enabled/match/operation kinds/on_error and redacted values; never header/body values. |
| `request-rewrite rules add FILE|- [--profile NAME]`, `request-rewrite rules update ID FILE|- [--profile NAME]` | One complete rule, exact ID on update, append on add, immutable candidate validation. |
| `request-rewrite rules remove ID [--profile NAME]` | Transactional removal. |
| `request-rewrite rules enable ID [--profile NAME]`, `request-rewrite rules disable ID [--profile NAME]` | Toggle one rule; never enable feature automatically. |
| `request-rewrite rules reorder FILE|- [--profile NAME]` | JSON array listing every rule ID exactly once, including disabled rules. |
| `request-rewrite import FILE|- [--profile NAME]` | Replace complete rule/domain document after validation; imported `enabled` must be false, otherwise reject. Retain existing enabled intent independently, never enable from import. |
| `request-rewrite export FILE|- [--profile NAME]` | Explicit sensitive raw JSON, always `enabled:false` for portable export; omit CA/secrets/runtime. |
| `request-rewrite validate [FILE|-] [--profile NAME]` | Validate supplied document or persisted one without activation/DNS; return `valid` and redacted field diagnostics. Operation failure if invalid. |
| `request-rewrite preview FILE|- [--profile NAME]` | Sensitive fixture `{scheme,host,port,method,path,query,headers,body_base64}`; return rule IDs, outcome and transformed request without forwarding or persisting it. Valid base64 body; total decoded body <=8 MiB; larger framing overhead rejected with bounded input allowance. |
| `request-rewrite ca status`, `request-rewrite ca generate` | Per-user metadata/trust assessment; generate refuses existing CA. |
| `request-rewrite ca export FILE|-` | Public PEM only, raw stdout/file rules above. |
| `request-rewrite ca trust install`, `request-rewrite ca trust remove` | Explicit OS trust effects; return verified platform state or unknown/error. |

Preview fixture envelope is limited to 12 MiB encoded to admit base64 of an 8 MiB body plus at most 64 KiB headers; ordinary document limits do not apply to this command. `port` is integer 1–65535; headers are a mapping to string arrays, body_base64 is required (empty for no body), and path/query follow `url_set` validation. `scheme` and normalized `host` must be authorized. Preview rejects unknown/duplicate fields and malformed framing. JSON preview returns its sensitive result inside `data`; text preview labels sensitive output. Domain/rule mutation results include `profile`, `saved:true`, `configured_rule_version`, `active_rule_version`, `actual`; list/show include `profile` and configured version. Import ignores neither a true enable flag nor invalid fields. Live mutations while disabled save validated state without starting engine/TUN/child. Enable is always a separate user action.

Implement every commandSpec, usage, missing/invalid-argument check, text empty state, JSON error/exit compatibility and Bash/Zsh/Fish completion. Completion can offer only local profile/rule/domain IDs through read-only sources; never complete secret values. Move implemented syntax to [CLI reference](../reference/cli.md) and keep this document authoritative for the internal protocol and transformation semantics, linking instead of duplicating full definitions.

## Terminal menu and macOS interaction

Terminal Settings gains **Request Rewrite**, with Status/Enable or Disable, Authorized Domains, Ordered Rules, Preview, CA, and Recovery pages. Explain default-off and TUN/CA prerequisites before enable. Domains support add/edit/remove; rules support add/edit/remove/toggle/reorder, header/URL/JSON operations, on-error choice and explicit destination consent. Preview accepts a local fixture through CLI input and warns of sensitive output; CA separates generation, public export, trust install and trust remove. Recovery exposes status, validation and disable even after read failures.

Use existing numeric menu/navigation, ten-item pagination, `0` Back/Cancel, Ctrl-C and EOF behavior. In text fields `0`, `b`, and `q` remain literal input. Cancellation never promises to undo an ordinary command already submitted. Forms retain all values after validation or command errors and permit edit/resubmit/cancel. Cancellation/EOF before submit mutates nothing; dispatch exactly once after submission and never automatically retry. Keep local drafts in UI memory, not persisted files. Known IDs are single argv elements; complete sensitive documents go to CLI stdin, not arguments or temporary public files. Menu code performs no direct storage, engine API, rule matching, CA or trust operations.

The macOS menu bar shows actual state and a daily enable/disable entry plus **Request Rewrite…**, opening an independent native window. Window pages provide equivalent status, domain and ordered-rule CRUD, toggles/reorder, preview, CA/trust, and recovery. It invokes CLI JSON/input interfaces only, with cancellable reads and serialized mutations through the existing operation coordinator. Do not hide failures behind cached state or infer active from intent. Retain editable drafts after validation/CLI failures, allow explicit resubmission and never auto-retry mutations. Cancel/close before submit mutates nothing; closing a window after submission does not promise cancellation or rollback of that operation. Closing the window or terminal menu leaves the child running. App **Quit** uses existing CLI cleanup and includes rewrite teardown before engine stop. CLI retains advanced automation, import/export, diagnostics and batch operations; the UI adds no business implementation. No current macOS behavior is claimed by this plan.

## Serial handoffs and acceptance matrix

T1 produced the frozen design, proposed ADR and README navigation. T2–T8 follow the original ownership below; T3 domain support is implemented as described above, while later scopes remain separately authorized work. Each task updates its affected authoritative documentation after behavior settles and runs focused checks before handoff; failures or contract changes return to this design rather than silently drifting. Use isolated temporary homes, runtime directories, throwaway local CA/trust fixtures and local HTTP/TLS servers. Never use real subscriptions, an existing user CA, ordinary proxy ports or the shared running proxy for tests. Real macOS UI, OS trust and TUN behavior remains unverified until exercised on a suitable host.

| Task | Ownership and frozen input | Required acceptance evidence |
| --- | --- | --- |
| T2 | Separate mihomo repository: branch placement, protocol/domain recognition, identity capture, v1 control/data channels, context registry, rerouting, QUIC domain policy and restricted policy. Helper schema implications must be supplied to T5. | Plain HTTP/TLS handoff bytes preserved; process/UID/source/inbound survive; changed domain routes differently under Rule/Global/Direct; fake-IP handled; internal DNS/node/reinjected skip; bad auth/UID/replay/expiry/target/context bounds rejected; original close cancels resources; child crash blocks only recognized authorized targets; probe has no outbound/DNS. Build/test exact engine commit. |
| T3 | Nagi rule/domain library, profile-local storage and CA primitives. | Ordered original matching, cumulative edits, immutable digest, final consent, precision/gzip/limits/rollback, only JSON buffers, sensitive no-network preview, private files, profile isolation, subscription preservation, explicit CA generation and public export. |
| T4 | Nagi goproxy v1.9.2 adapter and persistent internal child; Go minimum/dependency/build changes. Consume T2 protocol and T3 immutable domain library. | No environment proxy, OS DNS, TLS skip or raw errors; context/target pool separation; header stripping, streaming, cancellation and upgrade gates; authenticated channels and child control. HTTP/2 stays disabled until its complete gate passes. |
| T5 | Lifecycle/coordinator, privileged helper allowlist, runtime status, platform trust, complete ordinary CLI commands/help/completion and CLI reference. | Actual TUN and explicit CA/trust prerequisites; prepare/probe/publish; bounded disable/crash recovery; shared profile/backend/TUN/engine/App Quit coordinator; durable persistence/confirmed runtime rollback/uncertainty; owner-bound helper/files/pipes with no root CA key; full command inventory, bounds, import semantics, envelope/exit/help, sensitive preview/export and redaction. |
| T6 | Terminal Settings Request Rewrite pages through CLI only. | Daily CRUD, toggle/reorder, preview/CA/recovery; numeric navigation, retained drafts, EOF/cancel no pre-submit mutation, one dispatch, no direct API/config access; redirected/JSON menu compatibility. |
| T7 | macOS menu state/toggle and independent window through CLI only. | Equivalent daily operations, actual vs intent, serialized mutations, stale-read handling, no secrets in argv; window close preserves service; App Quit tears down in coordinator; native build/tests and explicit interactive host result. |
| T8 | Cross-repository integration, release pin, final docs and platform evidence. | Run combined local server/temporary CA matrix in all modes/backends; crash/race/rollback/resource tests; four target cross-builds; real macOS/TUN/trust/UI acceptance or explicit unverified report. Publish/obtain new engine commit from declared remote before any `engine.lock` change; then matching locked build and startup capability checks. No push/publication is authorized by T1. |

T2 must deliver endpoint examples and negative tests matching the exact v1 fields above, engine epoch/generation fencing, proof that routing cannot overwrite captured identity, actual TUN reporting and heartbeat definition. T4/T5 must confirm the original connection closure reaches HTTP/1, HTTP/2 (if enabled), upgrades and pooled sockets. Any required change to dependency, wire schema, limits, target authorization or failure semantics requires an explicit revision of this planned contract before implementation consumers proceed.

## T1 verification and open platform gates

T1 baseline evidence is read-only `git show`/`git grep` of the exact engine object and tracked goproxy `v1.9.2` files, including go.mod, proxy.go, certs.go, https.go, http2.go and websocket.go. A local cached locked checkout supplied the engine object; no dependency installation or runtime change was needed. Manual review verifies examples against this proposed schema, relative Markdown destinations and IDs, design/ADR/README status wording, absence of real secrets, and `git diff --check`. Documentation verification is not an implementation test.

Unverified gates: real TUN capture/domain visibility/process lookup, OS trust behavior, macOS native interactions, production upgrade and HTTP/2 compatibility, and full multi-process cleanup under actual backend failures. These are T2–T8 acceptance work, not evidence that this feature exists or a prerequisite to accepting the T1 documents.
