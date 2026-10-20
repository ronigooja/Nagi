package profile

import (
	"github.com/ronigooja/Nagi/internal/i18n"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const MaxTailscaleAuthKeyBytes = 16 << 10

type TailscaleNode struct {
	Name              string `json:"name"`
	ExitNode          string `json:"exit_node"`
	Hostname          string `json:"hostname"`
	ControlURL        string `json:"control_url"`
	AuthKeyConfigured bool   `json:"auth_key_configured"`
}
type TailscaleOptions struct{ ExitNode, Hostname, ControlURL, AuthKeyFile, AuthKey *string }

func ValidateTailscaleName(value string) error {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return i18n.Errorf("backend.profile.name_must_contain_1_128_printable_bytes_without_surrounding_whitespace", "NAME must contain 1..128 printable bytes without surrounding whitespace")
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return i18n.Errorf("backend.profile.name_must_contain_printable_characters", "NAME must contain printable characters")
		}
	}
	switch value {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL":
		return i18n.Errorf("backend.profile.name_conflicts_with_a_reserved_proxy_name", "NAME conflicts with a reserved proxy name")
	}
	return nil
}
func ValidateTailscaleOptions(o TailscaleOptions) error {
	if o.AuthKey != nil && o.AuthKeyFile != nil {
		return i18n.Errorf("backend.profile.choose_one_auth_key_input_method", "choose one auth key input method")
	}
	if o.AuthKeyFile != nil && strings.TrimSpace(*o.AuthKeyFile) == "" {
		return i18n.Errorf("backend.profile.auth_key_file_must_be_specified", "auth key file must be specified")
	}
	for _, value := range []*string{o.ExitNode, o.Hostname} {
		if value != nil {
			if strings.TrimSpace(*value) == "" || len(*value) > 253 {
				return i18n.Errorf("backend.profile.exit_node_and_hostname_must_be_nonempty_and_at_most_253_bytes", "exit-node and hostname must be nonempty and at most 253 bytes")
			}
			for _, r := range *value {
				if !unicode.IsPrint(r) {
					return i18n.Errorf("backend.profile.exit_node_and_hostname_must_be_printable", "exit-node and hostname must be printable")
				}
			}
		}
	}
	if o.ControlURL != nil {
		raw := nagiruntime.NormalizeTailscaleControlURL(*o.ControlURL)
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return i18n.Errorf("backend.profile.control_url_must_be_an_absolute_https_url_without_credentials_query_or_fragment", "control-url must be an absolute HTTPS URL without credentials, query, or fragment")
		}
	}
	return nil
}
func readTailscaleAuthKey(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxTailscaleAuthKeyBytes {
		return "", i18n.Errorf("backend.profile.auth_key_file_must_be_a_regular_file_containing_1_16384_bytes", "auth key file must be a regular file containing 1..16384 bytes")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", i18n.Errorf("backend.profile.could_not_read_auth_key_file", "could not read auth key file")
	}
	f := os.NewFile(uintptr(fd), "auth-key")
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return "", i18n.Errorf("backend.profile.auth_key_file_changed_while_opening", "auth key file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxTailscaleAuthKeyBytes+1))
	if err != nil || len(data) > MaxTailscaleAuthKeyBytes {
		return "", i18n.Errorf("backend.profile.could_not_read_bounded_auth_key_file", "could not read bounded auth key file")
	}
	key := strings.TrimSpace(string(data))
	if err := ValidateTailscaleAuthKey(key); err != nil {
		return "", err
	}
	return key, nil
}

// ValidateTailscaleAuthKey never includes the supplied key in an error.
func ValidateTailscaleAuthKey(value string) error {
	if len(value) > MaxTailscaleAuthKeyBytes {
		return i18n.Errorf("backend.profile.auth_key_must_contain_at_most_16384_bytes", "auth key must contain at most 16384 bytes")
	}
	key := strings.TrimSpace(value)
	if key == "" {
		return i18n.Errorf("backend.profile.auth_key_must_not_be_empty", "auth key must not be empty")
	}
	for _, r := range key {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return i18n.Errorf("backend.profile.auth_key_must_contain_one_printable_key", "auth key must contain one printable key")
		}
	}
	return nil
}
func tsString(m map[string]any, key string) string { value, _ := m[key].(string); return value }
func tsNode(m map[string]any) TailscaleNode {
	return TailscaleNode{tsString(m, "name"), tsString(m, "exit-node"), tsString(m, "hostname"), nagiruntime.NormalizeTailscaleControlURL(tsString(m, "control-url")), strings.TrimSpace(tsString(m, "auth-key")) != ""}
}
func (s *Store) tailscaleConfig(name string) (map[string]any, error) {
	data, err := s.Effective(name)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil || cfg == nil {
		return nil, i18n.Errorf("backend.profile.selected_profile_must_be_a_yaml_mapping", "selected profile must be a YAML mapping")
	}
	return cfg, nil
}
func tsProxies(cfg map[string]any) []any { list, _ := cfg["proxies"].([]any); return list }
func tsFind(cfg map[string]any, name string) map[string]any {
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if tsString(m, "name") == name {
			return m
		}
	}
	return nil
}
func tsProviderDialer(provider map[string]any) (string, bool) {
	override, _ := provider["override"].(map[string]any)
	if value, exists := override["dialer-proxy"]; exists {
		dialer, _ := value.(string)
		return dialer, true
	}
	dialer := tsString(provider, "dialer-proxy")
	return dialer, dialer != ""
}

