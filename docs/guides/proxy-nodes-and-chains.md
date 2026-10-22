# Proxy nodes and chains

This guide covers selected-profile node and proxy group management, group membership, and ordered outbound chains. Mihomo implements protocol connections and forwarding; Nagi saves, validates, and activates native configuration. Use the matching engine pinned in `engine.lock`.

## Create and manage a proxy group

Open **Proxies → Proxy Groups → Create proxy group** in `nagi menu`. Enter the group name, choose manual selection, automatic latency selection, fallback, or load balancing, and select nodes/groups or proxy providers. Automatic types expose their test settings. Choose **Save proxy group** to validate and save; failed saves keep the draft for correction. Back before Save leaves configuration unchanged. **Manage proxy groups** contains the group list and remains available while the engine is stopped. Select a group for editing and deletion, or choose **Select node** to open its live node list when mihomo is running. **Show group details** on that node list returns to the same settings.

For example, with `Node A` and `Node B` already configured:

```sh
nagi proxy group add 'My Group' --node 'Node A' --node 'Node B'
nagi proxy group add 'Automatic' --type url-test --node 'Node A' --node 'Node B'
nagi proxy group list
nagi proxy group edit 'My Group' --node 'Node B' --node DIRECT
nagi proxy group remove 'My Group'
```

Creating a group makes it available for selection but leaves routing rules unchanged. To use it, select it in global mode or target it from a routing rule. A group still used by rules, DNS, or another dependency cannot be deleted until those references are removed. Changes live in the selected profile's local override and activate immediately only if mihomo is running. See the [group reference](../reference/cli.md#proxy-group-management) for options, defaults, persistence, and failure behavior.

## Change proxy group display order

Open **Proxies → Proxy Groups → Manage proxy groups → Adjust display order**. Select a group, move it up/down or to the top/bottom, or use **Move to position** with a group number from the page where you selected it, then choose **Save display order**. All moves stay within the current page: top/bottom means the page’s first/last group, and up/down stops at its boundaries. Back before saving discards the draft. This changes the group display order immediately without reloading mihomo or switching nodes; node order inside each group stays the same. The sorting action is available when at least two configured groups exist, including while stopped.

For a profile containing exactly `Work`, `Automatic`, and `Media`, the equivalent CLI operation is:

```sh
nagi proxy group list
nagi proxy group order 'Work' 'Automatic' 'Media'
```

