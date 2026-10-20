package profile

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
	"gopkg.in/yaml.v3"
)

// ChainNodeRef identifies a top-level node or one effective node in a provider.
type ChainNodeRef struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
}

func (r ChainNodeRef) key() string {
	if r.Provider != "" {
		return (ProviderChainMember{Provider: r.Provider, Node: r.Name}).ID()
	}
	return r.Name
}
func (s *Store) applyProviderChainsData(name string, data []byte) ([]byte, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if _, ok := cfg[ProviderChainMembersKey]; !ok {
		return data, nil
	}
	if err := s.applyProviderChainMembers(name, cfg); err != nil {
		return nil, err
	}
	if err := chainValidateGraph(cfg); err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}
func (s *Store) savedProviderMembers(name string) ([]ProviderChainMember, error) {
	o, err := s.chainOverride(name)
	if err != nil {
		return nil, err
	}
	return providerChainMembers(o)
}

// localProviderNodes uses only local source/cache evidence. Inspection never downloads.
func (s *Store) localProviderNodes(source map[string]any) ([]map[string]any, error) {
	var values []any
	if tsString(source, "type") == "inline" {
		values, _ = source["payload"].([]any)
	} else {
		path := tsString(source, "path")
		if path == "" && tsString(source, "type") == "http" {
			path = filepath.Join("proxies", fmt.Sprintf("%x", md5.Sum([]byte(tsString(source, "url")))))
		}
		if path == "" {
			return nil, fmt.Errorf("provider nodes unavailable: no local payload or cache")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.Dir, "profiles", path)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("provider nodes unavailable: no readable local payload or cache")
		}
		if info.Size() > 32<<20 {
			return nil, fmt.Errorf("provider local payload is too large")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("provider nodes unavailable: local cache cannot be read")
		}
		var doc map[string]any
		if yaml.Unmarshal(data, &doc) != nil {
			return nil, fmt.Errorf("provider local payload cannot be inspected")
		}
		values, _ = doc["proxies"].([]any)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("provider nodes unavailable: local payload has no nodes")
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, value := range values {
		n, _ := value.(map[string]any)
		raw := tsString(n, "name")
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, n)
	}
	return out, nil
}
func (s *Store) resolveProviderMember(name string, cfg map[string]any, r ChainNodeRef) (ProviderChainMember, error) {
	providers, _ := cfg["proxy-providers"].(map[string]any)
	source, _ := providers[r.Provider].(map[string]any)
	if source == nil {
		return ProviderChainMember{}, fmt.Errorf("provider not found")
	}
	nodes, err := s.providerNodesForChainProfile(name, r.Provider, source)
	if err != nil {
		return ProviderChainMember{}, err
	}
	var found []ProviderChainMember
	override, _ := source["override"].(map[string]any)
	for _, n := range nodes {
		raw := tsString(n, "name")
		effective, err := providerNameAfterOverride(raw, override)
		if err != nil {
			return ProviderChainMember{}, err
		}
		if effective != r.Name {
			continue
		}
		m := ProviderChainMember{Provider: r.Provider, Node: r.Name, RawName: raw, Type: strings.ToLower(tsString(n, "type"))}
		if providerMemberAllowed(source, m) == nil {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		return ProviderChainMember{}, fmt.Errorf("provider node identity is missing or ambiguous in the local payload; refresh the provider and recreate the chain")
	}
	m := found[0]
	if !chainHop(m.Type) || m.Type == "tailscale" {
		return m, fmt.Errorf("provider node protocol is unsupported for isolated chains; provider Tailscale identities cannot be duplicated")
	}
	return m, nil
}
func (s *Store) ProviderChainInventory(name string) ([]ProxyChainTarget, []string, error) {
	cfg, err := s.chainLogicalConfig(name)
	if err != nil {
		return nil, nil, err
	}
	members, err := s.savedProviderMembers(name)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]ProviderChainMember{}
	for _, m := range members {
		byID[m.ID()] = m
	}
	names, err := s.ProxyNodeProviderCandidates(name)
	if err != nil {
		return nil, nil, err
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	out := []ProxyChainTarget{}
	unavailable := []string{}
	for _, p := range names {
		source, _ := providers[p].(map[string]any)
		nodes, e := s.providerNodesForChainProfile(name, p, source)
		if e != nil {
			unavailable = append(unavailable, p)
			continue
		}
		override, _ := source["override"].(map[string]any)
		seen := map[string]bool{}
		for _, n := range nodes {
			effective, e := providerNameAfterOverride(tsString(n, "name"), override)
			if e != nil {
				continue
			}
			r := ChainNodeRef{Kind: "provider-node", Provider: p, Name: effective}
			m := ProviderChainMember{Provider: p, Node: effective, RawName: tsString(n, "name"), Type: strings.ToLower(tsString(n, "type"))}
			if providerMemberAllowed(source, m) != nil || seen[effective] {
				continue
			}
			seen[effective] = true
			t := ProxyChainTarget{Kind: r.Kind, Name: r.Name, Provider: p, ID: r.key(), Type: m.Type, Via: []string{}, Available: true, Supported: chainHop(m.Type) && m.Type != "tailscale", PathRefs: []ChainNodeRef{r}}
			if saved, ok := byID[r.key()]; ok {
				t.Via = chainPath(cfg, saved.Dialer)
				t.PathRefs = chainPathRefs(cfg, saved.Dialer, members)
				t.PathRefs = append(t.PathRefs, r)
				if saved.Outlet {
					t.Outlet = saved.OutletName()
				}
			}
			if !t.Supported {
				t.UnsupportedReason = "provider node protocol is unsupported for isolated chains"
			}
			out = append(out, t)
		}
	}
	for _, m := range members {
		found := false
		for _, t := range out {
			if t.ID == m.ID() {
				found = true
			}
		}
		if found {
			continue
		}
		r := ChainNodeRef{Kind: "provider-node", Name: m.Node, Provider: m.Provider}
		path := chainPathRefs(cfg, m.Dialer, members)
		path = append(path, r)
		t := ProxyChainTarget{Kind: r.Kind, Name: r.Name, Provider: r.Provider, ID: m.ID(), Type: m.Type, Supported: true, Available: false, Via: []string{}, PathRefs: path, UnsupportedReason: "saved provider node is unavailable in its current source"}
		for _, x := range path[:len(path)-1] {
			t.Via = append(t.Via, x.Name)
		}
		if m.Outlet {
			t.Outlet = m.OutletName()
		}
		out = append(out, t)
	}
	return out, unavailable, nil
}
func chainPathRefs(cfg map[string]any, dialer string, members []ProviderChainMember) []ChainNodeRef {
	out := []ChainNodeRef{}
	seen := map[string]bool{}
	byID := map[string]ProviderChainMember{}
	for _, m := range members {
		byID[m.ID()] = m
	}
	for dialer != "" && !seen[dialer] {
		seen[dialer] = true
		r := ChainNodeRef{Kind: "node", Name: dialer}
		next := tsString(tsFind(cfg, dialer), "dialer-proxy")
		if m, ok := byID[dialer]; ok {
			r = ChainNodeRef{Kind: "provider-node", Name: m.Node, Provider: m.Provider}
			next = m.Dialer
		}
		out = append([]ChainNodeRef{r}, out...)
		dialer = next
	}
	return out
}
func (s *Store) PlanScopedProxyChain(name, kind string, targets []string, targetProvider string, refs []ChainNodeRef, clear bool) (ProxyChainPlan, error) {
	_, p, _, err := s.scopedChainCandidate(name, kind, targets, targetProvider, refs, clear)
	return p, err
}
func (s *Store) MutateScopedProxyChain(ctx context.Context, name, kind string, targets []string, targetProvider string, refs []ChainNodeRef, clear bool) (ProxyChainPlan, error) {
	overlay, p, _, err := s.scopedChainCandidate(name, kind, targets, targetProvider, refs, clear)
	if err != nil {
		return p, err
	}
	if len(p.Conflicts) > 0 {
		return p, fmt.Errorf("chain conflicts: %s", strings.Join(p.Conflicts, "; "))
	}
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return p, err
	}
	err = s.SetOverride(ctx, name, data)
	return p, err
}
func stripProviderResources(cfg map[string]any, members []ProviderChainMember) {
	providers, _ := cfg["proxy-providers"].(map[string]any)
	ids := map[string]bool{}
	for _, m := range members {
		delete(providers, m.providerID())
		ids[m.ID()] = true
		ids[m.OutletName()] = m.Outlet
	}
	groups, _ := cfg["proxy-groups"].([]any)
	out := []any{}
	for _, v := range groups {
		g, _ := v.(map[string]any)
		if ids[tsString(g, "name")] {
			continue
		}
		ps, _ := g["proxies"].([]any)
		kept := []any{}
		for _, p := range ps {
			n, _ := p.(string)
			if !ids[n] {
				kept = append(kept, p)
			}
		}
		if ps != nil {
			g["proxies"] = kept
		}
		out = append(out, g)
	}
	cfg["proxy-groups"] = out
}
func (s *Store) scopedChainCandidate(name, kind string, targets []string, targetProvider string, refs []ChainNodeRef, clear bool) (map[string]any, ProxyChainPlan, map[string]any, error) {
	p := ProxyChainPlan{PathChanges: []ProxyChainPathChange{}, Kind: kind, Targets: append([]string{}, targets...), Via: []string{}, Changes: []ProxyChainChange{}, SharedUse: []ProxyChainReference{}, Conflicts: []string{}}
	cfg, err := s.chainLogicalConfig(name)
	if err != nil {
		return nil, p, nil, err
	}
	members, err := s.savedProviderMembers(name)
	if err != nil {
		return nil, p, nil, err
	}
	beforePaths := logicalChainPaths(cfg, members)
	stripProviderResources(cfg, members)
	overlay, err := s.chainOverride(name)
	if err != nil {
		return nil, p, nil, err
	}
	if kind == "subscription" {
		return nil, p, nil, fmt.Errorf("provider-node hops cannot be retained by a subscription policy; use node or provider scope")
	}
	byID := map[string]int{}
	for i, m := range members {
		byID[m.ID()] = i
	}
	resolve := func(r ChainNodeRef) (string, error) {
		if r.Provider == "" {
			n := tsFind(cfg, r.Name)
			if n == nil || !chainHop(tsString(n, "type")) {
				return "", fmt.Errorf("chain hop must be a supported node")
			}
			return r.Name, nil
		}
		if _, ok := byID[r.key()]; !ok {
			m, e := s.resolveProviderMember(name, cfg, r)
			if e != nil {
				return "", e
			}
			byID[m.ID()] = len(members)
			members = append(members, m)
		}
		return r.key(), nil
	}
	via := []string{}
	seen := map[string]bool{}
	for _, r := range refs {
		id, e := resolve(r)
		if e != nil {
			return nil, p, nil, e
		}
		if seen[id] {
			return nil, p, nil, fmt.Errorf("chain contains a repeated hop")
		}
		seen[id] = true
		via = append(via, id)
		p.Via = append(p.Via, r.Name)
	}
	if !clear && len(via) == 0 || clear && len(via) > 0 {
		return nil, p, nil, fmt.Errorf("chain requires ordered via nodes or clear without via")
	}
	actualKind := kind
	actualTargets := append([]string{}, targets...)
	if kind == "provider-node" {
		if len(targets) != 1 || targetProvider == "" {
			return nil, p, nil, fmt.Errorf("provider-node requires one provider and node")
		}
		id, e := resolve(ChainNodeRef{Kind: kind, Provider: targetProvider, Name: targets[0]})
		if e != nil {
			return nil, p, nil, e
		}
		if seen[id] {
			return nil, p, nil, fmt.Errorf("chain target cannot be its own hop")
		}
		actualKind = "node"
		actualTargets = []string{id}
		i := byID[id]
		members[i].Outlet = !clear
		if !clear {
			p.Outlet = members[i].OutletName()
		}
	}
	// Synthetic nodes exist only while preparing logical dialer edges. Native generation
	// below replaces them with exact-provider bridges before graph validation.
	for _, m := range members {
		cfg["proxies"] = append(tsProxies(cfg), map[string]any{"name": m.ID(), "type": m.Type, "dialer-proxy": m.Dialer})
	}
	shared, err := chainSharedUses(cfg, actualKind, actualTargets, via)
	if err != nil {
		return nil, p, nil, err
	}
	p.SharedUse = shared
	basePlan, err := prepareChain(cfg, actualKind, actualTargets, via, clear)
	if err != nil {
		return nil, p, nil, err
	}
	p.Changes = basePlan.Changes
	p.Conflicts = basePlan.Conflicts
	p.Messages = basePlan.Messages
	policy, e := s.SubscriptionChain(name)
	if e != nil {
		return nil, p, nil, e
	}
	if policy != nil {
		if !clear && (kind == "provider-node" || lenProviderRefs(refs) > 0) {
			return nil, p, nil, i18n.Errorf("backend.profile.chain_provider_policy_incompatible", "provider-node chains cannot be combined with a subscription chain policy; clear the existing chain first")
		}
		for _, c := range p.Changes {
			if policyOwnsEdge(policy, c.Kind, c.Name) {
				return nil, p, nil, fmt.Errorf("subscription policy owns a changed edge; clear the subscription chain first")
			}
		}
	}
	for i, m := range members {
		members[i].Dialer = tsString(tsFind(cfg, m.ID()), "dialer-proxy")
	}
	nodes := []any{}
	for _, v := range tsProxies(cfg) {
		n, _ := v.(map[string]any)
		if _, ok := byID[tsString(n, "name")]; !ok {
			nodes = append(nodes, v)
		}
	}
	cfg["proxies"] = nodes
	// Remove only unreferenced provider members, retaining upstream segments in use.
	needed := map[string]bool{}
	for _, m := range members {
		if m.Outlet {
			needed[m.ID()] = true
		}
	}
	for _, v := range nodes {
		n, _ := v.(map[string]any)
		needed[tsString(n, "dialer-proxy")] = true
	}
	providersForRefs, _ := cfg["proxy-providers"].(map[string]any)
	for _, v := range providersForRefs {
		n, _ := v.(map[string]any)
		needed[chainProviderDialer(n)] = true
	}
	for changed := true; changed; {
		changed = false
		for _, m := range members {
			if needed[m.ID()] && m.Dialer != "" && !needed[m.Dialer] {
				needed[m.Dialer] = true
				changed = true
			}
		}
	}
	kept := []ProviderChainMember{}
	for _, m := range members {
		if needed[m.ID()] {
			kept = append(kept, m)
		}
	}
	members = kept
	cfg[ProviderChainMembersKey] = members
	if err = s.applyProviderChainMembers(name, cfg); err != nil {
		return nil, p, nil, err
	}
	if err = chainValidateGraph(cfg); err != nil {
		return nil, p, nil, err
	}
	overlay[ProviderChainMembersKey] = members
	overlay["proxies"] = nodes
	delete(overlay, proxyAdditionsKey)
	if kind == "provider" {
		locals, _ := overlay["proxy-providers"].(map[string]any)
		if locals == nil {
			locals = map[string]any{}
		}
		providers, _ := cfg["proxy-providers"].(map[string]any)
		for _, target := range targets {
			m, _ := locals[target].(map[string]any)
			if m == nil {
				m = map[string]any{}
			}
			o, _ := m["override"].(map[string]any)
			if o == nil {
				o = map[string]any{}
			}
			native, _ := providers[target].(map[string]any)
			o["dialer-proxy"] = chainProviderDialer(native)
			m["override"] = o
			locals[target] = m
		}
		overlay["proxy-providers"] = locals
	}
	for i, c := range p.Changes {
		if index, ok := byID[c.Name]; ok {
			_ = index
			for _, r := range refs {
				if r.key() == c.Name {
					p.Changes[i].Name = r.Name
					p.Changes[i].Provider = r.Provider
					p.Changes[i].Kind = "provider-node"
				}
			}
			if c.Name == (ProviderChainMember{Provider: targetProvider, Node: targets[0]}).ID() {
				p.Changes[i].Name = targets[0]
				p.Changes[i].Provider = targetProvider
				p.Changes[i].Kind = "provider-node"
			}
		}
	}
	if p.Outlet != "" {
		groups, _ := cfg["proxy-groups"].([]any)
		for _, v := range groups {
			g, _ := v.(map[string]any)
			ps, _ := g["proxies"].([]any)
			for _, x := range ps {
				if x == p.Outlet {
					p.OutletGroups = append(p.OutletGroups, tsString(g, "name"))
				}
			}
		}
	}
	afterPaths := logicalChainPaths(cfg, members)
	for _, b := range beforePaths {
		excluded := false
		for _, r := range refs {
			if r.Name == b.Name && r.Provider == b.Provider {
				excluded = true
			}
		}
		if b.Kind == kind && b.Provider == targetProvider {
			for _, t := range targets {
				if t == b.Name {
					excluded = true
				}
			}
		}
		if excluded {
			continue
		}
		for _, a := range afterPaths {
			if a.Kind == b.Kind && a.Name == b.Name && a.Provider == b.Provider && !reflect.DeepEqual(a.PathRefs, b.PathRefs) {
				p.PathChanges = append(p.PathChanges, ProxyChainPathChange{Kind: b.Kind, Name: b.Name, Provider: b.Provider, BeforeRefs: b.PathRefs, AfterRefs: a.PathRefs})
			}
		}
	}
	return overlay, p, cfg, nil
}

