# CLI reference

This document defines the implemented Nagi commands and their JSON interface. Except for the interactive `menu` command, `--json` may appear anywhere in the argument list. Without it, Nagi prints text intended for terminal reading. Scripts and applications should use ordinary commands with `--json` rather than parse the text presentation.

## Help and syntax

Run `nagi`, `nagi help`, `nagi -h`, or `nagi --help` for top-level help. Use `nagi help profile use` or `nagi profile use --help` for a command's syntax, description, and example. `-h` is also accepted for command help. Help topics contain command names, without positional argument values.

Valid help requests exit with status `0`, even if runtime configuration is invalid or mihomo is unavailable. With `--json`, help text is a string in the success envelope's `data` field. Unknown help topics and incorrect argument counts exit with status `2` and show the relevant usage or help syntax. Argument counts and the `logs` integer range are checked before runtime configuration is read.

## Commands

| Command | Result or effect |
| --- | --- |
| `menu` | Open the interactive, line-oriented terminal menu for common CLI operations. Requires terminal input and output. |
| `start`, `stop`, `restart` | Manage the mihomo process and its desired running state. `start` creates the default profile if absent. |
| `quit` | Stop this session's login monitor and mihomo, restore Nagi-managed system proxy settings, and preserve login startup preference. |
| `status` | Report running state, selected profile, PID, paths, and available mihomo version and mixed port. |
| `doctor` | Produce a read-only diagnostic report with checks and suggested next steps. |
| `logs [lines]` | Return the last 100 lines by default, or 1 through 10000 lines. |
| `logs follow` | Stream new log lines until interrupted. |
| `traffic watch` | Stream live transfer rates and cumulative byte counters until interrupted. |
| `config validate` | Check the selected profile with `mihomo -t`. |
| `config show` | Return the selected profile YAML. |
| `config reload` | Validate and safely reload the selected profile in a running mihomo instance. |
| `profile list` | List profile names and the current selection. |
| `profile use NAME` | Validate and activate an existing profile, reloading a running mihomo instance. |
| `profile import NAME FILE` | Validate and atomically write a profile from a local YAML file (maximum 8 MiB). |
| `profile export NAME FILE` | Write source YAML to a new private file; existing destinations are refused. |
| `profile backup NAME` | Copy source YAML to `profiles/NAME.yaml.bak`. |
| `profile restore NAME` | Validate and restore the latest backup; reload if selected and running. |
| `profile diff NAME [OTHER\|backup]` | Show changed source lines, structural YAML paths, and conflict warnings against another profile or the backup (default). |
| `profile override set NAME FILE` | Save a separate local YAML mapping (maximum 8 MiB) and apply it to this profile. |
| `profile override show NAME` | Print the local override YAML. |
| `profile override clear NAME` | Remove the local override and reload if selected and running. |
| `profile override apply-default NAME` | Copy safe local runtime settings from `profiles/default.yaml` onto NAME; nodes, groups, rules, and Nagi-managed fields are excluded. |
| `profile remove NAME` | Delete an unselected regular profile file, retaining any existing `.bak` file. |
| `dns status` | Show selected profile DNS policy, upstream hosts, IPv6, TUN configuration, and warnings. |
| `dns set direct URL [URL...]` | Save one or more HTTPS DoH upstreams for direct connection. |
| `dns set proxy NAME URL [URL...]` | Pin DoH upstreams to a named non-direct proxy node or proxy group in the effective profile. |
| `dns exception add DOMAIN SERVER` | Route a domain suffix to an explicit DoH or plaintext IP DNS server. |
| `dns exception remove DOMAIN` | Remove a local DNS exception. |
| `dns tun on`, `dns tun off` | Save TUN DNS interception settings for the selected profile. |
| `dns query DOMAIN [A\|AAAA]` | Query the running mihomo resolver over its private control socket. |
| `dns flush` | Clear the running mihomo DNS cache. |
| `dns check` | Audit the selected configuration and query mihomo DNS through its control API. |
| `subscription list` | List names, refresh and schedule state, cache/application metadata, and available expiry and traffic metadata without URLs. |
| `subscription watch` | Run the foreground scheduler until interrupted; the installed login service hosts the same scheduler. A duplicate host exits with `subscription_error`. |
| `subscription schedule NAME SECONDS\|off` | Persist an update interval from 1 through 31,536,000 seconds, or disable scheduling with `off`; a running scheduler refreshes when due and applies only the selected associated profile. |
| `subscription add NAME URL` | Save an HTTP or HTTPS subscription URL. |
| `subscription key NAME FILE\|clear` | Copy a local age identity file for a saved subscription, or clear its identity. |
| `subscription update NAME` | Fetch and validate up to 8 MiB, cache it, and report node changes against the preceding cache. |
| `subscription preview NAME` | Compare cached nodes with profile `NAME` without changing either. |
| `subscription apply NAME` | Replace profile `NAME` with the cached mihomo configuration, remove its local override and saved group choices, then activate it. |
| `subscription remove NAME` | Remove a saved subscription, its cache, and its stored identity. |
| `proxy groups`, `proxy show GROUP` | Read mihomo proxy groups, nodes, current selection, and selection availability through its Unix Socket API. |
| `proxy search QUERY` | Find nodes by case-insensitive name substring and list matching groups. |
| `proxy select GROUP NODE` | Select a node in a group and save the choice for the selected profile. |
| `proxy auto GROUP` | Remove a fixed choice from a URLTest or Fallback group and its saved selection, resuming mihomo automatic selection. |
| `proxy providers`, `proxy provider show NAME` | List or inspect proxy providers with redacted metadata and node names. |
| `proxy provider update NAME`, `proxy provider healthcheck NAME` | Ask mihomo to update a proxy provider or start its native healthcheck. |
| `proxy delay NODE [URL] [TIMEOUT_MS]` | Measure a proxy's HTTP request latency through mihomo. |
| `proxy delays GROUP [URL] [TIMEOUT_MS]` | Test all nodes in a group and sort successful measurements by latency. |
| `proxy restore` | Replay saved choices for the selected profile where the group and node still exist; report missing choices. |
| `connections list` | Return a snapshot of active connections, including mihomo's selected proxy chains when supplied. |
| `connections show ID` | Return one active connection by ID, including metadata and proxy chain. |
| `connections close ID` | Request closure of a connection by its ID. |
| `connections close-all` | Request closure of all current connections. |
| `mode [rule\|global\|direct\|save MODE\|saved]` | Read or change temporary runtime mode, or explicitly persist/read the selected profile's mode. |
| `service install`, `service uninstall` | Manage a per-user launchd Agent or systemd service. |
| `privileged-helper status\|install\|uninstall\|enable\|disable` | Inspect or manage the macOS privileged TUN helper and selected engine backend. |
| `version` | Report Nagi version, mihomo version and pinned commit, OS, and architecture. |
| `completion bash\|zsh\|fish` | Print a shell completion script without reading runtime configuration. |

