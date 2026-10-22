package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

const orderSource = `mixed-port: 0
proxies: [{name: Leaf, type: direct}]
proxy-groups:
 - {name: A, type: select, proxies: [Leaf, DIRECT], icon: example-icon}
 - {name: B, type: fallback, proxies: [DIRECT, Leaf], url: 'http://127.0.0.1:9/check', interval: 300}
 - {name: C, type: select, proxies: [B, DIRECT], hidden: true}
rules: ['MATCH,A']
dns: {enable: false}
`

func orderNames(t *testing.T, s *Store) []string {
	t.Helper()
	groups, err := s.ProxyGroupConfigs("work")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}
func TestProxyGroupOrderPreservesDefinitionsSourceAndChoicesWithoutReload(t *testing.T) {
	s := tailscaleFixture(t, orderSource)
	ctx := context.Background()
	if err := s.SetOverride(ctx, "work", []byte("mode: global\nnagi-group-healthcheck-intervals: {B: 73}\n")); err != nil {
		t.Fatal(err)
	}
	before, err := s.tailscaleConfig("work")
	if err != nil {
		t.Fatal(err)
	}
	_, definitions, _ := namedList(before["proxy-groups"])
	choices := []byte(`{"work":{"A":"Leaf","C":"B"}}`)
	choicePath := filepath.Join(s.Dir, "proxy-selections.json")
	if err := os.WriteFile(choicePath, choices, 0600); err != nil {
		t.Fatal(err)
	}
	s.Validate = func([]byte) error { t.Fatal("display sorting launched a validator"); return nil }
	s.Reload = func(context.Context) error { t.Fatal("display sorting reloaded engine"); return nil }
	if err := s.OrderProxyGroups(ctx, "work", []string{"C", "A", "B"}); err != nil {
		t.Fatal(err)
	}
	if got := orderNames(t, s); !reflect.DeepEqual(got, []string{"C", "A", "B"}) {
		t.Fatal(got)
	}
	after, err := s.tailscaleConfig("work")
	if err != nil {
		t.Fatal(err)
	}
	_, updated, _ := namedList(after["proxy-groups"])
	if !reflect.DeepEqual(definitions, updated) {
		t.Fatal("group definitions changed")
	}
	delete(before, "proxy-groups")
	delete(after, "proxy-groups")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("other effective settings changed")
	}
	raw, _ := s.Show("work")
	if string(raw) != orderSource {
		t.Fatal("source changed")
	}
	saved, _ := os.ReadFile(choicePath)
	if !reflect.DeepEqual(choices, saved) {
		t.Fatal("selections changed")
	}
	overlayBytes, _ := s.Override("work")
	var overlay map[string]any
	_ = yaml.Unmarshal(overlayBytes, &overlay)
	if _, exists := overlay["proxy-groups"]; exists {
		t.Fatal("sort created a group definition snapshot")
	}
	if overlay["mode"] != "global" || overlay[groupHealthcheckIntervalsKey].(map[string]any)["B"] != 73 {
		t.Fatal(overlay)
	}
	info, _ := os.Stat(filepath.Join(s.Dir, "overrides", "work.yaml"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestProxyGroupOrderRejectsPartialDuplicateAndStaleGroups(t *testing.T) {
	for _, names := range [][]string{{"A", "B"}, {"A", "A", "C"}, {"A", "B", "Missing"}, {"A", "B", "C", "Missing"}, {"A", "B", ""}, {"A", "B", " C"}} {
		s := tailscaleFixture(t, orderSource)
		if err := s.OrderProxyGroups(context.Background(), "work", names); err == nil {
			t.Fatal("accepted", names)
		}
		if _, err := s.Override("work"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid order persisted", err)
		}
	}
}
func TestProxyGroupOrderRetainedThroughSubscriptionChanges(t *testing.T) {
	s := tailscaleFixture(t, orderSource)
	ctx := context.Background()
	if err := s.OrderProxyGroups(ctx, "work", []string{"C", "A", "B"}); err != nil {
		t.Fatal(err)
	}
	next := []byte(`proxies: [{name: Leaf, type: direct}]
proxy-groups:
 - {name: D, type: select, proxies: [DIRECT]}
 - {name: A, type: select, proxies: [DIRECT], icon: updated-icon}
 - {name: C, type: select, proxies: [A]}
rules: ['MATCH,A']
`)
	if err := s.ApplySubscription(ctx, "work", next); err != nil {
		t.Fatal(err)
	}
	if got := orderNames(t, s); !reflect.DeepEqual(got, []string{"C", "A", "D"}) {
		t.Fatal(got)
	}
	cfg, _ := s.tailscaleConfig("work")
	_, groups, _ := namedList(cfg["proxy-groups"])
	if groups["A"]["icon"] != "updated-icon" || !reflect.DeepEqual(groups["A"]["proxies"], []any{"DIRECT"}) {
		t.Fatal("sorting froze old definitions", groups)
	}
	if _, leaked := cfg[groupOrderKey]; leaked {
		t.Fatal("Nagi metadata leaked to engine")
	}
	// A stale submitted order must not delete the newly introduced group D.
	before, _ := s.Override("work")
	if err := s.OrderProxyGroups(ctx, "work", []string{"C", "A", "B"}); err == nil {
		t.Fatal("accepted stale draft")
	}
	after, _ := s.Override("work")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("stale draft changed override")
	}
}
func TestProxyGroupOrderMalformedMetadataRollsBack(t *testing.T) {
	s := tailscaleFixture(t, orderSource)
	ctx := context.Background()
	if err := s.OrderProxyGroups(ctx, "work", []string{"B", "A", "C"}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Override("work")
	for _, raw := range []string{"nagi-group-order: {}\n", "nagi-group-order: [A, A]\n", "nagi-group-order: [1]\n"} {
		if err := s.SetOverride(ctx, "work", []byte(raw)); err == nil {
			t.Fatal("accepted malformed order", raw)
		}
		after, _ := s.Override("work")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed metadata write was retained")
		}
	}
}

func TestProxyGroupOrderCancelledWriteLeavesExistingOrder(t *testing.T) {
	s := tailscaleFixture(t, orderSource)
	if err := s.OrderProxyGroups(context.Background(), "work", []string{"A", "C", "B"}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Override("work")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.OrderProxyGroups(ctx, "work", []string{"B", "A", "C"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := s.Override("work")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled write changed override")
	}
}