// chainLogicalConfig deliberately omits generated resources so inspection and
// disconnect remain available after a source changes its name mapping.
func (s *Store) chainLogicalConfig(name string) (map[string]any, error) {
	source, err := s.Show(name)
	if err != nil {
		return nil, err
	}
	o, err := s.chainOverride(name)
	if err != nil {
		return nil, err
	}
	delete(o, ProviderChainMembersKey)
	patch, err := yaml.Marshal(o)
	if err != nil {
		return nil, err
	}
	data, err := s.effectiveSourceWithOverride(name, source, patch, nil)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	err = yaml.Unmarshal(data, &cfg)
	return cfg, err
}

type ProxyChainPathChange struct {
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Provider   string         `json:"provider,omitempty"`
	BeforeRefs []ChainNodeRef `json:"before_refs"`
	AfterRefs  []ChainNodeRef `json:"after_refs"`
}

func logicalChainPaths(cfg map[string]any, members []ProviderChainMember) []ProxyChainTarget {
	result := []ProxyChainTarget{}
	for _, v := range tsProxies(cfg) {
		m, _ := v.(map[string]any)
		name := tsString(m, "name")
		refs := chainPathRefs(cfg, tsString(m, "dialer-proxy"), members)
		refs = append(refs, ChainNodeRef{Kind: "node", Name: name})
		result = append(result, ProxyChainTarget{Kind: "node", Name: name, PathRefs: refs})
	}
	for _, m := range members {
		if !m.Outlet {
			continue
		}
		refs := chainPathRefs(cfg, m.Dialer, members)
		refs = append(refs, ChainNodeRef{Kind: "provider-node", Name: m.Node, Provider: m.Provider})
		result = append(result, ProxyChainTarget{Kind: "provider-node", Name: m.Node, Provider: m.Provider, PathRefs: refs})
	}
	return result
}