Profile and subscription names consist of ASCII letters, digits, `_`, or `-` and have a maximum length of 128 characters. A profile file is named `profiles/NAME.yaml`. `subscription update` accepts HTTP 200 responses only. Nagi validates a mihomo YAML document with `proxies` or `proxy-providers`, or a Base64/plain V2Ray-style link list supported by mihomo (including SS, SSR, VMess, VLESS, Trojan, Hysteria, TUIC, SOCKS, HTTP, AnyTLS, and Mieru), before replacing the cache. A YAML document without `proxy-groups` gets `Subscription Auto` (`url-test`), `Subscription Fallback`, `Subscription Balance` (`load-balance`), and a `Subscription` manual select group. The manual group defaults to Auto and includes the other strategies, top-level nodes, and `DIRECT`. Generated strategy groups use top-level nodes and all proxy providers; the manual group also includes provider nodes directly. The Auto group tests `https://www.gstatic.com/generate_204` every 300 seconds with zero tolerance. Generated names that conflict with top-level nodes, proxy provider names, or known inline-provider node names receive deterministic numeric suffixes. When a top-level node is named `GLOBAL`, Nagi gives that node the first available `GLOBAL-N` name (starting at `GLOBAL-2`) so it remains selectable and the required `GLOBAL` group can be generated. In this generated-group case, Nagi updates downloaded proxy targets in rules and sub-rules, node and inline-provider node `dialer-proxy` references, proxy-provider download `proxy` and `dialer-proxy` references, provider-override `dialer-proxy` references, rule-provider download `proxy` references, and DNS upstream proxy bindings that refer to that node. `SUB-RULE` targets remain sub-rule names, even when named `GLOBAL`. A generated `GLOBAL` group selects `Subscription`. If `rules` is absent, Nagi adds a `MATCH` rule targeting the generated manual group, the first existing group, or `DIRECT` when an existing group list is empty. Existing groups and rules retain their order and definitions. Unsupported or malformed responses leave the previous cache and refresh metadata intact.

`subscription key NAME FILE` accepts a readable, nonempty regular file of at most 1 MiB and copies it into a mode `0600` file in `config/subscription-keys` (mode `0700`). The source file path and contents are omitted from output. `subscription key NAME clear` removes the copy. A response beginning with the age armor header is passed via standard input to the matching installed mihomo (`NAGI_MIHOMO_BIN` or the sibling executable), using `--decrypt-config - --age-identity-file` with the private copy. The decrypted result is limited to 8 MiB before ordinary subscription parsing. A missing or wrong key, decryption failure, oversized plaintext, or invalid plaintext leaves the previous cache and refresh metadata intact. Plaintext responses do not require a key. Key changes and refresh do not activate a profile.

`subscription update` identifies itself as a Clash Meta/mihomo client and advertises YAML support so servers such as XBoard can return their complete mihomo configuration. It reports added, removed, and changed top-level nodes compared with the preceding cache; `subscription preview` compares the normalized cache with the existing profile. A changed node keeps its name but has a different definition. The preview's node change lists are sorted by name. `changes` reports every changed configuration field, including proxy order and settings such as `sniffer`, with redacted paths, change kinds, and behavior hints. Known field names appear in paths; other field names use numbered `field-N` placeholders. Parent and child paths may both appear for one change. It contains no configuration values or user-defined names. `remote_nodes_compared` is false when either side uses proxy providers because their remote nodes are not loaded for preview. On `subscription apply`, the downloaded configuration replaces the named source profile, even if its old content cannot be parsed. Settings omitted by the download, the named profile's local override, and old group choices are discarded. A failed activation restores the old source, override, selected profile, and saved choices. When the running activation changed, Nagi attempts to reactivate a validated prior effective configuration and confirms its new activation ID. If that confirmation fails, the error states that runtime state is uncertain. Proxy and group order follows the subscription. Nagi removes downloaded external-controller settings, disables LAN access, binds built-in and independent `listeners`, `tunnels`, `tuic-server.listen`, and `dns.listen` to loopback while retaining ports and tunnel targets. File-defined `ss-config` and `vmess-config` inbounds are rejected because their bind addresses cannot be safely rewritten. Unsupported address or protocol forms fail with a field-specific error, and sets `profile.store-selected` to `false` so mihomo does not replay cached choices. If neither an HTTP nor a mixed port is supplied, it adds mixed port `17890`. Custom rules stored separately still apply. An unparseable old profile prevents `subscription preview` but does not prevent `subscription apply`; the apply result lists downloaded nodes as added because a comparison is unavailable.

The optional `Subscription-Userinfo` response header supplies `upload`, `download`, `total` (nonnegative bytes) and `expire` (Unix seconds). Available values appear in `subscription list` and the update result. The URL remains hidden from list output. A missing header clears previously stored traffic and expiry values on a successful update.

## Terminal menu

Run `nagi menu` in a terminal. The home page offers runtime controls, proxy groups, subscriptions, local profiles, traffic access, diagnostics, and advanced functions. Advanced functions contain DNS, rules, connections, logs and traffic, service/startup, and helper operations where supported. Existing commands remain the complete interface for automation and advanced argument variants. See [the terminal menu design](../design/terminal-menu.md) for responsibilities and flows.

At `Choice:`, select a numbered resource or a displayed letter action. Resources display ten per page, with numbering restarting at `1`; fixed actions remain visible on every page. `n` and `p` move between pages, `b` returns to the parent, `r` refreshes, `h` explains the current page, and `q` exits only the menu. `/word` filters resource names without regard to case; `/` clears the filter. Changing the filter returns to page one. Actions and refreshes retain the filter and page, clamping to the last available page if needed. Blank or invalid choices repeat only the input prompt. A failed refresh clears stale resource choices and shows a read error; recovery actions remain accessible where available.

Actions execute once on selection, without typing `yes` or accepting a second confirmation. Effects such as replacing configuration, clearing local overrides or saved selections, stopping mihomo, and closing connections appear beside their entries. Selecting a proxy group opens its nodes; selecting a node switches immediately and stays in that list. Filtered nodes and latency results use the same selection behavior. Latency results retain their order and measurements across selections; refreshed nodes that no longer exist lose their measurements. Automatic groups show automatic or fixed state; group latency tests can release a fixed selection.

Text fields accept literal `b` and `q`. Ctrl-C cancels the current form and returns to its owning page; at a menu choice it returns to the parent, or exits on the home page. Required or invalid fields are prompted again without discarding other fields. After a failed form submission, values remain available: select a field number to edit and `s` to submit again, or Ctrl-C to cancel. Failed mutations are never retried automatically. End of input exits the menu. An already submitted ordinary operation can finish; Ctrl-C does not promise cancellation or rollback of that operation.

Adding a subscription opens its detail page. Downloading updates its cache and stays in the detail page; applying is a separate choice and does not automatically start mihomo or enable traffic access. Details show available update, quota, and expiry metadata and counts of cache changes against the target profile. Download results describe changes against the previous cache, a different comparison. Complete change names are available on demand in paginated pages. Apply remains available when preview fails, so a valid cache can repair a damaged target profile; the existing apply command still validates the cache and controls activation. Successful removal returns to the parent list; failed removal stays in detail.

