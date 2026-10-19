# Request capture and quick rules

Status: accepted

## Context

Rule-scoped rewriting requires users to know a destination before inspecting it.
Users need to discover requests and derive transformations from original data.

## Alternatives

- Require an existing enabled rule to admit inspection.
- Build a separate packet, routing and TLS proxy stack in Nagi.
- Extend the pinned engine's existing handoff and record content in ordinary-UID
  Nagi services independently of rule generations.

## Decision

Use the third option. Capture is explicitly authorized raw-content inspection,
defaults to all eligible TUN application traffic, and has optional App/domain
scope. Viewing, searching and export need no reveal toggle or repeated consent.
Original identity and forwarding remain engine-owned. Recording is bounded,
memory-only by default and independent of immutable rewrite generations.
Quick rules use the existing validator, local preview and activation transaction.
The [capture design](../design/request-capture.md) owns the implementation contract.

## Consequences

Both repositories, ordinary and privileged control paths, CLI and UI must agree
on the extension. Capture does not authorize arbitrary reinjection targets.
Explicit exports contain original credentials/content; ordinary diagnostics do
not automatically receive them. Native platform coverage remains a separate
acceptance requirement and is not established by synthetic fixtures.
