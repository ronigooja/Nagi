package profile

import (
	"context"
	"errors"
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"gopkg.in/yaml.v3"
	"io/fs"
	"sort"
	"strings"
)

type ProxyChainTarget struct {
	Kind              string         `json:"kind"`
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	Via               []string       `json:"via"`
	Provider          string         `json:"provider,omitempty"`
	ID                string         `json:"id,omitempty"`
	Available         bool           `json:"available"`
	Supported         bool           `json:"supported"`
	UnsupportedReason string         `json:"unsupported_reason,omitempty"`
	PathRefs          []ChainNodeRef `json:"path_refs,omitempty"`
	Outlet            string         `json:"outlet,omitempty"`
}
type ProxyChainChange struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Provider string `json:"provider,omitempty"`
}
type ProxyChainReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Via  string `json:"via"`
}
type ProxyChainPlan struct {
	PathChanges  []ProxyChainPathChange     `json:"path_changes"`
	Outlet       string                     `json:"outlet,omitempty"`
	OutletGroups []string                   `json:"outlet_groups,omitempty"`
	Kind         string                     `json:"kind"`
	Targets      []string                   `json:"targets"`
	Via          []string                   `json:"via"`
	Changes      []ProxyChainChange         `json:"changes"`
	Persistent   bool                       `json:"persistent"`
	SharedUse    []ProxyChainReference      `json:"shared_use"`
	Conflicts    []string                   `json:"conflicts"`
	Messages     map[string]i18n.Descriptor `json:"-"`
}

