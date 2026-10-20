package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

// ProxyNode deliberately contains no raw proxy configuration or credentials.
type ProxyNode struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Tailscale *TailscaleNode  `json:"tailscale,omitempty"`
	Managed   bool            `json:"managed"`
	Groups    ProxyNodeGroups `json:"groups"`
}

func (s *Store) proxyNode(profileName string, cfg, node map[string]any) (ProxyNode, error) {
	result := ProxyNode{Name: tsString(node, "name"), Type: tsString(node, "type")}
	if result.Type == "tailscale" {
		n := tsNode(node)
		result.Tailscale = &n
	}
	var err error
	result.Groups, err = nodeGroups(cfg, node)
	if err != nil {
		return ProxyNode{}, err
	}
	result.Managed, err = s.ManagedProxyNode(profileName, result.Name)
	return result, err
}
func (s *Store) ProxyNodes(profileName string) ([]ProxyNode, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return nil, err
	}
	result := []ProxyNode{}
	for _, v := range tsProxies(cfg) {
		node, _ := v.(map[string]any)
		n, err := s.proxyNode(profileName, cfg, node)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
func (s *Store) ProxyNode(profileName, name string) (ProxyNode, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return ProxyNode{}, err
	}
	node := tsFind(cfg, name)
	if node == nil {
		return ProxyNode{}, fmt.Errorf("proxy node not found")
	}
	return s.proxyNode(profileName, cfg, node)
}
func (s *Store) ProxyNodeGroupCandidates(profileName string) ([]string, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return nil, err
	}
	result := []string{}
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		g, _ := v.(map[string]any)
		result = append(result, tsString(g, "name"))
	}
	sort.Strings(result)
	return result, nil
}
func (s *Store) saveProxyNodeConfig(ctx context.Context, name string, cfg map[string]any, nodes, groups bool) error {
	overlay := map[string]any{}
	data, err := s.Override(name)
	if err == nil {
		if yaml.Unmarshal(data, &overlay) != nil || overlay == nil {
			return fmt.Errorf("local override must be a YAML mapping")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if nodes {
		delete(overlay, proxyAdditionsKey)
		overlay["proxies"] = tsProxies(cfg)
	}
	if groups {
		overlay["proxy-groups"] = cfg["proxy-groups"]
	}
	updated, err := yaml.Marshal(overlay)
	if err != nil {
		return fmt.Errorf("could not prepare proxy node override")
	}
	return s.SetOverride(ctx, name, updated)
}
func updateTailscaleNode(node map[string]any, o TailscaleOptions) error {
	if err := ValidateTailscaleOptions(o); err != nil {
		return err
	}
	if o.ExitNode != nil {
		node["exit-node"] = *o.ExitNode
	}
	if o.Hostname != nil {
		node["hostname"] = *o.Hostname
	}
	if o.ControlURL != nil {
		node["control-url"] = nagiruntime.NormalizeTailscaleControlURL(*o.ControlURL)
	}
	if o.AuthKey != nil {
		if err := ValidateTailscaleAuthKey(*o.AuthKey); err != nil {
			return err
		}
		node["auth-key"] = strings.TrimSpace(*o.AuthKey)
	}
	if o.AuthKeyFile != nil {
		key, err := readTailscaleAuthKey(*o.AuthKeyFile)
		if err != nil {
			return err
		}
		node["auth-key"] = key
	}
	return nil
}
func (s *Store) AddProxyNode(ctx context.Context, profileName, name string, o TailscaleOptions, groups []string) error {
	if err := ValidateTailscaleName(name); err != nil {
		return err
	}
	if o.ExitNode == nil || (o.AuthKey == nil && o.AuthKeyFile == nil) {
		return fmt.Errorf("add requires EXIT_NODE and an auth key")
	}
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	if tsFind(cfg, name) != nil {
		return fmt.Errorf("proxy name already exists")
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	if _, ok := providers[name]; ok {
		return fmt.Errorf("name conflicts with an existing provider")
	}
	candidates, err := s.ProxyNodeGroupCandidates(profileName)
	if err != nil {
		return err
	}
	for _, group := range candidates {
		if group == name {
			return fmt.Errorf("name conflicts with an existing group")
		}
	}
	node := map[string]any{"name": name, "type": "tailscale", "udp": true, "control-url": nagiruntime.TailscaleControlURL}
	if err = updateTailscaleNode(node, o); err != nil {
		return err
	}
	cfg["proxies"] = append(tsProxies(cfg), node)
	if err = setNodeGroups(cfg, name, groups); err != nil {
		return err
	}
	return s.saveProxyNodeConfig(ctx, profileName, cfg, true, len(groups) > 0)
}
func (s *Store) EditProxyNode(ctx context.Context, profileName, name string, o TailscaleOptions) error {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	node := tsFind(cfg, name)
	if node == nil {
		return fmt.Errorf("proxy node not found")
	}
	if tsString(node, "type") != "tailscale" {
		return fmt.Errorf("editing this proxy type is not supported")
	}
	if err = updateTailscaleNode(node, o); err != nil {
		return err
	}
	managed, err := s.ManagedProxyNode(profileName, name)
	if err != nil {
		return err
	}
	if managed {
		return s.EditManagedProxyNode(ctx, profileName, name, node)
	}
	return s.saveProxyNodeConfig(ctx, profileName, cfg, true, false)
}
func (s *Store) RemoveProxyNode(ctx context.Context, profileName, name string) error {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	node := tsFind(cfg, name)
	if node == nil {
		return fmt.Errorf("proxy node not found")
	}
	if tsString(node, "type") != "tailscale" {
		return fmt.Errorf("removing this proxy type is not supported")
	}
	if err = s.CheckProxyNodeRemoval(profileName, name); err != nil {
		return err
	}
	if nodeReferenced(cfg, name) {
		return fmt.Errorf("proxy node is still referenced; remove its group, chain, rule, DNS or provider references first")
	}
	list := []any{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if tsString(m, "name") != name {
			list = append(list, v)
		}
	}
	cfg["proxies"] = list
	// Identity state is intentionally retained across configuration removal.
	return s.saveProxyNodeConfig(ctx, profileName, cfg, true, false)
}
func nodeReferenced(cfg map[string]any, name string) bool {
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		g, _ := v.(map[string]any)
		members, _ := g["proxies"].([]any)
		for _, m := range members {
			if m == name {
				return true
			}
		}
		if tsString(g, "empty-fallback") == name {
			return true
		}
	}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if tsString(m, "dialer-proxy") == name {
			return true
		}
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	for _, v := range providers {
		p, _ := v.(map[string]any)
		dialer, _ := tsProviderDialer(p)
		if dialer == name || tsString(p, "dialer-proxy") == name || tsString(p, "proxy") == name {
			return true
		}
		payload, _ := p["payload"].([]any)
		for _, v := range payload {
			m, _ := v.(map[string]any)
			if tsString(m, "dialer-proxy") == name {
				return true
			}
		}
	}
	ruleProviders, _ := cfg["rule-providers"].(map[string]any)
	for _, v := range ruleProviders {
		p, _ := v.(map[string]any)
		if tsString(p, "proxy") == name {
			return true
		}
	}
	if tsDNSReferences(cfg["dns"], name) {
		return true
	}
	var rulesReference func(any) bool
	rulesReference = func(value any) bool {
		switch v := value.(type) {
		case []any:
			for _, child := range v {
				if rulesReference(child) {
					return true
				}
			}
		case map[string]any:
			for _, child := range v {
				if rulesReference(child) {
					return true
				}
			}
		case string:
			parts := strings.Split(v, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				part := strings.TrimSpace(parts[i])
				if part == "no-resolve" {
					continue
				}
				return part == name
			}
		}
		return false
	}
	return rulesReference(cfg["rules"]) || rulesReference(cfg["sub-rules"])
}
