package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ronigooja/Nagi/internal/menu"
	"github.com/ronigooja/Nagi/internal/profile"
)

type tailscaleOptions struct {
	profile.TailscaleOptions
	stdin bool
}

func tailscaleArguments(args []string) (tailscaleOptions, error) {
	o := tailscaleOptions{}
	if len(args) < 3 {
		return o, errors.New("missing Tailscale action")
	}
	action := args[2]
	if action == "show" || action == "remove" || action == "bind" {
		if len(args) < 4 {
			return o, errors.New("NAME is required")
		}
		if err := profile.ValidateTailscaleName(args[3]); err != nil {
			return o, err
		}
	}
	start := 4
	if action == "add" {
		if len(args) < 5 {
			return o, errors.New("add requires NAME EXIT_NODE")
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
		if len(args) <= index || (args[index] != "node" && args[index] != "provider") {
			return o, errors.New("KIND must be node or provider")
		}
		if len(args) <= index+1 {
			return o, errors.New("TARGET is required")
		}
		if err := profile.ValidateTailscaleName(args[index+1]); err != nil {
			return o, errors.New("TARGET must contain 1..128 printable bytes and must not be reserved")
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
			return o, errors.New("duplicate Tailscale option")
		}
		seen[flag] = true
		if flag == "--auth-key" || flag == "--auth-key-stdin" {
			if o.AuthKeyFile != nil || o.AuthKey != nil {
				return o, errors.New("choose one auth key input method")
			}
			key := ""
			o.AuthKey = &key // Input is resolved before taking configuration locks.
			o.stdin = flag == "--auth-key-stdin"
			i++
			continue
		}
		if i+1 >= len(args) {
			return o, errors.New("option requires a value")
		}
		switch flag {
		case "--exit-node":
			if action != "edit" {
				return o, errors.New("--exit-node is only valid for edit")
			}
			o.ExitNode = &args[i+1]
		case "--auth-key-file":
			if action != "edit" {
				return o, errors.New("--auth-key-file is only valid for edit")
			}
			o.AuthKeyFile = &args[i+1]
		case "--hostname":
			o.Hostname = &args[i+1]
		case "--control-url":
			o.ControlURL = &args[i+1]
		default:
			return o, errors.New("unknown Tailscale option")
		}
		i += 2
	}
	if action == "edit" && len(seen) == 0 {
		return o, errors.New("edit requires at least one option")
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
func tailscaleCommand(ctx context.Context, args []string, store *profile.Store, name string, reloaded bool) (any, error) {
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
		return map[string]any{"profile": name, "targets": targets}, err
	}
	o, err := tailscaleArguments(args)
	if err != nil {
		return nil, err
	}
	if o.AuthKey != nil {
		input, ok := ctx.Value(tailscaleAuthInputKey{}).(tailscaleAuthInput)
		if !ok {
			return nil, errors.New("auth key input unavailable")
		}
		o.AuthKey = input.key
	}
	node, kind, target := "", "", ""
	if action == "unbind" {
		kind, target = args[3], args[4]
	} else {
		node = args[3]
		if action == "bind" {
			kind, target = args[4], args[5]
		}
	}
	// Native diagnostics may quote arbitrary configuration values. Keep auth keys
	// and local key file paths outside the public response boundary.
	validate := store.Validate
	if validate != nil {
		store.Validate = func(data []byte) error {
			if err := validate(data); err != nil {
				return errors.New("Tailscale configuration validation failed; inspect a local copy with mihomo -t --check-json")
			}
			return nil
		}
	}
	if err := store.MutateTailscale(ctx, name, action, node, kind, target, o.TailscaleOptions); err != nil {
		return nil, err
	}
	result := map[string]any{"profile": name, "action": action, "saved": true, "reloaded": reloaded}
	if node != "" {
		result["name"] = node
	}
	if kind != "" {
		result["kind"] = kind
		result["target"] = target
	}
	return result, nil
}

type tailscaleAuthInputKey struct{}
type tailscaleAuthInput struct{ key *string }

// Input is kept outside argv and read before configuration locking. Prompts
// deliberately use the terminal's normal echo, matching the menu paste flow.
func prepareTailscaleAuthInput(args []string, in io.Reader, prompt io.Writer, jsonMode, stdinMode bool) (*string, error) {
	if stdinMode {
		data, err := io.ReadAll(io.LimitReader(in, profile.MaxTailscaleAuthKeyBytes+1))
		if err != nil {
			return nil, fail("proxy_tailscale_error", errors.New("could not read auth key from stdin"))
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
		return nil, fail("usage", errors.New("auth key paste requires terminal input without --json; use KEY_FILE, --auth-key-file FILE, or --auth-key-stdin for automation"))
	}
	label := "Paste Auth Key (input is visible)"
	if args[2] == "edit" {
		label += "; blank keeps the existing key"
	}
	session := menu.NewSession(nil, in, prompt)
	for {
		key, err := session.Prompt(label)
		if err != nil {
			return nil, fail("proxy_tailscale_error", errors.New("auth key input cancelled; no changes saved"))
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
