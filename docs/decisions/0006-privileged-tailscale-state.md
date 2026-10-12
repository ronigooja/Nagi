# Privileged Tailscale identity state

Status: accepted

## Context

Tailscale stores a persistent identity. A root mihomo process must not reuse mutable user-owned identity files or write to a user-chosen `state-dir`. Configuration snapshots are temporary and cannot be the lifetime owner of identity state. Configuration rollback must also leave live engine state intact.

## Alternatives

1. Reject Tailscale in privileged mode, retaining only user-mode support.
2. Copy user identity into each helper snapshot. This crosses the trust boundary and couples identity lifetime to snapshot cleanup.
3. Allocate persistent state inside the existing UID-isolated helper directory and rewrite every top-level Tailscale state path before validation.

## Decision

Use option 3. Nagi owns path allocation and rewriting; mihomo owns identity, login, exit initialization, and adapter lifecycle. Stable identifiers derive from normalized configuration identity, node name, and control-service address. Root state is inside the helper's `.tailscale-state` subtree, with root ownership and mode `0700`; symbolic links and paths outside the private boundary are refused. Mihomo independently verifies the root-state boundary under restricted policy.

The helper does not read or copy user identity and does not honor user-supplied root state paths. Only top-level Tailscale nodes are permitted in privileged mode. Provider-created Tailscale nodes are rejected for inline, file, and remote payloads; supported ordinary provider nodes may reference a top-level Tailscale node.

User-mode managed state belongs to the configuration directory's dedicated `.tailscale-state` subtree. Configuration transaction snapshots exclude precisely that subtree. State survives ordinary node removal, engine restart, and helper snapshot cleanup. There is no automatic identity migration between user and root backends.

Mihomo closes old top-level Tailscale adapters synchronously before activating replacements and serializes each adapter's Start and Close. A configured exit must finish login and exit preference initialization before forwarding; initialization errors fail forwarding.

## Consequences

Restart and reload can reuse identity without treating it as configuration. Switching backend or control service can require a reusable or new Auth Key. Reload may interrupt Tailscale connections. Persistent state can outlive a removed node and consumes private disk space. Supporting provider-created Tailscale nodes later requires a separate identity-allocation and lifecycle design.

The helper and engine policies must evolve together, as in [decision 0003](0003-restricted-root-configuration.md). Native macOS helper/TUN/DNS restoration and real tailnet forwarding require runtime acceptance beyond cross compilation.
