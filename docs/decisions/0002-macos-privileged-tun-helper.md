# 0002 macOS privileged TUN helper

Status: accepted

## Context

Nagi currently starts mihomo from a per-user launchd Agent. On macOS, that
process cannot create the native `utun` interface when TUN mode is enabled;
sing-tun returns `Connect: operation not permitted`. A root mihomo process can
create the interface, and the pinned build must include the `with_gvisor` tag
when a profile selects the gVisor stack.

## Alternatives

1. Keep starting mihomo as the logged-in user. This requires no installation
   changes but cannot activate TUN on affected macOS systems.
2. Move traffic handling into a Network Extension. This requires a packet
   tunnel provider, application groups, entitlements, signing, user approval,
   and a new data path between the extension and mihomo.
3. Add a small privileged LaunchDaemon helper. Nagi remains a user process;
   the helper starts and stops mihomo with narrowly scoped requests.

## Decision

Use the privileged LaunchDaemon helper. The helper will expose a private,
peer-authenticated Unix socket and accept only validated lifecycle requests.
It will start mihomo with the selected effective profile, a root-only
controller socket, and controlled log and state paths. Nagi will continue to
own profile and subscription data, while the helper owns the privileged child
process and signals. The helper must reject paths outside the requesting
user's Nagi data directory and must not expose a TCP listener. It must also
restrict or proxy control API requests: giving the user direct access to a
root mihomo controller would allow arbitrary configuration reload requests.
The helper must validate configuration features and referenced files before
allowing root mihomo to consume user-owned configuration.

The existing per-user launchd Agent remains the default for profiles that do
not require privileged TUN. Enabling privileged mode will be explicit and
will require administrator authorization during helper installation.

## Consequences

The implementation needs a versioned IPC protocol, a root LaunchDaemon
installer/uninstaller, peer credential checks, a restricted control API path,
configuration validation, and lifecycle integration in the engine manager.
The installer must run as root and use only a root-owned executable under a
root-owned, non-user-writable path. Failures must leave the user-mode path
available. Installation and use remain disabled until these security checks
are implemented and tested.

Network Extension support remains out of scope unless a future requirement
needs App Store-style sandboxing or packet-tunnel APIs.

## Implementation status

The configuration policy currently accepts only a small literal subset:
`mode: direct`, basic log/IPv6 settings, and selected TUN booleans and stack
values. It rejects proxy nodes, providers, listeners, external controllers,
file references, YAML aliases, unknown fields, and multiple YAML documents.
On Linux, validated bytes can be copied into a private root-owned snapshot
directory; root mihomo must never open the user's mutable profile path. macOS
snapshot creation remains disabled until ACLs can be checked in addition to
POSIX ownership and mode. This subset does not support subscription-backed
proxy use. Helper installation and privileged engine activation remain
disabled until the controller API is mediated and the complete
configuration/runtime path can be validated safely.