The menu presents command results as readable text and keeps configuration and logs as raw text. URL-shaped text in errors, lists, and context appears as `[redacted URL]`. Results report only completed operations; failures preserve the underlying command's known state and recovery advice. A failed action does not become the menu process's exit status.

Ctrl-C during `logs follow` or `traffic watch` cancels the child stream and returns to its page while the user-mode mihomo process keeps running. Leaving with `q`, EOF, or Ctrl-C at the home page exits `0` and does not stop mihomo. The runtime page has a separate action to stop the login monitor and mihomo. An unrecoverable menu input error exits `1`.

Both standard input and standard output must be terminals. `nagi menu` with redirected input or output, or with `--json`, writes a `usage` error to stderr and exits `2`. `nagi menu --help` is available without a terminal and exits `0`. There is no JSON menu session; automation should invoke ordinary commands with `--json`.

## Diagnostics

`doctor` inspects directory availability and owner read/search permission bits, the mihomo executable (including `NAGI_MIHOMO_BIN` resolved through `PATH`), selected-profile readability, PID liveness, and the Unix Socket `/version` endpoint. It does not create directories, change files, start the engine, validate YAML with mihomo, or test proxy connectivity. A live PID does not establish process identity. The API check has a two-second deadline.

The result is `{"healthy":true,"checks":[{"name":"...","status":"ok","message":"..."}]}` inside the success envelope. `status` is `ok`, `warning`, or `error`; `healthy` is false if any check has status `error`. Check names are `config_directory`, `data_directory`, `state_directory`, `runtime_directory`, `mihomo_executable`, `selected_profile`, `process`, and `control_api`. Missing first-use directories, a missing default profile, and a stopped engine can be warnings. A missing executable, invalid selection, missing selected non-default profile, or stale PID is an error.

A completed diagnostic report exits with status `0`, even when `healthy` is false. Scripts must inspect `data.healthy` and the individual checks. Failure to construct the report uses the normal error envelope and exit status `1`.

## Profile removal

`profile remove NAME` refuses to delete the selected profile, a nonregular file, or any profile when the current selection cannot be read. Select another profile first with `profile use NAME`. Missing profiles fail with `profile_error`. Removal does not delete a subscription with the same name or its cache. Existing `NAME.yaml.bak` files remain available for manual recovery; they are not listed as profiles.

## Local profile files and overrides

`profile export` reads the source profile, including any secrets it contains. It creates `FILE` with mode `0600` and never overwrites an existing path. `profile backup` explicitly refreshes the single `.bak` recovery copy. `profile restore` validates that copy before replacing the source profile; a successful restore swaps the source and backup contents. If validation or a running engine reload fails, the source and original backup remain available. `profile diff` compares source YAML lines and parses both files for structural changes. JSON retains `name`, `other`, `changed` (boolean), and `diff` (string), and adds `changes` and `warnings`. `changes` contains YAML pointer paths with `added`, `removed`, `modified`, `type_change`, or `order_changed` kinds, plus applicable `before` and `after` values. Proxies and groups with unique names use their escaped names in paths; their order is reported separately. Warnings flag rule changes, DNS policy changes, removed proxies or groups, YAML type changes, and exposure setting changes. If either file cannot be parsed as YAML or a structural value cannot be represented in JSON, the line diff remains available, `changes` is empty, and `warnings` explains the failure. The line diff and structural values can contain credentials; do not publish their output without review.

Overrides live in `config/overrides/NAME.yaml`, separate from `config/profiles/NAME.yaml` and subscription cache. The override must be a YAML mapping; its keys replace corresponding source keys, and nested mappings merge recursively except DNS `nameserver-policy` and `proxy-server-nameserver-policy`, which replace their source maps. Lists, including `rules` and DNS server lists, are replaced as a whole. `profile override set` validates the merged configuration with mihomo and reloads it when selected and running. The merged file is generated as `config/profiles/.effective-NAME.yaml` for mihomo, keeping relative file paths anchored in the profiles directory; `profile export`, `config show`, and backups continue to use the source YAML. `profile override clear` restores the source configuration. Removing a profile retains its override file, so clear it explicitly before reusing the name if needed. Overrides can contain credentials and are stored with private permissions.

`profile override apply-default NAME` extracts safe local runtime settings from `config/profiles/default.yaml` and replaces `NAME`'s local override with them. It excludes proxies, proxy providers, proxy groups, rules, rule providers, controller settings, and credentials. The default profile itself is unchanged, and subscription nodes remain in the target source. It validates the resulting effective configuration, reloads when `NAME` is selected and mihomo is running, and restores the previous target override if validation or reload fails.

## DNS policy and checks

A DNS leak occurs when a domain lookup is sent outside the resolver or network path intended by the user, exposing the queried domain to an unintended party.

New default profiles enable mihomo DNS, IPv6, and fake IP mode. They listen only on `127.0.0.1:1053`. The direct outbound resolver and bootstrap resolver use Ali DoH at `https://223.5.5.5/dns-query` and `https://223.6.6.6/dns-query`. The primary resolver uses Cloudflare and Google DoH at `https://1.1.1.1/dns-query` and `https://8.8.8.8/dns-query`. Proxy-node hostname resolution uses Ali DoH. All configured DNS upstreams use HTTPS; no plaintext fallback is configured. The default routing rule is `MATCH,DIRECT`; users manage subsequent routing rules. `direct-nameserver` resolves hostnames for direct outbound connections, while DNS questions handled by the primary resolver use Cloudflare or Google. The installer replaces `default.yaml` on install and update; ordinary DNS inspection does not rewrite profiles.

New default profiles include TUN settings for `auto-route`, `strict-route`, `auto-detect-interface`, and UDP/TCP port 53 hijack (`any:53` and `tcp://any:53`), but set `tun.enable` to `false`. Run `nagi tun enable` to activate TUN; this requires OS TUN permissions and working routes. If mihomo cannot activate TUN, inspect `nagi logs` and `nagi tun status`; configuration validation alone does not establish DNS interception. `system-proxy enable` changes only the OS HTTP/HTTPS proxy settings and does not replace TUN DNS interception.

`dns set` writes DNS settings into the selected profile's separate local override, preserving them across source imports. Applying a subscription removes that override, so repeat `dns set` afterward if needed. URLs must use HTTPS, include a path, and contain no credentials, query, or fragment. Multiple URLs are given to mihomo as primary upstreams. If all are unavailable, resolution fails; Nagi does not configure a plaintext or system DNS fallback. Direct mode connects to the DoH servers directly. Proxy mode appends mihomo's `#NAME` binding to each upstream and requires `NAME` to identify a non-direct top-level proxy node or a proxy group in the effective profile. Automatic groups are accepted. If that target disappears, activation fails rather than silently using a network interface as fallback. Provider-only nodes are not accepted for this binding unless reached through a configured group. Bootstrap and proxy-node hostname resolution still use the two fixed direct DoH servers, even in proxy mode. The DNS override enables both root and DNS IPv6 settings, clears source fallback and direct nameservers, and replaces source DNS server policies.

