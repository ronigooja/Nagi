# CLI reference

This document defines the implemented Nagi commands and their JSON interface. `--json` may appear anywhere in the argument list. Without it, Nagi prints text intended for terminal reading. Scripts and applications should use `--json` rather than parse the text presentation.

## Terminal dashboard

`nagi tui` opens the interactive dashboard on a terminal. It requires terminal stdin and stdout, and exits with status `2` if either is redirected or if `--json` is supplied. It shows engine, profile, runtime mode, DNS, system proxy, and TUN state, proxy groups and nodes, saved subscription names, recent logs, loading and error states. A value is `unknown` or `unavailable` when it cannot be verified. See the [terminal dashboard guide](../guides/terminal-dashboard.md) for keys and task flow. TUI actions use the same command behavior as their noninteractive equivalents. The command produces no JSON success envelope; the other commands and their JSON contracts are unchanged.

## Help and syntax

Run `nagi`, `nagi help`, `nagi -h`, or `nagi --help` for top-level help. Use `nagi help profile use` or `nagi profile use --help` for a command's syntax, description, and example. `-h` is also accepted for command help. Help topics contain command names, without positional argument values.

Valid help requests exit with status `0`, even if runtime configuration is invalid or mihomo is unavailable. With `--json`, help text is a string in the success envelope's `data` field. Unknown help topics and incorrect argument counts exit with status `2` and show the relevant usage or help syntax. Argument counts and the `logs` integer range are checked before runtime configuration is read.

## Commands

| Command | Result or effect |
| --- | --- |
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
| `profile remove NAME` | Delete an unselected regular profile file, retaining any existing `.bak` file. |
| `dns status` | Show selected profile DNS policy, upstream hosts, IPv6, TUN configuration, and warnings. |
| `dns set direct URL [URL...]` | Save one or more HTTPS DoH upstreams for direct connection. |
| `dns set proxy NODE URL [URL...]` | Pin DoH upstreams to a named non-direct proxy node in the source profile. |
| `dns exception add DOMAIN SERVER` | Route a domain suffix to an explicit DoH or plaintext IP DNS server. |
| `dns exception remove DOMAIN` | Remove a local DNS exception. |
| `dns tun on`, `dns tun off` | Save TUN DNS interception settings for the selected profile. |
| `dns query DOMAIN [A\|AAAA]` | Query the running mihomo resolver over its private control socket. |
| `dns flush` | Clear the running mihomo DNS cache. |
| `dns check` | Audit the selected configuration and query mihomo DNS through its control API. |
| `subscription list` | List names, refresh times, and available expiry and traffic metadata without URLs. |
| `subscription add NAME URL` | Save an HTTP or HTTPS subscription URL. |
| `subscription update NAME` | Fetch and validate up to 8 MiB, cache it, and report node changes against the preceding cache. |
| `subscription preview NAME` | Compare cached nodes with profile `NAME` without changing either. |
| `subscription apply NAME` | Parse and validate the mihomo YAML cache, retain local settings and available group selections from profile `NAME`, then activate it. |
| `subscription remove NAME` | Remove a saved subscription and its cache. |
| `proxy groups`, `proxy show GROUP` | Read mihomo proxy groups, nodes, current selection, and selection availability through its Unix Socket API. |
| `proxy search QUERY` | Find nodes by case-insensitive name substring and list matching groups. |
| `proxy select GROUP NODE` | Select a node in a group and save the choice for the selected profile. |
| `proxy delay NODE [URL] [TIMEOUT_MS]` | Measure a proxy's HTTP request latency through mihomo. |
| `proxy delays GROUP [URL] [TIMEOUT_MS]` | Test all nodes in a group and sort successful measurements by latency. |
| `proxy restore` | Replay saved choices for the selected profile where the group and node still exist; report missing choices. |
| `connections list` | Return a snapshot of active connections, including mihomo's selected proxy chains when supplied. |
| `connections show ID` | Return one active connection by ID, including metadata and proxy chain. |
| `connections close ID` | Request closure of a connection by its ID. |
| `connections close-all` | Request closure of all current connections. |
| `mode [rule\|global\|direct\|save MODE\|saved]` | Read or change temporary runtime mode, or explicitly persist/read the selected profile's mode. |
| `service install`, `service uninstall` | Manage a per-user launchd Agent or systemd service. |
| `version` | Report Nagi version, mihomo version and pinned commit, OS, and architecture. |
| `completion bash\|zsh\|fish` | Print a shell completion script without reading runtime configuration. |

