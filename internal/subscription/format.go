package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ronigooja/Nagi/internal/subscription/v2ray"
)

// Document parses mihomo YAML or mihomo-compatible V2Ray-style subscription responses.
// The returned document is independent of the source bytes and safe to normalize.
func Document(data []byte) (map[string]any, string, error) {
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) == nil && len(doc) > 0 {
		if proxies, ok := doc["proxies"].([]any); ok {
			if err := checkProxies(proxies); err != nil {
				return nil, "", err
			}
			completeGroups(doc, proxies)
			return doc, "mihomo-yaml", nil
		}
		if _, ok := doc["proxy-providers"]; ok {
			completeGroups(doc, nil)
			return doc, "mihomo-yaml", nil
		}
	}
	if proxies, err := v2ray.ConvertsV2Ray(data); err == nil && len(proxies) > 0 {
		items := make([]any, len(proxies))
		for i := range proxies {
			items[i] = proxies[i]
		}
		doc = map[string]any{"proxies": items}
		completeGroups(doc, items)
		return doc, "mihomo-v2ray", nil
	}
	return nil, "", errors.New("unsupported subscription format; expected mihomo YAML or V2Ray links")
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

func completeGroups(doc map[string]any, proxies []any) {
	if _, exists := doc["proxy-groups"]; exists {
		if _, hasRules := doc["rules"]; !hasRules {
			target := "DIRECT"
			for _, item := range sliceValue(doc["proxy-groups"]) {
				if group, ok := item.(map[string]any); ok && stringValue(group["name"]) != "" {
					target = stringValue(group["name"])
					break
				}
			}
			doc["rules"] = []any{"MATCH," + target}
		}
		return
	}
	used := map[string]bool{"DIRECT": true, "REJECT": true, "GLOBAL": true}
	for _, item := range proxies {
		used[item.(map[string]any)["name"].(string)] = true
	}
	if sources, ok := doc["proxy-providers"].(map[string]any); ok {
		for providerName, raw := range sources {
			used[providerName] = true
			provider, ok := raw.(map[string]any)
			if !ok || provider["type"] != "inline" {
				continue
			}
			for _, item := range sliceValue(provider["payload"]) {
				if node, ok := item.(map[string]any); ok {
					used[stringValue(node["name"])] = true
				}
			}
		}
	}
	unique := func(base string) string {
		if !used[base] {
			used[base] = true
			return base
		}
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s-%d", base, i)
			if !used[candidate] {
				used[candidate] = true
				return candidate
			}
		}
	}
	names := []any{}
	alias := ""
	for _, item := range proxies {
		proxy := item.(map[string]any)
		name := proxy["name"].(string)
		if name == "GLOBAL" {
			// mihomo reserves GLOBAL for its global-mode group. Keep the node
			// selectable without colliding with the generated GLOBAL group.
			name = unique("GLOBAL")
			proxy["name"] = name
			alias = name
		}
		names = append(names, name)
	}
	if alias != "" {
		rewriteGlobalReferences(doc, alias)
	}
	providers := []any{}
	if source, ok := doc["proxy-providers"].(map[string]any); ok {
		keys := make([]string, 0, len(source))
		for name := range source {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			providers = append(providers, name)
		}
	}
	auto, fallback, balance, manual := unique("Subscription Auto"), unique("Subscription Fallback"), unique("Subscription Balance"), unique("Subscription")
	group := func(name, kind string) map[string]any {
		g := map[string]any{"name": name, "type": kind, "proxies": names, "use": providers}
		if kind == "url-test" || kind == "fallback" {
			g["url"] = "https://www.gstatic.com/generate_204"
			g["interval"] = 300
		}
		if kind == "url-test" {
			g["tolerance"] = 0
		}
		if kind == "load-balance" {
			g["url"] = "https://www.gstatic.com/generate_204"
			g["interval"] = 300
		}
		return g
	}
	choices := []any{auto, fallback, balance}
	choices = append(choices, names...)
	choices = append(choices, "DIRECT")
	doc["proxy-groups"] = []any{group(auto, "url-test"), group(fallback, "fallback"), group(balance, "load-balance"), map[string]any{"name": manual, "type": "select", "proxies": choices, "use": providers}, map[string]any{"name": "GLOBAL", "type": "select", "proxies": []any{manual}}}
	if _, ok := doc["mode"]; !ok {
		doc["mode"] = "rule"
	}
	if _, ok := doc["rules"]; !ok {
		doc["rules"] = []any{"MATCH," + manual}
	}
}