`dns exception add` requires a prior `dns set` for that profile. `DOMAIN` is an ASCII domain suffix such as `lan` or `corp.example`; Nagi stores it as `+.lan` or `+.corp.example` in mihomo's `nameserver-policy`. `SERVER` can be an HTTPS DoH URL or an explicit `udp://IP:53` or `tcp://IP:53` server. A plaintext exception sends matching DNS questions to that server without encryption; `dns status` and `dns check` report it. `dns exception remove` removes that local mapping. Local DNS policies replace source DNS policies as a whole to avoid retaining an unnoticed plaintext source resolver.

`dns tun on` configures mihomo TUN with `auto-route`, `strict-route`, interface auto-detection, and UDP/TCP port 53 hijack (`any:53` and `tcp://any:53`). It requires an encrypted primary and bootstrap DNS configuration. The engine and OS may require additional privileges or platform support to activate TUN; configuration validation alone does not establish that traffic is intercepted. `dns tun off` disables the selected profile's TUN override. These commands do not change the operating system's DNS server setting. On a platform or network where TUN cannot be activated, applications outside mihomo can still resolve through the system resolver.

`dns query` supports `A` and `AAAA` only and returns mihomo's `/dns/query` response. `dns flush` calls `/cache/dns/flush`. Both require a running control API. `dns check` inspects the selected effective configuration and makes one `example.com` A query through mihomo. Its scope is configuration and mihomo API state; it does not inspect operating-system routes, resolver capture, or external DNS leaks. `dns status` is configuration inspection and does not prove running TUN state. Neither command prints full DoH URL paths or credentials; inspect `profile override show NAME` for exact local settings.

## Proxy latency, connections, and mode

`proxy delay NODE [URL] [TIMEOUT_MS]` defaults to `https://www.gstatic.com/generate_204` and `5000` milliseconds. Supply a URL before a custom timeout. The URL must be absolute HTTP or HTTPS without user information; the timeout must be an integer from `1` through `30000`. Invalid arguments fail before contacting the engine. The CLI allows up to 31 seconds for the control request, so the maximum measurement timeout is not cut short by the ordinary ten-second API timeout. `NODE` is a mihomo proxy name, including built-ins such as `DIRECT`; names with spaces must be quoted. The pinned mihomo implementation sends an HTTP HEAD request and records the latency in the proxy's delay history. This measures request latency, not bandwidth.

`proxy delays GROUP [URL] [TIMEOUT_MS]` uses the same URL and timeout rules. It asks mihomo to test the group and returns one result for each listed entry when the native test succeeds. Successful measurements sort by ascending `delay_ms`, then name; failed measurements follow with `error: "latency test failed"`. If every entry fails, mihomo returns an error. `proxy search QUERY` matches node names without case sensitivity and returns groups containing matches. `proxy groups` and `proxy search QUERY` follow the selected effective profile's `proxy-groups` order; mihomo-only groups absent from that profile follow in name order. If the selected profile cannot be read, groups fall back to name order. Each group's node list keeps mihomo's order. `proxy groups` and `proxy show GROUP` add `selected_status`: `available`, `unavailable` (mihomo reports the group not alive), `removed` (the selected name is absent), or `none`. An explicitly pinned URLTest or Fallback group also has `fixed`; its `now` may otherwise be an automatic result. Selecting a missing node fails with `proxy_selection_error` and suggests `proxy show`.

`proxy auto` requires a URLTest or Fallback group. It calls mihomo `DELETE /proxies/:name` and removes the saved choice for that group. If removing the saved choice fails, exit status is `1` with `proxy_selection_error` and the message states that the running engine already resumed automatic selection. `proxy delays` uses native `GET /group/:name/delay`; mihomo clears a fixed choice for URLTest or Fallback groups at the start of that handler, before measuring latency. Nagi removes its saved choice when the request succeeds, when mihomo reports a measurement timeout after entering the handler, or when a follow-up group read confirms a fixed-to-automatic transition. A failure before dispatch leaves the saved choice intact. If transport fails and the reset cannot be confirmed, Nagi keeps the saved choice and reports the uncertain state with a `proxy show` recovery step; replaying that choice may pin the group again. A failed saved-choice removal reports that the running engine reset but persistence failed. Provider operations return `proxy_provider_error` and exit status `1` on missing providers or failed updates/checks; native error details are suppressed because they can include provider URLs.

Successful `proxy select` requests save the group and node under the selected profile in `proxy-selections.json` in the configuration directory, with mode `0600`. If saving fails, the error states that the live engine was already changed. On start, restart, and profile reload, Nagi attempts to replay saved choices that still exist. Restore failure does not undo a successful start or reload. Run `proxy restore` to see accepted choices and choices whose group or node is unavailable or removed. Nagi does not delete unavailable saved choices automatically; a later configuration may restore them.

```sh
nagi proxy delay 'Node A'
nagi proxy search Hong
nagi proxy delays 'Proxy Group'
nagi proxy restore
nagi proxy delay 'Node A' https://example.com/ 8000
nagi connections list
nagi connections close CONNECTION_ID
nagi mode global
nagi mode
```

Replace `Node A`, `Proxy Group`, and `CONNECTION_ID` with names and IDs from your engine. Connection lists show IDs and any selected proxy chain in text and JSON. Closing an ID that has already disappeared succeeds because mihomo treats it as an idempotent request. `close-all` is explicit, noninteractive, and affects all connections present when mihomo processes the request. Applications may immediately establish new connections. No closed-connection count is returned.

`mode` without an argument reads the running mode. With `rule`, `global`, or `direct`, it changes only the running engine through `PATCH /configs` and returns `persistent: false`; restarting or reloading a profile reapplies the mode in that configuration. `mode save MODE` edits the selected profile's local override when one exists, otherwise its source profile, using the atomic replacement and reload path; it returns `persistent: true`. `mode saved` reads the effective stored mode without contacting mihomo and returns `persistent: true`. A persistent save while the engine is running reloads the profile; a failed reload rolls the modified file back. All temporary mode changes require a reachable mihomo Unix Socket API.

`config reload` validates the selected profile's effective configuration first, requires a running engine, and sends mihomo `PUT /configs?force=true` with the source or generated effective profile path. Validation failure leaves the running engine untouched. The API request supplies a unique `activation_id`; success requires the same ID from `GET /configs`. If a PUT times out, Nagi checks again with a fresh bounded context. If activation changed on failure, it reactivates the validated prior effective configuration and confirms the restoration ID. A timed-out original PUT whose ID was never observed may still finish after that confirmation, so failure output reports runtime recovery as incomplete, includes the currently observed activation ID when available, and advises checking `nagi status` and retrying `nagi config reload` after the pending request settles. Invalid prior content or an unreachable API leaves restoration unconfirmed and is reported as such. A successful reload returns `reloaded: true` and the profile name. Validation uses mihomo `-t --check-json`; field-level validation errors identify the failing path when mihomo provides it without exposing configuration values.

