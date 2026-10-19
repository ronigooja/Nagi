# Getting started

This guide starts and controls a local mihomo instance with Nagi.

Run `nagi --help` to discover commands, or `nagi help profile import` for a command's syntax and example. See the [CLI reference](../reference/cli.md) for output modes and validation troubleshooting.

1. Build Nagi and the pinned mihomo commit as described in [Build and test](../development/build-and-test.md).
2. Run `./bin/nagi start`. On first use, Nagi creates a `default` profile with a local mixed proxy port at `17890`, DoH resolvers, and disabled TUN DNS interception settings. This profile has no proxy nodes and sends traffic directly; import your own configuration or apply a subscription to use proxy nodes. Run `nagi tun enable` to activate TUN when needed; this requires OS permissions. Starting Nagi does not configure the OS HTTP/HTTPS proxy settings.
3. Run `./bin/nagi status` to check the PID and mihomo version. Run `./bin/nagi logs` if startup fails.
4. Run `./bin/nagi proxy groups` to list configured groups, `./bin/nagi proxy search QUERY` to find nodes, and `./bin/nagi proxy select GROUP NODE` to change a selection. Use `./bin/nagi proxy delays GROUP` to compare node latency. Selections are saved for the selected profile and restored when possible after restarting or reloading.
5. Run `./bin/nagi stop` when finished.

For an installation or startup problem, run `nagi doctor`. It reports checks and next steps without changing files or starting mihomo. A stopped engine can produce warnings; check the report rather than using the command's exit status as a health check. The [CLI reference](../reference/cli.md#diagnostics) defines the diagnostic results.

Nagi stores a profile at the platform specific configuration path. Use `nagi config show` to inspect it and `nagi config validate` to check it. To add or replace a profile with validation and atomic replacement, use `nagi profile import NAME FILE`. Switch with `nagi profile list` and `nagi profile use NAME`. The complete command and path definitions are in the [CLI reference](../reference/cli.md) and [runtime design](../design/runtime.md).

To set local ports, DNS settings, or rules after applying a subscription, put those values in a separate YAML mapping and run `nagi profile override set NAME FILE`. For example, a file containing `mixed-port: 17891` replaces that setting without editing the imported source. Later explicit and automatic subscription applications retain local settings and node/group changes. See the [subscription merge contract](../reference/cli.md#local-profile-files-and-overrides) for conflicts and recovery. Use `nagi profile override show NAME` to inspect the local values and `nagi profile override clear NAME` to remove them. Back up a source profile with `nagi profile backup NAME`, compare it later with `nagi profile diff NAME`, and recover with `nagi profile restore NAME`. `nagi profile export NAME FILE` writes a copy to a new file. These files can contain credentials; keep them private.

Use `nagi --json COMMAND` for scripts and the macOS app. Subscription URLs are stored locally and omitted from list output. `subscription update NAME` validates supported mihomo YAML or mihomo-compatible V2Ray-style link content, caches it, and shows added, removed, and changed nodes. Run `subscription preview NAME` to compare the cache with the named profile before applying. `subscription apply NAME` validates and replaces the named source profile with the downloaded configuration (including converted V2Ray-style links), then activates it. Local runtime settings and node/group changes are rebased against the previous subscription source and retained; saved node choices are cleared. Custom rules stored separately still apply. A refresh alone does not change the active profile. The [CLI reference](../reference/cli.md) defines the merge and conflict behavior.

For example, replace `work` with your chosen name and the example URL with your subscription URL. The URL argument may be exposed through shell history and process arguments; see [Sensitive content](../reference/cli.md#sensitive-content).

```sh
nagi subscription add work 'https://example.com/subscription.yaml'
nagi subscription update work
nagi subscription preview work
nagi subscription apply work
nagi status
```

If the engine is stopped after applying a subscription, run `nagi start` to use the selected profile.

## DNS setup

New default profiles use DNSPod DoH for primary, bootstrap, direct outbound, and proxy-node hostname resolution. They include settings for TUN interception of UDP/TCP port 53, but leave TUN disabled until `nagi tun enable` is run. Check `nagi tun status` and `nagi logs` if TUN cannot start. The default routing rule sends traffic directly; users can set their own routing rules. After importing a profile, set local DoH policy with `nagi dns set direct https://1.12.12.12/dns-query https://120.53.53.53/dns-query`. To send DoH through a proxy, use `nagi dns set proxy NODE URL [URL...]`, replacing `NODE` with a non-direct node name from that profile. If the default DNSPod bootstrap addresses are unreachable on your network, append `--bootstrap https://IP_ADDRESS/dns-query` with an IP-address DoH server reachable directly. Choose primary and bootstrap servers reachable from your network. The local policy survives a later source profile replacement. Run `nagi dns status` to inspect it.

To route a corporate suffix to a local resolver, run `nagi dns exception add corp.example udp://10.0.0.53:53`, replacing the example domain and address. This intentionally sends that suffix's DNS queries without encryption. For a profile without TUN interception, run `nagi dns tun on`; this may require system privileges and working platform routes. `nagi dns check` reports configuration risks and queries mihomo, while `nagi dns query example.com AAAA` tests one record type and `nagi dns flush` clears mihomo's cache. The [DNS reference](../reference/cli.md#dns-policy-and-checks) explains configuration checks and TUN activation limits.

## Daily operation

Use `nagi proxy delay 'Node A'` to check the latency of a proxy, replacing `Node A` with a name from your configuration. Run `nagi proxy restore` to inspect which saved choices remain available. Use `nagi connections list` to see connection IDs and selected proxy chains, then `nagi connections close CONNECTION_ID` to disconnect one or `nagi connections close-all` to disconnect all current connections. Applications can reconnect automatically.

Run `nagi mode` to inspect the routing mode, or `nagi mode rule`, `nagi mode global`, or `nagi mode direct` to change it for the running engine. Restarting or reloading restores the mode from the selected profile.

Use `nagi mode save global` to persist a mode in the selected profile, or `nagi mode saved` to inspect the stored value. Use `nagi config reload` after a validated profile change when mihomo is already running. Follow new log lines with `nagi logs follow`; press Ctrl-C to stop. Inspect one connection with `nagi connections show CONNECTION_ID`, or close it with `nagi connections close CONNECTION_ID`.

To remove an unused profile, select another profile first and run `nagi profile remove NAME`. This deletes its configuration, backups, local overrides, and saved proxy choices. Subscription records and caches are kept; see [profile removal](../reference/cli.md#profile-removal). For command completion, load the script printed by `nagi completion bash`, `nagi completion zsh`, or `nagi completion fish` using the [shell instructions](../reference/cli.md#shell-completion).
