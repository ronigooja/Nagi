package profile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dlclark/regexp2"
	"gopkg.in/yaml.v3"
)

func chainFixture(t *testing.T, text string) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestProviderChainResourcesRetainSourceAndIsolateSelectedNode(t *testing.T) {
	cfg := chainFixture(t, `proxies:
  - {name: Entry, type: socks5, server: 127.0.0.1, port: 10001}
proxy-providers:
  work:
    type: http
    url: https://example.com/fictional-feed
    proxy: DIRECT
    interval: 3600
    filter: ^Raw
    override: {additional-prefix: "P ", dialer-proxy: DIRECT}
proxy-groups:
  - {name: Manual, type: select, use: [work]}
  - {name: All, type: fallback, include-all: true}
  - {name: AllProviders, type: select, include-all-providers: true}
  - {name: AllProxies, type: select, include-all-proxies: true}
`)
	before, _ := yaml.Marshal(cfg["proxy-providers"])
	m := ProviderChainMember{Provider: "work", Node: "P Raw One", RawName: "Raw One", Type: "socks5", Dialer: "Entry"}
	cfg[ProviderChainMembersKey] = []ProviderChainMember{m}
	s := NewStore(t.TempDir(), nil, nil)
	if err := s.applyProviderChainMembers("work", cfg); err != nil {
		t.Fatal(err)
	}
	if _, exists := cfg[ProviderChainMembersKey]; exists {
		t.Fatal("private metadata reached native config")
	}
	providers := cfg["proxy-providers"].(map[string]any)
	after, _ := yaml.Marshal(map[string]any{"work": providers["work"]})
	if string(before) != string(after) {
		t.Fatal("source provider was changed")
	}
	derived := providers[m.providerID()].(map[string]any)
	if chainProviderDialer(derived) != "Entry" || derived["filter"] != exactProviderName("Raw One") || derived["proxy"] != "DIRECT" || derived["interval"] != 3600 {
		t.Fatalf("wrong native resource: %#v", derived)
	}
	if !strings.Contains(tsString(derived, "path"), "/profiles/.chain-cache/work/") {
		t.Fatal("cache outside engine home or profile scope")
	}
	if derived["exclude-type"] != "tailscale" {
		t.Fatal("future source protocol changes may duplicate Tailscale identity")
	}
	groups := cfg["proxy-groups"].([]any)
	bridge := groups[len(groups)-1].(map[string]any)
	if bridge["empty-fallback"] != "REJECT" || bridge["filter"] != exactProviderName(m.Node) {
		t.Fatal("bridge permits fallback drift")
	}
	manual := groups[0].(map[string]any)
	if !reflect.DeepEqual(manual["use"], []any{"work"}) {
		t.Fatal("source group was rebound")
	}
	for _, idx := range []int{1, 2} {
		group := groups[idx].(map[string]any)
		if !reflect.DeepEqual(group["use"], []any{"work"}) || group["include-all-providers"] != false {
			t.Fatal("derived provider leaked into include-all group")
		}
	}
	if groups[1].(map[string]any)["include-all-proxies"] != true || groups[3].(map[string]any)["include-all-proxies"] != true {
		t.Fatal("top-level automatic inclusion changed")
	}
	if err := chainValidateGraph(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestProviderChainMembersRespectSourceIdentityChangesAndFilters(t *testing.T) {
	m := ProviderChainMember{Provider: "p", RawName: "Raw One", Node: "P One S", Type: "socks5"}
	source := chainFixture(t, `filter: '^Raw'
override:
  proxy-name: [{pattern: '^Raw ', target: ''}]
  additional-prefix: 'P '
  additional-suffix: ' S'
`)
	if err := providerMemberAllowed(source, m); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"filter", "exclude-filter", "exclude-type", "rename"} {
		t.Run(field, func(t *testing.T) {
			copyData, _ := yaml.Marshal(source)
			var changed map[string]any
			yaml.Unmarshal(copyData, &changed)
			switch field {
			case "filter":
				changed[field] = "^Other"
			case "exclude-filter":
				changed[field] = "One$"
			case "exclude-type":
				changed[field] = "http|SOCKS5"
			case "rename":
				changed["override"].(map[string]any)["additional-prefix"] = "Changed "
			}
			if err := providerMemberAllowed(changed, m); err == nil {
				t.Fatal("changed source silently rebound provider node")
			}
		})
	}
}

func TestProviderChainExactNamesAndScopedIdentity(t *testing.T) {
	for _, name := range []string{"A [one].*", "quoted`node", "中文节点", "A\nB"} {
		filter := exactProviderName(name)
		if strings.Contains(filter, "`") {
			t.Fatal("literal backtick splits native filter")
		}
		r := regexp2.MustCompile(filter, regexp2.None)
		if match, _ := r.MatchString(name); !match {
			t.Fatalf("filter did not match %q", name)
		}
		if match, _ := r.MatchString(name + " other"); match {
			t.Fatal("filter allowed a different node")
		}
	}
	a := ProviderChainMember{Provider: "a", Node: "Shared"}
	b := ProviderChainMember{Provider: "b", Node: "Shared"}
	if a.ID() == b.ID() {
		t.Fatal("different providers share node identity")
	}
}

func TestProviderChainRejectsTailscaleAndGeneratedResourceCollisions(t *testing.T) {
	m := ProviderChainMember{Provider: "p", Node: "A", RawName: "A", Type: "tailscale"}
	cfg := map[string]any{ProviderChainMembersKey: []ProviderChainMember{m}}
	if _, err := providerChainMembers(cfg); err == nil {
		t.Fatal("provider Tailscale identity may be cloned")
	}
	m.Type = "socks5"
	cfg = chainFixture(t, "proxy-providers: {p: {type: inline, payload: []}}\n")
	cfg[ProviderChainMembersKey] = []ProviderChainMember{m}
	cfg["proxy-groups"] = []any{map[string]any{"name": m.ID(), "type": "select", "proxies": []any{"DIRECT"}}}
	if err := NewStore(t.TempDir(), nil, nil).applyProviderChainMembers("work", cfg); err == nil {
		t.Fatal("generated group overwrote existing group")
	}
}

func TestProviderChainGraphIncludesGeneratedDialerAndDownloadDependencies(t *testing.T) {
	m := ProviderChainMember{Provider: "p", Node: "A", RawName: "A", Type: "socks5"}
	for _, dependency := range []string{"dialer-proxy", "download-proxy"} {
		t.Run(dependency, func(t *testing.T) {
			cfg := chainFixture(t, "proxy-providers: {p: {type: inline, payload: []}}\n")
			if dependency == "dialer-proxy" {
				m.Dialer = m.ID()
			} else {
				m.Dialer = ""
				cfg["proxy-providers"].(map[string]any)["p"].(map[string]any)["proxy"] = m.ID()
			}
			cfg[ProviderChainMembersKey] = []ProviderChainMember{m}
			if err := NewStore(t.TempDir(), nil, nil).applyProviderChainMembers("work", cfg); err != nil {
				t.Fatal(err)
			}
			if err := chainValidateGraph(cfg); err == nil {
				t.Fatal("generated native provider cycle was accepted")
			}
		})
	}
}
