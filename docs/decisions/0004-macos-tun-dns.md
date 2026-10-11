# Manage macOS system DNS while TUN is active

Status: accepted

This decision assigns system DNS configuration and recovery to Nagi while keeping DNS resolution and packet interception in mihomo.

## Context

macOS can obtain a LAN DNS server through DHCP. Its connected LAN route is more specific than mihomo's automatic TUN routes. Queries to that server leave through the physical interface and never reach TUN's DNS hijack handler. Configuring encrypted mihomo upstreams does not affect those queries. The pinned Darwin TUN implementation does not add LAN DNS interception through `strict-route`.

An independent connection failure also exposed a second DNS path: a proxy protocol can send a hostname to its server even after local rule evaluation has resolved that hostname. The server then chooses the destination using its own resolution or policy. Nagi's managed DNS policy needs an explicit mihomo option to use the configured resolver's real address for proxied TCP destinations.

## Alternatives

- Keep system DNS unchanged and rely on `dns-hijack`. This leaves connected LAN DNS outside TUN.
- Add a route to the LAN DNS address. A DNS address can also be the default gateway, and changing its route affects traffic beyond DNS. This requires additional route ownership and recovery work.
- Manage network-service DNS settings while TUN runs. This uses macOS configuration tools and preserves the existing TUN implementation.
- Replace hostname preservation globally in proxy adapters. This changes existing protocol behavior, group selection, and remote-only hostname semantics without an explicit policy.

## Decision

Nagi manages macOS network-service DNS while the running TUN adapter is available and mihomo DNS is enabled. It derives DNS endpoints from the actual TUN addresses, rather than assuming a fixed subnet. It records each service's original static DNS list or DHCP state before changing it. Changes are verified; failures trigger compensation, and an incomplete restoration retains its journal for retry. External edits are reported rather than overwritten.

The privileged helper derives targets from its own running engine and keeps its journal in root-private storage. Its authenticated DNS operations accept no client-supplied command, address, or filesystem path. Engine exit recovery and configuration reconciliation use the same DNS manager. User-mode operation uses a private user journal and an exit watcher, and reports insufficient network-service permissions as an operation failure.

mihomo provides the explicit `dns.resolve-proxy-destinations` option. Nagi enables it in new default profiles and managed DNS overrides. It resolves proxied TCP destinations with mihomo's configured real resolver at the protocol adapter boundary, preserving hostnames for group selection and reporting. It preserves direct outbound and internal DNS transport behavior and does not silently fall back to remote resolution after a DNS failure. The option remains disabled by default in mihomo for compatibility.

## Consequences

TUN activation on macOS includes an OS configuration transaction and can fail when DNS ownership, permissions, or restoration cannot be established. Disabling TUN, stopping the engine, and recovering after an unexpected exit must restore system DNS. Reloads must reconcile DNS with the actual running TUN state, including reloads that complete after a caller timeout.

System proxy settings remain a separate HTTP/HTTPS operation. Application-specific encrypted DNS, other VPN resolvers, and third-party configuration changes are not made equivalent to mihomo DNS by this decision. Configuration checks retain their documented scope; external packet capture and DNS leak tests remain explicit integration checks.
