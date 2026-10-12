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

const dnsGroupProfile = `proxy-groups:
  - name: Only Direct
    type: select
    proxies: [DIRECT]
  - name: Active
    type: select
    proxies: [Node, DIRECT]
  - name: Starts Direct
    type: select
    proxies: [DIRECT, Node]
  - name: Via Active
    type: select
    proxies: [Active]
proxies:
  - name: Node
    type: ss
    server: example.com
    port: 443
    cipher: aes-128-gcm
    password: placeholder
dns:
  nameserver: [udp://1.1.1.1:53]
  fallback: [udp://8.8.8.8:53]
  nameserver-policy:
    +.example: udp://9.9.9.9:53
  use-hosts: true
`

func TestDNSSettingsBindings(t *testing.T) {
	servers := []string{"https://1.1.1.1/dns-query"}
	bootstrap := []string{"https://2.2.2.2/dns-query"}
	for _, test := range []struct{ group, wantPrimary, wantDirect string }{
		{"", "https://1.1.1.1/dns-query#DIRECT", "https://1.1.1.1/dns-query#DIRECT"},
		{"Proxy 组", "https://1.1.1.1/dns-query#Proxy 组", "https://2.2.2.2/dns-query#DIRECT"},
	} {
		dns := DNSSettings(servers, bootstrap, test.group)
		if got := dns["nameserver"].([]string)[0]; got != test.wantPrimary {
			t.Fatalf("primary = %q", got)
		}
		if got := dns["direct-nameserver"].([]string)[0]; got != test.wantDirect {
			t.Fatalf("direct = %q", got)
		}
		if got := dns["default-nameserver"].([]string)[0]; got != bootstrap[0]+"#DIRECT" {
			t.Fatalf("bootstrap = %q", got)
		}
	}
}

func TestValidateDNSGroup(t *testing.T) {
	for _, group := range []string{"", "DIRECT", "RULES", "A&B", "A=B", "A#B", "Only Direct", "Starts Direct", "Missing"} {
		if err := ValidateDNSGroup([]byte(dnsGroupProfile), group); err == nil {
			t.Errorf("accepted %q", group)
		}
	}
	for _, group := range []string{"Active", "Via Active"} {
		if err := ValidateDNSGroup([]byte(dnsGroupProfile), group); err != nil {
			t.Errorf("rejected %q: %v", group, err)
		}
	}
}

func TestDNSReplacementMarkerDoesNotReachMihomo(t *testing.T) {
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "work", []byte(dnsGroupProfile)); err != nil {
		t.Fatal(err)
	}
	patch := []byte("nagi-dns-replace: true\nnagi-dns-proxy-group: Active\ndns:\n  nameserver: [https://1.1.1.1/dns-query#Active]\n")
	if err := s.SetOverride(ctx, "work", patch); err != nil {
		t.Fatal(err)
	}
	effective, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(effective, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg[DNSReplaceKey]; ok {
		t.Fatal("replace metadata leaked")
	}
	if _, ok := cfg[DNSProxyGroupKey]; ok {
		t.Fatal("group metadata leaked")
	}
	dns := cfg["dns"].(map[string]any)
	if _, ok := dns["fallback"]; ok {
		t.Fatal("source fallback survived")
	}
	if _, ok := dns["use-hosts"]; ok {
		t.Fatal("source DNS option survived")
	}
	if _, ok := dns["nameserver-policy"]; ok {
		t.Fatal("source policy survived")
	}
}

