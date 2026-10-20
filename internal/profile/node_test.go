package profile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProxyNodesRedactEveryTopLevelType(t *testing.T) {
	s := tailscaleFixture(t, `proxies:
 - {name: Other, type: vmess, uuid: PRIVATE, server: private.example}
 - {name: Tail, type: tailscale, auth-key: PRIVATE, state-dir: /private/identity, exit-node: exit}
proxy-groups: []
`)
	nodes, err := s.ProxyNodes("work")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(nodes)
	for _, secret := range []string{"PRIVATE", "private.example", "/private/identity", "state-dir", "auth-key"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("leaked %s: %s", secret, data)
		}
	}
	if len(nodes) != 2 || nodes[1].Type != "vmess" || nodes[0].Tailscale == nil || !nodes[0].Tailscale.AuthKeyConfigured {
		t.Fatalf("nodes=%+v", nodes)
	}
}
func TestAddProxyNodeGroupsOneSaveAndFailureAtomic(t *testing.T) {
	s := tailscaleFixture(t, tsSource+"\n")
	validations := 0
	s.Validate = func([]byte) error { validations++; return nil }
	// A nonexistent second group must not persist the new node or an override.
	o := TailscaleOptions{ExitNode: ptr("exit"), AuthKey: ptr("tskey-FICTITIOUS")}
	err := s.AddProxyNode(context.Background(), "work", "Tail", o, []string{"Group", "Missing"})
	if err == nil {
		t.Fatal("accepted unknown group")
	}
	if _, err = os.Stat(filepath.Join(s.Dir, "overrides", "work.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("override persisted: %v", err)
	}
	if err = s.AddProxyNode(context.Background(), "work", "Tail", o, []string{"Group"}); err != nil {
		t.Fatal(err)
	}
	if validations != 1 {
		t.Fatalf("validations=%d", validations)
	}
	groups, err := s.ProxyNodeGroups("work", "Tail")
	if err != nil || !reflect.DeepEqual(groups.Explicit, []string{"Group"}) {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
}
func TestProxyNodeGroupsRetainFiltersAndOtherMembers(t *testing.T) {
	s := tailscaleFixture(t, `proxies:
 - {name: Alpha, type: socks5}
 - {name: Tail, type: tailscale, auth-key: FICTITIOUS, exit-node: exit}
proxy-groups:
 - {name: Explicit, type: select, proxies: [Alpha, Tail], url: 'https://example.invalid/test', filter: '^Never$'}
 - {name: Auto, type: select, include-all-proxies: true, filter: '(?=Tail)Tail'}
 - {name: Excluded, type: select, include-all: true, exclude-type: TAILSCALE}
 - {name: Filtered, type: select, include-all-proxies: true, filter: '^Alpha$'}
`)
	groups, err := s.ProxyNodeGroups("work", "Tail")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(groups.Explicit, []string{"Explicit"}) || !reflect.DeepEqual(groups.Automatic, []string{"Auto"}) || !reflect.DeepEqual(groups.Excluded, []string{"Excluded", "Filtered"}) {
		t.Fatalf("groups=%+v", groups)
	}
	if err = s.SetProxyNodeGroups(context.Background(), "work", "Tail", []string{"Auto"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.tailscaleConfig("work")
	list := cfg["proxy-groups"].([]any)
	first := list[0].(map[string]any)
	if first["filter"] != "^Never$" || first["url"] != "https://example.invalid/test" || !reflect.DeepEqual(first["proxies"], []any{"Alpha"}) {
		t.Fatalf("lost group settings: %+v", first)
	}
	if err = s.ClearProxyNodeGroups(context.Background(), "work", "Tail"); err != nil {
		t.Fatal(err)
	}
	groups, _ = s.ProxyNodeGroups("work", "Tail")
	if len(groups.Explicit) != 0 || !reflect.DeepEqual(groups.Automatic, []string{"Auto"}) {
		t.Fatalf("clear changed auto rules: %+v", groups)
	}
}
func TestProxyNodeRemovalReferences(t *testing.T) {
	for _, extra := range []string{
		"proxy-groups: [{name: G, type: select, proxies: [Tail]}]",
		"proxy-groups: [{name: G, type: select, proxies: [DIRECT], empty-fallback: Tail}]",
		"rules: ['DOMAIN,example.invalid,Tail,no-resolve']",
		"sub-rules: {nested: ['MATCH,Tail']}",
		"dns: {nameserver: ['https://example.invalid/dns-query#Tail']}",
		"proxy-providers: {P: {type: http, proxy: Tail}}",
		"proxy-providers: {P: {type: inline, payload: [{name: Inner, type: socks5, dialer-proxy: Tail}]}}",
		"rule-providers: {P: {type: http, proxy: Tail}}",
	} {
		t.Run(extra, func(t *testing.T) {
			s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale, auth-key: FICTITIOUS, exit-node: exit}]\n"+extra+"\n")
			if err := s.RemoveProxyNode(context.Background(), "work", "Tail"); err == nil {
				t.Fatal("removed referenced node")
			}
		})
	}
}
func TestProxyNodeRemoveRetainsIdentity(t *testing.T) {
	s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale, auth-key: FICTITIOUS, exit-node: exit}]\n")
	state := filepath.Join(s.Dir, ".tailscale-state", "fixture")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(state, "identity")
	if err := os.WriteFile(file, []byte("FICTITIOUS"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProxyNode(context.Background(), "work", "Tail"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
}
func TestProxyNodeInputValidationDoesNotLeak(t *testing.T) {
	s := tailscaleFixture(t, tsSource)
	for _, o := range []TailscaleOptions{
		{ExitNode: ptr("exit"), AuthKey: ptr("PRIVATE KEY")},
		{ExitNode: ptr("exit"), AuthKey: ptr("PRIVATE"), AuthKeyFile: ptr("PRIVATE")},
		{ExitNode: ptr("exit"), AuthKey: ptr(strings.Repeat("PRIVATE", 3000))},
	} {
		err := s.AddProxyNode(context.Background(), "work", "Tail", o, nil)
		if err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestProxyNodeGroupsNativeExcludeTypeAndExplicitExclusion(t *testing.T) {
	s := tailscaleFixture(t, `proxies: [{name: Alpha, type: ss, password: FICTITIOUS}]
proxy-groups:
 - {name: Hidden, type: select, proxies: [Alpha], exclude-filter: '^Alpha$'}
 - {name: NativeType, type: select, include-all-proxies: true, exclude-type: Shadowsocks}
 - {name: YAMLType, type: select, include-all-proxies: true, exclude-type: ss}
`)
	groups, err := s.ProxyNodeGroups("work", "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(groups.Explicit, []string{"Hidden"}) || !reflect.DeepEqual(groups.Excluded, []string{"Hidden", "NativeType"}) || !reflect.DeepEqual(groups.Automatic, []string{"YAMLType"}) {
		t.Fatalf("groups=%+v", groups)
	}
}
func TestAddProxyNodeValidationFailureLeavesNoNodeOrMembership(t *testing.T) {
	s := tailscaleFixture(t, tsSource)
	s.Validate = func([]byte) error { return errors.New("candidate rejected") }
	err := s.AddProxyNode(context.Background(), "work", "Tail", TailscaleOptions{ExitNode: ptr("exit"), AuthKey: ptr("FICTITIOUS")}, []string{"Group"})
	if err == nil {
		t.Fatal("accepted rejected configuration")
	}
	cfg, err := s.tailscaleConfig("work")
	if err != nil {
		t.Fatal(err)
	}
	if tsFind(cfg, "Tail") != nil {
		t.Fatal("failed candidate persisted a node")
	}
	groups := cfg["proxy-groups"].([]any)
	if !reflect.DeepEqual(groups[0].(map[string]any)["proxies"], []any{"Alpha"}) {
		t.Fatal("failed candidate persisted membership")
	}
}

func TestProxyNodeRemovalKnownInterfaceReferences(t *testing.T) {
	for _, extra := range []string{
		"proxy-groups: [{name: G, type: select, proxies: [DIRECT], dialer-proxy: Tail}]",
		"rules: ['IP-CIDR,192.0.2.0/24,Tail,src,no-resolve']",
		"rules: ['IP-CIDR,192.0.2.0/24,Tail,src']",
		"rules: ['AND,((DOMAIN,example.invalid),(NETWORK,TCP)),Tail']",
		"dns: {nameserver-policy: {'example.invalid': ['ts://Tail']}}",
		"dns: {proxy-server-nameserver-policy: {'example.invalid': ['tailscale://Tail']}}",
		"ntp: {enable: true, dialer-proxy: Tail}",
		"listeners: [{name: inbound, type: mixed, port: 1080, proxy: Tail}]",
		"tunnels: [{network: tcp, address: '127.0.0.1:1234', target: 'example.invalid:443', proxy: Tail}]",
	} {
		t.Run(extra, func(t *testing.T) {
			s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale, auth-key: FICTITIOUS, exit-node: exit}]\n"+extra+"\n")
			if err := s.RemoveProxyNode(context.Background(), "work", "Tail"); err == nil {
				t.Fatal("removed referenced node")
			}
		})
	}
	s := tailscaleFixture(t, "proxies: [{name: 'Tail Name', type: tailscale, auth-key: FICTITIOUS, exit-node: exit}]\ndns: {nameserver: ['https://example.invalid/dns-query#Tail%20Name']}\n")
	if err := s.RemoveProxyNode(context.Background(), "work", "Tail Name"); err == nil {
		t.Fatal("removed URL-escaped DNS reference")
	}
}
func TestProxyNodeRuleReferencesUseNativeTargetPosition(t *testing.T) {
	for _, rule := range []string{"DOMAIN,Tail,DIRECT", "DOMAIN,example.invalid,DIRECT,Tail", "SUB-RULE,(NETWORK,TCP),Tail"} {
		if nodeRuleTarget(rule) == "Tail" {
			t.Fatalf("false outbound reference in %s", rule)
		}
	}
}