Profile and subscription names consist of ASCII letters, digits, `_`, or `-` and have a maximum length of 128 characters. A profile file is named `profiles/NAME.yaml`. `subscription update` accepts HTTP 200 responses only. Nagi validates a mihomo YAML document with `proxies` or `proxy-providers` before replacing the cache. A YAML document with `proxies` but no groups gets a `Subscription` select group and `MATCH,Subscription` rule. Unsupported or malformed responses leave the previous cache and refresh metadata intact.

`subscription update` reports added, removed, and changed nodes compared with the preceding cache; `subscription preview` compares the cache with the existing profile. A changed node keeps its name but has a different definition. Lists are sorted by name. `subscription apply` preserves the existing profile's rules, rule providers, DNS, hosts, TUN, listening ports, LAN access, mode, controller, IPv6, sniffer, and profile settings. It carries existing proxy-group node order forward for nodes that remain available, then appends new nodes. When applying to the selected profile with a reachable engine, Nagi also attempts to restore the live selection in each group when that node remains available; `selections_restored` counts accepted restore requests. A removed node cannot be retained. Existing profile content that cannot be parsed for merging must be repaired or removed before applying.

The optional `Subscription-Userinfo` response header supplies `upload`, `download`, `total` (nonnegative bytes) and `expire` (Unix seconds). Available values appear in `subscription list` and the update result. The URL remains hidden from list output. A missing header clears previously stored traffic and expiry values on a successful update.

## Diagnostics

`doctor` inspects directory availability and owner read/search permission bits, the mihomo executable (including `NAGI_MIHOMO_BIN` resolved through `PATH`), selected-profile readability, PID liveness, and the Unix Socket `/version` endpoint. It does not create directories, change files, start the engine, validate YAML with mihomo, or test proxy connectivity. A live PID does not establish process identity. The API check has a two-second deadline.

The result is `{"healthy":true,"checks":[{"name":"...","status":"ok","message":"..."}]}` inside the success envelope. `status` is `ok`, `warning`, or `error`; `healthy` is false if any check has status `error`. Check names are `config_directory`, `data_directory`, `state_directory`, `runtime_directory`, `mihomo_executable`, `selected_profile`, `process`, and `control_api`. Missing first-use directories, a missing default profile, and a stopped engine can be warnings. A missing executable, invalid selection, missing selected non-default profile, or stale PID is an error.

A completed diagnostic report exits with status `0`, even when `healthy` is false. Scripts must inspect `data.healthy` and the individual checks. Failure to construct the report uses the normal error envelope and exit status `1`.

## Profile removal

`profile remove NAME` refuses to delete the selected profile, a nonregular file, or any profile when the current selection cannot be read. Select another profile first with `profile use NAME`. Missing profiles fail with `profile_error`. Removal does not delete a subscription with the same name or its cache. Existing `NAME.yaml.bak` files remain available for manual recovery; they are not listed as profiles.

## Local profile files and overrides

