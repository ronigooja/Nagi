package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"gopkg.in/yaml.v3"
)

// SubscriptionTailscalePolicy is a managed layer, separate from replaceable local overrides.
// Node contains sensitive configuration and must never be included in CLI results.
type SubscriptionTailscalePolicy struct {
	Subscription string         `yaml:"subscription"`
	Node         map[string]any `yaml:"node"`
	SourceNode   map[string]any `yaml:"source-node,omitempty"`
}

const proxyAdditionsKey = "nagi-proxy-additions"

func (s *Store) subscriptionTailscalePath(name string) (string, error) {
	p, err := s.overridePath(name)
	if err != nil {
		return "", err
	}
	return p + ".tailscale-subscription.yaml", nil
}
func (s *Store) SubscriptionTailscale(name string) (*SubscriptionTailscalePolicy, error) {
	p, err := s.subscriptionTailscalePath(name)
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
		return nil, errors.New("subscription Tailscale policy must be a regular file")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var policy SubscriptionTailscalePolicy
	if yaml.Unmarshal(data, &policy) != nil || !validName(policy.Subscription) || tsString(policy.Node, "type") != "tailscale" || ValidateTailscaleName(tsString(policy.Node, "name")) != nil {
		return nil, errors.New("subscription Tailscale policy is invalid")
	}
	return &policy, nil
}
func (s *Store) writeSubscriptionTailscale(name string, policy *SubscriptionTailscalePolicy) error {
	p, err := s.subscriptionTailscalePath(name)
	if err != nil {
		return err
	}
	if policy == nil {
		err = os.Remove(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := yaml.Marshal(policy)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	return writeReplace(p, data)
}

// ClearSubscriptionTailscale detaches a managed policy without activating. Callers
// own the configuration transaction and subsequently activate the replacement source.
func (s *Store) ClearSubscriptionTailscale(name string) error {
	return s.writeSubscriptionTailscale(name, nil)
}

func (s *Store) applySubscriptionTailscale(name string, data []byte) ([]byte, error) {
	policy, err := s.SubscriptionTailscale(name)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return data, nil
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil || cfg == nil {
		return nil, errors.New("subscription profile must be a YAML mapping")
	}
	nodeName := tsString(policy.Node, "name")
	if existing := tsFind(cfg, nodeName); existing != nil {
		if tsString(existing, "type") != "tailscale" {
			return nil, errors.New("subscription proxy conflicts with the managed Tailscale node")
		}
		// Only an identical retained node may coexist with the managed definition.
		if !sameTailscaleDefinition(existing, policy.Node) && (policy.SourceNode == nil || !sameTailscaleDefinition(existing, policy.SourceNode)) {
			return nil, errors.New("subscription Tailscale node conflicts with the managed definition")
		}
	} else {
		cfg["proxies"] = append(tsProxies(cfg), policy.Node)
	}
	for i, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if tsString(m, "name") == nodeName {
			tsProxies(cfg)[i] = policy.Node
		}
	}
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		m, _ := v.(map[string]any)
		if tsString(m, "name") == nodeName {
			return nil, errors.New("subscription group conflicts with the managed Tailscale node")
		}
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	if _, ok := providers[nodeName]; ok {
		return nil, errors.New("subscription provider conflicts with the managed Tailscale node")
	}
	for _, target := range tsTargets(cfg) {
		if err := tsChangeBinding(cfg, "bind", nodeName, target.Kind, target.Name); err != nil {
			return nil, fmt.Errorf("subscription Tailscale target %q: %w", target.Name, err)
		}
	}
	if tsCycle(cfg) {
		return nil, errors.New("subscription binding would create a proxy dependency cycle")
	}
	return yaml.Marshal(cfg)
}

// normalizeSubscriptionOverride releases a full node snapshot only when its
// subscription nodes match the source. Arbitrary replacement lists require an
// explicit user correction rather than silently discarding local configuration.
func (s *Store) normalizeSubscriptionOverride(name string, overlay map[string]any, nodeName string) error {
	raw, exists := overlay["proxies"]
	if !exists {
		return nil
	}
	source, err := s.Show(name)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if yaml.Unmarshal(source, &cfg) != nil {
		return errors.New("subscription source is invalid")
	}
	sourceNodes := map[string]map[string]any{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		sourceNodes[tsString(m, "name")] = m
	}
	list, ok := raw.([]any)
	if !ok {
		return errors.New("local proxy snapshot must be a list")
	}
	extras := []any{}
	seen := map[string]bool{}
	for _, v := range list {
		m, _ := v.(map[string]any)
		n := tsString(m, "name")
		if n == nodeName {
			continue
		}
		original := sourceNodes[n]
		if original == nil {
			if tsString(m, "type") != "tailscale" {
				return errors.New("local proxy snapshot contains added ordinary nodes; clear or revise it before binding a subscription")
			}
			extras = append(extras, m)
			continue
		}
		copyNode := map[string]any{}
		for k, value := range m {
			copyNode[k] = value
		}
		if dialer := tsString(copyNode, "dialer-proxy"); dialer != "" {
			existing := tsFind(map[string]any{"proxies": list}, dialer)
			if existing != nil && tsString(existing, "type") == "tailscale" {
				if value, ok := original["dialer-proxy"]; ok {
					copyNode["dialer-proxy"] = value
				} else {
					delete(copyNode, "dialer-proxy")
				}
			}
		}
		same := reflect.DeepEqual(copyNode, original)
		if tsString(original, "type") == "tailscale" {
			same = sameTailscaleDefinition(copyNode, original)
		}
		if !same {
			return errors.New("local proxy snapshot changes subscription nodes; clear or revise it before binding a subscription")
		}
		seen[n] = true
	}
	for n := range sourceNodes {
		if n != nodeName && !seen[n] {
			return errors.New("local proxy snapshot omits subscription nodes; clear or revise it before binding a subscription")
		}
	}
	delete(overlay, "proxies")
	if len(extras) > 0 {
		overlay[proxyAdditionsKey] = extras
	}
	return nil
}

// MutateSubscriptionTailscale prepares a durable whole-source policy. Callers
// hold config.lock and snapshot the entire configuration for runtime recovery.
func (s *Store) MutateSubscriptionTailscale(ctx context.Context, profileName, subscriptionName, action, nodeName string) error {
	if !validName(subscriptionName) {
		return errors.New("invalid subscription name")
	}
	old, err := s.SubscriptionTailscale(profileName)
	if err != nil {
		return err
	}
	if action != "bind" && action != "unbind" {
		return errors.New("subscription binding action must be bind or unbind")
	}
	var cfg map[string]any
	if action == "bind" {
		cfg, err = s.tailscaleConfig(profileName)
		if err != nil {
			return err
		}
	}
	overlay := map[string]any{}
	patch, patchErr := s.Override(profileName)
	if patchErr == nil {
		if yaml.Unmarshal(patch, &overlay) != nil {
			return errors.New("local override is invalid")
		}
	} else if !errors.Is(patchErr, fs.ErrNotExist) {
		return patchErr
	}
	var policy *SubscriptionTailscalePolicy
	if action == "bind" {
		node := tsFind(cfg, nodeName)
		if node == nil || tsString(node, "type") != "tailscale" {
			return errors.New("Tailscale node not found; run `nagi proxy tailscale list`")
		}
		policy = &SubscriptionTailscalePolicy{Subscription: subscriptionName, Node: node}
		source, err := s.Show(profileName)
		if err != nil {
			return err
		}
		var sourceCfg map[string]any
		if yaml.Unmarshal(source, &sourceCfg) != nil {
			return errors.New("subscription source is invalid")
		}
		policy.SourceNode = tsFind(sourceCfg, nodeName)
		if values, ok := overlay[proxyAdditionsKey].([]any); ok {
			kept := []any{}
			for _, v := range values {
				m, _ := v.(map[string]any)
				if tsString(m, "name") != nodeName {
					kept = append(kept, v)
				}
			}
			if len(kept) == 0 {
				delete(overlay, proxyAdditionsKey)
			} else {
				overlay[proxyAdditionsKey] = kept
			}
		}
		if err = s.normalizeSubscriptionOverride(profileName, overlay, nodeName); err != nil {
			return err
		}
		// A previous managed exit remains available as a local addition after switching.
		if old != nil && tsString(old.Node, "name") != nodeName {
			list, _ := overlay[proxyAdditionsKey].([]any)
			overlay[proxyAdditionsKey] = append(list, old.Node)
		}
	} else {
		if old == nil || old.Subscription != subscriptionName {
			return errors.New("subscription has no persistent Tailscale binding")
		}
		list, _ := overlay[proxyAdditionsKey].([]any)
		overlay[proxyAdditionsKey] = append(list, old.Node)
	}
	if err = s.writeSubscriptionTailscale(profileName, policy); err != nil {
		return err
	}
	encoded, err := yaml.Marshal(overlay)
	if err == nil {
		err = s.SetOverride(ctx, profileName, encoded)
	}
	if err != nil {
		return errors.Join(err, s.writeSubscriptionTailscale(profileName, old))
	}
	return nil
}

func sameTailscaleDefinition(a, b map[string]any) bool {
	left, right := map[string]any{}, map[string]any{}
	for k, v := range a {
		if k != "state-dir" {
			left[k] = v
		}
	}
	for k, v := range b {
		if k != "state-dir" {
			right[k] = v
		}
	}
	return reflect.DeepEqual(left, right)
}
