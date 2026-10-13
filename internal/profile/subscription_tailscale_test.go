package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func durableStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	if err := s.Write(context.Background(), "work", []byte("proxies:\n - {name: A, type: socks5, server: a.example, port: 1080}\nproxy-providers:\n feed: {type: inline, payload: [{name: B, type: socks5, server: b.example, port: 1080}]}\n")); err != nil {
		t.Fatal(err)
	}
	key, exit := "test-key", "100.64.0.1"
	if err := s.MutateTailscale(context.Background(), "work", "add", "TS", "", "", TailscaleOptions{AuthKey: &key, ExitNode: &exit}); err != nil {
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
func TestSubscriptionTailscaleRefreshReapplyEditAndUnbind(t *testing.T) {
	ctx := context.Background()
	s := durableStore(t)
	if err := s.MutateSubscriptionTailscale(ctx, "work", "work", "bind", "TS"); err != nil {
		t.Fatal(err)
	}
	initial := effectiveDoc(t, s)
	identity := tsString(tsFind(initial, "TS"), "state-dir")
	next := []byte("proxies:\n - {name: C, type: socks5, server: c.example, port: 1080}\nproxy-providers:\n feed: {type: inline, payload: [{name: D, type: socks5, server: d.example, port: 1080}]}\n")
	// Scheduled preserving-source apply and explicit destructive apply both regenerate inheritance.
	if err := s.Apply(ctx, "work", next); err != nil {
		t.Fatal(err)
	}
	cfg := effectiveDoc(t, s)
	if tsFind(cfg, "A") != nil || tsString(tsFind(cfg, "C"), "dialer-proxy") != "TS" {
		t.Fatalf("stale/new nodes: %#v", cfg)
	}
	provider := cfg["proxy-providers"].(map[string]any)["feed"].(map[string]any)
	dialer, _ := tsProviderDialer(provider)
	if dialer != "TS" {
		t.Fatalf("provider dialer %q", dialer)
	}
	exit, key := "100.64.0.2", "replacement-key"
	if err := s.MutateTailscale(ctx, "work", "edit", "TS", "", "", TailscaleOptions{ExitNode: &exit, AuthKey: &key}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubscription(ctx, "work", next); err != nil {
		t.Fatal(err)
	}
	cfg = effectiveDoc(t, s)
	node := tsFind(cfg, "TS")
	if tsString(node, "auth-key") != key || tsString(node, "exit-node") != exit || tsString(node, "state-dir") != identity {
		t.Fatalf("definition lost: %#v", node)
	}
	if err := s.MutateSubscriptionTailscale(ctx, "work", "work", "unbind", ""); err != nil {
		t.Fatal(err)
	}
	cfg = effectiveDoc(t, s)
	if tsString(tsFind(cfg, "C"), "dialer-proxy") != "" || tsString(tsFind(cfg, "TS"), "auth-key") != key {
		t.Fatalf("unbind changed source/exit: %#v", cfg)
	}
	provider = cfg["proxy-providers"].(map[string]any)["feed"].(map[string]any)
	dialer, _ = tsProviderDialer(provider)
	if dialer != "" {
		t.Fatalf("unbind provider %q", dialer)
	}
}
func TestSubscriptionTailscaleRollbackAndConflicts(t *testing.T) {
	ctx := context.Background()
	s := durableStore(t)
	before, _ := s.Override("work")
	s.Reload = func(context.Context) error { return errors.New("activation failed") }
	// selected work is required for reload test
	if err := os.WriteFile(filepath.Join(s.Dir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.MutateSubscriptionTailscale(ctx, "work", "work", "bind", "TS"); err == nil {
		t.Fatal("expected rollback")
	}
	after, _ := s.Override("work")
	policy, _ := s.SubscriptionTailscale("work")
	if string(before) != string(after) || policy != nil {
		t.Fatal("rollback did not restore override/policy")
	}
	s.Reload = nil
	if err := s.MutateSubscriptionTailscale(ctx, "work", "work", "bind", "TS"); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"proxies: [{name: TS, type: socks5}]",
		"proxies: [{name: A, type: socks5, dialer-proxy: OTHER}]",
		"proxy-providers: {feed: {type: inline, payload: [{name: X, type: socks5, dialer-proxy: OTHER}]}}",
		"proxy-groups: [{name: TS, type: select, proxies: [DIRECT]}]",
	}
	for _, source := range cases {
		if _, err := s.EffectiveSource("work", []byte(source)); err == nil {
			t.Fatalf("expected conflict for %s", source)
		}
	}
	if err := s.MutateTailscaleBindings(ctx, "work", "unbind", "", "node", []string{"A"}); err == nil {
		t.Fatal("individual binding should require detach")
	}
}
func TestSubscriptionTailscaleRejectsArbitrarySnapshot(t *testing.T) {
	s := durableStore(t)
	data, _ := s.Override("work")
	data = []byte(strings.Replace(string(data), "a.example", "changed.example", 1))
	if err := s.SetOverride(context.Background(), "work", data); err != nil {
		t.Fatal(err)
	}
	if err := s.MutateSubscriptionTailscale(context.Background(), "work", "work", "bind", "TS"); err == nil {
		t.Fatal("must not discard arbitrary local node edits")
	}
}