`profile export` reads the source profile, including any secrets it contains. It creates `FILE` with mode `0600` and never overwrites an existing path. `profile backup` explicitly refreshes the single `.bak` recovery copy. `profile restore` validates that copy before replacing the source profile; a successful restore swaps the source and backup contents. If validation or a running engine reload fails, the source and original backup remain available. `profile diff` compares source YAML lines and parses both files for structural changes. JSON retains `name`, `other`, `changed` (boolean), and `diff` (string), and adds `changes` and `warnings`. `changes` contains YAML pointer paths with `added`, `removed`, `modified`, `type_change`, or `order_changed` kinds, plus applicable `before` and `after` values. Proxies and groups with unique names use their escaped names in paths; their order is reported separately. Warnings flag rule changes, DNS policy changes, removed proxies or groups, YAML type changes, and exposure setting changes. If either file cannot be parsed as YAML or a structural value cannot be represented in JSON, the line diff remains available, `changes` is empty, and `warnings` explains the failure. The line diff and structural values can contain credentials; do not publish their output without review.

Overrides live in `config/overrides/NAME.yaml`, separate from `config/profiles/NAME.yaml` and subscription cache. The override must be a YAML mapping; its keys replace corresponding source keys, and nested mappings merge recursively except DNS `nameserver-policy` and `proxy-server-nameserver-policy`, which replace their source maps. Lists, including `rules` and DNS server lists, are replaced as a whole. `profile override set` validates the merged configuration with mihomo and reloads it when selected and running. The merged file is generated as `config/profiles/.effective-NAME.yaml` for mihomo, keeping relative file paths anchored in the profiles directory; `profile export`, `config show`, and backups continue to use the source YAML. `profile override clear` restores the source configuration. Removing a profile retains its override file, so clear it explicitly before reusing the name if needed. Overrides can contain credentials and are stored with private permissions.

## DNS policy and checks

New default profiles enable mihomo DNS, IPv6, and fake IP mode. They listen only on `127.0.0.1:1053` and use `https://1.1.1.1/dns-query` and `https://8.8.8.8/dns-query` as both primary and bootstrap DoH servers. Existing profiles are not rewritten. No system resolver or TUN routing is changed by creating a default profile, so applications that do not send traffic through mihomo can still use their own DNS.

`dns set` writes DNS settings into the selected profile's separate local override, preserving them across source imports and subscription application. URLs must use HTTPS, include a path, and contain no credentials, query, or fragment. Multiple URLs are given to mihomo as primary upstreams. If all are unavailable, resolution fails; Nagi does not configure a plaintext or system DNS fallback. Direct mode connects to the DoH servers directly. Proxy mode appends mihomo's `#NODE` binding to each upstream and requires `NODE` to name a non-direct proxy in the source profile. If that node disappears, activation fails rather than silently using a network interface as fallback. Node provider entries are not accepted for this binding. Bootstrap and proxy-node hostname resolution still use the two fixed direct DoH servers, even in proxy mode. The DNS override enables both root and DNS IPv6 settings, clears source fallback and direct nameservers, and replaces source DNS server policies.

`dns exception add` requires a prior `dns set` for that profile. `DOMAIN` is an ASCII domain suffix such as `lan` or `corp.example`; Nagi stores it as `+.lan` or `+.corp.example` in mihomo's `nameserver-policy`. `SERVER` can be an HTTPS DoH URL or an explicit `udp://IP:53` or `tcp://IP:53` server. A plaintext exception sends matching DNS questions to that server without encryption; `dns status` and `dns check` report it. `dns exception remove` removes that local mapping. Local DNS policies replace source DNS policies as a whole to avoid retaining an unnoticed plaintext source resolver.

`dns tun on` configures mihomo TUN with `auto-route`, `strict-route`, interface auto-detection, and UDP/TCP port 53 hijack (`any:53` and `tcp://any:53`). It requires an encrypted primary and bootstrap DNS configuration. The engine and OS may require additional privileges or platform support to activate TUN; configuration validation alone does not establish that traffic is intercepted. `dns tun off` disables the selected profile's TUN override. These commands do not change the operating system's DNS server setting. On a platform or network where TUN cannot be activated, applications outside mihomo can still resolve through the system resolver.

