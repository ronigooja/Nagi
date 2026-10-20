package command

import (
	"context"
	"strings"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

type chainOptions struct {
	kind         string
	targets, via []string
	dryRun       bool
}

func chainInvocation(args []string) bool {
	return len(args) > 1 && args[0] == "proxy" && args[1] == "chain"
}
func chainRead(args []string) bool {
	if len(args) < 3 {
		return false
	}
	return args[2] == "list" || args[2] == "show" || args[2] == "candidates" || hasArgument(args, "--dry-run")
}
func hasArgument(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
func chainArguments(args []string) (chainOptions, error) {
	o := chainOptions{}
	if len(args) < 3 {
		return o, messageError("cli.error.proxy_chain.missing_chain_action", "missing chain action")
	}
	action := args[2]
	if action == "list" {
		if len(args) == 4 {
			o.kind = args[3]
		} else {
			return o, nil
		}
	} else {
		if len(args) < 5 {
			return o, messageError("cli.error.proxy_chain.kind_and_target_are_required", "KIND and TARGET are required")
		}
		o.kind = args[3]
	}
	if o.kind != "node" && o.kind != "provider" && o.kind != "subscription" {
		return o, messageError("cli.error.proxy_chain.kind_must_be_node_provider_or_subscription", "KIND must be node, provider, or subscription")
	}
	if action == "list" {
		return o, nil
	}
	seen := map[string]bool{}
	options := false
	for i := 4; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--dry-run":
			if action != "set" && action != "clear" {
				return o, messageError("cli.error.proxy_chain.dry_run_is_only_valid_for_set_or_clear", "--dry-run is only valid for set or clear")
			}
			if o.dryRun {
				return o, messageError("cli.error.proxy_chain.duplicate_dry_run", "duplicate --dry-run")
			}
			o.dryRun = true
			options = true
		case "--via":
			if action != "set" {
				return o, messageError("cli.error.proxy_chain.via_is_only_valid_for_set", "--via is only valid for set")
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return o, messageError("cli.error.proxy_chain.via_requires_a_node_name", "--via requires a node name")
			}
			i++
			if err := profile.ValidateTailscaleName(args[i]); err != nil {
				return o, err
			}
			o.via = append(o.via, args[i])
			options = true
		default:
			if options || strings.HasPrefix(arg, "--") {
				return o, messageError("cli.error.proxy_chain.target_must_precede_options_unknown_chain_option", "TARGET must precede options; unknown chain option")
			}
			if err := profile.ValidateTailscaleName(arg); err != nil {
				return o, err
			}
			if seen[arg] {
				return o, messageError("cli.error.proxy_chain.duplicate_target", "duplicate TARGET")
			}
			seen[arg] = true
			o.targets = append(o.targets, arg)
		}
	}
	if len(o.targets) == 0 {
		return o, messageError("cli.error.proxy_chain.target_is_required", "TARGET is required")
	}
	if action == "show" && len(o.targets) != 1 {
		return o, messageError("cli.error.proxy_chain.show_requires_exactly_one_target", "show requires exactly one TARGET")
	}
	if action == "set" && len(o.via) == 0 {
		return o, messageError("cli.error.proxy_chain.set_requires_at_least_one_via_node_in_traffic_order", "set requires at least one --via NODE in traffic order")
	}
	if o.kind == "subscription" && len(o.targets) != 1 {
		return o, messageError("cli.error.proxy_chain.select_exactly_one_subscription_associated_with_the_selected_profile", "select exactly one subscription associated with the selected profile")
	}
	return o, nil
}
func chainSubscriptions(cache *subscription.Store, store *profile.Store, name string) ([]profile.ProxyChainTarget, error) {
	entries, err := cache.List()
	if err != nil {
		return nil, err
	}
	result := []profile.ProxyChainTarget{}
	policy, err := store.SubscriptionChain(name)
	if err != nil {
		return nil, err
	}
	source, err := store.Show(name)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Profile != name {
			continue
		}
		via := []string{}
		if policy != nil && policy.Subscription == entry.Name {
			via = append(via, policy.Via...)
		} else if subscription.Hash(source) != entry.SourceHash {
			continue
		}
		result = append(result, profile.ProxyChainTarget{Kind: "subscription", Name: entry.Name, Type: "subscription", Via: via})
	}

	return result, nil
}
func validateChainSubscription(cache *subscription.Store, store *profile.Store, name string, o chainOptions, checkHash bool) error {
	if o.kind != "subscription" {
		return nil
	}
	entry, err := cache.Find(o.targets[0])
	if err != nil {
		return err
	}
	if entry.Profile != name {
		return messageError("cli.error.proxy_chain.subscription_not_associated", "subscription is not associated with the selected profile; run `nagi subscription apply NAME` first")
	}
	if checkHash {
		source, err := store.Show(name)
		if err != nil {
			return err
		}
		if subscription.Hash(source) != entry.SourceHash {
			return messageError("cli.error.proxy_chain.subscription_source_changed", "subscription source profile changed; run `nagi subscription apply NAME` before setting its chain")
		}
	}
	return nil
}
func chainCommand(ctx context.Context, args []string, store *profile.Store, cache *subscription.Store, name string, reloaded bool) (any, error) {
	o, err := chainArguments(args)
	if err != nil {
		return nil, err
	}
	action := args[2]
	if action == "list" {
		targets, err := store.ProxyChains(name)
		if err != nil {
			return nil, err
		}
		entire, err := chainSubscriptions(cache, store, name)
		if err != nil {
			return nil, err
		}
		all := append(targets, entire...)
		filtered := []profile.ProxyChainTarget{}
		seen := map[string]bool{}
		for _, t := range all {
			key := t.Kind + "\x00" + t.Name
			if seen[key] || o.kind != "" && o.kind != t.Kind {
				continue
			}
			seen[key] = true
			filtered = append(filtered, t)
		}
		return map[string]any{"profile": name, "chains": filtered}, nil
	}
	if err := validateChainSubscription(cache, store, name, o, action == "set"); err != nil {
		return nil, err
	}
	switch action {
	case "show":
		target, err := store.ProxyChain(name, o.kind, o.targets[0])
		if o.kind == "subscription" && err != nil {
			policy, e := store.SubscriptionChain(name)
			if e != nil {
				return nil, e
			}
			if policy == nil {
				target = profile.ProxyChainTarget{Kind: o.kind, Name: o.targets[0], Type: "subscription", Via: []string{}}
				err = nil
			}
		}
		return map[string]any{"profile": name, "chain": target}, err
	case "candidates":
		nodes, err := store.ProxyChainCandidates(name, o.kind, o.targets)
		return map[string]any{"profile": name, "nodes": nodes}, err
	}
	var plan profile.ProxyChainPlan
	if o.dryRun {
		plan, err = store.PlanProxyChain(name, o.kind, o.targets, o.via, action == "clear")
	} else {
		plan, err = store.MutateProxyChain(ctx, name, o.kind, o.targets, o.via, action == "clear")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"profile": name, "action": action, "plan": plan, "dry_run": o.dryRun, "saved": !o.dryRun, "reloaded": reloaded && !o.dryRun, "resource": "chain"}, nil
}
