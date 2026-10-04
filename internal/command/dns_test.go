package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
)

func TestDNSLocalPolicySurvivesSourceReplacement(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config", "nagi")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	if err := os.WriteFile(source, []byte("proxies:\n  - name: NodeA\n    type: ss\n    server: proxy.example\n    port: 443\n    cipher: aes-128-gcm\n    password: placeholder\ndns:\n  enable: false\n  nameserver:\n    - 8.8.8.8\n  nameserver-policy:\n    '+.old.example': 8.8.8.8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", bin)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
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
	if _, code := run("dns", "exception", "add", "corp.example", "udp://10.0.0.53:53"); code != 0 {
		t.Fatal("exception failed")
	}
	if _, code := run("dns", "tun", "on"); code != 0 {
		t.Fatal("tun failed")
	}
	data, code := run("dns", "status")
	if code != 0 || data["policy"] != "proxy" || data["tun_enabled"] != true {
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
	if code != 0 || data["enabled"] != true {
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
