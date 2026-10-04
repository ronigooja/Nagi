# CLI reference

This document defines the implemented Nagi commands and their JSON interface. `--json` may appear anywhere in the argument list. Without it, Nagi prints text intended for terminal reading. Scripts and applications should use `--json` rather than parse the text presentation.

## Help and syntax

Run `nagi`, `nagi help`, `nagi -h`, or `nagi --help` for top-level help. Use `nagi help profile use` or `nagi profile use --help` for a command's syntax, description, and example. `-h` is also accepted for command help. Help topics contain command names, without positional argument values.

Valid help requests exit with status `0`, even if runtime configuration is invalid or mihomo is unavailable. With `--json`, help text is a string in the success envelope's `data` field. Unknown help topics and incorrect argument counts exit with status `2` and show the relevant usage or help syntax. Argument counts and the `logs` integer range are checked before runtime configuration is read.

## Commands

| Command | Result or effect |
| --- | --- |
| `start`, `stop`, `restart` | Manage the mihomo process. `start` creates the default profile if absent. |
| `status` | Report running state, selected profile, PID, paths, and available mihomo version and mixed port. |
| `doctor` | Produce a read-only diagnostic report with checks and suggested next steps. |
| `logs [lines]` | Return the last 100 lines by default, or 1 through 10000 lines. |
| `config validate` | Check the selected profile with `mihomo -t`. |
| `config show` | Return the selected profile YAML. |
| `profile list` | List profile names and the current selection. |
| `profile use NAME` | Validate and activate an existing profile, reloading a running mihomo instance. |
| `profile import NAME FILE` | Validate and atomically write a profile from a local YAML file (maximum 8 MiB). |
| `profile export NAME FILE` | Write source YAML to a new private file; existing destinations are refused. |
| `profile backup NAME` | Copy source YAML to `profiles/NAME.yaml.bak`. |
| `profile restore NAME` | Validate and restore the latest backup; reload if selected and running. |
| `profile diff NAME [OTHER\|backup]` | Show changed source lines against another profile or the backup (default). |
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
| `dns check` | Audit the selected configuration and try a mihomo DNS query. |
| `subscription list` | List names and refresh times without URLs. |
| `subscription add NAME URL` | Save an HTTP or HTTPS subscription URL. |
| `subscription update NAME` | Fetch up to 8 MiB into Nagi's cache. |
| `subscription apply NAME` | Validate cached YAML as a mihomo profile, import it as profile `NAME`, and activate it. |
| `subscription remove NAME` | Remove a saved subscription and its cache. |
| `proxy groups`, `proxy show GROUP` | Read mihomo proxy groups through its Unix Socket API. |
| `proxy select GROUP NODE` | Select a node in a group. |
| `proxy delay NODE [URL] [TIMEOUT_MS]` | Measure a proxy's HTTP request latency through mihomo. |
| `connections list` | Return a snapshot of active connections. |
| `connections close ID` | Request closure of a connection by its ID. |
| `connections close-all` | Request closure of all current connections. |
| `mode [rule\|global\|direct]` | Read or change the running engine's routing mode. |
| `service install`, `service uninstall` | Manage a per-user launchd Agent or systemd service. |
| `version` | Report Nagi version, mihomo version and pinned commit, OS, and architecture. |
| `completion bash\|zsh\|fish` | Print a shell completion script without reading runtime configuration. |

Profile and subscription names consist of ASCII letters, digits, `_`, or `-` and have a maximum length of 128 characters. A profile file is named `profiles/NAME.yaml`. `subscription update` accepts HTTP 200 responses only. Nagi saves the response in its own cache. For subscriptions that contain a complete mihomo YAML configuration, `subscription apply` validates and activates it. Other subscription formats require conversion outside Nagi before importing a profile.

## Diagnostics

`doctor` inspects directory availability and owner read/search permission bits, the mihomo executable (including `NAGI_MIHOMO_BIN` resolved through `PATH`), selected-profile readability, PID liveness, and the Unix Socket `/version` endpoint. It does not create directories, change files, start the engine, validate YAML with mihomo, or test proxy connectivity. A live PID does not establish process identity. The API check has a two-second deadline.

