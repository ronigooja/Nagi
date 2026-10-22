package profile

import (
	"context"
	"gopkg.in/yaml.v3"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// ProxyGroupConfig excludes provider definitions and protocol credentials. Other
// mihomo fields remain in the effective configuration when applying a patch.
type ProxyGroupConfig struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Proxies   []string `json:"proxies"`
	Use       []string `json:"use"`
	URL       string   `json:"url"`
	Interval  int      `json:"interval"`
	Tolerance int      `json:"tolerance"`
	Strategy  string   `json:"strategy"`
}

func groupError(key, fallback string, args ...any) error {
	return i18n.Errorf("backend.profile.group."+key, fallback, args...)
}

func ValidateProxyGroupPatch(patch map[string]any) error {
	for key, value := range patch {
		switch key {
		case "type":
			switch value {
			case "select", "url-test", "fallback", "load-balance":
			default:
				return groupError("type", "group type must be select, url-test, fallback, or load-balance")
			}
		case "url":
			s, ok := value.(string)
			if !ok {
				return groupError("url", "group URL must be an HTTP(S) URL")
			}
			if err := ValidateHealthcheckURL(s); err != nil {
				return err
			}
		case "interval", "tolerance":
			n, ok := value.(int)
			max, min := 31536000, 1
			if key == "tolerance" {
				max, min = 65535, 0
			}
			if !ok || n < min || n > max {
				return groupError("number", "%s must be an integer from %d to %d", key, min, max)
			}
		case "strategy":
			switch value {
			case "consistent-hashing", "round-robin", "sticky-sessions":
			default:
				return groupError("strategy", "strategy must be consistent-hashing, round-robin, or sticky-sessions")
			}
		case "proxies", "use":
			members, ok := value.([]any)
			if !ok {
				return groupError("members", "group members must be a list of distinct names")
			}
			seen := map[string]bool{}
			for _, member := range members {
				name, ok := member.(string)
				if !ok || name == "" || seen[name] {
					return groupError("members", "group members must be a list of distinct names")
				}
				seen[name] = true
			}
		default:
			return groupError("option", "unsupported group setting: %s", key)
		}
	}
	return nil
}

