package command

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
)

func groupInvocation(args []string) bool {
	return len(args) > 1 && args[0] == "proxy" && args[1] == "group"
}
func groupRead(args []string) bool { return len(args) > 2 && (args[2] == "list" || args[2] == "show") }
func groupArguments(args []string) (map[string]any, error) {
	patch := map[string]any{}
	if len(args) < 3 {
		return nil, messageError("cli.error.group.action", "expected group list, show, add, edit, remove, or order")
	}
	if args[2] == "list" {
		return patch, nil
	}
	if args[2] == "order" {
		return patch, profile.ValidateProxyGroupOrder(args[3:])
	}
	if len(args) < 4 {
		return nil, messageError("cli.error.group.name", "GROUP is required; run nagi proxy group list")
	}
	if err := profile.ValidateTailscaleName(args[3]); err != nil {
		return nil, err
	}
	if args[2] != "add" && args[2] != "edit" {
		return patch, nil
	}
	seen := map[string]bool{}
	for i := 4; i < len(args); i++ {
		flag := args[i]
		if seen[flag] && flag != "--node" && flag != "--provider" {
			return nil, messageError("cli.error.group.duplicate", "duplicate group option: %s", flag)
		}
		seen[flag] = true
		if flag == "--clear-nodes" || flag == "--clear-providers" {
			key := "proxies"
			other := "--node"
			if flag == "--clear-providers" {
				key = "use"
				other = "--provider"
			}
			if seen[other] {
				return nil, messageError("cli.error.group.clear", "cannot combine member options with the corresponding clear option")
			}
			patch[key] = []any{}
			continue
		}
		if i+1 >= len(args) {
			return nil, messageError("cli.error.group.value", "option requires a value: %s", flag)
		}
		i++
		value := args[i]
		switch flag {
		case "--node", "--provider":
			key, clear := "proxies", "--clear-nodes"
			if flag == "--provider" {
				key, clear = "use", "--clear-providers"
			}
			if seen[clear] {
				return nil, messageError("cli.error.group.clear", "cannot combine member options with the corresponding clear option")
			}
			list, _ := patch[key].([]any)
			patch[key] = append(list, value)
		case "--type", "--url", "--strategy":
			patch[strings.TrimPrefix(flag, "--")] = value
		case "--interval", "--tolerance":
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, messageError("cli.error.group.integer", "option requires an integer: %s", flag)
			}
			patch[strings.TrimPrefix(flag, "--")] = n
		default:
			return nil, messageError("cli.error.group.option", "unknown group option: %s", flag)
		}
	}
	if args[2] == "edit" && len(patch) == 0 {
		return nil, messageError("cli.error.group.edit", "edit requires at least one option")
	}
	return patch, profile.ValidateProxyGroupPatch(patch)
}
func groupCommand(ctx context.Context, args []string, store *profile.Store, name string, reloaded bool) (any, error) {
	if args[2] == "order" {
		if err := store.OrderProxyGroups(ctx, name, args[3:]); err != nil {
			return nil, err
		}
		return map[string]any{"profile": name, "resource": "group", "action": "order", "group_order": args[3:], "saved": true, "reloaded": false}, nil
	}

	if groupRead(args) {
		groups, err := store.ProxyGroupConfigs(name)
		if err != nil {
			return nil, err
		}
		if args[2] == "list" {
			return map[string]any{"profile": name, "group_configs": groups}, nil
		}
		for _, g := range groups {
			if g.Name == args[3] {
				return map[string]any{"profile": name, "group_config": g}, nil
			}
		}
		return nil, messageError("cli.error.group.missing", "proxy group not found: %s; run nagi proxy group list", args[3])
	}
	patch, err := groupArguments(args)
	if err != nil {
		return nil, err
	}

	// Choice persistence belongs to the CLI. The outer configuration
	// transaction restores this file if deletion or activation fails.
	if args[2] == "remove" {
		if err := (proxy.SelectionStore{Path: filepath.Join(store.Dir, "proxy-selections.json")}).RemoveGroup(name, args[3]); err != nil {
			return nil, err
		}
	}

	if args[2] == "remove" {
		err = store.RemoveProxyGroup(ctx, name, args[3])
	} else {
		err = store.SaveProxyGroup(ctx, name, args[3], patch, args[2] == "add")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"profile": name, "name": args[3], "resource": "group", "action": args[2], "saved": true, "reloaded": reloaded}, nil
}
