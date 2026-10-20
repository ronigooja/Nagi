package profile

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"gopkg.in/yaml.v3"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

const proxyAdditionsKey = "nagi-proxy-additions"
const subscriptionChainVersion = 1

// SubscriptionChainPolicy contains sensitive full outbound definitions. Never serialize it in ordinary results.
type SubscriptionChainPolicy struct {
	Version      int              `yaml:"version"`
	Subscription string           `yaml:"subscription"`
	Via          []string         `yaml:"via"`
	Nodes        []map[string]any `yaml:"nodes"`
	SourceNodes  []map[string]any `yaml:"source-nodes,omitempty"`
}

func (s *Store) subscriptionChainPath(name string) (string, error) {
	p, err := s.overridePath(name)
	return p + ".chain-subscription.yaml", err
}
func (s *Store) subscriptionTailscalePath(name string) (string, error) {
	p, err := s.overridePath(name)
	return p + ".tailscale-subscription.yaml", err
}
func (s *Store) SubscriptionChain(name string) (*SubscriptionChainPolicy, error) {
	old, err := s.subscriptionTailscalePath(name)
	if err != nil {
		return nil, err
	}
	if _, err = os.Lstat(old); err == nil {
		return nil, i18n.Errorf("backend.profile.chain_legacy_subscription_tailscale_policy_is_unsupported_remove_the_legacy_policy_explicitly_before_configuring_a_chain", "legacy subscription Tailscale policy is unsupported; remove the legacy policy explicitly before configuring a chain")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	p, err := s.subscriptionChainPath(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, i18n.Errorf("backend.profile.chain_subscription_chain_policy_must_be_a_regular_file", "subscription chain policy must be a regular file")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var policy SubscriptionChainPolicy
	if yaml.Unmarshal(data, &policy) != nil || policy.Version != subscriptionChainVersion || !validName(policy.Subscription) || len(policy.Via) == 0 || len(policy.Nodes) != len(policy.Via) {
		return nil, i18n.Errorf("backend.profile.chain_unsupported_or_invalid_subscription_chain_policy", "unsupported or invalid subscription chain policy")
	}
	seen := map[string]bool{}
	for i, n := range policy.Via {
		if seen[n] || tsString(policy.Nodes[i], "name") != n || !chainHop(tsString(policy.Nodes[i], "type")) {
			return nil, i18n.Errorf("backend.profile.chain_invalid_subscription_chain_definitions", "invalid subscription chain definitions")
		}
		seen[n] = true
	}
	return &policy, nil
}
func (s *Store) writeSubscriptionChain(name string, p *SubscriptionChainPolicy) error {
	path, err := s.subscriptionChainPath(name)
	if err != nil {
		return err
	}
	if p == nil {
		err = os.Remove(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeReplace(path, data)
}

// ClearSubscriptionChain is for profile replacement transactions, not user unbinding.
func (s *Store) ClearSubscriptionChain(name string) error { return s.writeSubscriptionChain(name, nil) }
func sameChainDefinition(a, b map[string]any) bool {
	copyWithout := func(m map[string]any) map[string]any {
		o := map[string]any{}
		for k, v := range m {
			if k != "state-dir" && k != "dialer-proxy" {
				o[k] = v
			}
		}
		return o
	}
	return reflect.DeepEqual(copyWithout(a), copyWithout(b))
}
func sameTailscaleDefinition(a, b map[string]any) bool { return sameChainDefinition(a, b) }
func policyOwnsEdge(p *SubscriptionChainPolicy, kind, name string) bool {
	if kind == "provider" {
		return true
	}
	if kind != "node" {
		return false
	}
	return true
}
func applyChainPolicy(cfg map[string]any, p *SubscriptionChainPolicy) error {
	for _, node := range p.Nodes {
		n := tsString(node, "name")
		providers, _ := cfg["proxy-providers"].(map[string]any)
		if _, ok := providers[n]; ok {
			return i18n.Errorf("backend.profile.chain_subscription_provider_conflicts_with_a_managed_chain_node", "subscription provider conflicts with a managed chain node")
		}
		groups, _ := cfg["proxy-groups"].([]any)
		for _, v := range groups {
			m, _ := v.(map[string]any)
			if tsString(m, "name") == n {
				return i18n.Errorf("backend.profile.chain_subscription_group_conflicts_with_a_managed_chain_node", "subscription group conflicts with a managed chain node")
			}
		}
		existing := tsFind(cfg, n)
		if existing != nil {
			allowed := sameChainDefinition(existing, node)
			for _, source := range p.SourceNodes {
				if tsString(source, "name") == n && sameChainDefinition(source, existing) {
					allowed = true
				}
			}
			if !allowed {
				return i18n.Errorf("backend.profile.chain_subscription_node_conflicts_with_a_managed_chain_definition", "subscription node conflicts with a managed chain definition")
			}
			for i, v := range tsProxies(cfg) {
				m, _ := v.(map[string]any)
				if tsString(m, "name") == n {
					tsProxies(cfg)[i] = node
				}
			}
		} else {
			cfg["proxies"] = append(tsProxies(cfg), node)
		}
	}
	plan, err := prepareChain(cfg, "subscription", []string{p.Subscription}, p.Via, false)
	if err != nil {
		return err
	}
	if len(plan.Conflicts) > 0 {
		return i18n.Errorf("backend.profile.chain_subscription_chain_conflicts_with_an_external_front_proxy", "subscription chain conflicts with an external front proxy")
	}
	return nil
}
func (s *Store) applySubscriptionChain(name string, data []byte) ([]byte, error) {
	p, err := s.SubscriptionChain(name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return data, nil
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil || cfg == nil {
		return nil, i18n.Errorf("backend.profile.chain_subscription_profile_must_be_a_yaml_mapping", "subscription profile must be a YAML mapping")
	}
	if err = applyChainPolicy(cfg, p); err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}
func (s *Store) mutateSubscriptionChain(ctx context.Context, name, subscription string, via []string, clear bool) error {
	old, err := s.SubscriptionChain(name)
	if err != nil {
		return err
	}
	if !validName(subscription) {
		return i18n.Errorf("backend.profile.chain_invalid_subscription_name", "invalid subscription name")
	}
	overlay, err := s.chainOverride(name)
	if err != nil {
		return err
	}
	var next *SubscriptionChainPolicy
	if clear {
		if old == nil || old.Subscription != subscription {
			return i18n.Errorf("backend.profile.chain_subscription_has_no_persistent_chain", "subscription has no persistent chain")
		}
	} else {
		cfg, _, err := s.subscriptionChainCandidate(name, []string{subscription}, via, false)
		if err != nil {
			return err
		}
		next = &SubscriptionChainPolicy{Version: subscriptionChainVersion, Subscription: subscription, Via: append([]string{}, via...), Nodes: []map[string]any{}}
		source, err := s.Show(name)
		if err != nil {
			return err
		}
		var original map[string]any
		if yaml.Unmarshal(source, &original) != nil {
			return i18n.Errorf("backend.profile.chain_invalid_subscription_source", "invalid subscription source")
		}
		for i, n := range via {
			node := tsFind(cfg, n)
			if node == nil || !chainHop(tsString(node, "type")) {
				return i18n.Errorf("backend.profile.chain_invalid_chain_hop", "invalid chain hop")
			}
			cloned := map[string]any{}
			for k, v := range node {
				cloned[k] = v
			}
			cloned["dialer-proxy"] = ""
			if i > 0 {
				cloned["dialer-proxy"] = via[i-1]
			}
			next.Nodes = append(next.Nodes, cloned)
			if m := tsFind(original, n); m != nil {
				next.SourceNodes = append(next.SourceNodes, m)
			}
		}
	}
	// Retain previous chain definitions as ordinary additions, including their edges.
	additions, _ := overlay[proxyAdditionsKey].([]any)
	if old != nil {
		for _, node := range old.Nodes {
			keep := true
			for _, n := range via {
				if tsString(node, "name") == n {
					keep = false
				}
			}
			if keep {
				retained := map[string]any{}
				for k, v := range node {
					retained[k] = v
				}
				if !clear {
					retained["dialer-proxy"] = ""
				}
				additions = append(additions, retained)
			}
		}
	}
	kept := []any{}
	for _, v := range additions {
		m, _ := v.(map[string]any)
		managed := false
		for _, n := range via {
			if tsString(m, "name") == n {
				managed = true
			}
		}
		if !managed {
			kept = append(kept, v)
		}
	}
	if len(kept) > 0 {
		overlay[proxyAdditionsKey] = kept
	} else {
		delete(overlay, proxyAdditionsKey)
	}
	if next != nil {
		if err = s.sanitizeManagedSnapshot(name, overlay, next); err != nil {
			return err
		}
	}
	if err = s.writeSubscriptionChain(name, next); err != nil {
		return err
	}
	data, err := yaml.Marshal(overlay)
	if err == nil {
		err = s.SetOverride(ctx, name, data)
	}
	if err != nil {
		return errors.Join(err, s.writeSubscriptionChain(name, old))
	}
	return nil
}
func (s *Store) ManagedProxyNode(name, node string) (bool, error) {
	p, err := s.SubscriptionChain(name)
	if err != nil || p == nil {
		return false, err
	}
	for _, n := range p.Via {
		if n == node {
			return true, nil
		}
	}
	return false, nil
}
func (s *Store) CheckProxyNodeRemoval(name, node string) error {
	managed, err := s.ManagedProxyNode(name, node)
	if err != nil {
		return err
	}
	if managed {
		return i18n.Errorf("backend.profile.chain_node_belongs_to_a_subscription_chain_clear_the_subscription_chain_first", "node belongs to a subscription chain; clear the subscription chain first")
	}
	return nil
}
func (s *Store) EditManagedProxyNode(ctx context.Context, name, node string, definition map[string]any) error {
	p, err := s.SubscriptionChain(name)
	if err != nil {
		return err
	}
	if p == nil {
		return i18n.Errorf("backend.profile.chain_managed_chain_node_not_found", "managed chain node not found")
	}
	previous, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	found := false
	for i, n := range p.Via {
		if n == node {
			found = true
			definition["dialer-proxy"] = ""
			if i > 0 {
				definition["dialer-proxy"] = p.Via[i-1]
			}
			p.Nodes[i] = definition
		}
	}
	if !found {
		return i18n.Errorf("backend.profile.chain_managed_chain_node_not_found", "managed chain node not found")
	}
	if err = s.writeSubscriptionChain(name, p); err != nil {
		return err
	}
	effective, err := s.Effective(name)
	if err == nil {
		err = s.validate(name, effective)
	}
	if err == nil {
		current, e := s.Current()
		err = e
		if e == nil && current == name && s.Reload != nil {
			err = s.Reload(ctx)
		}
	}
	if err != nil {
		var old SubscriptionChainPolicy
		_ = yaml.Unmarshal(previous, &old)
		return errors.Join(err, s.writeSubscriptionChain(name, &old))
	}
	return nil
}

// validateSubscriptionChainSource checks collisions before ordinary overrides can hide them.
func (s *Store) validateSubscriptionChainSource(name string, data []byte) error {
	p, err := s.SubscriptionChain(name)
	if err != nil || p == nil {
		return err
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil {
		return i18n.Errorf("backend.profile.chain_invalid_subscription_source", "invalid subscription source")
	}
	for _, node := range p.Nodes {
		n := tsString(node, "name")
		if existing := tsFind(cfg, n); existing != nil {
			allowed := sameChainDefinition(existing, node)
			for _, before := range p.SourceNodes {
				if tsString(before, "name") == n && sameChainDefinition(before, existing) {
					allowed = true
				}
			}
			if !allowed {
				return i18n.Errorf("backend.profile.chain_subscription_source_conflicts_with_a_managed_chain_definition", "subscription source conflicts with a managed chain definition")
			}
		}
		providers, _ := cfg["proxy-providers"].(map[string]any)
		if _, ok := providers[n]; ok {
			return i18n.Errorf("backend.profile.chain_subscription_source_provider_conflicts_with_a_managed_chain_node", "subscription source provider conflicts with a managed chain node")
		}
		groups, _ := cfg["proxy-groups"].([]any)
		for _, v := range groups {
			g, _ := v.(map[string]any)
			if tsString(g, "name") == n {
				return i18n.Errorf("backend.profile.chain_subscription_source_group_conflicts_with_a_managed_chain_node", "subscription source group conflicts with a managed chain node")
			}
		}
	}
	return nil
}

// sanitizeManagedSnapshot stores user definitions without materializing policy edges.
func (s *Store) sanitizeManagedSnapshot(name string, overlay map[string]any, p *SubscriptionChainPolicy) error {
	raw, exists := overlay["proxies"]
	if !exists {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return i18n.Errorf("backend.profile.chain_proxies_must_be_a_list", "proxies must be a list")
	}
	source, err := s.Show(name)
	if err != nil {
		return err
	}
	var original map[string]any
	if yaml.Unmarshal(source, &original) != nil {
		return i18n.Errorf("backend.profile.chain_invalid_subscription_source", "invalid subscription source")
	}
	kept := []any{}
	managed := map[string]bool{}
	for _, n := range p.Via {
		managed[n] = true
	}
	last := p.Via[len(p.Via)-1]
	for _, v := range list {
		m, _ := v.(map[string]any)
		n := tsString(m, "name")
		if managed[n] {
			if old := tsFind(original, n); old != nil {
				kept = append(kept, old)
			}
			continue
		}
		if chainSupported(tsString(m, "type")) {
			dialer := tsString(m, "dialer-proxy")
			if dialer != "" && dialer != last {
				return i18n.Errorf("backend.profile.chain_subscription_policy_owns_this_node_edge_clear_the_subscription_chain_first", "subscription policy owns this node edge; clear the subscription chain first")
			}
			if old := tsFind(original, n); old != nil {
				if d, ok := old["dialer-proxy"]; ok {
					m["dialer-proxy"] = d
				} else {
					delete(m, "dialer-proxy")
				}
			} else {
				delete(m, "dialer-proxy")
			}
		}
		kept = append(kept, m)
	}
	// Release a source-equivalent snapshot so feed additions/removals remain visible.
	if reflect.DeepEqual(kept, tsProxies(original)) {
		delete(overlay, "proxies")
	} else {
		overlay["proxies"] = kept
	}
	return nil
}

func (s *Store) unmanagedChainConfig(name string) (map[string]any, error) {
	source, err := s.Show(name)
	if err != nil {
		return nil, err
	}
	patch, patchErr := s.Override(name)
	data, err := s.effectiveSourceWithOverride(name, source, patch, patchErr, true)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil || cfg == nil {
		return nil, i18n.Errorf("backend.profile.chain_invalid_effective_profile", "invalid effective profile")
	}
	return cfg, nil
}

func chainDifferences(before, after []ProxyChainTarget) []ProxyChainChange {
	edges := map[string]string{}
	for _, v := range before {
		edge := ""
		if len(v.Via) > 0 {
			edge = v.Via[len(v.Via)-1]
		}
		edges[v.Kind+":"+v.Name] = edge
	}
	out := []ProxyChainChange{}
	for _, v := range after {
		edge := ""
		if len(v.Via) > 0 {
			edge = v.Via[len(v.Via)-1]
		}
		old := edges[v.Kind+":"+v.Name]
		if old != edge {
			out = append(out, ProxyChainChange{v.Kind, v.Name, old, edge})
		}
	}
	return out
}
func (s *Store) subscriptionChainCandidate(name string, targets, via []string, clear bool) (map[string]any, ProxyChainPlan, error) {
	p := ProxyChainPlan{Kind: "subscription", Targets: append([]string{}, targets...), Via: append([]string{}, via...), Changes: []ProxyChainChange{}, SharedUse: []ProxyChainReference{}, Conflicts: []string{}, Persistent: !clear}
	if len(targets) != 1 || !validName(targets[0]) {
		return nil, p, i18n.Errorf("backend.profile.chain_subscription_chain_requires_one_subscription_name", "subscription chain requires one subscription name")
	}
	old, err := s.SubscriptionChain(name)
	if err != nil {
		return nil, p, err
	}
	if clear && (old == nil || old.Subscription != targets[0]) {
		return nil, p, i18n.Errorf("backend.profile.chain_subscription_has_no_persistent_chain", "subscription has no persistent chain")
	}
	if clear && len(via) > 0 {
		return nil, p, i18n.Errorf("backend.profile.chain_clear_requires_no_via_nodes", "clear requires no via nodes")
	}
	cfg, err := s.unmanagedChainConfig(name)
	if err != nil {
		return nil, p, err
	}
	if old != nil {
		for _, node := range old.Nodes {
			retained := map[string]any{}
			for k, v := range node {
				retained[k] = v
			}
			if !clear {
				retained["dialer-proxy"] = ""
			}
			n := tsString(retained, "name")
			replaced := false
			for i, v := range tsProxies(cfg) {
				m, _ := v.(map[string]any)
				if tsString(m, "name") == n {
					tsProxies(cfg)[i] = retained
					replaced = true
				}
			}
			if !replaced {
				cfg["proxies"] = append(tsProxies(cfg), retained)
			}
		}
	}
	if clear {
		return cfg, p, chainValidateGraph(cfg)
	}
	plan, err := prepareChain(cfg, "subscription", targets, via, false)
	return cfg, plan, err
}

func (s *Store) validateManagedCandidate(name string, source []byte) error {
	p, err := s.SubscriptionChain(name)
	if err != nil || p == nil {
		return err
	}
	effective, err := s.EffectiveSource(name, source)
	if err != nil {
		return err
	}
	return s.validate(name, effective)
}