`connections show ID` finds one connection in a fresh `/connections` snapshot and reports the same `id`, `metadata`, `chains`, `upload`, and `download` fields as a list item. A missing ID fails with a suggestion to list current IDs. `connections list` remains a snapshot. `connections close ID` is idempotent when mihomo has already removed the connection; `connections close-all` affects connections present when mihomo processes the request. A closed connection can be recreated by applications.

`logs follow` starts at the current end of the log and streams newly appended complete lines until interrupted. It does not replay existing lines. Text mode prints each new line; `--json` emits one success envelope per line with `data.line`. Terminal users should press Ctrl-C. Log file errors are reported without exposing configuration values.

`traffic watch` connects to mihomo's private Unix Socket `GET /traffic` stream and prints each sample as it arrives, approximately once per second. It requires a running mihomo control API. Text mode labels rates in bytes per second and cumulative totals in bytes. `--json` emits one newline-delimited success envelope per sample with `data.upload_bps`, `data.download_bps`, `data.upload_total`, and `data.download_total` as nonnegative integer byte counts, plus `data.sampled_at` as an RFC 3339 UTC timestamp. The rates are mihomo's current rates; the totals are mihomo's counters since its current start. Ctrl-C or SIGTERM ends the command successfully. A failed connection, malformed sample, or unexpected stream end writes a failure envelope to stderr in JSON mode and exits `1` with `mihomo_api_error`; a failed output write uses `output_error`. The stream is not subject to the ordinary control request's whole-request timeout.

## Shell completion

`completion bash`, `completion zsh`, and `completion fish` print scripts for the selected shell. Text mode prints the script directly; `--json` returns it as the envelope's string `data`. Generation does not require mihomo or a valid selected profile. Completions cover command names, subcommands, global flags, supported mode/shell and DNS policy values, DNS query record types, and local files for profile import, export, and override set. At completion time, proxy group, node, and provider names are queried from the running engine; an unreachable engine yields no dynamic proxy candidates. Profile and subscription names are queried from local stores without exposing URLs. `subscription key` completes subscription names and then local files or `clear`. `completion candidates groups|nodes [GROUP]|providers|profile|subscription` returns newline-separated text or a JSON string array; connection IDs are not completed.

Load the script in the corresponding shell:

```sh
# Bash
source <(nagi completion bash)

# Zsh (initialize completion first)
autoload -Uz compinit
compinit
source <(nagi completion zsh)

# Fish
nagi completion fish | source
```

## Text output

Status output labels the running state, selected profile, and available process details. Profile lists mark the current selection; proxy output identifies the current node. Empty lists have an explanatory message. `logs` prints individual log lines, and `config show` prints YAML directly.

Subscription feedback distinguishes saving a URL from downloading content and applying a profile. Downloading does not change the selected profile. It prints node changes and the apply command. Preview prints node changes without applying. Applying selects the profile and reloads a running engine; it does not start a stopped engine. Profile import validates and writes a file; use `profile use NAME` to select it. Replacing the currently selected profile also reloads a running engine; importing another profile does not reload it. Valid replacement content can repair an invalid existing profile.

## Recovery and service failures

An invalid selected-profile record does not prevent `stop`, `quit`, `logs`, `version`, service management, subscription listing/downloading/removal, or proxy and connection operations. `status` still reports process state, omitting `profile` and adding `profile_error` when the selection cannot be read. `profile list` still lists files, omitting `current` and adding `current_error`. Text output warns that the selection is unavailable. Use `profile use NAME` with an existing valid profile or `subscription apply NAME` with valid cached content to repair an invalid selection. Import and removal require a readable selection so Nagi can determine whether their target is active.

If saving a subscription refresh time fails, Nagi attempts to restore the previous cache, or remove a newly created cache. Restoration failures are also returned. A failed refresh does not activate a profile.

If service installation fails after writing its definition, Nagi attempts to restore the old definition or remove the new one. The error reports the file outcome; service manager state can still have changed and must be checked before retrying. Uninstallation retains the definition if stopping/disabling the service fails. If the final systemd reload fails after removing the definition, the error explains that removal already completed and asks for a reload. An absent definition makes uninstallation a no-op; this does not verify that the service manager has no loaded service.

## Configuration validation failures

Nagi omits mihomo validation stdout and stderr because they may contain configuration values. Errors provide a next step instead: inspect the selected configuration for `config validate` or startup failures, and inspect the source YAML being imported/applied or the profile being selected for profile operations. Missing executable errors suggest checking `NAGI_MIHOMO_BIN`.

To inspect mihomo's own diagnostics locally, use `mihomo -t -f FILE -d DIRECTORY`, replacing `FILE` with the configuration file and `DIRECTORY` with its configuration directory. Use the executable selected by `NAGI_MIHOMO_BIN` when applicable. Those diagnostics may contain private configuration values. Nagi does not expose its temporary validation files as a recovery path.

## JSON and exit codes

JSON success output has the shape `{"ok":true,"data":{...}}`. JSON failure output is written to stderr as `{"ok":false,"error":{"code":"...","message":"..."}}`. Successful commands exit with status `0`; usage errors exit with `2`; all other errors exit with `1`. Error codes include `usage`, `already_running`, `not_running`, `lifecycle_busy`, `not_found`, `invalid_config`, `profile_error`, `subscription_error`, `dns_error`, `rule_error`, `proxy_selection_error`, `proxy_provider_error`, `system_proxy_error`, `tun_error`, `diagnostic_error`, `mihomo_api_error`, and `internal_error`. These codes and field names are the CLI integration contract for the macOS app. Error messages are intended for people and may change; applications should branch on `error.code`. URL-shaped text in errors is replaced with `[redacted URL]` before text or JSON output.

Additional success payloads are defined below; all appear in `data`:

