# 0003 Restricted root configuration

Status: accepted

## Context

The privileged helper must run mihomo as root for TUN on macOS. A user profile may contain providers, proxy TLS files, plugins, and nested protocol settings. Passing the user-owned file directly to root mihomo, or relying only on a Nagi field filter, would leave mutable file reads and protocol-specific escape surfaces at the privilege boundary.

## Alternatives

1. Reject all providers and local references. This limits common subscriptions and TLS profiles.
2. Reimplement every mihomo protocol and provider validator in Nagi. That duplicates engine semantics and can drift as the engine evolves.
3. Copy a narrow set of local references into root-private storage and enforce an immutable policy in mihomo before parsing and activation.

## Decision

Use option 3. The helper binds one authenticated session UID, opens source files with no-follow `openat` traversal confined to that user's Nagi configuration directory, and copies permitted file provider payloads and proxy TLS files into a root-owned mode `0700` directory as mode `0600` snapshots. Relative references are anchored to the source profile. The helper checks the rewritten profile through mihomo's `-t --check-json --config-policy restricted` before start or reload and always starts the child with `--config-policy restricted`.

Mihomo owns the protocol, built-in plugin, provider, listener, and nested option allowlists. It rechecks remote provider nodes after decoding, conversion, and override. Nagi owns safe source reads, snapshot lifetime, activation ID validation and forwarding, and an explicit controller API allowlist. A 32-character lowercase hexadecimal activation ID is passed unchanged to `PUT /configs`; a timed-out reload may still apply, so the helper retains all snapshots and references until the child exits. The controller remains root-private.

## Consequences

Common inline and HTTP subscription providers, controlled file providers, inline WireGuard keys, and audited built-in plugins can run in privileged mode. Scripts, arbitrary local files, external plugins, and exposed controllers fail validation. The helper and mihomo restricted policy must be updated together when adding a new protocol feature. Privileged mode requires a matching engine binary from the commit in `engine.lock`. Retaining snapshots after uncertain reloads increases private disk use until the process stops, and prevents delayed provider reads from losing their files.
