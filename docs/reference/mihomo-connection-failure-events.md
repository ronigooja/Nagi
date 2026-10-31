# Mihomo connection failure events

This document defines the versioned connection-failure event contract for the
Nagi integration. **Status: foundation only.** The current pinned mihomo
source (`ronigooja/mihomo`, commit
`8e139d44bc86d1107042edc95514ec7733dd2d94`) does not yet expose this endpoint
or publish these events. The Go package
[`internal/connectionevents`](../../internal/connectionevents/) provides the
wire types and bounded-history semantics so the engine and Nagi clients can be
implemented against one contract without changing `/connections`.

## Endpoint and compatibility

The future endpoint is a dedicated WebSocket at `GET /connection-events` on
the existing mihomo controller. It must not change `GET /connections`, its
WebSocket snapshot mode, or the close operations under `/connections`.

The stream sends JSON text messages. The first message is `hello`; subsequent
messages are `connection_failure` or `gap`. Clients must validate `version`
and `kind`, ignore unknown fields, and reject unknown versions or kinds. The
stream is process-local: a restart creates a new `stream_id` and resets the
sequence namespace.

## Messages

### Hello

```json
{
  "version": 1,
  "kind": "hello",
  "stream_id": "opaque-process-id",
  "capacity": 1024,
  "oldest_sequence": 42,
  "latest_sequence": 1065
}
```

`oldest_sequence` and `latest_sequence` are zero when no event exists. The
queue capacity is fixed by the engine and is informational; clients cannot
change it through the endpoint.

### Connection failure

```json
{
  "version": 1,
  "kind": "connection_failure",
  "event_id": "opaque-process-id/1066",
  "sequence": 1066,
  "timestamp": "2026-10-31T12:34:56.789Z",
  "stage": "dial",
  "code": "connection_refused",
  "metadata": {
    "network": "tcp",
    "source": "192.0.2.10:50000",
    "destination": "example.test:443",
    "destination_host": "example.test",
    "destination_port": 443,
    "rule": "MATCH",
    "proxy": "Proxy A",
    "chains": ["Proxy A"]
  }
}
```

`event_id` is `stream_id/sequence`. It is opaque and stable for the life of
the event stream. `timestamp` is the occurrence time in UTC RFC 3339 format;
it is not the WebSocket delivery time. Metadata is best effort and every
field is optional. `connection_id` may be absent because many failures occur
before a connection tracker is created.

The stable stages are `resolve`, `route`, `dial`, `handshake`, and `forward`.
The stable codes are `dns_failed`, `route_failed`, `proxy_dial_failed`,
`proxy_handshake_failed`, `connect_timeout`, `connection_refused`,
`network_unreachable`, `tls_failed`, `canceled`, and `unknown`. The engine may
add codes only in a later protocol version; clients must not branch on raw
error text. Raw error strings and credentials are not sent by this contract.

### Gap

```json
{
  "version": 1,
  "kind": "gap",
  "from_sequence": 100,
  "to_sequence": 141,
  "dropped": 42,
  "reason": "overflow"
}
```

The queue is bounded to 1024 failure events by default and producers never
block. When full, mihomo drops the oldest events. A subscriber whose cursor is
older than retained history receives one gap before the retained events. The
range is inclusive and `dropped` equals `to_sequence-from_sequence+1`.
Consumers must advance their cursor to `to_sequence` and mark the interval as
unavailable; they must not infer individual failures from it. A gap is not a
failure event and has no `event_id` or timestamp.

The initial implementation may start a subscriber at the current latest
sequence. If a future `since` query is added, it must use the same inclusive
gap rules and must not silently replay an incomplete history.

## Required engine insertion points

The pinned source currently has these relevant pieces:

- `hub/route/connections.go` exposes snapshots and periodic WebSocket
  snapshots through `statistic.DefaultManager`.
- `tunnel/tunnel.go` resolves routing and calls `proxy.DialContext` in the TCP
  path, then logs failures in `logMetadataErr`.
- `adapter/adapter.go` delegates to proxy adapters, but protocol adapters and
  DNS dialers make additional nested calls outside one shared failure hook.

The source has no event hub, process stream ID, sequence cursor, or centralized
failure classifier. A complete engine implementation must therefore:

1. Create one process-lifetime event log with the bounded semantics above.
2. Add a non-blocking publication hook at the routed TCP and UDP failure
   boundaries, preserving the existing return errors and `/connections`
   tracker behavior.
3. Classify errors at the operation boundary into the stable stage/code pair;
   do not classify by controller or client log parsing.
4. Add the dedicated authenticated controller route and WebSocket writer.
5. Cover DNS resolution, route selection, proxy dialing, protocol handshake,
   and forwarding failures separately. A hook only around
   `tunnel/tunnel.go`'s TCP `retry` callback is not complete coverage.

Those engine changes are intentionally not represented as completed here:
the Nagi repository contains no mihomo source, and the inspected pinned source
has no existing failure-event insertion points. `/connections` remains the
authoritative active-connection snapshot API until the engine endpoint is
published and pinned.

## Validation

The protocol package tests cover monotonic process-local IDs, UTC occurrence
timestamps, metadata ownership, bounded retention, explicit overflow gaps,
JSON validation, and invalid queue configuration. They do not claim runtime
mihomo coverage. Once the engine endpoint exists, add engine tests for each
insertion point above, controller authentication, slow subscribers, restart
stream IDs, and preservation of existing `/connections` behavior.