| Command | Fields |
| --- | --- |
| `doctor` | `healthy` (boolean), `checks` (array of `name`, `status`, and `message` strings), as defined above. |
| `profile export NAME FILE` | `name`, `file` (strings), `exported: true`. |
| `profile backup NAME` | `name` (string), `backed_up: true`. |
| `profile restore NAME` | `name` (string), `restored: true`. |
| `profile diff NAME [OTHER\|backup]` | `name`, `other`, `diff` (strings), `changed` (boolean), `changes` (array of `path`, `kind`, optional `before` and `after`), `warnings` (string array). |
| `profile override set NAME FILE` | `name` (string), `override_saved: true`. |
| `profile override show NAME` | `name`, `yaml` (strings). |
| `profile override clear NAME` | `name` (string), `override_cleared: true`. |
| `profile override apply-default NAME` | `name` (string), `default_override_applied: true`. |
| `profile remove NAME` | `name` (string), `removed: true`, `kind: "profile"`. |
| `dns status` | `profile`, `policy` (strings), `enabled`, `ipv6`, `tun_enabled` (booleans), `upstream_hosts`, `issues` (string arrays). |
| `dns check` | DNS status fields plus `scope` (string) and `engine_query_ok` (boolean). Scope is `configuration and mihomo API`. |
| `dns set` | `profile`, `policy`, `proxy_node` (strings), `upstream_count` (integer), `saved: true`. |
| `dns exception add/remove` | `profile`, `domain` (strings), `exception_changed: true`. |
| `dns tun on/off` | `profile` (string), `tun_enabled` (boolean), `saved: true`. |
| `dns query` | `domain`, `type` (strings), `response` (mihomo JSON object). |
| `dns flush` | `flushed: true`. |
| `subscription list` | `subscriptions` array: `name`, `key_configured` (boolean), and `schedule_seconds` (integer; `0` means off); optional `next_due`, `updated_at`, and `expires_at` (RFC 3339), `last_failure_stage` and `last_failure_reason` (fixed redacted status codes), `pause_reason` (`profile_changed` or `source_unreadable` while automatic apply is paused), `cache_version`, `applied_cache_version`, `source_hash` (SHA-256 hex), and `profile`; optional nonzero `upload`, `download`, and `total` (bytes). The top-level payload also includes `scheduler_running` (boolean based on the live scheduler host lock and fresh heartbeat). URLs, identities, and identity paths are omitted. |
| `subscription schedule NAME SECONDS\|off` | `name`, `schedule_seconds` (integer; `0` means off), and `scheduler_running` (boolean, based on the live host lock and fresh heartbeat). |
| `subscription key NAME FILE\|clear` | `name` and `key_configured` (boolean). |
| `subscription update NAME` | `name`, `bytes` (cached plaintext bytes, including after decryption), `updated_at`, `preview`, and optional `expires_at`, `upload`, `download`, `total`. |
| `subscription preview NAME` | `name`, `format`, sorted `added`, `removed`, and `changed` top-level node-name arrays; `changes` array of redacted `path`, `kind` (`added`, `removed`, or `changed`), and `behavior`; `remote_nodes_compared` boolean. |
| `subscription apply NAME` | `name`, `profile`, `applied: true`, `preview`, `selections_restored` (always `0`; retained for JSON compatibility). |
| `proxy delay` | `proxy` and `url` (strings), `timeout_ms` and `delay_ms` (integer milliseconds). Provider nodes use mihomo’s provider-node healthcheck endpoint. |
| `proxy auto` | `group` (string), `automatic: true`, `saved_selection_removed: true`. |
| `proxy providers` | `providers` array of redacted `name`, `type`, `vehicle_type`, optional `updated_at`, and `nodes` names. URLs, node definitions, and credentials are excluded. |
| `proxy provider show` | One redacted provider object with the same fields. |
| `proxy provider update` | `provider` and `updated: true`. |
| `proxy provider healthcheck` | `provider` and `healthcheck_started: true`. |
| `proxy delays` | `group`, `url`, `timeout_ms`, `results` array of `proxy`, optional `delay_ms` or `error`. Uses mihomo’s native group test. For URLTest and Fallback, a confirmed handler run clears the fixed node and Nagi removes the saved choice. The `url` field records the requested URL. In the pinned mihomo version, URLTest uses its configured test URL despite the request’s `url` parameter; other group types use the supplied or default URL. Successful automatic-group tests add `automatic_reset: true` and `saved_selection_removed: true`. |
| `proxy groups`, `proxy show`, `proxy search` | Group objects include `name`, `type`, `now`, `fixed` for explicitly pinned automatic groups, `all`, `alive`, `selected_status`, and additive native `hidden` and `icon` metadata. Nagi menus omit hidden groups; URLs and credentials are never included. |
| `proxy restore` | `restored` and `unavailable` string arrays of `GROUP -> NODE`. |
| `connections close ID` | `id` (string), `closed: true` (request accepted, including an already absent connection). |
| `connections close-all` | `closed: true` (request accepted; no count). |
| `connections show ID` | One connection object with `id`, `metadata`, `chains`, `upload`, and `download`. |
| `mode` | `mode` (string: `rule`, `global`, or `direct`), `persistent: false`. |
| `mode MODE` | `mode` (string), `changed: true`, `persistent: false` (temporary update accepted). |
| `mode save MODE` | `mode`, `changed: true`, `persistent: true` (profile update and reload accepted). |
| `mode saved` | `mode`, `persistent: true` (stored profile mode). |
| `config reload` | `profile`, `reloaded: true`. |
| `logs follow` | A stream of success envelopes, each with `line` (string), when `--json` is used. |
| `traffic watch` | A stream of success envelopes, each with `upload_bps`, `download_bps`, `upload_total`, `download_total` (nonnegative integers), and `sampled_at` (RFC 3339 UTC string), when `--json` is used. |
| `completion SHELL` | A string containing the completion script. |
| `completion candidates RESOURCE [GROUP]` | An array of local profile or subscription names, or live proxy group, node, or provider names. `GROUP` applies only to nodes. |

`NAGI_MIHOMO_BIN` overrides the default mihomo executable path, which is a file named `mihomo` beside the Nagi executable. Nagi uses XDG directories on Linux and `~/Library/Application Support/Nagi` on macOS; see the [runtime design](../design/runtime.md). The local artifact installer writes the built-in default profile on both install and update; see the [installation workflow](../development/install.md).

## Rules

| Command | Result or effect |
| --- | --- |
| `rules list` | Read mihomo's active ordered rules, including index, type, payload, target, and wrapper statistics when provided. |
| `rules providers` | Read active mihomo rule providers. |
| `rules custom` | List Nagi-owned persistent rules and imported sets. |
| `rules conflicts` | Show effective rule order and duplicate or shadowed matchers. |
| `rules connection ID` | Show the rule and payload mihomo recorded for an active connection. |
| `rules add NAME TYPE PAYLOAD TARGET` | Add a validated custom rule. Supported types include `DOMAIN`, `DOMAIN-SUFFIX`, `DOMAIN-KEYWORD`, `DOMAIN-REGEX`, `IP-CIDR`, `IP-CIDR6`, `GEOIP`, `GEOSITE`, `PROCESS-NAME`, and `MATCH`; use `-` as MATCH payload. |
| `rules remove NAME` | Remove a custom rule or imported set. |
| `rules enable NAME`, `rules disable NAME` | Enable or disable a custom rule or set while preserving its order. |
| `rules import-local NAME BEHAVIOR FILE TARGET` | Import a local YAML payload/list, or text domain/IP-CIDR set, as an inline provider. |
| `rules import-remote NAME BEHAVIOR URL TARGET` | Download an HTTP(S) rule set (maximum 8 MiB) and store a snapshot as an inline provider. |

Targets are `DIRECT`, `REJECT`, or a mihomo proxy group name. Custom rules are stored in `rules.yaml` under Nagi's config directory and are prepended to profile rules. Imported sets use `nagi-NAME` inline providers and a generated `RULE-SET` rule. They remain separate from profile and subscription files, so applying or refreshing a subscription preserves them. Every mutation validates the effective YAML; when mihomo is running it reloads the effective profile, and failed validation or reload restores the previous rules file.

