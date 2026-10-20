package profile

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/subscription"
	"io/fs"
	"net/url"
	"sort"
	"strings"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

// ProxyNode deliberately contains no raw proxy configuration or credentials.
type ProxyNode struct {
	Subscription string          `json:"subscription,omitempty"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Tailscale    *TailscaleNode  `json:"tailscale,omitempty"`
	Managed      bool            `json:"managed"`
	Groups       ProxyNodeGroups `json:"groups"`
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
	source, err := s.Show(profileName)
	if err != nil {
		return nil, err
	}
	entries, err := subscription.NewStore(s.Dir, "", nil).List()
	if err != nil {
		return nil, err
	}
	subscriptionName := ""
	for _, entry := range entries {
		if entry.Profile == profileName && entry.SourceHash == subscription.Hash(source) {
			subscriptionName = entry.Name
			break
		}
	}
	var original struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(source, &original); err != nil {
		return nil, err
	}
	ranks := map[string]int{}
	if subscriptionName != "" {
		for i, node := range original.Proxies {
			ranks[node.Name] = i
		}
	}
	result := []ProxyNode{}
	for _, v := range tsProxies(cfg) {
		node, _ := v.(map[string]any)
		n, err := s.proxyNode(profileName, cfg, node)
		if err != nil {
			return nil, err
		}
		if _, ok := ranks[n.Name]; ok {
			n.Subscription = subscriptionName
		}
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool {
		left, leftSource := ranks[result[i].Name]
		right, rightSource := ranks[result[j].Name]
		if leftSource != rightSource {
			return leftSource
		}
		if leftSource {
			return left < right
		}
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// ProxyNodeProviderCandidates preserves declaration order from the source and
// then local overrides, omitting providers absent from effective configuration.
func (s *Store) ProxyNodeProviderCandidates(profileName string) ([]string, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return nil, err
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	source, err := s.Show(profileName)
	if err != nil {
		return nil, err
	}
	patch, err := s.Override(profileName)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	result := []string{}
	seen := map[string]bool{}
	for _, data := range [][]byte{source, patch} {
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		if len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value != "proxy-providers" {
				continue
			}
			mapping := root.Content[i+1]
			if mapping.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(mapping.Content); j += 2 {
				name := mapping.Content[j].Value
				if _, ok := providers[name]; ok && !seen[name] {
					result = append(result, name)
					seen[name] = true
				}
			}
		}
	}
	remaining := []string{}
	for name := range providers {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	return append(result, remaining...), nil
}
func (s *Store) ProxyNode(profileName, name string) (ProxyNode, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return ProxyNode{}, err
	}
	node := tsFind(cfg, name)
	if node == nil {
		return ProxyNode{}, i18n.Errorf("backend.profile.node_proxy_node_not_found", "proxy node not found")
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
			return i18n.Errorf("backend.profile.node_local_override_must_be_a_yaml_mapping", "local override must be a YAML mapping")
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
		return i18n.Errorf("backend.profile.node_could_not_prepare_proxy_node_override", "could not prepare proxy node override")
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
		return i18n.Errorf("backend.profile.node_add_requires_exit_node_and_an_auth_key", "add requires EXIT_NODE and an auth key")
	}
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	if tsFind(cfg, name) != nil {
		return i18n.Errorf("backend.profile.node_proxy_name_already_exists", "proxy name already exists")
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	if _, ok := providers[name]; ok {
		return i18n.Errorf("backend.profile.node_name_conflicts_with_an_existing_provider", "name conflicts with an existing provider")
	}
	candidates, err := s.ProxyNodeGroupCandidates(profileName)
	if err != nil {
		return err
	}
	for _, group := range candidates {
		if group == name {
			return i18n.Errorf("backend.profile.node_name_conflicts_with_an_existing_group", "name conflicts with an existing group")
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
		return i18n.Errorf("backend.profile.node_proxy_node_not_found", "proxy node not found")
	}
	if tsString(node, "type") != "tailscale" {
		return i18n.Errorf("backend.profile.node_editing_this_proxy_type_is_not_supported", "editing this proxy type is not supported")
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
		return i18n.Errorf("backend.profile.node_proxy_node_not_found", "proxy node not found")
	}
	if tsString(node, "type") != "tailscale" {
		return i18n.Errorf("backend.profile.node_removing_this_proxy_type_is_not_supported", "removing this proxy type is not supported")
	}
	if err = s.CheckProxyNodeRemoval(profileName, name); err != nil {
		return err
	}
	if nodeReferenced(cfg, name) {
		return i18n.Errorf("backend.profile.node_proxy_node_is_still_referenced_remove_its_group_chain_rule_dns_or_provider_references_first", "proxy node is still referenced; remove its group, chain, rule, DNS or provider references first")
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
		if tsString(g, "empty-fallback") == name || tsString(g, "dialer-proxy") == name {
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
	if nodeDNSReferences(cfg["dns"], name) {
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
			return nodeRuleTarget(v) == name
		}
		return false
	}

	ntp, _ := cfg["ntp"].(map[string]any)
	if tsString(ntp, "dialer-proxy") == name {
		return true
	}
	for _, key := range []string{"listeners", "tunnels"} {
		items, _ := cfg[key].([]any)
		for _, v := range items {
			item, _ := v.(map[string]any)
			if tsString(item, "proxy") == name {
				return true
			}
		}
	}
	return rulesReference(cfg["rules"]) || rulesReference(cfg["sub-rules"])
}

// Extract only the reference position defined by mihomo ParseRulePayload;
// this does not evaluate the rule or duplicate engine routing behavior.
func nodeRuleTarget(rule string) string {
	parts := strings.Split(rule, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) < 2 {
		return ""
	}
	switch strings.ToUpper(parts[0]) {
	case "SUB-RULE":
		return "" // Its target names a sub-rule, not an outbound.
	case "MATCH":
		return parts[1]
	case "NOT", "OR", "AND", "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
		return parts[len(parts)-1]
	default:
		if len(parts) > 2 {
			return parts[2]
		}
	}
	return ""
}
func nodeDNSReferences(value any, name string) bool {
	if tsDNSReferences(value, name) {
		return true
	}
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if nodeDNSReferences(child, name) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if nodeDNSReferences(child, name) {
				return true
			}
		}
	case string:
		u, err := url.Parse(v)
		if err != nil {
			return false
		}
		for _, part := range strings.Split(u.Fragment, "&") {
			if part == name {
				return true
			}
		}
	}
	return false
}
