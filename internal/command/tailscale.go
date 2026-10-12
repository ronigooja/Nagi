package command

import (
	"context"
	"errors"

	"github.com/ronigooja/Nagi/internal/profile"
)

func tailscaleArguments(args []string) (profile.TailscaleOptions, error) {
	o := profile.TailscaleOptions{}
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
		if len(args) < 6 {
			return o, errors.New("add requires NAME EXIT_NODE KEY_FILE")
		}
		o.ExitNode = &args[4]
		o.AuthKeyFile = &args[5]
		start = 6
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
	for i := start; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return o, errors.New("option requires a value")
		}
		flag := args[i]
		if seen[flag] {
			return o, errors.New("duplicate Tailscale option")
		}
		seen[flag] = true
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
	}
	if action == "edit" && len(seen) == 0 {
		return o, errors.New("edit requires at least one option")
	}
	if err := profile.ValidateTailscaleName(args[3]); err != nil {
		return o, err
	}
	return o, profile.ValidateTailscaleOptions(o)
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
	if err := store.MutateTailscale(ctx, name, action, node, kind, target, o); err != nil {
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
func tailscaleRead(args []string) bool {
	return len(args) > 2 && args[0] == "proxy" && args[1] == "tailscale" && (args[2] == "list" || args[2] == "show" || args[2] == "targets")
}
func tailscaleInvocation(args []string) bool {
	return len(args) > 1 && args[0] == "proxy" && args[1] == "tailscale"
}
