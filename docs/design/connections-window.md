# macOS Connections window

This document describes the implemented macOS 13+ Connections window, its CLI boundary, request lifecycle, and failure states. It displays current active connections without storing history. The [App guide](../../macos/NagiApp/README.md#active-connections) describes use; the [CLI reference](../reference/cli.md#proxy-latency-connections-and-mode) defines commands, JSON fields, response limits, and closure semantics.

## Responsibilities and interfaces

mihomo owns connection tracking, rule matches, traffic counters, and closure. Nagi's CLI reads `GET /connections` and submits the existing `DELETE /connections/ID` and `DELETE /connections` requests through its selected ordinary or privileged backend. The App invokes `nagi --json connections list`, `connections close ID`, and `connections close-all` through the installed CLI. It does not call mihomo or read runtime files.

The connection client decodes typed snapshot data on a worker queue. Missing fields display `unknown`; numeric and string ports are accepted, IPv6 endpoints are bracketed, and traffic counters are explicitly cumulative. The CLI does not pass connection start time, so duration is not shown.

An independent connection model owns the snapshot, filtered and sorted rows, selected ID, request generation, read task, close task, timer, and error state. Search and sort calculation runs outside the main actor. The AppKit controller owns native views, column descriptors, search editing, and scroll anchoring. Only view updates occur on the main actor. The menu delegate retains one resizable window controller, presents it again on repeated menu selection, and preserves the accessory activation policy.

## Snapshot lifecycle

Opening a visible window immediately requests a snapshot. A one-second timer attempts reads; an in-flight read skips the tick without queueing another. Manual refresh shares that gate. Window close, minimization or hiding, App hiding, sleep, and starting quit invalidate the generation and cancel the read and timer. Cancellation terminates the CLI child, with bounded escalation if it ignores termination. A later read waits for cancellation to finish. Old results cannot replace a newer state. Becoming visible or waking starts an immediate read again. Explicit window ordering hooks complement delegate and App hide notifications. If AppKit declines native minimization, a cancellable 500 ms fallback hides the window; reopening cancels that fallback and resumes reading.

A successful read replaces the snapshot and updates the last-success timestamp, which the window displays in the local time zone to the minute. Stable IDs retain selection and scrolling despite insertions, removal, sorting, and filtering. A selected connection removed from the snapshot clears selection and details and reports its disappearance. Details read the selected item from that same snapshot; they do not issue `connections show`.

Engine state comes from the existing App status model. A confirmed stop clears connection data and shows recovery guidance. Status unavailability suspends reads without inferring a stop. A connection API failure also does not change engine state: it retains the prior snapshot as stale, records the error, and disables closure until a successful read. A response-size failure retains data as stale and pauses automatic reads, with explicit manual retry. Backend 4 MiB overflow and App 8 MiB stdout overflow have separate messages. Results are never truncated.

## Closure and settings coordination

The selected ID is captured before submitting a single close request; row indices are never operation identifiers. The all-connections sheet discloses that search does not limit its scope and that applications can reconnect. Once confirmed, it submits once without automatic retry, even on timeout.

Before dispatch, the model invalidates and cancels the current read and waits for its child to finish. Close controls remain disabled while the operation runs. Completion records request acceptance or failure and immediately refreshes if the window is eligible. An absent ID is accepted by mihomo's existing idempotent endpoint; no closed count is inferred.

The App settings gate prevents close requests from overlapping configuration changes or quit cleanup. Settings wait for a submitted close operation to finish; connections pause while settings run. Starting quit cancels reads immediately and waits for an existing close operation before invoking CLI cleanup. A failed quit releases the gate and restores reading for a visible window. Reads do not set the App's settings busy flag, trigger settings animation, or invoke menu refresh.

## Verification scope

Swift tests exercise decoding, search/sort order, selection identity, timer skips, cancellation and obsolete results, stale/overflow recovery, single dispatch, and coordination with settings and quit. Native window checks on macOS cover layout, table behavior, window reuse and visibility, confirmation, and lifecycle notifications. Response-boundary tests cover both Go backends and the App stdout/stderr limits. macOS packaging and sustained near-limit refresh checks complement those tests. An isolated test CLI may be injected for window validation without changing the installed engine.

The view is a periodic snapshot; connections shorter than its interval can be absent. It does not preserve historical connections, infer duration, verify routes or DNS leaks, or introduce configuration options.