// Rewrite only fields that mihomo interprets as proxy references. Downloaded
// rule text and unrelated strings remain untouched.
func rewriteGlobalReferences(doc map[string]any, alias string) {
	for _, item := range sliceValue(doc["proxies"]) {
		if node, ok := item.(map[string]any); ok && node["dialer-proxy"] == "GLOBAL" {
			node["dialer-proxy"] = alias
		}
	}
	if providers, ok := doc["proxy-providers"].(map[string]any); ok {
		for _, raw := range providers {
			if provider, ok := raw.(map[string]any); ok {
				if override, ok := provider["override"].(map[string]any); ok && override["dialer-proxy"] == "GLOBAL" {
					override["dialer-proxy"] = alias
				}
			}
		}
	}
	rewriteRules := func(raw any) {
		for i, item := range sliceValue(raw) {
			rule, ok := item.(string)
			if !ok {
				continue
			}
			parts := strings.Split(rule, ",")
			last := len(parts) - 1
			if last > 0 && strings.TrimSpace(parts[last]) == "no-resolve" {
				last--
			}
			if last > 0 && strings.TrimSpace(parts[last]) == "GLOBAL" {
				parts[last] = alias
				sliceValue(raw)[i] = strings.Join(parts, ",")
			}
		}
	}
	rewriteRules(doc["rules"])
	if subrules, ok := doc["sub-rules"].(map[string]any); ok {
		for _, rules := range subrules {
			rewriteRules(rules)
		}
	}
	if dns, ok := doc["dns"].(map[string]any); ok {
		for _, field := range []string{"nameserver", "fallback", "default-nameserver", "proxy-server-nameserver", "direct-nameserver"} {
			rewriteDNSBindings(dns[field], alias)
		}
		for _, field := range []string{"nameserver-policy", "proxy-server-nameserver-policy"} {
			if policy, ok := dns[field].(map[string]any); ok {
				for key, value := range policy {
					if server, ok := value.(string); ok {
						policy[key] = rewriteDNSBinding(server, alias)
					} else {
						rewriteDNSBindings(value, alias)
					}
				}
			}
		}
	}
}

func rewriteDNSBindings(raw any, alias string) {
	for i, item := range sliceValue(raw) {
		value, ok := item.(string)
		if !ok {
			continue
		}
		sliceValue(raw)[i] = rewriteDNSBinding(value, alias)
	}
}

func rewriteDNSBinding(value, alias string) string {
	prefix, binding, found := strings.Cut(value, "#")
	if !found {
		return value
	}
	name, rest, hasInterface := strings.Cut(binding, "&")
	if name != "GLOBAL" {
		return value
	}
	if hasInterface {
		return prefix + "#" + alias + "&" + rest
	}
	return prefix + "#" + alias
}

func stringValue(v any) string { s, _ := v.(string); return s }

// Normalize keeps downloaded settings without carrying values from an old profile.
func Normalize(incoming map[string]any) map[string]any {
	_, _ = NormalizeChecked(incoming)
	return incoming
}

