package command

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/installation"
	"github.com/ronigooja/Nagi/internal/profile"
)

type commandSpec struct {
	path, syntax, summary, detail, example, hint string
	minArgs, maxArgs                             int
}

// Keep command syntax and help in one place. Group entries have maxArgs -1.
var commandSpecs = []commandSpec{
	{"app", "app <status|install [TAG]|update [TAG]|uninstall>", "Manage the macOS menu bar App", "Requires a release-managed CLI at /usr/local/bin/nagi. App install and uninstall change only the App; app update updates the whole installation.", "nagi app install", "", 0, -1},
	{"app status", "app status", "Show managed App installation", "Reads the local installation record; does not contact GitHub or require mihomo.", "nagi app status", "", 0, 0},
	{"app install", "app install [TAG]", "Install the macOS menu bar App", "Downloads and verifies the release matching the current CLI version by default. TAG selects another release. Installs only the App; leaves binaries, profiles, engine, and login service unchanged. Administrator authorization may be required.", "nagi app install", "", 0, 1},
	{"app update", "app update [TAG]", "Update Nagi including the installed App", "Alias of update for an installation that includes the App. Downloads the latest release unless TAG is supplied. Quit the App first.", "nagi app update", "", 0, 1},
	{"app uninstall", "app uninstall", "Remove only the menu bar App", "Quit the App and disable its login item first. Preserves the CLI, mihomo, profiles, and engine service. Administrator authorization may be required.", "nagi app uninstall", "", 0, 0},
	{"update", "update [TAG|--terminal|--app]", "Update the installed Nagi release", "Verifies the release before stopping Nagi. Updates CLI, mihomo, and an already installed App, refreshes the default profile, and restores login service preferences. Restarts a previously running engine. TAG defaults to latest. On macOS, --terminal opens Terminal; --app schedules the update in the background for the menu App. Administrator authorization may be required.", "nagi update", "", 0, 1},
	{"uninstall", "uninstall [--purge-data]", "Remove the installed Nagi release", "Stops Nagi and removes its login service and managed CLI, mihomo, privileged helper, and App. User data is preserved unless --purge-data is supplied. Quit the App and disable its login item first. Administrator authorization may be required.", "nagi uninstall", "", 0, 1},
	{"menu", "menu", "Open the interactive terminal menu", "Browse and run common commands using a line-oriented menu. Requires terminal input and output; use ordinary commands or --json for automation.", "nagi menu", "", 0, 0},
	{"start", "start", "Start mihomo", "Starts mihomo with the selected profile; creates the default profile if needed.", "nagi start", "", 0, 0},
	{"stop", "stop", "Stop mihomo", "Records an explicit stop so the login monitor leaves mihomo stopped; restores system proxy settings managed by Nagi.", "nagi stop", "", 0, 0},
	{"quit", "quit", "Quit Nagi for this login session", "Stops the login monitor and mihomo, restores Nagi-managed system proxy settings, and preserves the next-login startup preference. A later start resumes the monitor.", "nagi quit", "", 0, 0},
	{"restart", "restart", "Restart mihomo", "Restarts mihomo with the selected profile.", "nagi restart", "", 0, 0},
	{"status", "status", "Show process status", "Shows running state, selected profile, PID, and runtime paths.", "nagi status", "", 0, 0},
	{"diagnostics", "diagnostics [export FILE]", "Run security diagnostics", "Checks executable, profile, listeners, DNS, proxy control API, permissions, and controller exposure. Output is redacted.", "nagi --json diagnostics", "", 0, 2},
	{"doctor", "doctor", "Inspect the local installation", "Checks local paths, mihomo executable, selected profile, process marker, and Unix Socket API without changing files. Exit status is zero for a completed report; inspect healthy and checks for problems.", "nagi --json doctor", "", 0, 0},
	{"logs", "logs [lines]", "Show or follow logs", "Shows recent lines or follows newly appended log lines until interrupted.", "nagi logs follow", "", 0, 1},
	{"logs follow", "logs follow", "Follow mihomo logs", "Streams newly appended log lines until interrupted.", "nagi logs follow", "", 0, 0},
	{"traffic", "traffic <watch>", "Watch live traffic", "Streams upload and download rates and cumulative byte counters from the running mihomo control API.", "nagi traffic watch", "", 0, -1},
	{"traffic watch", "traffic watch", "Watch live traffic", "Streams one sample per second from mihomo until interrupted. Text mode prints labeled rates and totals; --json emits one success envelope per sample.", "nagi --json traffic watch", "Start mihomo before watching traffic.", 0, 0},
	{"config", "config <validate|show>", "Inspect or reload configuration", "Validate, print, or safely reload the selected profile. Use `config reload` for a running engine.", "nagi config reload", "", 0, -1},
	{"config validate", "config validate", "Validate the selected profile", "Runs mihomo -t against the selected profile; requires a mihomo executable.", "nagi config validate", "", 0, 0},
	{"config show", "config show", "Print the selected profile", "Prints the selected profile YAML.", "nagi config show", "", 0, 0},
	{"config reload", "config reload", "Safely reload configuration", "Validates the selected profile, then asks a running mihomo instance to reload it.", "nagi config reload", "Start mihomo before reloading.", 0, 0},
	{"profile", "profile <list|use|import|export|backup|restore|diff|override|remove>", "Manage local profiles", "Profile names use letters, digits, _ or -, up to 128 characters.", "nagi profile list", "", 0, -1},
	{"profile list", "profile list", "List local profiles", "Lists available profiles and marks the current selection.", "nagi profile list", "", 0, 0},
	{"profile use", "profile use NAME", "Select a profile", "Validates and activates an existing profile; reloads a running engine.", "nagi profile use work", "Run `nagi profile list` to find a profile name.", 1, 1},
	{"profile import", "profile import NAME FILE", "Import a profile", "Validates and saves a local YAML file, up to 8 MiB. Use profile use to select it.", "nagi profile import work ./work.yaml", "Run `nagi profile list` to see existing names.", 2, 2},
	{"profile export", "profile export NAME FILE", "Export a profile", "Writes the source YAML to a new file with private permissions; refuses to overwrite an existing file.", "nagi profile export work ./work.yaml", "", 2, 2},
	{"profile backup", "profile backup NAME", "Back up a profile", "Copies the source YAML to profiles/NAME.yaml.bak.", "nagi profile backup work", "", 1, 1},
	{"profile restore", "profile restore NAME", "Restore a profile backup", "Validates and writes profiles/NAME.yaml.bak; reloads if selected and running.", "nagi profile restore work", "", 1, 1},
	{"profile diff", "profile diff NAME [OTHER|backup]", "Compare profile versions", "Shows changed lines against another profile or the latest backup (default).", "nagi profile diff work backup", "", 1, 2},
	{"profile override", "profile override <set NAME FILE|show NAME|clear NAME|apply-default NAME [--dns-proxy GROUP]|default-status NAME|restore-default NAME>", "Manage local overrides", "Stores a YAML mapping separately and applies its keys over the source profile.", "nagi profile override set work ./local.yaml", "", 0, -1},
	{"profile override set", "profile override set NAME FILE", "Save local override", "Validates and saves a YAML mapping, up to 8 MiB; reloads the selected running profile.", "nagi profile override set work ./local.yaml", "", 2, 2},
	{"profile override show", "profile override show NAME", "Show local override", "Prints user-owned override YAML.", "nagi profile override show work", "", 1, 1},
	{"profile override clear", "profile override clear NAME", "Clear local override", "Removes the user-owned override and reloads the selected running profile.", "nagi profile override clear work", "", 1, 1},
	{"profile override apply-default", "profile override apply-default NAME [--dns-proxy GROUP]", "Apply local default settings", "Copies safe local runtime settings from profiles/default.yaml onto NAME; subscription nodes, groups, rules, and Nagi-managed fields remain unchanged; an enabled TUN stays enabled. On first use, select a target profile proxy group for default DNS; later uses reuse the saved group unless changed explicitly.", "nagi profile override apply-default work --dns-proxy Proxy", "The default profile and named target group must exist.", 1, 3},
	{"profile override default-status", "profile override default-status NAME", "Show local default settings state", "Reports whether the saved default overlay still matches the profile override.", "nagi profile override default-status work", "", 1, 1},
	{"profile override restore-default", "profile override restore-default NAME", "Restore previous local override", "Restores the local override saved by apply-default; refuses if the override changed since applying.", "nagi profile override restore-default work", "", 1, 1},
	{"profile remove", "profile remove NAME", "Remove a profile", "Removes an unselected local profile and keeps its backup file.", "nagi profile remove old", "Run `nagi profile list` to see profile names.", 1, 1},
	{"subscription", "subscription <list|add NAME URL|key NAME FILE|clear|schedule NAME SECONDS|off|update NAME|preview NAME|apply NAME|remove NAME|watch>", "Manage subscriptions", "Stores URLs and private age identities separately from cached downloads and active profiles.", "nagi subscription list", "", 0, -1},
	{"subscription list", "subscription list", "List subscriptions", "Lists names, refresh/schedule state, cache and applied versions, and profile association without URLs. scheduler_running reports a live scheduler host.", "nagi subscription list", "", 0, 0},
	{"subscription add", "subscription add NAME URL", "Save a subscription URL", "Saves an HTTP or HTTPS URL; does not download or activate it. Names use letters, digits, _ or -, up to 128 characters.", "nagi subscription add work https://example.com/subscription", "", 2, 2},
	{"subscription key", "subscription key NAME FILE|clear", "Set or clear an age identity", "Copies a bounded regular identity file into private storage, or clears it. The next encrypted update uses mihomo to decrypt; plaintext feeds are unaffected.", "nagi subscription key work ./identity.txt", "Run `nagi subscription list` to find a subscription name.", 2, 2},
	{"subscription schedule", "subscription schedule NAME SECONDS|off", "Set a subscription interval", "Saves 1..31536000 seconds, or off. New subscriptions default to 3600 seconds; legacy entries default to off. A running scheduler downloads when due and may apply the current associated profile.", "nagi subscription schedule work 3600", "Run `nagi subscription list` to find a subscription name.", 2, 2},
	{"subscription watch", "subscription watch", "Run subscription scheduler", "Runs foreground updates until interrupted. A second scheduler for this user exits with subscription_error; the login service also hosts this task.", "nagi subscription watch", "Run `nagi subscription list` to inspect due times and failures.", 0, 0},
	{"subscription update", "subscription update NAME", "Download a subscription", "Validates and caches up to 8 MiB, then reports node changes; does not activate it.", "nagi subscription update work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription preview", "subscription preview NAME", "Preview cached node changes", "Compares cached nodes with the existing profile without changing it.", "nagi subscription preview work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription apply", "subscription apply NAME", "Apply a cached subscription", "Replaces the named profile with cached configuration, removes its local override and saved group choices, validates, and selects it. Nagi owns local listeners and controller access.", "nagi subscription apply work", "Run `nagi subscription preview NAME` to inspect node changes.", 1, 1},
	{"subscription remove", "subscription remove NAME", "Remove a subscription", "Removes the saved subscription and its cache.", "nagi subscription remove work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"dns", "dns <status|set|exception|tun|query|flush|check>", "Manage DNS protection", "Configures selected-profile DNS in a separate local override; inspects mihomo DNS through its private control API.", "nagi dns status", "", 0, -1},
	{"dns status", "dns status", "Show selected DNS policy", "Reports configured upstreams, IPv6, TUN, and configuration risks.", "nagi dns status", "", 0, 0},
	{"dns check", "dns check", "Check DNS configuration and mihomo API", "Inspects the selected effective DNS configuration and queries the running mihomo resolver.", "nagi dns check", "", 0, 0},
	{"dns set", "dns set direct URL [URL...] [--bootstrap IP_DOH_URL ...]|proxy NAME URL [URL...] [--bootstrap IP_DOH_URL ...]", "Set encrypted DNS upstreams", "Saves HTTPS DoH URLs. Proxy binds primary DoH to a named non-direct node or group; bootstrap and proxy-node resolution use direct IP-address DoH. --bootstrap overrides the Ali DoH defaults. No plaintext fallback is configured.", "nagi dns set proxy NodeA https://1.1.1.1/dns-query --bootstrap https://9.9.9.9/dns-query", "", 2, 32},
	{"dns exception", "dns exception <add DOMAIN SERVER|remove DOMAIN>", "Manage DNS exceptions", "Exceptions route a domain suffix to an explicit server; plaintext IP servers are allowed only here.", "nagi dns exception add corp.example udp://10.0.0.53:53", "", 0, -1},
	{"dns exception add", "dns exception add DOMAIN SERVER", "Add DNS exception", "Maps a domain suffix to a DoH URL or an explicitly requested plaintext IP server.", "nagi dns exception add corp.example udp://10.0.0.53:53", "", 2, 2},
	{"dns exception remove", "dns exception remove DOMAIN", "Remove DNS exception", "Removes the selected domain suffix from local DNS policy.", "nagi dns exception remove corp.example", "", 1, 1},
	{"dns tun", "dns tun <on|off>", "Configure TUN interception", "Stores TUN auto-route, strict-route, and port 53 hijack settings; actual activation may require platform privileges.", "nagi dns tun on", "", 0, -1},
	{"dns tun on", "dns tun on", "Enable TUN DNS interception", "Configures TUN auto-route, strict-route, and port 53 hijack for IPv4 and IPv6.", "nagi dns tun on", "", 0, 0},
	{"dns tun off", "dns tun off", "Disable TUN DNS interception", "Disables TUN in the selected profile override.", "nagi dns tun off", "", 0, 0},
	{"dns query", "dns query DOMAIN [A|AAAA]", "Query through mihomo DNS", "Queries the running mihomo resolver over the private control socket.", "nagi dns query example.com AAAA", "", 1, 2},
	{"dns flush", "dns flush", "Clear mihomo DNS cache", "Requests mihomo to clear its DNS cache; requires a running control API.", "nagi dns flush", "", 0, 0},
	{"proxy", "proxy <groups|show GROUP|search QUERY|select GROUP NODE|auto GROUP|providers|provider SUBCOMMAND|tailscale SUBCOMMAND|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore>", "Inspect and select proxies", "Adds persistent providers to the selected profile; reads live proxy groups and providers and measures delay.", "nagi proxy groups", "", 0, -1},
	{"proxy groups", "proxy groups", "List proxy groups", "Lists groups in the current profile's order with their live selections.", "nagi proxy groups", "", 0, 0},
	{"proxy show", "proxy show GROUP", "Show a proxy group", "Shows one group and its available nodes.", "nagi proxy show 'Proxy Group'", "Run `nagi proxy groups` to find a group name.", 1, 1},
	{"proxy search", "proxy search QUERY", "Search nodes", "Lists case-insensitive node-name matches by group.", "nagi proxy search Hong", "Run `nagi proxy groups` to inspect all nodes.", 1, 1},
	{"proxy select", "proxy select GROUP NODE", "Select a proxy node", "Selects a node in a group and saves the choice for the selected profile.", "nagi proxy select 'Proxy Group' 'Node A'", "Run `nagi proxy groups` and `nagi proxy show GROUP` to find names.", 2, 2},
	{"proxy auto", "proxy auto GROUP", "Resume automatic selection", "Clears the fixed node of a URLTest or Fallback group and removes its saved choice.", "nagi proxy auto 'Auto Group'", "Run `nagi proxy groups` to find an automatic group.", 1, 1},
	{"proxy tailscale", "proxy tailscale <list|show NAME|targets|add NAME EXIT_NODE KEY_FILE [OPTIONS]|edit NAME OPTIONS|bind NAME node|provider TARGET|unbind node|provider TARGET|remove NAME>", "Manage Tailscale exit-node chaining", "Offline inspection reads the selected effective profile. Mutations validate and save its local override, reloading only when running. Control URL defaults to https://controlplane.tailscale.com; key files are regular, nonempty, and at most 16384 bytes. Tailscale identities are retained after removal.", "nagi proxy tailscale list", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 0, -1},
	{"proxy tailscale list", "proxy tailscale list", "List configured Tailscale nodes", "Reads top-level Tailscale nodes from the selected effective profile without a running engine; reports key presence only.", "nagi proxy tailscale list", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 0, 0},
	{"proxy tailscale show", "proxy tailscale show NAME", "Show a Tailscale node and its bindings", "Reads one top-level Tailscale node and its node/provider bindings while offline. Auth keys and their input file paths are omitted.", "nagi proxy tailscale show \"TS Exit\"", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 1, 1},
	{"proxy tailscale targets", "proxy tailscale targets", "List eligible local binding targets", "Reads eligible top-level proxy nodes and providers from local configuration. Groups, Tailscale nodes, and special outbounds are excluded. A provider binding affects all its nodes.", "nagi proxy tailscale targets", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 0, 0},
	{"proxy tailscale add", "proxy tailscale add NAME EXIT_NODE KEY_FILE [--hostname HOSTNAME] [--control-url URL]", "Add a Tailscale exit-node proxy", "NAME is a new printable proxy name (1..128 bytes; spaces allowed). EXIT_NODE is a Tailscale exit IP, name, or auto selector. KEY_FILE must contain one nonempty printable Auth Key, at most 16384 bytes; symlinks are rejected. --hostname sets the Tailscale device hostname; --control-url selects an HTTPS Headscale service (default https://controlplane.tailscale.com). Saves a full local node snapshot, validates, and reloads only when running. Subscription apply clears this override; saved/reloaded does not establish login.", "nagi proxy tailscale add \"TS Exit\" 100.64.0.10 ./auth-key.txt --hostname nagi-work", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 3, 7},
	{"proxy tailscale edit", "proxy tailscale edit NAME [--exit-node VALUE] [--hostname HOSTNAME] [--control-url URL] [--auth-key-file FILE]", "Edit a Tailscale proxy", "Updates an existing top-level Tailscale node; at least one option is required. Omitted fields, including Auth Key, are retained. --auth-key-file reads a regular nonempty file of at most 16384 bytes. --control-url accepts HTTPS Headscale or an empty value for official Tailscale. Saves and validates the full node override snapshot; reloads only when running.", "nagi proxy tailscale edit \"TS Exit\" --exit-node 100.64.0.11", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 3, 9},
	{"proxy tailscale bind", "proxy tailscale bind NAME node|provider TARGET", "Chain a proxy or provider through Tailscale", "Sets a node dialer-proxy or provider override.dialer-proxy to NAME. Provider binding affects ALL nodes, including future refreshes, without changing the download proxy. Existing non-Tailscale front proxies and dependency cycles are refused; Tailscale bindings may be reassigned. Validates and saves locally, reloading only when running.", "nagi proxy tailscale bind \"TS Exit\" provider work-feed", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 3, 3},
	{"proxy tailscale unbind", "proxy tailscale unbind node|provider TARGET", "Clear a Tailscale binding", "Writes an explicit empty front proxy on a node or provider so a source binding cannot reappear. Provider changes affect ALL nodes, including future refreshes. Validates and saves locally, reloading only when running.", "nagi proxy tailscale unbind node \"Node A\"", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 2, 2},
	{"proxy tailscale remove", "proxy tailscale remove NAME", "Remove an unbound Tailscale proxy", "Removes a top-level Tailscale node only after node, provider, and DNS references are removed. Mihomo validation protects rules and groups. Saves the full node snapshot and reloads only when running; persistent identity state is retained.", "nagi proxy tailscale remove \"TS Exit\"", "Run `nagi proxy tailscale list` or `nagi proxy tailscale targets` to discover names.", 1, 1},
	{"proxy providers", "proxy providers", "List proxy providers", "Lists provider names, types, update time, and node names without revealing URLs or credentials.", "nagi proxy providers", "", 0, 0},
	{"proxy provider", "proxy provider <add NAME URL GROUP [SECONDS]|show NAME|update NAME|healthcheck NAME>", "Manage one proxy provider", "Adds provider configuration through the selected profile override; inspections, updates, and healthchecks use mihomo APIs.", "nagi proxy provider show work", "", 0, -1},
	{"proxy provider add", "proxy provider add NAME URL GROUP [SECONDS]", "Add an HTTP proxy provider", "Adds a provider to the selected profile local override and attaches it to GROUP. Creates a select group with DIRECT fallback if absent. NAME uses 1..128 ASCII letters, digits, _ or -; GROUP permits internal spaces. URL must be absolute HTTP(S) without userinfo or fragment. SECONDS defaults to 3600 (1..31536000). mihomo downloads and healthchecks nodes; routing rules remain unchanged. Reloads only when running.", "nagi proxy provider add work https://example.com/nodes.yaml \"My Proxy\"", "Run `nagi proxy groups` to find a running group, or choose a new GROUP name.", 3, 4},
	{"proxy provider show", "proxy provider show NAME", "Show a proxy provider", "Reports redacted provider metadata and node names.", "nagi proxy provider show work", "", 1, 1},
	{"proxy provider update", "proxy provider update NAME", "Update a proxy provider", "Asks mihomo to download the provider again.", "nagi proxy provider update work", "", 1, 1},
	{"proxy provider healthcheck", "proxy provider healthcheck NAME", "Check provider nodes", "Asks mihomo to start its provider healthcheck.", "nagi proxy provider healthcheck work", "", 1, 1},
	{"proxy delay", "proxy delay NODE [URL] [TIMEOUT_MS]", "Measure proxy delay", "Measures an HTTP(S) URL through a proxy node. URL defaults to https://www.gstatic.com/generate_204 and timeout to 5000 ms.", "nagi proxy delay NodeA", "Run `nagi proxy groups` to find a node name.", 1, 3},
	{"rules", "rules <list|providers|custom|conflicts|connection ID|add NAME TYPE PAYLOAD TARGET|remove NAME|enable NAME|disable NAME|import-local NAME BEHAVIOR FILE TARGET|import-remote NAME BEHAVIOR URL TARGET>", "Manage routing rules", "Custom rules are stored outside profiles and apply before profile rules.", "nagi rules list", "", 0, -1},
	{"rules list", "rules list", "List active rules", "Shows mihomo's ordered active rules, including disabled state and hit counts.", "nagi rules list", "", 0, 0},
	{"rules providers", "rules providers", "List active rule providers", "Shows mihomo rule providers from the running engine.", "nagi rules providers", "", 0, 0},
	{"rules custom", "rules custom", "List custom rules", "Shows persistent user-owned rules and imported sets.", "nagi rules custom", "", 0, 0},
	{"rules conflicts", "rules conflicts", "Report rule order and conflicts", "Shows effective rule order, duplicate matchers, and rules shadowed by MATCH.", "nagi rules conflicts", "", 0, 0},
	{"rules connection", "rules connection ID", "Show a connection's matched rule", "Reads the rule and payload recorded by mihomo for an active connection.", "nagi rules connection CONNECTION_ID", "Run `nagi connections list` to find an ID.", 1, 1},
	{"rules add", "rules add NAME TYPE PAYLOAD TARGET", "Add a custom rule", "Supports DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD, DOMAIN-REGEX, IP-CIDR, IP-CIDR6, GEOIP, GEOSITE, PROCESS-NAME, and MATCH (use - for its payload). TARGET is DIRECT, REJECT, or a group name.", "nagi rules add work DOMAIN-SUFFIX example.com DIRECT", "", 4, 4},
	{"rules remove", "rules remove NAME", "Remove a custom rule", "Removes a named custom rule or imported set.", "nagi rules remove work", "Run `nagi rules custom` to find names.", 1, 1},
	{"rules enable", "rules enable NAME", "Enable a custom rule", "Enables a named custom rule or imported set.", "nagi rules enable work", "Run `nagi rules custom` to find names.", 1, 1},
	{"rules disable", "rules disable NAME", "Disable a custom rule", "Disables a named custom rule or imported set.", "nagi rules disable work", "Run `nagi rules custom` to find names.", 1, 1},
	{"rules import-local", "rules import-local NAME BEHAVIOR FILE TARGET", "Import a local rule set", "Imports YAML payload/list (or domain/ipcidr text) as a persistent inline provider. BEHAVIOR is classical, domain, or ipcidr.", "nagi rules import-local ads domain ./ads.yaml REJECT", "", 4, 4},
	{"rules import-remote", "rules import-remote NAME BEHAVIOR URL TARGET", "Import a remote rule set", "Downloads up to 8 MiB over HTTP(S), stores a snapshot as a persistent inline provider, and applies it before profile rules.", "nagi rules import-remote ads domain https://example.com/ads.yaml REJECT", "", 4, 4},
	{"proxy delays", "proxy delays GROUP [URL] [TIMEOUT_MS]", "Measure group node delays", "Measures all nodes in a group and sorts successful results by ascending latency. Uses the same URL and timeout defaults as proxy delay.", "nagi proxy delays 'Proxy Group'", "Run `nagi proxy groups` to find a group name.", 1, 3},
	{"proxy restore", "proxy restore", "Restore saved proxy choices", "Restores available saved choices for the selected profile and reports missing groups or nodes.", "nagi proxy restore", "Run `nagi proxy groups` to see live selections.", 0, 0},
	{"connections", "connections <list|show ID|close ID|close-all>", "Inspect connections", "Reads or closes active connections from a running engine.", "nagi connections list", "", 0, -1},
	{"connections list", "connections list", "List active connections", "Shows a snapshot of active connections.", "nagi connections list", "", 0, 0},
	{"connections show", "connections show ID", "Inspect one connection", "Shows one active connection, metadata, counters, and proxy chain.", "nagi connections show 42", "Run `nagi connections list` to find IDs.", 1, 1},
	{"connections close", "connections close ID", "Close a connection", "Closes one connection by its mihomo ID.", "nagi connections close 42", "Run `nagi connections list` to find IDs.", 1, 1},
	{"connections close-all", "connections close-all", "Close all connections", "Closes all active connections.", "nagi connections close-all", "", 0, 0},
	{"system-proxy", "system-proxy <status|enable|disable>", "Manage OS proxy settings", "Uses macOS networksetup. Linux system proxy integration is unsupported; configure applications to use a mihomo listener. Saved macOS settings are restored on stop or engine exit.", "nagi system-proxy status", "", 0, -1},
	{"system-proxy status", "system-proxy status", "Show OS proxy settings", "Reports the active OS proxy settings.", "nagi system-proxy status", "", 0, 0},
	{"system-proxy enable", "system-proxy enable", "Enable OS proxy", "Points OS HTTP and HTTPS proxy settings at the running local mihomo port and saves previous settings for restore.", "nagi system-proxy enable", "", 0, 0},
	{"system-proxy disable", "system-proxy disable", "Restore OS proxy settings", "Restores settings saved by system-proxy enable.", "nagi system-proxy disable", "", 0, 0},
	{"tun", "tun <status|enable|disable>", "Manage TUN mode", "Reads or changes runtime mihomo TUN mode; requires OS TUN permissions.", "nagi tun status", "", 0, -1},
	{"tun status", "tun status", "Show TUN mode", "Reports runtime TUN configuration and checks a named OS adapter when available; an up adapter does not prove route capture.", "nagi tun status", "", 0, 0},
	{"tun enable", "tun enable", "Enable TUN mode", "Enables runtime TUN with auto-route and interface detection; macOS takes over system DNS after the adapter is up and restores it when TUN or mihomo stops. Administrator DNS access is required.", "nagi tun enable", "", 0, 0},
	{"tun disable", "tun disable", "Disable TUN mode", "Disables runtime TUN and restores Nagi-managed macOS system DNS; profile reload may re-enable it.", "nagi tun disable", "", 0, 0},
	{"ports", "ports <status>", "Show listener ports", "Reports HTTP, HTTPS, SOCKS, and mixed listener ports from the running engine.", "nagi ports status", "", 0, -1},
	{"ports status", "ports status", "Show listener ports", "HTTP and HTTPS share mihomo's HTTP proxy port; mixed accepts both HTTP and SOCKS.", "nagi ports status", "", 0, 0},
	{"lan", "lan <status|enable [ADDRESS]|disable>", "Manage LAN access", "Changes runtime allow-lan and bind address. Enabling defaults to all interfaces; supply an explicit local IP to restrict listening.", "nagi lan enable 192.168.1.10", "", 0, -1},
	{"lan status", "lan status", "Show LAN access", "Reports allow-lan and bind address.", "nagi lan status", "", 0, 0},
	{"lan enable", "lan enable [ADDRESS]", "Enable LAN access", "Binds to all interfaces by default; ADDRESS must be a local IPv4 or IPv6 address.", "nagi lan enable 192.168.1.10", "", 0, 1},
	{"lan disable", "lan disable", "Disable LAN access", "Restricts proxy listeners to loopback.", "nagi lan disable", "", 0, 0},
	{"mode", "mode [rule|global|direct|save MODE|saved]", "Read or set runtime mode", "Reads or changes temporary runtime mode, or explicitly persists a mode in the selected profile.", "nagi mode save global", "", 0, 2},
	{"service", "service <install|uninstall>", "Manage the user service", "Installs or removes the per-user launchd Agent or systemd service.", "nagi service install", "", 0, -1},
	{"recover", "recover", "Recover stale runtime state", "Removes a stale PID/socket marker after an unexpected mihomo exit; it never touches a live process.", "nagi recover", "", 0, 0},
	{"startup", "startup <status|enable|disable|check>", "Manage startup and recovery", "Inspect or control login autostart and recheck mihomo state after network or sleep/wake changes.", "nagi startup status", "", 0, -1},
	{"startup status", "startup status", "Show startup status", "Reports service installation, enablement, activity, and engine state.", "nagi startup status", "", 0, 0},
	{"startup enable", "startup enable", "Enable login autostart", "Enables the existing per-user launchd/systemd service; install it first with `nagi service install`.", "nagi startup enable", "", 0, 0},
	{"startup disable", "startup disable", "Disable login autostart", "Disables the per-user service without deleting its definition.", "nagi startup disable", "", 0, 0},
	{"startup check", "startup check", "Recheck runtime state", "Rechecks stale runtime markers, the control socket, and managed OS proxy settings. It only reasserts settings matching the saved baseline and never starts mihomo.", "nagi startup check", "", 0, 0},
	{"service install", "service install", "Install the user service", "Installs a per-user launchd Agent or systemd service.", "nagi service install", "", 0, 0},
	{"service uninstall", "service uninstall", "Remove the user service", "Removes the per-user launchd Agent or systemd service.", "nagi service uninstall", "", 0, 0},
	{"service status", "service status", "Show service status", "Reports definition, enabled, and active states.", "nagi service status", "", 0, 0},
	{"privileged-helper", "privileged-helper <status|install|uninstall|enable|disable>", "Manage privileged TUN helper", "Installs a system helper with administrator privileges and selects whether Nagi uses it for mihomo lifecycle and control.", "nagi privileged-helper status", "", 0, -1},
	{"privileged-helper status", "privileged-helper status", "Show privileged helper state", "Reports system helper installation and the selected engine backend.", "nagi privileged-helper status", "", 0, 0},
	{"privileged-helper install", "privileged-helper install", "Install privileged helper", "Requires sudo and a root-owned installed Nagi executable at /usr/local/bin/nagi.", "sudo /usr/local/bin/nagi privileged-helper install", "", 0, 0},
	{"privileged-helper uninstall", "privileged-helper uninstall", "Remove privileged helper", "Requires sudo; stop mihomo and disable the privileged backend first.", "sudo /usr/local/bin/nagi privileged-helper uninstall", "", 0, 0},
	{"privileged-helper enable", "privileged-helper enable", "Select privileged backend", "Requires an active helper and a stopped user mihomo process; then use nagi start.", "nagi privileged-helper enable", "", 0, 0},
	{"privileged-helper disable", "privileged-helper disable", "Select user backend", "Requires the privileged mihomo process to be stopped; then use nagi start.", "nagi privileged-helper disable", "", 0, 0},
	{"version", "version", "Show version information", "Shows Nagi and mihomo versions, pinned commit, OS, and architecture.", "nagi --json version", "", 0, 0},
	{"completion", "completion <bash|zsh|fish|candidates RESOURCE>", "Generate shell completion", "Prints a shell script or local/live completion candidates.", "nagi completion bash", "", 0, -1},
	{"completion bash", "completion bash", "Generate Bash completion", "Prints a Bash completion script.", "nagi completion bash", "", 0, 0},
	{"completion zsh", "completion zsh", "Generate Zsh completion", "Prints a Zsh completion script.", "nagi completion zsh", "", 0, 0},
	{"completion fish", "completion fish", "Generate Fish completion", "Prints a Fish completion script.", "nagi completion fish", "", 0, 0},
	{"completion candidates", "completion candidates profile|subscription|profile-groups NAME|groups|nodes [GROUP]|providers|tailscale-nodes|tailscale-targets [node|provider]", "List completion candidates", "Lists local names, eligible proxy groups in a named profile, or live proxy and provider names; URLs are excluded.", "nagi completion candidates profile-groups work", "", 1, 2},
}

func findSpec(path string) *commandSpec {
	for i := range commandSpecs {
		if commandSpecs[i].path == path {
			return &commandSpecs[i]
		}
	}
	return nil
}

func topHelp() string {
	var b strings.Builder
	b.WriteString("Nagi manages a local mihomo process.\n\nUsage: nagi [--json] <command> [arguments]\n       nagi help [command [subcommand]]\n\nCommands:\n")
	for _, spec := range commandSpecs {
		if strings.Contains(spec.path, " ") {
			continue
		}
		fmt.Fprintf(&b, "  %-22s %s\n", spec.syntax, spec.summary)
	}
	b.WriteString("\nRun `nagi help COMMAND` or `nagi COMMAND --help` for details.\n")
	return b.String()
}

func specHelp(spec *commandSpec) string {
	return fmt.Sprintf("Usage: nagi %s\n\n%s\n\nExample: %s\n", spec.syntax, spec.detail, spec.example)
}

func requestedHelp(args []string) (string, bool, error) {
	if len(args) == 0 {
		return topHelp(), true, nil
	}
	help := args[0] == "help"
	path := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			help = true
			continue
		}
		path = append(path, arg)
	}
	if !help {
		return "", false, nil
	}
	if len(path) > 0 && path[0] == "help" {
		path = path[1:]
	}
	if len(path) == 0 {
		return topHelp(), true, nil
	}
	if spec := findSpec(strings.Join(path, " ")); spec != nil {
		return specHelp(spec), true, nil
	}
	return "", true, fmt.Errorf("unknown help topic or extra arguments\nUsage: nagi help [command [subcommand]]\nRun `nagi help` to list commands")
}

