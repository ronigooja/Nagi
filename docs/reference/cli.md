# CLI reference

This document defines the implemented Nagi commands and their JSON interface. Except for the interactive `menu` command and explicit raw request-rewrite exports, `--json` may appear anywhere in the argument list. Without it, Nagi prints text intended for terminal reading. Scripts and applications should use ordinary commands with `--json` rather than parse the text presentation.

## Help and syntax

Run `nagi`, `nagi help`, `nagi -h`, or `nagi --help` for top-level help. Use `nagi help profile use` or `nagi profile use --help` for a command's syntax, description, and example. `-h` is also accepted for command help. Help topics contain command names, without positional argument values.

Valid help requests exit with status `0`, even if runtime configuration is invalid or mihomo is unavailable. With `--json`, help text is a string in the success envelope's `data` field. Unknown help topics and incorrect argument counts exit with status `2` and show the relevant usage or help syntax. Argument counts and the `logs` integer range are checked before runtime configuration is read.

## Language selection

`--lang en` or `--lang zh-Hans` selects the human presentation language when
placed before the command, for example `nagi --lang zh-Hans --json status`.
`--lang=zh-Hans` is equivalent. Without an explicit selection, the first nonempty
`NAGI_LANG`, `LC_ALL`, `LC_MESSAGES`, or `LANG` determines the language; unsupported
automatic locales fall back to English. Missing, repeated, empty, or unsupported
explicit language options exit with usage status `2`. Command operands remain
command-owned, so a resource named `--lang` is preserved after the command.

Human command help, source-owned errors, output labels and terminal menu pages
support both languages. Technical data fields keep their literal missing-value
placeholders in either language. Unknown or unannotated messages retain their
original fallback. JSON values retain their original
English strings. Typed messages may
add optional `messages` metadata mapping existing string JSON Pointers to
`{"id":"semantic.id","args":{"name":"value"}}`; absent or invalid metadata
must preserve the original response. The [localization design](../design/localization.md)
defines argument types, fallback and acceptance status. Optional
`argument_messages` composes descriptors for existing string arguments while
leaving their original scalar values unchanged. Invalid children fall back
independently; legacy consumers can ignore this field.

Human traffic samples and certificate validity times display timestamps at minute
precision. Machine-readable `sampled_at` and stored timestamps retain their
documented formats. Generated completion scripts and candidate values are
language-independent raw content.

## Commands

