# Rule-driven content rewrite

Status: accepted

This decision replaces [0010](0010-tun-request-rewrite.md) for rewrite scope,
response/message processing and script isolation. The
[current design](../design/request-rewrite.md) owns the complete contract.

## Context

Users need ordered request, response and bidirectional WebSocket content edits.
A separate domain consent list duplicates rule scope and makes enabling the first
rule require unrelated configuration. Response bodies and scripts introduce
commit boundaries, memory costs and protocol obligations that must be explicit.

## Alternatives

- Retain separate consent entries and request-only transformations.
- Give Nagi its own capture, DNS, routing and protocol forwarding engine.
- Execute host JavaScript with broad process or I/O capabilities.
- Derive scope from rules, keep mihomo transport ownership, and execute bounded
  transformations with a sandboxed guest and message-level adapters.

## Decision

Use one v2 rule document with four stages and referenced scripts. Enabled rule
predicates derive interception domains; request consent adds a separate target
set. Nagi validates the actual final destination against executed rule consent.
Mihomo retains capture, original identity, routing, DNS and outbound connections.

HTTP requests/responses share immutable snapshots and send upstream once.
Fragment Range editing preserves request intent and byte lengths; fetching a
complete representation requires explicit full mode. Body and trailer errors
respect commit boundaries. WebSockets use a protocol library for control frames,
masking and fragmentation and Nagi for ordered complete-message transformations.

Run deterministic QuickJS/WASM in wazero with a fresh instance, guest memory
limits, deadline, bounded host input/output and no external capabilities. Script
body access is declared. Treat guest linear memory, logical conversion workspace
and host RSS as separate measurements. Do not maintain a second configuration
parser or historical command/protocol paths.

## Consequences

Configuration, command, UI and engine changes must land together. Source and
binary dependencies need fixed versions, licenses and reproducible artifacts.
Large input capacity must be verified through every internal/public transport.
Failures after commit terminate the stream; response failure cannot resend an
already executed upstream operation. Native TUN, CA trust and UI verification
require the corresponding host and remain explicit acceptance gates.
