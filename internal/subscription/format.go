package subscription

import (
	"encoding/json"
	"errors"
	"sort"

	"gopkg.in/yaml.v3"
)

// Document parses supported mihomo YAML subscription responses.
// The returned document is independent of the source bytes and safe to merge.
func Document(data []byte) (map[string]any, string, error) {
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) == nil && len(doc) > 0 {
		if proxies, ok := doc["proxies"].([]any); ok {
			if err := checkProxies(proxies); err != nil {
				return nil, "", err
			}
			if _, ok := doc["proxy-groups"]; !ok {
				addGroup(doc, proxies)
			}
			return doc, "mihomo-yaml", nil
		}
		if _, ok := doc["proxy-providers"]; ok {
			return doc, "mihomo-yaml", nil
		}
	}
	return nil, "", errors.New("unsupported subscription format; expected mihomo YAML")
}

func checkProxies(proxies []any) error {
	for _, item := range proxies {
		p, ok := item.(map[string]any)
		if !ok || stringValue(p["name"]) == "" || stringValue(p["type"]) == "" {
			return errors.New("subscription contains a node without a name or type")
		}
	}
	return nil
}

func addGroup(doc map[string]any, proxies []any) {
	names := []any{"DIRECT"}
	for _, item := range proxies {
		names = append(names, item.(map[string]any)["name"])
	}
	doc["proxy-groups"] = []any{map[string]any{"name": "Subscription", "type": "select", "proxies": names}}
	if _, ok := doc["rules"]; !ok {
		doc["rules"] = []any{"MATCH,Subscription"}
	}
}

func stringValue(v any) string { s, _ := v.(string); return s }

// Merge retains local configuration and valid group choices in an existing profile.
func Merge(incoming, existing map[string]any) map[string]any {
	if existing == nil {
		return incoming
	}
	// These values are user-owned once a profile has been applied.
	for _, key := range []string{"rules", "rule-providers", "dns", "hosts", "tun", "mixed-port", "port", "socks-port", "redir-port", "tproxy-port", "allow-lan", "bind-address", "mode", "external-controller", "external-controller-unix", "secret", "ipv6", "sniffer", "profile"} {
		if value, ok := existing[key]; ok {
			incoming[key] = value
		}
	}
	oldGroups := groupMap(existing["proxy-groups"])
	for _, item := range sliceValue(incoming["proxy-groups"]) {
		group, ok := item.(map[string]any)
		if !ok {
			continue
		}
		old := oldGroups[stringValue(group["name"])]
		if old == nil {
			continue
		}
		previous := sliceValue(old["proxies"])
		available := map[string]bool{}
		for _, p := range sliceValue(group["proxies"]) {
			available[stringValue(p)] = true
		}
		retained := make([]any, 0, len(previous))
		seen := map[string]bool{}
		for _, p := range previous {
			name := stringValue(p)
			if available[name] && !seen[name] {
				retained = append(retained, p)
				seen[name] = true
			}
		}
		for _, p := range sliceValue(group["proxies"]) {
			name := stringValue(p)
			if !seen[name] {
				retained = append(retained, p)
				seen[name] = true
			}
		}
		if len(retained) > 0 {
			group["proxies"] = retained
		}
	}
	return incoming
}
func sliceValue(v any) []any { s, _ := v.([]any); return s }
func groupMap(v any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, item := range sliceValue(v) {
		if g, ok := item.(map[string]any); ok {
			out[stringValue(g["name"])] = g
		}
	}
	return out
}

type NodeChange struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type Preview struct {
	Name    string   `json:"name"`
	Format  string   `json:"format"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}

func Compare(name, format string, incoming, existing map[string]any) Preview {
	result := Preview{Name: name, Format: format, Added: []string{}, Removed: []string{}, Changed: []string{}}
	oldNodes, newNodes := nodeMap(existing), nodeMap(incoming)
	for n, v := range newNodes {
		if old, ok := oldNodes[n]; !ok {
			result.Added = append(result.Added, n)
		} else if old != v {
			result.Changed = append(result.Changed, n)
		}
	}
	for n := range oldNodes {
		if _, ok := newNodes[n]; !ok {
			result.Removed = append(result.Removed, n)
		}
	}
	sort.Strings(result.Added)
	sort.Strings(result.Removed)
	sort.Strings(result.Changed)
	return result
}
func nodeMap(doc map[string]any) map[string]string {
	out := map[string]string{}
	for _, item := range sliceValue(doc["proxies"]) {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		b, _ := json.Marshal(p)
		out[stringValue(p["name"])] = string(b)
	}
	return out
}
func Encode(doc map[string]any) ([]byte, error) { return yaml.Marshal(doc) }
