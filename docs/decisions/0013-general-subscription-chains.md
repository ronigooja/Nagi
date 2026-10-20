# General subscription chains

Status: accepted

This decision supersedes [decision 0007](0007-persistent-subscription-tailscale.md). It records ownership and persistence of ordered subscription chains; exact commands belong to the [CLI reference](../reference/cli.md#proxy-nodes-and-chains).

## Context

A single retained Tailscale exit cannot describe reverse ordering, a WireGuard or SOCKS hop, or a chain containing several nodes. Subscription replacement must replay the complete chain without freezing the feed's nodes or discarding credentials. Intermediate nodes can be shared by several targets, so replacing one dialer can change another path.

## Alternatives

1. Extend the Tailscale binding format with protocol-specific cases. This preserves a narrow interface but duplicates general chain semantics and cannot clearly represent ordered definitions.
2. Store only ordered hop names in ordinary override snapshots. Names cannot recover removed feed definitions or their credentials, and list snapshots can freeze subscription updates.
3. Store a versioned profile-local subscription policy with ordered complete hop definitions and compose native mihomo dialer edges over the effective profile.

## Decision

Use option 3. Nagi owns node configuration, explicit group membership, ordered user intent, dependency analysis, composition, private persistence, and transactional activation. Mihomo owns every protocol connection, dialer operation, DNS lookup, route, and packet forward.

Treat target scope and hop order as separate inputs. Preview every affected edge and shared use before mutation. Clear the first hop's dialer explicitly, link successive hops in traffic order, and link eligible targets to the last hop. Provider overrides cover future payload nodes without changing download proxies. Clearing a target removes its incoming edge only; it does not recursively clear shared upstream segments.

Keep subscription association and source-hash checks. Store complete sensitive hop definitions with a policy version and enough source identity to detect collisions. Replay the policy over current effective nodes and providers after explicit and scheduled application, excluding chain members from targets. Reject the complete candidate on a dependency, collision, validation, save, or activation failure. Keep unchanged source/cache semantics and pending-cache behavior.

Do not provide compatibility commands, compatibility DTOs, or automatic migration of decision 0007's policy. Report an existing legacy policy as unsupported rather than silently ignoring or interpreting it. Clearing or replacing a current policy preserves retired hop definitions as ordinary local additions so references and credentials are not silently lost.

## Consequences

The interface handles both Tailscale directions and mixed-protocol multi-hop paths using one model. Policies contain credentials for every retained hop and require owner-only permissions, redacted ordinary output, and complete configuration-transaction coverage. Managed policy edges cannot be individually changed; editing a managed Tailscale definition must update its policy copy. Existing single-exit policy users need an explicit replacement workflow. Neither successful validation nor activation establishes authenticated forwarding or platform network readiness. Tailscale identity isolation remains governed by [decision 0006](0006-privileged-tailscale-state.md).
