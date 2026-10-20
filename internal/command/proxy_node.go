package command

import (
	"context"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/menu"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/profile"
	"io"
	"os"
	"strings"
)

type nodeOptions struct {
	profile.TailscaleOptions
	stdin  bool
	groups []string
}

func nodeInvocation(args []string) bool {
	return len(args) > 1 && args[0] == "proxy" && args[1] == "node"
}
func nodeRead(args []string) bool {
	return len(args) > 2 && (args[2] == "list" || args[2] == "show" || args[2] == "groups" && len(args) > 3 && args[3] == "show")
}
func nodeArguments(args []string) (nodeOptions, error) {
	o := nodeOptions{}
	if len(args) < 3 {
		return o, messageError("cli.error.proxy_node.missing_node_action", "missing node action")
	}
	action := args[2]
	if action == "list" {
		return o, nil
	}
	if action == "groups" {
		if len(args) < 5 {
			return o, messageError("cli.error.proxy_node.groups_requires_show_set_or_clear_and_name", "groups requires show, set, or clear and NAME")
		}
		if args[3] != "show" && args[3] != "set" && args[3] != "clear" {
			return o, messageError("cli.error.proxy_node.unknown_groups_action", "unknown groups action")
		}
		if err := profile.ValidateTailscaleName(args[4]); err != nil {
			return o, err
		}
		seen := map[string]bool{}
		for _, g := range args[5:] {
			if err := profile.ValidateTailscaleName(g); err != nil {
				return o, err
			}
			if seen[g] {
				return o, messageError("cli.error.proxy_node.duplicate_group", "duplicate GROUP")
			}
			seen[g] = true
		}
		return o, nil
	}
	index, start := 3, 4
	if action == "add" {
		if len(args) < 6 || args[3] != "tailscale" {
			return o, messageError("cli.error.proxy_node.add_requires_tailscale_name_exit_node", "add requires tailscale NAME EXIT_NODE")
		}
		index, start = 4, 6
		o.ExitNode = &args[5]
	}
	if len(args) <= index {
		return o, messageError("cli.error.proxy_node.name_is_required", "NAME is required")
	}
	if err := profile.ValidateTailscaleName(args[index]); err != nil {
		return o, err
	}
	if action != "add" && action != "edit" {
		return o, nil
	}
	seen := map[string]bool{}
	for i := start; i < len(args); i++ {
		flag := args[i]
		if seen[flag] && flag != "--group" {
			return o, messageError("cli.error.proxy_node.duplicate_node_option", "duplicate node option")
		}
		seen[flag] = true
		if flag == "--auth-key" || flag == "--auth-key-stdin" {
			if o.AuthKey != nil || o.AuthKeyFile != nil {
				return o, messageError("cli.error.proxy_node.choose_one_auth_key_input_method", "choose one auth key input method")
			}
			key := ""
			o.AuthKey = &key
			o.stdin = flag == "--auth-key-stdin"
			continue
		}
		if i+1 >= len(args) {
			return o, messageError("cli.error.proxy_node.option_requires_a_value", "option requires a value")
		}
		i++
		switch flag {
		case "--exit-node":
			if action != "edit" {
				return o, messageError("cli.error.proxy_node.exit_node_is_only_valid_for_edit", "--exit-node is only valid for edit")
			}
			o.ExitNode = &args[i]
		case "--hostname":
			o.Hostname = &args[i]
		case "--control-url":
			o.ControlURL = &args[i]
		case "--auth-key-file":
			if o.AuthKey != nil || o.AuthKeyFile != nil {
				return o, messageError("cli.error.proxy_node.choose_one_auth_key_input_method", "choose one auth key input method")
			}
			o.AuthKeyFile = &args[i]
		case "--group":
			if action != "add" {
				return o, messageError("cli.error.proxy_node.use_node_groups_set_to_change_groups", "use node groups set to change groups")
			}
			if err := profile.ValidateTailscaleName(args[i]); err != nil {
				return o, err
			}
			for _, g := range o.groups {
				if g == args[i] {
					return o, messageError("cli.error.proxy_node.duplicate_group", "duplicate GROUP")
				}
			}
			o.groups = append(o.groups, args[i])
		default:
			return o, messageError("cli.error.proxy_node.unknown_node_option", "unknown node option")
		}
	}
	if action == "edit" && len(seen) == 0 {
		return o, messageError("cli.error.proxy_node.edit_requires_at_least_one_option", "edit requires at least one option")
	}
	if action == "add" && o.AuthKeyFile == nil && o.AuthKey == nil {
		key := ""
		o.AuthKey = &key
	}
	return o, profile.ValidateTailscaleOptions(o.TailscaleOptions)
}
func nodeCommand(ctx context.Context, args []string, store *profile.Store, name string, reloaded bool) (any, error) {
	action := args[2]
	switch action {
	case "list":
		nodes, err := store.ProxyNodes(name)
		if err != nil {
			return nil, err
		}
		groups, err := store.ProxyNodeGroupCandidates(name)
		if err != nil {
			return nil, err
		}
		providers, err := store.ProxyNodeProviderCandidates(name)
		return map[string]any{"profile": name, "nodes": nodes, "group_candidates": groups, "provider_candidates": providers}, err
	case "show":
		node, err := store.ProxyNode(name, args[3])
		return map[string]any{"profile": name, "node": node}, err
	case "groups":
		if args[3] == "show" {
			groups, err := store.ProxyNodeGroups(name, args[4])
			return map[string]any{"profile": name, "node": args[4], "groups": groups}, err
		}
	}
	o, err := nodeArguments(args)
	if err != nil {
		return nil, err
	}
	if o.AuthKey != nil {
		input, ok := ctx.Value(tailscaleAuthInputKey{}).(tailscaleAuthInput)
		if !ok {
			return nil, messageError("cli.error.proxy_node.auth_key_input_unavailable", "auth key input unavailable")
		}
		o.AuthKey = input.key
	}
	validate := store.Validate
	if validate != nil {
		store.Validate = func(data []byte) error {
			if err := validate(data); err != nil {
				return messageError("cli.error.tailscale.tailscale_configuration_validation_failed_inspect_a_local_copy_with_", "Tailscale configuration validation failed; inspect a local copy with mihomo -t --check-json")
			}
			return nil
		}
	}
	node := args[3]
	switch action {
	case "add":
		node = args[4]
		err = store.AddProxyNode(ctx, name, node, o.TailscaleOptions, o.groups)
	case "edit":
		err = store.EditProxyNode(ctx, name, node, o.TailscaleOptions)
	case "remove":
		err = store.RemoveProxyNode(ctx, name, node)
	case "groups":
		node = args[4]
		if args[3] == "set" {
			err = store.SetProxyNodeGroups(ctx, name, node, args[5:])
		} else {
			err = store.ClearProxyNodeGroups(ctx, name, node)
		}
		action = "groups " + args[3]
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"profile": name, "name": node, "action": action, "saved": true, "reloaded": reloaded, "resource": "node"}, nil
}