`dns query` supports `A` and `AAAA` only and returns mihomo's `/dns/query` response. `dns flush` calls `/cache/dns/flush`. Both require a running control API. `dns check` inspects the selected effective configuration and makes one `example.com` A query through mihomo. Its scope is configuration and mihomo API state; it does not inspect operating-system routes, resolver capture, or external DNS leaks. `dns status` is configuration inspection and does not prove running TUN state. Neither command prints full DoH URL paths or credentials; inspect `profile override show NAME` for exact local settings.

## Proxy latency, connections, and mode

`proxy delay NODE [URL] [TIMEOUT_MS]` defaults to `https://www.gstatic.com/generate_204` and `5000` milliseconds. Supply a URL before a custom timeout. The URL must be absolute HTTP or HTTPS without user information; the timeout must be an integer from `1` through `30000`. Invalid arguments fail before contacting the engine. The CLI allows up to 31 seconds for the control request, so the maximum measurement timeout is not cut short by the ordinary ten-second API timeout. `NODE` is a mihomo proxy name, including built-ins such as `DIRECT`; names with spaces must be quoted. The pinned mihomo implementation sends an HTTP HEAD request and records the latency in the proxy's delay history. This measures request latency, not bandwidth.

`proxy delays GROUP [URL] [TIMEOUT_MS]` uses the same URL and timeout rules. It measures group entries with at most eight concurrent requests and returns one result for each node. Successful measurements sort by ascending `delay_ms`, then name; failed measurements follow with `error: "latency test failed"`. An empty group returns an empty `results` array. `proxy search QUERY` matches node names without case sensitivity and returns groups containing matches. `proxy groups` and `proxy show GROUP` add `selected_status`: `available`, `unavailable` (mihomo reports the group not alive), `removed` (the selected name is absent), or `none`. Selecting a missing node fails with `proxy_selection_error` and suggests `proxy show`.

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

`config reload` validates the selected profile's effective configuration first, requires a running engine, and sends mihomo `PUT /configs?force=true` with the source or generated effective profile path. Validation failure leaves the running engine untouched. If the API reload fails after validation, the command reports that runtime state may still use the previous configuration; Nagi does not claim a rollback of mihomo's internal state. A successful reload returns `reloaded: true` and the profile name.

`connections show ID` finds one connection in a fresh `/connections` snapshot and reports the same `id`, `metadata`, `chains`, `upload`, and `download` fields as a list item. A missing ID fails with a suggestion to list current IDs. `connections list` remains a snapshot. `connections close ID` is idempotent when mihomo has already removed the connection; `connections close-all` affects connections present when mihomo processes the request. A closed connection can be recreated by applications.

`logs follow` starts at the current end of the log and streams newly appended complete lines until interrupted. It does not replay existing lines. Text mode prints each new line; `--json` emits one success envelope per line with `data.line`. Terminal users should press Ctrl-C. Log file errors are reported without exposing configuration values.

`traffic watch` connects to mihomo's private Unix Socket `GET /traffic` stream and prints each sample as it arrives, approximately once per second. It requires a running mihomo control API. Text mode labels rates in bytes per second and cumulative totals in bytes. `--json` emits one newline-delimited success envelope per sample with `data.upload_bps`, `data.download_bps`, `data.upload_total`, and `data.download_total` as nonnegative integer byte counts, plus `data.sampled_at` as an RFC 3339 UTC timestamp. The rates are mihomo's current rates; the totals are mihomo's counters since its current start. Ctrl-C or SIGTERM ends the command successfully. A failed connection, malformed sample, or unexpected stream end writes a failure envelope to stderr in JSON mode and exits `1` with `mihomo_api_error`; a failed output write uses `output_error`. The stream is not subject to the ordinary control request's whole-request timeout.

## Shell completion

