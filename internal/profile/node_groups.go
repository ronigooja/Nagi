package profile

import (
	"context"
	"github.com/ronigooja/Nagi/internal/i18n"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// ProxyNodeGroups reports configured membership separately from automatic inclusion.
type ProxyNodeGroups struct {
	Name      string   `json:"name"`
	Explicit  []string `json:"explicit"`
	Automatic []string `json:"automatic"`
	Excluded  []string `json:"excluded"`
	Available []string `json:"available"`
}

func nodeGroups(cfg map[string]any, node map[string]any) (ProxyNodeGroups, error) {
	name := tsString(node, "name")
	result := ProxyNodeGroups{Name: name, Explicit: []string{}, Automatic: []string{}, Excluded: []string{}, Available: []string{}}
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		g, _ := v.(map[string]any)
		group := tsString(g, "name")
		result.Available = append(result.Available, group)
		explicit := false
		members, _ := g["proxies"].([]any)
		for _, m := range members {
			if m == name {
				explicit = true
				break
			}
		}
		if explicit {
			result.Explicit = append(result.Explicit, group)
		}
		all, _ := g["include-all"].(bool)
		proxies, _ := g["include-all-proxies"].(bool)
		if !all && !proxies && !explicit {
			continue
		}
		excluded, err := nodeGroupExcluded(g, node)
		if err != nil {
			return result, err
		}
		if all || proxies {
			match, err := nodeGroupMatch(tsString(g, "filter"), name, true)
			if err != nil {
				return result, err
			}
			if match && !excluded {
				result.Automatic = append(result.Automatic, group)
			} else {
				result.Excluded = append(result.Excluded, group)
			}
		} else if excluded {
			result.Excluded = append(result.Excluded, group)
		}
	}
	return result, nil
}

func nodeGroupMatch(pattern, name string, empty bool) (bool, error) {
	if pattern == "" {
		return empty, nil
	}
	for _, part := range strings.Split(pattern, "`") {
		re, err := regexp2.Compile(part, regexp2.None)
		if err != nil {
			return false, i18n.Errorf("backend.profile.node_proxy_group_contains_an_invalid_filter", "proxy group contains an invalid filter")
		}
		re.MatchTimeout = 100 * time.Millisecond
		match, err := re.MatchString(name)
		if err != nil {
			return false, i18n.Errorf("backend.profile.node_proxy_group_filter_could_not_be_evaluated", "proxy group filter could not be evaluated")
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}
func nodeGroupExcluded(group, node map[string]any) (bool, error) {
	for _, kind := range strings.Split(tsString(group, "exclude-type"), "|") {
		if kind != "" && strings.EqualFold(kind, nodeAdapterType(tsString(node, "type"))) {
			return true, nil
		}
	}
	return nodeGroupMatch(tsString(group, "exclude-filter"), tsString(node, "name"), false)
}
func (s *Store) ProxyNodeGroups(profileName, name string) (ProxyNodeGroups, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return ProxyNodeGroups{}, err
	}
	node := tsFind(cfg, name)
	if node == nil {
		return ProxyNodeGroups{}, i18n.Errorf("backend.profile.node_proxy_node_not_found", "proxy node not found")
	}
	return nodeGroups(cfg, node)
}
func setNodeGroups(cfg map[string]any, name string, wanted []string) error {
	selected := map[string]bool{}
	groups, _ := cfg["proxy-groups"].([]any)
	available := map[string]bool{}
	for _, v := range groups {
		g, _ := v.(map[string]any)
		available[tsString(g, "name")] = true
	}
	for _, group := range wanted {
		if !available[group] {
			return i18n.Errorf("backend.profile.node_proxy_group_not_found", "proxy group not found")
		}
		if selected[group] {
			return i18n.Errorf("backend.profile.node_duplicate_proxy_group", "duplicate proxy group")
		}
		selected[group] = true
	}
	for _, v := range groups {
		g, _ := v.(map[string]any)
		members, _ := g["proxies"].([]any)
		next := make([]any, 0, len(members)+1)
		present := false
		for _, m := range members {
			if m == name {
				if selected[tsString(g, "name")] && !present {
					next = append(next, m)
					present = true
				}
			} else {
				next = append(next, m)
			}
		}
		if selected[tsString(g, "name")] && !present {
			next = append(next, name)
		}
		// Preserve omitted proxies fields unless membership actually changes.
		if _, exists := g["proxies"]; exists || selected[tsString(g, "name")] {
			g["proxies"] = next
		}
	}
	if tsCycle(cfg) {
		return i18n.Errorf("backend.profile.node_proxy_group_membership_would_create_a_dependency_cycle", "proxy group membership would create a dependency cycle")
	}
	return nil
}
func (s *Store) SetProxyNodeGroups(ctx context.Context, profileName, name string, groups []string) error {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	if tsFind(cfg, name) == nil {
		return i18n.Errorf("backend.profile.node_proxy_node_not_found", "proxy node not found")
	}
	if err = setNodeGroups(cfg, name, groups); err != nil {
		return err
	}
	return s.saveProxyNodeConfig(ctx, profileName, cfg, false, true)
}
func (s *Store) ClearProxyNodeGroups(ctx context.Context, profileName, name string) error {
	return s.SetProxyNodeGroups(ctx, profileName, name, nil)
}

// mihomo filters against AdapterType.String(), which differs from YAML for these types.
func nodeAdapterType(kind string) string {
	switch kind {
	case "ss":
		return "Shadowsocks"
	case "ssr":
		return "ShadowsocksR"
	case "gost-relay":
		return "GostRelay"
	case "reject-drop":
		return "RejectDrop"
	case "pass-rule":
		return "PassRule"
	}
	return kind
}
