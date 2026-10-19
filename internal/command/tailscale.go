package command

import (
	"context"

	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/menu"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

type tailscaleOptions struct {
	profile.TailscaleOptions
	stdin bool
}

func tailscaleArguments(args []string) (tailscaleOptions, error) {
	o := tailscaleOptions{}
	if len(args) < 3 {
		return o, messageError("cli.error.tailscale.missing_tailscale_action", "missing Tailscale action")
	}
	action := args[2]
	if action == "show" || action == "remove" || action == "bind" {
		if len(args) < 4 {
			return o, messageError("cli.error.tailscale.name_is_required", "NAME is required")
		}
		if err := profile.ValidateTailscaleName(args[3]); err != nil {
			return o, err
		}
	}
	start := 4
	if action == "add" {
		if len(args) < 5 {
			return o, messageError("cli.error.tailscale.add_requires_name_exit_node", "add requires NAME EXIT_NODE")
		}
		o.ExitNode = &args[4]
		start = 5
		if len(args) > 5 && !strings.HasPrefix(args[5], "--") {
			o.AuthKeyFile = &args[5]
			start = 6
		}
	}
	if action == "bind" || action == "unbind" {
		index := 4
		if action == "unbind" {
			index = 3
		}
		if len(args) <= index || (args[index] != "node" && args[index] != "provider" && args[index] != "subscription") {
			return o, messageError("cli.error.tailscale.kind_must_be_node_provider_or_subscription", "KIND must be node, provider, or subscription")
		}
		if len(args) <= index+1 {
			return o, messageError("cli.error.tailscale.target_is_required", "TARGET is required")
		}
		seen := map[string]bool{}
		for _, target := range args[index+1:] {
			if args[index] == "subscription" {
				if target == "" || len(target) > 128 || strings.IndexFunc(target, func(r rune) bool {
					return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
				}) >= 0 {
					return o, messageError("cli.error.tailscale.subscription_target_must_contain_1_128_ascii_letters_digits_or", "subscription TARGET must contain 1..128 ASCII letters, digits, _ or -")
				}
			} else if err := profile.ValidateTailscaleName(target); err != nil {
				return o, messageError("cli.error.tailscale.target_must_contain_1_128_printable_bytes_and_must_not_be_reserved", "TARGET must contain 1..128 printable bytes and must not be reserved")
			}
			if seen[target] {
				return o, messageError("cli.error.tailscale.duplicate_target_list_each_target_once", "duplicate TARGET; list each target once")
			}
			seen[target] = true
		}
		return o, nil
	}
	if action != "add" && action != "edit" {
		return o, nil
	}
	seen := map[string]bool{}
	for i := start; i < len(args); {
		flag := args[i]
		if seen[flag] {
			return o, messageError("cli.error.tailscale.duplicate_tailscale_option", "duplicate Tailscale option")
		}
		seen[flag] = true
		if flag == "--auth-key" || flag == "--auth-key-stdin" {
			if o.AuthKeyFile != nil || o.AuthKey != nil {
				return o, messageError("cli.error.tailscale.choose_one_auth_key_input_method", "choose one auth key input method")
			}
			key := ""
			o.AuthKey = &key // Input is resolved before taking configuration locks.
			o.stdin = flag == "--auth-key-stdin"
			i++
			continue
		}
		if i+1 >= len(args) {
			return o, messageError("cli.error.tailscale.option_requires_a_value", "option requires a value")
		}
		switch flag {
		case "--exit-node":
			if action != "edit" {
				return o, messageError("cli.error.tailscale.exit_node_is_only_valid_for_edit", "--exit-node is only valid for edit")
			}
			o.ExitNode = &args[i+1]
		case "--auth-key-file":
			if action != "edit" {
				return o, messageError("cli.error.tailscale.auth_key_file_is_only_valid_for_edit", "--auth-key-file is only valid for edit")
			}
			o.AuthKeyFile = &args[i+1]
		case "--hostname":
			o.Hostname = &args[i+1]
		case "--control-url":
			o.ControlURL = &args[i+1]
		default:
			return o, messageError("cli.error.tailscale.unknown_tailscale_option", "unknown Tailscale option")
		}
		i += 2
	}
	if action == "edit" && len(seen) == 0 {
		return o, messageError("cli.error.tailscale.edit_requires_at_least_one_option", "edit requires at least one option")
	}
	if action == "add" && o.AuthKeyFile == nil && o.AuthKey == nil {
		key := ""
		o.AuthKey = &key
	}
	if err := profile.ValidateTailscaleName(args[3]); err != nil {
		return o, err
	}
	return o, profile.ValidateTailscaleOptions(o.TailscaleOptions)
}
func tailscaleCommand(ctx context.Context, args []string, store *profile.Store, subscriptions *subscription.Store, name string, reloaded bool) (any, error) {
	action := args[2]
	switch action {
	case "list":
		nodes, err := store.TailscaleNodes(name)
		return map[string]any{"profile": name, "nodes": nodes}, err
	case "show":
		node, bindings, err := store.TailscaleShow(name, args[3])
		return map[string]any{"profile": name, "node": node, "bindings": bindings}, err
	case "targets":
		targets, err := store.TailscaleTargets(name)
		if err == nil {
			var entire []profile.TailscaleTarget
			entire, err = subscriptionTailscaleTargets(subscriptions, store, name)
			targets = append(targets, entire...)
		}
		return map[string]any{"profile": name, "targets": targets}, err
	}
	o, err := tailscaleArguments(args)
	if err != nil {
		return nil, err
	}
	if o.AuthKey != nil {
		input, ok := ctx.Value(tailscaleAuthInputKey{}).(tailscaleAuthInput)
		if !ok {
			return nil, messageError("cli.error.tailscale.auth_key_input_unavailable", "auth key input unavailable")
		}
		o.AuthKey = input.key
	}
	node, kind, target := "", "", ""
	var targets []string
	if action == "unbind" {
		kind, target = args[3], args[4]
		targets = args[4:]
	} else {
		node = args[3]
		if action == "bind" {
			kind, target = args[4], args[5]
			targets = args[5:]
		}
	}
	// Native diagnostics may quote arbitrary configuration values. Keep auth keys
	// and local key file paths outside the public response boundary.
	validate := store.Validate
	if validate != nil {
		store.Validate = func(data []byte) error {
			if err := validate(data); err != nil {
				return messageError("cli.error.tailscale.tailscale_configuration_validation_failed_inspect_a_local_copy_with_", "Tailscale configuration validation failed; inspect a local copy with mihomo -t --check-json")
			}
			return nil
		}
	}
	if kind == "subscription" {
		err = mutateSubscriptionTailscale(ctx, subscriptions, store, name, action, node, targets)
	} else if len(targets) > 0 {
		err = store.MutateTailscaleBindings(ctx, name, action, node, kind, targets)
	} else {
		err = store.MutateTailscale(ctx, name, action, node, kind, target, o.TailscaleOptions)
	}
	if err != nil {
		return nil, err
	}
	result := map[string]any{"profile": name, "action": action, "saved": true, "reloaded": reloaded}
	if node != "" {
		result["name"] = node
	}
	if kind != "" {
		result["kind"] = kind
		if len(targets) == 1 {
			result["target"] = target
		} else {
			result["targets"] = targets
		}
	}
	return result, nil
}

type tailscaleAuthInputKey struct{}
type tailscaleAuthInput struct{ key *string }

// Input is kept outside argv and read before configuration locking. Prompts
// deliberately use the terminal's normal echo, matching the menu paste flow.
func prepareTailscaleAuthInput(args []string, in io.Reader, prompt io.Writer, jsonMode, stdinMode bool, locales ...i18n.Context) (*string, error) {
	if stdinMode {
		data, err := io.ReadAll(io.LimitReader(in, profile.MaxTailscaleAuthKeyBytes+1))
		if err != nil {
			return nil, fail("proxy_tailscale_error", messageError("cli.error.tailscale.could_not_read_auth_key_from_stdin", "could not read auth key from stdin"))
		}
		key := string(data)
		if err := profile.ValidateTailscaleAuthKey(key); err != nil {
			return nil, fail("proxy_tailscale_error", err)
		}
		key = strings.TrimSpace(key)
		return &key, nil
	}
	file, ok := in.(*os.File)
	if jsonMode || !ok || !menu.IsTerminal(file) {
		return nil, fail("usage", messageError("cli.error.tailscale.auth_key_paste_requires_terminal_input_without_json_use_key_file_aut", "auth key paste requires terminal input without --json; use KEY_FILE, --auth-key-file FILE, or --auth-key-stdin for automation"))
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
			return nil, fail("proxy_tailscale_error", messageError("cli.error.tailscale.auth_key_input_cancelled_no_changes_saved", "auth key input cancelled; no changes saved"))
		}
		if key == "" && args[2] == "edit" {
			return nil, nil
		}
		if err := profile.ValidateTailscaleAuthKey(key); err != nil {
			fmt.Fprintln(prompt, err)
			continue
		}
		return &key, nil
	}
}
func tailscaleRead(args []string) bool {
	return len(args) > 2 && args[0] == "proxy" && args[1] == "tailscale" && (args[2] == "list" || args[2] == "show" || args[2] == "targets")
}
func tailscaleInvocation(args []string) bool {
	return len(args) > 1 && args[0] == "proxy" && args[1] == "tailscale"
}