`completion bash`, `completion zsh`, and `completion fish` print scripts for the selected shell. Text mode prints the script directly; `--json` returns it as the envelope's string `data`. Generation does not require mihomo or a valid selected profile. Completions cover command names, subcommands, global flags, supported mode/shell and DNS policy values, DNS query record types, and local files for profile import, export, and override set. At completion time, proxy group and node names are queried from the running engine; an unreachable engine yields no dynamic proxy candidates. Profile and subscription names are queried from local stores without exposing URLs. `completion candidates groups|nodes [GROUP]|profile|subscription` returns newline-separated text or a JSON string array; connection IDs are not completed.

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

JSON success output has the shape `{"ok":true,"data":{...}}`. JSON failure output is written to stderr as `{"ok":false,"error":{"code":"...","message":"..."}}`. Successful commands exit with status `0`; usage errors exit with `2`; all other errors exit with `1`. Error codes include `usage`, `already_running`, `not_running`, `lifecycle_busy`, `not_found`, `invalid_config`, `profile_error`, `subscription_error`, `dns_error`, `rule_error`, `proxy_selection_error`, `system_proxy_error`, `tun_error`, `diagnostic_error`, `mihomo_api_error`, and `internal_error`. These codes and field names are the CLI integration contract for the macOS app. Error messages are intended for people and may change; applications should branch on `error.code`. URL-shaped text in errors is replaced with `[redacted URL]` before text or JSON output.

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
| `profile remove NAME` | `name` (string), `removed: true`, `kind: "profile"`. |
| `dns status` | `profile`, `policy` (strings), `enabled`, `ipv6`, `tun_enabled` (booleans), `upstream_hosts`, `issues` (string arrays). |
| `dns check` | DNS status fields plus `scope` (string) and `engine_query_ok` (boolean). Scope is `configuration and mihomo API`. |
| `dns set` | `profile`, `policy`, `proxy_node` (strings), `upstream_count` (integer), `saved: true`. |
| `dns exception add/remove` | `profile`, `domain` (strings), `exception_changed: true`. |
| `dns tun on/off` | `profile` (string), `tun_enabled` (boolean), `saved: true`. |
| `dns query` | `domain`, `type` (strings), `response` (mihomo JSON object). |
| `dns flush` | `flushed: true`. |
| `subscription list` | `subscriptions` array: `name`, optional `updated_at` and `expires_at` (RFC 3339), optional nonzero `upload`, `download`, `total` (bytes). URLs are omitted. |
| `subscription update NAME` | `name`, `bytes`, `updated_at`, `preview`, and optional `expires_at`, `upload`, `download`, `total`. |
| `subscription preview NAME` | `name`, `format`, sorted `added`, `removed`, and `changed` node-name arrays. |
| `subscription apply NAME` | `name`, `profile`, `applied: true`, `preview`, `selections_restored` (accepted live selection restore requests). |
| `proxy delay` | `proxy` and `url` (strings), `timeout_ms` and `delay_ms` (integer milliseconds). |
| `proxy delays` | `group`, `url`, `timeout_ms`, `results` array of `proxy`, optional `delay_ms` or `error`. |
| `proxy search` | `query`, `groups` array with the same group fields as `proxy groups`. |
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
| `completion candidates RESOURCE [GROUP]` | An array of local profile or subscription names, or live proxy group or node names. `GROUP` applies only to nodes. |

