package profile

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io"
	"io/fs"
	"net/url"
	"os"
	"sort"
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
type TailscaleTarget struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	DialerProxy string `json:"dialer_proxy"`
}
type TailscaleBinding struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	DialerProxy string `json:"dialer_proxy"`
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
func (s *Store) TailscaleNodes(name string) ([]TailscaleNode, error) {
	cfg, err := s.tailscaleConfig(name)
	if err != nil {
		return nil, err
	}
	nodes := []TailscaleNode{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if tsString(m, "type") == "tailscale" {
			nodes = append(nodes, tsNode(m))
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}
func tailscaleSupported(kind string) bool {
	switch kind {
	case "ss", "ssr", "socks5", "http", "vmess", "vless", "snell", "trojan", "hysteria", "hysteria2", "wireguard", "tuic", "gost-relay", "ssh", "mieru", "anytls", "sudoku", "masque", "trusttunnel", "openvpn":
		return true
	}
	return false
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

func tsTargets(cfg map[string]any) []TailscaleTarget {
	targets := []TailscaleTarget{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		kind := tsString(m, "type")
		if tailscaleSupported(kind) {
			targets = append(targets, TailscaleTarget{"node", tsString(m, "name"), kind, tsString(m, "dialer-proxy")})
		}
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	for name, v := range providers {
		m, _ := v.(map[string]any)
		dialer, _ := tsProviderDialer(m)
		targets = append(targets, TailscaleTarget{"provider", name, tsString(m, "type"), dialer})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Kind != targets[j].Kind {
			return targets[i].Kind < targets[j].Kind
		}
		return targets[i].Name < targets[j].Name
	})
	return targets
}
func (s *Store) TailscaleTargets(name string) ([]TailscaleTarget, error) {
	cfg, err := s.tailscaleConfig(name)
	if err != nil {
		return nil, err
	}
	return tsTargets(cfg), nil
}
func (s *Store) TailscaleShow(profileName, name string) (TailscaleNode, []TailscaleBinding, error) {
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return TailscaleNode{}, nil, err
	}
	m := tsFind(cfg, name)
	if m == nil || tsString(m, "type") != "tailscale" {
		return TailscaleNode{}, nil, i18n.Errorf("backend.profile.tailscale_node_not_found_run_nagi_proxy_tailscale_list", "Tailscale node not found; run `nagi proxy tailscale list`")
	}
	bindings := []TailscaleBinding{}
	for _, t := range tsTargets(cfg) {
		if t.DialerProxy == name {
			bindings = append(bindings, TailscaleBinding{t.Kind, t.Name, name})
		}
	}
	policy, err := s.SubscriptionTailscale(profileName)
	if err != nil {
		return TailscaleNode{}, nil, err
	}
	if policy != nil && tsString(policy.Node, "name") == name {
		bindings = append(bindings, TailscaleBinding{"subscription", policy.Subscription, name})
	}
	return tsNode(m), bindings, nil
}
func (s *Store) saveTailscale(ctx context.Context, name string, cfg map[string]any, kind string, targets []string) error {
	overlay := map[string]any{}
	data, err := s.Override(name)
	if err == nil {
		if yaml.Unmarshal(data, &overlay) != nil || overlay == nil {
			return i18n.Errorf("backend.profile.local_override_must_be_a_yaml_mapping", "local override must be a YAML mapping")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if kind != "provider" {
		delete(overlay, proxyAdditionsKey)
		overlay["proxies"] = tsProxies(cfg)
	} else {
		locals, _ := overlay["proxy-providers"].(map[string]any)
		if locals == nil {
			locals = map[string]any{}
		}
		for _, provider := range targets {
			item, _ := locals[provider].(map[string]any)
			if item == nil {
				item = map[string]any{}
			}
			o, _ := item["override"].(map[string]any)
			if o == nil {
				o = map[string]any{}
			}
			providers, _ := cfg["proxy-providers"].(map[string]any)
			p, _ := providers[provider].(map[string]any)
			effective, _ := p["override"].(map[string]any)
			o["dialer-proxy"] = tsString(effective, "dialer-proxy")
			item["override"] = o
			locals[provider] = item
		}
		overlay["proxy-providers"] = locals
	}
	updated, err := yaml.Marshal(overlay)
	if err != nil {
		return i18n.Errorf("backend.profile.could_not_prepare_tailscale_override", "could not prepare Tailscale override")
	}
	return s.SetOverride(ctx, name, updated)
}
func (s *Store) MutateTailscale(ctx context.Context, profileName, action, name, kind, target string, o TailscaleOptions) error {
	if err := ValidateTailscaleOptions(o); err != nil {
		return err
	}
	if o.AuthKey != nil {
		if err := ValidateTailscaleAuthKey(*o.AuthKey); err != nil {
			return err
		}
	}
	if action == "bind" || action == "unbind" {
		return s.MutateTailscaleBindings(ctx, profileName, action, name, kind, []string{target})
	}
	policy, err := s.SubscriptionTailscale(profileName)
	if err != nil {
		return err
	}
	if policy != nil && (action != "edit" || tsString(policy.Node, "name") != name) {
		return i18n.Errorf("backend.profile.subscription_tailscale_policy_is_active_unbind_the_subscription_before_adding_or_removing_nodes", "subscription Tailscale policy is active; unbind the subscription before adding or removing nodes")
	}
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	node := tsFind(cfg, name)
	if action == "add" {
		if err := ValidateTailscaleName(name); err != nil {
			return err
		}
		if node != nil {
			return i18n.Errorf("backend.profile.proxy_name_already_exists", "proxy NAME already exists")
		}
	}
	if action == "add" {
		providers, _ := cfg["proxy-providers"].(map[string]any)
		if _, exists := providers[name]; exists {
			return i18n.Errorf("backend.profile.name_conflicts_with_an_existing_provider", "NAME conflicts with an existing provider")
		}
		groups, _ := cfg["proxy-groups"].([]any)
		for _, v := range groups {
			m, _ := v.(map[string]any)
			if tsString(m, "name") == name {
				return i18n.Errorf("backend.profile.name_conflicts_with_an_existing_group", "NAME conflicts with an existing group")
			}
		}
		if o.ExitNode == nil || o.AuthKeyFile == nil && o.AuthKey == nil {
			return i18n.Errorf("backend.profile.add_requires_exit_node_and_an_auth_key", "add requires EXIT_NODE and an auth key")
		}
		node = map[string]any{"name": name, "type": "tailscale", "udp": true, "control-url": nagiruntime.TailscaleControlURL}
		cfg["proxies"] = append(tsProxies(cfg), node)
	} else if action != "unbind" && (node == nil || tsString(node, "type") != "tailscale") {
		return i18n.Errorf("backend.profile.tailscale_node_not_found_run_nagi_proxy_tailscale_list", "Tailscale node not found; run `nagi proxy tailscale list`")
	}
	switch action {
	case "add", "edit":
		if o.ExitNode != nil {
			node["exit-node"] = *o.ExitNode
		}
		if o.Hostname != nil {
			node["hostname"] = *o.Hostname
		}
		if o.ControlURL != nil {
			node["control-url"] = nagiruntime.NormalizeTailscaleControlURL(*o.ControlURL)
		}
		if o.AuthKeyFile != nil {
			key, err := readTailscaleAuthKey(*o.AuthKeyFile)
			if err != nil {
				return err
			}
			node["auth-key"] = key
		}
		if o.AuthKey != nil {
			node["auth-key"] = strings.TrimSpace(*o.AuthKey)
		}
		path, err := nagiruntime.TailscaleStatePath(s.Dir, profileName, name, tsString(node, "control-url"))
		if err != nil {
			return err
		}
		node["state-dir"] = path
	case "remove":
		for _, t := range tsTargets(cfg) {
			if t.DialerProxy == name {
				return i18n.Errorf("backend.profile.tailscale_node_is_still_bound_unbind_its_targets_first", "Tailscale node is still bound; unbind its targets first")
			}
		}
		for _, v := range tsProxies(cfg) {
			m, _ := v.(map[string]any)
			if tsString(m, "dialer-proxy") == name {
				return i18n.Errorf("backend.profile.tailscale_node_is_still_referenced_by_a_dialer_proxy", "Tailscale node is still referenced by a dialer-proxy")
			}
		}

		providers, _ := cfg["proxy-providers"].(map[string]any)
		for _, v := range providers {
			p, _ := v.(map[string]any)
			dialer, overrides := tsProviderDialer(p)
			if dialer == name {
				return i18n.Errorf("backend.profile.tailscale_node_is_still_referenced_by_a_provider", "Tailscale node is still referenced by a provider")
			}
			if !overrides {
				payload, _ := p["payload"].([]any)
				for _, v := range payload {
					m, _ := v.(map[string]any)
					if tsString(m, "dialer-proxy") == name {
						return i18n.Errorf("backend.profile.tailscale_node_is_still_referenced_by_a_provider_node", "Tailscale node is still referenced by a provider node")
					}
				}
			}
		}
		if tsDNSReferences(cfg["dns"], name) {
			return i18n.Errorf("backend.profile.tailscale_node_is_still_referenced_by_dns_configuration_edit_its_configuration_explicitly", "Tailscale node is still referenced by DNS configuration; edit its configuration explicitly")
		}
		list := []any{}
		for _, v := range tsProxies(cfg) {
			m, _ := v.(map[string]any)
			if tsString(m, "name") != name {
				list = append(list, v)
			}
		}
		cfg["proxies"] = list
	default:
		return i18n.Errorf("backend.profile.unsupported_tailscale_action", "unsupported Tailscale action")
	}
	if policy != nil {
		oldNode := policy.Node
		policy.Node = node
		if err := s.writeSubscriptionTailscale(profileName, policy); err != nil {
			return err
		}
		patch, err := s.Override(profileName)
		if errors.Is(err, fs.ErrNotExist) {
			patch, err = []byte("{}\n"), nil
		}
		if err == nil {
			err = s.SetOverride(ctx, profileName, patch)
		}
		if err != nil {
			policy.Node = oldNode
			return errors.Join(err, s.writeSubscriptionTailscale(profileName, policy))
		}
		return nil
	}
	return s.saveTailscale(ctx, profileName, cfg, "node", nil)
}

// Dependencies include groups and provider payloads so a group-mediated dialer
// chain cannot escape the cycle guard. mihomo performs final native validation.
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
func (s *Store) MutateTailscaleBindings(ctx context.Context, profileName, action, name, kind string, targets []string) error {
	if action != "bind" && action != "unbind" {
		return i18n.Errorf("backend.profile.binding_action_must_be_bind_or_unbind", "binding action must be bind or unbind")
	}
	policy, err := s.SubscriptionTailscale(profileName)
	if err != nil {
		return err
	}
	if policy != nil {
		return i18n.Errorf("backend.profile.subscription_tailscale_policy_is_active_unbind_the_subscription_before_changing_individual_bindings", "subscription Tailscale policy is active; unbind the subscription before changing individual bindings")
	}
	if kind != "node" && kind != "provider" {
		return i18n.Errorf("backend.profile.kind_must_be_node_or_provider", "KIND must be node or provider")
	}
	if len(targets) == 0 {
		return i18n.Errorf("backend.profile.at_least_one_target_is_required", "at least one TARGET is required")
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if err := ValidateTailscaleName(target); err != nil {
			return err
		}
		if seen[target] {
			return i18n.Errorf("backend.profile.duplicate_binding_target", "duplicate binding target")
		}
		seen[target] = true
	}
	cfg, err := s.tailscaleConfig(profileName)
	if err != nil {
		return err
	}
	if action == "bind" {
		node := tsFind(cfg, name)
		if node == nil || tsString(node, "type") != "tailscale" {
			return i18n.Errorf("backend.profile.tailscale_node_not_found_run_nagi_proxy_tailscale_list", "Tailscale node not found; run `nagi proxy tailscale list`")
		}
	}
	for _, target := range targets {
		if err := tsChangeBinding(cfg, action, name, kind, target); err != nil {
			return err
		}
	}
	return s.saveTailscale(ctx, profileName, cfg, kind, targets)
}

func tsChangeBinding(cfg map[string]any, action, name, kind, target string) error {
	var selected map[string]any
	current := ""
	for _, t := range tsTargets(cfg) {
		if t.Kind == kind && t.Name == target {
			current = t.DialerProxy
			if kind == "node" {
				selected = tsFind(cfg, target)
			} else {
				providers, _ := cfg["proxy-providers"].(map[string]any)
				p, _ := providers[target].(map[string]any)
				selected, _ = p["override"].(map[string]any)
				if selected == nil {
					selected = map[string]any{}
					p["override"] = selected
				}
				if action == "bind" {
					payload, _ := p["payload"].([]any)
					for _, v := range payload {
						m, _ := v.(map[string]any)
						dialer := tsString(m, "dialer-proxy")
						if dialer != "" {
							existing := tsFind(cfg, dialer)
							if existing == nil || tsString(existing, "type") != "tailscale" {
								return i18n.Errorf("backend.profile.provider_contains_a_node_with_a_non_tailscale_dialer_proxy", "provider contains a node with a non-Tailscale dialer-proxy")
							}
						}
					}
				}
			}
			break
		}
	}
	if selected == nil {
		return i18n.Errorf("backend.profile.target_not_found_or_unsupported_run_nagi_proxy_tailscale_targets", "target not found or unsupported; run `nagi proxy tailscale targets`")
	}
	if current != "" {
		existing := tsFind(cfg, current)
		if existing == nil || tsString(existing, "type") != "tailscale" {
			return i18n.Errorf("backend.profile.target_has_an_existing_non_tailscale_dialer_proxy_edit_its_configuration_explicitly", "target has an existing non-Tailscale dialer-proxy; edit its configuration explicitly")
		}
	}
	if action == "bind" {
		selected["dialer-proxy"] = name
		if tsCycle(cfg) {
			return i18n.Errorf("backend.profile.binding_would_create_a_proxy_dependency_cycle", "binding would create a proxy dependency cycle")
		}
	} else {
		selected["dialer-proxy"] = ""
	}
	return nil
}
