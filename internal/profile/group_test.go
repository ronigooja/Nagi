package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const groupSource = `mixed-port: 17890
proxies:
 - {name: Alpha, type: socks5, server: 127.0.0.1, port: 1080}
 - {name: Beta, type: direct}
proxy-providers:
 feed: {type: inline, payload: [{name: Remote, type: direct}]}
proxy-groups:
 - {name: Original, type: select, proxies: [Alpha, DIRECT], hidden: true, filter: '^Alpha$'}
rules: ['MATCH,DIRECT']
dns: {enable: false}
`

func TestProxyGroupCRUDPreservesSourceAndUnrelatedSettings(t *testing.T) {
	ctx := context.Background()
	s := tailscaleFixture(t, groupSource)
	if err := s.SetOverride(ctx, "work", []byte("mode: global\ndns: {enable: false}\n")); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"select", "url-test", "fallback", "load-balance"} {
		patch := map[string]any{"type": kind, "proxies": []any{"Beta", "Alpha"}, "use": []any{"feed"}}
		if kind == "url-test" {
			patch["tolerance"] = 50
		}
		if kind == "load-balance" {
			patch["strategy"] = "round-robin"
		}
		if err := s.SaveProxyGroup(ctx, "work", kind, patch, true); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := s.ProxyGroupConfigs("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 5 || groups[0].Name != "Original" {
		t.Fatal(groups)
	}
	for _, g := range groups[1:] {
		if !reflect.DeepEqual(g.Proxies, []string{"Beta", "Alpha"}) || !reflect.DeepEqual(g.Use, []string{"feed"}) {
			t.Fatal(g)
		}
		if g.Type != "select" && (g.Interval != 300 || g.URL != "https://cp.cloudflare.com/generate_204") {
			t.Fatal(g)
		}
	}
	if err := s.SaveProxyGroup(ctx, "work", "Original", map[string]any{"proxies": []any{"Beta", "DIRECT"}}, false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.tailscaleConfig("work")
	first := cfg["proxy-groups"].([]any)[0].(map[string]any)
	if first["hidden"] != true || first["filter"] != "^Alpha$" || cfg["mode"] != "global" {
		t.Fatal(cfg)
	}
	if err := s.SaveProxyGroup(ctx, "work", "url-test", map[string]any{"type": "load-balance", "strategy": "sticky-sessions"}, false); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.tailscaleConfig("work")
	changed := cfg["proxy-groups"].([]any)[2].(map[string]any)
	if _, ok := changed["tolerance"]; ok {
		t.Fatal(changed)
	}
	if err := s.RemoveProxyGroup(ctx, "work", "Original"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.tailscaleConfig("work")
	if len(cfg["proxy-groups"].([]any)) != 4 || len(tsProxies(cfg)) != 2 {
		t.Fatal(cfg)
	}
	source, _ := s.Show("work")
	if string(source) != groupSource {
		t.Fatal("source rewritten")
	}
	info, _ := os.Stat(filepath.Join(s.Dir, "overrides", "work.yaml"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestProxyGroupRejectsConflictsUnknownMembersAndCycles(t *testing.T) {
	for _, test := range []struct {
		name  string
		patch map[string]any
	}{
		{"Original", map[string]any{}}, {"Alpha", map[string]any{}}, {"feed", map[string]any{}}, {"GLOBAL", map[string]any{}},
		{"New", map[string]any{"type": "relay"}}, {"New", map[string]any{"proxies": []any{"Missing"}}},
		{"New", map[string]any{"proxies": []any{"New"}}}, {"New", map[string]any{"use": []any{"Missing"}}},
		{"New", map[string]any{"proxies": []any{}}}, {"New", map[string]any{"interval": 0}},
		{"New", map[string]any{"type": "url-test", "tolerance": 65536}},
		{"New", map[string]any{"strategy": "round-robin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := tailscaleFixture(t, groupSource)
			if err := s.SaveProxyGroup(context.Background(), "work", test.name, test.patch, true); err == nil {
				t.Fatal("accepted invalid group")
			}
			if _, err := s.Override("work"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid operation persisted", err)
			}
		})
	}
	s := tailscaleFixture(t, groupSource)
	ctx := context.Background()
	if err := s.SaveProxyGroup(ctx, "work", "Nested", map[string]any{"proxies": []any{"Original"}}, true); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Override("work")
	if err := s.SaveProxyGroup(ctx, "work", "Original", map[string]any{"proxies": []any{"Nested"}}, false); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal(err)
	}
	after, _ := s.Override("work")
	if string(before) != string(after) {
		t.Fatal("cycle persisted")
	}
}
func TestProxyGroupRemovalDependencies(t *testing.T) {
	for _, suffix := range []string{
		"rules: ['MATCH,Original']\n", "dns: {nameserver: ['https://example.invalid/dns-query#Original']}\n",
		"proxy-groups: [{name: Original, type: select, proxies: [DIRECT]}, {name: Parent, type: select, proxies: [Original]}]\n",
		"proxy-providers: {feed: {type: http, url: 'https://example.invalid/feed', proxy: Original}}\n",
		"proxies: [{name: Child, type: socks5, dialer-proxy: Original}]\n",
		"listeners: [{name: entry, type: mixed, proxy: Original}]\n", "tunnels: [{proxy: Original}]\n",
	} {
		t.Run(strings.Split(suffix, ":")[0], func(t *testing.T) {
			s := tailscaleFixture(t, "proxy-groups: [{name: Original, type: select, proxies: [DIRECT]}]\n")
			if err := s.SetOverride(context.Background(), "work", []byte(suffix)); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Override("work")
			if err := s.RemoveProxyGroup(context.Background(), "work", "Original"); err == nil || !strings.Contains(err.Error(), "referenced") {
				t.Fatal(err)
			}
			after, _ := s.Override("work")
			if string(before) != string(after) {
				t.Fatal("reference rejection changed override")
			}
		})
	}
}
func TestProxyGroupValidationAndReloadFailureRestoreOverride(t *testing.T) {
	for _, stage := range []string{"validation", "reload"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			s := tailscaleFixture(t, groupSource)
			if err := s.SaveProxyGroup(ctx, "work", "New", map[string]any{}, true); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Override("work")
			calls := 0
			if stage == "validation" {
				s.Validate = func([]byte) error { return errors.New("validation failed") }
				s.Reload = func(context.Context) error { t.Fatal("reload after invalid configuration"); return nil }
			} else {
				s.Reload = func(context.Context) error { calls++; return errors.New("reload failed") }
			}
			for _, operation := range []func() error{
				func() error { return s.SaveProxyGroup(ctx, "work", "Another", map[string]any{}, true) },
				func() error {
					return s.SaveProxyGroup(ctx, "work", "New", map[string]any{"proxies": []any{"Alpha"}}, false)
				},
				func() error { return s.RemoveProxyGroup(ctx, "work", "New") },
			} {
				if err := operation(); err == nil {
					t.Fatal("failure accepted")
				}
				after, _ := s.Override("work")
				if string(before) != string(after) {
					t.Fatal("failed operation changed override")
				}
			}
			if stage == "reload" && calls != 3 {
				t.Fatal(calls)
			}
		})
	}
}

func TestProxyGroupEditingReconcilesExplicitHealthcheckIntent(t *testing.T) {
	ctx := context.Background()
	s := tailscaleFixture(t, groupSource)
	oldURL := "https://example.invalid/old"
	interval := 17
	if err := s.SetHealthcheckURL(ctx, "work", "group", "Original", &oldURL); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHealthcheck(ctx, "work", "group", "Original", &interval); err != nil {
		t.Fatal(err)
	}
	patch := map[string]any{"type": "url-test", "url": "https://example.invalid/new", "interval": 27}
	if err := s.SaveProxyGroup(ctx, "work", "Original", patch, false); err != nil {
		t.Fatal(err)
	}
	groups, err := s.ProxyGroupConfigs("work")
	if err != nil {
		t.Fatal(err)
	}
	if groups[0].URL != patch["url"] || groups[0].Interval != 27 {
		t.Fatalf("stale healthcheck metadata won: %+v", groups[0])
	}
	if err := s.RemoveProxyGroup(ctx, "work", "Original"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProxyGroup(ctx, "work", "Original", map[string]any{"type": "url-test"}, true); err != nil {
		t.Fatal(err)
	}
	groups, err = s.ProxyGroupConfigs("work")
	if err != nil {
		t.Fatal(err)
	}
	if groups[0].URL != "https://cp.cloudflare.com/generate_204" || groups[0].Interval != 300 {
		t.Fatal("recreation inherited removed group's settings", groups[0])
	}
}

func TestProxyGroupMalformedExistingListsAreNotOverwritten(t *testing.T) {
	for _, groups := range []string{"{}", "[broken]", "[{name: Duplicate, type: select}, {name: Duplicate, type: select}]"} {
		s := tailscaleFixture(t, "proxy-groups: "+groups+"\n")
		if _, err := s.ProxyGroupConfigs("work"); err == nil {
			t.Fatal("read accepted malformed groups", groups)
		}
		if err := s.SaveProxyGroup(context.Background(), "work", "New", map[string]any{}, true); err == nil {
			t.Fatal("overwrote malformed groups", groups)
		}
		if _, err := s.Override("work"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("persisted override", err)
		}
	}
}

func TestProxyGroupEditRetainsNativeAutomaticInclusion(t *testing.T) {
	s := tailscaleFixture(t, "proxies: [{name: Leaf, type: direct}]\nproxy-groups: [{name: Included, type: select, include-all-proxies: 'true'}]\n")
	if err := s.SaveProxyGroup(context.Background(), "work", "Included", map[string]any{"type": "url-test", "url": "https://example.invalid/check"}, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.tailscaleConfig("work")
	if err != nil {
		t.Fatal(err)
	}
	group := cfg["proxy-groups"].([]any)[0].(map[string]any)
	if group["include-all-proxies"] != "true" || group["proxies"] != nil {
		t.Fatal("automatic inclusion changed", group)
	}
}
