package profile

import (
	"context"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func durableStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	data := []byte("proxies:\n - {name: A, type: socks5, server: a.example, port: 1080}\n - {name: B, type: wireguard, server: b.example, port: 51820}\n - {name: C, type: socks5, server: c.example, port: 1080}\n - {name: Other, type: socks5, server: other.example, port: 1080}\nproxy-providers:\n feed: {type: inline, payload: [{name: FeedNode, type: socks5, server: feed.example, port: 1080}]}\n")
	if err := s.Write(context.Background(), "work", data); err != nil {
		t.Fatal(err)
	}
	return s
}
func effectiveDoc(t *testing.T, s *Store) map[string]any {
	t.Helper()
	data, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func TestChainOrderedDryRunAndClear(t *testing.T) {
	s := durableStore(t)
	ctx := context.Background()
	before, err := s.Show("work")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PlanProxyChain("work", "node", []string{"C"}, []string{"A", "B"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 2 {
		t.Fatalf("changes %#v", p)
	}
	if _, err = s.Override("work"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dryrun wrote override: %v", err)
	}
	if _, err = s.MutateProxyChain(ctx, "work", "node", []string{"C"}, []string{"A", "B"}, false); err != nil {
		t.Fatal(err)
	}
	cfg := effectiveDoc(t, s)
	if tsString(tsFind(cfg, "A"), "dialer-proxy") != "" || tsString(tsFind(cfg, "B"), "dialer-proxy") != "A" || tsString(tsFind(cfg, "C"), "dialer-proxy") != "B" {
		t.Fatal("wrong order")
	}
	if _, err = s.MutateProxyChain(ctx, "work", "node", []string{"C"}, nil, true); err != nil {
		t.Fatal(err)
	}
	cfg = effectiveDoc(t, s)
	if tsString(tsFind(cfg, "B"), "dialer-proxy") != "A" || tsString(tsFind(cfg, "C"), "dialer-proxy") != "" {
		t.Fatal("clear changed a shared hop")
	}
	after, _ := s.Show("work")
	if string(before) != string(after) {
		t.Fatal("source changed")
	}
}
func TestChainValidationConflictsAndSharedUse(t *testing.T) {
	s := durableStore(t)
	ctx := context.Background()
	if _, err := s.MutateProxyChain(ctx, "work", "node", []string{"Other"}, []string{"A"}, false); err != nil {
		t.Fatal(err)
	}
	p, err := s.PlanProxyChain("work", "node", []string{"C"}, []string{"A", "B"}, false)
	if err != nil || len(p.SharedUse) == 0 {
		t.Fatalf("shared uses %#v %v", p, err)
	}
	for _, via := range [][]string{{"A", "A"}, {"C"}, {"missing"}} {
		if _, err = s.PlanProxyChain("work", "node", []string{"C"}, via, false); err == nil {
			t.Fatal("invalid path accepted")
		}
	}
	cfg := effectiveDoc(t, s)
	providers := cfg["proxy-providers"].(map[string]any)
	provider := providers["feed"].(map[string]any)
	provider["override"] = map[string]any{"dialer-proxy": "Other"}
	overlay := map[string]any{"proxy-providers": providers}
	data, _ := yaml.Marshal(overlay)
	if err = s.SetOverride(ctx, "work", data); err != nil {
		t.Fatal(err)
	}
	p, err = s.PlanProxyChain("work", "provider", []string{"feed"}, []string{"A"}, false)
	if err != nil || len(p.Conflicts) == 0 {
		t.Fatalf("conflict %#v %v", p, err)
	}
	if _, err = s.MutateProxyChain(ctx, "work", "provider", []string{"feed"}, []string{"A"}, false); err == nil {
		t.Fatal("external binding replaced")
	}
}
func TestSubscriptionChainReplayDetachAndSensitiveStorage(t *testing.T) {
	s := durableStore(t)
	ctx := context.Background()
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, []string{"A", "B"}, false); err != nil {
		t.Fatal(err)
	}
	path, _ := s.subscriptionChainPath("work")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private policy %v %v", info, err)
	}
	next := []byte("proxies: [{name: Fresh, type: socks5, server: fresh.example, password: PRIVATE_VALUE}]\nproxy-providers: {new: {type: inline, payload: []}}\n")
	if err = s.ApplySubscription(ctx, "work", next); err != nil {
		t.Fatal(err)
	}
	cfg := effectiveDoc(t, s)
	if tsString(tsFind(cfg, "Fresh"), "dialer-proxy") != "B" || tsString(tsFind(cfg, "B"), "dialer-proxy") != "A" {
		t.Fatal("policy not replayed")
	}
	p, err := s.PlanProxyChain("work", "node", []string{"Fresh"}, nil, true)
	if err != nil || len(p.Conflicts) == 0 {
		t.Fatalf("policy edge not protected %#v %v", p, err)
	}
	encoded, _ := json.Marshal(p)
	if strings.Contains(string(encoded), "PRIVATE_VALUE") {
		t.Fatal("secret in plan")
	}
	if _, err = s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, nil, true); err != nil {
		t.Fatal(err)
	}
	cfg = effectiveDoc(t, s)
	if tsString(tsFind(cfg, "Fresh"), "dialer-proxy") != "" || tsString(tsFind(cfg, "B"), "dialer-proxy") != "A" {
		t.Fatal("detach did not preserve local chain")
	}
	if err = s.ApplySubscription(ctx, "work", []byte("proxies: [{name: Later, type: socks5}]\n")); err != nil {
		t.Fatal(err)
	}
	if tsFind(effectiveDoc(t, s), "B") == nil {
		t.Fatal("detached node lost on apply")
	}
}
func TestSubscriptionChainLegacyAndCollision(t *testing.T) {
	s := durableStore(t)
	legacy, _ := s.subscriptionTailscalePath("work")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("subscription: work\nnode: {name: TS, type: tailscale}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Effective("work"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("legacy policy ignored %v", err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(context.Background(), "work", "subscription", []string{"work"}, []string{"A"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubscription(context.Background(), "work", []byte("proxies: [{name: A, type: socks5, server: collision.example}]\n")); err == nil {
		t.Fatal("source collision accepted")
	}
}
func TestChainGraphAutomaticIncludes(t *testing.T) {
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte("proxies: [{name: A, type: socks5, dialer-proxy: G}]\nproxy-groups: [{name: G, type: select, include-all-proxies: true}]\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := chainValidateGraph(cfg); err == nil {
		t.Fatal("automatic group cycle accepted")
	}
	groups := cfg["proxy-groups"].([]any)
	g := groups[0].(map[string]any)
	g["exclude-filter"] = "^A$"
	if err := chainValidateGraph(cfg); err != nil {
		t.Fatalf("excluded node falsely cycles: %v", err)
	}
	delete(g, "exclude-filter")
	g["exclude-type"] = "SOCKS5"
	if err := chainValidateGraph(cfg); err != nil {
		t.Fatal(err)
	}
}
func TestSubscriptionChainValidationRollback(t *testing.T) {
	s := durableStore(t)
	s.Validate = func([]byte) error { return errors.New("candidate invalid") }
	if _, err := s.MutateProxyChain(context.Background(), "work", "subscription", []string{"work"}, []string{"A"}, false); err == nil {
		t.Fatal("invalid candidate saved")
	}
	p, err := s.SubscriptionChain("work")
	if err != nil || p != nil {
		t.Fatalf("policy not rolled back %#v %v", p, err)
	}
}

