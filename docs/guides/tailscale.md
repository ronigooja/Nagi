# Tailscale command replacement

Tailscale nodes now use the common [proxy nodes and chains workflow](proxy-nodes-and-chains.md). Use `nagi proxy node` for node settings and group membership, and `nagi proxy chain` for node, provider, or subscription chains.

The former `nagi proxy tailscale` commands and single-exit subscription policy format are unsupported. Nagi does not migrate the old policy automatically. Consult the [CLI compatibility contract](../reference/cli.md#proxy-nodes-and-chains) before replacing an existing policy. Mihomo continues to own Tailscale identity, login, and forwarding.