func TestApplyDefaultOverrideDNSBindingAndRestore(t *testing.T) {
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("mixed-port: 17890\ndns:\n  nameserver: [udp://3.3.3.3:53]\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "work", []byte(dnsGroupProfile)); err != nil {
		t.Fatal(err)
	}
	previous := []byte("mode: global\ndns:\n  use-hosts: false\n")
	if err := s.SetOverride(ctx, "work", previous); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDefaultOverride(ctx, "work", "Active"); err != nil {
		t.Fatal(err)
	}
	assertBoundDNS := func() {
		t.Helper()
		effective, err := s.Effective("work")
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err := yaml.Unmarshal(effective, &cfg); err != nil {
			t.Fatal(err)
		}
		if _, ok := cfg[DNSReplaceKey]; ok {
			t.Fatal("replace metadata leaked into effective config")
		}
		if _, ok := cfg[DNSProxyGroupKey]; ok {
			t.Fatal("group metadata leaked into effective config")
		}
		dns := cfg["dns"].(map[string]any)
		if _, ok := dns["use-hosts"]; ok {
			t.Fatalf("source DNS field survived replacement: %v", dns)
		}
		if got := dnsStrings(dns["nameserver"])[0]; !strings.HasSuffix(got, "#Active") {
			t.Fatalf("nameserver = %q", got)
		}
		if _, ok := dns["nameserver-policy"].(map[string]any); !ok {
			t.Fatalf("source policy survived: %v", dns["nameserver-policy"])
		}
	}
	assertBoundDNS()
	if group, err := s.DefaultDNSProxyGroup("work"); err != nil || group != "Active" {
		t.Fatalf("binding = %q, %v", group, err)
	}
	if err := s.ApplyDefaultOverride(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	assertBoundDNS()
	if err := s.RestoreDefaultOverride(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Override("work")
	if err != nil || string(got) != string(previous) {
		t.Fatalf("restored = %q, %v", got, err)
	}
}

func TestApplyDefaultOverrideRejectsSavedDirectAndRollsBack(t *testing.T) {
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("mixed-port: 17890\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "work", []byte(dnsGroupProfile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "proxy-selections.json"), []byte(`{"work":{"Active":"DIRECT"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDefaultOverride(ctx, "work", "Active"); err == nil {
		t.Fatal("accepted saved DIRECT selection")
	}
	if _, err := s.Override("work"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("override after rejected apply: %v", err)
	}
	if err := os.Remove(filepath.Join(s.Dir, "proxy-selections.json")); err != nil {
		t.Fatal(err)
	}
	s.Validate = func(data []byte) error {
		if strings.Contains(string(data), "#Active") {
			return errors.New("invalid")
		}
		return nil
	}
	if err := s.ApplyDefaultOverride(ctx, "work", "Active"); err == nil {
		t.Fatal("accepted invalid generated config")
	}
	if _, err := s.Override("work"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("override after validation failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "overrides", "work.yaml.default-state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore point after failure: %v", err)
	}
}

func TestApplyDefaultOverrideRequiresGroupAndRetainsBusinessOverlay(t *testing.T) {
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	ctx := context.Background()
	if err := s.Write(ctx, "default", []byte("tun:\n  enable: true\n  stack: system\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "work", []byte("tun:\n  enable: false\npreserve-proxy-hostname: true\n")); err != nil {
		t.Fatal(err)
	}
	previous := []byte("proxies:\n  - name: Node\n    type: ss\nproxy-groups:\n  - name: Active\n    type: select\n    proxies: [Node]\nrules: [MATCH,Active]\n")
	if err := s.SetOverride(ctx, "work", previous); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyDefaultOverride(ctx, "work"); err == nil {
		t.Fatal("accepted direct fallback despite proxy nodes")
	}
	if err := s.ApplyDefaultOverride(ctx, "work", "Active"); err != nil {
		t.Fatal(err)
	}
	effective, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(effective, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["tun"].(map[string]any)["enable"] != false {
		t.Fatal("TUN changed from disabled to enabled")
	}
	if cfg["preserve-proxy-hostname"] != false {
		t.Fatal("conflicting preserve-proxy-hostname setting survived")
	}
	if len(cfg["proxies"].([]any)) != 1 || len(cfg["proxy-groups"].([]any)) != 1 || len(cfg["rules"].([]any)) != 2 {
		t.Fatalf("business overlay was discarded: %v", cfg)
	}
	if err := s.RestoreDefaultOverride(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Override("work")
	if err != nil || string(got) != string(previous) {
		t.Fatalf("restore = %q, %v", got, err)
	}
}