func (s *Store) ProxyGroupConfigs(profileName string) ([]ProxyGroupConfig, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return nil, err
	}
	result := []ProxyGroupConfig{}
	groups, _, err := namedList(cfg["proxy-groups"])
	if err != nil {
		return nil, err
	}
	for _, value := range groups {
		g, _ := value.(map[string]any)
		item := ProxyGroupConfig{Name: tsString(g, "name"), Type: tsString(g, "type"), URL: tsString(g, "url"), Strategy: tsString(g, "strategy"), Proxies: []string{}, Use: []string{}}
		item.Interval, _ = healthInterval(g["interval"])
		item.Tolerance, _ = healthInterval(g["tolerance"])
		for _, entry := range []struct {
			key string
			dst *[]string
		}{{"proxies", &item.Proxies}, {"use", &item.Use}} {
			values, _ := g[entry.key].([]any)
			for _, v := range values {
				if str, ok := v.(string); ok {
					*entry.dst = append(*entry.dst, str)
				}
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) SaveProxyGroup(ctx context.Context, profileName, name string, patch map[string]any, create bool) error {
	if err := ValidateTailscaleName(name); err != nil {
		return err
	}
	if err := ValidateProxyGroupPatch(patch); err != nil {
		return err
	}
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	groups, _, err := namedList(cfg["proxy-groups"])
	if err != nil {
		return err
	}
	var target map[string]any
	for _, value := range groups {
		g, _ := value.(map[string]any)
		if tsString(g, "name") == name {
			target = g
			break
		}
	}
	if create {
		if target != nil {
			return groupError("exists", "proxy group already exists: %s", name)
		}
		if tsFind(cfg, name) != nil {
			return groupError("conflict", "group name conflicts with a proxy or provider: %s", name)
		}
		providers, _ := cfg["proxy-providers"].(map[string]any)
		if _, ok := providers[name]; ok {
			return groupError("conflict", "group name conflicts with a proxy or provider: %s", name)
		}
		target = map[string]any{"name": name, "type": "select"}
		groups = append(groups, target)
	} else if target == nil {
		return groupError("missing", "proxy group not found: %s; run nagi proxy group list", name)
	}
	oldType := tsString(target, "type")
	for k, v := range patch {
		target[k] = v
	}
	kind := tsString(target, "type")
	// Check existing types too; only the four supported types may be edited.
	if err := ValidateProxyGroupPatch(map[string]any{"type": kind}); err != nil {
		return err
	}
	if _, ok := patch["tolerance"]; ok && kind != "url-test" {
		return groupError("tolerance_type", "tolerance requires a url-test group")
	}
	if _, ok := patch["strategy"]; ok && kind != "load-balance" {
		return groupError("strategy_type", "strategy requires a load-balance group")
	}
	if kind != oldType {
		if kind != "url-test" {
			delete(target, "tolerance")
		}
		if kind != "load-balance" {
			delete(target, "strategy")
		}
	}
	if create && patch["proxies"] == nil && patch["use"] == nil {
		target["proxies"] = []any{"DIRECT"}
	}
	members, _ := target["proxies"].([]any)
	uses, _ := target["use"].([]any)
	auto := healthBool(target["include-all"])
	autoNodes := healthBool(target["include-all-proxies"])
	autoProviders := healthBool(target["include-all-providers"])
	if len(members)+len(uses) == 0 && !auto && !autoNodes && !autoProviders {
		return groupError("empty", "select at least one node or provider")
	}
	available := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}
	for _, v := range tsProxies(cfg) {
		node, _ := v.(map[string]any)
		available[tsString(node, "name")] = true
	}
	for _, v := range groups {
		g, _ := v.(map[string]any)
		available[tsString(g, "name")] = true
	}
	for _, v := range members {
		member, _ := v.(string)
		if member == name || !available[member] {
			return groupError("node_missing", "group member is missing or refers to the group itself: %s", member)
		}
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	for _, v := range uses {
		member, _ := v.(string)
		if _, ok := providers[member]; !ok {
			return groupError("provider_missing", "proxy provider not found: %s", member)
		}
	}
	if create && kind != "select" {
		if _, ok := target["url"]; !ok {
			target["url"] = "https://cp.cloudflare.com/generate_204"
		}
		if _, ok := target["interval"]; !ok {
			target["interval"] = 300
		}
	}
	cfg["proxy-groups"] = groups
	if tsCycle(cfg) {
		return groupError("cycle", "proxy group change would create a dependency cycle")
	}
	settings := patch
	if create {
		settings = target
	}
	return s.saveProxyGroupConfig(ctx, profileName, name, cfg, settings, create, false)
}

func (s *Store) RemoveProxyGroup(ctx context.Context, profileName, name string) error {
	if err := ValidateTailscaleName(name); err != nil {
		return err
	}
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	groups, _, err := namedList(cfg["proxy-groups"])
	if err != nil {
		return err
	}
	next := []any{}
	found := false
	for _, v := range groups {
		g, _ := v.(map[string]any)
		if tsString(g, "name") == name {
			found = true
		} else {
			next = append(next, v)
		}
	}
	if !found {
		return groupError("missing", "proxy group not found: %s; run nagi proxy group list", name)
	}
	cfg["proxy-groups"] = next

	if nodeReferenced(cfg, name) {
		references := []string{}
		for _, key := range []string{"proxy-groups", "proxies", "proxy-providers", "rule-providers", "rules", "sub-rules", "dns", "ntp", "listeners", "tunnels"} {
			if nodeReferenced(map[string]any{key: cfg[key]}, name) {
				references = append(references, key)
			}
		}
		return groupError("referenced", "proxy group %q is still referenced by %s; remove these references first", name, strings.Join(references, ", "))
	}

	// SetOverride composes custom rules and validates with mihomo before persistence.
	return s.saveProxyGroupConfig(ctx, profileName, name, cfg, nil, false, true)
}

// Keep explicit scheduler intent in the same metadata used by healthcheck
// commands; an old metadata entry must not override a group edit or recreation.
func (s *Store) saveProxyGroupConfig(ctx context.Context, profileName, name string, cfg, settings map[string]any, create, remove bool) error {
	_, _, overlay, err := s.healthMaps(profileName)
	if err != nil {
		return err
	}
	overlay["proxy-groups"] = cfg["proxy-groups"]
	for _, field := range []struct{ name, key string }{{"url", groupHealthcheckURLsKey}, {"interval", groupHealthcheckIntervalsKey}} {
		values, _ := overlay[field.key].(map[string]any)
		if values == nil {
			values = map[string]any{}
		}
		if create || remove {
			delete(values, name)
		}
		if value, present := settings[field.name]; present && !remove {
			values[name] = value
		}
		if len(values) == 0 {
			delete(overlay, field.key)
		} else {
			overlay[field.key] = values
		}
	}
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	return s.SetOverride(ctx, profileName, data)
}