// NormalizeChecked rejects listener forms whose local bind cannot be established.
func NormalizeChecked(incoming map[string]any) (map[string]any, error) {
	// Nagi owns controller access and keeps subscription listeners local.
	for _, key := range []string{"external-controller", "external-controller-unix", "external-controller-tls", "external-controller-pipe", "external-controller-cors", "external-controller-routing-mark", "secret"} {
		delete(incoming, key)
	}
	incoming["allow-lan"] = false
	incoming["bind-address"] = "127.0.0.1"
	for _, field := range []string{"ss-config", "vmess-config"} {
		if raw, exists := incoming[field]; exists && raw != nil && raw != "" {
			return nil, fmt.Errorf("%s defines an inbound from an external file and cannot be bound to loopback", field)
		}
	}
	if raw, exists := incoming["tuic-server"]; exists {
		server, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("tuic-server must be a mapping")
		}
		if address, exists := server["listen"]; exists {
			value, ok := address.(string)
			if !ok {
				return nil, errors.New("tuic-server.listen must be a host:port address")
			}
			if value != "" {
				local, err := loopbackAddress(value, "tuic-server.listen")
				if err != nil {
					return nil, err
				}
				server["listen"] = local
			} else if server["enable"] == true {
				return nil, errors.New("tuic-server.listen is required when tuic-server.enable is true")
			}
		} else if server["enable"] == true {
			return nil, errors.New("tuic-server.listen is required when tuic-server.enable is true")
		}
	}
	if raw, exists := incoming["tunnels"]; exists {
		items, ok := raw.([]any)
		if !ok {
			return nil, errors.New("tunnels must be a list")
		}
		for i, item := range items {
			field := fmt.Sprintf("tunnels[%d].address", i)
			switch tunnel := item.(type) {
			case map[string]any:
				address, ok := tunnel["address"].(string)
				if !ok {
					return nil, fmt.Errorf("%s must be a host:port address", field)
				}
				local, err := loopbackAddress(address, field)
				if err != nil {
					return nil, err
				}
				tunnel["address"] = local
			case string:
				parts := strings.Split(tunnel, ",")
				if len(parts) != 3 && len(parts) != 4 {
					return nil, fmt.Errorf("tunnels[%d] must have network,address,target[,proxy]", i)
				}
				local, err := loopbackAddress(strings.TrimSpace(parts[1]), field)
				if err != nil {
					return nil, err
				}
				parts[1] = local
				items[i] = strings.Join(parts, ",")
			default:
				return nil, fmt.Errorf("tunnels[%d] must be a mapping or string", i)
			}
		}
	}
	if listeners, exists := incoming["listeners"]; exists {
		items, ok := listeners.([]any)
		if !ok {
			return nil, errors.New("listeners must be a list")
		}
		for i, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("listeners[%d] must be a mapping", i)
			}
			if _, ok := m["type"].(string); !ok {
				return nil, fmt.Errorf("listeners[%d].type must be a string", i)
			}
			if _, exists := m["listen"]; exists {
				if _, ok := m["listen"].(string); !ok {
					return nil, fmt.Errorf("listeners[%d].listen must be a host address", i)
				}
			}
			if m["type"] != "tun" {
				if raw, exists := m["listen"]; exists {
					host := raw.(string)
					if strings.ContainsAny(host, ":/") && net.ParseIP(host) == nil {
						return nil, fmt.Errorf("listeners[%d].listen must be a host address; use port for the port", i)
					}
				}
				m["listen"] = "127.0.0.1"
			}
		}
	}
	if rawDNS, exists := incoming["dns"]; exists {
		dns, ok := rawDNS.(map[string]any)
		if !ok {
			return nil, errors.New("dns must be a mapping")
		}
		if raw, exists := dns["listen"]; exists {
			address, ok := raw.(string)
			if !ok {
				return nil, errors.New("dns.listen must be a host:port address")
			}
			if address != "" {
				local, err := loopbackAddress(address, "dns.listen")
				if err != nil {
					return nil, err
				}
				dns["listen"] = local
			}
		}
	}
	// mihomo defaults this to true and otherwise restores cached group choices.
	profile, _ := incoming["profile"].(map[string]any)
	if profile == nil {
		profile = map[string]any{}
		incoming["profile"] = profile
	}
	profile["store-selected"] = false
	if incoming["mixed-port"] == nil && incoming["port"] == nil {
		incoming["mixed-port"] = 17890
	}
	return incoming, nil
}

func loopbackAddress(address, field string) (string, error) {
	if strings.Contains(address, "://") {
		return "", fmt.Errorf("%s has unsupported protocol; expected host:port", field)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("%s must be a host:port address", field)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s has invalid port", field)
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}
func sliceValue(v any) []any { s, _ := v.([]any); return s }

type NodeChange struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type Preview struct {
	Name                string         `json:"name"`
	Format              string         `json:"format"`
	Added               []string       `json:"added"`
	Removed             []string       `json:"removed"`
	Changed             []string       `json:"changed"`
	Changes             []ConfigChange `json:"changes"`
	RemoteNodesCompared bool           `json:"remote_nodes_compared"`
}

// ConfigChange contains only a path, change kind, and general behavioral hint.
// Values and user-defined names are intentionally excluded.
type ConfigChange struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Behavior string `json:"behavior"`
}

