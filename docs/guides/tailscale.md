# Tailscale exit bindings

This guide covers managing top-level Tailscale nodes and connecting ordinary proxy servers through a Tailscale exit node. Mihomo owns Tailscale login, exit selection, and forwarding; Nagi saves and activates configuration.

## Prepare a profile and key

Import or select a profile containing your proxy nodes. Have a dedicated tailnet, an approved exit node, and a Tailscale Auth Key available. Store the key in a private regular file outside shared directories. For Headscale, use its HTTPS control-service address and a key issued by that service.

```sh
nagi profile use work
nagi proxy tailscale targets
nagi proxy tailscale add 'TS Exit' EXIT_NODE ./auth-key.txt --hostname nagi-work
# Headscale instead of the official service:
nagi proxy tailscale edit 'TS Exit' --control-url https://headscale.example.com --auth-key-file ./headscale-key.txt
```

Replace `EXIT_NODE` with your exit-node IP or name accepted by mihomo. Replace file paths and the example control address with your own values. Ordinary results report whether a key is configured, without printing its value or source file path. Keys remain sensitive configuration data: protect profile overrides and avoid sharing raw configuration output.

## Bind a node or provider

```sh
nagi proxy tailscale bind 'TS Exit' node 'Node A'
nagi proxy tailscale bind 'TS Exit' provider work-feed
nagi proxy tailscale show 'TS Exit'
```

The path becomes application → Nagi/mihomo → Tailscale exit → selected proxy server → destination. Select the ordinary proxy node through your normal proxy group. Groups are selection controls and cannot be binding targets.

A provider binding affects **all its nodes**, including nodes added by refresh. It changes `override.dialer-proxy`, retaining the provider's download proxy. A pre-existing non-Tailscale front proxy must be removed deliberately before binding; Nagi allows switching an existing Tailscale binding to another Tailscale exit.

For the terminal menu, open **Select Node → Tailscale**. Add a node, open its details, and choose a target from Bind. Provider entries disclose their all-node effect. Forms keep entered values after a failed submission and require an explicit retry.

## Activation and persistence

Queries and target selection work while mihomo is stopped. Mutations require the matching mihomo executable for validation. Saving while stopped leaves the engine stopped; saving while running reloads its configuration. A successful save or reload does not establish that Tailscale login, exit approval, or the proxy path works. Reload can interrupt existing Tailscale connections.

Nagi stores node-list changes as a full local override snapshot. That snapshot takes precedence over later source changes and preserving-source automatic subscription refresh. Explicit `subscription apply` clears the local override, including Tailscale nodes and bindings; inspect and recreate them afterward. See the [CLI reference](../reference/cli.md#tailscale-management) for exact fields and limits.

User-mode and privileged-mode identities are stored separately. The helper uses root-private, UID-isolated state and never imports a user's existing identity. Switching backends can require a reusable or new Auth Key. These commands do not install a helper, change backend, or enable TUN; follow the [traffic access guide](traffic-access.md) for those operations.

## Change or remove bindings

```sh
nagi proxy tailscale edit 'TS Exit' --exit-node NEW_EXIT_NODE
nagi proxy tailscale unbind node 'Node A'
nagi proxy tailscale unbind provider work-feed
nagi proxy tailscale remove 'TS Exit'
```

Omitting `--auth-key-file` during edit keeps the current key. Remove is refused while a node or provider references the Tailscale node; known provider payload and DNS references also block removal, while effective configuration validation protects rule and group references. Unbind writes an explicit empty front proxy so a source binding cannot reappear. Removing configuration leaves identity state available for reuse.

If activation fails, inspect `nagi status` and the selected profile override. Nagi restores saved configuration and attempts runtime recovery; a timed-out reload may still apply, so heed any uncertain-recovery error and retry `nagi config reload` after it settles. Login and exit failures during forwarding belong to mihomo: check your service's key, exit approval, and access policy.
