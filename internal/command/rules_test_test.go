package command

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/rules"
)

func TestRuleTestPreflight(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	var out, errs bytes.Buffer
	if code := Run([]string{"rules", "test", "--help"}, &out, &errs, "test", "test"); code != 0 || !strings.Contains(out.String(), "--network tcp|udp") {
		t.Fatalf("help: %d %s %s", code, out.String(), errs.String())
	}
	for _, tail := range [][]string{
		{}, {""}, {"https://example.com"}, {"example.com:443"}, {"bad..example"}, {"[::1]"}, {"fe80::1%eth0"},
		{"example.com", "--port", "0"}, {"example.com", "--port", "65536"}, {"example.com", "--port", "abc"},
		{"example.com", "--ip", "bad"}, {"192.0.2.1", "--ip", "192.0.2.2"}, {"example.com", "--ip"},
		{"example.com", "--network", "icmp"}, {"example.com", "--network", "udp", "--network", "tcp"},
		{"example.com", "--process", "/bin/app"}, {"example.com", "--process", "bad\nname"},
		{"example.com", "--unexpected", "true"}, {"example.com", "extra"},
	} {
		out.Reset()
		errs.Reset()
		args := append([]string{"--json", "rules", "test"}, tail...)
		if code := Run(args, &out, &errs, "test", "test"); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), `"code":"usage"`) || !strings.Contains(errs.String(), "Usage: nagi rules test") {
			t.Fatalf("args=%q code=%d out=%s err=%s", args, code, out.String(), errs.String())
		}
	}
	args := []string{"rules", "test", "Example.COM.", "--ip", "::ffff:192.0.2.1", "--port", "80", "--network", "udp", "--process", "Browser App"}
	req, err := ruleTestArguments(args)
	if err != nil || req.Target != "example.com" || req.IP != "192.0.2.1" || req.Port != 80 || req.Network != "udp" || req.Process != "Browser App" {
		t.Fatalf("request=%+v err=%v", req, err)
	}
	if configMutation(args) || transactionalMutation(args) {
		t.Fatal("a rule test must not enter the configuration transaction")
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, _ := completion(shell)
		if !strings.Contains(script, "test") {
			t.Fatalf("missing rule test completion in %s", shell)
		}
	}
}

func TestRuleTestCLIContract(t *testing.T) {
	root, err := os.MkdirTemp("", "nrt-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(paths.RuntimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	status := http.StatusOK
	var mu sync.Mutex
	var requests []rules.TestRequest
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/rules/test" {
			t.Errorf("unexpected operation %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var req rules.TestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		requests = append(requests, req)
		code := status
		mu.Unlock()
		w.WriteHeader(code)
		if code != http.StatusOK {
			message := "unsupported"
			if code == http.StatusUnprocessableEntity {
				message = "DNS resolution required by a rule failed; supply a destination IP to test without DNS"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
			return
		}
		_ = json.NewEncoder(w).Encode(rules.TestResult{Target: req.Target, Port: req.Port, Network: req.Network, Mode: "rule", Matched: true, Rule: "RuleSet", RulePayload: "nagi-personal", Providers: []string{"nagi-personal"}, Outbound: "DIRECT", Warnings: []string{"Synthetic destination test"}})
	})}
	go server.Serve(listener)
	defer server.Close()
	for _, jsonMode := range []bool{false, true} {
		var out, errs bytes.Buffer
		args := []string{"rules", "test", "154.19.187.117"}
		if jsonMode {
			args = append([]string{"--json"}, args...)
		}
		if code := Run(args, &out, &errs, "test", "test"); code != 0 || errs.Len() != 0 {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), errs.String())
		}
		if jsonMode {
			var response struct {
				OK   bool             `json:"ok"`
				Data rules.TestResult `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil || !response.OK || response.Data.RulePayload != "nagi-personal" || response.Data.Outbound != "DIRECT" {
				t.Fatalf("JSON: %s %v", out.String(), err)
			}
		} else if !strings.Contains(out.String(), "Matched: RuleSet nagi-personal -> DIRECT") {
			t.Fatalf("text: %s", out.String())
		}
	}
	mu.Lock()
	if len(requests) != 2 || requests[0].Port != 443 || requests[0].Network != "tcp" {
		t.Fatalf("requests: %+v", requests)
	}
	mu.Unlock()
	for _, code := range []int{404, 405, 422} {
		mu.Lock()
		status = code
		mu.Unlock()
		var out, errs bytes.Buffer
		if exit := Run([]string{"--json", "rules", "test", "192.0.2.1"}, &out, &errs, "test", "test"); exit != 1 || out.Len() != 0 || !strings.Contains(errs.String(), `"code":"rule_error"`) {
			t.Fatalf("status=%d out=%s err=%s", code, out.String(), errs.String())
		}
		if code == 422 && !strings.Contains(errs.String(), "DNS resolution required by a rule failed; supply a destination IP to test without DNS") {
			t.Fatalf("status=%d did not preserve mihomo error: %s", code, errs.String())
		}
		if code != 422 && !strings.Contains(errs.String(), "does not support rule testing") {
			t.Fatal(errs.String())
		}
	}
}
