package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document converts supported subscription responses to a mihomo configuration.
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
	raw := strings.TrimSpace(string(data))
	if !strings.Contains(raw, "://") {
		decoded, err := decodeBase64(raw)
		if err != nil {
			return nil, "", errors.New("unsupported subscription format")
		}
		raw = strings.TrimSpace(string(decoded))
	}
	var proxies []any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxy, err := parseURI(line)
		if err != nil {
			return nil, "", err
		}
		proxies = append(proxies, proxy)
	}
	if len(proxies) == 0 {
		return nil, "", errors.New("subscription has no nodes")
	}
	doc = map[string]any{"proxies": proxies}
	addGroup(doc, proxies)
	return doc, "proxy-uris", nil
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

func decodeBase64(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(raw); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("invalid base64")
}

func parseURI(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid proxy URI")
	}
	switch u.Scheme {
	case "ss":
		host, user := u.Host, ""
		if u.User != nil {
			user = u.User.String()
		} else if i := strings.Index(host, "@"); i >= 0 {
			user, host = host[:i], host[i+1:]
		}
		if !strings.Contains(user, ":") {
			b, e := decodeBase64(user)
			if e != nil {
				return nil, errors.New("invalid Shadowsocks credentials")
			}
			user = string(b)
		}
		cipher, password, ok := strings.Cut(user, ":")
		server, port, err := splitHostPort(host)
		if !ok || err != nil || cipher == "" || password == "" {
			return nil, errors.New("invalid Shadowsocks URI")
		}
		return map[string]any{"name": uriName(u, server), "type": "ss", "server": server, "port": port, "cipher": cipher, "password": password}, nil
	case "trojan":
		server, port, err := splitHostPort(u.Host)
		if err != nil || u.User == nil {
			return nil, errors.New("invalid Trojan URI")
		}
		p, _ := u.User.Password()
		if p == "" {
			p = u.User.Username()
		}
		if p == "" {
			return nil, errors.New("invalid Trojan URI")
		}
		node := map[string]any{"name": uriName(u, server), "type": "trojan", "server": server, "port": port, "password": p}
		if sni := u.Query().Get("sni"); sni != "" {
			node["sni"] = sni
		}
		return node, nil
	case "vmess":
		b, e := decodeBase64(strings.TrimPrefix(raw, "vmess://"))
		if e != nil {
			return nil, errors.New("invalid VMess URI")
		}
		var v struct {
			Name    string      `json:"ps"`
			Server  string      `json:"add"`
			Port    json.Number `json:"port"`
			ID      string      `json:"id"`
			AlterID int         `json:"aid"`
			Cipher  string      `json:"scy"`
			Network string      `json:"net"`
			Host    string      `json:"host"`
			Path    string      `json:"path"`
			TLS     string      `json:"tls"`
			SNI     string      `json:"sni"`
		}
		if json.Unmarshal(b, &v) != nil || v.Server == "" || v.ID == "" {
			return nil, errors.New("invalid VMess URI")
		}
		var port int
		if _, e := fmt.Sscan(string(v.Port), &port); e != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid VMess URI")
		}
		if v.Name == "" {
			v.Name = v.Server
		}
		if v.Cipher == "" {
			v.Cipher = "auto"
		}
		node := map[string]any{"name": v.Name, "type": "vmess", "server": v.Server, "port": port, "uuid": v.ID, "alterId": v.AlterID, "cipher": v.Cipher}
		if v.TLS == "tls" {
			node["tls"] = true
		}
		if v.SNI != "" {
			node["servername"] = v.SNI
		}
		if v.Network != "" && v.Network != "tcp" {
			node["network"] = v.Network
		}
		if v.Network == "ws" {
			node["ws-opts"] = map[string]any{"path": v.Path, "headers": map[string]any{"Host": v.Host}}
		}
		return node, nil
	default:
		return nil, errors.New("unsupported proxy URI scheme")
	}
}

func splitHostPort(host string) (string, int, error) {
	u, e := url.Parse("http://" + host)
	if e != nil {
		return "", 0, errors.New("invalid host")
	}
	server := u.Hostname()
	var port int
	if _, e = fmt.Sscan(u.Port(), &port); e != nil || server == "" || port < 1 || port > 65535 {
		return "", 0, errors.New("invalid host or port")
	}
	return server, port, nil
}
func uriName(u *url.URL, fallback string) string {
	if n, e := url.QueryUnescape(u.Fragment); e == nil && n != "" {
		return n
	}
	return fallback
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