List every current group exactly once. Subscription updates retain the saved order and append new groups. If a save fails because another operation added/removed groups, reopen sorting and try again. See the [group reference](../reference/cli.md#proxy-group-management) for persistence and JSON behavior.

## Inspect nodes and add a Tailscale node

```sh
nagi profile use work
nagi proxy node list
nagi proxy node show 'Node A'
nagi proxy node add tailscale 'TS Exit' EXIT_NODE --group 'Proxy'
nagi proxy node groups show 'TS Exit'
```

Replace the example names and `EXIT_NODE` with your own configuration values. Tailscale requires a dedicated tailnet, an approved exit, and a valid Auth Key. The terminal form accepts visible key paste; do not paste credentials into shell arguments. For automation, use `--auth-key-file FILE` or `--auth-key-stdin`. Keep key files private. Headscale uses `--control-url https://headscale.example.com` with the service's key. Ordinary summaries show key presence, never key content. Raw configuration, override output, and explicitly viewed or exported logs can contain secrets.

`node list` includes all top-level nodes, including WireGuard and SOCKS5. Provider payload node inventory remains separate from this list; individual supported provider nodes can participate in chains. Adding and editing protocol settings currently supports Tailscale; ordinary nodes have details and group membership controls. Configure chains through **Node Chains** or `proxy chain`. Repeated `--group` options add a new node to several groups in the same transaction.

```sh
nagi proxy node groups set 'TS Exit' 'Proxy' 'Manual'
nagi proxy node groups clear 'TS Exit'
```

Setting memberships replaces explicit memberships atomically. Automatic group inclusion and filters are reported separately; clearing explicit membership does not disable automatic inclusion. Group membership makes a node selectable; it does not change traffic routing or select the node automatically.

## Build a chain in traffic order

`--via` names the first hop from Nagi, followed by each subsequent hop. The target is last. For example:

```sh
nagi proxy chain candidates node 'Node A'
nagi proxy chain set node 'Node A' --via 'TS Exit' --dry-run
nagi proxy chain set node 'Node A' --via 'TS Exit'
nagi proxy chain set node 'TS Exit' --via 'Node A'
nagi proxy chain set node 'Node A' --via 'SOCKS Entry' --via 'WG Transit'
nagi proxy chain show node 'Node A'
```

The first example forwards application → Tailscale exit → Node A → destination. The reverse example forwards application → Node A → Tailscale exit → destination. The three-hop example forwards application → SOCKS Entry → WG Transit → Node A → destination. Use only one of the alternatives unless deliberately replacing an existing chain. The pinned engine supports Tailscale as a later hop and WireGuard as a chain member. Candidates include supported native dialer protocols; final validation still depends on the complete configuration and backend restrictions.

A chain sets each node's native `dialer-proxy`. Its first hop receives an explicit empty dialer so inherited source configuration cannot add another hop. Intermediate nodes are shared objects: changing their dialer can affect other targets. Review the dry-run plan's changes, shared use, and conflicts before applying. A conflicting preview is inspectable; committing that plan fails as a whole. Self references, repeated members, unresolved dependencies, and indirect cycles are refused.

```sh
nagi proxy chain set node 'Node A' 'Node B' --via 'TS Exit'
nagi proxy chain set provider work-feed --via 'SOCKS Entry' --via 'TS Exit'
nagi proxy chain list
nagi proxy chain clear node 'Node A' 'Node B' --dry-run
nagi proxy chain clear node 'Node A' 'Node B'
```

A batch contains distinct targets of one kind and shares one ordered hop list. A provider chain affects all current and future payload nodes through `override.dialer-proxy`; its download proxy is retained. Clearing a target clears its final incoming dialer only. It does not recursively dismantle upstream segments or other chains.

## Use a single provider node in a chain

Identify a provider member by both its source name and its effective node name. It can be the first, middle, or final hop:

```sh
nagi proxy chain set node 'Node A' --via-provider work-feed 'Entry Node'
nagi proxy chain set node 'Node A' --via 'SOCKS Entry' --via-provider work-feed 'Transit Node' --dry-run
nagi proxy chain set provider-node work-feed 'Exit Node' --via 'SOCKS Entry'
nagi proxy chain show provider-node work-feed 'Exit Node'
nagi proxy chain clear provider-node work-feed 'Exit Node'
```

Repeated `--via` and `--via-provider` options preserve their mixed traffic order. Nagi isolates the selected member through mihomo's exact provider filtering and native dialer settings; the original provider and its source nodes remain available. A provider member used as the exit creates an independent, readable chain outlet. Saving appends that outlet to related existing manual selection groups without switching their selection; choose the reported outlet in one of those groups or GLOBAL to use it. Disconnecting removes this outlet while retaining source nodes and upstream segments.

Inspection uses local provider payloads or caches and does not download missing data. While the privileged backend is running, its authenticated helper supplies only raw node names and protocols from the active profile’s private payload/cache; candidates must also remain in mihomo’s current provider list. Raw identities support regex renames and added prefixes/suffixes without exposing node credentials. The helper rejects evidence from a different profile, changed provider definition, uncertain reload, symlink, or file larger than 8 MiB. An unavailable cache is reported explicitly. While stopped, inspection uses only user-readable local evidence, so a root-only cache remains unavailable. Update the CLI and installed helper together to use this operation. Provider Tailscale members cannot be derived safely because of their persistent identity and are unsupported; top-level Tailscale nodes retain their existing chain support. Provider members cannot be retained as hops in a subscription policy; use node or provider scope.

When an explicit native provider refresh loses the selected node, it reports failure and can retain that same last-good member. It does not choose another node or fall back to DIRECT. For file and HTTP providers, missing initial content leaves the bridge using REJECT; an inline provider whose selected content is missing refuses activation. Review refresh errors and recreate a chain explicitly after source name mappings change. Raw effective configuration and explicitly viewed logs can contain provider credentials.

## Keep a subscription chain across updates

```sh
nagi subscription apply work
nagi proxy chain set subscription work --via 'SOCKS Entry' --via 'TS Exit' --dry-run
nagi proxy chain set subscription work --via 'SOCKS Entry' --via 'TS Exit'
nagi proxy chain show subscription work
```

The target must be the actual Nagi subscription associated with the selected profile; providers are a separate scope. The applied source hash must still match that association. Chain setting neither downloads nor applies a subscription. Download-only updates change the cache, not the running configuration.

A subscription policy saves the complete ordered hop definitions independently of ordinary overrides. It applies to eligible effective top-level nodes and providers, including local additions and nodes introduced by explicit or scheduled apply. Chain members are excluded from the target set. Every source collision and protected dependency is checked before saving or activation. A conflicting scheduled update remains pending; it does not partially activate.

Managed chain edges cannot be changed individually. Edit a managed Tailscale node through `proxy node edit` to update its retained definition, or clear the subscription chain before changing individual edges. Clearing the subscription chain restores source and ordinary-override dialers rather than recursively clearing them; the preview reports restored values. Clearing or switching retains former hop definitions as ordinary local additions; later subscription application keeps those additions unless a conflicting source entry prevents application.

```sh
nagi proxy chain clear subscription work
nagi proxy node remove 'TS Exit'
```

Removal fails while effective nodes, groups, providers, DNS settings, or an active policy still depend on the node. Clear or correct references first. Identity state remains available after node deletion. Clearing ordinary overrides leaves the separate subscription policy active; manual profile import detaches it, and profile removal deletes it. The former `proxy tailscale` commands and single-exit policy format are unsupported; there is no automatic migration. See the [CLI reference](../reference/cli.md#proxy-nodes-and-chains) for exact input, output, and compatibility rules.

## Use the terminal menu

Open **Proxies → Node Management** to inspect nodes, add or edit Tailscale, and replace explicit group memberships. Applied subscription nodes appear first in source order, followed by provider entries, then other nodes grouped by type and sorted by name. Open a provider entry to see its nodes in source order; provider details require the running engine. See the [node list reference](../reference/cli.md#proxy-nodes-and-chains) for source classification and ordering rules.

Open **Proxies → Node Chains** to see existing full paths and choose **New chain**. An intermediate prefix is shown only when it is no longer part of another node path; an independent provider-node outlet remains visible even when another path uses it. New drafts start empty. Select nodes in traffic order using their numbers; choices preserve the [CLI source order](../reference/cli.md#order-scope-and-plans), provider choices append their source name, selected nodes use the existing green style, and the header updates the complete path with `Node [Source]` for provider members. Unsupported provider members show their reason and cannot be selected; sources without usable local payloads or authenticated active-backend cache evidence are marked unavailable. The last selected node is the exit. Select a node again to remove it; reselect it to append it. Choose **Reset selection** to clear only the draft, or **Apply** after selecting at least two nodes.

Select an existing path to view it, then choose **Disconnect chain** or Back. To change a path in the menu, disconnect it and choose **New chain**; use ordinary CLI `proxy chain set` for direct changes. Disconnect removes only a top-level exit's incoming edge or the independent provider-node outlet; upstream segments and source nodes remain. Apply and disconnect return to the refreshed list after success. Failures retain the draft or detail for explicit retry. If a selected member becomes unavailable, the complete draft remains visible and Apply is hidden; current available choices and Reset selection remain usable. Plans block conflicts; when other configured paths actually change, **View changed paths** shows their before/after paths. Group references are not listed as changed paths. When a provider member is the exit, successful saving reports its selectable outlet and where to choose it; the menu does not switch the group automatically. Use ordinary CLI commands to configure whole-provider or subscription scopes.

All choices are numeric and long lists are paginated. `0` or Ctrl-C cancels before submission; EOF exits. Failed operations retain entered values and selections for explicit retry. The menu dispatches CLI operations and does not implement configuration or forwarding itself.

## Activation and recovery

Reads and chain previews work while mihomo is stopped. Mutations require the matching mihomo executable for validation. A stopped engine stays stopped; a running selected profile receives one candidate activation. Validation, save, or activation failure restores the whole configuration transaction and attempts runtime recovery. If recovery cannot be confirmed, the error reports uncertainty; inspect `nagi status` and retry `nagi config reload` after the pending activation settles.

Saving or reloading confirms configuration work, not tailnet login, exit approval, or end-to-end connectivity. Reload can interrupt existing Tailscale connections. These operations do not install a helper, enable TUN, or change backend. User and privileged Tailscale identities remain isolated; switching backend may require a reusable or new Auth Key. See [traffic access](traffic-access.md) and the [runtime design](../design/runtime.md#proxy-nodes-chains-and-tailscale-identity).
