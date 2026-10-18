package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"gopkg.in/yaml.v3"
)

func TestNewDefaultProfileDNSAndTUNDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles", "default.yaml")
	if err := ensureDefaultProfile(path, "default"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Rules []string `yaml:"rules"`
		DNS   struct {
			Enable                   bool     `yaml:"enable"`
			ResolveProxyDestinations bool     `yaml:"resolve-proxy-destinations"`
			EnhancedMode             string   `yaml:"enhanced-mode"`
			DefaultNameserver        []string `yaml:"default-nameserver"`
			Nameserver               []string `yaml:"nameserver"`
			DirectNameserver         []string `yaml:"direct-nameserver"`
			ProxyServerNameserver    []string `yaml:"proxy-server-nameserver"`
			Fallback                 []string `yaml:"fallback"`
		} `yaml:"dns"`
		TUN struct {
			Enable              bool     `yaml:"enable"`
			AutoRoute           bool     `yaml:"auto-route"`
			StrictRoute         bool     `yaml:"strict-route"`
			AutoDetectInterface bool     `yaml:"auto-detect-interface"`
			DNSHijack           []string `yaml:"dns-hijack"`
		} `yaml:"tun"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	dnspod := []string{"https://1.12.12.12/dns-query#DIRECT", "https://120.53.53.53/dns-query#DIRECT"}
	if !cfg.DNS.Enable || !cfg.DNS.ResolveProxyDestinations || cfg.DNS.EnhancedMode != "fake-ip" || !slices.Equal(cfg.Rules, []string{"MATCH,DIRECT"}) {
		t.Fatalf("unexpected default DNS or rules: %+v", cfg)
	}
	if !slices.Equal(cfg.DNS.DefaultNameserver, dnspod) || !slices.Equal(cfg.DNS.DirectNameserver, dnspod) || !slices.Equal(cfg.DNS.ProxyServerNameserver, dnspod) || !slices.Equal(cfg.DNS.Nameserver, dnspod) || len(cfg.DNS.Fallback) != 0 {
		t.Fatalf("unexpected DoH roles: %+v", cfg.DNS)
	}
	if cfg.TUN.Enable || !cfg.TUN.AutoRoute || !cfg.TUN.StrictRoute || !cfg.TUN.AutoDetectInterface || !slices.Equal(cfg.TUN.DNSHijack, []string{"any:53", "tcp://any:53"}) {
		t.Fatalf("unexpected TUN interception: %+v", cfg.TUN)
	}
}

func TestDNSLocalPolicySurvivesSourceReplacement(t *testing.T) {
	root := t.TempDir()
	config := isolatedTestPaths(t, root).ConfigDir
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	if err := os.WriteFile(source, []byte("proxies:\n  - name: NodeA\n    type: ss\n    server: proxy.example\n    port: 443\n    cipher: aes-128-gcm\n    password: placeholder\ndns:\n  enable: false\n  nameserver:\n    - 8.8.8.8\n  nameserver-policy:\n    '+.old.example': 8.8.8.8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", bin)
	run := func(args ...string) (map[string]any, int) {
		t.Helper()
		var out, errout bytes.Buffer
		code := Run(append([]string{"--json"}, args...), &out, &errout, "test", "commit")
		if code != 0 {
			return map[string]any{"error": errout.String()}, code
		}
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Data, code
	}
	if _, code := run("dns", "set", "proxy", "NodeA", "https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"); code != 0 {
		t.Fatal("set failed")
	}
	assertDNSRoles := func(wantBootstrap []string, wantPrimary []string) {
		t.Helper()
		effective, err := os.ReadFile(filepath.Join(config, "overrides", "default.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			DNS struct {
				Default  []string `yaml:"default-nameserver"`
				Direct   []string `yaml:"direct-nameserver"`
				Proxy    []string `yaml:"proxy-server-nameserver"`
				Primary  []string `yaml:"nameserver"`
				Fallback []string `yaml:"fallback"`
			} `yaml:"dns"`
		}
		if err := yaml.Unmarshal(effective, &cfg); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cfg.DNS.Default, wantBootstrap) || !slices.Equal(cfg.DNS.Direct, wantBootstrap) || !slices.Equal(cfg.DNS.Proxy, wantBootstrap) || !slices.Equal(cfg.DNS.Primary, wantPrimary) || len(cfg.DNS.Fallback) != 0 {
			t.Fatalf("unexpected managed DNS roles: %+v", cfg.DNS)
		}
	}
	assertDNSRoles([]string{"https://1.12.12.12/dns-query#DIRECT", "https://120.53.53.53/dns-query#DIRECT"}, []string{"https://1.1.1.1/dns-query#NodeA", "https://8.8.8.8/dns-query#NodeA"})
	if _, code := run("dns", "set", "proxy", "NodeA", "https://1.1.1.1/dns-query", "--bootstrap", "https://9.9.9.9/dns-query"); code != 0 {
		t.Fatal("custom bootstrap failed")
	}
	assertDNSRoles([]string{"https://9.9.9.9/dns-query#DIRECT"}, []string{"https://1.1.1.1/dns-query#NodeA"})
	if _, code := run("dns", "set", "proxy", "NodeA", "https://1.1.1.1/dns-query", "--bootstrap", "https://dns.example/dns-query"); code != 1 {
		t.Fatal("hostname bootstrap accepted")
	}
	assertDNSRoles([]string{"https://9.9.9.9/dns-query#DIRECT"}, []string{"https://1.1.1.1/dns-query#NodeA"})
	if _, code := run("dns", "exception", "add", "corp.example", "udp://10.0.0.53:53"); code != 0 {
		t.Fatal("exception failed")
	}
	if _, code := run("dns", "tun", "on"); code != 0 {
		t.Fatal("tun failed")
	}
	data, code := run("dns", "status")
	if code != 0 || data["policy"] != "proxy" || data["resolve_proxy_destinations"] != true || data["tun_enabled"] != true {
		t.Fatalf("status: %v", data)
	}
	issues := data["issues"].([]any)
	if len(issues) != 1 || !strings.Contains(issues[0].(string), "corp.example") {
		t.Fatalf("issues: %v", issues)
	}
	original, _ := os.ReadFile(source)
	if !strings.Contains(string(original), "8.8.8.8") {
		t.Fatal("source was modified")
	}
	if err := os.WriteFile(source, []byte("proxies:\n  - name: NodeA\n    type: ss\n    server: proxy.example\n    port: 443\n    cipher: aes-128-gcm\n    password: placeholder\ndns:\n  enable: false\n  nameserver:\n    - 9.9.9.9\n  nameserver-policy:\n    '+.stale.example': 9.9.9.9\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, code = run("dns", "status")
	if code != 0 || data["enabled"] != true || data["resolve_proxy_destinations"] != true {
		t.Fatalf("source replacement lost policy: %v", data)
	}
	for _, issue := range data["issues"].([]any) {
		if strings.Contains(issue.(string), "stale.example") {
			t.Fatalf("stale policy survived: %v", data)
		}
	}
	if err := os.WriteFile(source, []byte("proxies: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, code := run("profile", "use", "default"); code != 1 {
		t.Fatal("missing DNS proxy node activated")
	}
	if _, code := run("dns", "set", "direct", "udp://1.1.1.1:53"); code != 1 {
		t.Fatal("plaintext upstream accepted")
	}
	if _, code := run("dns", "set", "direct", "https://1.1.1.1/dns-query"); code != 0 {
		t.Fatal("direct DoH policy failed")
	}
	directOverride, err := os.ReadFile(filepath.Join(config, "overrides", "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var directDNS struct {
		DNS struct {
			Primary []string `yaml:"nameserver"`
			Direct  []string `yaml:"direct-nameserver"`
			Proxy   []string `yaml:"proxy-server-nameserver"`
		} `yaml:"dns"`
	}
	if err := yaml.Unmarshal(directOverride, &directDNS); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(directDNS.DNS.Primary, []string{"https://1.1.1.1/dns-query#DIRECT"}) || !slices.Equal(directDNS.DNS.Direct, directDNS.DNS.Primary) || !slices.Equal(directDNS.DNS.Proxy, []string{"https://1.12.12.12/dns-query#DIRECT", "https://120.53.53.53/dns-query#DIRECT"}) {
		t.Fatalf("unexpected direct DNS roles: %+v", directDNS.DNS)
	}
	data, code = run("dns", "status")
	if code != 0 || data["policy"] != "direct" || data["resolve_proxy_destinations"] != true {
		t.Fatalf("direct DNS lost destination resolution: %v", data)
	}
}

func TestDNSQueryAndFlushUsePrivateControlAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mihomo.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dns/query":
			if r.Method != http.MethodGet || r.URL.Query().Get("name") != "example.com" || r.URL.Query().Get("type") != "AAAA" {
				t.Errorf("unexpected query: %s", r.URL.String())
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Status":0,"Answer":[]}`))
		case "/cache/dns/flush":
			if r.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	client := control.New(path)
	query, err := dnsCommand(context.Background(), []string{"dns", "query", "example.com", "AAAA"}, nil, "default", client)
	if err != nil || query.(map[string]any)["response"] == nil {
		t.Fatalf("query: %v, %v", query, err)
	}
	flush, err := dnsCommand(context.Background(), []string{"dns", "flush"}, nil, "default", client)
	if err != nil || flush.(map[string]any)["flushed"] != true {
		t.Fatalf("flush: %v, %v", flush, err)
	}
}

func TestDNSURLValidation(t *testing.T) {
	for _, raw := range []string{"https://1.1.1.1/dns-query", "https://dns.example/dns-query"} {
		if err := dnsURL(raw, false); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"system://", "http://dns.example/dns-query", "udp://1.1.1.1:53", "https://user:pass@dns.example/dns-query"} {
		if err := dnsURL(raw, false); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if err := dnsURL("udp://10.0.0.53:53", true); err != nil {
		t.Fatal(err)
	}
	if err := dnsURL("udp://resolver.example:53", true); err == nil {
		t.Fatal("plaintext hostname accepted")
	}
}

func TestUnrelatedOverrideDoesNotRejectSourceRulesDNS(t *testing.T) {
	effective := []byte("dns:\n  nameserver: [https://1.1.1.1/dns-query#RULES]\n")
	if err := validateManagedDNSProxyTargets([]byte("mixed-port: 17890\n"), effective); err != nil {
		t.Fatal(err)
	}
}