The result is `{"healthy":true,"checks":[{"name":"...","status":"ok","message":"..."}]}` inside the success envelope. `status` is `ok`, `warning`, or `error`; `healthy` is false if any check has status `error`. Check names are `config_directory`, `data_directory`, `state_directory`, `runtime_directory`, `mihomo_executable`, `selected_profile`, `process`, and `control_api`. Missing first-use directories, a missing default profile, and a stopped engine can be warnings. A missing executable, invalid selection, missing selected non-default profile, or stale PID is an error.

A completed diagnostic report exits with status `0`, even when `healthy` is false. Scripts must inspect `data.healthy` and the individual checks. Failure to construct the report uses the normal error envelope and exit status `1`.

## Profile removal

`profile remove NAME` refuses to delete the selected profile, a nonregular file, or any profile when the current selection cannot be read. Select another profile first with `profile use NAME`. Missing profiles fail with `profile_error`. Removal does not delete a subscription with the same name or its cache. Existing `NAME.yaml.bak` files remain available for manual recovery; they are not listed as profiles.

## Local profile files and overrides

`profile export` reads the source profile, including any secrets it contains. It creates `FILE` with mode `0600` and never overwrites an existing path. `profile backup` explicitly refreshes the single `.bak` recovery copy. `profile restore` validates that copy before replacing the source profile; a successful restore swaps the source and backup contents. If validation or a running engine reload fails, the source and original backup remain available. `profile diff` compares source YAML lines and returns `name`, `other`, `changed` (boolean), and `diff` (string) in JSON. It does not parse or redact secrets.

Overrides live in `config/overrides/NAME.yaml`, separate from `config/profiles/NAME.yaml` and subscription cache. The override must be a YAML mapping; its keys replace corresponding source keys, and nested mappings merge recursively except DNS `nameserver-policy` and `proxy-server-nameserver-policy`, which replace their source maps. Lists, including `rules` and DNS server lists, are replaced as a whole. `profile override set` validates the merged configuration with mihomo and reloads it when selected and running. The merged file is generated as `config/profiles/.effective-NAME.yaml` for mihomo, keeping relative file paths anchored in the profiles directory; `profile export`, `config show`, and backups continue to use the source YAML. `profile override clear` restores the source configuration. Removing a profile retains its override file, so clear it explicitly before reusing the name if needed. Overrides can contain credentials and are stored with private permissions.

## DNS policy and leak checks

New default profiles enable mihomo DNS, IPv6, and fake IP mode. They listen only on `127.0.0.1:1053` and use `https://1.1.1.1/dns-query` and `https://8.8.8.8/dns-query` as both primary and bootstrap DoH servers. Existing profiles are not rewritten. No system resolver or TUN routing is changed by creating a default profile, so applications that do not send traffic through mihomo can still use their own DNS.

`dns set` writes DNS settings into the selected profile's separate local override, preserving them across source imports and subscription application. URLs must use HTTPS, include a path, and contain no credentials, query, or fragment. Multiple URLs are given to mihomo as primary upstreams. If all are unavailable, resolution fails; Nagi does not configure a plaintext or system DNS fallback. Direct mode connects to the DoH servers directly. Proxy mode appends mihomo's `#NODE` binding to each upstream and requires `NODE` to name a non-direct proxy in the source profile. If that node disappears, activation fails rather than silently using a network interface as fallback. Node provider entries are not accepted for this binding. Bootstrap and proxy-node hostname resolution still use the two fixed direct DoH servers, even in proxy mode. The DNS override enables both root and DNS IPv6 settings, clears source fallback and direct nameservers, and replaces source DNS server policies.

`dns exception add` requires a prior `dns set` for that profile. `DOMAIN` is an ASCII domain suffix such as `lan` or `corp.example`; Nagi stores it as `+.lan` or `+.corp.example` in mihomo's `nameserver-policy`. `SERVER` can be an HTTPS DoH URL or an explicit `udp://IP:53` or `tcp://IP:53` server. A plaintext exception sends matching DNS questions to that server without encryption; `dns status` and `dns check` report it. `dns exception remove` removes that local mapping. Local DNS policies replace source DNS policies as a whole to avoid retaining an unnoticed plaintext source resolver.