| Command | Result or effect |
| --- | --- |
| `rewrite <command>` (also `request-rewrite`) | Manage TUN content rewrite rules, scripts, activation, sensitive preview/export and local CA; see [TUN request rewrite](#tun-request-rewrite). |
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
| `profile override clear NAME` | Remove ordinary local overrides and additions, retaining any persistent subscription chain policy; reload if selected and running. |
| `profile override apply-default NAME [--dns-proxy GROUP]` | Apply default runtime settings while retaining the target's proxy and routing resources and TUN enable flag. A profile with proxies needs a DNS proxy group on first apply; later applies reuse the saved group. |
| `profile override default-status NAME` | Report whether the applied local default override is still active and the recorded DNS proxy group, if any. |
| `profile override restore-default NAME` | Restore the override that existed before `apply-default`, or remove it if none existed. |
| `profile remove NAME` | Delete an unselected regular profile file, its backup, local overrides, and saved proxy choices. |
| `dns status` | Show selected profile DNS policy, upstream hosts, IPv6, TUN configuration, and warnings. |
| `dns set direct URL [URL...] [--bootstrap IP_DOH_URL ...]` | Save one or more HTTPS DoH upstreams for direct connection, optionally selecting direct bootstrap resolvers. |
| `dns set proxy NAME URL [URL...] [--bootstrap IP_DOH_URL ...]` | Pin DoH upstreams to a named non-direct proxy node or proxy group in the effective profile, optionally selecting direct bootstrap resolvers. |
| `dns exception add DOMAIN SERVER` | Route a domain suffix to an explicit DoH or plaintext IP DNS server. |
| `dns exception remove DOMAIN` | Remove a local DNS exception. |
| `dns tun on`, `dns tun off` | Save TUN DNS interception settings for the selected profile. |
| `dns query DOMAIN [A\|AAAA]` | Query the running mihomo resolver over its private control socket. |
| `dns flush` | Clear the running mihomo DNS cache. |
| `dns check` | Audit the selected configuration and query mihomo DNS through its control API. |
| `subscription list` | List names, refresh and schedule state, cache/application metadata, and available expiry and traffic metadata without URLs. |
| `subscription watch` | Run the foreground scheduler until interrupted; the installed login service hosts the same scheduler. A duplicate host exits with structured error code `scheduler_already_running`. |
| `subscription schedule NAME SECONDS\|off` | Persist an update interval from 1 through 31,536,000 seconds, or disable scheduling with `off`; a running scheduler refreshes when due and applies only the selected associated profile. |
| `subscription add NAME URL [--schedule SECONDS|off] [--test-url URL]` | Save an HTTP or HTTPS subscription URL with optional update interval and automatic test URL before the first download. |
| `subscription key NAME FILE\|clear` | Copy a local age identity file for a saved subscription, or clear its identity. |
| `subscription update NAME` | Fetch and validate up to 8 MiB, cache it, and report node changes against the preceding cache. |
| `subscription preview NAME` | Compare cached nodes with profile `NAME` without changing either. |
| `subscription apply NAME` | Replace profile `NAME` with the cached mihomo configuration, retain local settings and node/group changes and any persistent subscription chain policy, clear saved group choices, then activate it. |
| `subscription remove NAME` | Remove a saved subscription, its cache, and its stored identity. |
| `proxy groups`, `proxy show GROUP` | Read mihomo proxy groups, nodes, current selection, and selection availability through its Unix Socket API. |
| `proxy group list`, `proxy group show GROUP` | Read configured proxy groups and editable settings locally, including while stopped. |
| `proxy group order GROUP [GROUP...]` | Save the complete proxy group display order locally without reloading mihomo; see [proxy group management](#proxy-group-management). |
| `proxy group add GROUP [OPTIONS]`, `proxy group edit GROUP OPTIONS`, `proxy group remove GROUP` | Create, edit, or delete a configured group through the local override; validate and reload only when running. See [proxy group management](#proxy-group-management). |
| `proxy search QUERY` | Find nodes by case-insensitive name substring and list matching groups. |
| `proxy select GROUP NODE` | Select a node in a group and save the choice for the selected profile. |
| `proxy auto GROUP` | Remove a fixed choice from a URLTest or Fallback group and its saved selection, resuming mihomo automatic selection. |
| `proxy providers`, `proxy provider show NAME` | List or inspect proxy providers with redacted metadata and node names. |
| `proxy provider add NAME URL GROUP [SECONDS|off] [--test-url URL]` | Save an HTTP provider in the selected profile's local override, attach it to a group, validate, and reload if running. |
| `proxy provider remove NAME` | Remove a provider and group references from the selected profile, validate, and reload if running. |
| `proxy provider update NAME`, `proxy provider healthcheck NAME` | Ask mihomo to update a proxy provider or start its native healthcheck. |
| `proxy provider interval show|set|reset NAME [SECONDS|off]` | Inspect, change, or restore a Provider's automatic download interval. |
| `proxy healthcheck show group\|provider\|subscription NAME` | Inspect a saved subscription policy or the selected profile's Provider/group check interval without probing nodes. |
| `proxy healthcheck set group\|provider\|subscription NAME SECONDS` | Save a local interval; validate associated configuration and reload only the affected selected/running profile. |
| `proxy healthcheck reset group\|provider\|subscription NAME` | Remove the local interval choice and follow inherited configuration while retaining unrelated settings. |
| `proxy healthcheck url set group\|provider\|subscription NAME URL`, `proxy healthcheck url reset group\|provider\|subscription NAME` | Save or restore a persistent HTTP(S) latency test URL. |
| `proxy node list`, `show`, `groups show` | Inspect top-level node metadata and effective group membership offline. |
| `proxy node add tailscale`, `edit`, `remove`, `groups set`, `groups clear` | Change node configuration or explicit group memberships atomically. See [proxy nodes and chains](#proxy-nodes-and-chains). |
| `proxy chain list`, `show`, `candidates`, `set`, `clear` | Inspect or configure ordered node/provider/subscription chains, with dry-run plans. |
| `proxy test group GROUP NODE`, `proxy test provider PROVIDER NODE` | Measure an exact node using its group or provider test conditions. |
| `proxy delay NODE [URL] [TIMEOUT_MS]` | Measure a proxy's HTTP request latency through mihomo with advanced explicit target options. |
| `proxy delays GROUP [URL] [TIMEOUT_MS]` | Test all nodes in a group and sort successful measurements by latency. |
| `proxy restore` | Replay saved choices for the selected profile where the group and node still exist; report missing choices. |
| `connections list` | Return a snapshot of active connections, including mihomo's selected proxy chains when supplied. |
| `connections show ID` | Return one active connection by ID, including metadata and proxy chain. |
| `connections close ID` | Request closure of a connection by its ID. |
| `connections close-all` | Request closure of all current connections. |
| `mode [rule\|global\|direct\|save MODE\|saved]` | Read or change temporary runtime mode, or explicitly persist/read the selected profile's mode. |
| `service install`, `service uninstall` | Manage a per-user launchd Agent or systemd service. |
| `privileged-helper status\|install\|uninstall\|enable\|disable` | Inspect or manage the macOS privileged TUN helper and selected engine backend. |
| `app status\|install [TAG]\|update [TAG]\|uninstall` | Inspect, install, or remove the macOS App; App update updates the whole managed installation. |
| `update [TAG\|--terminal\|--app]` | Update the managed CLI, mihomo, and any installed App; macOS `--terminal` opens Terminal, while `--app` schedules a detached update for the menu App. |
| `uninstall` | Stop Nagi and remove its managed executables and services, preserving user data. |
| `version` | Report Nagi version, mihomo version and pinned commit, OS, and architecture. |
| `completion bash\|zsh\|fish` | Print a shell completion script without reading runtime configuration. |

Profile and subscription names consist of ASCII letters, digits, `_`, or `-` and have a maximum length of 128 characters. A profile file is named `profiles/NAME.yaml`. `subscription update` accepts HTTP 200 responses only. Nagi validates a mihomo YAML document with `proxies` or `proxy-providers`, or a Base64/plain V2Ray-style link list supported by mihomo (including SS, SSR, VMess, VLESS, Trojan, Hysteria, TUIC, SOCKS, HTTP, AnyTLS, and Mieru), before replacing the cache. A YAML document without `proxy-groups` gets `Subscription Auto` (`url-test`), `Subscription Fallback`, `Subscription Balance` (`load-balance`), and a `Subscription` manual select group. The manual group defaults to Auto and includes the other strategies, top-level nodes, and `DIRECT`. Generated strategy groups use top-level nodes and all proxy providers; the manual group also includes provider nodes directly. The Auto group tests `https://cp.cloudflare.com/generate_204` every 300 seconds with zero tolerance. Generated names that conflict with top-level nodes, proxy provider names, or known inline-provider node names receive deterministic numeric suffixes. When a top-level node is named `GLOBAL`, Nagi gives that node the first available `GLOBAL-N` name (starting at `GLOBAL-2`) so it remains selectable and the required `GLOBAL` group can be generated. In this generated-group case, Nagi updates downloaded proxy targets in rules and sub-rules, node and inline-provider node `dialer-proxy` references, proxy-provider download `proxy` and `dialer-proxy` references, provider-override `dialer-proxy` references, rule-provider download `proxy` references, and DNS upstream proxy bindings that refer to that node. `SUB-RULE` targets remain sub-rule names, even when named `GLOBAL`. A generated `GLOBAL` group selects `Subscription`. If `rules` is absent, Nagi adds a `MATCH` rule targeting the generated manual group, the first existing group, or `DIRECT` when an existing group list is empty. Existing groups and rules retain their order and definitions. Unsupported or malformed responses leave the previous cache and refresh metadata intact.

`subscription add NAME URL` defaults to hourly updates. `--schedule SECONDS|off` and `--test-url URL` save optional settings with the new record before any download or apply; their order is interchangeable. `off` disables scheduled downloads and automatic apply. The test URL follows the saved subscription when it is applied to a profile. The terminal menu and macOS App collect these options during Add and Apply. Creation does not fetch or activate a profile; their Add and Apply flows perform those later steps explicitly.

`subscription key NAME FILE` accepts a readable, nonempty regular file of at most 1 MiB and copies it into a mode `0600` file in `config/subscription-keys` (mode `0700`). The source file path and contents are omitted from output. `subscription key NAME clear` removes the copy. A response beginning with the age armor header is passed via standard input to the matching installed mihomo (`NAGI_MIHOMO_BIN` or the sibling executable), using `--decrypt-config - --age-identity-file` with the private copy. The decrypted result is limited to 8 MiB before ordinary subscription parsing. A missing or wrong key, decryption failure, oversized plaintext, or invalid plaintext leaves the previous cache and refresh metadata intact. Plaintext responses do not require a key. Key changes and refresh do not activate a profile.

`subscription update` identifies itself as a Clash Meta/mihomo client and advertises YAML support so servers such as XBoard can return their complete mihomo configuration. It reports added, removed, and changed top-level nodes compared with the preceding cache; `subscription preview` compares the normalized cache with the existing profile. A changed node keeps its name but has a different definition. The preview's node change lists are sorted by name. `changes` reports every changed configuration field, including proxy order and settings such as `sniffer`, with redacted paths, change kinds, and behavior hints. Known field names appear in paths; other field names use numbered `field-N` placeholders. Parent and child paths may both appear for one change. It contains no configuration values or user-defined names. `remote_nodes_compared` is false when either side uses proxy providers because their remote nodes are not loaded for preview. On `subscription apply`, the downloaded configuration replaces the named source profile, even if its old content cannot be parsed. Local runtime settings and node/group changes are rebased against the previous subscription source and retained. For existing subscription groups, locally edited `proxies` and `use` lists retain membership additions and removals relative to the previous source. Inherited members follow the new subscription, including deletions and new entries, in source order; local additions follow in saved order without duplicates. Reordering an inherited member alone does not override subscription order. Other list fields keep their explicit replacement semantics. For a node removed from the new subscription, an empty local `dialer-proxy` does not count as a change when the previous source omitted that field; the node is removed unless it has another local change. An empty value still counts as a local change when the previous source specified a nonempty `dialer-proxy`. If that previous source cannot be parsed while a list override exists, application fails without changing the source or override. A persistent subscription chain policy remains effective; see [proxy nodes and chains](#proxy-nodes-and-chains). A failed activation restores the old source, override, selected profile, and saved choices. When the running activation changed, Nagi attempts to reactivate a validated prior effective configuration and confirms its new activation ID. If that confirmation fails, the error states that runtime state is uncertain. Proxy and group order follows the subscription. Nagi removes downloaded external-controller settings, disables LAN access, binds built-in and independent `listeners`, `tunnels`, `tuic-server.listen`, and `dns.listen` to loopback while retaining ports and tunnel targets. File-defined `ss-config` and `vmess-config` inbounds are rejected because their bind addresses cannot be safely rewritten. Unsupported address or protocol forms fail with a field-specific error, and sets `profile.store-selected` to `false` so mihomo does not replay cached choices. If neither an HTTP nor a mixed port is supplied, it adds mixed port `17890`. Custom rules stored separately still apply. An unparseable old profile prevents `subscription preview` but does not prevent `subscription apply`; the apply result lists downloaded nodes as added because a comparison is unavailable.

The optional `Subscription-Userinfo` response header supplies `upload`, `download`, `total` (nonnegative bytes) and `expire` (Unix seconds). Available values appear in `subscription list` and the update result. The URL remains hidden from list output. A missing header clears previously stored traffic and expiry values on a successful update.

Text `subscription list` displays available update, expiry, and next-due timestamps as `YYYY-MM-DD HH:mm` followed by the timestamp's zone abbreviation, without seconds. JSON timestamps retain their RFC 3339 format.

## Terminal menu

`nagi menu` opens a line-oriented menu in the invocation's resolved language and preserves terminal scrollback. The home page shows mihomo state and the current profile, followed by these fixed entries:

| Number | Entry | Contents |
| --- | --- | --- |
| 1 | Service | Start mihomo, Stop mihomo, Restart mihomo, Show full status, Quit Nagi for this login session, Start at Login, Startup Recovery, and Privileged helper (macOS only), in that order. |
| 2 | Network Access | System Proxy (macOS only), TUN, LAN Access, and Listener Ports. |
| 3 | Proxies | Proxy Groups, Node Management, Node Chains, and Proxy Providers, in that order. Groups provide node selection, latency tests, and saved-selection restore; node and chain configuration also work offline. |
| 4 | Routing & DNS | Proxy Mode, Routing Rules, and DNS. Proxy Mode offers For this session or Save for future starts, each with Rule, Global, and Direct. |
| 5 | Profiles & Subscriptions | Profiles, Subscriptions, and Current Configuration. Profile details include selection, export, Backup & Restore, Local Overrides, and removal; subscription details include Update and Apply, Automatic Updates, Automatic latency checks, Encryption Settings, and removal. |
| 6 | Rewrite & Capture | Request Rewrite, Request Capture, and Certificate Authority. |
| 7 | Monitoring & Diagnostics | Connections, Traffic, Logs, Basic checks, Detailed diagnostics, and Export diagnostics, in that order. |
| 8 | Maintenance | Show version, Update Nagi, macOS App installation/removal, and Nagi uninstall variants. |
| 0 | Exit menu | Leaves mihomo running. |

Monitoring & Diagnostics groups connection inspection and closure with separate Traffic and Logs pages and read-only checks. Connections offers the existing list, show, close, and close-all actions plus **Live connections**. This dedicated live view is a keyboard-driven exception to the numbered menu: `/text` searches, `f` cycles All/Active/Recently ended, `s` switches target/traffic sorting, `p` freezes or resumes the displayed snapshot, `n`/`b` page through rows, `d ID` opens the last observed details, `x` returns from details, and `c ID` requests closure for an active connection. `q` or Ctrl-C exits the live view and returns to Connections, including while details are open; Ctrl-D exits the menu. Press Enter after a command. Shortcuts are grouped across lines, and the table fits the actual terminal width and height. The live view uses a separate terminal screen, updates changed lines in place (at most twice per second for incoming samples), and restores the previous screen on exit. `NO_COLOR` does not disable cursor positioning. With `TERM=dumb`, this action shows an ordinary connection snapshot instead. The live view samples through `connections watch` at its default 250 ms interval; connections that start and end between samples can be missed. Recently ended observations remain in memory for five minutes, subject to 1000-entry and 32 MiB limits. Freeze holds the visible rows, counts, sample time, and detail snapshot while collection continues. To copy, enter `p`, press Enter, then select text normally with the mouse. Incoming samples produce no screen updates while frozen; keyboard commands and resizing can still redraw the view. A failed sample does not mark connections ended. Traffic offers Watch traffic; Logs offers Show recent logs and Follow logs. Startup Recovery under Service contains Repair startup state and Clean stale runtime records. These dispatch `startup check` and `recover` respectively; existing diagnostic and recovery behavior is unchanged.

Profiles keeps Import a profile after the visible profile resources, including on every paginated page and when the list is empty or unreadable. Current Configuration is a sibling of Profiles and Subscriptions. It applies to the selected profile and retains show, validate, and reload in that order; it is not an action on the profile whose detail page was last opened. Subscriptions offers Add and Apply with a download interval and optional test URL; Local Overrides retains applying or restoring default runtime settings.

System Proxy, TUN, and LAN Access each list Enable, Disable, then their details action. Both explicit enable and disable choices remain available; these are not state-dependent toggles. Network Access retains its summaries, platform-specific pages, and Listener Ports. Existing permissions, persistence, activation, and restoration behavior are unchanged.

Network Access and its settings pages summarize switches as `On` or `Off`, for example `TUN: On` and `LAN Access: Off`. Listener summaries list only nonzero ports (`Listener Ports: Mixed 17890`); all three known ports being zero displays `None`. Missing, null, or malformed state values display `unknown` rather than implying `Off` or a disabled listener. LAN summaries include the bind address only when access is enabled. Explicit detail actions retain the complete result, including disabled ports and bind addresses. These summaries report configured state, not verified traffic interception or connectivity.

DNS summaries show separate DNS, Upstream Routing, and TUN lines. Servers shows only Upstream Routing; TUN DNS Interception shows the CLI's `tun_enabled` state as `TUN: On` or `Off`, without claiming that DNS capture has been verified. The macOS Privileged helper summary reads installation and activity from `service.installed` and `service.active`, and the engine backend from `selected_backend`; each missing or invalid value remains independently unknown. These menu labels and states are localized in English and Simplified Chinese.

Maintenance lists Show version, then Update Nagi, followed on macOS by Install menu bar App and Uninstall menu bar App, then Uninstall Nagi and Uninstall Nagi and clear user data. Version dispatches `version`; update dispatches `update`; App actions dispatch `app install` and `app uninstall`; removal dispatches `uninstall` (preserve user data) or `uninstall --purge-data` (remove user data). The separate Privileged helper page under Service retains macOS runtime-backend selection and helper management.

Menu effects are brief and appear beside relevant actions. Node Management displays selected-profile top-level nodes with details and group membership; Node Chains provides chain creation and disconnection. Only Tailscale has protocol add/edit/remove forms.

Node Chains lists existing complete node paths and a fixed **New chain** action, or **No chains yet** when empty. A path that is a prefix of another node path is omitted; paths sharing predecessors remain separate. Provider and subscription references do not suppress node entries. The menu does not create named line records. Provider and subscription scope and batch operations remain available through ordinary CLI commands.

**New chain** starts an empty draft. Numeric choices toggle nodes in the CLI's [source-preserving order](#order-scope-and-plans), using the existing green selection style. The header shows `This device → first node → next node → final node → Destination`. Selection order determines traffic order; deselecting removes a node, and reselecting appends it. The final selected node is the exit; at least two nodes are required. **Reset selection** clears only the draft. Apply maps the final node to the CLI target and predecessors to ordered `--via` arguments.

Selecting an existing path opens its full-path detail with **Disconnect chain** and Back. To change a path in the menu, disconnect it and create a new chain; ordinary CLI `proxy chain set` remains available for direct changes. Disconnect clears only that exit's incoming edge, retaining upstream segments and nodes, with no second confirmation. Successful apply or disconnect returns to the refreshed list; disconnect success feedback is only `Chain disconnected.` (`串联已断开` in Simplified Chinese). An upstream path becomes visible when no longer a prefix of another node path.

Read-only plans show conflicts inline. Group membership and indirect `shared_use` references are not presented as actual path changes. Structured `path_changes` supply before/after paths for other affected configured node/provider chains, deduplicated and available through a count and paginated **View changed paths** action. Apply requests a fresh wrapped plan, refuses conflicts, and submits once. Failed submissions retain the draft for explicit retry; failed inventory reads hide stale choices and mutation actions. Cancellation discards unapplied choices. Saving and confirmed reload do not claim login or connectivity. Ordinary CLI output and exit codes remain unchanged.

Terminal styling for human-readable CLI output and the menu enables automatically only when output is a terminal, `TERM` is nonempty and is not `dumb`, and `NO_COLOR` is absent or empty. A nonempty `NO_COLOR` disables color and dimming. Piped output and JSON remain unstyled. Only the home title `Nagi menu` uses bold styling. Input and output must still be terminals to open the menu.

| Text | Style when enabled |
| --- | --- |
| Home title `Nagi menu` | ANSI bold bright cyan |
| Other page titles | ANSI standard yellow (amber) |
| Choice numbers | Terminal default, without added styling |
| Menu, resource, action, form, and pagination labels | ANSI bright blue |
| Selected node or profile label | Green, retaining the `(selected)` text marker |
| State values in CLI summaries, menu operation results, and page context (including profile names, TUN `yes`/`no`, lifecycle and availability, DNS modes, and service states) | Green for positive and selected profile values, yellow for warning or unknown, red for error, bright blue for neutral mode/state values; field labels such as `enabled:` stay terminal default |
| Page count | Terminal default, without added styling |
| Successful completion feedback | Green |
| Warnings, partial completion, and input validation | Yellow |
| Command and read errors | Red |
| Action effects beside entries, including their separator | Dimmed terminal foreground |
| Back, Cancel, Exit menu, and input prompts | Terminal default, without added styling |

Selected styling reflects the actual current node or profile; color is never the only indication of selection or failure. Styles reset immediately after each styled span, so plain prompts, typed input, and later output do not inherit them. Menu labels, context, effects, and errors are redacted before styling. Requested configuration, overrides, profile comparisons, recent logs, and live log or traffic streams receive no added styles. State coloring does not change ordinary CLI wording or JSON.

DNS groups operations under Servers, Domain Exceptions, TUN DNS Interception, and Checks & Queries. Routing Rules offers Test Rule, View Rules, Manage Rules and Sets, and one Import rules action. Import collects only a name and local file path or HTTP(S) URL; each rule keeps its own target. Failed imports retain the name and source for explicit editing or retry. Manage Rules and Sets lists named objects in saved order with their type and enabled state, plus Add custom rule and Import rules. Selecting an object opens its details and View rules, Edit rule (single rules), source update actions (imports), a state-dependent Enable/Disable action, and Remove with the affected rule count. Contents are paginated in source order; selecting a line opens its complete raw value. Removal returns to the refreshed list. Import rules also remains available directly under Routing Rules. Start at Login shows one state-dependent action: Enable start at login or Disable start at login. Enabling installs the login service if needed; disabling changes next-login activation while keeping the current monitor and mihomo running. View details shows the complete `startup status` output, including service installation, path, manager, enabled/active state, and engine state. Advanced options contains Remove login startup service, which can stop mihomo but keeps Nagi and its configuration. Recovery operations are in Service → Startup Recovery. The page reads `service.enabled` and `engine.running` from one `startup status` query, for example `Start automatically at login: On | Mihomo: running`. Missing, null, or invalid boolean fields show `unknown` only for the affected state. A failed query shows both states as `unavailable` with the command error or guidance to open View details. If enablement is unknown or unavailable, the toggle is omitted; details and advanced options remain accessible. Shell completion and cross-group node search remain available through ordinary CLI commands only.

Request Rewrite shows selected-profile intent and actual runtime/TUN/service state separately, plus CA/trust, versions and recovery. Enable is offered only with confirmed running TUN and rereads the selected profile before dispatch. Local Scripts and Ordered Rules support CRUD; rules additionally support enable/disable, ordering, matching/behavior, complete v2 rule JSON, all stage operations and explicit destination consent. Rule editing explicitly loads sensitive values through CLI export into an in-memory draft; only **Save rule** persists it. Draft failures retain values, and Back/EOF before Save discards unsaved changes. Rule and operation moves use the selected item’s page numbers (`1..K`, where `K` is that page’s item count, at most ten), matching proxy group position moves. Position `1` means the first item on that page; action/navigation numbers and noncanonical or out-of-range integers are rejected. Lists show only the menu choice numbers. Moving an operation returns to its list. Empty edits keep existing values; **Use root JSON pointer** and **Set empty value** explicitly clear supported fields. Other rewrite forms require numeric Submit after all fields and keep values after failures for explicit retry. File import/export, stored/file validation, sample/file preview, and Recovery are available under Request Rewrite. Certificate Authority is a sibling page under Rewrite & Capture and provides CA status, generation, and public export. Preview explicitly displays sensitive local results without network activity and pauses before return; ordinary pages hide operation values. macOS exposes trust install/remove, while Linux gives manual distribution/application certificate-store instructions. Child commands remain bound to their captured profile through `--profile`; CA and runtime commands use their defined selected-profile interface. See the [request rewrite contract](#tun-request-rewrite) for exact requirements and [menu design](../design/terminal-menu.md#request-rewrite-flow) for draft and failure handling.

All selectable entries use numbers. Resources show at most ten per page, followed by actions and numeric Next page/Previous page entries, with consecutive numbering across the page. Read the displayed numbers on each page: action and pagination numbers depend on the number of visible resources. `0` returns from a subpage, cancels a resource selector or failed form, and exits at home. Entering `q` at any numeric choice exits the menu with status `0`; no hint is displayed. There are no other letter shortcuts, manual refresh, filtering, menu search, or page-help entries.

State reloads on entry, after an operation, and on return from a child page. The Network Access page loads its independent TUN, listener, LAN, and macOS system proxy summaries concurrently, then displays them in the same order; each failed read retains its own unknown/error summary. Pagination uses the existing snapshot without another read. The page index survives reload and clamps to the last available page. Blank input repeats only the prompt; invalid input asks for a listed number without reprinting the page. Failed reads clear stale resource choices and display unknown/unavailable state, preserving available recovery actions.

Proxy Groups always shows Create proxy group, Manage proxy groups, and Restore saved selections as choices 1, 2, and 3. The group list is inside Manage proxy groups, including while stopped. The entry page reads live groups only to check runtime availability; when that read fails, it also reads `status` and directs recovery through Service. A stopped engine requires `start` before live node selection is available and shows stopped-state guidance without the generic read-error line. An unexpected exit prompts log inspection, `recover`, and then `start`. If the process is still running, the page advises checking `logs` and `status` before deciding whether to restart. Failed or incomplete status reads leave the process state unknown and preserve the group-read error. Opening the page never automatically starts or restarts mihomo. Reopening Proxies → Proxy Groups after using Service rechecks runtime availability.

Actions execute once on selection without a second confirmation. Effects not already clear from the action name, such as configuration replacement, clearing overrides or saved selections, and stopping mihomo, appear beside entries. Six numeric selections from home switch a node: Proxies, Proxy Groups, Manage proxy groups, group, Select node, node (before any pagination). The node list stays open after selection. Node lists show the latest engine latency, failed, untested, or unknown state without starting a test. Latency results remain directly selectable and retain batch ordering across selections; current engine measurements take precedence over the page's older results. Removed nodes lose their page results. Subscription, Provider, and group details offer **Automatic latency checks**. The page summarizes the selected profile and returned check settings, including their source. Page state and success feedback do not show the full saved URL; endpoint state shows only scheme and host. The terminal may echo a URL while the user enters it. Missing or defaulted interval data stays qualified, and the menu does not infer a fixed period from an omitted or zero interval. A group's `enabled` field does not mean all automatic checks are disabled. Subscription summaries distinguish an unapplied subscription and omit per-checker enablement and scheduling details. Group and Provider summaries label the returned checker state as checks On/Off and lazy scheduling as skip when unused, without cross-scope explanatory notes. The four actions are **Set check interval**, **Restore inherited interval**, **Set test URL**, and **Restore inherited test URL**. Subscription restore actions read **Use profile check intervals** and **Use profile test URLs**. Defaulted intervals display **as configured** with their source. Test URL and source appear together; a subscription inheriting profile URLs displays **as configured**, while unavailable endpoint data remains **unknown**. Provider URL reset appears only when the CLI reports that it is available. Forms retain input after validation or command failure for explicit editing and retry; successful changes give brief feedback. Reading the page does not test nodes. Subscription and Provider detail pages also edit their separate download update intervals. Group pages retain manual tests and selection controls. Automatic groups show automatic/fixed state, and group latency tests disclose that they may release a fixed selection. The provider list offers Add provider even when empty or unreadable; its retained form collects an update interval and optional test URL, then calls `proxy provider add`. Provider details offer numeric update/healthcheck actions; their nodes invoke individual latency checks. Membership reloads after each operation.

Text fields accept literal content, including `0`, `b`, and `q`. Terminal text input edits at the end of the value. Backspace (BS or DEL) removes one complete visible character, including Chinese characters, flags, and combined emoji. Ctrl-U clears the input; Ctrl-W removes the previous whitespace-delimited word. Long input scrolls horizontally within the prompt without truncating the submitted value. The menu redraws the prompt using terminal cursor controls and restores the previous terminal settings after each read. Ctrl-C cancels a form and returns to its owning page; at a menu choice it returns to the parent or exits at home. Required or invalid fields are prompted again without losing other values. After a failed submission, numbered entries edit fields or submit again, and `0` cancels. Failed mutations never retry automatically. EOF exits. Already submitted ordinary operations may finish; Ctrl-C does not promise cancellation or rollback.

Local Overrides rereads default runtime settings state on entry and after operations, updating Apply/Disable, the selection marker, and the DNS group explanation. If state cannot be read, the toggle is hidden while Show, Set, and Clear remain available. The toggle checks state again before submitting; a change since rendering prompts a fresh choice without applying or restoring settings.

Add and Apply subscription collects a name, source URL, update interval, and optional test URL; it saves the new record and settings, downloads its cache, and applies it in sequence, then opens its detail page. Existing subscriptions offer Update and Apply. A failed download prevents apply and retains a newly added record; a failed apply retains the new cache. Later-stage failures never resubmit add. For encrypted feeds, use Encryption Settings to set an identity file, then retry Update and Apply. Automatic Updates shows the configured download interval or disabled state; Disable automatic updates appears only while enabled. Encryption Settings shows whether an identity file is configured, offers Set or Replace identity file, and shows Clear encryption key only when configured. Both pages reread subscription metadata on entry and after operations; failed reads or missing records show unavailable state and retain only the setting action. Identity contents and the stored identity path are never shown. Applying updates the source configuration, retains local settings and node/group changes and any persistent subscription chain policy, and clears saved node selections; the entry discloses these effects. The flow does not start mihomo or enable traffic access. Details show available update, quota, and expiry metadata without automatic previews or change counts. Ordinary `subscription preview`, `update`, and `apply` remain available for advanced use. Existing command validation and activation remain authoritative. Successful removal returns to the parent list; failed removal stays in detail.

Subscription timestamps in the list and details use the local time zone and `YYYY-MM-DD HH:mm`, omitting seconds and the zone suffix. Missing or invalid timestamps display `unknown`; a missing update timestamp displays `never downloaded` in the list and details. Quota remains visible as available/total and uploaded/downloaded amounts, using binary units (`B`, `KiB`, `MiB`, `GiB`, `TiB`, `PiB`, `EiB`); values above bytes have up to two decimal places. Available quota is clamped to zero, and an absent or zero total displays `unknown`. These amounts are the provider's latest `Subscription-Userinfo` snapshot, not live traffic measurements. Ordinary command output, JSON timestamps and byte fields, and stored metadata are unchanged.

Success and failure results appear after the reloaded page and before its next input prompt. The menu does not echo internal command arguments. Pages show current state and concise recovery guidance; DNS and diagnostic check limitations are documented in their reference sections rather than repeated on menu pages. Configuration, local override, profile comparison, and recent log output pause with `Press Enter to return` before the page reappears. Requested configuration and logs retain their raw content. Errors, lists, and context retain useful URL details while the formatter removes URL userinfo and common credential query values. Failures preserve the underlying reason and known state; a failed action does not determine the menu process's exit status.

Ctrl-C during `logs follow` or `traffic watch` cancels the child stream and returns to its page while user-mode mihomo keeps running. `q`, `0` at home, EOF, or Ctrl-C at home exits `0` without stopping mihomo. An unrecoverable menu input error exits `1`.

Both standard input and standard output must be terminals. Redirected input/output or `--json` produces a `usage` error on stderr and exits `2`. `nagi menu --help` works without a terminal and exits `0`. Automation should invoke ordinary commands with `--json`.

## Diagnostics

Profile validation consumes mihomo’s structured `--check-json` diagnostics for all profiles, including those containing Tailscale nodes. Errors retain the failing field path, code, and cause; an unrelated Tailscale node does not suppress rule errors. Without a structured diagnostic, the CLI gives a local validation command using `FILE` and `DIRECTORY` placeholders. Raw validation stdout/stderr and temporary configuration files are not included in ordinary errors.

`doctor` inspects directory availability and owner read/search permission bits, the mihomo executable (including `NAGI_MIHOMO_BIN` resolved through `PATH`), selected-profile readability, PID liveness, and the Unix Socket `/version` endpoint. It does not create directories, change files, start the engine, validate YAML with mihomo, or test proxy connectivity. A live PID does not establish process identity. The API check has a two-second deadline.

The result is `{"healthy":true,"checks":[{"name":"...","status":"ok","message":"..."}]}` inside the success envelope. `status` is `ok`, `warning`, or `error`; `healthy` is false if any check has status `error`. Check names are `config_directory`, `data_directory`, `state_directory`, `runtime_directory`, `mihomo_executable`, `selected_profile`, `process`, and `control_api`. Missing first-use directories, a missing default profile, and a stopped engine can be warnings. A missing executable, invalid selection, missing selected non-default profile, or stale PID is an error.

A completed diagnostic report exits with status `0`, even when `healthy` is false. Scripts must inspect `data.healthy` and the individual checks. Failure to construct the report uses the normal error envelope and exit status `1`.

## Profile removal

`profile remove NAME` refuses to delete the selected profile, a nonregular file, or any profile when the current selection cannot be read. Select another profile first with `profile use NAME`. Missing profiles fail with `profile_error`. Removal does not delete a subscription with the same name or its cache. Removal deletes `rules/NAME.yaml`, `profiles/NAME.yaml.bak`, `overrides/NAME.yaml`, its `.bak` file, any persistent subscription chain policy, the local default-settings restoration record, the generated `profiles/.effective-NAME.yaml` configuration, and saved proxy choices for that profile. It clears subscription associations with the removed profile while keeping subscription records and caches. Cleanup and association changes run in the configuration transaction; a failed operation restores the prior state.

## Local profile files and overrides

`profile export` reads the source profile, including any secrets it contains. It creates `FILE` with mode `0600` and never overwrites an existing path. `profile backup` explicitly refreshes the single `.bak` recovery copy. `profile restore` validates that copy before replacing the source profile; a successful restore swaps the source and backup contents. If validation or a running engine reload fails, the source and original backup remain available. `profile diff` compares source YAML lines and parses both files for structural changes. JSON retains `name`, `other`, `changed` (boolean), and `diff` (string), and adds `changes` and `warnings`. `changes` contains YAML pointer paths with `added`, `removed`, `modified`, `type_change`, or `order_changed` kinds, plus applicable `before` and `after` values. Proxies and groups with unique names use their escaped names in paths; their order is reported separately. Warnings flag rule changes, DNS policy changes, removed proxies or groups, YAML type changes, and exposure setting changes. If either file cannot be parsed as YAML or a structural value cannot be represented in JSON, the line diff remains available, `changes` is empty, and `warnings` explains the failure. The line diff and structural values can contain credentials; do not publish their output without review.

Overrides live in `config/overrides/NAME.yaml`, separate from `config/profiles/NAME.yaml` and subscription cache. The override must be a YAML mapping; its keys replace corresponding source keys, and nested mappings merge recursively except DNS `nameserver-policy` and `proxy-server-nameserver-policy`, which replace their source maps. Lists, including `rules` and DNS server lists, are replaced as a whole. `profile override set` validates the merged configuration with mihomo and reloads it when selected and running. The merged file is generated as `config/profiles/.effective-NAME.yaml` for mihomo, keeping relative file paths anchored in the profiles directory; `profile export`, `config show`, and backups continue to use the source YAML. Local proxy additions use root-level `nagi-proxy-additions`, a list of complete named proxy node mappings merged into the effective node list; this marker is removed before mihomo validation. Matching source definitions can be replaced by retained local definitions; conflicting node, group, and provider identities are refused. A DNS override may contain root-level `nagi-dns-replace: true` and `nagi-dns-proxy-group: GROUP` metadata visible through `profile override show`; Nagi uses these markers when preparing the effective DNS configuration and removes them before mihomo validation or activation. `profile override clear` removes ordinary local overrides and additive local proxy nodes. Effective configuration still includes separately stored custom rules and any persistent subscription chain policy. Use `proxy chain clear subscription NAME` to remove that policy. Removing a profile also deletes its override and related state; see [profile removal](#profile-removal). Overrides can contain credentials and are stored with private permissions.

`profile override apply-default NAME` copies safe local runtime settings from `config/profiles/default.yaml` into `NAME`'s local override. It excludes proxies, proxy providers, proxy groups, rules, rule providers, controller settings, and credentials from the template. Existing proxy, group, and business-rule fields in the target's source and local override are retained, including additive local proxy nodes. The separate persistent subscription chain policy remains active. A target with proxies must specify `--dns-proxy GROUP` on first apply. Nagi records that binding in the override and reuses it on later applies when the option is omitted; supplying the option again changes the binding. Only a node-less direct profile can apply the default DNSPod DNS without a bound group. Nagi binds the two primary DoH upstreams to the named group, which must exist in the target's effective profile and have a usable non-`DIRECT` selection when the command runs. An invalid or missing group is rejected before activation. The command does not alter the group's selection or the profile's traffic-routing rules. If a user subsequently selects `DIRECT` in the bound group, mihomo follows that selection for the DNS upstream too. Nagi retains `tun.enable` from the target's effective configuration, whether true or false; other TUN settings come from the default profile. The default profile itself is unchanged. Nagi validates the resulting effective configuration, reloads when `NAME` is selected and mihomo is running, and restores the previous target override if validation or reload fails. It also saves the previous override in a private restore point under `config/overrides/`. Reapplying while the default override remains active retains the original restore point.

`profile override default-status NAME` returns `default_override_applied: true` only while the current override matches the one written by `apply-default`. Its `dns_proxy_required` boolean identifies a target with proxies that needs a binding. `dns_proxy_group` reports the recorded group, or is empty for a node-less profile applied without a binding; `dns_proxy_groups` lists group candidates for the option. Other override edits make the applied status false; applying defaults again after such an edit saves the edited override as the new restore point. `profile override restore-default NAME` restores the saved override byte for byte, or clears the override when it was previously absent; it validates and reloads a selected running profile. It refuses to overwrite an override changed since `apply-default`. A successful restore removes the restore point. Applying or restoring default settings does not remove the separate persistent subscription chain policy. Both commands use the ordinary `profile_error` on failure.

## DNS policy and checks

A DNS leak occurs when a domain lookup is sent outside the resolver or network path intended by the user, exposing the queried domain to an unintended party.

New default profiles enable mihomo DNS, IPv6, fake IP mode, and `dns.resolve-proxy-destinations`. They listen only on `127.0.0.1:1053`. The bootstrap, primary, direct outbound, and proxy-node hostname resolvers use DNSPod DoH at `https://1.12.12.12/dns-query` and `https://120.53.53.53/dns-query`. This gives the node-less `MATCH,DIRECT` profile one consistent direct resolution path. All configured DNS upstreams use HTTPS; no plaintext fallback is configured. These addresses are defaults, not a guarantee of reachability on every network. The installer replaces `default.yaml` on install and update; ordinary DNS inspection does not rewrite profiles. Applying its settings to a profile with a DNS proxy binding instead sets primary DoH to Cloudflare `https://1.1.1.1/dns-query` and Google `https://8.8.8.8/dns-query`, each bound to `#GROUP`. DNSPod remains the direct bootstrap, proxy-node hostname, and direct-outbound resolver. DNS `respect-rules` and `direct-nameserver-follow-policy` are disabled, DNS server policies are empty, and `fallback` is empty. Thus ordinary primary lookups do not inherit the profile's traffic rules or use DNSPod when both primary upstreams fail. Domain-specific exceptions configured later can choose other servers.

New default profiles include TUN settings for `auto-route`, `strict-route`, `auto-detect-interface`, and UDP/TCP port 53 hijack (`any:53` and `tcp://any:53`), but set `tun.enable` to `false`. Run `nagi tun enable` to activate TUN; this requires OS TUN permissions and working routes. If mihomo cannot activate TUN, inspect `nagi logs` and `nagi tun status`; configuration validation alone does not establish DNS interception. `system-proxy enable` changes only the OS HTTP/HTTPS proxy settings and does not replace TUN DNS interception.

`dns set` writes DNS settings into the selected profile's separate local override, preserving them across source imports. Explicit and automatic subscription application retain that override. If its bound proxy target disappears, application fails and retains the prior configuration. Primary URLs must use HTTPS, include a path, and contain no credentials, query, or fragment. Multiple URLs are given to mihomo as primary upstreams. If all are unavailable, resolution fails; Nagi does not configure a plaintext or system DNS fallback. Direct mode binds the primary DoH servers to mihomo's `#DIRECT`; proxy mode appends mihomo's `#NAME` binding to each primary upstream and requires `NAME` to identify a non-direct top-level proxy node or a proxy group in the effective profile. Automatic groups are accepted. If that target disappears, activation fails rather than silently using a network interface as fallback. Provider-only nodes are not accepted for this binding unless reached through a configured group. Bootstrap and proxy-node hostname resolution use DNSPod DoH at `https://1.12.12.12/dns-query` and `https://120.53.53.53/dns-query` by default, each explicitly bound to `#DIRECT`. `--bootstrap` replaces both with one or more directly reachable HTTPS DoH URLs whose hosts are IP addresses, avoiding a hostname lookup cycle. In proxy mode, direct outbound host resolution uses the same direct bootstrap list; in direct mode it uses the selected primary list, also bound to `#DIRECT`. The DNS override enables both root and DNS IPv6 settings, clears source fallback and source DNS server policies, and retains existing local domain exceptions. Choose bootstrap addresses reachable from the current network; if they are blocked, proxy-node hostnames cannot resolve even when the proxied primary DoH server is reachable.

`dns set` also enables mihomo's `dns.resolve-proxy-destinations` policy. For proxied TCP connections, mihomo resolves destination hostnames with its configured real resolver and sends the resulting IP to the proxy protocol. The original hostname remains available for group selection, routing, and reporting. DNS failure fails the connection without falling back to proxy-server resolution. Direct outbound resolution and internal DNS transports retain their existing behavior; UDP behavior is unchanged. This option is disabled by default in mihomo and requires the engine commit pinned by Nagi. It cannot be enabled together with `preserve-proxy-hostname: true`; configuration validation reports the conflict. `dns status` and `dns check` include `resolve_proxy_destinations` (boolean), describing the selected persisted configuration.

`dns exception add` requires a prior `dns set` for that profile. `DOMAIN` is an ASCII domain suffix such as `lan` or `corp.example`; Nagi stores it as `+.lan` or `+.corp.example` in mihomo's `nameserver-policy`. `SERVER` can be an HTTPS DoH URL or an explicit `udp://IP:53` or `tcp://IP:53` server. A plaintext exception sends matching DNS questions to that server without encryption; `dns status` and `dns check` report it. `dns exception remove` removes that local mapping. Local DNS policies replace source DNS policies as a whole to avoid retaining an unnoticed plaintext source resolver.

`dns tun on` configures mihomo TUN with `auto-route`, `strict-route`, interface auto-detection, and UDP/TCP port 53 hijack (`any:53` and `tcp://any:53`). It requires an encrypted primary and bootstrap DNS configuration. The engine and OS may require additional privileges or platform support to activate TUN; configuration validation alone does not establish that traffic is intercepted. `dns tun off` disables the selected profile's TUN override. On macOS, activating a running TUN also manages network-service DNS as described under [traffic access](#traffic-access). Saving the override while stopped does not change OS settings. On Linux, these commands do not change the operating system's DNS server setting. On a platform or network where TUN cannot be activated, applications outside mihomo can still resolve through the system resolver.

`dns query` supports `A` and `AAAA` only and returns mihomo's `/dns/query` response. `dns flush` calls `/cache/dns/flush`. Both require a running control API. `dns check` inspects the selected effective configuration and makes one `example.com` A query through mihomo. Its scope is configuration and mihomo API state; it does not inspect operating-system routes, resolver capture, or external DNS leaks. `dns status` is configuration inspection and does not prove running TUN state. Neither command prints full DoH URL paths or credentials; inspect `profile override show NAME` for exact local settings.

## Proxy latency, connections, and mode

### Adding proxy providers

`proxy provider add NAME URL GROUP [SECONDS|off] [--test-url URL]` adds an HTTP proxy provider to the selected profile. The profile must already exist. `NAME` contains 1 through 128 ASCII letters, digits, `_`, or `-`; `COMPATIBLE`, existing effective provider names, and names conflicting with proxy groups are rejected. `URL` must be an absolute HTTP or HTTPS URL without user information or a fragment; query parameters are allowed. `SECONDS` defaults to `3600` and must be an integer from `1` through `31536000`; `off` disables automatic source downloads. The optional test URL follows the same HTTP(S) validation as other persistent test URLs. Invalid input exits `2` before runtime configuration is resolved.

`GROUP` is a nonempty printable name of at most 128 bytes without surrounding whitespace; quote names containing spaces. Existing groups must have configuration type `select`, `url-test`, `fallback`, or `load-balance`. Nagi appends the provider to the group's `use` list while retaining its other settings and members. A missing group creates a `select` group with `DIRECT` as its initial choice and the new provider in `use`. Names conflicting with reserved mihomo proxy names, top-level nodes, or providers are rejected when creating a group. Rules and routing mode remain unchanged: route traffic to the group with existing rules or select it through `GLOBAL` in global mode.

`proxy provider add` accepts `off` in place of `SECONDS` to disable automatic source downloads. Optional `--test-url URL` saves a Provider-specific latency test URL at creation. `proxy provider interval show NAME` reads the configured download interval while stopped; `set NAME SECONDS|off` changes it and `reset NAME` restores the source value when one exists. A Provider created locally has no source interval to inherit, so `reset` is unavailable for it. These commands change download timing, independently of `proxy healthcheck` settings. They read local configuration without requiring a running engine; mutations validate and reload the selected running profile, using the existing rollback transaction. Invalid values exit `2`; missing targets, validation, and activation failures exit `1` with `proxy_provider_error`. `show` and successful mutations return the same Provider interval fields listed under [JSON and exit codes](#json-and-exit-codes).

The provider uses mihomo's automatic cache path, the requested update interval when enabled, and native healthchecks every 300 seconds. For `url-test`, `fallback`, and `load-balance` groups, its healthcheck uses the group's `url` and any configured `expected-status`. When the group omits `url`, it uses the first nonempty healthcheck URL from an already attached provider, matching mihomo's group default. Otherwise the URL defaults to `https://cp.cloudflare.com/generate_204`. Mihomo owns downloading, decoding, and checking its nodes; Nagi does not require a separate connectivity test before saving the configuration.

Nagi merges the provider into `config/overrides/PROFILE.yaml` and stores the full effective `proxy-groups` list there because override lists replace source lists. Existing source content, DNS, rules, nodes, and unrelated override settings are retained. This group snapshot takes precedence over later source imports. Subscription application rebases local group changes against the new source as described under [profile overrides](#local-profile-files-and-overrides). Explicit `profile override clear` or replacement of the override removes these local additions. Source export, backup, and `config show` do not include the override.

The command takes the configuration lock and uses the existing activation transaction. Mihomo validates the effective configuration before activation. A running engine is reloaded; mihomo automatically triggers an asynchronous initial healthcheck when it loads the provider nodes. Provider creation can finish before measurements are available. A stopped engine stays stopped; mihomo performs the initial check when it next loads the provider at startup. On validation or reload failure, Nagi restores disk settings and attempts confirmed runtime recovery when the activation changed; uncertain runtime recovery is reported. Operational failures exit `1` with `proxy_provider_error`. Success omits the subscription URL and reports the affected profile and group; `group_created` identifies a new group. See the [provider guide](../guides/proxy-providers.md) for menu and CLI steps.

### Removing proxy providers

`proxy provider remove NAME` removes a proxy provider from the selected profile, including definitions in the source and local override and every group `use` reference in both layers. `NAME` contains 1 through 128 printable bytes without surrounding whitespace; `COMPATIBLE` is reserved. Invalid arguments exit `2` before resolving runtime configuration. Names need not follow the stricter add-name syntax, so imported names with spaces or Unicode can be removed.

Groups, their remaining members and settings, routing rules, and unrelated override settings are retained. Removing the last provider from a group uses mihomo's configured empty-group behavior; Nagi does not insert a direct member. References to individual provider nodes in other fields are not rewritten; mihomo validation can reject such a removal. Saved node choices that become unavailable remain stored under the existing selection restore contract.

The command works while stopped and does not start the engine. It takes the configuration lock, validates the remaining effective configuration, and reloads a running engine once through the activation transaction. Missing providers, validation, persistence, and activation failures exit `1` with `proxy_provider_error`; failures restore disk settings and attempt confirmed runtime recovery under the existing transaction contract. A source-defined provider is removed from the source profile, so a later subscription replacement can restore it. Such source edits follow the automatic subscription apply source-change pause rules. Source exports include source removals; locally added provider removal leaves the source unchanged. Both menu entry points dispatch the same CLI operation.

### Proxy group management

These commands manage the selected profile's native mihomo `proxy-groups`. They do not change routing rules, routing mode, OS traffic access, or the running/stopped preference.

```sh
nagi proxy group list
nagi proxy group show 'My Group'
nagi proxy group add 'My Group' --type select --node DIRECT --node 'Node A'
nagi proxy group add 'Automatic' --type url-test --provider work-feed --interval 300 --tolerance 50
nagi proxy group edit 'My Group' --node 'Node B' --node DIRECT
nagi proxy group edit 'Automatic' --clear-providers --node 'Node A'
nagi proxy group remove 'My Group'
```

`GROUP` is a printable name of 1..128 bytes without surrounding whitespace. Creation, editing, and removal reject reserved names `DIRECT`, `REJECT`, `REJECT-DROP`, `PASS`, `COMPATIBLE`, and `GLOBAL`; display ordering accepts existing configured group names as described below. Creation rejects existing group, top-level node, and provider names. Renaming is not supported. Reads include groups of other native types, but editing supports only the four types below.

| Option | Definition |
| --- | --- |
| `--type TYPE` | `select` (manual), `url-test` (latency selection), `fallback` (ordered failover), or `load-balance`. Creation defaults to `select`. |
| `--node NAME` | Repeat for explicit top-level nodes, other groups, or built-in outbounds such as `DIRECT`. Replaces the entire `proxies` list in argument order. Self references, missing members, duplicate members, and dependency cycles are rejected. Provider payload nodes are included through their provider. |
| `--provider NAME` | Repeat for existing proxy providers. Replaces the entire `use` list in argument order. Unknown or duplicate names are rejected. |
| `--clear-nodes`, `--clear-providers` | Explicitly clear the corresponding list. Cannot be combined with that list's member option. At least one explicit member/provider or existing native automatic-inclusion setting is required. |
| `--url URL` | Absolute HTTP(S) test URL without userinfo, fragment, or surrounding whitespace. Newly created automatic groups default to `https://cp.cloudflare.com/generate_204`. |
| `--interval SECONDS` | Integer `1..31536000`. Newly created automatic groups default to 300 seconds. Uses mihomo's native healthcheck scheduler. |
| `--tolerance MS` | Integer `0..65535`, only for `url-test`; omitted creation value uses mihomo's zero default. |
| `--strategy STRATEGY` | `consistent-hashing`, `round-robin`, or `sticky-sessions`, only for `load-balance`; omission uses mihomo's `consistent-hashing` default. |

`proxy group order GROUP [GROUP...]` sets the display order for all currently configured groups. Supply every group from `proxy group list` exactly once, in the desired order. Quoting preserves spaces in names. Duplicate/invalid names are usage errors (exit `2`); unknown names or an incomplete/stale list fail with `proxy_group_error` (exit `1`). A generated `GLOBAL` group is not part of this list and continues to follow configured groups; an explicitly configured group named GLOBAL can be ordered like any existing group.

```sh
# Example profile containing exactly Work, Automatic, and Media:
nagi proxy group order 'Work' 'Automatic' 'Media'
```

Ordering stores only the `nagi-group-order` name list in the selected profile's private local override. It creates no group-definition snapshot and retains source profiles, group fields, member/node/provider order, rules, DNS, and saved proxy choices. Nagi applies this preference when composing effective configuration and ordering CLI/menu/App results, removes the metadata before passing configuration to mihomo, and appends groups not mentioned in the preference in their existing order. Missing groups are skipped; their names remain dormant so a later reappearance recovers its preferred position. Subscription updates retain the preference and keep new group definitions editable rather than freezing old definitions. Clearing/replacing the override removes the preference.

Sorting is a display operation: it takes the configuration lock and atomically saves the validated permutation without launching a validator, requiring an installed engine, starting mihomo, or calling its reload API. Running group selections and activation IDs remain unchanged. It returns `{profile, resource: "group", action: "order", group_order: [NAME...], saved: true, reloaded: false}`; here `reloaded: false` applies to both running and stopped engines. A failed save retains the prior override.

The terminal entry is **Proxies → Proxy Groups → Manage proxy groups → Adjust display order**, available when at least two local groups exist. Select a group, then **Move to previous page**, **Move to next page**, or **Move to position**. Previous-page movement is omitted on the first page, and next-page movement is omitted on the last page. Cross-page moves keep the group’s one-based position within the page; if the destination page has fewer groups, the group moves to its last position. The reordered list opens on the destination page. There are no single-step up/down or top/bottom actions. The position prompt uses the numbers displayed on the page where the group was selected: `1..K`, where `K` is that page’s group count (at most ten). For example, position `1` on page 3 moves the group to the first position on page 3. Action and navigation numbers are not valid target positions; malformed or out-of-range values are rejected and prompted again. Move to position stays within the selected page. Cross-page moves shift the intervening groups while preserving their relative order. Ctrl-C cancels the current move without changing the draft. Moves affect one in-memory draft; **Save display order** sends the complete list once and returns to management on success. Back/EOF discards an unsaved draft. Failures preserve the draft for explicit retry; if groups or the selected profile changed externally, reopen sorting to build a current draft.

On creation, omitting both member-list options adds `DIRECT`; explicitly empty lists do not get a silent fallback. On edit, at least one option is required. Omitted fields remain unchanged, including native filters, inclusion settings, hidden/icon metadata, and other advanced settings. Changing type removes an inapplicable `tolerance` or `strategy`. Edits retain compatible saved selections, including a prior fixed choice after changing type. Use `proxy auto GROUP` or **Resume automatic selection** to release a URLTest/Fallback choice and let mihomo select automatically. Omitted URL/interval fields on existing groups continue to use native inheritance/defaults; `show` reports configured values, with an empty URL or zero interval indicating omission rather than a measured runtime value.

Deletion refuses effective references from other groups, node chains, rules/sub-rules, DNS upstreams, providers, NTP, listeners, and tunnels, naming the affected configuration sections. Custom rules and managed-policy composition still undergo effective configuration validation. Remove or redirect references first. Successful deletion removes the saved choice for that group, while retaining its nodes, providers, and routing rules.

Definition mutations store the complete effective group list in `config/overrides/PROFILE.yaml` with private permissions, preserving unrelated override keys and leaving source profiles and subscription caches unchanged. Explicit group URL/interval edits use the same durable per-group metadata as the healthcheck commands, so neither interface silently overrides a newer edit; deletion also clears that group's metadata. Source imports retain this snapshot; subscription application rebases local group intent as described under [profile overrides](#local-profile-files-and-overrides). Clearing/replacing the override removes these changes. The configuration lock and existing activation transaction serialize mutations: mihomo validates the effective configuration, the selected running engine reloads through its configuration API, and failures use the existing file/runtime rollback and uncertain-activation handling. Stopped engines stay stopped. No separate connectivity check is required to save a group; automatic checks are mihomo's configured behavior.

Successful reads/mutations, including ordering, exit `0`; syntax and option-value errors exit `2`. Missing groups, conflicts, invalid effective configurations, and activation failures exit `1` with `proxy_group_error`. `list` returns `{profile, group_configs: [...]}`; `show` returns `{profile, group_config: {...}}`. Each configuration has `name`, `type`, ordered `proxies`/`use` arrays, `url`, `interval`, `tolerance`, and `strategy`. Provider URLs and credentials are not included. Definition mutations return `{profile, name, resource: "group", action: "add"|"edit"|"remove", saved: true, reloaded: BOOLEAN}`. For definition mutations, `reloaded: false` means saved while stopped; start explicitly to activate.

The terminal entry is **Proxies → Proxy Groups → Create proxy group**. **Manage proxy groups** lists local groups even while mihomo is stopped. Selecting a configured group in management opens local settings with **Select node**, editing, live inspection, and deletion. **Select node** opens live node switching and latency controls; **Show group details** there returns to the local settings. Generated `GLOBAL` is not a local management entry and remains accessible through the ordinary CLI. Creation/editing uses an in-memory draft with numeric type/member selection and a final **Save proxy group** action. Toggling members appends newly selected names, preserving failover order; cancellation discards that selector's pending changes. Save failure keeps the draft for explicit editing/retry. Back/EOF before Save does not mutate configuration; successful save/deletion returns to the parent list/detail. Bash, Zsh, and Fish completion include these commands, types, strategies, and local group/node/provider candidates.

## Proxy nodes and chains

These commands read and modify the selected effective profile. Reads and dry-run plans do not require a running engine. Mutations require the matching binary pinned in `engine.lock` to validate the complete candidate. A stopped engine stays stopped; a selected running profile reloads through the configuration transaction. Successful saving or activation does not establish Tailscale login or end-to-end forwarding. These commands do not enable TUN, install a helper, select a backend, or rename nodes.

```text
proxy node list
proxy node show NAME
proxy node add tailscale NAME EXIT_NODE [--group GROUP]...
                 [--hostname HOSTNAME] [--control-url URL]
                 [--auth-key|--auth-key-stdin|--auth-key-file FILE]
proxy node edit NAME [--exit-node VALUE] [--hostname HOSTNAME]
                 [--control-url URL]
                 [--auth-key|--auth-key-stdin|--auth-key-file FILE]
proxy node remove NAME
proxy node groups show NAME
proxy node groups set NAME GROUP [GROUP...]
proxy node groups clear NAME
proxy chain list [node|provider-node|provider|subscription]
proxy chain show node|provider|subscription TARGET
proxy chain candidates node|provider|subscription TARGET [TARGET...]
proxy chain set node|provider|subscription TARGET [TARGET...] --via FIRST [--via NEXT...] [--dry-run]
proxy chain clear node|provider|subscription TARGET [TARGET...] [--dry-run]
proxy chain show|candidates provider-node PROVIDER NODE
proxy chain set provider-node PROVIDER NODE (--via NODE | --via-provider PROVIDER NODE)... [--dry-run]
proxy chain clear provider-node PROVIDER NODE [--dry-run]
```

#### Nodes, keys, and groups

`node list` reports all top-level nodes, local group candidates, and provider names. Nodes belonging to the selected profile's applied subscription come first, in the source YAML `proxies` order; effective local edits retain that source position. Subscription ownership requires a persisted profile association whose source hash matches the profile source, so download-only updates do not change this order and stale associations do not classify nodes as subscription nodes. Other nodes follow in ascending type order, then case-sensitive name order. `provider_candidates` preserves source YAML declaration order, followed by providers added in local override declaration order; effective providers without a declaration position follow in name order. Provider payload nodes remain outside the `nodes` array and are read through `proxy provider show NAME`. Node details show name, type, managed-policy membership, and group classification; Tailscale details additionally show exit, hostname, normalized control URL, and key presence. Adding and editing protocol settings supports Tailscale only. Removing a referenced node is refused, including effective group, provider, node, DNS, and managed-policy dependencies.

`NAME` is printable text of 1 through 128 bytes without surrounding whitespace; reserved mihomo names and collisions with nodes, groups, or providers are refused. Quote names with spaces. `EXIT_NODE` and hostname values are nonempty printable text of at most 253 bytes. The default control service is `https://controlplane.tailscale.com`. A custom control URL must be HTTPS without user information, query, or fragment; an empty URL restores the official service. `edit` requires at least one option. Duplicate or unknown options are rejected, except repeated distinct `--group` during add.

Without a key option, add prompts for visible terminal paste. `--auth-key` explicitly prompts, takes no value, and is usable only with terminal input without `--json`. Editing with a blank prompt retains the existing key; omitting key options also retains it. `--auth-key-file FILE` accepts a regular nonsymlink file. `--auth-key-stdin` reads bounded stdin through EOF, including for JSON and pipelines. Choose one key method. Input is limited to 16,384 bytes including surrounding whitespace, which is trimmed; the resulting value must be nonempty and printable. Empty stdin fails. Input and cancellation are handled before configuration locks, and keys never become command arguments. Ordinary errors and results omit key material and input file paths. Explicit raw configuration, override, and log viewing/export may contain credentials.

Repeated `--group GROUP` assigns a new node to several explicit group lists atomically with node creation. `groups set` replaces the entire explicit membership set; `groups clear` removes only explicit memberships. Reports distinguish `explicit`, `automatic`, `excluded`, and `available` groups from effective group inclusion and filters. Each group array and `node list`'s `group_candidates` follow the effective profile's `proxy-groups` order, including the terminal menu's group membership selector. Automatic inclusion remains controlled by the group's own configuration. Changing memberships does not select a node or change routing rules.

#### Order, scope, and plans

`chain list` and `chain candidates` display applied subscription nodes first, in source order with the ownership checks of `node list`, followed by provider nodes in provider declaration and payload order, then other top-level nodes in case-sensitive name order. Provider and whole-subscription targets remain CLI scopes; the terminal menu supports top-level and provider-node paths. Filtering preserves this order. These display orders do not change traffic order.

Provider-node identity is the pair of source provider and effective node name, so equal names in different sources remain distinct. Local inline payloads, file payloads, and HTTP caches supply candidates even while stopped. While the privileged backend is running, the authenticated helper reads the selected provider in its confirmed active root snapshot and returns only raw node names and protocols; the CLI applies source filters and name overrides, then retains nodes present in the current mihomo provider list in source order. This requires the updated CLI and helper together. The complete provider-definition fingerprint must match the selected profile; different profiles, changed source definitions, uncertain activations, unsafe files, and private payloads larger than 8 MiB are unavailable. Stopped inspection uses only user-readable local evidence, so root-only caches remain unavailable. Inspection and dry-run never download a source or create a cache; missing usable evidence appears in `unavailable_providers`. Saved missing members remain inspectable and disconnectable. Provider Tailscale nodes are excluded because cloning them would duplicate native identity/state. Top-level Tailscale hops remain supported. An effective name must map uniquely to a locally known raw source name; changed source renaming/filtering may require clearing and recreating the chain.

A provider-node target creates an independent readable outlet such as `Chain: work / Node A`, appended to manual select groups using that original source. The original provider and its other nodes are unchanged. Select the outlet explicitly with `proxy select GROUP OUTLET`; creation does not select it. Clearing removes the outlet and only its saved choices, including when a generic override replacement or clear removes it. Other choices remain. File/HTTP provider initialization with no matching node uses `REJECT`; inline zero-match payloads fail activation. Native refresh failures may retain the last good node. Nagi never substitutes another node or `DIRECT`. Explicit provider update refreshes the original source first, then all its derived chain providers; a source failure skips dependent refresh, and dependent failures report partial success without exposing source credentials.

`--via NODE` and `--via-provider PROVIDER NODE` may be interleaved for node, provider, and provider-node targets; their argument order is the traffic order. `--via` follows traffic order: first hop → next hop → target → destination. A target can be a supported top-level node, one provider-scoped node, or an entire provider. Supported native dialer protocol types are `ss`, `ssr`, `vmess`, `vless`, `trojan`, `http`, `socks5`, `snell`, `hysteria`, `hysteria2`, `tuic`, `wireguard`, `anytls`, `ssh`, `mieru`, `gost-relay`, `sudoku`, `masque`, `trusttunnel`, `openvpn`, and `tailscale`. Special outbounds and groups cannot be explicit hop choices. Engine build capabilities and backend restrictions still determine whether the complete configuration validates; this list does not promise every protocol/transport combination.

Nagi clears the first hop's native `dialer-proxy` explicitly, links each later hop to its predecessor, and links targets to the final hop. Provider targets use `override.dialer-proxy` for every current and future payload node while preserving the provider download proxy. An explicit empty override suppresses inherited source dialers. Clearing a node or provider target removes its incoming edge only; upstream segments and other targets remain configured. Clearing subscription scope removes the managed layer and restores the remaining source/ordinary-override edges; its plan reports those actual restored values.

All targets in one batch have the same kind and share one ordered hop list. Names must be distinct, and target arguments precede options. Self references, repeated hops, missing dependencies, indirect cycles through nodes/groups/providers, and conflicting external provider dialers are refused. Intermediate nodes are shared: their replacements may affect other targets, which the plan reports. Explicit node relationships can be replaced deliberately; provider payload conflicts reject the entire batch.

`--dry-run` prepares a plan without saving or activating. A preview containing conflicts succeeds so callers can inspect it; committing conflicts fails without partial mutation. Plans include `kind`, ordered `targets` and `via`, `persistent`, `changes` (`kind`, `name`, `before`, `after`), `shared_use` (`kind`, `name`, `via`), and `conflicts`. Provider-node inventory adds `provider`, opaque stable `id`, `available`, `supported`, optional `unsupported_reason`, `outlet`, and `path_refs`. Each path reference has `kind`, `name`, and optional `provider`; `path_refs` includes the target and is the complete traffic path. Legacy `via` remains an array of names. Plans additionally report `outlet`, `outlet_groups`, and `path_changes` (`kind`, `name`, optional `provider`, `before_refs`, `after_refs`) for other affected paths. Scoped plans use an empty array when no other path changes. Raw edge values and opaque IDs are machine fields; clients should display structured path references rather than generated IDs. Before/after values are dialer names, not proxy definitions or credentials. One batch uses one candidate validation and one candidate activation. Validation, save, or activation failure restores the entire transaction and attempts runtime recovery. If recovery cannot be confirmed, the error states that runtime state is uncertain.

#### Persistent subscription policies

Subscription scope targets exactly one actual Nagi subscription associated with the selected profile. Version 1 policies accept static top-level hops only; `--via-provider` and simultaneous provider-node chains are explicitly refused. Clear the existing chain before switching between these scopes. Setting requires its applied source hash still to match; explicitly apply the subscription after a source edit. Setting does not download or apply a subscription. Download-only refresh changes the cache without changing configuration.

A private versioned policy retains the complete ordered hop definitions and source-collision identity independently of ordinary local overrides. It covers all eligible effective top-level nodes and providers, including ordinary local additions and later explicit or scheduled feed updates, while excluding the chain members themselves. Provider download proxies remain unchanged. Full source collisions, protected external dialers, cycles, and validation failures reject the whole candidate; automatic apply retains a conflicting cache as pending.

Managed edges cannot be edited individually. `node edit` updates a managed Tailscale node's retained definition. Clearing or switching the policy retains former hop definitions as ordinary local additions, which later subscription application preserves. `profile override clear` removes ordinary overrides/additions but retains the separate policy. Manual profile import detaches the policy and subscription association transactionally. Profile removal deletes policy state. Subscription removal refuses an active policy until its chain is cleared. Local default-setting application preserves nodes, groups, additions, and the policy.

The current storage format is version `1` at `config/overrides/PROFILE.yaml.chain-subscription.yaml`, with `subscription`, ordered `via`, matching complete `nodes`, optional `source-nodes`, and optional `replaced-edges` (`kind`, `name`, `via`) recording precisely adopted source relationships. It contains secrets and is owner-only. Unsupported versions fail explicitly. The former `proxy tailscale` commands, result DTOs, and `PROFILE.yaml.tailscale-subscription.yaml` single-exit policy are unsupported. An existing legacy policy produces an explicit error; there is no automatic migration. Explicitly preserve any needed definitions in private configuration and remove the legacy policy before using the new interface. No ordinary command exports its secrets for migration.

#### JSON and failures

Results use the standard envelope. `node list` returns `profile`, `nodes`, `group_candidates`, and the non-null string array `provider_candidates`; `node show` returns `profile` and `node`. A node contains `name`, `type`, `managed`, `groups`, and optional `tailscale`. Listed subscription nodes also contain `subscription`, the associated subscription name; other nodes omit it. Group data contains `name` and non-null arrays `explicit`, `automatic`, `excluded`, and `available`. Tailscale data contains `name`, `exit_node`, `hostname`, `control_url`, and `auth_key_configured`. `node groups show` returns `profile`, the string `node`, and `groups`.

`chain list` returns `profile` and `chains`; `chain show` returns `profile` and `chain`. A chain target contains `kind`, `name`, `type`, and ordered `via`. `chain candidates` returns `profile` and redacted `nodes`. Chain set/clear results contain `profile`, `action`, `plan`, `dry_run`, `saved`, `reloaded`, and `resource: "chain"`. Node mutations contain `profile`, `name`, `action`, `saved`, `reloaded`, and `resource: "node"`.

Usage errors exit `2`; operational failures exit `1` with `proxy_node_error` or `proxy_chain_error`. Reads, previews, text results, and JSON follow the same stopped-engine behavior. See the [guide](../guides/proxy-nodes-and-chains.md) for workflows and the [runtime design](../design/runtime.md#proxy-nodes-chains-and-tailscale-identity) for identity and transaction boundaries.

### Automatic latency check intervals

These settings control mihomo latency and availability checks, separately from subscription or Provider downloads. The terminal menu exposes subscription, Provider, and group check intervals and URLs; the App exposes subscription and Provider settings. The CLI supports all three scopes.

```sh
nagi proxy healthcheck show subscription work
nagi proxy healthcheck set subscription work 300
nagi proxy healthcheck show provider airport
nagi proxy healthcheck show provider airport --full-url
nagi proxy healthcheck set provider airport 600
nagi proxy healthcheck reset provider airport
nagi proxy healthcheck reset subscription work
nagi proxy healthcheck url set subscription work https://example.com/generate_204
nagi proxy healthcheck url set provider airport https://example.com/generate_204
nagi proxy healthcheck url reset provider airport
# Advanced group-level control:
nagi proxy healthcheck set group 'Subscription Auto' 300
nagi proxy healthcheck url set group 'Subscription Auto' https://example.com/generate_204
```

Replace subscription, provider, and group names with your own. `show` reads persisted settings without probing nodes or requiring a running engine. `set` accepts integer seconds from `1` through `31536000`; `off` and zero are rejected because zero can select mihomo's default rather than disable checks. Provider enable, URL, timeout, expected status, lazy behavior, download interval, and unrelated settings are retained. Saving an interval does not enable a disabled provider check.

A subscription setting follows the actual subscription-to-profile association. It is saved as optional `healthcheck_interval_seconds` in the subscription index, and can be read, set, or reset before the first download or apply without mihomo. Once applied, it supplies native intervals to `url-test`, `fallback`, and `load-balance` groups, to `select` groups already configured with a positive interval, and to existing provider `health-check` mappings. It does not add a check configuration to providers lacking one or enable previously unscheduled select groups. Explicit and scheduled subscription application extend this policy to newly added eligible groups/providers without changing the downloaded source. Manual profile import detaches the old association; removing a subscription removes its policy and revalidates/reactivates the affected selected profile.

Provider local intervals take precedence over the subscription setting, which takes precedence over the original configuration and mihomo defaults. Advanced explicit group intervals remain local settings for the group's own scheduler. `reset provider` removes its local interval leaf and inherits the subscription setting or original interval; `reset subscription` follows the original configuration while retaining separate Provider/group local settings. Source/default values may differ among groups; a subscription following configuration is reported as `interval_seconds: 0` with `interval_defaulted: true`, not a zero-second schedule. The subscription result's `profile` is empty before association; `enabled` is null because the policy does not represent one running checker.

Group explicit intervals are stored as `nagi-group-healthcheck-intervals`, a local override mapping from group names to integer seconds. Effective generation applies subscription settings, ordinary per-field local settings, and explicit group interval metadata, removing Nagi metadata before passing configuration to mihomo. This preserves deliberate choices even when their values initially equal the source. If a group disappears, its explicit setting remains dormant for a same-name return. Group reset removes only its target setting and restores that source interval in any existing group snapshot; an associated subscription policy then applies normally, and unrelated local differences remain.

`proxy healthcheck url set KIND NAME URL` saves a persistent HTTP(S) test URL; `proxy healthcheck url reset KIND NAME` restores inherited behavior. The URL must be absolute, have a hostname, and contain no user information, fragment, or whitespace. Paths and query strings are allowed. Healthcheck inspection returns only the URL scheme and host by default. `proxy healthcheck show KIND NAME --full-url` returns the complete saved test URL in `test_url`, including its path and query, for local editing; it works offline for `group`, `provider`, and `subscription`, and an unset subscription URL remains absent in `test_url`. Editing JSON also includes `inherited_interval_status` and `inherited_url_status` (`known`, `mixed`, `unavailable`, or `not_applied`), with `inherited_interval_seconds` and the complete `inherited_test_url` only when known. Provider/group previews remove the target local setting and retain subscription policy. Subscription previews remove its policy and summarize the associated profile while retaining Provider/group local settings; differing checker values report `mixed`. These previews are read-only and do not activate or persist settings. The option changes only the read result, with the same success envelope and exit codes; unknown options exit `2` before runtime access. Subscription list and mutation results continue returning only origins. Healthcheck results include `url_source`. URL edits never trigger a measurement. Subscription URL intent is stored with the subscription before first apply and follows its profile association. It supplies eligible automatic group URLs and URLs in existing Provider `health-check` mappings without enabling disabled checks or creating missing mappings. A Provider's explicit local URL takes precedence for its own checker, and an explicit group URL takes precedence for that group's checker. Resetting a Provider or group URL inherits the subscription policy when associated, otherwise the original configuration. Provider reset is refused when neither a usable source test URL nor an associated subscription URL exists, because clearing its only URL would disable native checks; `can_reset_url` indicates availability. Group reset retains mihomo’s configured group URL fallback. Group interval and URL settings are available through the CLI and the terminal menu under **Proxies → Proxy Groups → Manage proxy groups → group → Select node → Automatic latency checks**; the App exposes subscription and Provider settings. A group's test URL can differ from its attached Provider's own healthcheck URL, because mihomo registers group tests with the group's conditions.

Set/reset operations take the configuration lock. Associated profiles are validated with mihomo, with reload only when the affected profile is selected and running. A saved subscription without an associated profile only updates metadata. A failure restores prior configuration/metadata and attempts runtime recovery under the activation transaction; uncertain recovery is reported. Usage errors exit `2`; operational failures exit `1` with `proxy_healthcheck_error`.

The pinned engine shares concurrent latency tests for the same ordinary node instance, full test URL, expected HTTP status, configured timeout, and unified-delay mode. A successful measurement is reused for five seconds after completion, including manual node/group tests and automatic checks from different groups. Failed measurements are not cached. Reuse preserves the original measurement timestamp and does not append a new history entry. Different node instances (including same-name nodes in different providers), URLs, status conditions, or timeouts are independent. Group adapters themselves are not cached because their selected path can change. There is no configuration switch for this five-second window. One caller canceling does not cancel another caller's shared measurement; each caller still respects its own deadline.

An interval describes configuration, not a promise that every node is checked on that cadence. Provider nodes share their provider scheduler, whose nonzero interval can take precedence over a group's registered task. A group containing only providers or uncertain filtered include-all membership reports unknown enablement rather than guessing its schedule. Provider `enabled` describes its own `health-check.enable` field; groups registering other test URLs can also activate its scheduler. `lazy` defaults to `true`, permitting unused checks to be skipped. `source` is `override` for explicit local settings, `subscription` for an inherited subscription policy, or `profile` for source/default configuration. `interval_defaulted` identifies an omitted/zero native field or subscription follow-configuration mode.

### Recent node measurements

`proxy groups`, `proxy show`, `proxy search`, `proxy providers`, and `proxy provider show` add `node_info` and `node_delays`, keyed by member name. Reading uses existing mihomo observations without starting a measurement or persisting another history database. Effective group membership limits identity resolution to its explicit proxies and attached providers. A provider context always refers to that provider's node, even when a top-level node or another provider has the same name. Ambiguous ownership remains unknown rather than using another node's data.

`node_info` values have `kind` (`node`, `group`, `builtin`, or `unknown`) and optional `status` (`available` or `unavailable`). A nested group's optional `selected_delay` uses the `node_delays` value format for its current selected ordinary leaf, followed recursively through selectable groups. If the selected leaf is a built-in action, `selected_builtin` gives its name instead; it has no inferred availability or delay. An untested ordinary leaf has `selected_delay.status: untested` and no inferred availability. Cycles, missing/ambiguous selections, and LoadBalance groups have no inferred single-node measurement or availability. Built-in actions such as `DIRECT` and `REJECT` have no latency status. Menu/App retain selection markers and exclude built-ins and nested groups from individual node-test choices. The terminal menu shows a nested group's current selected ordinary leaf latency from `selected_delay`, including failed, untested, and unknown states; zero milliseconds displays `<1 ms`. When no selected-leaf observation is available, it retains known group availability without inferring a latency. The App shows the selected ordinary leaf's measurement without its name, or the built-in action's name. Explicit backend identity takes precedence over name matching; a provider node named `DIRECT` is still a provider node.

`node_delays` contains observations for ordinary nodes, with `status` (`ok`, `failed`, or `untested`), optional nonnegative `delay_ms`, optional RFC 3339 `measured_at`, optional sanitized `test_url`, and optional explanatory `reason`. Group and built-in entries have no own delay observation in this map; a nested group's selected leaf measurement appears only in `node_info.selected_delay`. The newest URL-specific observation wins across test URLs, with the context's configured URL breaking timestamp ties; generic history is used when URL-specific histories are absent. Native per-URL `alive` distinguishes successful zero milliseconds from failure. A successful zero is displayed as `<1 ms`; a newer failure replaces an older successful value. Unavailable or ambiguous history is displayed as unknown.

`test_url` is only the HTTP(S) origin (scheme and host, including any port); user information, path, query, and fragment are omitted. App node tooltips and terminal group/provider details display measured time to the minute and test origin when available; machine timestamps retain their RFC 3339 precision. Results belong to the current engine and may disappear after reload/restart. They represent the last check, not guaranteed current connectivity. Selecting a node in the App refreshes group state immediately, including nested leaf measurement and automatic/fixed selection state.

### Latency and selections

`proxy test group GROUP NODE` and `proxy test provider PROVIDER NODE` are the context-aware single-node operations used by the menu and App. They resolve the exact ordinary node before dispatch and use that group's or provider's configured test URL, timeout, and `expected-status`. A group without a URL inherits the first attached provider's nonempty test URL; include-all provider sources use mihomo's name order. Omitted URL/timeout use mihomo's default test address (`https://www.gstatic.com/generate_204`) and 5000 milliseconds. Nagi-created strategy groups and new Providers without an inherited test URL use `https://cp.cloudflare.com/generate_204`; existing configured URLs are retained. These commands reject built-ins, nested groups, missing effective configuration, and ambiguous nodes before measuring. Single-node tests preserve group fixed selections. Usage errors exit `2`; unavailable configuration, ambiguous identity, or failed measurements exit `1` through the ordinary CLI error envelope. The pinned engine validates expected HTTP status before reporting success and accepts successful zero-millisecond results. A successful response can reuse a recent measurement without producing fresh history. Failed requests are not rescued by old successful history; compatibility recovery requires a new native URL-specific observation. The privileged backend permits the same configured expected-status request and read-only scoped history.

`proxy delay NODE [URL] [TIMEOUT_MS]` defaults to `https://cp.cloudflare.com/generate_204` and `5000` milliseconds. Supply a URL before a custom timeout. The URL must be absolute HTTP or HTTPS without user information; the timeout must be an integer from `1` through `30000`. Invalid arguments fail before contacting the engine. The CLI allows up to 31 seconds for the control request, so the maximum measurement timeout is not cut short by the ordinary ten-second API timeout. `NODE` is a mihomo proxy name, including built-ins such as `DIRECT`; names with spaces must be quoted. The pinned mihomo implementation sends an HTTP HEAD request and records the latency in the proxy's delay history. This measures request latency, not bandwidth.

`proxy delays GROUP [URL] [TIMEOUT_MS]` uses the group's effective URL, timeout, and expected status when arguments are omitted. An explicit URL and optional timeout override those values for advanced callers, subject to the same URL/timeout constraints as `proxy delay`. When effective conditions cannot be read, implicit defaults fail before dispatch; explicitly supplying both URL and timeout remains available. URLTest uses its configured test URL in the pinned engine even if an explicit URL is supplied. The result's `url` is the requested URL. The pinned engine excludes measurements with an unexpected HTTP status from successful group results, including when sharing tests. Nagi accepts successful native results without requiring a new history entry. It asks mihomo to test the group and returns one result for each listed entry when the native test succeeds. Built-in `REJECT` and `REJECT-DROP` entries, including nested groups currently selecting one, are reported with `unmeasurable: "reject action has no latency"` instead of an error when the group has another successful result. If every member resolves to one of these reject actions, mihomo returns HTTP 504; Nagi reports the same unmeasurable results with success after the native request. A 504 involving any other member remains an error. Ordinary provider nodes named `REJECT` remain testable. Successful measurements sort by ascending `delay_ms`, then name; failed measurements follow with `error: "latency test failed"`, then unmeasurable reject actions. `proxy search QUERY` matches node names without case sensitivity and returns groups containing matches. `proxy groups` and `proxy search QUERY` follow the selected effective profile's `proxy-groups` order; mihomo-only groups absent from that profile follow in name order. If the selected profile cannot be read, groups fall back to name order. Each group's node list keeps mihomo's order. `proxy groups` and `proxy show GROUP` add `selected_status`: `available`, `unavailable` (mihomo reports the group not alive), `removed` (the selected name is absent), or `none`. An explicitly pinned URLTest or Fallback group also has `fixed`; its `now` may otherwise be an automatic result. Selecting a missing node fails with `proxy_selection_error` and suggests `proxy show`.

`proxy auto` requires a URLTest or Fallback group. It calls mihomo `DELETE /proxies/:name` and removes the saved choice for that group. If removing the saved choice fails, exit status is `1` with `proxy_selection_error` and the message states that the running engine already resumed automatic selection. `proxy delays` uses native `GET /group/:name/delay`; mihomo clears a fixed choice for URLTest or Fallback groups at the start of that handler, before measuring latency. Nagi removes its saved choice when the request succeeds, when mihomo reports a measurement timeout after entering the handler, or when a follow-up group read confirms a fixed-to-automatic transition. A failure before dispatch leaves the saved choice intact. If transport fails and the reset cannot be confirmed, Nagi keeps the saved choice and reports the uncertain state with a `proxy show` recovery step; replaying that choice may pin the group again. A failed saved-choice removal reports that the running engine reset but persistence failed. Provider operations return `proxy_provider_error` and exit status `1` on missing providers or failed updates/checks; native error details are suppressed because they can include provider URLs.

Successful `proxy select` requests save the group and node under the selected profile in `proxy-selections.json` in the configuration directory, with mode `0600`. If saving fails, the error states that the live engine was already changed. On start, restart, and profile reload, Nagi places saved choices for available `select` groups into the generated effective profile before mihomo activates DNS or TUN. Nagi disables mihomo's separate selection cache in that generated profile and then attempts control API replay for choices that become available after startup. A choice backed only by a remote provider may use the group fallback until that provider supplies its node. A failed replay returns `proxy_selection_error` with exit status `1` and states that mihomo started or reloaded; inspect `nagi status` and retry `nagi proxy restore`. Run `proxy restore` to see accepted choices and choices whose group or node is unavailable or removed. Nagi does not delete unavailable saved choices automatically; a later configuration may restore them.

```sh
nagi proxy test group 'Proxy Group' 'Node A'
nagi proxy test provider airport 'Node A'
nagi proxy delay 'Node A'
nagi proxy search Hong
nagi proxy delays 'Proxy Group'
nagi proxy restore
nagi proxy delay 'Node A' https://example.com/ 8000
nagi connections list
nagi --json connections watch --interval 250
nagi connections close CONNECTION_ID
nagi mode global
nagi mode
```

Replace `Node A`, `Proxy Group`, and `CONNECTION_ID` with names and IDs from your engine. Connection lists show IDs and any selected proxy chain in text and JSON. Closing an ID that has already disappeared succeeds because mihomo treats it as an idempotent request. `close-all` is explicit, noninteractive, and affects all connections present when mihomo processes the request. Applications may immediately establish new connections. No closed-connection count is returned.

`mode` without an argument reads the running mode. With `rule`, `global`, or `direct`, it changes only the running engine through `PATCH /configs` and returns `persistent: false`; restarting or reloading a profile reapplies the mode in that configuration. `mode save MODE` edits the selected profile's local override when one exists, otherwise its source profile, using the atomic replacement and reload path; it returns `persistent: true`. `mode saved` reads the effective stored mode without contacting mihomo and returns `persistent: true`. A persistent save while the engine is running reloads the profile; a failed reload rolls the modified file back. The macOS menu offers this as **Save Current Mode as Default**. All temporary mode changes require a reachable mihomo Unix Socket API.

`config reload` validates the selected profile's effective configuration first, requires a running engine, and sends mihomo `PUT /configs?force=true` with the source or generated effective profile path. Validation failure leaves the running engine untouched. The API request supplies a unique `activation_id`; success requires the same ID from `GET /configs`. If a PUT times out, Nagi checks again with a fresh bounded context. If activation changed on failure, it reactivates the validated prior effective configuration and confirms the restoration ID. A timed-out original PUT whose ID was never observed may still finish after that confirmation, so failure output reports runtime recovery as incomplete, includes the currently observed activation ID when available, and advises checking `nagi status` and retrying `nagi config reload` after the pending request settles. Invalid prior content or an unreachable API leaves restoration unconfirmed and is reported as such. A successful reload returns `reloaded: true` and the profile name. Validation uses mihomo `-t --check-json`; field-level validation errors identify the failing path when mihomo provides it without exposing configuration values.

`connections watch [--interval MILLISECONDS]` streams complete snapshots immediately, then at a default interval of 250 ms. The interval must be an integer from 250 to 60000; invalid syntax exits `2` before runtime resolution. Ordinary backends use mihomo's WebSocket; privileged backends use sequential full-snapshot GET requests through the existing helper. Requests never overlap or accumulate timer tasks. Connections entirely between samples may be missed. JSON output is one successful envelope per line, with `data.connections` (including an empty array) and `data.sampled_at` in RFC3339Nano UTC format. Text output labels each sampling time to minute precision and explicitly reports empty lists. Ctrl-C/SIGTERM exits normally; collection failures exit `1` on stderr with `mihomo_api_error`, overflow with `response_too_large`, and output failures with `output_error`. The stream has no whole-request timeout. The 4 MiB limit applies to each complete engine snapshot; the App accepts at most 8 MiB per CLI stream line. Results are never truncated.

`connections show ID` finds one connection in a fresh `/connections` snapshot and reports the same `id`, `metadata`, `chains`, `upload`, and `download` fields as a list item. A missing ID fails with a suggestion to list current IDs. `connections list` remains a snapshot. The CLI preserves optional `rule` and `rulePayload` fields but does not expose mihomo's connection start time. The ordinary and privileged backends both limit complete mihomo control responses to 4 MiB; larger responses fail with `response_too_large` and exit `1`, without returning a truncated list. Reduce the active connection set before retrying. The macOS App accepts up to 8 MiB of CLI stdout for connection lists to allow JSON re-encoding; other App commands and stderr retain their 1 MiB limit. `connections close ID` is idempotent when mihomo has already removed the connection; `connections close-all` affects connections present when mihomo processes the request. A closed connection can be recreated by applications.

`logs follow` starts at the current end of the log and streams newly appended complete lines until interrupted. It does not replay existing lines. Text mode prints each new line; `--json` emits one success envelope per line with `data.line`. Terminal users should press Ctrl-C. Log file errors are reported without exposing configuration values.

`traffic watch` connects to mihomo's private Unix Socket `GET /traffic` stream and prints each sample as it arrives, approximately once per second. It requires a running mihomo control API. Text mode labels rates in bytes per second and cumulative totals in bytes. `--json` emits one newline-delimited success envelope per sample with `data.upload_bps`, `data.download_bps`, `data.upload_total`, and `data.download_total` as nonnegative integer byte counts, plus `data.sampled_at` as an RFC 3339 UTC timestamp. The rates are mihomo's current rates; the totals are mihomo's counters since its current start. Ctrl-C or SIGTERM ends the command successfully. A failed connection, malformed sample, or unexpected stream end writes a failure envelope to stderr in JSON mode and exits `1` with `mihomo_api_error`; a failed output write uses `output_error`. The stream is not subject to the ordinary control request's whole-request timeout.

## Shell completion

Provider-node shell completion uses local selected-profile evidence: `completion candidates chain-targets provider-node` lists source providers, and `completion candidates chain-targets provider-node PROVIDER` lists their effective node names. Bash, Zsh, and Fish use these for provider-node targets and `--via-provider`; existing `node`, whole `provider`, and `subscription` completion remains available.

`completion bash`, `completion zsh`, and `completion fish` print scripts for the selected shell. Text mode prints the script directly; `--json` returns it as the envelope's string `data`. Generation does not require mihomo or a valid selected profile. Global-prefix completion offers `--lang`, `en` and `zh-Hans` and skips the language value before determining the command context. Completions cover command names, subcommands, global flags, supported mode/shell and DNS policy values, DNS query record types, and local files for profile import, export, and override set. Healthcheck actions complete `group` or `provider` and then names from the selected local effective profile through `completion candidates healthcheck-targets group|provider|subscription`, without a running engine. Context node tests complete group members or `completion candidates provider-nodes PROVIDER` so same-name provider nodes retain their owner. At completion time, other proxy group, node, and provider names are queried from the running engine; an unreachable engine yields no dynamic proxy candidates. For `profile override apply-default NAME --dns-proxy`, group candidates instead come from the named profile's local effective configuration, so the running engine may be stopped or using another profile. Profile and subscription names are queried from local stores without exposing URLs. `subscription key` completes subscription names and then local files or `clear`. `completion candidates groups|nodes [GROUP]|providers|profile|subscription|rules|profile-groups NAME` returns newline-separated text or a JSON string array; `profile-groups NAME` reads eligible DNS proxy groups from that profile without contacting the engine. `rules` reads stored rule, bundle, and set names from the selected profile without contacting the engine; Bash, Zsh, and Fish use these candidates for `rules remove`, `rules enable`, and `rules disable`. Node and chain completion reads local `proxy-nodes`, `node-groups`, and `chain-targets node|provider|subscription` candidates while stopped, and completes `--via`, `--group`, dry-run, and key-input options without exposing keys. `completion candidates proxy-nodes|node-groups|chain-targets node|provider|subscription` returns names only. Connection IDs are not completed.

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

## Sensitive content

The [local output and sensitive-content rules](../development/ui-design.md#local-output-and-sensitive-content)
apply to CLI text, JSON, errors, and the terminal menu. Local summaries retain
useful node names, domains, paths, and URL context. Error formatting removes
URL userinfo and query or fragment values with common credential names while
keeping the host, path, and other values; this is a narrow formatter, not a
general secret detector. Subscription fetch errors omit the complete request
URL because credentials may be embedded in its path, and retain the underlying
DNS, TLS, timeout, or connection cause. Summary commands continue to omit
credential fields and request bodies. Explicit raw views and exports preserve
their content. This includes `proxy healthcheck show KIND NAME --full-url`, which
returns the complete configured test URL for local editing; its query may
contain private values. Shareable diagnostic reports omit URL paths, query
strings, fragments, and userinfo, retaining at most the URL origin.

Commands that display or export raw configuration, overrides, profile comparisons, backups, or logs may expose credentials and private subscription URLs. This includes `config show`, `profile override show`, `profile diff`, `profile export`, `profile backup`, `logs`, and `logs follow`, including their menu equivalents. Explicit `request-rewrite export` and `request-rewrite preview` also contain sensitive rule/request values. These outputs are not automatically redacted; keep them private and review them before sharing. For a report intended for troubleshooting, use the [redacted diagnostic export](#diagnostics-and-security).

The existing `subscription add NAME URL` and `proxy provider add NAME URL GROUP` interfaces require the subscription URL as a command argument; neither provides a URL stdin or file option. A private URL passed this way may be exposed through shell history and process arguments. This is a current limitation relative to the project's preference to avoid passing secrets through command arguments. Shell quoting preserves the argument but does not hide it. Tailscale Auth Keys support separate file and stdin input as described under [proxy nodes and chains](#proxy-nodes-and-chains).

## Text output

Status output labels the running state, selected profile, and available process details. Profile lists mark the current selection; proxy output identifies the current node. Empty lists have an explanatory message. `logs` prints individual log lines, and `config show` prints YAML directly.

Subscription feedback distinguishes saving a URL from downloading content and applying a profile. Downloading does not change the selected profile. It prints node changes and the apply command. Preview prints node changes without applying. Applying selects the profile and reloads a running engine; it does not start a stopped engine. Profile import validates and writes a file; use `profile use NAME` to select it. Replacing the currently selected profile also reloads a running engine; importing another profile does not reload it. Valid replacement content can repair an invalid existing profile.

## Recovery and service failures

An invalid selected-profile record does not prevent `stop`, `quit`, `logs`, `version`, service management, subscription listing/downloading/removal, or proxy and connection operations. `status` still reports process state, omitting `profile` and adding `profile_error` when the selection cannot be read. It completes its lifecycle check and any stopped-engine DNS/system proxy restoration first, then gathers independent DNS state, engine version, and listener configuration concurrently. Version and listener queries run only while the engine is running; failed version or configuration reads omit `version` or `mixed_port` respectively without failing the status command. `profile list` still lists files, omitting `current` and adding `current_error`. Text output warns that the selection is unavailable. Use `profile use NAME` with an existing valid profile or `subscription apply NAME` with valid cached content to repair an invalid selection. Import and removal require a readable selection so Nagi can determine whether their target is active.

If saving a subscription refresh time fails, Nagi attempts to restore the previous cache, or remove a newly created cache. Restoration failures are also returned. A failed refresh does not activate a profile.

If service installation fails after writing its definition, Nagi attempts to restore the old definition or remove the new one. The error reports the file outcome; service manager state can still have changed and must be checked before retrying. Uninstallation retains the definition if stopping/disabling the service fails. If the final systemd reload fails after removing the definition, the error explains that removal already completed and asks for a reload. An absent definition makes uninstallation a no-op; this does not verify that the service manager has no loaded service.

## Configuration validation failures

Nagi does not pass through raw mihomo validation stdout or stderr because they may contain configuration values. When the engine supplies structured redacted diagnostics, errors preserve the field path, code, and message; otherwise they report a generic validation failure. Profile validation of configurations containing Tailscale returns a generic error instead. Errors also provide a next step: inspect the selected configuration for `config validate` or startup failures, and inspect the source YAML being imported/applied or the profile being selected for profile operations. Missing executable errors suggest checking `NAGI_MIHOMO_BIN`.

To inspect mihomo's own diagnostics locally, use `mihomo -t -f FILE -d DIRECTORY`, replacing `FILE` with the configuration file and `DIRECTORY` with its configuration directory. Use the executable selected by `NAGI_MIHOMO_BIN` when applicable. Those diagnostics may contain private configuration values. Nagi does not expose its temporary validation files as a recovery path.

## JSON and exit codes

JSON success output has the shape `{"ok":true,"data":{...}}`. JSON failure output is written to stderr as `{"ok":false,"error":{"code":"...","message":"..."}}`. Successful commands exit with status `0`; usage errors exit with `2`; all other errors exit with `1`. Error codes include `usage`, `already_running`, `scheduler_already_running`, `not_running`, `lifecycle_busy`, `not_found`, `invalid_config`, `profile_error`, `subscription_error`, `dns_error`, `rule_error`, `proxy_selection_error`, `proxy_provider_error`, `proxy_node_error`, `proxy_group_error`, `proxy_chain_error`, `system_proxy_error`, `tun_error`, `diagnostic_error`, `mihomo_api_error`, `response_too_large`, and `internal_error`. These codes and field names are the CLI integration contract for the macOS app. Error messages are intended for people and may change; applications should branch on `error.code`. Error URLs retain useful host/path context; URL userinfo and query or fragment values whose names identify common credentials are masked in text and JSON errors. Subscription fetch failures omit the private request URL and retain the underlying cause.

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
| `profile override apply-default NAME [--dns-proxy GROUP]` | `name` (string), `default_override_applied: true`. |
| `profile override default-status NAME` | `name`, `dns_proxy_group` (strings), `default_override_applied`, `dns_proxy_required` (booleans), `dns_proxy_groups` (string array). |
| `profile override restore-default NAME` | `name` (string), `default_override_applied: false`, `default_override_restored: true`. |
| `profile remove NAME` | `name` (string), `removed: true`, `kind: "profile"`. |
| `dns status` | `profile`, `policy` (strings), `enabled`, `ipv6`, `tun_enabled` (booleans), `upstream_hosts`, `issues` (string arrays). |
| `dns check` | DNS status fields plus `scope` (string) and `engine_query_ok` (boolean). Scope is `configuration and mihomo API`. |
| `dns set` | `profile`, `policy`, `proxy_node` (strings), `upstream_count` (integer), `saved: true`. |
| `dns exception add/remove` | `profile`, `domain` (strings), `exception_changed: true`. |
| `dns tun on/off` | `profile` (string), `tun_enabled` (boolean), `saved: true`. |
| `dns query` | `domain`, `type` (strings), `response` (mihomo JSON object). |
| `dns flush` | `flushed: true`. |
| `subscription list` | `subscriptions` array: `name`, `key_configured` (boolean), and `schedule_seconds` (integer; `0` means off); optional `healthcheck_interval_seconds` (integer, otherwise follow original configuration) and `healthcheck_url` (scheme and host only, otherwise follow original configuration), `next_due`, `updated_at`, and `expires_at` (RFC 3339), `last_failure_stage` and `last_failure_reason` (fixed redacted status codes), `pause_reason` (`profile_changed` or `source_unreadable` while automatic apply is paused), `cache_version`, `applied_cache_version`, `source_hash` (SHA-256 hex), and `profile`; optional nonzero `upload`, `download`, and `total` (bytes). The top-level payload also includes `scheduler_running` (boolean based on the live scheduler host lock and fresh heartbeat). Full URLs, identities, and identity paths are omitted. |
| `subscription add NAME URL [OPTIONS]` | `name` (string). The saved update interval and test URL origin appear on the next `subscription list`. |
| `subscription schedule NAME SECONDS\|off` | `name`, `schedule_seconds` (integer; `0` means off), and `scheduler_running` (boolean, based on the live host lock and fresh heartbeat). |
| `subscription key NAME FILE\|clear` | `name` and `key_configured` (boolean). |
| `subscription update NAME` | `name`, `bytes` (cached plaintext bytes, including after decryption), `updated_at`, `preview`, and optional `expires_at`, `upload`, `download`, `total`. |
| `subscription preview NAME` | `name`, `format`, sorted `added`, `removed`, and `changed` top-level node-name arrays; `changes` array of redacted `path`, `kind` (`added`, `removed`, or `changed`), and `behavior`; `remote_nodes_compared` boolean. |
| `subscription apply NAME` | `name`, `profile`, `applied: true`, `preview`, `selections_restored` (always `0`; retained for JSON compatibility). |
| `proxy delay`, `proxy test group/provider` | `proxy` and `url` (strings), `timeout_ms` and `delay_ms` (integer milliseconds). Provider nodes use mihomo’s provider-node healthcheck endpoint. |
| `proxy auto` | `group` (string), `automatic: true`, `saved_selection_removed: true`. |
| `proxy providers` | `providers` array of redacted `name`, `type`, `vehicle_type`, optional `updated_at`, `nodes` names, and `node_info`/`node_delays` values defined under [Recent node measurements](#recent-node-measurements). Subscription URLs, node definitions, and credentials are excluded. |
| `proxy provider show` | One redacted provider object with the same fields. |
| `proxy provider add` | `provider`, `group`, `profile` (strings), `interval_seconds` (integer), `added: true`, and `group_created` (boolean). URL and credentials are excluded. |
| `proxy provider remove` | `provider`, `profile` (strings), and `removed: true`. |
| `proxy provider update` | `provider` and `updated: true`. |
| `proxy provider healthcheck` | `provider` and `healthcheck_started: true`. |
| `proxy healthcheck show/set/reset`, `proxy healthcheck url set/reset` | `kind`, `name`, `profile`, `interval_seconds` (integer), `enabled` (boolean or null), `lazy` (boolean), interval `source` (`profile`, `subscription`, or `override`), URL `url_source` with the same source values, `interval_defaulted` and `can_reset_url` (booleans), and optional sanitized `test_url` and scheduling `reason`. Mutations return the resulting configuration with the same fields. |
| `proxy provider interval show/set/reset` | `kind: "provider"`, `name`, `profile`, `interval_seconds` (integer; `0` means off), `source` (`profile` or `override`), `enabled` (boolean), `interval_defaulted` (boolean), and `can_reset` (boolean). `source_interval_seconds` is present when `can_reset` is true and gives the source interval that reset will restore (`0` means off). |
| `proxy delays` | `group`, `url`, `timeout_ms`, `results` array of `proxy`, optional `delay_ms`, `error`, or `unmeasurable`. Uses mihomo’s native group test, including for reject-only groups. For URLTest and Fallback, a confirmed handler run clears the fixed node and Nagi removes the saved choice. The `url` field records the requested URL. In the pinned mihomo version, URLTest uses its configured test URL despite the request’s `url` parameter; other group types use the supplied or default URL. Successful automatic-group tests add `automatic_reset: true` and `saved_selection_removed: true`. |
| `proxy groups`, `proxy show`, `proxy search` | Group objects include `name`, `type`, `now`, `fixed` for explicitly pinned automatic groups, `all`, `alive`, `selected_status`, additive native `hidden` and `icon` metadata, and `node_info`/`node_delays` as defined under [Recent node measurements](#recent-node-measurements). Nagi menus omit hidden groups; subscription URLs and credentials are excluded. |
| `proxy restore` | `restored` and `unavailable` string arrays of `GROUP -> NODE`. Failed API application returns `proxy_selection_error` with affected group and node names. |
| `connections close ID` | `id` (string), `closed: true` (request accepted, including an already absent connection). |
| `connections close-all` | `closed: true` (request accepted; no count). |
| `connections list` | `connections` array (empty when no connections are active). Items contain `id`, `upload`, `download` (cumulative bytes), and optional `metadata`, `chains`, `rule`, `rulePayload`. Metadata uses native mihomo fields; ports may be strings. |
| `connections watch [--interval MILLISECONDS]` | Per-line envelope with `connections` (same items as list) and `sampled_at` (RFC3339Nano UTC). |
| `connections show ID` | One connection object with the same fields as a list item. |
| `mode` | `mode` (string: `rule`, `global`, or `direct`), `persistent: false`. |
| `mode MODE` | `mode` (string), `changed: true`, `persistent: false` (temporary update accepted). |
| `mode save MODE` | `mode`, `changed: true`, `persistent: true` (profile update and reload accepted). |
| `mode saved` | `mode`, `persistent: true` (stored profile mode). |
| `config reload` | `profile`, `reloaded: true`. |
| `logs follow` | A stream of success envelopes, each with `line` (string), when `--json` is used. |
| `traffic watch` | A stream of success envelopes, each with `upload_bps`, `download_bps`, `upload_total`, `download_total` (nonnegative integers), and `sampled_at` (RFC 3339 UTC string), when `--json` is used. |
| `completion SHELL` | A string containing the completion script. |
| `completion candidates RESOURCE [ARG]` | An array of local profile or subscription names, selected-profile stored-rule names, live proxy group, node, or provider names, or DNS proxy group names from a specified profile. `ARG` is a group for `nodes` and a profile name for `profile-groups`. |

`NAGI_MIHOMO_BIN` overrides the default mihomo executable path, which is a file named `mihomo` beside the Nagi executable. Nagi uses XDG directories on Linux and `~/Library/Application Support/Nagi` on macOS; see the [runtime design](../design/runtime.md). The local artifact installer writes the built-in default profile on both install and update; see the [installation workflow](../development/install.md).

## Rules

| Command | Result or effect |
| --- | --- |
| `rules list` | Read mihomo's active ordered rules, including index, type, payload, target, and wrapper statistics when provided. |
| `rules test TARGET [--ip IP] [--port PORT] [--network tcp\|udp] [--process NAME]` | Test synthetic destination metadata against the running engine's ordered rules and rule providers. |
| `rules providers` | Read active mihomo rule providers. |
| `rules custom` | List Nagi-owned persistent rules, imported rule bundles, and imported sets. |
| `rules conflicts` | Show effective rule order and duplicate or shadowed matchers. |
| `rules connection ID` | Show the rule and payload mihomo recorded for an active connection. |
| `rules add NAME TYPE PAYLOAD TARGET` | Add a validated custom rule. Supported types include `DOMAIN`, `DOMAIN-SUFFIX`, `DOMAIN-KEYWORD`, `DOMAIN-REGEX`, `IP-CIDR`, `IP-CIDR6`, `GEOIP`, `GEOSITE`, `PROCESS-NAME`, and `MATCH`; use `-` as MATCH payload. |
| `rules remove NAME` | Remove an entire named custom rule, imported rule bundle, or imported set. |
| `rules enable NAME`, `rules disable NAME` | Enable or disable an entire named rule, bundle, or set while preserving its order. |
| `rules import NAME SOURCE` | Read a local file or download an HTTP(S) snapshot of complete native rules. |
| `rules edit NAME TYPE PAYLOAD TARGET` | Replace a single rule’s contents, preserving name, position, and enabled state. Uses the same types and MATCH payload convention as `rules add`. |
| `rules update NAME [SOURCE]` | Refresh an imported bundle or existing set from its saved source, or read and save a replacement source. Preserves name, position, kind, enabled state, and set behavior/target. |

Names use 1–128 ASCII letters, digits, `_`, or `-` and must be unique across custom rules, rule bundles, and sets within the selected profile. All stored-rule commands operate on the selected profile. Custom rules are stored in `config/rules/PROFILE.yaml` and enabled entries are prepended only to that profile's rules in saved entry order. Different profiles may reuse a rule name and keep independent rules. Imported objects remain separate from source profiles and subscription files, so applying or refreshing a subscription preserves the target profile's rules. Switching profiles selects that profile's own rules; rules targeting a subscription group do not affect `default` or another profile. The former global `config/rules.yaml` is neither read nor migrated; import wanted rules explicitly into each profile. Every mutation validates the effective YAML with mihomo; when running, the engine reloads the effective profile. Failed validation or reload restores the previous rules file. Mutations do not start the engine. Rule-test API errors returned by mihomo keep their actionable message; profile validation errors identify the candidate profile being validated and preserve mihomo's redacted path, code, and message. A `rules[N]` path is a zero-based index in the configuration passed to validation, which may include local overlays, rather than a source-file line number. GeoIP, GeoSite, and ASN database initialization failures retain their rule path and expose `geodata_download_failed` or `geodata_remove_failed`, with a redacted cause such as an HTTP status, DNS failure, timeout, refused/reset connection, unreachable network, TLS failure, permission denial, or full disk. These diagnostics omit download URLs, hostnames, credentials, and local paths; unknown causes are explicitly reported as unavailable. Database failures do not suggest changing rule syntax. Mihomo returns `rule_invalid_format`, `rule_proxy_not_found`, `rule_subrule_not_found`, or `rule_provider_not_found` for the corresponding native failures. Missing references include a redacted `target` field. Nagi does not infer missing targets from candidate YAML when the engine returns a generic failure. A rejected import referencing a missing outbound uses concise human output: `Import Personal failed: rule 1 references missing outbound: missing-target`, followed by `Rules were not saved; runtime configuration was not changed.` The displayed rule number is one-based in the generated candidate; machine diagnostics retain the zero-based path and engine code in the English error message. That unchanged-state assurance requires successful rules-file restoration and no activation attempt. Other validation errors preserve the engine diagnostic without appending a troubleshooting checklist. Detailed raw output from `mihomo -t -f FILE -d DIRECTORY` may contain private configuration values. Failed subscription apply retains the downloaded cache; retry with `nagi subscription apply NAME` after correcting the cause.

### Rule file imports

`SOURCE` is a local file path or an absolute HTTP(S) URL without userinfo. HTTP downloads require status 200. Files and downloaded responses must contain 1 byte to 8 MiB. Imports save the content as a snapshot; they do not establish a periodically refreshed remote provider.

Input must be a single YAML document: either a mapping containing a nonempty `rules:` string list, or a top-level string list. Each entry is a complete native mihomo rule, including its own target and any options. Imports contain at most 100,000 entries; entries must be nonempty strings without embedded newlines. The saved named bundle preserves every string and its relative order, and its rules are prepended in that order. No format or common-target argument is accepted. Provider `payload:` files and domain/IP-CIDR text lists are not supported, and Nagi does not detect or convert other formats.

Full-rule strings use mihomo's native syntax and targets, including logical rules and options such as `no-resolve`. Importing a mapping extracts only its `rules:` list; proxy groups, rule providers, sub-rules, DNS, and other profile fields are not imported. Any `RULE-SET` provider or `SUB-RULE` definition referenced by the rules must already exist in the effective profile. Missing references or invalid native syntax fail mihomo validation before the import is committed. Validation failures identify the import and selected profile. When the staged rules file has been restored successfully and activation was never attempted, the error states that the imported rules were not saved and the runtime configuration was not changed. Restoration or activation failures do not receive this assurance. `rules add` targets are `DIRECT`, `REJECT`, or a mihomo proxy group name without commas or newlines.

Use `rules disable NAME` or `rules enable NAME` for the whole imported object. `rules remove NAME` deletes that whole named object, including all rules in a bundle; it does not select or edit an individual string. Previously imported provider sets remain readable, continue to generate inline `nagi-NAME` providers with one `RULE-SET` rule, and can be enabled, disabled, or removed. New provider-set imports are not supported. `rules import-local` and `rules import-remote` are removed; use `rules import NAME SOURCE` for complete native rules.

`rules update NAME` explicitly rereads the saved source; no automatic refresh is scheduled. `rules update NAME SOURCE` replaces both the snapshot and its update source after validation. Bundle updates use the same full native rule input contract as import. Existing `kind: set` objects accept a single YAML `payload:` string list or top-level string list, with the same byte/entry limits; their behavior and common target stay unchanged. New set imports remain unsupported. Updating a single rule or editing an import fails with `rule_error`; use `rules edit` for single rules.

New imports and successful updates save the complete source in the private `refresh-source` field of the owner-only rules file; local paths are made absolute so later updates do not depend on the working directory. `rules custom` exposes only the existing `source` summary and additive `can_update: true` when a saved source exists; the complete source is excluded from JSON and ordinary output. Older objects with only a summary require `rules update NAME SOURCE` once; the menu offers Set update source instead of Update from source until then. Change source reads and validates the replacement immediately. Raw configuration exports/backups and explicitly opened raw rule values may contain sensitive information.

Edit and update validate the effective selected profile and reload only when running. Parsing, download, validation or activation failures preserve the prior stored object, including its source, through the existing rollback path. An unchanged-runtime assurance is reported only after successful restoration following validation rejection before activation; a failed activation does not prove the runtime state is unchanged. Disabled entries remain disabled and are excluded from the effective configuration until enabled.

```yaml
rules:
  - DOMAIN-SUFFIX,intranet.example,DIRECT
  - DOMAIN-SUFFIX,ads.example,REJECT
  - IP-CIDR,192.0.2.0/24,DIRECT,no-resolve
```

Save this example as `personal-rules.yaml`, then import and manage it:

```sh
nagi rules import personal ./personal-rules.yaml
nagi rules import remote https://example.com/rules.yaml
nagi rules update remote
nagi rules update personal ./personal-rules.yaml
nagi rules remove personal
```

`rules conflicts` reports duplicate matchers and rules appearing after an earlier `MATCH`, which cannot be reached. This is a static report and does not prove semantic overlap between arbitrary regular expressions, geolocation databases, or provider contents. `rules connection ID` only works while the connection remains active; mihomo supplies the recorded `rule` and `rulePayload` fields.

JSON payloads contain `rules`, `providers`, `entries`, `order`/`conflicts`, or `id`/`rule`/`rule_payload` as appropriate. `rules custom` returns `profile` alongside `entries`; successful add, edit, update, remove, enable, disable, and import mutations return `profile`, `name`, `action`, and `saved`. The `profile` field identifies the profile whose stored rules were read or changed. Errors use `rule_error` for validation, persistence, download, and reload failures, with the standard envelope. Import/update argument errors (invalid name or source, missing arguments, or extra arguments) and invalid edit arguments exit `2`; edit/update/import validation, missing objects, file/download, persistence, and activation failures exit `1` with `rule_error`; success exits `0`.

### Destination rule tests

`rules test` requires a running engine built from the commit in `engine.lock`. It sends `POST /rules/test` through Nagi's private control API, including the privileged helper when selected. It tests active rules, which may differ from stored configuration that has not been activated. Older engines return `rule_error` with guidance to install the matching engine and restart.

`TARGET` is a bare IPv4/IPv6 address or ASCII domain (use punycode for internationalized names), without a URL, port, brackets, wildcard, zone, or whitespace. Domains are lowercased and a final dot is removed; IPv4-mapped IPv6 addresses are normalized to IPv4. Defaults are TCP and destination port 443. `--port` accepts 1..65535; `--network` accepts `tcp` or `udp`. Each option may occur once. `--ip` supplies an IPv4/IPv6 destination address for a domain and avoids resolving that destination. It cannot accompany an IP target. `--process` supplies a process name, at most 256 bytes, without path separators, control characters, or surrounding whitespace. Other connection metadata is unavailable.

Mihomo uses its native ordered matcher, including disabled rules, logical rules, rule providers, pass behavior, and UDP support. Hosts mappings and DNS resolution follow the engine's normal routing semantics; a domain is resolved only when a rule requests it, and `no-resolve` remains effective. A failed required DNS lookup returns an error rather than a successful fallback result. The synthetic match does not increment rule hit/miss statistics, save configuration, reload, or connect to the destination. DNS lookups can generate real resolver traffic and affect DNS caches. Rematch adapters are not evaluated and return an error before dialing.

Tests evaluate rules even when the running mode is `global` or `direct`, reporting that mode and a warning that ordinary traffic bypasses the rules. They do not simulate source addresses/ports, inbound identity, UID, process paths, sniffing, or the eventual proxy connection. These limitations appear in the result. Use `rules connection ID` to inspect actual traffic.

Success JSON contains `target`, `destination_ip` (empty if unavailable), `port`, `network`, `process`, `mode`, `matched`, `rule`, `rule_payload`, `providers`, `outbound`, and `warnings`. `rule` uses mihomo's native type spelling, such as `RuleSet` or `IPCIDR`. A matched rule set reports its provider name as `rule_payload`; it does not identify a particular entry within the provider. With no matched rule, `matched` is false, rule fields are empty, and `outbound` is the engine's `DIRECT` fallback. `outbound` is the matched adapter or group name, not proof of a working connection or a selected leaf node. Usage errors exit `2`; engine/API/test failures exit `1` with `rule_error`; successful tests exit `0`, including fallback results.

The terminal menu exposes **Routing & DNS → Routing Rules → Test Rule**, prompting for a domain/IP and using TCP port 443. It displays the matched rule, provider, outbound target, and limitations. Failed submissions retain the input for explicit editing or retry; the CLI provides the additional metadata options.

```sh
nagi rules test 154.19.187.117
nagi rules test service.example --ip 192.0.2.1 --port 80
nagi --json rules test service.example --network udp --process Browser
```

## Traffic access

| Command | Result or effect |
| --- | --- |
| `traffic watch` | Stream live transfer rates and cumulative byte counters from mihomo once per second until interrupted. |
| `system-proxy status` | On macOS, read active OS HTTP, HTTPS, and SOCKS proxy settings. |
| `system-proxy enable` | On macOS, save current OS settings and point HTTP and HTTPS proxy settings at mihomo's local mixed port (or HTTP port). Requires a running engine. |
| `system-proxy disable` | On macOS, restore settings saved by Nagi; when no restore record exists, report the current settings without changing them. |
| `tun status`, `tun enable`, `tun disable` | Read or change mihomo TUN at runtime. Enable requests automatic routes, interface detection, strict routing, and UDP/TCP port 53 interception; macOS also manages network-service DNS. Status inspects a named adapter when available. |
| `ports status` | Show HTTP/HTTPS, SOCKS, and mixed listener ports, bind address, and LAN flag. Port `0` means the listener is disabled. HTTPS here means an HTTP CONNECT proxy, not TLS termination. |
| `lan status`, `lan enable [ADDRESS]`, `lan disable` | Read or change runtime LAN access and bind address. `lan enable` defaults to all interfaces (`*`); ADDRESS must be an IPv4 or IPv6 address. Disable binds to `127.0.0.1`. |

`system-proxy` uses `networksetup` for active macOS network services. On Linux, all `system-proxy` subcommands return `system_proxy_error` with exit status `1`; configure individual applications to use the listener shown by `nagi ports status`. Old Linux GNOME restore records are ignored by lifecycle commands; proxy settings left by an older Nagi version must be reset in the desktop settings. On macOS, saved settings are held in a mode `0600` restore record under Nagi's state directory. A successful configuration apply updates managed HTTP/HTTPS ports to the newly active mixed or HTTP listener while retaining the original restore baseline. A service changed externally is reported as a conflict and is not overwritten. `stop` and `restart` restore them before stopping mihomo. A detached watcher restores them after an unexpected engine exit, and `status` retries restoration when it finds the engine stopped. If restoration fails, the record remains for `system-proxy disable` to retry. These commands cannot restore settings after the user removes the restore record or if the watcher and Nagi are both unable to run. `startup check`, including checks made by the installed login service, compares managed settings with both the saved baseline and Nagi's target. When an individual known macOS service's settings exactly revert to the baseline, it reapplies Nagi's target. A different value is reported as a conflict and left untouched. New macOS services are reported as untracked; removed services are reported as missing. Disable and re-enable the system proxy to include a newly active service. Existing restore records without a saved target are reported as `legacy_journal` and are not automatically reasserted. A user deliberately restoring the exact original settings cannot be distinguished from an OS reset and may be reasserted while the login service is active.

`dns tun on|off` persists profile-local settings but cannot contradict an explicit saved TUN preference: a conflicting request returns `dns_error` before changing the override. With mihomo running, run `nagi tun enable` before `dns tun on`, or `nagi tun disable` before `dns tun off`. When stopped, start the engine with its current preference first.

`tun enable` needs OS permission to create a TUN device and install routes. macOS and Linux permission mechanisms differ; inspect `nagi logs` if the adapter does not start. A successful `tun enable` also saves an enabled preference shared by all profiles. `tun disable` saves an explicit disabled preference. Nagi applies this preference to the effective configuration before validation and activation on startup, restart, reload, profile changes, and explicit or automatic subscription application. Enabled intent supplies automatic routes, interface detection, strict routing, and UDP/TCP port 53 interception while retaining other TUN settings; disabled intent overrides `tun.enable` to `false`. Without a saved preference (including a legacy empty preference file), profile TUN settings retain their existing meaning. Source profiles and subscription caches are unchanged. If mihomo does not report the requested TUN enable state after configuration activation, the command reports failure and attempts the ordinary runtime and disk rollback. A startup mismatch attempts to stop the newly started engine. If a saved enabled preference cannot start because TUN permissions are unavailable, fix those permissions (or use the macOS privileged helper) before starting again; `tun disable` requires a running engine. The mihomo API may accept a TUN patch while logging an adapter creation error, so a reported `enabled` setting alone does not prove that interception works. `tun status|enable|disable` also reports `adapter_status`: `inactive` when TUN is disabled, `unknown` when no device name is exposed by mihomo or OS interface enumeration fails, and `missing`, `down`, or `up` for a configured `tun.device` name. `adapter_name` is present only when mihomo exposes that name. An up adapter does not prove route ownership or DNS capture. The saved TUN preference survives profile reloads and engine restarts. Runtime LAN changes are reset when a profile is reloaded or mihomo restarts. Enabling LAN exposes configured proxy listeners at the selected address; use firewall rules and authentication appropriate to that network.

On macOS, a successful TUN activation waits for an up adapter and enabled mihomo DNS, then points network-service DNS at the addresses derived from the running TUN prefixes. For example, `198.18.0.1/30` supplies `198.18.0.2`; an IPv6 prefix supplies its corresponding next address. Nagi records original static DNS lists or DHCP state before changing them. The privileged backend uses the root helper and a root-private `tun-dns-restore.json`; the user backend uses a private journal under Nagi's state directory and requires permission to change network-service DNS. Prefer the privileged helper when those permissions are unavailable. A failed runtime TUN change attempts to restore the previous TUN and DNS state and reports incomplete recovery explicitly.

TUN disable, engine stop, quit, restart, and unexpected engine exit restore the original DNS settings. Configuration activation reconciles DNS with the actual TUN state and addresses. The helper also reconciles configurations that finish applying after a caller timeout. Newly discovered services retain their own baseline. External DNS edits are reported as conflicts and left untouched; missing services and failed restoration retain the journal for retry. Restore the affected service to its reported original settings or Nagi's target before retrying `nagi tun disable` or `nagi stop`. This management does not change application-specific encrypted DNS or guarantee capture of every other VPN resolver.

On macOS, an installed privileged helper can run mihomo with permission to create the TUN adapter. Install it from a root-owned `/usr/local/bin/nagi` using `sudo /usr/local/bin/nagi privileged-helper install`. Stop the user engine, run `nagi privileged-helper enable`, then `nagi start`. `privileged-helper status` reports `service` (`path`, `manager`, `installed`, `active`, `executable`), `selected_backend` (`user` or `privileged`), and `available`. `enable` and `disable` return `selected_backend` and `changed`. Switching is refused while the outgoing backend's mihomo process is running. The selected backend is persisted in the private `engine-backend` file in Nagi's configuration directory; an absent file means `user`. The app continues to use the same CLI commands. In privileged mode, lifecycle, logs, traffic samples, and control operations use the authenticated helper socket; mihomo's root-only controller is not exposed to the user. Privileged starts and reloads accept inline proxies and providers, HTTP providers, and file providers or proxy TLS files located under the session user's Nagi configuration directory. Relative file references resolve beside the source profile. The helper copies those files into root-private snapshots, runs mihomo restricted-policy validation, and rejects unsupported fields and unsafe paths before activation. A reload requires a 32-character lowercase hexadecimal `activation_id`; if its request times out, activation state may remain uncertain until queried through `/configs`. The helper retains snapshots until the child exits. A matching restricted-policy mihomo executable is required. Restricted mode accepts a string-valued Tailscale `dialer-proxy`, including the explicit empty value for a first hop; native parsing still validates references and cycles.

Helper protocol version 1 adds optional `not_forwarded` to failed configuration reload responses. `true` means the helper rejected the request before controller forwarding; it requires `ok: false`, a nonempty `error`, and no HTTP `status`. Nagi reports this rejection directly and attempts disk restoration without warning that this PUT may apply later. Omitted or false values provide no such evidence; transport failures and failures after forwarding retain activation-ID recovery. Older helpers remain supported with that conservative behavior. The updated CLI, matching engine, and restarted helper must be installed together; the [installation workflow](../development/install.md) updates an existing macOS helper as part of replacing the binaries.

On macOS, JSON payloads for `system-proxy status|enable|disable` include `enabled` (boolean), `backend`, and `http`, `https`, and `socks` objects with `enabled`, optional `host`, and optional `port`. `tun status|enable|disable` include `enabled` (boolean), `settings` (mihomo TUN object), `adapter_status` (string), and optional `adapter_name` (string). `lan status|enable|disable` include `enabled` and `bind_address`. `ports status` includes integer `http`, `https`, `socks`, and `mixed`, plus `bind_address` and `allow_lan`. The `http` and `https` values are the same because mihomo uses one HTTP proxy listener for both schemes. On macOS, TUN results also include `system_dns` and an optional `system_dns_error` when inspection fails. `system_dns` contains `supported` and `managed` booleans, `status` (`unmanaged`, `managed`, `attention`, or `stale`), optional `targets` (DNS address list), `services` (service names mapped to their static DNS lists; an empty list means DHCP), and optional `conflicts` (service names). Non-macOS DNS management reports `unsupported`. All use the usual success/error envelope and exit codes. `system_proxy_error` and `tun_error` indicate failed traffic operations. macOS `status` and `startup check` also expose `system_dns` and optional `system_dns_error`; `status` can report `system_dns_recovery_error` from an unsuccessful user-mode exit watcher. These errors do not replace the process's `running` state or PID.

## Installation maintenance

These commands require an installation record created by the release/local
artifact installer, except `app update-error`. They find the prefix from the
running `PREFIX/bin/nagi`;
they do not use `NAGI_MIHOMO_BIN` to select files for replacement. They do not
require a valid active profile merely to display help or manage the App.

Mutating maintenance also uses an independent per-prefix file transaction
journal. The installer recovers an interrupted transaction before reading its
installation record: uncommitted changes are rolled back, while committed
changes are retained and cleanup is completed. Recovery is safe to repeat.
A damaged journal or incomplete recovery stops maintenance and preserves recovery
data. If the installed CLI is missing or cannot execute, invoke a verified
release's artifact installer for the same prefix; see the
[recovery workflow](../development/install.md#install-update-and-remove).
Read-only installer verification does not recover an interrupted transaction. Ordinary
engine start/restart, configuration reload, and startup activation return
`installation_recovery_required` while a journal is pending; read-only App
status also reports pending recovery. Use the artifact installer's `recover`
action; `nagi recover` continues to clean stale runtime markers only. Recovery
after a hard crash does not automatically restore services or restart mihomo.

After a successful update, an already-open terminal menu can still find the
installed mihomo; exit and reopen it to use the new CLI version. See the
[maintenance design](../design/installation.md#transactions-and-recovery).

| Command | Release selection and effect |
| --- | --- |
| `app status` | macOS only. Read the managed App path and report whether that directory exists; no download. |
| `app update-error` | macOS only. Read and clear the last persisted, sanitized background-update failure. No installation record or running engine is required; no download. |
| `app install [TAG]` | macOS only; requires `/usr/local/bin/nagi` and no recorded App. Without TAG, download the release matching this CLI's version. Install only `/Applications/Nagi.app` and update its installer record. CLI, mihomo, profiles, engine state, and engine login service are preserved. A development CLI without a release version requires an explicit TAG. |
| `app update [TAG]` | macOS only; requires a recorded App. Run the same whole-installation operation as `update`. |
| `app uninstall` | macOS only; remove only the recorded App and clear its installer record fields. Keep the managed CLI and mihomo and user data. An absent recorded App is a successful no-op. No download. |
| `update [TAG]` | Download latest by default, or the specified release. Verify the platform archive checksum, installation record, paths, and candidate executables, stop Nagi, temporarily remove its engine login service, and update the CLI, mihomo, and any recorded App. Modified recorded files are replaced. Refresh the built-in default profile. Restore engine login service installation/enabled preferences and restart an engine that was running before preparation. A CLI-only installation remains CLI-only. |
| `update --terminal` | macOS only; requires a recorded App. Open Terminal and schedule `nagi update` after the App exits; reopen its recorded path after success. Wait at most 60 seconds for App exit. Returns after launching Terminal; the caller must quit the App. No tag argument can accompany this option. |
| `update --app` | macOS only; requires a recorded App. Download the latest release, verify it, and obtain any required native administrator authorization before returning. Hand the prepared package to a detached updater that waits at most 60 seconds for App exit, installs it, and reopens the recorded App. After failure, persist a sanitized error and attempt to reopen the App. Returns before installation completes; the App uses this mode for About → Update. No tag argument can accompany this option. |
| `uninstall [--purge-data]` | Stop Nagi and remove its engine login service, recorded CLI, mihomo, App, privileged helper, and installation record, even when recorded files have changed. By default preserve user profiles, subscriptions, settings, and logs. `--purge-data` also removes Nagi's per-user configuration, data, state, runtime, and cache directories. No download. |

TAG accepts `vX.Y.Z` or `X.Y.Z`, with an optional alphanumeric/dot/hyphen release
suffix introduced by `-` or `+`. Invalid arguments exit `2` with `usage`.
Downloads use `nagi-OS-ARCH.tar.gz` (`OS` is `linux` or `macos`; `ARCH` is
`amd64` or `arm64`) and `SHA256SUMS` from `ronigooja/Nagi` GitHub Releases.
An installed/requested App requires an App in that release archive. Archive
paths escaping extraction, duplicate files, links, and special files are
rejected. Compressed and extracted archives are each limited to 512 MiB.

Quit the App before updating or removing it; disable its App login item before
removal. Run as the intended user rather than through `sudo`. The installer
requests administrator authorization when required, so use a terminal when
credentials must be entered for ordinary terminal maintenance. Background App
updates use native macOS administrator authorization before the App quits;
Nagi does not collect the password. The App's About Nagi panel offers Update,
shows a short informational prompt, and uses ordinary quit cleanup only after
the CLI has prepared the release and launched the authorized updater. It keeps
the App available with a menu-top error if preparation, authorization, launch,
or quit fails. No download or installation progress is shown. After an
installation failure, the updater attempts to reopen the App; the App consumes
the persisted error through `app update-error` and retains it across routine
refreshes until another explicit Update attempt. Details, including a failed
reopen attempt, remain in `~/Library/Caches/Nagi/update.log`.

`update --app` preparation has a 14-minute overall deadline and responds to
interrupt (`SIGINT`) and termination (`SIGTERM`) signals before handoff. A timeout
or interruption cancels preparation, releases its maintenance resources, and
returns an `installation_error` without stopping the engine or requesting App
quit. The App allows 15 minutes for this CLI call. Once a scheduled result is
returned, the detached updater owns the installation; the preparation deadline
no longer applies.

Success exits `0` and uses the standard JSON envelope. `app status` returns
`app` (recorded path or empty string), `installed` (directory exists), and
`managed: true`. `app update-error` returns `error` (a sanitized string, empty
when no failure is pending) and removes the pending failure after reading it.
Human output labels a pending message as `Last background update error:` or
reports `No background update failure is pending.` when empty.
App install/uninstall return `action` (`app-install` or
`app-uninstall`), `completed: true`, and `app` (destination/removed path or empty
string for an absent App). Whole updates return `action: "update"`,
`completed: true`, and `engine_restarted` (boolean). Whole removal returns
`action: "uninstall"`, `completed: true`, and `user_data_removed` (boolean).
Background scheduling returns
`action: "update"`, `scheduled: true`, and `background: true`. This confirms
that download, verification, required authorization, and updater handoff
succeeded; it does not confirm installation completion.

Operational failures exit `1` with `installation_error`, including unsupported
platforms, missing or invalid installation records, unsafe managed paths, download or
checksum failures, authorization/installer failures, running Apps, and another
maintenance operation for the same user. Download and preflight failures occur
before stopping Nagi. After preparation, a failed transaction leaves the engine
stopped and attempts to restore engine login preferences; errors include the
installer's rollback details and any restoration failure. A failure to clean up a durably committed file transaction reports that the new
files remain installed and cleanup is pending. A storage failure that leaves
the commit outcome uncertain preserves the journal and backups, skips
compensation, and requires explicit recovery before runtime activation. A successful file
update followed by a service/restart failure is reported as partial completion
with a recovery command. If user data cleanup fails after `uninstall --purge-data`
has removed the managed installation, the error states that Nagi was removed,
some user data may remain, and login service restoration was skipped. See the
[installation workflow](../development/install.md#installed-cli-maintenance)
and [maintenance design](../design/installation.md) for recovery and runtime
setting implications.

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
| `startup enable` | Install the login service if missing and enable login startup; retain an existing definition and do not reload an active monitor. |
| `startup disable` | Disable next-login startup while retaining the definition, current monitor, and running mihomo. An absent definition is a no-op. |
| `startup check` | Recheck PID/socket state and `/version`, and reassert managed system proxy settings only when they reverted to the saved baseline. Remove stale runtime markers after an unexpected exit. It never silently starts mihomo. |
| `quit` | Stop this session's monitor, restore Nagi-managed system proxy settings, and stop mihomo while preserving login startup preference. Returns `quit: true`. |
| `recover` | Remove stale PID and socket markers after an unexpected exit. A live process is never touched. |

`start` is duplicate-safe: a live PID returns `already_running` while recording the desired running state. A stale PID is reported as an unexpected exit and can be repaired with `recover` or `startup check` before starting again. `start` resumes an enabled but inactive login monitor after a previous `quit`. `stop` records an explicit stopped state before restoring managed macOS system proxy settings and stopping mihomo. It still returns `not_running` when there is no live process; the stopped intent is recorded even in that case. `restart` records a running state. The lifecycle lock serializes these state transitions with monitor recovery and `startup check`; an outer quit lock keeps user lifecycle commands from interleaving with a full quit. Login service support is platform-specific: macOS uses a per-user launchd Agent and Linux uses a per-user systemd unit. Missing service-manager sessions return an actionable manager error.

The installed login service runs a foreground Nagi monitor. It checks the process and control API every 30 seconds, and on macOS also checks managed system proxy settings; after a crash it removes stale runtime markers and attempts to start mihomo again. Failed checks or starts use exponential delays up to five minutes. The systemd unit restarts the monitor after a monitor failure; launchd uses `KeepAlive`. Disabling login startup changes only next-login activation: the current monitor, subscription scheduler, and mihomo keep running. Linux uses `systemctl --user disable` without `--now`; macOS uses `launchctl disable` without unloading the job. macOS enablement is read from the installed definition and the launchd disabled-service preference, separately from current job activity. Uninstalling the service stops the monitor and mihomo without changing the desired state. The monitor restarts mihomo only when the desired state is running. An explicit stopped state persists in Nagi's state directory until `start` or `restart`. `quit` pauses the monitor for this login session. Once cleanup succeeds, a new login monitor resumes running intent at the next login; a later `start` resumes it in this session if login startup remains enabled. If quit fails, the intent remains stopped and the CLI reports the failed step; retry `quit` or run `start` after resolving the failure. A live process with an unreachable control API is reported in service logs and is never killed automatically. `start`, `stop`, and `restart` return `lifecycle_busy` while quit is in progress. If the quit process was interrupted and left a pending marker, run `nagi quit` again to finish cleanup; it is safe to repeat after the original process has ended. The periodic check also runs after network changes and sleep/wake, but it does not prove routes or DNS interception were restored. `startup check` remains a manual, non-restarting check. Its JSON payload includes `checked`, `running`, and `control_api`; on macOS, when running and reachable, it adds `system_proxy` with `managed`, `status`, and `reasserted`, `conflicts`, `untracked`, and `missing` arrays. Status is `unmanaged`, `current`, `reasserted`, `attention`, `stale_journal`, `legacy_journal`, or `backend_changed`. An OS read or write failure appears as `system_proxy_error` in the completed macOS check result, which still exits `0`; scripts must inspect that field. Applications should inspect these fields instead of assuming that an active service means an active proxy.

Direct artifact updates and uninstalls require `service uninstall` before
replacing or removing executables, including when the service is disabled or
inactive. `startup disable` retains the definition and current monitor, so it
does not satisfy that preflight. See the [artifact installation workflow](../development/install.md#install-update-and-remove)
for preparation and restart steps. The installed `nagi update` command handles
service removal and preference restoration automatically.

## Diagnostics and security

| Command | Result or effect |
| --- | --- |
| `diagnostics` | Run a redacted report covering paths, executable, selected profile readability, process and Unix Socket API state, listener ports, DNS, proxy listener exposure, controller exposure, and permission checks. |
| `diagnostics export ABSOLUTE_FILE` | Write the same redacted report to an owner-only JSON file. Profile contents, subscription URLs, credentials, and command output are never included. |

`doctor` remains the compatibility diagnostic with its documented eight checks. `diagnostics` adds security and connectivity checks and distinguishes a live PID, reachable Unix Socket API, configured listener, reachable listener, DNS enabled state, and controller exposure. A configured proxy or live control API does not prove that a request can traverse a proxy; use `proxy delay` for an explicit path test.

The diagnostic export is redacted by construction and written mode `0600`. It reports paths and actionable messages but never copies profile YAML or subscription metadata. Any URL in a report retains only its origin; path, query, fragment, and userinfo are removed so tokens embedded in unusual URL fields do not enter a shareable report. Socket and configuration directory permissions are checked for group/other access. A non-loopback external controller or LAN listener produces a warning or error with a corrective action.

## Subscription scheduling metadata

`subscription schedule NAME SECONDS|off` persists an update interval from 1 through 31,536,000 seconds (one year), or disables scheduling with `off`. New subscriptions default to 3,600 seconds. Entries created by older Nagi versions without schedule metadata migrate to `off`. Run `nagi subscription watch` in the foreground or use the installed login monitor to host the scheduler. Only one scheduler runs per user; `scheduler_running` requires its live host lock and fresh heartbeat. The scheduler wakes at the next due time, polling at least once a minute for schedule changes, and downloads due subscriptions without holding `config.lock`, and commits cache/index changes under a short configuration lock. A completed download for an unselected or unassociated subscription updates only its cache. `subscription list` adds schedule, next due, cache/applied version, profile association, and redacted failure fields while continuing to omit URLs and credentials. A foreground watch prints a running message in text mode or one JSON success envelope (`scheduler_running: true`) when acquired. A second `subscription watch` exits 1 with `scheduler_already_running`; interrupting the foreground host releases the scheduler lock.

Scheduler failures use fixed `last_failure_stage` and `last_failure_reason` values. `fetch/network_error` covers download failures; `decrypt/identity_unavailable` covers private identity or decryption failures; `validate/invalid_content` covers an unsupported or invalid feed, including normalization of inbound
listener configuration. Stages come from typed source failures; external
diagnostic words do not determine them. `apply/dns_target_missing`, `apply/custom_rule_conflict`, and `apply/selection_conflict` identify local settings that cannot be retained. Other activation or validation failures use `apply/activation_failed`. Effective-configuration composition failures also use `apply/activation_failed`; they are not classified as custom-rule conflicts. Missing proxy dependencies identify both the referring node, group, or provider and the missing outbound or provider. Human subscription errors show `apply subscription failed: "apps" references missing outbound: "removed-node"`, without custom-rule repair advice. URL-shaped names retain useful context with common credential query and fragment values masked. These preflight failures happen before source persistence or runtime activation. `pause_reason` persists as `profile_changed` or `source_unreadable` until explicit apply. During the pause, a failed download is visible through `last_failure_stage` and `last_failure_reason`; a later successful download returns those fields to `paused` and the pause reason. URLs, credentials, and feed contents are not stored in these fields.

A successful refresh creates a cache version independently of the applied version. A failed refresh may still retry a previously validated pending cache when the selected profile and source remain eligible. Automatic apply preserves local overrides, persistent subscription chain policy, enabled custom rules, and saved group choices that still exist; removed choices are pruned. Rule targets are checked against the candidate source plus that profile's local override and custom rules; rules owned by other profiles do not participate. Known inline-provider nodes are included; remote-provider node names cannot be determined statically. Live mihomo group options are authoritative for choice pruning. When the engine or control API is unavailable, pruning uses the effective profile; choices for a group using a remote provider remain until mihomo can verify them. Invalid DNS proxy targets, missing custom rule targets, failed validation, or failed activation leave the new cache pending and report a redacted failure reason. External source edits or an unreadable source pause automatic apply while scheduled downloads continue to refresh the cache and advance `next_due`. A download failure advances `next_due` to a retry time and remains visible alongside `pause_reason`. Restoring the old source bytes does not resume automatic apply; explicitly run `subscription apply NAME` after reviewing the source to resume. `schedule off` disables both downloading and automatic apply. Applying a cached subscription records the cache version, profile association, and a source hash only after activation and saved-choice replacement succeed. Failed activation leaves the new cache available for retry and records only a fixed failure stage and redacted reason. Manual profile import or profile removal clears a subscription association after the profile operation succeeds; `profile use` leaves associations unchanged.

## TUN request rewrite

`rewrite` (also available as `request-rewrite`) manages profile-local ordered request, response and WebSocket message transformations. It defaults to off. Enable with at least one enabled rule requires a compatible mihomo v2 capability, an actually running TUN, a generated local CA and confirmed platform trust. With no enabled rules it confirms teardown and retains enabled intent in `idle`; enabling the first rule then checks those prerequisites. Generation does not install trust, and enable does not start TUN or install trust. The published engine commit in `engine.lock` provides this capability for matching source builds. Availability through ordinary installation or `nagi update` requires a matching Nagi release; older engines return an actionable capability error. Ordinary HTTP/SOCKS listeners are outside the interception path. Unmatched domains and unsupported traffic are not evidence of interception; certificate pinning, client certificates, ECH and protocol limitations can prevent coverage. See the [request rewrite design](../design/request-rewrite.md#traffic-and-ownership) for the engine/service boundary and coverage limits.

| Command after `nagi rewrite` or `nagi request-rewrite` | Result or effect |
| --- | --- |
| `status` | Confirm saved intent, actual runtime state, versions, CA and recovery guidance. |
| `enable`, `disable` | Coordinate runtime activation or teardown for the selected profile. Disable retains rules, CA and trust. |
| `scripts list [--profile NAME]` | List script IDs, body access, source sizes and referencing rule IDs; omit source. |
| `scripts show ID [--profile NAME]` | Show one script summary. |
| `scripts source ID [--profile NAME]` | Explicit sensitive read of complete source; terminal output preserves source bytes, JSON returns `sensitive:true` and the complete `script` object. |
| `scripts add FILE\|- [--profile NAME]` | Add one `{id,body_access,source}` JSON object. `body_access` is `none` or `buffer`; source is limited to 64 KiB. |
| `scripts update ID FILE\|- [--profile NAME]` | Replace a script with the same ID and validate all referencing rules. |
| `scripts remove ID [--profile NAME]` | Remove a script only if no enabled or disabled rule references it. |
| `rules list [--profile NAME]` | List ordered redacted rule summaries. |
| `rules show ID [--profile NAME]` | Show one redacted summary. |
| `rules add FILE\|- [--profile NAME]` | Append one complete rule JSON object. |
| `rules update ID FILE\|- [--profile NAME]` | Replace one complete rule; document ID must equal `ID`. |
| `rules remove ID [--profile NAME]` | Remove one rule. |
| `rules enable ID [--profile NAME]`, `rules disable ID [--profile NAME]` | Toggle a rule without enabling the feature. |
| `rules reorder FILE\|- [--profile NAME]` | Read a JSON array containing every rule ID exactly once, including disabled rules. |
| `import FILE\|- [--profile NAME]` | Validate and replace the complete document transactionally. Require imported `enabled:false`; retain the existing intent independently. |
| `export FILE\|- [--profile NAME]` | Explicit sensitive raw JSON export, always with `enabled:false`; exclude CA and runtime secrets. |
| `validate [FILE\|-] [--profile NAME]` | Validate a supplied complete document or the stored document, without activation, persistence, DNS or forwarding. |
| `preview FILE\|- [--profile NAME]` | Explicit sensitive transformation of a local staged fixture without network or persistence. |
| `ca status` | Report public CA metadata and assessed OS trust. |
| `ca generate` | Explicitly generate one per-user CA; refuse an existing CA. |
| `ca export FILE\|-` | Export only the public PEM certificate. |
| `ca trust install`, `ca trust remove` | Explicit platform trust operations, returning confirmed trust or an actionable unknown/unsupported error. |

`--profile NAME` defaults to the selected profile and applies only to scripts, rules, import, export, validate and preview. Place it after the complete command path, before or after positional arguments. Status, enable, disable and CA commands reject it. IDs and profile names contain 1–128 ASCII letters, digits, `_` or `-`. There are no `set`, `move`, CA rotation, replacement or deletion aliases.

Both command names accept the complete command tree, flags and private file/stdin inputs, and produce the same output and exit codes. Existing integrations may continue to use `request-rewrite`. JSON command fields and usage-error guidance retain the compatibility name `request-rewrite`; requested help uses the invoked name.

Input `FILE` is a regular local file owned by the invoking user with no group/other permissions; symbolic links are rejected. `-` reads stdin. URLs and secret values in command arguments are not accepted. Script/rule/reorder/import/validation input is limited to 1 MiB. JSON rejects unknown fields, duplicate object keys, trailing values and invalid UTF-8. Missing/incomplete input fails before a save. Rule and script mutations validate the complete candidate and use the configuration/document locks and coordinator to confirm activation or rollback. A disabled profile can be edited while the engine is stopped; editing never starts TUN or the rewrite child. An activation failure does not claim success or confirmed rollback when the resulting runtime state is uncertain.

Complete document and rule field definitions, operation constraints and limits are authoritative in the [v2 document schema](../design/request-rewrite.md#document-and-limits). The document has `version:2`, `enabled`, `rules` and `scripts`. Each rule specifies `stage` (`request`, `response`, `websocket_client` or `websocket_server`), `match`, `on_error`, destination consent and ordered operations. `range_mode` defaults to `fragment`; `full` is an explicit full-representation policy. Full rule JSON inputs support every match predicate and operation, including headers, URL/method/status, body replacement, literal/RE2 replacement, JSON Pointer, trailers and local scripts. Enabled rule domains determine interception scope. Exact ASCII DNS names and explicit `*.service.example` wildcards are supported; a wildcard excludes the apex. Destination scheme/host/port changes require explicit `redirect_consent` from every executed request rule contributing to the final target change. Target-only domains need no entry rule. Ordinary rule reads include stage/range policy, redacted match predicates and operation kinds; operation values and redirect consent are omitted. Script summaries omit source. Use explicit export or `scripts source` to read sensitive values.

Supply a rule through stdin to keep secret-bearing values out of process arguments. Use a private input file when a shell would retain pasted here-document input in history (the example values below are fictitious):

```sh
nagi rewrite rules add - <<'JSON'
{"id":"edit-api","enabled":true,"stage":"request","match":{"domain":"api.service.example","methods":["GET"],"path_prefix":"/v1/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"header_set","name":"X-Demo","value":"example"}]}
JSON
nagi rewrite rules list
nagi rewrite validate
```

Preview accepts a staged envelope `{stage,request,response?,message?}`. `stage` is `request`, `response`, `exchange`, `websocket_client` or `websocket_server`. The `request` object requires `{scheme,host,port,method,path,query,headers,body_base64}`; `trailers` is optional. `scheme` is `http` or `https`; normalized `host` must fall within enabled rule scope; `port` is an integer 1–65535; headers/trailers map names to string arrays. `body_base64` is required, with an empty string for no body, and uses strict base64. Path/query obey URL validation. Response and exchange stages require `response:{status,headers,body_base64}` with optional `trailers`. WebSocket stages require a status-101 response and `message:{type,body_base64}`, where type is `text` or `binary`. Request previews omit response/message. The encoded fixture is limited to 24 MiB, each decoded body/message to 8 MiB, headers to 64 KiB and trailers to 16 KiB. Preview makes no DNS/network call and does not prove live interception coverage.

Results include `sensitive:true`, `stage`, `rule_version`, contributing `rule_ids`, `outcome`, `request`, per-stage `stages` entries (`stage`, `rule_ids`, `outcome`) and the applicable `response`/`message`. JSON wraps this sensitive result inside the normal `data` envelope; terminal preview output begins with a sensitive-output label.

```sh
nagi --json rewrite preview - <<'JSON'
{"stage":"request","request":{"scheme":"https","host":"api.service.example","port":443,"method":"GET","path":"/v1/demo","query":"","headers":{},"body_base64":""}}
JSON
```

Rule exports, script source and previews may contain credentials, complete URLs and request bodies. They are explicit sensitive operations and are never included automatically in diagnostics. Both rule and public CA exports refuse existing destination paths and create `0600` files; `-` writes raw bytes to stdout. Raw exports reject `--json`, including file destinations. Public CA export excludes the private key. Disable retains CA/trust, and trust removal retains the key. CA generation/status/export do not imply trust. macOS trust install/remove target the login keychain and verify with a fresh local security-tool operation; native macOS acceptance remains unverified. Linux assessment rereads the available system root bundles/directories (including standard `SSL_CERT_FILE`/`SSL_CERT_DIR` overrides), and reports trusted only for that store. Linux trust install/remove return manual export/distribution/application instructions; they never install OS trust automatically. Applications with independent certificate stores need their own explicit trust configuration. Trust removal first tears down rewriting while retaining enabled intent.

Status data includes `config_version:2`, `profile`, `enabled_intent`, `actual`, `configured_rule_version`, optional `active_rule_version`, `generation`, `engine_epoch`, nullable `tun_running`, `backend`, nullable `service_running`, nullable `protocol_version`, `ca`, nullable redacted `last_error`, and recovery guidance when available. `actual` is `disabled`, `idle`, `starting`, `active`, `blocked`, `draining`, `unavailable` or `uncertain`. Missing runtime evidence is `unavailable`/null, even if saved intent is false; neither intent nor a PID proves activation. When child health is available, `counters` contains `requests`, `responses`, `messages`, `rewritten`, `stage_hits`, `transform_failures`, `fallbacks`, `resource_exhaustion`, `script_timeouts` and `websocket_abnormal_closes`. Counts are nonnegative integers for that child generation; a recovered original fallback increments both its failure category and `fallbacks`. CA metadata uses `state`, `trust`, optional public fingerprint and RFC 3339 validity timestamps. Human timestamps omit seconds. Script/rule mutation results include `{profile,saved,configured_rule_version,active_rule_version,actual}`. List/show include profile and configured version; validation returns `valid:true` and version, or fails with a redacted stable reason. Invalid supplied documents do not return success.

JSON and exit behavior follow the [common interface](#json-and-exit-codes): success stdout, failure stderr, exit `0` success, `2` usage, `1` operation failure. Rewrite operation errors use `request_rewrite_error` with a stable sanitized reason, such as `rewrite_invalid`, `rewrite_limit`, `rewrite_tun_required`, `rewrite_ca_required`, `rewrite_capability_missing`, `rewrite_activation_failed` or `rewrite_state_uncertain`, plus a recovery command. Help and generated Bash/Zsh/Fish scripts require no valid profile or engine. Completion offers command paths, `--profile`, local files/`-`, profile names, and local rule/script IDs through `completion candidates rewrite-rules [PROFILE]` and `completion candidates rewrite-scripts [PROFILE]`; these ID sources contact no engine and expose no rule values.

### Original request capture and capture-derived drafts

`nagi rewrite capture` and `nagi request-rewrite capture` share the following
commands and the existing `--json` success/error envelope:

```text
capture check
capture start [FILE|-]
capture status
capture pause SESSION
capture resume SESSION
capture stop SESSION
capture apps
capture list [FILE|-]
capture show RECORD
capture body RECORD SNAPSHOT OFFSET [LIMIT]
capture clear SESSION
capture export FILE [native|har]
capture import FILE|-
capture fixture RECORD STAGE
capture draft RECORD FILE|-
rewrite draft preview FILE|- [--profile NAME]
rewrite draft save FILE|- [--profile NAME]
```

The terminal menu opens capture under **Rewrite & Capture → Request Capture**. It dispatches these CLI commands for scoped start,
application selection, paginated records, original content, session controls and
archives. A record can produce a draft for complete JSON editing, full-chain
local preview and version-checked save; advanced JSON and automation remain
available through the CLI.

`capture check` is a read-only readiness query independent of the recorder. It
checks the selected profile, engine reachability, ordinary-user execution,
rewrite/capture capabilities, running TUN, valid CA and confirmed certificate
trust in that order. Each failed stage stops the check immediately; later stages
are not inspected. A successful query exits `0` even when capture is not ready,
returning `data.readiness: {ready, code?, message}` in the JSON success envelope
and `sensitive: false`. `code` identifies only the first unmet prerequisite and
is absent when ready. Invalid arguments exit `2`; ordinary command/runtime
setup failures retain the existing error envelope and exit behavior. It accepts
no positional arguments or `--profile`, and never starts the recorder or changes
TUN, CA or trust. Opening the terminal capture page or App window invokes this
query and displays its single message without setup actions. The App's **Recheck**
button beside an unmet-prerequisite message repeats the check; manual **Refresh**
and automatic record polling do not. Reopening the menu page or App window
checks again. Start still validates prerequisites independently.
`capture status` succeeds without a `status` field when no recorder socket exists.

The `capture` lines above follow either rewrite alias. Capture uses the selected
profile; it rejects `--profile`. Start requires a running TUN engine advertising
`capture_protocol:1` and an explicitly generated, trusted rewrite CA. It never
starts TUN, generates a CA, or installs trust. Rules may remain globally disabled:
capture suppresses their transformations while retaining the configured rule
version. Initial attachment creates a rule generation; pause, resume and stop
update capture policy without replacing the generation or terminating open
streams when all runtime components agree. If a policy update fails, the command
reports uncertainty without retrying the mutation. `status` compares recorder
state with the selected profile/backend, engine generation/epoch, active branch,
TUN state and child policy. A mismatch reports `state: "uncertain"`, preserves the
recorder state in `recorder_state`, and supplies `recovery`. Pause/resume reject
this state. `stop SESSION` stops local collection and tears down an uncertain
runtime generation while retaining history; explicitly re-enable rewrite after
recovery if needed. Failed initial attachment also attempts runtime teardown.
Rule changes retain recorder history. Profile/backend changes and
TUN/engine exit stop collection; engine restart does not resume it.

Start reads an optional JSON object from a private file or stdin:

```json
{"owner":"cli","scope":{"domains":["api.fixture.example"],"process_paths":[],"app_paths":[]},"body_limit":1048576}
```

`session_id` is optional and otherwise generated. `profile` and `backend` are
bound to the selected runtime. `owner` defaults to `cli`; a window can supply its
own ownership token and stop only its own returned session. Empty scope arrays
mean unrestricted capture. Populated categories are ANDed; entries within a
category are ORed. Process paths match exactly. Application paths name outermost
macOS `.app` directories and include their helpers. Unknown process identity
does not match process/application restrictions. Domain patterns use rewrite
syntax. Scope categories contain at most 256 distinct entries. Paths must be
absolute and clean; nested `.app` paths are rejected. Start/list JSON rejects
duplicate keys, unknown fields and trailing values. The body limit defaults to 1 MiB per snapshot and is at most 8 MiB.

Starting a session authorizes its original content reads. These commands show
complete URLs, query/header/trailer values and bodies, including credentials;
there is no redacted view or reveal operation. Ordinary diagnostics and errors
continue to omit content. The independent recorder is memory-only, bounded to
1,000 records and 64 MiB aggregate content and event buffering. Closing a CLI
command retains the session. `stop` retains history; `clear` removes the named
session's records and invalidates cursors. `quit` ends the recorder and discards
memory-only history.

`status`, state mutations, list/show/body and import return `capture.Reply` under
`data`: `sensitive`, optional `status`, `records`, `record`, `body`, and
`next_cursor`. Status includes session/revision/state, profile/backend/owner,
scope/body limit, record/byte/drop/truncation counts and `read_only`; uncertain
runtime observations also include `recorder_state` and `recovery`. Lists and
show omit body bytes while preserving all original metadata values. Human
output uses minute precision for record timestamps; JSON retains timestamp
formats. `apps` returns `{apps:[{process,process_path,app_path}],sensitive:true}`
from recorded identities and currently observable engine connections; an empty
list means no process identities were observed.

List accepts optional JSON `{cursor,limit,query,kind,stage,process_path,domain}`.
The default page limit is 100 and the maximum is 200. Search includes original
content. Cursor values are opaque. Stable record IDs identify HTTP exchanges or
WebSocket messages; messages include `parent_id` for their handshake. Snapshot
names are `request_original`, `request_actual`, `response_original`,
`response_actual`, `message_original`, and `message_actual`. Body reads use
nonnegative decoded-byte offsets and return base64 `data`, `offset`,
`next_offset`, `eof` and collection `state`; LIMIT defaults to 256 KiB and may be
1 through 262144. Complete empty bodies, truncation, collection cutoff and
failures remain distinct.

Export streams raw native NDJSON (default) or HAR, rejects `--json`, refuses an
existing file and creates files with mode 0600. `-` writes raw stdout. Native
archives contain a versioned session header, records, body chunks and a required
end marker; HAR retains WebSocket information in named extensions. Import streams
a native archive from a private file or stdin, validates it before replacing the
retained stopped session, and creates an offline stopped/read-only session.
Import during recording or pause fails. Imported history cannot be cleared or
resumed; a new explicit start may replace it. Truncated/invalid imports leave
existing history unchanged.

`fixture RECORD STAGE` returns a local preview fixture from complete original
snapshots. STAGE is `request`, `response`, `exchange`, `websocket_client`, or
`websocket_server`; WebSocket fixtures retrieve their original HTTP handshake.
Incomplete bodies cannot become fixtures. `draft RECORD FILE|-` reads
`{stage,id,operations,match?,position?,enable?,on_error?,range_mode?,redirect_consent?}`
and returns `{base_rule_version,position,rule,fixture?,enable}`. Defaults are
insertion at the end, disabled candidate, `on_error:block`, and an exact original
host/method/path match. Query/header predicates require an explicit `match`.
Incomplete original bodies permit a metadata-derived draft with no fixture;
preview then reports unavailable input instead of inventing body bytes.

Draft preview consumes that versioned insertion, enables the candidate only in
an ephemeral snapshot and runs the full existing chain locally against the
original fixture. It performs no network requests or persistence. Draft save
checks `base_rule_version` inside the store transaction, inserts at the zero-based
`position`, applies `enable` to the candidate rule, preserves global rewrite
intent, and uses ordinary activation and rollback. Conflicts return
`rewrite_conflict` without modifying the stored document. These JSON inputs use
private owner-only regular files or stdin; use stdin for header/body values.
