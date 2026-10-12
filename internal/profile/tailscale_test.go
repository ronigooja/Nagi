package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

func tailscaleFixture(t *testing.T, source string) *Store {
	t.Helper()
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	if err := os.MkdirAll(filepath.Join(s.Dir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "profiles", "work.yaml"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return s
}
func testKey(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func ptr(value string) *string { return &value }

const tsSource = `proxies:
  - {name: Alpha, type: socks5, server: 127.0.0.1, port: 1080}
  - {name: Local, type: direct}
proxy-groups:
  - {name: Group, type: select, proxies: [Alpha]}
proxy-providers:
  remote: {type: http, url: 'https://example.com/feed'}
  local: {type: inline, payload: [{name: Inner, type: socks5, server: 127.0.0.1, port: 1081}]}
rules: [MATCH,DIRECT]
`

func TestTailscaleProfileLifecycleAndOverride(t *testing.T) {
	s := tailscaleFixture(t, tsSource)
	ctx := context.Background()
	key := testKey(t, "tskey-PRIVATE\n")
	source, _ := s.Show("work")
	if err := s.MutateTailscale(ctx, "work", "add", "My Tail", "", "", TailscaleOptions{ExitNode: ptr("exit"), AuthKeyFile: &key}); err != nil {
		t.Fatal(err)
	}
	nodes, err := s.TailscaleNodes("work")
	if err != nil || len(nodes) != 1 || nodes[0].ControlURL != nagiruntime.TailscaleControlURL || !nodes[0].AuthKeyConfigured {
		t.Fatalf("nodes=%+v err=%v", nodes, err)
	}
	if err := s.MutateTailscale(ctx, "work", "edit", "My Tail", "", "", TailscaleOptions{Hostname: ptr("work-device")}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.tailscaleConfig("work")
	node := tsFind(cfg, "My Tail")
	if node["auth-key"] != "tskey-PRIVATE" || node["exit-node"] != "exit" {
		t.Fatal("edit lost retained fields")
	}
	state := tsString(node, "state-dir")
	if !strings.HasPrefix(state, filepath.Join(s.Dir, ".tailscale-state")) {
		t.Fatal(state)
	}
	for _, kind := range []string{"node", "provider"} {
		target := "Alpha"
		if kind == "provider" {
			target = "remote"
		}
		if err := s.MutateTailscale(ctx, "work", "bind", "My Tail", kind, target, TailscaleOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	_, bindings, err := s.TailscaleShow("work", "My Tail")
	if err != nil || len(bindings) != 2 {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}
	if err := s.MutateTailscale(ctx, "work", "remove", "My Tail", "", "", TailscaleOptions{}); err == nil {
		t.Fatal("removed bound node")
	}
	for _, kind := range []string{"node", "provider"} {
		target := "Alpha"
		if kind == "provider" {
			target = "remote"
		}
		if err := s.MutateTailscale(ctx, "work", "unbind", "", kind, target, TailscaleOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MutateTailscale(ctx, "work", "remove", "My Tail", "", "", TailscaleOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatal("identity state removed")
	}
	after, _ := s.Show("work")
	if string(after) != string(source) {
		t.Fatal("source modified")
	}
	override, _ := s.Override("work")
	var local map[string]any
	if yaml.Unmarshal(override, &local) != nil {
		t.Fatal("bad override")
	}
	if len(local["proxies"].([]any)) != 2 {
		t.Fatal("full proxies snapshot not retained")
	}
	providers := local["proxy-providers"].(map[string]any)
	remote := providers["remote"].(map[string]any)
	if remote["override"].(map[string]any)["dialer-proxy"] != "" {
		t.Fatal("provider unbind did not suppress inheritance")
	}
	if remote["url"] != nil {
		t.Fatal("copied provider source URL")
	}
}
func TestTailscaleOfflineTargetsAndStateRewrite(t *testing.T) {
	s := tailscaleFixture(t, tsSource+"\n")
	cfg, _ := s.tailscaleConfig("work")
	cfg["proxies"] = append(tsProxies(cfg), map[string]any{"name": "Tail", "type": "tailscale", "state-dir": "/unsafe/source", "exit-node": "exit"})
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(s.Dir, "profiles", "work.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	effective, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(effective), "/unsafe/source") {
		t.Fatal("raw state path retained")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ".tailscale-state")); !os.IsNotExist(err) {
		t.Fatal("read-only query created identity state")
	}
	targets, err := s.TailscaleTargets("work")
	if err != nil || len(targets) != 3 {
		t.Fatalf("targets=%+v err=%v", targets, err)
	}
	for _, target := range targets {
		if target.Name == "Tail" || target.Name == "Local" || target.Name == "Group" {
			t.Fatal("unsupported target exposed")
		}
	}
}
func TestTailscaleGuardsAndCycles(t *testing.T) {
	cases := []struct{ name, source, kind, target string }{
		{"ordinary dialer", "proxies: [{name: Tail, type: tailscale}, {name: Other, type: socks5}, {name: Target, type: socks5, dialer-proxy: Other}]", "node", "Target"},
		{"direct dialer", "proxies: [{name: Tail, type: tailscale}, {name: Target, type: socks5, dialer-proxy: DIRECT}]", "node", "Target"},
		{"group cycle", "proxies: [{name: Tail, type: tailscale, dialer-proxy: Group}, {name: Target, type: socks5}]\nproxy-groups: [{name: Group, type: select, proxies: [Target]}]", "node", "Target"},
		{"provider cycle", "proxies: [{name: Tail, type: tailscale, dialer-proxy: Group}]\nproxy-groups: [{name: Group, type: select, use: [remote]}]\nproxy-providers: {remote: {type: http}}", "provider", "remote"},
		{"payload dialer", "proxies: [{name: Tail, type: tailscale}]\nproxy-providers: {remote: {type: inline, payload: [{name: Inner, type: socks5, dialer-proxy: DIRECT}]}}", "provider", "remote"},
		{"self", "proxies: [{name: Tail, type: tailscale}]", "node", "Tail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := tailscaleFixture(t, c.source)
			if err := s.MutateTailscale(context.Background(), "work", "bind", "Tail", c.kind, c.target, TailscaleOptions{}); err == nil {
				t.Fatal("unsafe binding succeeded")
			}
			if _, err := s.Override("work"); !os.IsNotExist(err) {
				t.Fatal("unsafe binding persisted")
			}
		})
	}
	s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale}, {name: Other, type: tailscale}, {name: Target, type: socks5, dialer-proxy: Other}]")
	if err := s.MutateTailscale(context.Background(), "work", "bind", "Tail", "node", "Target", TailscaleOptions{}); err != nil {
		t.Fatal("Tailscale rebind rejected", err)
	}
}
func TestTailscaleKeyFileBoundsAndPrivacy(t *testing.T) {
	for _, content := range []string{"", " \n", strings.Repeat("k", MaxTailscaleAuthKeyBytes+1), "one\ntwo", "abc\x00secret"} {
		path := testKey(t, content)
		if _, err := readTailscaleAuthKey(path); err == nil || strings.Contains(err.Error(), path) || strings.Contains(err.Error(), content) && content != "" && strings.TrimSpace(content) != "" {
			t.Fatalf("unsafe key content accepted/leaked: %v", err)
		}
	}
	if _, err := readTailscaleAuthKey(testKey(t, strings.Repeat("k", MaxTailscaleAuthKeyBytes))); err != nil {
		t.Fatal("maximum rejected", err)
	}
	path := testKey(t, "secret")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readTailscaleAuthKey(link); err == nil {
		t.Fatal("key symlink accepted")
	}
	if _, err := readTailscaleAuthKey(t.TempDir()); err == nil {
		t.Fatal("directory key accepted")
	}
}
func TestTailscaleFailedValidationRestoresOverride(t *testing.T) {
	s := tailscaleFixture(t, tsSource)
	key := testKey(t, "SECRET")
	s.Validate = func([]byte) error { return errors.New("validation rejected") }
	if err := s.MutateTailscale(context.Background(), "work", "add", "Tail", "", "", TailscaleOptions{ExitNode: ptr("exit"), AuthKeyFile: &key}); err == nil {
		t.Fatal("validation failure ignored")
	}
	if _, err := s.Override("work"); !os.IsNotExist(err) {
		t.Fatal("failed validation retained override")
	}
}

func TestTailscaleProviderInheritanceProtectionAndRemoval(t *testing.T) {
	s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale}]\nproxy-providers: {remote: {type: http, dialer-proxy: DIRECT}}")
	targets, err := s.TailscaleTargets("work")
	if err != nil || len(targets) != 1 || targets[0].DialerProxy != "DIRECT" {
		t.Fatalf("provider inheritance: %+v %v", targets, err)
	}
	for _, action := range []string{"bind", "unbind"} {
		name := "Tail"
		if action == "unbind" {
			name = ""
		}
		if err := s.MutateTailscale(context.Background(), "work", action, name, "provider", "remote", TailscaleOptions{}); err == nil {
			t.Fatal("overwrote non-TS provider dialer", action)
		}
	}
	for _, source := range []string{
		"proxies: [{name: Tail, type: tailscale}]\nproxy-providers: {remote: {type: http, dialer-proxy: Tail}}",
		"proxies: [{name: Tail, type: tailscale}]\nproxy-providers: {remote: {type: inline, payload: [{name: Inner, type: socks5, dialer-proxy: Tail}]}}",
	} {
		s := tailscaleFixture(t, source)
		if err := s.MutateTailscale(context.Background(), "work", "remove", "Tail", "", "", TailscaleOptions{}); err == nil {
			t.Fatal("removed provider-referenced TS node")
		}
	}
	s = tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale}]\nproxy-providers: {remote: {type: inline, dialer-proxy: Tail, override: {dialer-proxy: ''}, payload: [{name: Inner, type: socks5, dialer-proxy: Tail}]}}")
	targets, err = s.TailscaleTargets("work")
	if err != nil || targets[0].DialerProxy != "" {
		t.Fatal("blank override did not suppress source", targets, err)
	}
	if err := s.MutateTailscale(context.Background(), "work", "remove", "Tail", "", "", TailscaleOptions{}); err != nil {
		t.Fatal("suppressed provider reference prevented removal", err)
	}
}
func TestTailscaleRemovalProtectsNativeDNSReferences(t *testing.T) {
	for _, dns := range []string{
		"nameserver: [ts://Tail]",
		"fallback: [tailscale://Tail]",
		"nameserver-policy: {example.com: [tailscale://Tail]}",
		"proxy-server-nameserver: ['https://resolver.example/dns-query#Tail']",
		"direct-nameserver: ['https://resolver.example/dns-query#Tail&h3=true']",
	} {
		s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale}]\ndns: {"+dns+"}")
		if err := s.MutateTailscale(context.Background(), "work", "remove", "Tail", "", "", TailscaleOptions{}); err == nil || !strings.Contains(err.Error(), "DNS") {
			t.Fatalf("DNS reference removed: %s %v", dns, err)
		}
	}
}
func TestTailscaleNameCollisions(t *testing.T) {
	for _, name := range []string{"DIRECT", "REJECT", "GLOBAL", "Alpha", "Group", "remote"} {
		s := tailscaleFixture(t, tsSource)
		key := testKey(t, "secret")
		if err := s.MutateTailscale(context.Background(), "work", "add", name, "", "", TailscaleOptions{ExitNode: ptr("exit"), AuthKeyFile: &key}); err == nil {
			t.Fatal("name collision accepted", name)
		}
	}
}

