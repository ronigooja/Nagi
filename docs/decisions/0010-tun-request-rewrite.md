# 0010 TUN request rewriting through an unprivileged Nagi service

Status: superseded

Superseded by [0011: rule-driven content rewrite](0011-rule-driven-content-rewrite.md). This record preserves the original request-only decision and rationale.

This decision records the accepted ownership and security boundaries for opt-in HTTP/HTTPS request rewriting. The domain library, service, coordinator, helper and CLI are implemented against the published engine commit now recorded in `engine.lock`; Nagi release publication and UI/platform acceptance remain pending. Complete interfaces, semantics, lifecycle, coverage limits and acceptance tasks belong to the [request rewrite design](../design/request-rewrite.md).

## Context

Users need explicit, per-profile request edits for authorized domains while retaining mihomo's TUN capture, DNS, process-aware routing and outbound selection. At the pre-implementation audit baseline recorded in the design, the pinned engine had protocol sniffing and metadata but no authenticated rewrite handoff/reinjection contract. Ordinary proxy listener redirection loses the application's identity and can recurse or bypass routing. HTTPS edits also require an explicitly generated and trusted local CA.

Nagi currently owns user operations and engine management; mihomo owns network implementation. Request content transformation in Nagi is a deliberate narrow exception: a separate unprivileged Nagi child interprets user-authorized content rules, while mihomo retains every traffic capture, DNS, identity, routing and outbound decision. This exception does not authorize Nagi to implement a competing TUN, resolver, protocol router or HTTP/3 stack.

## Alternatives

1. Implement transformations and CA ownership inside mihomo. This keeps all bytes inside the engine but couples application consent, content rules and CA lifecycle to a potentially privileged process and expands its responsibilities.
2. Run a second engine or ordinary local proxy and change system proxy/routing settings. This duplicates network ownership, can lose original process/UID identity, and makes mode, DNS and loop behavior difficult to preserve.
3. Add a narrow engine branch after TUN identification and before routing, handing authorized HTTP/TLS to an unprivileged Nagi goproxy service, then restore opaque original context on authenticated reinjection.
4. Use system-proxy-only interception. This excludes applications that rely on TUN and cannot satisfy the intended coverage or identity contract.

## Decision

Choose option 3. Keep one mihomo instance and default rewriting off. Require actual running TUN, per-profile explicit domain authorization, immutable ordered request rules and a per-user locally generated CA. Generation and OS trust installation/removal are separate user actions. Subscription updates retain local settings and never grant authorization or enable rewriting.

Mihomo retains opaque original source/process/UID/inbound context, recognizes eligible domains/protocols, branches before final routing and reroutes changed targets after authenticated reinjection. Internal DNS, node connections and reinjected flows skip the branch. Rule, Global and Direct determine only the final outbound. Unknown traffic retains ordinary forwarding with documented coverage limits; recognized authorized targets fail closed on interception/service errors. Block QUIC only when the engine identifies an authorized domain; implement no HTTP/3 proxy.

Use the frozen goproxy release, toolchain gate, bounded mutually authenticated transport-TLS channels and exact v1 protocol defined in the design. Replace environment-proxy defaults and configure explicit verifying upstream TLS; require engine-only dialing/DNS and context/target-partitioned pools. Validate new destinations against both profile authorization and explicit rule consent. Default transformation failure blocks; explicit per-rule original-forward discards all uncommitted edits and cannot bypass channel, consent or TLS failures.

The persistent internal CLI child runs unprivileged and outlives menu/window sessions. A shared coordinator covers enable, disable, TUN/engine stop, profile/backend switches and App Quit. Remove interception before a bounded drain; a crashed child keeps recognized authorized targets blocked until explicit recovery or disable. The privileged helper exposes only bounded, UID-bound management messages; neither it nor root engine configuration receives the user's CA private key or arbitrary file access.

## Consequences

Users gain explicit request edits while routing and DNS remain engine-owned. Original application identity and domain consent become enforceable across reinjection, at the cost of a versioned cross-repository protocol, additional process supervision, CA management and stricter failure handling. Certificate-pinned clients, hidden domain identity and unsupported application protocols remain coverage limitations. HTTP/2 and upgraded streams require adapter evidence before support claims.

Implementation must test context expiry/replay/closure, new-target routing, secret redaction, rollback, resource bounds and platform trust without using real subscriptions or normal proxy state. The implementing engine commit is now published, obtainable from the declared remote and recorded in `engine.lock`; older engines must still return an actionable capability error through Nagi. Future engine lock updates likewise require the implementing commit to be published and obtainable from the declared remote. Current architecture and CLI reference remain descriptions of implemented behavior; planned syntax stays in the design until each implementation lands.
