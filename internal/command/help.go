package command

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/installation"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
)

type commandSpec struct {
	path, syntax, summary, detail, example, hint string
	minArgs, maxArgs                             int
}

// Keep command syntax and help in one place. Group entries have maxArgs -1;
// variadic leaf commands have maxArgs -2.
var commandSpecs = []commandSpec{
	{"app", "app <status|install [TAG]|update [TAG]|uninstall|update-error>", "Manage the macOS menu bar App", "Requires a release-managed CLI at /usr/local/bin/nagi. App install and uninstall change only the App; app update updates the whole installation.", "nagi app install", "", 0, -1},
	{"app update-error", "app update-error", "Read and clear the last background update failure", "Returns a redacted error string, or an empty error when no failure is pending. Does not require an installation record or running engine.", "nagi --json app update-error", "", 0, 0},
	{"app status", "app status", "Show managed App installation", "Reads the local installation record; does not contact GitHub or require mihomo.", "nagi app status", "", 0, 0},
	{"app install", "app install [TAG]", "Install the macOS menu bar App", "Downloads and verifies the release matching the current CLI version by default. TAG selects another release. Installs only the App; leaves binaries, profiles, engine, and login service unchanged. Administrator authorization may be required.", "nagi app install", "", 0, 1},
	{"app update", "app update [TAG]", "Update Nagi including the installed App", "Alias of update for an installation that includes the App. Downloads the latest release unless TAG is supplied. Quit the App first.", "nagi app update", "", 0, 1},
	{"app uninstall", "app uninstall", "Remove only the menu bar App", "Quit the App and disable its login item first. Preserves the CLI, mihomo, profiles, and engine service. Administrator authorization may be required.", "nagi app uninstall", "", 0, 0},
	{"update", "update [TAG|--terminal|--app]", "Update the installed Nagi release", "Verifies the release before stopping Nagi. Updates CLI, mihomo, and an already installed App, refreshes the default profile, and restores login service preferences. Restarts a previously running engine. TAG defaults to latest. On macOS, --terminal opens Terminal; --app downloads, verifies, and authorizes the update before scheduling installation after the menu App quits. Administrator authorization may be required.", "nagi update", "", 0, 1},
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
	{"profile use", "profile use NAME", "Select a profile", "Validates and activates an existing profile with its own custom rules; reloads a running engine.", "nagi profile use work", "Run `nagi profile list` to find a profile name.", 1, 1},
	{"profile import", "profile import NAME FILE", "Import a profile", "Validates and saves a local YAML file, up to 8 MiB. Use profile use to select it.", "nagi profile import work ./work.yaml", "Run `nagi profile list` to see existing names.", 2, 2},
	{"profile export", "profile export NAME FILE", "Export a profile", "Writes the source YAML to a new file with private permissions; refuses to overwrite an existing file.", "nagi profile export work ./work.yaml", "", 2, 2},
	{"profile backup", "profile backup NAME", "Back up a profile", "Copies the source YAML to profiles/NAME.yaml.bak.", "nagi profile backup work", "", 1, 1},
	{"profile restore", "profile restore NAME", "Restore a profile backup", "Validates and writes profiles/NAME.yaml.bak; reloads if selected and running.", "nagi profile restore work", "", 1, 1},
	{"profile diff", "profile diff NAME [OTHER|backup]", "Compare profile versions", "Shows changed lines against another profile or the latest backup (default).", "nagi profile diff work backup", "", 1, 2},
	{"profile override", "profile override <set NAME FILE|show NAME|clear NAME|apply-default NAME [--dns-proxy GROUP]|default-status NAME|restore-default NAME>", "Manage local overrides", "Stores a YAML mapping separately and applies its keys over the source profile.", "nagi profile override set work ./local.yaml", "", 0, -1},
	{"profile override set", "profile override set NAME FILE", "Save local override", "Validates and saves a YAML mapping, up to 8 MiB; reloads the selected running profile.", "nagi profile override set work ./local.yaml", "", 2, 2},
	{"profile override show", "profile override show NAME", "Show local override", "Prints user-owned override YAML.", "nagi profile override show work", "", 1, 1},
	{"profile override clear", "profile override clear NAME", "Clear local override", "Removes the user-owned override and reloads the selected running profile.", "nagi profile override clear work", "", 1, 1},
	{"profile override apply-default", "profile override apply-default NAME [--dns-proxy GROUP]", "Apply default runtime settings", "Copies safe local runtime settings from profiles/default.yaml onto NAME; subscription nodes, groups, rules, and Nagi-managed fields remain unchanged; an enabled TUN stays enabled. On first use, select a target profile proxy group for default DNS; later uses reuse the saved group unless changed explicitly.", "nagi profile override apply-default work --dns-proxy Proxy", "The default profile and named target group must exist.", 1, 3},
	{"profile override default-status", "profile override default-status NAME", "Show default runtime settings state", "Reports whether the saved default overlay still matches the profile override.", "nagi profile override default-status work", "", 1, 1},
	{"profile override restore-default", "profile override restore-default NAME", "Restore previous local override", "Restores the local override saved by apply-default; refuses if the override changed since applying.", "nagi profile override restore-default work", "", 1, 1},
	{"profile remove", "profile remove NAME", "Remove a profile", "Removes an unselected local profile, its backup, local overrides, custom rules, and saved proxy choices; keeps subscriptions and their caches.", "nagi profile remove old", "Run `nagi profile list` to see profile names.", 1, 1},
	{"subscription", "subscription <list|add NAME URL [--schedule SECONDS|off] [--test-url URL]|key NAME FILE|clear|schedule NAME SECONDS|off|update NAME|preview NAME|apply NAME|remove NAME|watch>", "Manage subscriptions", "Stores URLs and private age identities separately from cached downloads and active profiles.", "nagi subscription list", "", 0, -1},
	{"subscription list", "subscription list", "List subscriptions", "Lists names, refresh/schedule state, cache and applied versions, and profile association without URLs. scheduler_running reports a live scheduler host.", "nagi subscription list", "", 0, 0},
	{"subscription add", "subscription add NAME URL [--schedule SECONDS|off] [--test-url URL]", "Save a subscription URL", "Saves an HTTP or HTTPS URL and optional settings before downloading or activating. Schedule defaults to 3600 seconds; accepts 1..31536000 or off. Test URL must be HTTP(S) without user information or fragment. Names use letters, digits, _ or -, up to 128 characters.", "nagi subscription add work https://example.com/subscription --schedule 3600 --test-url https://example.com/check", "", 2, 6},
	{"subscription key", "subscription key NAME FILE|clear", "Set or clear an age identity", "Copies a bounded regular identity file into private storage, or clears it. The next encrypted update uses mihomo to decrypt; plaintext feeds are unaffected.", "nagi subscription key work ./identity.txt", "Run `nagi subscription list` to find a subscription name.", 2, 2},
	{"subscription schedule", "subscription schedule NAME SECONDS|off", "Set a subscription interval", "Saves 1..31536000 seconds, or off. New subscriptions default to 3600 seconds; legacy entries default to off. A running scheduler downloads when due and may apply the current associated profile.", "nagi subscription schedule work 3600", "Run `nagi subscription list` to find a subscription name.", 2, 2},
	{"subscription watch", "subscription watch", "Run subscription scheduler", "Runs foreground updates until interrupted. A second scheduler for this user exits with subscription_error; the login service also hosts this task.", "nagi subscription watch", "Run `nagi subscription list` to inspect due times and failures.", 0, 0},
	{"subscription update", "subscription update NAME", "Download a subscription", "Validates and caches up to 8 MiB, then reports node changes; does not activate it.", "nagi subscription update work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription preview", "subscription preview NAME", "Preview cached node changes", "Compares cached nodes with the existing profile without changing it.", "nagi subscription preview work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription apply", "subscription apply NAME", "Apply a cached subscription", "Replaces the named profile with cached configuration, retains local settings and node/group changes, clears saved group choices, validates, and selects it. Nagi owns local listeners and controller access.", "nagi subscription apply work", "Run `nagi subscription preview NAME` to inspect node changes.", 1, 1},
	{"subscription remove", "subscription remove NAME", "Remove a subscription", "Removes the saved subscription and its cache.", "nagi subscription remove work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"dns", "dns <status|set|exception|tun|query|flush|check>", "Manage DNS protection", "Configures selected-profile DNS in a separate local override; inspects mihomo DNS through its private control API.", "nagi dns status", "", 0, -1},
	{"dns status", "dns status", "Show selected DNS policy", "Reports configured upstreams, IPv6, TUN, and configuration risks.", "nagi dns status", "", 0, 0},
	{"dns check", "dns check", "Check DNS configuration and mihomo API", "Inspects the selected effective DNS configuration and queries the running mihomo resolver.", "nagi dns check", "", 0, 0},
	{"dns set", "dns set direct URL [URL...] [--bootstrap IP_DOH_URL ...]|proxy NAME URL [URL...] [--bootstrap IP_DOH_URL ...]", "Set encrypted DNS upstreams", "Saves HTTPS DoH URLs. Proxy binds primary DoH to a named non-direct node or group; bootstrap and proxy-node resolution use direct IP-address DoH. --bootstrap overrides the DNSPod DoH defaults. No plaintext fallback is configured.", "nagi dns set proxy NodeA https://1.1.1.1/dns-query --bootstrap https://9.9.9.9/dns-query", "", 2, 32},
	{"dns exception", "dns exception <add DOMAIN SERVER|remove DOMAIN>", "Manage DNS exceptions", "Exceptions route a domain suffix to an explicit server; plaintext IP servers are allowed only here.", "nagi dns exception add corp.example udp://10.0.0.53:53", "", 0, -1},
	{"dns exception add", "dns exception add DOMAIN SERVER", "Add DNS exception", "Maps a domain suffix to a DoH URL or an explicitly requested plaintext IP server.", "nagi dns exception add corp.example udp://10.0.0.53:53", "", 2, 2},
	{"dns exception remove", "dns exception remove DOMAIN", "Remove DNS exception", "Removes the selected domain suffix from local DNS policy.", "nagi dns exception remove corp.example", "", 1, 1},
	{"dns tun", "dns tun <on|off>", "Configure TUN interception", "Stores TUN auto-route, strict-route, and port 53 hijack settings; actual activation may require platform privileges.", "nagi dns tun on", "", 0, -1},
	{"dns tun on", "dns tun on", "Enable TUN DNS interception", "Configures TUN auto-route, strict-route, and port 53 hijack for IPv4 and IPv6.", "nagi dns tun on", "", 0, 0},
	{"dns tun off", "dns tun off", "Disable TUN DNS interception", "Disables TUN in the selected profile override.", "nagi dns tun off", "", 0, 0},
	{"dns query", "dns query DOMAIN [A|AAAA]", "Query through mihomo DNS", "Queries the running mihomo resolver over the private control socket.", "nagi dns query example.com AAAA", "", 1, 2},
	{"dns flush", "dns flush", "Clear mihomo DNS cache", "Requests mihomo to clear its DNS cache; requires a running control API.", "nagi dns flush", "", 0, 0},
	{"proxy", "proxy <groups|group SUBCOMMAND|show GROUP|search QUERY|select GROUP NODE|auto GROUP|providers|provider SUBCOMMAND|healthcheck SUBCOMMAND|node SUBCOMMAND|tailscale SUBCOMMAND|chain SUBCOMMAND|test SUBCOMMAND|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore>", "Inspect and select proxies", "Adds persistent providers to the selected profile; reads live proxy groups and providers and measures delay.", "nagi proxy groups", "", 0, -1},
	{"proxy healthcheck", "proxy healthcheck <show|set|reset|url> group|provider|subscription NAME [SECONDS]", "Manage configured healthcheck intervals and test URLs", "Reads or changes local configuration while offline. Subscription settings can be saved before first apply; associated profiles validate and reload only when selected and running. Provider and group schedulers may differ.", "nagi proxy healthcheck show group Auto", "", 0, -1},
	{"proxy healthcheck show", "proxy healthcheck show group|provider|subscription NAME [--full-url]", "Show automatic-check settings", "Reports interval, test URL origin, sources, lazy setting, and scheduling caveats without probing nodes. --full-url includes the complete saved test URL and read-only inherited-value previews for editing.", "nagi proxy healthcheck show provider work", "", 2, 3},
	{"proxy healthcheck set", "proxy healthcheck set group|provider|subscription NAME SECONDS", "Save a healthcheck interval", "SECONDS must be 1..31536000. Changes interval only; provider enable and URL remain unchanged.", "nagi proxy healthcheck set group Auto 600", "", 3, 3},
	{"proxy healthcheck reset", "proxy healthcheck reset group|provider|subscription NAME", "Restore the source healthcheck interval", "Removes only the local interval setting while retaining unrelated overrides.", "nagi proxy healthcheck reset group Auto", "", 2, 2},
	{"proxy group", "proxy group <list|show GROUP|add GROUP [OPTIONS]|edit GROUP OPTIONS|remove GROUP|order GROUP [GROUP...]>", "Manage proxy groups", "Reads local groups while stopped. Definition changes validate with mihomo and reload only when running; display ordering saves locally without reloading. Routing rules remain unchanged.", "nagi proxy group list", "", 0, -1},
	{"proxy group list", "proxy group list", "List configured proxy groups", "Reports local groups in profile order without requiring the engine.", "nagi proxy group list", "", 0, 0},
	{"proxy group show", "proxy group show GROUP", "Show configured proxy group", "Reports editable settings without provider credentials.", "nagi proxy group show Choice", "Run nagi proxy group list to discover groups.", 1, 1},
	{"proxy group add", "proxy group add GROUP [--type TYPE] [--node NODE ...|--clear-nodes] [--provider PROVIDER ...|--clear-providers] [--url URL] [--interval SECONDS] [--tolerance MS] [--strategy STRATEGY]", "Create a proxy group", "TYPE: select (default), url-test, fallback, load-balance. Without member options, defaults to DIRECT. Automatic groups default to the Cloudflare generate_204 URL and 600 seconds. Interval: 1..31536000. URL-test tolerance: 0..65535 ms. Load-balance strategy: consistent-hashing, round-robin, sticky-sessions. Repeat --node/--provider in desired order.", "nagi proxy group add Choice --type select --node DIRECT", "", 1, -2},
	{"proxy group edit", "proxy group edit GROUP [--type TYPE] [--node NODE ...|--clear-nodes] [--provider PROVIDER ...|--clear-providers] [--url URL] [--interval SECONDS] [--tolerance MS] [--strategy STRATEGY]", "Edit a proxy group", "Omitted fields remain unchanged. Member options replace the corresponding list; clear removes it. At least one member or automatic inclusion is required. Changing type removes inapplicable tolerance/strategy fields; other native settings remain unchanged.", "nagi proxy group edit Choice --node DIRECT --node 'Node A'", "", 2, -2},
	{"proxy group order", "proxy group order GROUP [GROUP...]", "Reorder proxy group display", "Provide every configured group exactly once in display order. Saves local display metadata without reloading or starting mihomo. Subscription updates retain this order and append new groups. Use proxy group list to discover names; generated GLOBAL is excluded.", "nagi proxy group order 'Work' 'Automatic'", "", 1, -2},

	{"proxy group remove", "proxy group remove GROUP", "Delete a proxy group", "Refuses referenced groups. Remove routing, DNS and other dependencies first. Retains nodes and providers.", "nagi proxy group remove Choice", "", 1, 1},

	{"proxy groups", "proxy groups", "List proxy groups", "Lists groups in the current profile's order with their live selections.", "nagi proxy groups", "", 0, 0},
	{"proxy show", "proxy show GROUP", "Show a proxy group", "Shows one group and its available nodes.", "nagi proxy show 'Proxy Group'", "Run `nagi proxy groups` to find a group name.", 1, 1},
	{"proxy search", "proxy search QUERY", "Search nodes", "Lists case-insensitive node-name matches by group.", "nagi proxy search Hong", "Run `nagi proxy groups` to inspect all nodes.", 1, 1},
	{"proxy select", "proxy select GROUP NODE", "Select a proxy node", "Selects a node in a group and saves the choice for the selected profile.", "nagi proxy select 'Proxy Group' 'Node A'", "Run `nagi proxy groups` and `nagi proxy show GROUP` to find names.", 2, 2},
	{"proxy auto", "proxy auto GROUP", "Resume automatic selection", "Clears the fixed node of a URLTest or Fallback group and removes its saved choice.", "nagi proxy auto 'Auto Group'", "Run `nagi proxy groups` to find an automatic group.", 1, 1},
	{"proxy healthcheck url", "proxy healthcheck url <set|reset> group|provider|subscription NAME [URL]", "Manage persistent test URLs", "Changes only native healthcheck URLs, independently of intervals.", "nagi proxy healthcheck url set provider work https://example.com/check", "", 0, -1},
	{"proxy healthcheck url set", "proxy healthcheck url set group|provider|subscription NAME URL", "Save a test URL", "URL must be absolute HTTP(S), without userinfo or fragment. Inspection shows the origin by default; show --full-url returns the complete test URL.", "nagi proxy healthcheck url set group Auto https://example.com/check", "", 3, 3},
	{"proxy healthcheck url reset", "proxy healthcheck url reset group|provider|subscription NAME", "Restore the inherited test URL", "Removes only the local URL setting.", "nagi proxy healthcheck url reset provider work", "", 2, 2},
	{"proxy provider interval", "proxy provider interval <show|set|reset> NAME [SECONDS|off]", "Manage provider download intervals", "Changes downloads independently of latency checks. Off disables automatic downloads.", "nagi proxy provider interval show work", "", 0, -1},
	{"proxy provider interval show", "proxy provider interval show NAME", "Show provider download interval", "Reads local configuration while stopped.", "nagi proxy provider interval show work", "", 1, 1},
	{"proxy provider interval set", "proxy provider interval set NAME SECONDS|off", "Save provider download interval", "SECONDS must be 1..31536000, or off. Validates and reloads only when running.", "nagi proxy provider interval set work 3600", "", 2, 2},
	{"proxy provider interval reset", "proxy provider interval reset NAME", "Restore source download interval", "Retains the provider URL and healthcheck settings.", "nagi proxy provider interval reset work", "", 1, 1},

	{"proxy node", "proxy node <list|show NAME|add tailscale NAME EXIT_NODE [OPTIONS]|edit NAME OPTIONS|remove NAME|groups SUBCOMMAND>", "Manage top-level proxy nodes", "Reads local effective configuration without a running engine. Mutations validate, save, and reload only when running.", "nagi proxy node list", "Run `nagi proxy node list` to discover node names.", 0, -1},
	{"proxy node list", "proxy node list", "List top-level proxy nodes", "Reports node type, managed state, and group membership without secrets.", "nagi proxy node list", "Run `nagi proxy node list` to discover node names.", 0, 0},
	{"proxy node show", "proxy node show NAME", "Show a proxy node", "Shows redacted metadata and explicit, automatic, and excluded groups.", "nagi proxy node show NAME", "Run `nagi proxy node list` to discover node names.", 1, 1},
	{"proxy node add", "proxy node add tailscale NAME EXIT_NODE [--group GROUP] [--auth-key|--auth-key-file FILE|--auth-key-stdin] [--hostname HOSTNAME] [--control-url URL]", "Add a Tailscale proxy node", "Repeat --group to select explicit groups. Omit key options to paste interactively. Auth keys are never accepted as argument values; stdin and regular key files accept at most 16384 bytes. HTTPS control URL defaults to official Tailscale.", "nagi proxy node add tailscale Tail 100.64.0.10 --group Choice --auth-key-stdin", "Run `nagi proxy node list` to discover node names.", 3, -2},
	{"proxy node edit", "proxy node edit NAME [--exit-node VALUE] [--hostname HOSTNAME] [--control-url URL] [--auth-key|--auth-key-file FILE|--auth-key-stdin]", "Edit a Tailscale proxy node", "At least one option is required; omitted fields remain unchanged. Use groups set or clear to change explicit membership.", "nagi proxy node edit Tail --hostname nagi-work", "Run `nagi proxy node list` to discover node names.", 2, -2},
	{"proxy node remove", "proxy node remove NAME", "Remove a proxy node", "References must be removed before deletion; persistent Tailscale identity is retained.", "nagi proxy node remove NAME", "Run `nagi proxy node list` to discover node names.", 1, 1},
	{"proxy node groups", "proxy node groups <show NAME|set NAME GROUP [GROUP...]|clear NAME>", "Manage explicit group membership", "Automatic membership is determined by mihomo include-all settings and filters.", "nagi proxy node groups show Tail", "Run `nagi proxy node list` to discover node names.", 0, -1},
	{"proxy node groups show", "proxy node groups show NAME", "Show node group membership", "Lists explicit, automatic, excluded, and available groups offline.", "nagi proxy node groups show NAME", "Run `nagi proxy node list` to discover node names.", 1, 1},
	{"proxy node groups set", "proxy node groups set NAME GROUP [GROUP...]", "Set explicit groups", "Replaces explicit membership atomically; automatic membership is unchanged.", "nagi proxy node groups set Tail Choice", "Run `nagi proxy node list` to discover node names.", 2, -2},
	{"proxy node groups clear", "proxy node groups clear NAME", "Clear explicit groups", "Clears explicit membership; automatic membership is unchanged.", "nagi proxy node groups clear NAME", "Run `nagi proxy node list` to discover node names.", 1, 1},
	{"proxy tailscale", "proxy tailscale <status|probe>", "Inspect embedded Tailscale paths", "Status reads are passive; probes are explicit and may start the embedded backend.", "nagi proxy tailscale status Tail", "Run `nagi proxy node list` to discover node names.", 0, -1},
	{"proxy tailscale status", "proxy tailscale status NODE", "Show embedded Tailscale path status", "Reads a passive local snapshot without starting the Tailscale backend or probing peers.", "nagi proxy tailscale status Tail", "Run `nagi proxy node list` to discover node names.", 1, 1},
	{"proxy tailscale probe", "proxy tailscale probe NODE PEER_IP [TIMEOUT_MS]", "Probe an embedded Tailscale peer", "Explicitly probes a literal peer IP; timeout defaults to 5000 ms and must be 1000..15000 ms. This operation may start the embedded backend.", "nagi proxy tailscale probe Tail 100.64.0.2", "Run `nagi proxy tailscale status NODE` to inspect peers.", 2, 3},
	{"proxy chain", "proxy chain <list [KIND]|show KIND TARGET|candidates KIND TARGET...|set KIND TARGET... --via NODE [--via NODE...] [--dry-run]|clear KIND TARGET... [--dry-run]>", "Manage proxy chains", "KIND is node, provider-node, provider, or subscription. provider-node takes PROVIDER NODE. Read local effective configuration without a running engine.", "nagi proxy chain list", "Run `nagi proxy chain list` to discover target names.", 0, -1},
	{"proxy chain list", "proxy chain list [node|provider-node|provider|subscription]", "List chain targets", "Reports targets and paths in traffic order.", "nagi proxy chain list", "Run `nagi proxy chain list` to discover target names.", 0, 1},
	{"proxy chain show", "proxy chain show node|provider-node|provider|subscription TARGET", "Show a target chain", "Reports its ordered path without secrets.", "nagi proxy chain show node Target", "Run `nagi proxy chain list` to discover target names.", 2, 3},
	{"proxy chain candidates", "proxy chain candidates node|provider-node|provider|subscription TARGET [TARGET...]", "List eligible chain nodes", "Lists concrete top-level and locally cached provider nodes eligible for all selected targets.", "nagi proxy chain candidates node Target", "Run `nagi proxy chain list` to discover target names.", 2, -2},
	{"proxy chain set", "proxy chain set node|provider-node|provider|subscription TARGET [TARGET...] --via FIRST [--via SECOND...] [--dry-run]", "Set an ordered proxy chain", "Repeat --via NODE and --via-provider PROVIDER NODE in traffic order, from first hop toward target. Dry-run reports path, replaced edges, shared impact, and conflicts without saving or activating. Mutations validate atomically and reload only when running.", "nagi proxy chain set node Target --via First --via Second --dry-run", "Run `nagi proxy chain list` to discover target names.", 4, -2},
	{"proxy chain clear", "proxy chain clear node|provider-node|provider|subscription TARGET [TARGET...] [--dry-run]", "Clear a proxy chain", "Dry-run previews changes without writing. Subscription policies survive apply and scheduled refresh until cleared.", "nagi proxy chain clear provider Feed --dry-run", "Run `nagi proxy chain list` to discover target names.", 2, -2},
	{"proxy providers", "proxy providers", "List proxy providers", "Lists provider names, types, update time, and node names without revealing URLs or credentials.", "nagi proxy providers", "", 0, 0},
	{"proxy provider", "proxy provider <add NAME URL GROUP [SECONDS|off] [--test-url URL]|interval SUBCOMMAND|remove NAME|show NAME|update NAME|healthcheck NAME>", "Manage one proxy provider", "Adds provider configuration through the selected profile override; inspections, updates, and healthchecks use mihomo APIs.", "nagi proxy provider show work", "", 0, -1},
	{"proxy provider add", "proxy provider add NAME URL GROUP [SECONDS|off] [--test-url URL]", "Add an HTTP proxy provider", "Adds a provider to the selected profile local override and attaches it to GROUP. Creates a select group with DIRECT fallback if absent. NAME uses 1..128 ASCII letters, digits, _ or -; GROUP permits internal spaces. URL must be absolute HTTP(S) without userinfo or fragment. SECONDS defaults to 3600 (1..31536000), or off to disable automatic downloads. Optional --test-url URL sets only the provider healthcheck URL; otherwise uses the target automatic group's healthcheck URL and requests an immediate native healthcheck after a running reload, without separate check output. mihomo downloads and healthchecks nodes; routing rules remain unchanged. Reloads only when running.", "nagi proxy provider add work https://example.com/nodes.yaml \"My Proxy\"", "Run `nagi proxy groups` to find a running group, or choose a new GROUP name.", 3, -2},
	{"proxy provider remove", "proxy provider remove NAME", "Remove a proxy provider", "Removes the provider definition and all group use references from the selected profile and local override. Retains groups and routing rules. Validates and reloads if running; failure restores prior settings. NAME is printable, 1..128 bytes, without surrounding whitespace; COMPATIBLE is reserved.", "nagi proxy provider remove work", "Inspect the selected profile and override for provider names while stopped.", 1, 1},
	{"proxy provider show", "proxy provider show NAME", "Show a proxy provider", "Reports redacted provider metadata and node names.", "nagi proxy provider show work", "", 1, 1},
	{"proxy provider update", "proxy provider update NAME", "Update a proxy provider", "Asks mihomo to download the provider again.", "nagi proxy provider update work", "", 1, 1},
	{"proxy provider healthcheck", "proxy provider healthcheck NAME", "Check provider nodes", "Asks mihomo to start its provider healthcheck.", "nagi proxy provider healthcheck work", "", 1, 1},
	{"proxy test", "proxy test <group|provider> OWNER NODE", "Measure a node in its source context", "Uses the effective group or provider URL, timeout and expected status; requires an unambiguous individual node.", "nagi proxy test group 'Proxy Group' NodeA", "Inspect `nagi proxy groups` or `nagi proxy providers`.", 0, -1},
	{"proxy test group", "proxy test group GROUP NODE", "Measure a group node", "Uses the group's configured URL, timeout and expected status, resolving only its configured node sources.", "nagi proxy test group 'Proxy Group' NodeA", "Run `nagi proxy show GROUP` to inspect nodes.", 2, 2},
	{"proxy test provider", "proxy test provider PROVIDER NODE", "Measure a provider node", "Uses the provider's configured health-check URL, timeout and expected status.", "nagi proxy test provider Remote NodeA", "Run `nagi proxy provider show PROVIDER` to inspect nodes.", 2, 2},
	{"proxy delay", "proxy delay NODE [URL] [TIMEOUT_MS]", "Measure proxy delay", "Measures an HTTP(S) URL through a proxy node. URL defaults to https://cp.cloudflare.com/generate_204 and timeout to 5000 ms.", "nagi proxy delay NodeA", "Run `nagi proxy groups` to find a node name.", 1, 3},
	{"rules", "rules <test TARGET [options]|list|providers|custom|conflicts|connection ID|add NAME TYPE PAYLOAD TARGET|remove NAME|enable NAME|disable NAME|import NAME SOURCE|edit NAME TYPE PAYLOAD TARGET|update NAME [SOURCE]>", "Manage routing rules", "Custom rules belong to the selected profile, are stored separately from its source, and apply before its rules. Switching profiles selects that profile's custom rules.", "nagi rules list", "", 0, -1},
	{"rules test", "rules test TARGET [--ip IP] [--port PORT] [--network tcp|udp] [--process NAME]", "Test a domain or IP against active rules", "Requires a running matching mihomo engine. Tests its active rules and rule providers without connecting to the destination; synthetic matching does not update rule counters. Defaults to TCP port 443. TARGET is an ASCII domain or bare IP; --ip supplies a domain's destination address. Mihomo resolves DNS when a rule requires it. Always tests rules even in global/direct mode. Source, inbound, UID, process path and sniffing are unavailable; use rules connection for actual traffic.", "nagi rules test 154.19.187.117", "Start the matching mihomo engine before testing.", 1, -2},
	{"rules list", "rules list", "List active rules", "Shows mihomo's ordered active rules, including disabled state and hit counts.", "nagi rules list", "", 0, 0},
	{"rules providers", "rules providers", "List active rule providers", "Shows mihomo rule providers from the running engine.", "nagi rules providers", "", 0, 0},
	{"rules custom", "rules custom", "List custom rules", "Shows persistent user-owned rules, imported per-rule lists, and rule sets for the selected profile.", "nagi rules custom", "", 0, 0},
	{"rules conflicts", "rules conflicts", "Report rule order and conflicts", "Shows the selected profile's effective rule order, duplicate matchers, and rules shadowed by MATCH.", "nagi rules conflicts", "", 0, 0},
	{"rules connection", "rules connection ID", "Show a connection's matched rule", "Reads the rule and payload recorded by mihomo for an active connection.", "nagi rules connection CONNECTION_ID", "Run `nagi connections list` to find an ID.", 1, 1},
	{"rules add", "rules add NAME TYPE PAYLOAD TARGET", "Add a custom rule", "Supports DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD, DOMAIN-REGEX, IP-CIDR, IP-CIDR6, GEOIP, GEOSITE, PROCESS-NAME, and MATCH (use - for its payload). TARGET is DIRECT, REJECT, or a group name. Saves to the selected profile.", "nagi rules add work DOMAIN-SUFFIX example.com DIRECT", "", 4, 4},
	{"rules edit", "rules edit NAME TYPE PAYLOAD TARGET", "Edit a custom rule", "Replace a single rule's type, payload and target; use - as MATCH payload. Preserves its name, position and enabled state. Validates and reloads if running; restores saved rules on failure.", "nagi rules edit work DOMAIN-SUFFIX example.com DIRECT", "", 4, 4},
	{"rules update", "rules update NAME [SOURCE]", "Update imported rules or a set", "Refresh from the saved source, or replace it with a local file or absolute HTTP(S) URL without userinfo (maximum 8 MiB). Preserves name, order, kind and enabled state. Existing sets accept YAML payload lists; bundles accept native rules. Validates and reloads if running; restores saved rules on failure. Older imports require SOURCE once.", "nagi rules update work", "", 1, 2},
	{"rules remove", "rules remove NAME", "Remove a custom rule", "Removes a named custom rule, imported per-rule list, or rule set from the selected profile.", "nagi rules remove work", "Run `nagi rules custom` to find names.", 1, 1},
	{"rules enable", "rules enable NAME", "Enable a custom rule", "Enables a named custom rule, imported per-rule list, or rule set in the selected profile.", "nagi rules enable work", "Run `nagi rules custom` to find names.", 1, 1},
	{"rules disable", "rules disable NAME", "Disable a custom rule", "Disables a named custom rule, imported per-rule list, or rule set in the selected profile.", "nagi rules disable work", "Run `nagi rules custom` to find names.", 1, 1},
	{"rules import", "rules import NAME SOURCE", "Import routing rules", "SOURCE is a local file or an absolute HTTP(S) URL without userinfo (maximum 8 MiB). Imports native YAML rules with their own targets into the selected profile. NAME uses 1..128 ASCII letters, digits, _ or -. Imported rules apply before profile rules; validation or reload failure restores saved rules.", "nagi rules import work ./rules.yaml", "", 2, 2},
	{"proxy delays", "proxy delays GROUP [URL] [TIMEOUT_MS]", "Measure group node delays", "Measures all nodes in a group and sorts successful results by ascending latency. Omitted URL and timeout use the effective group settings (fallback: https://www.gstatic.com/generate_204, 5000 ms). Explicit arguments override them; URLTest uses its native configured URL.", "nagi proxy delays 'Proxy Group'", "Run `nagi proxy groups` to find a group name.", 1, 3},
	{"proxy restore", "proxy restore", "Restore saved proxy choices", "Restores available saved choices for the selected profile and reports missing groups or nodes.", "nagi proxy restore", "Run `nagi proxy groups` to see live selections.", 0, 0},
	{"connections", "connections <list|watch|show ID|close ID|close-all>", "Inspect connections", "Reads or closes active connections from a running engine.", "nagi connections list", "", 0, -1},
	{"connections list", "connections list", "List active connections", "Shows a snapshot of active connections.", "nagi connections list", "", 0, 0},
	{"connections watch", "connections watch [--interval MILLISECONDS]", "Watch connection snapshots", "Streams complete snapshots immediately and every 250 ms by default; --interval accepts 250–60000 ms. Shorter connections may be missed. Ctrl-C stops watching.", "nagi --json connections watch --interval 250", "", 0, 2},
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
	{"startup enable", "startup enable", "Enable login autostart", "Installs the per-user launchd/systemd service if needed and enables login startup.", "nagi startup enable", "", 0, 0},
	{"startup disable", "startup disable", "Disable login autostart", "Disables next-login startup while retaining the service definition, current monitor, and running mihomo.", "nagi startup disable", "", 0, 0},
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
	{"completion candidates", "completion candidates profile|subscription|rules|rewrite-scripts [PROFILE]|rewrite-rules [PROFILE]|profile-groups NAME|groups|nodes [GROUP]|provider-nodes PROVIDER|providers|proxy-nodes|node-groups|chain-targets [node|provider-node|provider|subscription] [PROVIDER]|healthcheck-targets group|provider|subscription", "List completion candidates", "Lists local names (rules belong to the selected profile), eligible proxy groups in a named profile, or live proxy and provider names; URLs are excluded.", "nagi completion candidates profile-groups work", "", 1, 3},
}

func findSpec(path string) *commandSpec {
	for i := range commandSpecs {
		if commandSpecs[i].path == path {
			return &commandSpecs[i]
		}
	}
	return nil
}

// Help prose uses the stable command path and field identity as semantic keys.
// Command syntax, examples and command lookup always use their original values.
func helpLocale(locales []i18n.Context) i18n.Context {
	if len(locales) > 0 {
		return locales[0]
	}
	return i18n.New(i18n.English)
}

func helpText(locale i18n.Context, id, fallback string) string {
	return locale.Render(i18n.NewMessage(id, nil, fallback, nil))
}

func (spec *commandSpec) helpText(locale i18n.Context, field string) string {
	var fallback string
	switch field {
	case "summary":
		fallback = spec.summary
	case "detail":
		fallback = spec.detail
	case "hint":
		fallback = spec.hint
	}
	if fallback == "" {
		return ""
	}
	return helpText(locale, "help.command."+strings.ReplaceAll(spec.path, " ", ".")+"."+field, fallback)
}

const globalHelpSyntax = "nagi [--json] [--lang en|zh-Hans] <command> [arguments]"

func topHelp(locales ...i18n.Context) string {
	locale := helpLocale(locales)
	var b strings.Builder
	b.WriteString(helpText(locale, "help.top.intro", "Nagi manages a local mihomo process."))
	fmt.Fprintf(&b, "\n\n%s %s\n       nagi help [command [subcommand]]\n\n%s\n", helpText(locale, "help.heading.usage", "Usage:"), globalHelpSyntax, helpText(locale, "help.heading.commands", "Commands:"))
	for _, spec := range commandSpecs {
		if strings.Contains(spec.path, " ") {
			continue
		}
		fmt.Fprintf(&b, "  %-22s %s\n", spec.syntax, spec.helpText(locale, "summary"))
	}
	fmt.Fprintf(&b, "\n%s\n\n%s\n", helpText(locale, "help.top.next", "Run `nagi help COMMAND` or `nagi COMMAND --help` for details."), helpText(locale, "help.language", ""))
	return b.String()
}

func specHelp(spec *commandSpec, locales ...i18n.Context) string {
	locale := helpLocale(locales)
	return fmt.Sprintf("%s nagi %s\n\n%s\n\n%s %s\n", helpText(locale, "help.heading.usage", "Usage:"), spec.syntax, spec.helpText(locale, "detail"), helpText(locale, "help.heading.example", "Example:"), spec.example)
}

func requestedHelp(args []string, locales ...i18n.Context) (string, bool, error) {
	locale := helpLocale(locales)
	if len(args) == 0 {
		return topHelp(locale), true, nil
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
		return topHelp(locale), true, nil
	}
	if spec := findSpec(strings.Join(path, " ")); spec != nil {
		return specHelp(spec, locale), true, nil
	}
	return "", true, i18n.NewMessage("help.error.topic", nil, "unknown help topic or extra arguments\nUsage: nagi help [command [subcommand]]\nRun `nagi help` to list commands", nil)
}

func validateInvocation(args []string) error {
	if len(args) > 0 && args[0] == "request-rewrite" {
		o, e := parseRewriteArguments(args)
		if e != nil {
			spec := findSpec(o.path)
			if spec == nil {
				spec = findSpec("request-rewrite")
			}
			return syntaxError(e, spec)
		}
		return nil
	}
	root := findSpec(args[0])
	if root == nil {
		return i18n.NewMessage("help.error.command", nil, "unknown command\nUsage: "+globalHelpSyntax+"\nRun `nagi help` to list commands", nil)
	}
	spec := root
	consumed := 1

	for spec.maxArgs == -1 {
		if len(args) == consumed {
			return syntaxError(messageError("cli.error.validation.missing_subcommand", "missing subcommand"), spec)
		}
		parent := spec
		spec = findSpec(strings.Join(args[:consumed+1], " "))
		if spec == nil {
			return syntaxError(messageError("cli.error.validation.unknown_subcommand", "unknown subcommand"), parent)
		}
		consumed++
	}

	n := len(args) - consumed
	if n < spec.minArgs {
		return syntaxError(messageError("cli.error.validation.missing_argument", "missing argument"), spec)
	}
	if spec.maxArgs >= 0 && n > spec.maxArgs {
		return syntaxError(messageError("cli.error.validation.too_many_arguments", "too many arguments"), spec)
	}
	if spec.path == "connections watch" {
		if _, err := connectionsInterval(args); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "uninstall" && n == 1 && args[consumed] != "--purge-data" {
		return syntaxError(messageError("cli.error.validation.expected_purge_data", "expected --purge-data"), spec)
	}
	if spec.path == "profile override apply-default" && n != 1 && (n != 3 || args[consumed+1] != "--dns-proxy" || strings.TrimSpace(args[consumed+2]) == "") {
		return syntaxError(messageError("cli.error.validation.expected_dns_proxy_group", "expected --dns-proxy GROUP"), spec)
	}
	if n == 1 && (spec.path == "update" || spec.path == "app install" || spec.path == "app update") {
		value := args[consumed]
		if !(spec.path == "update" && (value == "--terminal" || value == "--app")) && !installation.ValidTag(value) {
			return syntaxError(messageError("cli.error.validation.tag_must_be_vx_y_z_an_optional_release_suffix_is_allowed", "TAG must be vX.Y.Z (an optional release suffix is allowed)"), spec)
		}
	}
	if strings.HasPrefix(spec.path, "proxy group ") {
		if _, err := groupArguments(args); err != nil {
			return syntaxError(err, spec)
		}
	}
	if strings.HasPrefix(spec.path, "proxy node ") {
		if _, err := nodeArguments(args); err != nil {
			return syntaxError(err, spec)
		}
	}

	if strings.HasPrefix(spec.path, "proxy chain ") {
		if _, err := chainArguments(args); err != nil {
			return syntaxError(err, spec)
		}
	}

	if spec.path == "proxy healthcheck show" && n == 3 && args[5] != "--full-url" {
		return syntaxError(messageError("cli.error.validation.expected_full_url", "expected --full-url"), spec)
	}
	if strings.HasPrefix(spec.path, "proxy healthcheck ") && !strings.HasPrefix(spec.path, "proxy healthcheck url") {
		if args[3] != "group" && args[3] != "provider" && args[3] != "subscription" {
			return syntaxError(messageError("cli.error.validation.kind_must_be_group_provider_or_subscription", "KIND must be group, provider, or subscription"), spec)
		}
		if strings.TrimSpace(args[4]) == "" {
			return syntaxError(messageError("cli.error.validation.name_is_required", "NAME is required"), spec)
		}
		if args[2] == "set" {
			v, e := strconv.Atoi(args[5])
			if e != nil || v < 1 || v > 31536000 {
				return syntaxError(messageError("cli.error.validation.seconds_must_be_an_integer_from_1_to_31536000", "SECONDS must be an integer from 1 to 31536000"), spec)
			}
		}
	}

	if strings.HasPrefix(spec.path, "proxy healthcheck url ") {
		if args[4] != "group" && args[4] != "provider" && args[4] != "subscription" {
			return syntaxError(messageError("cli.error.validation.kind_must_be_group_provider_or_subscription", "KIND must be group, provider, or subscription"), spec)
		}
		if strings.TrimSpace(args[5]) == "" {
			return syntaxError(messageError("cli.error.validation.name_is_required", "NAME is required"), spec)
		}
		if args[3] == "set" {
			if err := profile.ValidateHealthcheckURL(args[6]); err != nil {
				return syntaxError(err, spec)
			}
		}
	}
	if strings.HasPrefix(spec.path, "proxy provider interval ") {
		if err := profile.ValidateProviderRemoval(args[4]); err != nil {
			return syntaxError(err, spec)
		}
		if args[3] == "set" {
			if _, err := providerSeconds(args[5]); err != nil {
				return syntaxError(err, spec)
			}
		}
	}

	if spec.path == "proxy provider remove" {
		if err := profile.ValidateProviderRemoval(args[3]); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "proxy provider add" {
		interval, _, err := providerAddOptions(args)
		if err != nil {
			return syntaxError(err, spec)
		}
		if err := profile.ValidateProviderInput(args[3], args[4], args[5], interval); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "subscription add" {
		if _, err := subscriptionAddOptions(args); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "subscription schedule" {
		if args[3] != "off" {
			seconds, err := strconv.ParseInt(args[3], 10, 64)
			if err != nil || seconds < 1 || seconds > 31536000 {
				return syntaxError(messageError("cli.error.validation.seconds_must_be_1_31536000_or_off", "SECONDS must be 1..31536000 or off"), spec)
			}
		}
	}
	if spec.path == "completion candidates" {
		if n == 2 && (args[2] == "rewrite-scripts" || args[2] == "rewrite-rules") && !rewriteID(args[3]) {
			return syntaxError(messageError("cli.error.validation.invalid_profile", "invalid PROFILE"), spec)
		}
		if args[2] != "rewrite-scripts" && args[2] != "rewrite-rules" && args[2] != "profile" && args[2] != "rules" && args[2] != "subscription" && args[2] != "profile-groups" && args[2] != "groups" && args[2] != "nodes" && args[2] != "providers" && args[2] != "provider-nodes" && args[2] != "node-groups" && args[2] != "proxy-nodes" && args[2] != "chain-targets" && args[2] != "healthcheck-targets" {
			return syntaxError(messageError("cli.error.validation.resource_must_be_profile_subscription_rules_rewrite_scripts_rewrite_rules_p", "RESOURCE must be profile, subscription, rules, rewrite-scripts, rewrite-rules, profile-groups, groups, nodes, providers, proxy-nodes, node-groups, chain-targets, or healthcheck-targets"), spec)
		}
		if n == 2 && args[2] != "rewrite-scripts" && args[2] != "rewrite-rules" && args[2] != "nodes" && args[2] != "provider-nodes" && args[2] != "profile-groups" && args[2] != "chain-targets" && args[2] != "healthcheck-targets" {
			return syntaxError(messageError("cli.error.validation.second_argument_is_only_valid_for_rewrite_scripts_rewrite_rules_nodes_provi", "second argument is only valid for rewrite-scripts, rewrite-rules, nodes, provider-nodes, profile-groups, chain-targets, or healthcheck-targets"), spec)
		}
		if args[2] == "healthcheck-targets" && (n != 2 || args[3] != "group" && args[3] != "provider" && args[3] != "subscription") {
			return syntaxError(messageError("cli.error.validation.healthcheck_targets_requires_group_provider_or_subscription", "healthcheck-targets requires group, provider, or subscription"), spec)
		}

		if n >= 2 && args[2] == "chain-targets" && args[3] != "provider-node" && args[3] != "node" && args[3] != "provider" && args[3] != "subscription" {
			return syntaxError(messageError("cli.error.validation.kind_must_be_node_provider_or_subscription", "KIND must be node, provider-node, provider, or subscription"), spec)
		}

		if n == 3 && (args[2] != "chain-targets" || args[3] != "provider-node") {
			return syntaxError(messageError("cli.error.validation.provider_chain_completion", "PROVIDER is only valid after chain-targets provider-node"), spec)
		}
		if n == 1 && args[2] == "provider-nodes" {
			return syntaxError(messageError("cli.error.validation.provider_nodes_requires_provider", "provider-nodes requires PROVIDER"), spec)
		}
		if n == 1 && args[2] == "profile-groups" {
			return syntaxError(messageError("cli.error.validation.profile_groups_requires_name", "profile-groups requires NAME"), spec)
		}
	}
	if spec.path == "logs" && n == 1 {
		if args[1] == "follow" {
			return nil
		}
		count, err := strconv.Atoi(args[1])
		if err != nil || count < 1 || count > 10000 {
			return syntaxError(messageError("cli.error.validation.lines_must_be_an_integer_from_1_to_10000", "lines must be an integer from 1 to 10000"), spec)
		}
	}
	if spec.path == "proxy test" {
		return syntaxError(messageError("cli.error.validation.choose_group_or_provider", "choose group or provider"), spec)
	}
	if spec.path == "proxy test group" || spec.path == "proxy test provider" {
		if strings.TrimSpace(args[3]) == "" || strings.TrimSpace(args[4]) == "" {
			return syntaxError(messageError("cli.error.validation.owner_and_node_required", "owner and node required"), spec)
		}
	}
	if spec.path == "proxy delay" || spec.path == "proxy delays" {
		if strings.TrimSpace(args[2]) == "" {
			return syntaxError(messageError("cli.error.validation.proxy_node_or_group_required", "proxy node or group required"), spec)
		}
		target := "https://cp.cloudflare.com/generate_204"
		if n >= 2 {
			target = args[3]
		}
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return syntaxError(messageError("cli.error.validation.url_must_be_an_absolute_http_s_url_without_userinfo", "URL must be an absolute HTTP(S) URL without userinfo"), spec)
		}
		if n == 3 {
			timeout, err := strconv.Atoi(args[4])
			if err != nil || timeout < 1 || timeout > 30000 {
				return syntaxError(messageError("cli.error.validation.timeout_must_be_an_integer_from_1_to_30000_milliseconds", "timeout must be an integer from 1 to 30000 milliseconds"), spec)
			}
		}
	}
	if spec.path == "proxy tailscale status" || spec.path == "proxy tailscale probe" {
		if err := profile.ValidateTailscaleName(args[3]); err != nil {
			return syntaxError(messageError("cli.error.validation.invalid_proxy_node_name", "invalid proxy node name"), spec)
		}
		if spec.path == "proxy tailscale probe" {
			addr, err := netip.ParseAddr(args[4])
			if err != nil || addr.Zone() != "" {
				return syntaxError(messageError("cli.error.validation.tailscale_peer_ip_must_be_a_literal_ip_address", "PEER_IP must be a literal IP address"), spec)
			}
			if n == 3 {
				timeout, err := strconv.Atoi(args[5])
				if err != nil || timeout < 1000 || timeout > 15000 {
					return syntaxError(messageError("cli.error.validation.tailscale_probe_timeout_must_be_between_1000_and_15000", "TIMEOUT_MS must be an integer from 1000 to 15000"), spec)
				}
			}
		}
	}
	if spec.path == "rules add" || spec.path == "rules edit" {
		if strings.TrimSpace(args[2]) == "" || strings.TrimSpace(args[3]) == "" || strings.TrimSpace(args[5]) == "" {
			return syntaxError(messageError("cli.error.validation.name_type_and_target_are_required", "NAME, TYPE, and TARGET are required"), spec)
		}
	}
	if spec.path == "rules edit" {
		payload := args[4]
		typ := strings.ToUpper(args[3])
		if typ == "MATCH" && payload == "-" {
			payload = ""
		}
		if err := rules.Validate(rules.Entry{Name: args[2], Kind: "rule", Type: typ, Payload: payload, Target: args[5]}); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "rules test" {
		if _, err := ruleTestArguments(args); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "rules import" || spec.path == "rules update" {
		if err := validateRuleImportArguments(args); err != nil {
			return syntaxError(err, spec)
		}
	}
	if spec.path == "lan enable" && n == 1 && net.ParseIP(args[2]) == nil {
		return syntaxError(messageError("cli.error.validation.address_must_be_an_ipv4_or_ipv6_address", "ADDRESS must be an IPv4 or IPv6 address"), spec)
	}
	if spec.path == "diagnostics" && n == 2 {
		if args[1] != "export" || !filepath.IsAbs(args[2]) {
			return syntaxError(messageError("cli.error.validation.export_requires_an_absolute_output_path", "export requires an absolute output path"), spec)
		}
	}
	if spec.path == "mode" && n == 1 {
		if args[1] != "rule" && args[1] != "global" && args[1] != "direct" && args[1] != "saved" {
			return syntaxError(messageError("cli.error.validation.mode_must_be_rule_global_or_direct", "mode must be rule, global, or direct"), spec)
		}
	}
	if spec.path == "mode" && n == 2 && (args[1] != "save" || args[2] != "rule" && args[2] != "global" && args[2] != "direct") {
		return syntaxError(messageError("cli.error.validation.mode_save_requires_rule_global_or_direct", "mode save requires rule, global, or direct"), spec)
	}
	if spec.path == "connections close" && strings.TrimSpace(args[2]) == "" {
		return syntaxError(messageError("cli.error.validation.connection_id_required", "connection ID required"), spec)
	}
	return nil
}

// Hint categories are semantic identities shared by commands with the same
// authored guidance. Usage resources embed that guidance, keeping descriptor
// arguments independent of the selected display language.
var helpHintCategories = map[string]string{
	"config reload":                    "config.reload",
	"connections close":                "connections.close",
	"connections show":                 "connections.close",
	"profile import":                   "profile.import",
	"profile override apply-default":   "profile.override.apply-default",
	"profile remove":                   "profile.remove",
	"profile use":                      "profile.use",
	"proxy auto":                       "proxy.auto",
	"proxy delay":                      "proxy.delay",
	"proxy delays":                     "proxy.delays",
	"proxy provider add":               "proxy.provider.add",
	"proxy provider remove":            "proxy.provider.remove",
	"proxy restore":                    "proxy.restore",
	"proxy search":                     "proxy.search",
	"proxy select":                     "proxy.select",
	"proxy show":                       "proxy.delays",
	"proxy test":                       "proxy.test",
	"proxy test group":                 "proxy.test.group",
	"proxy test provider":              "proxy.test.provider",
	"request-rewrite":                  "request-rewrite.ca.export",
	"request-rewrite ca":               "request-rewrite.ca.export",
	"request-rewrite ca export":        "request-rewrite.ca.export",
	"request-rewrite ca generate":      "request-rewrite.ca.export",
	"request-rewrite ca status":        "request-rewrite.ca.export",
	"request-rewrite ca trust":         "request-rewrite.ca.export",
	"request-rewrite ca trust install": "request-rewrite.ca.export",
	"request-rewrite ca trust remove":  "request-rewrite.ca.export",
	"request-rewrite capture":          "request-rewrite.ca.export",
	"request-rewrite capture apps":     "request-rewrite.ca.export",
	"request-rewrite capture body":     "request-rewrite.ca.export",
	"request-rewrite capture check":    "request-rewrite.ca.export",
	"request-rewrite capture clear":    "request-rewrite.ca.export",
	"request-rewrite capture draft":    "request-rewrite.ca.export",
	"request-rewrite capture export":   "request-rewrite.ca.export",
	"request-rewrite capture fixture":  "request-rewrite.ca.export",
	"request-rewrite capture import":   "request-rewrite.ca.export",
	"request-rewrite capture list":     "request-rewrite.ca.export",
	"request-rewrite capture pause":    "request-rewrite.ca.export",
	"request-rewrite capture resume":   "request-rewrite.ca.export",
	"request-rewrite capture show":     "request-rewrite.ca.export",
	"request-rewrite capture start":    "request-rewrite.ca.export",
	"request-rewrite capture status":   "request-rewrite.ca.export",
	"request-rewrite capture stop":     "request-rewrite.ca.export",
	"request-rewrite disable":          "request-rewrite.ca.export",
	"request-rewrite draft":            "request-rewrite.ca.export",
	"request-rewrite draft preview":    "request-rewrite.ca.export",
	"request-rewrite draft save":       "request-rewrite.ca.export",
	"request-rewrite enable":           "request-rewrite.ca.export",
	"request-rewrite export":           "request-rewrite.ca.export",
	"request-rewrite import":           "request-rewrite.ca.export",
	"request-rewrite preview":          "request-rewrite.ca.export",
	"request-rewrite rules":            "request-rewrite.ca.export",
	"request-rewrite rules add":        "request-rewrite.ca.export",
	"request-rewrite rules disable":    "request-rewrite.ca.export",
	"request-rewrite rules enable":     "request-rewrite.ca.export",
	"request-rewrite rules list":       "request-rewrite.ca.export",
	"request-rewrite rules remove":     "request-rewrite.ca.export",
	"request-rewrite rules reorder":    "request-rewrite.ca.export",
	"request-rewrite rules show":       "request-rewrite.ca.export",
	"request-rewrite rules update":     "request-rewrite.ca.export",
	"request-rewrite scripts":          "request-rewrite.ca.export",
	"request-rewrite scripts add":      "request-rewrite.ca.export",
	"request-rewrite scripts list":     "request-rewrite.ca.export",
	"request-rewrite scripts remove":   "request-rewrite.ca.export",
	"request-rewrite scripts show":     "request-rewrite.ca.export",
	"request-rewrite scripts source":   "request-rewrite.ca.export",
	"request-rewrite scripts update":   "request-rewrite.ca.export",
	"request-rewrite status":           "request-rewrite.ca.export",
	"request-rewrite validate":         "request-rewrite.ca.export",
	"rewrite":                          "rewrite.ca.export",
	"rewrite ca":                       "rewrite.ca.export",
	"rewrite ca export":                "rewrite.ca.export",
	"rewrite ca generate":              "rewrite.ca.export",
	"rewrite ca status":                "rewrite.ca.export",
	"rewrite ca trust":                 "rewrite.ca.export",
	"rewrite ca trust install":         "rewrite.ca.export",
	"rewrite ca trust remove":          "rewrite.ca.export",
	"rewrite capture":                  "rewrite.ca.export",
	"rewrite capture apps":             "rewrite.ca.export",
	"rewrite capture body":             "rewrite.ca.export",
	"rewrite capture check":            "rewrite.ca.export",
	"rewrite capture clear":            "rewrite.ca.export",
	"rewrite capture draft":            "rewrite.ca.export",
	"rewrite capture export":           "rewrite.ca.export",
	"rewrite capture fixture":          "rewrite.ca.export",
	"rewrite capture import":           "rewrite.ca.export",
	"rewrite capture list":             "rewrite.ca.export",
	"rewrite capture pause":            "rewrite.ca.export",
	"rewrite capture resume":           "rewrite.ca.export",
	"rewrite capture show":             "rewrite.ca.export",
	"rewrite capture start":            "rewrite.ca.export",
	"rewrite capture status":           "rewrite.ca.export",
	"rewrite capture stop":             "rewrite.ca.export",
	"rewrite disable":                  "rewrite.ca.export",
	"rewrite draft":                    "rewrite.ca.export",
	"rewrite draft preview":            "rewrite.ca.export",
	"rewrite draft save":               "rewrite.ca.export",
	"rewrite enable":                   "rewrite.ca.export",
	"rewrite export":                   "rewrite.ca.export",
	"rewrite import":                   "rewrite.ca.export",
	"rewrite preview":                  "rewrite.ca.export",
	"rewrite rules":                    "rewrite.ca.export",
	"rewrite rules add":                "rewrite.ca.export",
	"rewrite rules disable":            "rewrite.ca.export",
	"rewrite rules enable":             "rewrite.ca.export",
	"rewrite rules list":               "rewrite.ca.export",
	"rewrite rules remove":             "rewrite.ca.export",
	"rewrite rules reorder":            "rewrite.ca.export",
	"rewrite rules show":               "rewrite.ca.export",
	"rewrite rules update":             "rewrite.ca.export",
	"rewrite scripts":                  "rewrite.ca.export",
	"rewrite scripts add":              "rewrite.ca.export",
	"rewrite scripts list":             "rewrite.ca.export",
	"rewrite scripts remove":           "rewrite.ca.export",
	"rewrite scripts show":             "rewrite.ca.export",
	"rewrite scripts source":           "rewrite.ca.export",
	"rewrite scripts update":           "rewrite.ca.export",
	"rewrite status":                   "rewrite.ca.export",
	"rewrite validate":                 "rewrite.ca.export",
	"rules connection":                 "rules.connection",
	"rules disable":                    "rules.disable",
	"rules enable":                     "rules.disable",
	"rules remove":                     "rules.disable",
	"rules test":                       "rules.test",
	"subscription apply":               "subscription.apply",
	"subscription key":                 "subscription.key",
	"subscription preview":             "subscription.key",
	"subscription remove":              "subscription.key",
	"subscription schedule":            "subscription.key",
	"subscription update":              "subscription.key",
	"subscription watch":               "subscription.watch",
	"traffic watch":                    "traffic.watch",
}

func syntaxError(problem any, spec *commandSpec) error {
	var original string
	var descriptor i18n.Descriptor
	switch value := problem.(type) {
	case *i18n.Message:
		original = value.Error()
		descriptor = value.Descriptor()
	case error:
		original = value.Error()
		if message, ok := value.(*i18n.Message); ok {
			descriptor = message.Descriptor()
		}
	case string:
		original = value
	}
	message := fmt.Sprintf("%s\nUsage: nagi %s", original, spec.syntax)
	hint := ""
	if spec.hint != "" {
		hint = "\n" + spec.hint
		message += hint
	}
	message += "\nRun `nagi help " + spec.path + "` for details"
	if descriptor.ID == "" {
		descriptor = i18n.Descriptor{ID: "cli.error.validation.external", Args: map[string]any{"problem": output.RedactError(original)}}
	}
	if descriptor.Args == nil {
		descriptor.Args = map[string]any{}
	}
	descriptor.Args["syntax"] = spec.syntax
	descriptor.Args["path"] = spec.path
	if category := helpHintCategories[spec.path]; category != "" {
		return i18n.NewMessage("help.usage."+descriptor.ID+"."+category, descriptor.Args, message, nil)
	}
	descriptor.Args["hint"] = hint
	return i18n.NewMessage(descriptor.ID+".usage", descriptor.Args, message, nil)
}
