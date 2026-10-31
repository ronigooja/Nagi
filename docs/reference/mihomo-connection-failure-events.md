# Mihomo connection failure events

This document defines the connection-failure event contract used by Nagi.
**Status: implemented in mihomo commit
`3aaa47ec730b121ee58b7b2000f60fbd1dc2c14f`.** The engine exposes the endpoint
and publishes failures from the supported tunnel connection paths.

## Endpoint and compatibility

The authenticated controller exposes an HTTP NDJSON stream:

```text
GET /connection-events?kind=failure
```

Each successful event is one JSON object followed by a newline. Privileged
callers may request one event with:

```text
GET /connection-events?kind=failure&once=true&wait_ms=25000
```

The one-shot form returns `204 No Content` when the wait expires. The endpoint
does not change `/connections`, its WebSocket snapshot mode, or close actions.

## Event format

```json
{
  "type": "connection_failure",
  "occurred_at": "2026-10-31T12:34:56.789Z",
  "connection_id": "failure-1",
  "target": "example.test:443",
  "network": "tcp",
  "proxy": "Proxy A",
  "metadata": {
    "host": "example.test",
    "process": "curl",
    "processPath": "/usr/bin/curl",
    "destinationPort": "443",
    "type": "HTTPS"
  },
  "error": {
    "code": "connect_timeout",
    "message": "connection attempt timed out"
  }
}
```

`occurred_at` is the UTC occurrence time. Metadata is best effort. The event
contains a stable process-local failure ID, but it is not an active connection
ID and cannot be closed. Error messages are fixed safe summaries; raw errors
and credentials are not sent by the event publisher.

The current stable codes are:

```text
resolve_failed
route_failed
proxy_dial_failed
connect_timeout
```

## Delivery and retention

The engine uses a bounded per-subscriber channel with a default capacity of
128. Publishing never blocks tunnel processing. A slow subscriber can lose
events; the current implementation does not replay history or emit a
synthetic gap record. Nagi treats stream termination or unavailable engine
support as an observation interruption and never invents individual failure
records.

## Implemented coverage

The engine publishes events for:

- TCP metadata preprocessing failures;
- TCP route selection failures;
- TCP proxy dialing and handshake failures;
- UDP route selection failures;
- UDP packet-dial failures.

Successful connections continue to be added to the ordinary connection
tracker only after the outbound connection succeeds. Existing `/connections`
snapshots and closure behavior are unchanged.

Failures that occur before an inbound connection reaches the routed tunnel
boundary, and failures deep inside protocol adapters that do not return through
the routed dial boundary, are not yet published as separate events.

## Nagi integration

The Go CLI exposes the stream through `nagi connections failures`. The same
event source is merged into `nagi connections watch` as optional
`failure_events` records. The macOS Connections window retains these records
in memory for five minutes, deduplicates them by ID, displays them under the
Failed filter, and never offers a close action.

The Go protocol package at
[`internal/connectionevents`](../../internal/connectionevents/) documents the
planned versioned replay/gap protocol. The current engine endpoint is the
minimal live NDJSON implementation and does not yet expose that replay cursor.

## Verification

Mihomo tests cover safe metadata publication, bounded subscriber behavior, the
controller route, one-shot timeout compatibility, and preservation of the
existing statistic/route packages. A platform-specific tunnel test may require
an operating system with UID-routing support.