const tsBatchSource = `proxies:
  - {name: Tail, type: tailscale, exit-node: exit}
  - {name: Alpha, type: socks5, server: localhost, port: 1080}
  - {name: Beta, type: socks5, server: localhost, port: 1081}
proxy-providers:
  first: {type: http, url: 'https://example.com/first', proxy: DIRECT, override: {udp: true}}
  second: {type: inline, payload: [{name: Inner, type: socks5}], override: {tfo: true}}
`

func TestTailscaleBatchBindingsSaveAndReloadOnce(t *testing.T) {
	for _, kind := range []string{"node", "provider"} {
		t.Run(kind, func(t *testing.T) {
			s := tailscaleFixture(t, tsBatchSource)
			targets := []string{"Alpha", "Beta"}
			if kind == "provider" {
				targets = []string{"first", "second"}
			}
			validations, reloads := 0, 0
			s.Validate = func([]byte) error { validations++; return nil }
			s.Reload = func(context.Context) error { reloads++; return nil }
			for _, action := range []string{"bind", "unbind"} {
				if err := s.MutateTailscaleBindings(context.Background(), "work", action, "Tail", kind, targets); err != nil {
					t.Fatal(err)
				}
				cfg, err := s.tailscaleConfig("work")
				if err != nil {
					t.Fatal(err)
				}
				want := "Tail"
				if action == "unbind" {
					want = ""
				}
				found := 0
				for _, target := range tsTargets(cfg) {
					if target.Kind == kind {
						found++
						if target.DialerProxy != want {
							t.Fatalf("target=%+v", target)
						}
					}
				}
				if found != 2 {
					t.Fatalf("targets=%v", tsTargets(cfg))
				}
				if kind == "provider" {
					providers := cfg["proxy-providers"].(map[string]any)
					first := providers["first"].(map[string]any)
					second := providers["second"].(map[string]any)
					if first["proxy"] != "DIRECT" || first["override"].(map[string]any)["udp"] != true || second["override"].(map[string]any)["tfo"] != true {
						t.Fatal("provider settings lost", providers)
					}
				}
			}
			if validations != 2 || reloads != 2 {
				t.Fatalf("validations=%d reloads=%d", validations, reloads)
			}
			source, _ := s.Show("work")
			if string(source) != tsBatchSource {
				t.Fatal("source changed")
			}
		})
	}
}