func TestSubscriptionChainSwitchAndClearPlan(t *testing.T) {
	s := durableStore(t)
	ctx := context.Background()
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, []string{"A", "B"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, []string{"Other"}, false); err != nil {
		t.Fatal(err)
	}
	cfg := effectiveDoc(t, s)
	for _, n := range []string{"A", "B", "C"} {
		if tsString(tsFind(cfg, n), "dialer-proxy") != "Other" {
			t.Fatalf("old/ordinary node %s not covered by new policy", n)
		}
	}
	p, err := s.PlanProxyChain("work", "subscription", []string{"work"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, nil, true); err != nil {
		t.Fatal(err)
	}
	cfg = effectiveDoc(t, s)
	for _, c := range p.Changes {
		if c.Kind == "node" && tsString(tsFind(cfg, c.Name), "dialer-proxy") != c.After {
			t.Fatalf("plan and actual differ: %#v", c)
		}
	}
	if err = s.ApplySubscription(ctx, "work", []byte("proxies: [{name: Next, type: vmess}]\n")); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"A", "B", "Other"} {
		if tsFind(effectiveDoc(t, s), n) == nil {
			t.Fatalf("retained hop %s lost", n)
		}
	}
}
func TestSubscriptionChainClearRepairsSourceCollision(t *testing.T) {
	s := durableStore(t)
	ctx := context.Background()
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, []string{"A"}, false); err != nil {
		t.Fatal(err)
	}
	path, _ := s.path("work")
	if err := os.WriteFile(path, []byte("proxies: [{name: A, type: socks5, server: collision.example}]"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, nil, true); err != nil {
		t.Fatal(err)
	}
}