`rules conflicts` reports duplicate matchers and rules appearing after an earlier `MATCH`, which cannot be reached. This is a static report and does not prove semantic overlap between arbitrary regular expressions, geolocation databases, or provider contents. `rules connection ID` only works while the connection remains active; mihomo supplies the recorded `rule` and `rulePayload` fields.

JSON payloads contain `rules`, `providers`, `entries`, `order`/`conflicts`, or `id`/`rule`/`rule_payload` as appropriate. Errors use `rule_error` for validation, persistence, download, and reload failures, with the standard envelope and exit codes.

## Traffic access

| Command | Result or effect |
| --- | --- |
| `traffic watch` | Stream live transfer rates and cumulative byte counters from mihomo once per second until interrupted. |
| `system-proxy status` | On macOS, read active OS HTTP, HTTPS, and SOCKS proxy settings. |
| `system-proxy enable` | On macOS, save current OS settings and point HTTP and HTTPS proxy settings at mihomo's local mixed port (or HTTP port). Requires a running engine. |
| `system-proxy disable` | On macOS, restore settings saved by Nagi; when no restore record exists, report the current settings without changing them. |
| `tun status`, `tun enable`, `tun disable` | Read or change mihomo TUN at runtime. Enable requests automatic route and interface detection; status also inspects a named adapter when available. |
| `ports status` | Show HTTP/HTTPS, SOCKS, and mixed listener ports, bind address, and LAN flag. Port `0` means the listener is disabled. HTTPS here means an HTTP CONNECT proxy, not TLS termination. |
| `lan status`, `lan enable [ADDRESS]`, `lan disable` | Read or change runtime LAN access and bind address. `lan enable` defaults to all interfaces (`*`); ADDRESS must be an IPv4 or IPv6 address. Disable binds to `127.0.0.1`. |

`system-proxy` uses `networksetup` for active macOS network services. On Linux, all `system-proxy` subcommands return `system_proxy_error` with exit status `1`; configure individual applications to use the listener shown by `nagi ports status`. Old Linux GNOME restore records are ignored by lifecycle commands; proxy settings left by an older Nagi version must be reset in the desktop settings. On macOS, saved settings are held in a mode `0600` restore record under Nagi's state directory. A successful configuration apply updates managed HTTP/HTTPS ports to the newly active mixed or HTTP listener while retaining the original restore baseline. A service changed externally is reported as a conflict and is not overwritten. `stop` and `restart` restore them before stopping mihomo. A detached watcher restores them after an unexpected engine exit, and `status` retries restoration when it finds the engine stopped. If restoration fails, the record remains for `system-proxy disable` to retry. These commands cannot restore settings after the user removes the restore record or if the watcher and Nagi are both unable to run. `startup check`, including checks made by the installed login service, compares managed settings with both the saved baseline and Nagi's target. When an individual known macOS service's settings exactly revert to the baseline, it reapplies Nagi's target. A different value is reported as a conflict and left untouched. New macOS services are reported as untracked; removed services are reported as missing. Disable and re-enable the system proxy to include a newly active service. Existing restore records without a saved target are reported as `legacy_journal` and are not automatically reasserted. A user deliberately restoring the exact original settings cannot be distinguished from an OS reset and may be reasserted while the login service is active.

`tun enable` needs OS permission to create a TUN device and install routes. macOS and Linux permission mechanisms differ; inspect `nagi logs` if the adapter does not start. The mihomo API may accept a TUN patch while logging an adapter creation error, so a reported `enabled` setting alone does not prove that interception works. `tun status|enable|disable` also reports `adapter_status`: `inactive` when TUN is disabled, `unknown` when no device name is exposed by mihomo or OS interface enumeration fails, and `missing`, `down`, or `up` for a configured `tun.device` name. `adapter_name` is present only when mihomo exposes that name. An up adapter does not prove route ownership or DNS capture. Runtime TUN and LAN changes are reset when a profile is reloaded or mihomo restarts. Enabling LAN exposes configured proxy listeners at the selected address; use firewall rules and authentication appropriate to that network.

On macOS, an installed privileged helper can run mihomo with permission to create the TUN adapter. Install it from a root-owned `/usr/local/bin/nagi` using `sudo /usr/local/bin/nagi privileged-helper install`. Stop the user engine, run `nagi privileged-helper enable`, then `nagi start`. `privileged-helper status` reports `service` (`path`, `manager`, `installed`, `active`, `executable`), `selected_backend` (`user` or `privileged`), and `available`. `enable` and `disable` return `selected_backend` and `changed`. Switching is refused while the outgoing backend's mihomo process is running. The selected backend is persisted in the private `engine-backend` file in Nagi's configuration directory; an absent file means `user`. The app continues to use the same CLI commands. In privileged mode, lifecycle, logs, traffic samples, and control operations use the authenticated helper socket; mihomo's root-only controller is not exposed to the user. Privileged starts and reloads accept inline proxies and providers, HTTP providers, and file providers or proxy TLS files located under the session user's Nagi configuration directory. Relative file references resolve beside the source profile. The helper copies those files into root-private snapshots, runs mihomo restricted-policy validation, and rejects unsupported fields and unsafe paths before activation. A reload requires a 32-character lowercase hexadecimal `activation_id`; if its request times out, activation state may remain uncertain until queried through `/configs`. The helper retains snapshots until the child exits. A matching restricted-policy mihomo executable is required.

On macOS, JSON payloads for `system-proxy status|enable|disable` include `enabled` (boolean), `backend`, and `http`, `https`, and `socks` objects with `enabled`, optional `host`, and optional `port`. `tun status|enable|disable` include `enabled` (boolean), `settings` (mihomo TUN object), `adapter_status` (string), and optional `adapter_name` (string). `lan status|enable|disable` include `enabled` and `bind_address`. `ports status` includes integer `http`, `https`, `socks`, and `mixed`, plus `bind_address` and `allow_lan`. The `http` and `https` values are the same because mihomo uses one HTTP proxy listener for both schemes. All use the usual success/error envelope and exit codes. `system_proxy_error` and `tun_error` indicate failed traffic operations.

## Startup and background operation

| Command | Result or effect |
| --- | --- |
| `service install` | Install and enable the per-user launchd/systemd service. |
| `service uninstall` | Disable, stop, and remove the service definition. If `quit` already unloaded the launchd job, remove its remaining definition. |
| `service status` | Report definition path, manager, installed, enabled, and active state. |
| `privileged-helper status` | Report system helper installation, activity, and selected engine backend. |
| `privileged-helper install` | Install the system helper; requires administrator privileges and the root-owned installed executable. |
| `privileged-helper uninstall` | Stop and remove the system helper; requires administrator privileges. Stop privileged mihomo and select the user backend first. |
| `privileged-helper enable` | Select the privileged backend for subsequent starts; requires an active helper and a stopped user mihomo process. |
| `privileged-helper disable` | Select the user backend for subsequent starts; requires privileged mihomo to be stopped. |
| `startup status` | Report service status together with mihomo process state. |
| `startup enable` | Enable an installed login service without rewriting its definition. |
| `startup disable` | Disable the login service while retaining its definition. |
| `startup check` | Recheck PID/socket state and `/version`, and reassert managed system proxy settings only when they reverted to the saved baseline. Remove stale runtime markers after an unexpected exit. It never silently starts mihomo. |
| `quit` | Stop this session's monitor, restore Nagi-managed system proxy settings, and stop mihomo while preserving login startup preference. Returns `quit: true`. |
| `recover` | Remove stale PID and socket markers after an unexpected exit. A live process is never touched. |

