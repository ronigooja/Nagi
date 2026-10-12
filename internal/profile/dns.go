package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DNSReplaceKey    = "nagi-dns-replace"
	DNSProxyGroupKey = "nagi-dns-proxy-group"
)

var defaultDNSUpstreams = []string{"https://223.5.5.5/dns-query", "https://223.6.6.6/dns-query"}
var proxyDNSUpstreams = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

// DNSSettings generates Nagi's complete DNS policy. An empty group binds
// primary queries to DIRECT; bootstrap and proxy hostname queries stay direct.
func DNSSettings(servers, bootstrap []string, group string) map[string]any {
	primary := make([]string, len(servers))
	for i, server := range servers {
		binding := group
		if binding == "" {
			binding = "DIRECT"
		}
		primary[i] = server + "#" + binding
	}
	bootstrapBound := make([]string, len(bootstrap))
	for i, server := range bootstrap {
		bootstrapBound[i] = server + "#DIRECT"
	}
	direct := append([]string(nil), bootstrapBound...)
	if group == "" {
		direct = append([]string(nil), primary...)
	}
	return map[string]any{
		"enable": true, "resolve-proxy-destinations": true, "listen": "127.0.0.1:1053",
		"ipv6": true, "enhanced-mode": "fake-ip",
		"default-nameserver": append([]string(nil), bootstrapBound...),
		"nameserver":         primary, "direct-nameserver": direct,
		"direct-nameserver-follow-policy": false,
		"proxy-server-nameserver":         append([]string(nil), bootstrapBound...),
		"fallback":                        []string{}, "nameserver-policy": map[string]any{},
		"proxy-server-nameserver-policy": map[string]any{}, "respect-rules": false,
	}
}

func DefaultDNS(group string) map[string]any {
	if group != "" {
		return DNSSettings(proxyDNSUpstreams, defaultDNSUpstreams, group)
	}
	return DNSSettings(defaultDNSUpstreams, defaultDNSUpstreams, group)
}

func dnsGroups(data []byte) ([]map[string]any, map[string]bool, map[string]bool, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil || cfg == nil {
		return nil, nil, nil, errors.New("profile must be a YAML mapping")
	}
	groups, _ := cfg["proxy-groups"].([]any)
	proxies, _ := cfg["proxies"].([]any)
	proxyNames := map[string]bool{}
	providerNames := map[string]bool{}
	if providers, ok := cfg["proxy-providers"].(map[string]any); ok {
		for name := range providers {
			providerNames[name] = true
		}
	}
	for _, entry := range proxies {
		proxy, _ := entry.(map[string]any)
		name, _ := proxy["name"].(string)
		if name != "" && proxy["type"] != "direct" && proxy["type"] != "reject" && proxy["type"] != "reject-drop" && proxy["type"] != "pass" {
			proxyNames[name] = true
		}
	}
	result := make([]map[string]any, 0, len(groups))
	for _, entry := range groups {
		group, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, group)
	}
	return result, proxyNames, providerNames, nil
}