func chainSupported(kind string) bool {
	switch kind {
	case "tailscale", "ss", "ssr", "socks5", "http", "vmess", "vless", "snell", "trojan", "hysteria", "hysteria2", "wireguard", "tuic", "gost-relay", "ssh", "mieru", "anytls", "sudoku", "masque", "trusttunnel", "openvpn":
		return true
	}
	return false
}
func chainHop(kind string) bool {
	return chainSupported(kind)
}
func chainProviderDialer(p map[string]any) string {
	o, _ := p["override"].(map[string]any)
	if v, ok := o["dialer-proxy"]; ok {
		s, _ := v.(string)
		return s
	}
	return tsString(p, "dialer-proxy")
}
func chainPath(cfg map[string]any, dialer string) []string {
	result := []string{}
	seen := map[string]bool{}
	for dialer != "" && !seen[dialer] {
		seen[dialer] = true
		result = append([]string{dialer}, result...)
		dialer = tsString(tsFind(cfg, dialer), "dialer-proxy")
	}
	return result
}
func chainTargets(cfg map[string]any) []ProxyChainTarget {
	result := []ProxyChainTarget{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		result = append(result, ProxyChainTarget{Kind: "node", Name: tsString(m, "name"), Type: tsString(m, "type"), Via: chainPath(cfg, tsString(m, "dialer-proxy")), Available: true, Supported: chainHop(tsString(m, "type"))})
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	for n, v := range providers {
		p, _ := v.(map[string]any)
		result = append(result, ProxyChainTarget{Kind: "provider", Name: n, Type: tsString(p, "type"), Via: chainPath(cfg, chainProviderDialer(p))})
	}
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		m, _ := v.(map[string]any)
		result = append(result, ProxyChainTarget{Kind: "group", Name: tsString(m, "name"), Type: tsString(m, "type"), Via: []string{}})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Name < result[j].Name
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}
func (s *Store) chainConfig(name string) (map[string]any, error) {
	data, err := s.Effective(name)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if yaml.Unmarshal(data, &cfg) != nil || cfg == nil {
		return nil, i18n.Errorf("backend.profile.chain_profile_must_be_a_yaml_mapping", "profile must be a YAML mapping")
	}
	return cfg, nil
}
func (s *Store) ProxyChains(name string) ([]ProxyChainTarget, error) {
	cfg, err := s.chainLogicalConfig(name)
	if err != nil {
		return nil, err
	}
	_, ranks, err := s.subscriptionNodeOrder(name)
	if err != nil {
		return nil, err
	}
	members, err := s.savedProviderMembers(name)
	if err != nil {
		return nil, err
	}
	stripProviderResources(cfg, members)
	out := chainTargets(cfg)
	for i, t := range out {
		if t.Kind == "node" {
			out[i].PathRefs = chainPathRefs(cfg, tsString(tsFind(cfg, t.Name), "dialer-proxy"), members)
			out[i].PathRefs = append(out[i].PathRefs, ChainNodeRef{Kind: "node", Name: t.Name})
			out[i].Via = []string{}
			for _, r := range out[i].PathRefs[:len(out[i].PathRefs)-1] {
				out[i].Via = append(out[i].Via, r.Name)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind == "node" && out[j].Kind == "node" {
			return sourceNodeLess(out[i].Name, out[j].Name, ranks)
		}
		return out[i].Kind < out[j].Kind
	})
	p, err := s.SubscriptionChain(name)
	if err != nil {
		return nil, err
	}
	if p != nil {
		out = append(out, ProxyChainTarget{Kind: "subscription", Name: p.Subscription, Via: append([]string{}, p.Via...)})
	}
	return out, nil
}
func (s *Store) ProxyChain(profileName, kind, target string) (ProxyChainTarget, error) {
	values, err := s.ProxyChains(profileName)
	if err != nil {
		return ProxyChainTarget{}, err
	}
	for _, v := range values {
		if v.Kind == kind && v.Name == target {
			return v, nil
		}
	}
	return ProxyChainTarget{}, i18n.Errorf("backend.profile.chain_chain_target_not_found", "chain target not found")
}
func (s *Store) ProxyChainCandidates(profileName, kind string, targets []string) ([]ProxyNode, error) {
	cfg, err := s.chainConfig(profileName)
	if err != nil {
		return nil, err
	}
	_, ranks, err := s.subscriptionNodeOrder(profileName)
	if err != nil {
		return nil, err
	}
	out := []ProxyNode{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if !chainHop(tsString(m, "type")) {
			continue
		}
		excluded := false
		for _, n := range targets {
			if kind == "node" && n == tsString(m, "name") {
				excluded = true
			}
		}
		if !excluded {
			out = append(out, ProxyNode{Name: tsString(m, "name"), Type: tsString(m, "type")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return sourceNodeLess(out[i].Name, out[j].Name, ranks) })
	return out, nil
}

type chainVertex struct{ Kind, Name string }

func chainGraph(cfg map[string]any) (map[chainVertex][]chainVertex, error) {
	graph := map[chainVertex][]chainVertex{}
	names := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	addName := func(n string) error {
		if n == "" || names[n] {
			return i18n.Errorf("backend.profile.chain_duplicate_or_invalid_proxy_name", "duplicate or invalid proxy name")
		}
		names[n] = true
		return nil
	}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if err := addName(tsString(m, "name")); err != nil {
			return nil, err
		}
	}
	groups, _ := cfg["proxy-groups"].([]any)
	for _, v := range groups {
		m, _ := v.(map[string]any)
		if err := addName(tsString(m, "name")); err != nil {
			return nil, err
		}
	}
	add := func(from chainVertex, to string, provider bool) error {
		if to == "" {
			return nil
		}
		kind := "proxy"
		if provider {
			kind = "provider"
			if _, ok := providers[to]; !ok {
				return i18n.Errorf("backend.profile.chain_proxy_group_references_a_missing_provider", "proxy group references a missing provider")
			}
		} else if !names[to] {
			return i18n.Errorf("backend.profile.chain_proxy_dependency_references_a_missing_node_or_group", "proxy dependency references a missing node or group")
		}
		graph[from] = append(graph[from], chainVertex{kind, to})
		return nil
	}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		if err := add(chainVertex{"proxy", tsString(m, "name")}, tsString(m, "dialer-proxy"), false); err != nil {
			return nil, err
		}
	}
	for n, v := range providers {
		p, _ := v.(map[string]any)
		from := chainVertex{"provider", n}
		for _, d := range []string{chainProviderDialer(p), tsString(p, "proxy")} {
			if err := add(from, d, false); err != nil {
				return nil, err
			}
		}
		override, _ := p["override"].(map[string]any)
		_, hasOverride := override["dialer-proxy"]
		payload, _ := p["payload"].([]any)
		if !hasOverride {
			for _, x := range payload {
				m, _ := x.(map[string]any)
				if err := add(from, tsString(m, "dialer-proxy"), false); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, v := range groups {
		m, _ := v.(map[string]any)
		from := chainVertex{"proxy", tsString(m, "name")}
		members, _ := m["proxies"].([]any)
		for _, x := range members {
			d, _ := x.(string)
			node := tsFind(cfg, d)
			excluded := false
			var err error
			if node != nil {
				excluded, err = nodeGroupExcluded(m, node)
				if err != nil {
					return nil, err
				}
			}
			if !excluded {
				if err := add(from, d, false); err != nil {
					return nil, err
				}
			}
		}
		uses, _ := m["use"].([]any)
		for _, x := range uses {
			d, _ := x.(string)
			if err := add(from, d, true); err != nil {
				return nil, err
			}
		}
		all, _ := m["include-all"].(bool)
		pn, _ := m["include-all-proxies"].(bool)
		pp, _ := m["include-all-providers"].(bool)
		if all || pn {
			for _, x := range tsProxies(cfg) {
				node, _ := x.(map[string]any)
				excluded, err := nodeGroupExcluded(m, node)
				if err != nil {
					return nil, err
				}
				match, err := nodeGroupMatch(tsString(m, "filter"), tsString(node, "name"), true)
				if err != nil {
					return nil, err
				}
				if match && !excluded {
					_ = add(from, tsString(node, "name"), false)
				}
			}
		}
		if all || pp {
			for n := range providers {
				_ = add(from, n, true)
			}
		}
		if err := add(from, tsString(m, "dialer-proxy"), false); err != nil {
			return nil, err
		}
	}
	return graph, nil
}
func chainValidateGraph(cfg map[string]any) error {
	graph, err := chainGraph(cfg)
	if err != nil {
		return err
	}
	state := map[chainVertex]int{}
	var visit func(chainVertex) bool
	visit = func(n chainVertex) bool {
		if state[n] == 1 {
			return true
		}
		if state[n] == 2 {
			return false
		}
		state[n] = 1
		for _, d := range graph[n] {
			if visit(d) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	for n := range graph {
		if visit(n) {
			return i18n.Errorf("backend.profile.chain_proxy_chain_creates_a_dependency_cycle", "proxy chain creates a dependency cycle")
		}
	}
	return nil
}
func chainSharedUses(cfg map[string]any, kind string, targets, via []string) ([]ProxyChainReference, error) {
	graph, err := chainGraph(cfg)
	if err != nil {
		return nil, err
	}
	out := []ProxyChainReference{}
	for _, target := range chainTargets(cfg) {
		selected := false
		for _, n := range targets {
			if kind == target.Kind && n == target.Name {
				selected = true
			}
		}
		if selected {
			continue
		}
		vKind := "proxy"
		if target.Kind == "provider" {
			vKind = "provider"
		}
		for _, hop := range via {
			wanted := chainVertex{"proxy", hop}
			seen := map[chainVertex]bool{}
			var reaches func(chainVertex) bool
			reaches = func(n chainVertex) bool {
				if seen[n] {
					return false
				}
				seen[n] = true
				for _, next := range graph[n] {
					if next == wanted || reaches(next) {
						return true
					}
				}
				return false
			}
			if reaches(chainVertex{vKind, target.Name}) {
				out = append(out, ProxyChainReference{target.Kind, target.Name, hop})
			}
		}
	}
	return out, nil
}

func prepareChain(cfg map[string]any, kind string, targets, via []string, clear bool) (ProxyChainPlan, error) {
	p := ProxyChainPlan{Kind: kind, Targets: append([]string{}, targets...), Via: append([]string{}, via...), Changes: []ProxyChainChange{}, SharedUse: []ProxyChainReference{}, Conflicts: []string{}, Persistent: kind == "subscription"}
	if kind != "node" && kind != "provider" && kind != "subscription" {
		return p, i18n.Errorf("backend.profile.chain_chain_kind_must_be_node_provider_or_subscription", "chain kind must be node, provider or subscription")
	}
	if len(targets) == 0 {
		return p, i18n.Errorf("backend.profile.chain_chain_requires_a_target", "chain requires a target")
	}
	if clear && len(via) > 0 || !clear && len(via) == 0 {
		return p, i18n.Errorf("backend.profile.chain_chain_requires_ordered_via_nodes_or_clear_without_via", "chain requires ordered via nodes, or clear without via")
	}
	seen := map[string]bool{}
	for _, n := range via {
		if seen[n] {
			return p, i18n.Errorf("backend.profile.chain_chain_contains_a_repeated_hop", "chain contains a repeated hop")
		}
		seen[n] = true
		m := tsFind(cfg, n)
		if m == nil || !chainHop(tsString(m, "type")) {
			return p, i18n.Errorf("backend.profile.chain_chain_hops_must_be_compatible_static_top_level_nodes", "chain hops must be compatible static top-level nodes")
		}
	}
	changes := func(k, n, b, a string) {
		if b != a {
			p.Changes = append(p.Changes, ProxyChainChange{Kind: k, Name: n, Before: b, After: a})
		}
	}
	for i, n := range via {
		m := tsFind(cfg, n)
		before := tsString(m, "dialer-proxy")
		after := ""
		if i > 0 {
			after = via[i-1]
		}
		changes("node", n, before, after)
		m["dialer-proxy"] = after
	}
	targetSeen := map[string]bool{}
	for _, n := range targets {
		if targetSeen[n] {
			return p, i18n.Errorf("backend.profile.chain_chain_contains_repeated_targets", "chain contains repeated targets")
		}
		targetSeen[n] = true
		if seen[n] && kind == "node" {
			return p, i18n.Errorf("backend.profile.chain_chain_target_cannot_be_its_own_hop", "chain target cannot be its own hop")
		}
	}
	actual := targets
	if kind == "subscription" {
		if len(targets) != 1 || !validName(targets[0]) {
			return p, i18n.Errorf("backend.profile.chain_subscription_chain_requires_one_subscription_name", "subscription chain requires one subscription name")
		}
		actual = []string{}
		for _, t := range chainTargets(cfg) {
			if (t.Kind == "node" && chainSupported(t.Type) && !seen[t.Name]) || t.Kind == "provider" {
				actual = append(actual, t.Kind+":"+t.Name)
			}
		}
	}
	after := ""
	if len(via) > 0 {
		after = via[len(via)-1]
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	for _, target := range actual {
		k, n := kind, target
		if kind == "subscription" {
			k, n, _ = strings.Cut(target, ":")
		}
		before := ""
		if k == "node" {
			m := tsFind(cfg, n)
			if m == nil || !chainSupported(tsString(m, "type")) {
				return p, i18n.Errorf("backend.profile.chain_unsupported_or_missing_chain_target", "unsupported or missing chain target")
			}
			before = tsString(m, "dialer-proxy")
			m["dialer-proxy"] = after
		} else {
			m, _ := providers[n].(map[string]any)
			if m == nil {
				return p, i18n.Errorf("backend.profile.chain_provider_target_not_found", "provider target not found")
			}
			before = chainProviderDialer(m)
			configuredOverride, _ := m["override"].(map[string]any)
			_, hasOverride := configuredOverride["dialer-proxy"]
			if kind != "node" && !clear && before == "" && !hasOverride {
				payload, _ := m["payload"].([]any)
				for _, value := range payload {
					node, _ := value.(map[string]any)
					d := tsString(node, "dialer-proxy")
					if d != "" && d != after && !seen[d] {
						p.addConflict(i18n.Errorf("backend.profile.chain_provider_payload_external", "provider payload has an external front proxy"))
						break
					}
				}
			}
			o, _ := m["override"].(map[string]any)
			if o == nil {
				o = map[string]any{}
				m["override"] = o
			}
			o["dialer-proxy"] = after
		}
		if kind != "node" && !clear && before != "" && before != after && !seen[before] {
			p.addConflict(i18n.Errorf("backend.profile.chain_target_external", "%s %q has an external front proxy", k, n))
		}
		changes(k, n, before, after)
	}
	if err := chainValidateGraph(cfg); err != nil {
		return p, err
	}
	return p, nil
}
func (s *Store) PlanProxyChain(name, kind string, targets, via []string, clear bool) (ProxyChainPlan, error) {
	if kind == "subscription" {
		candidate, plan, err := s.subscriptionChainCandidate(name, targets, via, clear)
		if err != nil {
			return plan, err
		}
		cfg, activeErr := s.chainConfig(name)
		if activeErr != nil && !clear {
			return plan, activeErr
		}
		if activeErr == nil {
			plan.Changes = chainDifferences(chainTargets(cfg), chainTargets(candidate))
			shared, err := chainSharedUses(cfg, kind, targets, via)
			if err != nil {
				return plan, err
			}
			plan.SharedUse = shared
		}
		return plan, nil
	}
	cfg, err := s.chainConfig(name)
	if err != nil {
		return ProxyChainPlan{}, err
	}
	shared, err := chainSharedUses(cfg, kind, targets, via)
	if err != nil {
		return ProxyChainPlan{}, err
	}
	p, err := prepareChain(cfg, kind, targets, via, clear)
	if err != nil {
		return p, err
	}
	p.SharedUse = shared
	policy, err := s.SubscriptionChain(name)
	if err != nil {
		return p, err
	}
	if policy != nil && kind != "subscription" {
		for _, c := range p.Changes {
			if policyOwnsEdge(policy, c.Kind, c.Name) {
				p.addConflict(i18n.Errorf("backend.profile.chain_policy_owns_edge", "subscription policy owns a changed edge; clear the subscription chain first"))
			}
		}
	}
	return p, nil
}
func (s *Store) MutateProxyChain(ctx context.Context, name, kind string, targets, via []string, clear bool) (ProxyChainPlan, error) {
	p, err := s.PlanProxyChain(name, kind, targets, via, clear)
	if err != nil {
		return p, err
	}
	if len(p.Conflicts) > 0 {
		causes := []error{}
		for i, text := range p.Conflicts {
			if d, ok := p.Messages[fmt.Sprintf("/conflicts/%d", i)]; ok {
				causes = append(causes, i18n.MessageFromDescriptor(d, text, nil))
			} else {
				causes = append(causes, errors.New(text))
			}
		}
		return p, errors.Join(causes...)
	}
	if kind == "subscription" {
		return p, s.mutateSubscriptionChain(ctx, name, targets[0], via, clear)
	}
	cfg, err := s.chainConfig(name)
	if err != nil {
		return p, err
	}
	if _, err = prepareChain(cfg, kind, targets, via, clear); err != nil {
		return p, err
	}
	overlay, err := s.chainOverride(name)
	if err != nil {
		return p, err
	}
	if kind == "node" || len(via) > 0 {
		overlay["proxies"] = tsProxies(cfg)
		delete(overlay, proxyAdditionsKey)
	}
	if kind == "provider" {
		locals, _ := overlay["proxy-providers"].(map[string]any)
		if locals == nil {
			locals = map[string]any{}
		}
		for _, n := range targets {
			m, _ := locals[n].(map[string]any)
			if m == nil {
				m = map[string]any{}
			}
			o, _ := m["override"].(map[string]any)
			if o == nil {
				o = map[string]any{}
			}
			providers, _ := cfg["proxy-providers"].(map[string]any)
			effective, _ := providers[n].(map[string]any)
			o["dialer-proxy"] = chainProviderDialer(effective)
			m["override"] = o
			locals[n] = m
		}
		overlay["proxy-providers"] = locals
	}
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return p, err
	}
	return p, s.SetOverride(ctx, name, data)
}
func (s *Store) chainOverride(name string) (map[string]any, error) {
	out := map[string]any{}
	data, err := s.Override(name)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if yaml.Unmarshal(data, &out) != nil || out == nil {
		return nil, i18n.Errorf("backend.profile.chain_local_override_must_be_a_yaml_mapping", "local override must be a YAML mapping")
	}
	return out, nil
}

func (p ProxyChainPlan) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(p.Messages)
}
func (p *ProxyChainPlan) addConflict(message *i18n.Message) {
	if p.Messages == nil {
		p.Messages = map[string]i18n.Descriptor{}
	}
	p.Messages[fmt.Sprintf("/conflicts/%d", len(p.Conflicts))] = message.Descriptor()
	p.Conflicts = append(p.Conflicts, message.Error())
}