func Compare(name, format string, incoming, existing map[string]any) Preview {
	result := Preview{Name: name, Format: format, Added: []string{}, Removed: []string{}, Changed: []string{}, Changes: []ConfigChange{}}
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
	for _, category := range []string{"proxy-groups", "proxy-providers", "rules", "rule-providers", "dns", "tun", "listeners", "tunnels", "tuic-server", "mode", "port", "socks-port", "mixed-port", "redir-port", "tproxy-port", "bind-address", "allow-lan"} {
		var before, after any
		var oldExists, newExists bool
		if existing != nil {
			before, oldExists = existing[category]
		}
		if incoming != nil {
			after, newExists = incoming[category]
		}
		if reflect.DeepEqual(before, after) && oldExists == newExists {
			continue
		}
		kind := "changed"
		if !oldExists {
			kind = "added"
		} else if !newExists {
			kind = "removed"
		}
		behavior := "Configuration behavior may change."
		switch category {
		case "proxy-groups":
			behavior = "Proxy selection or group membership may change."
		case "proxy-providers":
			behavior = "Provider membership or update behavior may change."
		case "rules", "rule-providers":
			behavior = "Routing order or targets may change."
		case "dns":
			behavior = "DNS resolution or upstream routing may change."
		case "tun":
			behavior = "TUN traffic interception may change."
		case "listeners", "tunnels", "tuic-server", "port", "socks-port", "mixed-port", "redir-port", "tproxy-port", "bind-address", "allow-lan":
			behavior = "Local listening behavior may change."
		case "mode":
			behavior = "Default traffic routing mode may change."
		}
		result.Changes = append(result.Changes, ConfigChange{Path: "/" + category, Kind: kind, Behavior: behavior})
		if oldExists && newExists && category != "proxy-providers" && category != "rule-providers" {
			appendDetailedChanges(&result.Changes, "/"+category, before, after, behavior, 0)
		}
	}
	_, oldProvider := existing["proxy-providers"]
	_, newProvider := incoming["proxy-providers"]
	result.RemoteNodesCompared = !oldProvider && !newProvider
	return result
}

func appendDetailedChanges(out *[]ConfigChange, path string, before, after any, behavior string, depth int) {
	if depth >= 4 || reflect.DeepEqual(before, after) {
		return
	}
	if oldMap, ok := before.(map[string]any); ok {
		if newMap, ok := after.(map[string]any); ok {
			keys := map[string]bool{}
			for key := range oldMap {
				keys[key] = true
			}
			for key := range newMap {
				keys[key] = true
			}
			sorted := make([]string, 0, len(keys))
			for key := range keys {
				sorted = append(sorted, key)
			}
			sort.Strings(sorted)
			for _, key := range sorted {
				if !safePreviewField(key) {
					continue
				}
				a, oldOK := oldMap[key]
				b, newOK := newMap[key]
				if reflect.DeepEqual(a, b) && oldOK == newOK {
					continue
				}
				kind := "changed"
				if !oldOK {
					kind = "added"
				} else if !newOK {
					kind = "removed"
				}
				child := path + "/" + key
				*out = append(*out, ConfigChange{Path: child, Kind: kind, Behavior: behavior})
				if oldOK && newOK {
					appendDetailedChanges(out, child, a, b, behavior, depth+1)
				}
			}
			return
		}
	}
	if oldList, ok := before.([]any); ok {
		if newList, ok := after.([]any); ok {
			max := len(oldList)
			if len(newList) > max {
				max = len(newList)
			}
			for i := 0; i < max; i++ {
				child := fmt.Sprintf("%s/%d", path, i)
				kind := "changed"
				if i >= len(oldList) {
					kind = "added"
				} else if i >= len(newList) {
					kind = "removed"
				} else if reflect.DeepEqual(oldList[i], newList[i]) {
					continue
				}
				*out = append(*out, ConfigChange{Path: child, Kind: kind, Behavior: behavior})
				if i < len(oldList) && i < len(newList) {
					appendDetailedChanges(out, child, oldList[i], newList[i], behavior, depth+1)
				}
			}
		}
	}
}

func safePreviewField(field string) bool {
	switch field {
	case "name", "type", "proxies", "use", "url", "interval", "tolerance", "strategy", "filter", "exclude-filter", "rules", "enable", "listen", "port", "address", "network", "target", "nameserver", "fallback", "default-nameserver", "proxy-server-nameserver", "direct-nameserver", "nameserver-policy", "ipv6", "enhanced-mode", "fake-ip-range", "dns-hijack", "auto-route", "strict-route", "auto-detect-interface", "stack", "device", "gateway", "mtu", "include-interface", "exclude-interface":
		return true
	}
	return false
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