`dns tun on` configures mihomo TUN with `auto-route`, `strict-route`, interface auto-detection, and UDP/TCP port 53 hijack (`any:53` and `tcp://any:53`). It requires an encrypted primary and bootstrap DNS configuration. The engine and OS may require additional privileges or platform support to activate TUN; configuration validation alone does not establish that traffic is intercepted. `dns tun off` disables the selected profile's TUN override. These commands do not change the operating system's DNS server setting. On a platform or network where TUN cannot be activated, applications outside mihomo can still resolve through the system resolver.

`dns query` supports `A` and `AAAA` only and returns mihomo's `/dns/query` response. `dns flush` calls `/cache/dns/flush`. Both require a running control API. `dns check` inspects the selected effective configuration and makes one `example.com` A query through mihomo; it reports `engine_query_ok` and a list of configuration `issues`. The result always sets `leak_protection_verified: false`: Nagi does not yet prove OS route ownership, capture every application path, or perform an external DNS leak test. `dns status` is configuration inspection, not proof of running TUN state. Neither command prints full DoH URL paths or credentials; inspect `profile override show NAME` for exact local settings.

## Proxy latency, connections, and mode

`proxy delay NODE [URL] [TIMEOUT_MS]` defaults to `https://www.gstatic.com/generate_204` and `5000` milliseconds. Supply a URL before a custom timeout. The URL must be absolute HTTP or HTTPS without user information; the timeout must be an integer from `1` through `30000`. Invalid arguments fail before contacting the engine. The CLI allows up to 31 seconds for the control request, so the maximum measurement timeout is not cut short by the ordinary ten-second API timeout. `NODE` is a mihomo proxy name, including built-ins such as `DIRECT`; names with spaces must be quoted. The pinned mihomo implementation sends an HTTP HEAD request and records the latency in the proxy's delay history. This measures request latency, not bandwidth.

```sh
nagi proxy delay 'Node A'
nagi proxy delay 'Node A' https://example.com/ 8000
nagi connections list
nagi connections close CONNECTION_ID
nagi mode global
nagi mode
```

Replace `Node A` and `CONNECTION_ID` with names and IDs from your engine. Connection lists show IDs in text and JSON. Closing an ID that has already disappeared succeeds because mihomo treats it as an idempotent request. `close-all` is explicit, noninteractive, and affects all connections present when mihomo processes the request. Applications may immediately establish new connections. No closed-connection count is returned.

`mode` without an argument reads the running mode. With `rule`, `global`, or `direct`, it changes only the running engine through `PATCH /configs`. It does not edit profile YAML; restarting or reloading a profile reapplies the mode in that configuration. All commands in this section require a reachable mihomo Unix Socket API.

## Shell completion

`completion bash`, `completion zsh`, and `completion fish` print scripts for the selected shell. Text mode prints the script directly; `--json` returns it as the envelope's string `data`. Generation does not require mihomo or a valid selected profile. Completions cover command names, subcommands, global flags, supported mode/shell and DNS policy values, DNS query record types, and local files for profile import, export, and override set. Profile names, proxy names, and connection IDs are not queried dynamically.

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

Subscription feedback distinguishes saving a URL from downloading content and applying a profile. Downloading does not change the selected profile. Applying selects the profile and reloads a running engine; it does not start a stopped engine. Profile import validates and writes a file; use `profile use NAME` to select it. Replacing the currently selected profile also reloads a running engine; importing another profile does not reload it. Valid replacement content can repair an invalid existing profile.

## Recovery and service failures

An invalid selected-profile record does not prevent `stop`, `logs`, `version`, service management, subscription listing/downloading/removal, or proxy and connection operations. `status` still reports process state, omitting `profile` and adding `profile_error` when the selection cannot be read. `profile list` still lists files, omitting `current` and adding `current_error`. Text output warns that the selection is unavailable. Use `profile use NAME` with an existing valid profile or `subscription apply NAME` with valid cached content to repair an invalid selection. Import and removal require a readable selection so Nagi can determine whether their target is active.