func TestTailscaleBatchFailuresPreserveWholeOverride(t *testing.T) {
	for _, kind := range []string{"node", "provider"} {
		for _, failure := range []string{"missing", "duplicate", "front proxy", "validation", "reload"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				s := tailscaleFixture(t, tsBatchSource)
				old := []byte("mixed-port: 7899\n")
				if err := s.SetOverride(context.Background(), "work", old); err != nil {
					t.Fatal(err)
				}
				targets := []string{"Alpha", "Beta"}
				if kind == "provider" {
					targets = []string{"first", "second"}
				}
				validations, reloads := 0, 0
				s.Validate = func([]byte) error {
					validations++
					if failure == "validation" {
						return errors.New("rejected")
					}
					return nil
				}
				s.Reload = func(context.Context) error {
					reloads++
					if failure == "reload" {
						return errors.New("activation failed")
					}
					return nil
				}
				switch failure {
				case "missing":
					targets[1] = "Missing"
				case "duplicate":
					targets[1] = targets[0]
				case "front proxy":
					source := strings.Replace(tsBatchSource, "name: Beta, type: socks5", "name: Beta, dialer-proxy: DIRECT, type: socks5", 1)
					if kind == "provider" {
						source = strings.Replace(tsBatchSource, "tfo: true", "tfo: true, dialer-proxy: DIRECT", 1)
					}
					if err := os.WriteFile(filepath.Join(s.Dir, "profiles", "work.yaml"), []byte(source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.MutateTailscaleBindings(context.Background(), "work", "bind", "Tail", kind, targets); err == nil {
					t.Fatal("batch failure ignored")
				}
				after, _ := s.Override("work")
				if string(after) != string(old) {
					t.Fatalf("partial override: %s", after)
				}
				if failure != "validation" && failure != "reload" && validations != 0 {
					t.Fatal("invalid batch reached save")
				}
				if failure != "reload" && reloads != 0 {
					t.Fatal("invalid batch reloaded")
				}
			})
		}
	}
}