type tailscaleAuthInputKey struct{}
type tailscaleAuthInput struct{ key *string }

// Input is kept outside argv and read before configuration locking. Prompts
// deliberately use the terminal's normal echo, matching the menu paste flow.
func prepareTailscaleAuthInput(args []string, in io.Reader, prompt io.Writer, jsonMode, stdinMode bool, locales ...i18n.Context) (*string, error) {
	if stdinMode {
		data, err := io.ReadAll(io.LimitReader(in, profile.MaxTailscaleAuthKeyBytes+1))
		if err != nil {
			return nil, fail("proxy_node_error", messageError("cli.error.tailscale.could_not_read_auth_key_from_stdin", "could not read auth key from stdin"))
		}
		key := string(data)
		if err := profile.ValidateTailscaleAuthKey(key); err != nil {
			return nil, fail("proxy_node_error", err)
		}
		key = strings.TrimSpace(key)
		return &key, nil
	}
	file, ok := in.(*os.File)
	if jsonMode || !ok || !menu.IsTerminal(file) {
		return nil, fail("usage", messageError("cli.error.tailscale.auth_key_paste_requires_terminal_input_without_json_use_key_file_aut", "auth key paste requires terminal input without --json; use --auth-key-file FILE, or --auth-key-stdin for automation"))
	}
	locale := i18n.New(i18n.English)
	if len(locales) > 0 {
		locale = locales[0]
	}
	label := locale.Render(messageError("cli.prompt.tailscale.auth_key", "Paste Auth Key (input is visible)"))
	if args[2] == "edit" {
		label += locale.Render(messageError("cli.prompt.tailscale.auth_key.keep", "; blank keeps the existing key"))
	}
	session := menu.NewSession(nil, in, prompt)
	for {
		key, err := session.Prompt(label)
		if err != nil {
			return nil, fail("proxy_node_error", messageError("cli.error.tailscale.auth_key_input_cancelled_no_changes_saved", "auth key input cancelled; no changes saved"))
		}
		if key == "" && args[2] == "edit" {
			return nil, nil
		}
		if err := profile.ValidateTailscaleAuthKey(key); err != nil {
			output.WriteErrorCause(prompt, false, locale, "proxy_node_error", err)
			continue
		}
		return &key, nil
	}
}
