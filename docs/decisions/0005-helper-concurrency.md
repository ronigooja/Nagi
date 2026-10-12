# Concurrent privileged helper snapshots

Status: accepted

This decision records how the privileged helper schedules requests while preserving lifecycle and configuration ownership. The implemented flow is defined in the [runtime design](../design/runtime.md).

## Context

The macOS app reads one-second traffic samples through CLI helper requests while refreshing engine status and menu state. The pinned mihomo `/traffic` endpoint waits for a ticker before writing its first sample and then streams indefinitely. A helper that handles each accepted connection serially and retains its lifecycle mutex during this HTTP read queues unrelated status and snapshot requests behind every sample.

The helper owns root process lifecycle, restricted configuration activation, authenticated session requests, and DNS recovery. Those operations must keep their existing ordering and security checks. Mihomo continues to implement traffic sampling and networking.

## Alternatives

- Keep all requests serial and suspend traffic polling around every refresh. This leaves helper clients coupled and repeats scheduling work in UI callers.
- Run every request concurrently, including every GET. This lets lifecycle and configuration mutations race; some mihomo GET measurement endpoints update automatic selections or health state.
- Serve bounded concurrent connections and release the runtime lock only for explicit controller snapshots.

## Decision

Use the third option, with at most 32 accepted connections. Clients still decoding a request consume that bound and retain their ten-second request deadline. Authenticate and validate each connection independently. Keep lifecycle, configuration, DNS operations, and controller actions with side effects serialized.

Snapshot GETs capture the child identity and configuration generation under the runtime lock, perform bounded controller I/O outside it, then reacquire the lock before accepting a result. Engine replacement, exit, or configuration activation rejects an older reply. Proxy selection and latency measurements do not advance the configuration generation, allowing existing traffic samples to complete.

On cancellation or listener failure, cancel all workers, close their sockets to unblock protocol reads and writes, and drain accepted requests before closing the runtime handler. Use cancellation watchers compatible with the project's Go 1.20 minimum.

## Consequences

A traffic sample can wait independently of short status and snapshot requests. Mutations still wait for other mutations, and DNS journal reads retain their transaction locking. The connection bound limits worker and request memory growth but allows many incomplete clients to consume all slots until cancellation or their request deadline.

A snapshot interrupted by lifecycle or configuration activation can return `stale_controller_response`, so callers receive an explicit failure instead of data from a prior engine. Controller I/O may reach a replacement socket during a concurrent restart, but its captured child check prevents that response from being accepted. Configuration activation timing and rollback uncertainty remain governed by the existing activation-ID workflow.