`NAGI_MIHOMO_BIN` overrides the default mihomo executable path, which is a file named `mihomo` beside the Nagi executable. Nagi uses XDG directories on Linux and `~/Library/Application Support/Nagi` on macOS; see the [runtime design](../design/runtime.md).

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
| `system-proxy status` | Read active OS HTTP, HTTPS, and SOCKS proxy settings. |
| `system-proxy enable` | Save current OS settings and point HTTP and HTTPS proxy settings at mihomo's local mixed port (or HTTP port). Requires a running engine. |
| `system-proxy disable` | Restore settings saved by Nagi; when no restore record exists, report the current settings without changing them. |
| `tun status`, `tun enable`, `tun disable` | Read or change mihomo TUN at runtime. Enable requests automatic route and interface detection; status also inspects a named adapter when available. |
| `ports status` | Show HTTP/HTTPS, SOCKS, and mixed listener ports, bind address, and LAN flag. Port `0` means the listener is disabled. HTTPS here means an HTTP CONNECT proxy, not TLS termination. |
| `lan status`, `lan enable [ADDRESS]`, `lan disable` | Read or change runtime LAN access and bind address. `lan enable` defaults to all interfaces (`*`); ADDRESS must be an IPv4 or IPv6 address. Disable binds to `127.0.0.1`. |

`system-proxy` uses `networksetup` for active macOS network services. On Linux it requires an active GNOME desktop session with `gsettings`; other desktop environments return `system_proxy_error` with the required capability. The saved settings are held in a mode `0600` restore record under Nagi's state directory. `stop` and `restart` restore them before stopping mihomo. A detached watcher restores them after an unexpected engine exit, and `status` retries restoration when it finds the engine stopped. If restoration fails, the record remains for `system-proxy disable` to retry. These commands cannot restore settings after the user removes the restore record or if the watcher and Nagi are both unable to run. `startup check`, including checks made by the installed login service, compares managed settings with both the saved baseline and Nagi's target. When all GNOME settings, or an individual known macOS service's settings, exactly revert to the baseline, it reapplies Nagi's target. A different value is reported as a conflict and left untouched. New macOS services are reported as untracked; removed services are reported as missing. Disable and re-enable the system proxy to include a newly active service. Existing restore records without a saved target are reported as `legacy_journal` and are not automatically reasserted. A user deliberately restoring the exact original settings cannot be distinguished from an OS reset and may be reasserted while the login service is active.

`tun enable` needs OS permission to create a TUN device and install routes. macOS and Linux permission mechanisms differ; inspect `nagi logs` if the adapter does not start. The mihomo API may accept a TUN patch while logging an adapter creation error, so a reported `enabled` setting alone does not prove that interception works. `tun status|enable|disable` also reports `adapter_status`: `inactive` when TUN is disabled, `unknown` when no device name is exposed by mihomo or OS interface enumeration fails, and `missing`, `down`, or `up` for a configured `tun.device` name. `adapter_name` is present only when mihomo exposes that name. An up adapter does not prove route ownership or DNS capture. Runtime TUN and LAN changes are reset when a profile is reloaded or mihomo restarts. Enabling LAN exposes configured proxy listeners at the selected address; use firewall rules and authentication appropriate to that network.

JSON payloads for `system-proxy status|enable|disable` include `enabled` (boolean), `backend`, and `http`, `https`, and `socks` objects with `enabled`, optional `host`, and optional `port`. `tun status|enable|disable` include `enabled` (boolean), `settings` (mihomo TUN object), `adapter_status` (string), and optional `adapter_name` (string). `lan status|enable|disable` include `enabled` and `bind_address`. `ports status` includes integer `http`, `https`, `socks`, and `mixed`, plus `bind_address` and `allow_lan`. The `http` and `https` values are the same because mihomo uses one HTTP proxy listener for both schemes. All use the usual success/error envelope and exit codes. `system_proxy_error` and `tun_error` indicate failed traffic operations.

## Startup and background operation

| Command | Result or effect |
| --- | --- |
| `service install` | Install and enable the per-user launchd/systemd service. |
| `service uninstall` | Disable, stop, and remove the service definition. |
| `service status` | Report definition path, manager, installed, enabled, and active state. |
| `startup status` | Report service status together with mihomo process state. |
| `startup enable` | Enable an installed login service without rewriting its definition. |
| `startup disable` | Disable the login service while retaining its definition. |
| `startup check` | Recheck PID/socket state and `/version`, and reassert managed system proxy settings only when they reverted to the saved baseline. Remove stale runtime markers after an unexpected exit. It never silently starts mihomo. |
| `quit` | Stop this session's monitor, restore Nagi-managed system proxy settings, and stop mihomo while preserving login startup preference. Returns `quit: true`. |
| `recover` | Remove stale PID and socket markers after an unexpected exit. A live process is never touched. |

