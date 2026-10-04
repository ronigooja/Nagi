package command

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type commandSpec struct {
	path, syntax, summary, detail, example, hint string
	minArgs, maxArgs                             int
}

// Keep command syntax and help in one place. Group entries have maxArgs -1.
var commandSpecs = []commandSpec{
	{"start", "start", "Start mihomo", "Starts mihomo with the selected profile; creates the default profile if needed.", "nagi start", "", 0, 0},
	{"stop", "stop", "Stop mihomo", "Stops a running mihomo process.", "nagi stop", "", 0, 0},
	{"restart", "restart", "Restart mihomo", "Restarts mihomo with the selected profile.", "nagi restart", "", 0, 0},
	{"status", "status", "Show process status", "Shows running state, selected profile, PID, and runtime paths.", "nagi status", "", 0, 0},
	{"doctor", "doctor", "Inspect the local installation", "Checks local paths, mihomo executable, selected profile, process marker, and Unix Socket API without changing files. Exit status is zero for a completed report; inspect healthy and checks for problems.", "nagi --json doctor", "", 0, 0},
	{"logs", "logs [lines]", "Show recent logs", "Shows the last 100 lines by default; lines must be an integer from 1 to 10000.", "nagi logs 50", "", 0, 1},
	{"config", "config <validate|show>", "Inspect the selected configuration", "Validate or print the selected profile.", "nagi config validate", "", 0, -1},
	{"config validate", "config validate", "Validate the selected profile", "Runs mihomo -t against the selected profile; requires a mihomo executable.", "nagi config validate", "", 0, 0},
	{"config show", "config show", "Print the selected profile", "Prints the selected profile YAML.", "nagi config show", "", 0, 0},
	{"profile", "profile <list|use|import|export|backup|restore|diff|override|remove>", "Manage local profiles", "Profile names use letters, digits, _ or -, up to 128 characters.", "nagi profile list", "", 0, -1},
	{"profile list", "profile list", "List local profiles", "Lists available profiles and marks the current selection.", "nagi profile list", "", 0, 0},
	{"profile use", "profile use NAME", "Select a profile", "Validates and activates an existing profile; reloads a running engine.", "nagi profile use work", "Run `nagi profile list` to find a profile name.", 1, 1},
	{"profile import", "profile import NAME FILE", "Import a profile", "Validates and saves a local YAML file, up to 8 MiB. Use profile use to select it.", "nagi profile import work ./work.yaml", "Run `nagi profile list` to see existing names.", 2, 2},
	{"profile export", "profile export NAME FILE", "Export a profile", "Writes the source YAML to a new file with private permissions; refuses to overwrite an existing file.", "nagi profile export work ./work.yaml", "", 2, 2},
	{"profile backup", "profile backup NAME", "Back up a profile", "Copies the source YAML to profiles/NAME.yaml.bak.", "nagi profile backup work", "", 1, 1},
	{"profile restore", "profile restore NAME", "Restore a profile backup", "Validates and writes profiles/NAME.yaml.bak; reloads if selected and running.", "nagi profile restore work", "", 1, 1},
	{"profile diff", "profile diff NAME [OTHER|backup]", "Compare profile versions", "Shows changed lines against another profile or the latest backup (default).", "nagi profile diff work backup", "", 1, 2},
	{"profile override", "profile override <set NAME FILE|show NAME|clear NAME>", "Manage local overrides", "Stores a YAML mapping separately and applies its keys over the source profile.", "nagi profile override set work ./local.yaml", "", 0, -1},
	{"profile override set", "profile override set NAME FILE", "Save local override", "Validates and saves a YAML mapping, up to 8 MiB; reloads the selected running profile.", "nagi profile override set work ./local.yaml", "", 2, 2},
	{"profile override show", "profile override show NAME", "Show local override", "Prints user-owned override YAML.", "nagi profile override show work", "", 1, 1},
	{"profile override clear", "profile override clear NAME", "Clear local override", "Removes the user-owned override and reloads the selected running profile.", "nagi profile override clear work", "", 1, 1},
	{"profile remove", "profile remove NAME", "Remove a profile", "Removes an unselected local profile and keeps its backup file.", "nagi profile remove old", "Run `nagi profile list` to see profile names.", 1, 1},
	{"subscription", "subscription <list|add NAME URL|update NAME|preview NAME|apply NAME|remove NAME>", "Manage subscriptions", "Stores URLs separately from cached downloads and active profiles.", "nagi subscription list", "", 0, -1},
	{"subscription list", "subscription list", "List subscriptions", "Lists saved names and refresh times without showing URLs.", "nagi subscription list", "", 0, 0},
	{"subscription add", "subscription add NAME URL", "Save a subscription URL", "Saves an HTTP or HTTPS URL; does not download or activate it. Names use letters, digits, _ or -, up to 128 characters.", "nagi subscription add work https://example.com/subscription", "", 2, 2},
	{"subscription update", "subscription update NAME", "Download a subscription", "Validates and caches up to 8 MiB, then reports node changes; does not activate it.", "nagi subscription update work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription preview", "subscription preview NAME", "Preview cached node changes", "Compares cached nodes with the existing profile without changing it.", "nagi subscription preview work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription apply", "subscription apply NAME", "Apply a cached subscription", "Converts supported formats, retains local settings and valid group choices, validates, and selects the profile.", "nagi subscription apply work", "Run `nagi subscription preview NAME` to inspect node changes.", 1, 1},
	{"subscription remove", "subscription remove NAME", "Remove a subscription", "Removes the saved subscription and its cache.", "nagi subscription remove work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"dns", "dns <status|set|exception|tun|query|flush|check>", "Manage DNS protection", "Configures selected-profile DNS in a separate local override; inspects mihomo DNS through its private control API.", "nagi dns status", "", 0, -1},
	{"dns status", "dns status", "Show selected DNS policy", "Reports configured upstreams, IPv6, TUN, and configuration risks.", "nagi dns status", "", 0, 0},
	{"dns check", "dns check", "Check DNS configuration and API", "Audits configuration and probes mihomo DNS; does not prove system-wide leak protection.", "nagi dns check", "", 0, 0},
	{"dns set", "dns set direct URL [URL...]|proxy NODE URL [URL...]", "Set encrypted DNS upstreams", "Saves one or more HTTPS DoH URLs. Direct connects directly; proxy pins DoH to a named non-direct node in the selected profile. No plaintext fallback is configured.", "nagi dns set direct https://1.1.1.1/dns-query", "", 2, 32},
	{"dns exception", "dns exception <add DOMAIN SERVER|remove DOMAIN>", "Manage DNS exceptions", "Exceptions route a domain suffix to an explicit server; plaintext IP servers are allowed only here.", "nagi dns exception add corp.example udp://10.0.0.53:53", "", 0, -1},
	{"dns exception add", "dns exception add DOMAIN SERVER", "Add DNS exception", "Maps a domain suffix to a DoH URL or an explicitly requested plaintext IP server.", "nagi dns exception add corp.example udp://10.0.0.53:53", "", 2, 2},
	{"dns exception remove", "dns exception remove DOMAIN", "Remove DNS exception", "Removes the selected domain suffix from local DNS policy.", "nagi dns exception remove corp.example", "", 1, 1},
	{"dns tun", "dns tun <on|off>", "Configure TUN interception", "Stores TUN auto-route, strict-route, and port 53 hijack settings; actual activation may require platform privileges.", "nagi dns tun on", "", 0, -1},
	{"dns tun on", "dns tun on", "Enable TUN DNS interception", "Configures TUN auto-route, strict-route, and port 53 hijack for IPv4 and IPv6.", "nagi dns tun on", "", 0, 0},
	{"dns tun off", "dns tun off", "Disable TUN DNS interception", "Disables TUN in the selected profile override.", "nagi dns tun off", "", 0, 0},
	{"dns query", "dns query DOMAIN [A|AAAA]", "Query through mihomo DNS", "Queries the running mihomo resolver over the private control socket.", "nagi dns query example.com AAAA", "", 1, 2},
	{"dns flush", "dns flush", "Clear mihomo DNS cache", "Requests mihomo to clear its DNS cache; requires a running control API.", "nagi dns flush", "", 0, 0},
	{"proxy", "proxy <groups|show GROUP|search QUERY|select GROUP NODE|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore>", "Inspect and select proxies", "Reads proxy groups and selections from a running mihomo process and measures node delay.", "nagi proxy groups", "", 0, -1},
	{"proxy groups", "proxy groups", "List proxy groups", "Lists groups and their current selections.", "nagi proxy groups", "", 0, 0},
	{"proxy show", "proxy show GROUP", "Show a proxy group", "Shows one group and its available nodes.", "nagi proxy show 'Proxy Group'", "Run `nagi proxy groups` to find a group name.", 1, 1},
	{"proxy search", "proxy search QUERY", "Search nodes", "Lists case-insensitive node-name matches by group.", "nagi proxy search Hong", "Run `nagi proxy groups` to inspect all nodes.", 1, 1},
	{"proxy select", "proxy select GROUP NODE", "Select a proxy node", "Selects a node in a group and saves the choice for the selected profile.", "nagi proxy select 'Proxy Group' 'Node A'", "Run `nagi proxy groups` and `nagi proxy show GROUP` to find names.", 2, 2},
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
	{"connections", "connections <list|close ID|close-all>", "Inspect connections", "Reads or closes active connections from a running engine.", "nagi connections list", "", 0, -1},
	{"connections list", "connections list", "List active connections", "Shows a snapshot of active connections.", "nagi connections list", "", 0, 0},
	{"connections close", "connections close ID", "Close a connection", "Closes one connection by its mihomo ID.", "nagi connections close 42", "Run `nagi connections list` to find IDs.", 1, 1},
	{"connections close-all", "connections close-all", "Close all connections", "Closes all active connections.", "nagi connections close-all", "", 0, 0},
	{"mode", "mode [rule|global|direct]", "Read or set runtime mode", "Reads mihomo mode or updates it at runtime; profile files are not edited.", "nagi mode rule", "", 0, 1},
	{"service", "service <install|uninstall>", "Manage the user service", "Installs or removes the per-user launchd Agent or systemd service.", "nagi service install", "", 0, -1},
	{"service install", "service install", "Install the user service", "Installs a per-user launchd Agent or systemd service.", "nagi service install", "", 0, 0},
	{"service uninstall", "service uninstall", "Remove the user service", "Removes the per-user launchd Agent or systemd service.", "nagi service uninstall", "", 0, 0},
	{"version", "version", "Show version information", "Shows Nagi and mihomo versions, pinned commit, OS, and architecture.", "nagi --json version", "", 0, 0},
	{"completion", "completion <bash|zsh|fish|candidates groups|nodes [GROUP]>", "Generate shell completion", "Prints a shell completion script or live proxy name candidates.", "nagi completion bash", "", 0, -1},
	{"completion bash", "completion bash", "Generate Bash completion", "Prints a Bash completion script.", "nagi completion bash", "", 0, 0},
	{"completion zsh", "completion zsh", "Generate Zsh completion", "Prints a Zsh completion script.", "nagi completion zsh", "", 0, 0},
	{"completion fish", "completion fish", "Generate Fish completion", "Prints a Fish completion script.", "nagi completion fish", "", 0, 0},
	{"completion candidates", "completion candidates groups|nodes [GROUP]", "List live completion candidates", "Lists current proxy groups or nodes; requires a reachable engine.", "nagi completion candidates nodes 'Proxy Group'", "", 1, 2},
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
	if spec.path == "logs" && n == 1 {
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
	if spec.path == "mode" && n == 1 {
		if args[1] != "rule" && args[1] != "global" && args[1] != "direct" {
			return syntaxError("mode must be rule, global, or direct", spec)
		}
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
