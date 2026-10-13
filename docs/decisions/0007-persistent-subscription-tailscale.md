# Persistent subscription Tailscale bindings

Status: accepted

## Context

A whole-subscription binding must follow an actual Nagi subscription across explicit application and scheduled refresh, including newly downloaded nodes. Existing local proxy-list overrides replace the entire source list and explicit subscription apply clears them. Reusing those overrides would either freeze the feed's node list or discard its Tailscale exit and key.

## Alternatives

1. Bind only current nodes through the existing full-list override. Future nodes would not inherit the binding.
2. Treat the subscription as a mihomo proxy provider. This would change the meaning of an existing Nagi subscription and would not preserve its complete configuration.
3. Keep a separate profile-local policy and regenerate native mihomo front-proxy fields against each effective subscription configuration.

## Decision

Use option 3. Nagi owns durable user intent, configuration composition, validation, and activation. The policy retains the selected top-level Tailscale definition independently of ordinary local overrides. Mihomo remains responsible for Tailscale login, identity, exit selection, provider payload overrides, and forwarding. No new engine protocol or connectivity-verification mechanism is required.

Preserve the source and cache as subscription data so scheduler source hashes retain their existing meaning. Convert a source-matching local node snapshot when entering subscription scope, but refuse arbitrary local replacements instead of silently discarding them. Apply the managed policy atomically to a candidate; conflicts fail the candidate rather than allowing some nodes to use a different front proxy.

## Consequences

Explicit apply can replace the feed and clear ordinary local customizations while retaining the chosen exit and binding intent. New eligible nodes inherit the policy. The retained definition is sensitive configuration because it includes the Auth Key. Individual binding changes require leaving subscription scope, and a conflicting later feed can remain pending until corrected. Unbind retains the exit as an ordinary local addition; persistent identity storage continues to follow [decision 0006](0006-privileged-tailscale-state.md).

The exact implemented composition, failure, and cleanup behavior is maintained in the [runtime design](../design/runtime.md#tailscale-configuration-and-identity); commands and eligibility belong to the [CLI reference](../reference/cli.md#tailscale-management).