`start` is duplicate-safe: a live PID returns `already_running` while recording the desired running state. A stale PID is reported as an unexpected exit and can be repaired with `recover` or `startup check` before starting again. `start` resumes an enabled but inactive login monitor after a previous `quit`. `stop` records an explicit stopped state before restoring managed system proxy settings and stopping mihomo. It still returns `not_running` when there is no live process; the stopped intent is recorded even in that case. `restart` records a running state. The lifecycle lock serializes these state transitions with monitor recovery and `startup check`; an outer quit lock keeps user lifecycle commands from interleaving with a full quit. Login service support is platform-specific: macOS uses a per-user launchd Agent and Linux uses a per-user systemd unit. Missing service-manager sessions return an actionable manager error.

The installed login service runs a foreground Nagi monitor. It checks the process, control API, and managed system proxy every 30 seconds; after a crash it removes stale runtime markers and attempts to start mihomo again. Failed checks or starts use exponential delays up to five minutes. The systemd unit restarts the monitor after a monitor failure; launchd uses `KeepAlive`. Disabling or uninstalling the service stops the monitor and mihomo without changing the desired state. The monitor restarts mihomo only when the desired state is running. An explicit stopped state persists in Nagi's state directory until `start` or `restart`. `quit` pauses the monitor for this login session. Once cleanup succeeds, a new login monitor resumes running intent at the next login; a later `start` resumes it in this session if login startup remains enabled. If quit fails, the intent remains stopped and the CLI reports the failed step; retry `quit` or run `start` after resolving the failure. A live process with an unreachable control API is reported in service logs and is never killed automatically. `start`, `stop`, and `restart` return `lifecycle_busy` while quit is in progress. If the quit process was interrupted and left a pending marker, run `nagi quit` again to finish cleanup; it is safe to repeat after the original process has ended. The periodic check also runs after network changes and sleep/wake, but it does not prove routes or DNS interception were restored. `startup check` remains a manual, non-restarting check. Its JSON payload includes `checked`, `running`, and `control_api`; when running and reachable it adds `system_proxy` with `managed`, `status`, and `reasserted`, `conflicts`, `untracked`, and `missing` arrays. Status is `unmanaged`, `current`, `reasserted`, `attention`, `stale_journal`, `legacy_journal`, or `backend_changed`. An OS read or write failure appears as `system_proxy_error` in the completed check result, which still exits `0`; scripts must inspect that field. Applications should inspect these fields instead of assuming that an active service means an active proxy.

## Diagnostics and security

| Command | Result or effect |
| --- | --- |
| `diagnostics` | Run a redacted report covering paths, executable, selected profile readability, process and Unix Socket API state, listener ports, DNS, proxy listener exposure, controller exposure, and permission checks. |
| `diagnostics export ABSOLUTE_FILE` | Write the same redacted report to an owner-only JSON file. Profile contents, subscription URLs, credentials, and command output are never included. |

`doctor` remains the compatibility diagnostic with its documented eight checks. `diagnostics` adds security and connectivity checks and distinguishes a live PID, reachable Unix Socket API, configured listener, reachable listener, DNS enabled state, and controller exposure. A configured proxy or live control API does not prove that a request can traverse a proxy; use `proxy delay` for an explicit path test.

The diagnostic export is redacted by construction and written mode `0600`. It reports paths and actionable messages but never copies profile YAML or subscription metadata. Socket and configuration directory permissions are checked for group/other access. A non-loopback external controller or LAN listener produces a warning or error with a corrective action.