func validateInvocation(args []string) error {
	root := findSpec(args[0])
	if root == nil {
		return fmt.Errorf("unknown command\nUsage: nagi [--json] <command> [arguments]\nRun `nagi help` to list commands")
	}
	spec := root
	consumed := 1
	if root.maxArgs == -1 {
		if len(args) == 1 {
			return syntaxError("missing subcommand", root)
		}
		spec = findSpec(args[0] + " " + args[1])
		if spec == nil {
			return syntaxError("unknown subcommand", root)
		}
		consumed = 2
	}
	if spec.maxArgs == -1 && consumed == 2 {
		if len(args) == 2 {
			return syntaxError("missing subcommand", spec)
		}
		parent := spec
		spec = findSpec(args[0] + " " + args[1] + " " + args[2])
		if spec == nil {
			return syntaxError("unknown subcommand", parent)
		}
		consumed = 3
	}
	n := len(args) - consumed
	if n < spec.minArgs {
		return syntaxError("missing argument", spec)
	}
	if n > spec.maxArgs {
		return syntaxError("too many arguments", spec)
	}
	if spec.path == "uninstall" && n == 1 && args[consumed] != "--purge-data" {
		return syntaxError("expected --purge-data", spec)
	}
	if spec.path == "profile override apply-default" && n != 1 && (n != 3 || args[consumed+1] != "--dns-proxy" || strings.TrimSpace(args[consumed+2]) == "") {
		return syntaxError("expected --dns-proxy GROUP", spec)
	}
	if n == 1 && (spec.path == "update" || spec.path == "app install" || spec.path == "app update") {
		value := args[consumed]
		if !(spec.path == "update" && (value == "--terminal" || value == "--app")) && !installation.ValidTag(value) {
			return syntaxError("TAG must be vX.Y.Z (an optional release suffix is allowed)", spec)
		}
	}
	if strings.HasPrefix(spec.path, "proxy tailscale ") {
		if _, err := tailscaleArguments(args); err != nil {
			return syntaxError(err.Error(), spec)
		}
	}

	if spec.path == "proxy provider add" {
		interval := profile.DefaultProviderInterval
		if n == 4 {
			var err error
			interval, err = strconv.Atoi(args[6])
			if err != nil {
				return syntaxError("SECONDS must be an integer from 1 to 31536000", spec)
			}
		}
		if err := profile.ValidateProviderInput(args[3], args[4], args[5], interval); err != nil {
			return syntaxError(err.Error(), spec)
		}
	}
	if spec.path == "subscription schedule" {
		if args[3] != "off" {
			seconds, err := strconv.ParseInt(args[3], 10, 64)
			if err != nil || seconds < 1 || seconds > 31536000 {
				return syntaxError("SECONDS must be 1..31536000 or off", spec)
			}
		}
	}
	if spec.path == "completion candidates" {
		if args[2] != "profile" && args[2] != "subscription" && args[2] != "profile-groups" && args[2] != "groups" && args[2] != "nodes" && args[2] != "providers" && args[2] != "tailscale-nodes" && args[2] != "tailscale-targets" {
			return syntaxError("RESOURCE must be profile, subscription, profile-groups, groups, nodes, providers, tailscale-nodes, or tailscale-targets", spec)
		}
		if n == 2 && args[2] != "nodes" && args[2] != "profile-groups" && args[2] != "tailscale-targets" {
			return syntaxError("second argument is only valid for nodes, profile-groups, or tailscale-targets", spec)
		}
		if n == 2 && args[2] == "tailscale-targets" && args[3] != "node" && args[3] != "provider" {
			return syntaxError("KIND must be node or provider", spec)
		}

		if n == 1 && args[2] == "profile-groups" {
			return syntaxError("profile-groups requires NAME", spec)
		}
	}
	if spec.path == "logs" && n == 1 {
		if args[1] == "follow" {
			return nil
		}
		count, err := strconv.Atoi(args[1])
		if err != nil || count < 1 || count > 10000 {
			return syntaxError("lines must be an integer from 1 to 10000", spec)
		}
	}
	if spec.path == "proxy delay" || spec.path == "proxy delays" {
		if strings.TrimSpace(args[2]) == "" {
			return syntaxError("proxy node or group required", spec)
		}
		target := "https://www.gstatic.com/generate_204"
		if n >= 2 {
			target = args[3]
		}
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return syntaxError("URL must be an absolute HTTP(S) URL without userinfo", spec)
		}
		if n == 3 {
			timeout, err := strconv.Atoi(args[4])
			if err != nil || timeout < 1 || timeout > 30000 {
				return syntaxError("timeout must be an integer from 1 to 30000 milliseconds", spec)
			}
		}
	}
	if spec.path == "rules add" {
		if strings.TrimSpace(args[2]) == "" || strings.TrimSpace(args[3]) == "" || strings.TrimSpace(args[5]) == "" {
			return syntaxError("NAME, TYPE, and TARGET are required", spec)
		}
	}
	if spec.path == "rules import-remote" {
		u, e := url.Parse(args[4])
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return syntaxError("URL must be absolute HTTP(S) without userinfo", spec)
		}
	}
	if spec.path == "rules import-local" || spec.path == "rules import-remote" {
		if args[3] != "classical" && args[3] != "domain" && args[3] != "ipcidr" {
			return syntaxError("BEHAVIOR must be classical, domain, or ipcidr", spec)
		}
	}
	if spec.path == "lan enable" && n == 1 && net.ParseIP(args[2]) == nil {
		return syntaxError("ADDRESS must be an IPv4 or IPv6 address", spec)
	}
	if spec.path == "diagnostics" && n == 2 {
		if args[1] != "export" || !filepath.IsAbs(args[2]) {
			return syntaxError("export requires an absolute output path", spec)
		}
	}
	if spec.path == "mode" && n == 1 {
		if args[1] != "rule" && args[1] != "global" && args[1] != "direct" && args[1] != "saved" {
			return syntaxError("mode must be rule, global, or direct", spec)
		}
	}
	if spec.path == "mode" && n == 2 && (args[1] != "save" || args[2] != "rule" && args[2] != "global" && args[2] != "direct") {
		return syntaxError("mode save requires rule, global, or direct", spec)
	}
	if spec.path == "connections close" && strings.TrimSpace(args[2]) == "" {
		return syntaxError("connection ID required", spec)
	}
	return nil
}

func syntaxError(problem string, spec *commandSpec) error {
	message := fmt.Sprintf("%s\nUsage: nagi %s", problem, spec.syntax)
	if spec.hint != "" {
		message += "\n" + spec.hint
	}
	message += "\nRun `nagi help " + spec.path + "` for details"
	return fmt.Errorf("%s", message)
}
