package command

import (
	"fmt"
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
	{"profile", "profile <list|use NAME|import NAME FILE>", "Manage local profiles", "Profile names use letters, digits, _ or -, up to 128 characters.", "nagi profile list", "", 0, -1},
	{"profile list", "profile list", "List local profiles", "Lists available profiles and marks the current selection.", "nagi profile list", "", 0, 0},
	{"profile use", "profile use NAME", "Select a profile", "Validates and activates an existing profile; reloads a running engine.", "nagi profile use work", "Run `nagi profile list` to find a profile name.", 1, 1},
	{"profile import", "profile import NAME FILE", "Import a profile", "Validates and saves a local YAML file, up to 8 MiB. Use profile use to select it.", "nagi profile import work ./work.yaml", "Run `nagi profile list` to see existing names.", 2, 2},
	{"subscription", "subscription <list|add NAME URL|update NAME|apply NAME|remove NAME>", "Manage subscriptions", "Stores URLs separately from cached downloads and active profiles.", "nagi subscription list", "", 0, -1},
	{"subscription list", "subscription list", "List subscriptions", "Lists saved names and refresh times without showing URLs.", "nagi subscription list", "", 0, 0},
	{"subscription add", "subscription add NAME URL", "Save a subscription URL", "Saves an HTTP or HTTPS URL; does not download or activate it. Names use letters, digits, _ or -, up to 128 characters.", "nagi subscription add work https://example.com/subscription", "", 2, 2},
	{"subscription update", "subscription update NAME", "Download a subscription", "Downloads at most 8 MiB into the local cache; does not activate it.", "nagi subscription update work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription apply", "subscription apply NAME", "Apply a cached subscription", "Validates cached mihomo YAML, imports it as a profile, and selects it.", "nagi subscription apply work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"subscription remove", "subscription remove NAME", "Remove a subscription", "Removes the saved subscription and its cache.", "nagi subscription remove work", "Run `nagi subscription list` to find a subscription name.", 1, 1},
	{"proxy", "proxy <groups|show GROUP|select GROUP NODE>", "Inspect and select proxies", "Reads proxy groups and selections from a running mihomo process.", "nagi proxy groups", "", 0, -1},
	{"proxy groups", "proxy groups", "List proxy groups", "Lists groups and their current selections.", "nagi proxy groups", "", 0, 0},
	{"proxy show", "proxy show GROUP", "Show a proxy group", "Shows one group and its available nodes.", "nagi proxy show 'Proxy Group'", "Run `nagi proxy groups` to find a group name.", 1, 1},
	{"proxy select", "proxy select GROUP NODE", "Select a proxy node", "Selects a node in a group on the running engine.", "nagi proxy select 'Proxy Group' 'Node A'", "Run `nagi proxy groups` and `nagi proxy show GROUP` to find names.", 2, 2},
	{"connections", "connections list", "Inspect connections", "Reads a snapshot of active connections from a running engine.", "nagi connections list", "", 0, -1},
	{"connections list", "connections list", "List active connections", "Shows a snapshot of active connections.", "nagi connections list", "", 0, 0},
	{"service", "service <install|uninstall>", "Manage the user service", "Installs or removes the per-user launchd Agent or systemd service.", "nagi service install", "", 0, -1},
	{"service install", "service install", "Install the user service", "Installs a per-user launchd Agent or systemd service.", "nagi service install", "", 0, 0},
	{"service uninstall", "service uninstall", "Remove the user service", "Removes the per-user launchd Agent or systemd service.", "nagi service uninstall", "", 0, 0},
	{"version", "version", "Show version information", "Shows Nagi and mihomo versions, pinned commit, OS, and architecture.", "nagi --json version", "", 0, 0},
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