If saving a subscription refresh time fails, Nagi attempts to restore the previous cache, or remove a newly created cache. Restoration failures are also returned. A failed refresh does not activate a profile.

If service installation fails after writing its definition, Nagi attempts to restore the old definition or remove the new one. The error reports the file outcome; service manager state can still have changed and must be checked before retrying. Uninstallation retains the definition if stopping/disabling the service fails. If the final systemd reload fails after removing the definition, the error explains that removal already completed and asks for a reload. An absent definition makes uninstallation a no-op; this does not verify that the service manager has no loaded service.

## Configuration validation failures

Nagi omits mihomo validation stdout and stderr because they may contain configuration values. Errors provide a next step instead: inspect the selected configuration for `config validate` or startup failures, and inspect the source YAML being imported/applied or the profile being selected for profile operations. Missing executable errors suggest checking `NAGI_MIHOMO_BIN`.

To inspect mihomo's own diagnostics locally, use `mihomo -t -f FILE -d DIRECTORY`, replacing `FILE` with the configuration file and `DIRECTORY` with its configuration directory. Use the executable selected by `NAGI_MIHOMO_BIN` when applicable. Those diagnostics may contain private configuration values. Nagi does not expose its temporary validation files as a recovery path.

## JSON and exit codes

JSON success output has the shape `{"ok":true,"data":{...}}`. JSON failure output is written to stderr as `{"ok":false,"error":{"code":"...","message":"..."}}`. Successful commands exit with status `0`; usage errors exit with `2`; all other errors exit with `1`. Error codes include `usage`, `already_running`, `not_running`, `not_found`, `invalid_config`, `profile_error`, `subscription_error`, `dns_error`, `proxy_selection_error`, `mihomo_api_error`, and `internal_error`. These codes and field names are the CLI integration contract for the macOS app.

Additional success payloads are defined below; all appear in `data`:

| Command | Fields |
| --- | --- |
| `doctor` | `healthy` (boolean), `checks` (array of `name`, `status`, and `message` strings), as defined above. |
| `profile export NAME FILE` | `name`, `file` (strings), `exported: true`. |
| `profile backup NAME` | `name` (string), `backed_up: true`. |
| `profile restore NAME` | `name` (string), `restored: true`. |
| `profile diff NAME [OTHER\|backup]` | `name`, `other`, `diff` (strings), `changed` (boolean). |
| `profile override set NAME FILE` | `name` (string), `override_saved: true`. |
| `profile override show NAME` | `name`, `yaml` (strings). |
| `profile override clear NAME` | `name` (string), `override_cleared: true`. |
| `profile remove NAME` | `name` (string), `removed: true`, `kind: "profile"`. |
| `dns status` | `profile`, `policy` (strings), `enabled`, `ipv6`, `tun_enabled`, `leak_protection_verified` (booleans), `upstream_hosts`, `issues` (string arrays). |
| `dns check` | DNS status fields plus `scope` (string), `engine_query_ok` (boolean). |
| `dns set` | `profile`, `policy`, `proxy_node` (strings), `upstream_count` (integer), `saved: true`. |
| `dns exception add/remove` | `profile`, `domain` (strings), `exception_changed: true`. |
| `dns tun on/off` | `profile` (string), `tun_enabled` (boolean), `saved: true`. |
| `dns query` | `domain`, `type` (strings), `response` (mihomo JSON object). |
| `dns flush` | `flushed: true`. |
| `proxy delay` | `proxy` and `url` (strings), `timeout_ms` and `delay_ms` (integer milliseconds). |
| `connections close ID` | `id` (string), `closed: true` (request accepted, including an already absent connection). |
| `connections close-all` | `closed: true` (request accepted; no count). |
| `mode` | `mode` (string: `rule`, `global`, or `direct`). |
| `mode MODE` | `mode` (string), `changed: true` (update accepted). |
| `completion SHELL` | A string containing the completion script. |

`NAGI_MIHOMO_BIN` overrides the default mihomo executable path, which is a file named `mihomo` beside the Nagi executable. Nagi uses XDG directories on Linux and `~/Library/Application Support/Nagi` on macOS; see the [runtime design](../design/runtime.md).