`start` is duplicate-safe: a live PID returns `already_running` while recording the desired running state. A stale PID is reported as an unexpected exit and can be repaired with `recover` or `startup check` before starting again. `start` resumes an enabled but inactive login monitor after a previous `quit`. `stop` records an explicit stopped state before restoring managed macOS system proxy settings and stopping mihomo. It still returns `not_running` when there is no live process; the stopped intent is recorded even in that case. `restart` records a running state. The lifecycle lock serializes these state transitions with monitor recovery and `startup check`; an outer quit lock keeps user lifecycle commands from interleaving with a full quit. Login service support is platform-specific: macOS uses a per-user launchd Agent and Linux uses a per-user systemd unit. Missing service-manager sessions return an actionable manager error.

The installed login service runs a foreground Nagi monitor. It checks the process and control API every 30 seconds, and on macOS also checks managed system proxy settings; after a crash it removes stale runtime markers and attempts to start mihomo again. Failed checks or starts use exponential delays up to five minutes. The systemd unit restarts the monitor after a monitor failure; launchd uses `KeepAlive`. Disabling or uninstalling the service stops the monitor and mihomo without changing the desired state. The monitor restarts mihomo only when the desired state is running. An explicit stopped state persists in Nagi's state directory until `start` or `restart`. `quit` pauses the monitor for this login session. Once cleanup succeeds, a new login monitor resumes running intent at the next login; a later `start` resumes it in this session if login startup remains enabled. If quit fails, the intent remains stopped and the CLI reports the failed step; retry `quit` or run `start` after resolving the failure. A live process with an unreachable control API is reported in service logs and is never killed automatically. `start`, `stop`, and `restart` return `lifecycle_busy` while quit is in progress. If the quit process was interrupted and left a pending marker, run `nagi quit` again to finish cleanup; it is safe to repeat after the original process has ended. The periodic check also runs after network changes and sleep/wake, but it does not prove routes or DNS interception were restored. `startup check` remains a manual, non-restarting check. Its JSON payload includes `checked`, `running`, and `control_api`; on macOS, when running and reachable, it adds `system_proxy` with `managed`, `status`, and `reasserted`, `conflicts`, `untracked`, and `missing` arrays. Status is `unmanaged`, `current`, `reasserted`, `attention`, `stale_journal`, `legacy_journal`, or `backend_changed`. An OS read or write failure appears as `system_proxy_error` in the completed macOS check result, which still exits `0`; scripts must inspect that field. Applications should inspect these fields instead of assuming that an active service means an active proxy.

## Diagnostics and security

| Command | Result or effect |
| --- | --- |
| `diagnostics` | Run a redacted report covering paths, executable, selected profile readability, process and Unix Socket API state, listener ports, DNS, proxy listener exposure, controller exposure, and permission checks. |
| `diagnostics export ABSOLUTE_FILE` | Write the same redacted report to an owner-only JSON file. Profile contents, subscription URLs, credentials, and command output are never included. |

`doctor` remains the compatibility diagnostic with its documented eight checks. `diagnostics` adds security and connectivity checks and distinguishes a live PID, reachable Unix Socket API, configured listener, reachable listener, DNS enabled state, and controller exposure. A configured proxy or live control API does not prove that a request can traverse a proxy; use `proxy delay` for an explicit path test.

The diagnostic export is redacted by construction and written mode `0600`. It reports paths and actionable messages but never copies profile YAML or subscription metadata. Socket and configuration directory permissions are checked for group/other access. A non-loopback external controller or LAN listener produces a warning or error with a corrective action.

## Subscription scheduling metadata

`subscription schedule NAME SECONDS|off` persists an update interval from 1 through 31,536,000 seconds (one year), or disables scheduling with `off`. New subscriptions default to 3,600 seconds. Entries created by older Nagi versions without schedule metadata migrate to `off`. Run `nagi subscription watch` in the foreground or use the installed login monitor to host the scheduler. Only one scheduler runs per user; `scheduler_running` requires its live host lock and fresh heartbeat. The scheduler wakes at the next due time, polling at least once a minute for schedule changes, and downloads due subscriptions without holding `config.lock`, and commits cache/index changes under a short configuration lock. A completed download for an unselected or unassociated subscription updates only its cache. `subscription list` adds schedule, next due, cache/applied version, profile association, and redacted failure fields while continuing to omit URLs and credentials. A foreground watch prints a running message in text mode or one JSON success envelope (`scheduler_running: true`) when acquired. A second `subscription watch` exits 1 with `subscription_error`; interrupting the foreground host releases the scheduler lock.

Scheduler failures use fixed `last_failure_stage` and `last_failure_reason` values. `fetch/network_error` covers download failures; `decrypt/identity_unavailable` covers private identity or decryption failures; `validate/invalid_content` covers an unsupported or invalid feed. `apply/dns_target_missing`, `apply/custom_rule_conflict`, and `apply/selection_conflict` identify local settings that cannot be retained. Other activation or validation failures use `apply/activation_failed`. `pause_reason` persists as `profile_changed` or `source_unreadable` until explicit apply. During the pause, a failed download is visible through `last_failure_stage` and `last_failure_reason`; a later successful download returns those fields to `paused` and the pause reason. URLs, credentials, and feed contents are not stored in these fields.

A successful refresh creates a cache version independently of the applied version. A failed refresh may still retry a previously validated pending cache when the selected profile and source remain eligible. Automatic apply preserves local overrides, enabled custom rules, and saved group choices that still exist; removed choices are pruned. Rule targets are checked against the candidate source plus local override and custom rules. Known inline-provider nodes are included; remote-provider node names cannot be determined statically. Live mihomo group options are authoritative for choice pruning. When the engine or control API is unavailable, pruning uses the effective profile; choices for a group using a remote provider remain until mihomo can verify them. Invalid DNS proxy targets, missing custom rule targets, failed validation, or failed activation leave the new cache pending and report a redacted failure reason. External source edits or an unreadable source pause automatic apply while scheduled downloads continue to refresh the cache and advance `next_due`. A download failure advances `next_due` to a retry time and remains visible alongside `pause_reason`. Restoring the old source bytes does not resume automatic apply; explicitly run `subscription apply NAME` after reviewing the source to resume. `schedule off` disables both downloading and automatic apply. Applying a cached subscription records the cache version, profile association, and a source hash only after activation and saved-choice replacement succeed. Failed activation leaves the new cache available for retry and records only a fixed failure stage and redacted reason. Manual profile import or profile removal clears a subscription association after the profile operation succeeds; `profile use` leaves associations unchanged.