func dnsStrings(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func usableDNSGroup(group map[string]any, byName map[string]map[string]any, nodes, providers map[string]bool, seen map[string]bool) bool {
	name, _ := group["name"].(string)
	if seen[name] {
		return false
	}
	seen[name] = true
	defer delete(seen, name)
	for _, name := range dnsStrings(group["use"]) {
		if providers[name] {
			return true
		}
	}
	if len(nodes) > 0 && (group["include-all"] == true || group["include-all-proxies"] == true) {
		return true
	}
	if len(providers) > 0 && (group["include-all"] == true || group["include-all-providers"] == true) {
		return true
	}
	for _, member := range dnsStrings(group["proxies"]) {
		if nodes[member] {
			return true
		}
		if child := byName[member]; child != nil && usableDNSGroup(child, byName, nodes, providers, seen) {
			return true
		}
	}
	return false
}

// ValidateDNSGroup rejects a missing group, a group without proxy members,
// and a startup selection that would send DNS through DIRECT.
func ValidateDNSGroup(data []byte, name string) error {
	return validateDNSGroup(data, name, nil)
}

func validateDNSGroup(data []byte, name string, saved map[string]string) error {
	if name == "" || name == "DIRECT" || name == "RULES" || strings.ContainsAny(name, "&#=") {
		return errors.New("DNS proxy group must be a configured proxy group")
	}
	groups, nodes, providers, err := dnsGroups(data)
	if err != nil {
		return err
	}
	byName := map[string]map[string]any{}
	for _, group := range groups {
		groupName, _ := group["name"].(string)
		byName[groupName] = group
	}
	group := byName[name]
	if group == nil {
		return fmt.Errorf("DNS proxy group %q does not exist", name)
	}
	if !usableDNSGroup(group, byName, nodes, providers, map[string]bool{}) {
		return fmt.Errorf("DNS proxy group %q has no proxy members", name)
	}
	visited := map[string]bool{}
	for group != nil {
		current, _ := group["name"].(string)
		if visited[current] {
			return fmt.Errorf("DNS proxy group %q has a selection cycle", name)
		}
		visited[current] = true
		if group["type"] != "select" {
			break
		}
		selected := saved[current]
		if selected == "" {
			selected, _ = group["default-selected"].(string)
		}
		if selected == "" {
			members := dnsStrings(group["proxies"])
			if len(members) > 0 {
				selected = members[0]
			}
		}
		if selected == "DIRECT" || selected == "REJECT" || selected == "REJECT-DROP" || selected == "PASS" || selected == "RULES" {
			return fmt.Errorf("DNS proxy group %q selects %s", current, selected)
		}
		providerChoice := false
		for _, provider := range dnsStrings(group["use"]) {
			if providers[provider] {
				providerChoice = true
			}
		}
		if selected != "" && !nodes[selected] && byName[selected] == nil && !providerChoice {
			return fmt.Errorf("DNS proxy group %q selects a non-proxy member", current)
		}
		group = byName[selected]
	}
	return nil
}

// DNSProxyGroups returns viable group names in profile order.
func (s *Store) DNSProxyGroups(name string) ([]string, error) {
	data, err := s.Effective(name)
	if err != nil {
		return nil, err
	}
	groups, _, _, err := dnsGroups(data)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, group := range groups {
		groupName, _ := group["name"].(string)
		if groupName != "" && dnsGroupWithSavedSelection(data, s.Dir, name, groupName) == nil {
			result = append(result, groupName)
		}
	}
	return result, nil
}

// DefaultDNSProxyRequired reports whether a profile has proxy nodes or
// providers and therefore needs an explicit DNS proxy group.
func (s *Store) DefaultDNSProxyRequired(name string) (bool, error) {
	data, err := s.Effective(name)
	if err != nil {
		return false, err
	}
	_, nodes, providers, err := dnsGroups(data)
	if err != nil {
		return false, err
	}
	return len(nodes) > 0 || len(providers) > 0, nil
}

// DefaultDNSProxyGroup returns the group bound by apply-default, if any.
func (s *Store) DefaultDNSProxyGroup(name string) (string, error) {
	data, err := s.Override(name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var override map[string]any
	if err := yaml.Unmarshal(data, &override); err != nil {
		return "", err
	}
	group, _ := override[DNSProxyGroupKey].(string)
	return group, nil
}

func dnsGroupWithSavedSelection(data []byte, dir, profileName, groupName string) error {
	selection, err := os.ReadFile(filepath.Join(dir, "proxy-selections.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(selection) == 0 {
		return ValidateDNSGroup(data, groupName)
	}
	var saved map[string]map[string]string
	if err := json.Unmarshal(selection, &saved); err != nil {
		return errors.New("saved proxy selections are invalid")
	}
	return validateDNSGroup(data, groupName, saved[profileName])
}