func TestChainGraphProviderNamesAndSharedGroups(t *testing.T) {
	s := durableStore(t)
	source, _ := s.Show("work")
	source = append(source, []byte("proxy-groups: [{name: G, type: select, proxies: [A]}]\n")...)
	if err := s.Write(context.Background(), "work", source); err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanProxyChain("work", "node", []string{"C"}, []string{"A", "B"}, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, use := range plan.SharedUse {
		if use.Kind == "group" && use.Name == "G" && use.Via == "A" {
			found = true
		}
	}
	if !found {
		t.Fatal("shared group omitted")
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte("proxies: [{name: 'provider:Feed', type: socks5}]\nproxy-providers: {Feed: {type: inline, override: {dialer-proxy: 'provider:Feed'}, payload: []}}"), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := chainValidateGraph(cfg); err != nil {
		t.Fatalf("provider namespace collision: %v", err)
	}
}

func TestSubscriptionChainSwitchReplacesAdoptedSourceEdge(t *testing.T) {
	s := durableStore(t)
	ctx := context.Background()
	source, _ := s.Show("work")
	var cfg map[string]any
	if err := yaml.Unmarshal(source, &cfg); err != nil {
		t.Fatal(err)
	}
	tsFind(cfg, "C")["dialer-proxy"] = "A"
	data, _ := yaml.Marshal(cfg)
	if err := s.Write(ctx, "work", data); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, []string{"A"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, []string{"B"}, false); err != nil {
		t.Fatal(err)
	}
	if tsString(tsFind(effectiveDoc(t, s), "C"), "dialer-proxy") != "B" {
		t.Fatal("adopted source edge not replaced")
	}
	if err := s.ApplySubscription(ctx, "work", data); err != nil {
		t.Fatal(err)
	}
	if tsString(tsFind(effectiveDoc(t, s), "C"), "dialer-proxy") != "B" {
		t.Fatal("adopted edge not replayed")
	}
	plan, err := s.PlanProxyChain("work", "subscription", []string{"work"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MutateProxyChain(ctx, "work", "subscription", []string{"work"}, nil, true); err != nil {
		t.Fatal(err)
	}
	if tsString(tsFind(effectiveDoc(t, s), "C"), "dialer-proxy") != "A" {
		t.Fatal("clear did not restore source edge")
	}
	found := false
	for _, c := range plan.Changes {
		if c.Kind == "node" && c.Name == "C" && c.After == "A" {
			found = true
		}
	}
	if !found {
		t.Fatal("clear plan omitted restored source edge")
	}
}