func tsCycle(cfg map[string]any) bool {
	graph := map[string][]string{}
	add := func(from, to string) {
		if to != "" {
			graph[from] = append(graph[from], to)
		}
	}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		add(tsString(m, "name"), tsString(m, "dialer-proxy"))
	}
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		m, _ := v.(map[string]any)
		name := tsString(m, "name")
		members, _ := m["proxies"].([]any)
		for _, x := range members {
			s, _ := x.(string)
			add(name, s)
		}
		uses, _ := m["use"].([]any)
		for _, x := range uses {
			s, _ := x.(string)
			add(name, "provider:"+s)
		}
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	for name, v := range providers {
		m, _ := v.(map[string]any)

		dialer, overrides := tsProviderDialer(m)
		add("provider:"+name, dialer)
		if !overrides {
			payload, _ := m["payload"].([]any)
			for _, x := range payload {
				p, _ := x.(map[string]any)
				add("provider:"+name, tsString(p, "dialer-proxy"))
			}
		}

	}
	state := map[string]int{}
	var visit func(string) bool
	visit = func(n string) bool {
		if state[n] == 1 {
			return true
		}
		if state[n] == 2 {
			return false
		}
		state[n] = 1
		for _, next := range graph[n] {
			if visit(next) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	for n := range graph {
		if visit(n) {
			return true
		}
	}
	return false
}

// RewriteTailscaleState replaces arbitrary source state paths without reading them.
// Query generation does not create directories; validation/activation prepares them.
func RewriteTailscaleState(data []byte, root, name string, create bool) ([]byte, bool, error) {
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil || cfg == nil {
		return data, false, nil
	}
	changed := false
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if tsString(m, "type") != "tailscale" {
			continue
		}
		changed = true
		var path string
		var err error
		if create {
			path, err = nagiruntime.TailscaleStatePath(root, name, tsString(m, "name"), tsString(m, "control-url"))
		} else {
			path = nagiruntime.TailscaleStateName(root, name, tsString(m, "name"), tsString(m, "control-url"))
		}
		if err != nil {
			return nil, true, err
		}
		m["state-dir"] = path
	}
	if !changed {
		return data, false, nil
	}
	out, err := yaml.Marshal(cfg)
	return out, true, err
}

func tsDNSReferences(value any, name string) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if tsDNSReferences(child, name) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if tsDNSReferences(child, name) {
				return true
			}
		}
	case string:
		transport := strings.SplitN(v, "#", 2)[0]
		if strings.HasPrefix(transport, "ts://") || strings.HasPrefix(transport, "tailscale://") {
			u, err := url.Parse(transport)
			if err == nil && u.Host == name {
				return true
			}
			if strings.TrimPrefix(strings.TrimPrefix(transport, "tailscale://"), "ts://") == name {
				return true
			}
		}
		if _, suffix, has := strings.Cut(v, "#"); has {
			for _, part := range strings.Split(suffix, "&") {
				if part == name {
					return true
				}
			}
		}
	}
	return false
}

// MutateTailscaleBindings prepares every target before saving one override. The
// caller holds config.lock and owns runtime recovery through the CLI transaction.