func (s *Store) ProviderChainSources(name string) (map[string][]string, error) {
	members, err := s.savedProviderMembers(name)
	out := map[string][]string{}
	for _, m := range members {
		out[m.Provider] = append(out[m.Provider], m.providerID())
	}
	return out, err
}

func lenProviderRefs(refs []ChainNodeRef) int {
	n := 0
	for _, r := range refs {
		if r.Provider != "" {
			n++
		}
	}
	return n
}

// ProviderSourceHash binds identity evidence to the exact logical source definition.
// It contains no source URL or credentials.
func ProviderSourceHash(source map[string]any) string {
	data, _ := json.Marshal(source)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (s *Store) providerNodesForChainProfile(name, provider string, source map[string]any) ([]map[string]any, error) {
	if s.ProviderNodes != nil {
		nodes, err := s.ProviderNodes(name, provider, source)
		if err != nil || nodes != nil {
			return nodes, err
		}
	}
	return s.localProviderNodes(source)
}

// FilterProviderChainEvidence intersects raw source identities with active names
// without guessing the inverse of a potentially lossy regex rename.
func FilterProviderChainEvidence(source map[string]any, nodes []map[string]any, available map[string]bool) ([]map[string]any, error) {
	out := []map[string]any{}
	override, _ := source["override"].(map[string]any)
	for _, node := range nodes {
		name, err := providerNameAfterOverride(tsString(node, "name"), override)
		if err != nil {
			return nil, err
		}
		if available[name] {
			out = append(out, node)
		}
	}
	return out, nil
}
