package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeChainHelpAndPreflightWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	for _, path := range []string{"proxy node", "proxy node list", "proxy node show", "proxy node add", "proxy node edit", "proxy node remove", "proxy node groups", "proxy node groups show", "proxy node groups set", "proxy node groups clear", "proxy chain", "proxy chain list", "proxy chain show", "proxy chain candidates", "proxy chain set", "proxy chain clear"} {
		var out, errs bytes.Buffer
		args := append(strings.Fields(path), "--help")
		if code := Run(args, &out, &errs, "test", "test"); code != 0 || !strings.Contains(out.String(), path) {
			t.Fatalf("help %s: %d %s", path, code, &errs)
		}
	}
	for _, args := range [][]string{
		{"proxy", "tailscale", "list"}, {"proxy", "node", "add"}, {"proxy", "node", "add", "tailscale", "DIRECT", "exit", "--auth-key-stdin"},
		{"proxy", "node", "add", "tailscale", "Tail", "exit", "PRIVATE"}, {"proxy", "node", "edit", "Tail"}, {"proxy", "node", "edit", "Tail", "--hostname", "one", "--hostname", "two"},
		{"proxy", "node", "groups", "set", "Tail"}, {"proxy", "chain", "set", "node", "Target"}, {"proxy", "chain", "set", "node", "Target", "--via"},
		{"proxy", "chain", "clear", "node", "Target", "--via", "Tail"}, {"proxy", "chain", "show", "invalid", "Target"}, {"proxy", "chain", "show", "node", "A", "B"},
		{"proxy", "chain", "set", "node", "A", "A", "--via", "Tail"}, {"completion", "candidates", "tailscale-nodes"},
	} {
		var out, errs bytes.Buffer
		code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "test")
		if code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) || strings.Contains(errs.String(), "PRIVATE") {
			t.Fatalf("preflight %v: %d %s", args, code, &errs)
		}
	}
}
func nodeChainFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte("proxies: [{name: First, type: socks5, server: localhost, port: 1080}, {name: Second, type: socks5, server: localhost, port: 1081}, {name: Target A, type: socks5, server: localhost, port: 1082}, {name: Target B, type: socks5, server: localhost, port: 1083}]\nproxy-groups: [{name: Choice, type: select, proxies: [DIRECT]}]\nproxy-providers: {Feed: {type: inline, payload: []}}\n")
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "default.yaml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n"))
	return paths.ConfigDir
}
func runNodeChainJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out, errs bytes.Buffer
	if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "test"); code != 0 {
		t.Fatalf("%v: %d %s", args, code, &errs)
	}
	var env struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil || !env.OK {
		t.Fatalf("invalid envelope: %v %s", err, &out)
	}
	return env.Data
}
func TestNodeChainOfflineContractDryRunAndBatch(t *testing.T) {
	config := nodeChainFixture(t)
	nodes := runNodeChainJSON(t, "proxy", "node", "list")
	if len(nodes["nodes"].([]any)) != 4 || len(nodes["group_candidates"].([]any)) != 1 {
		t.Fatal(nodes)
	}
	runNodeChainJSON(t, "proxy", "node", "groups", "set", "First", "Choice")
	groups := runNodeChainJSON(t, "proxy", "node", "groups", "show", "First")["groups"].(map[string]any)
	if groups["explicit"].([]any)[0] != "Choice" {
		t.Fatal(groups)
	}
	runNodeChainJSON(t, "proxy", "node", "groups", "clear", "First")
	before, err := os.ReadFile(filepath.Join(config, "overrides", "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	preview := runNodeChainJSON(t, "proxy", "chain", "set", "node", "Target A", "Target B", "--via", "First", "--via", "Second", "--dry-run")
	if preview["saved"] != false || preview["dry_run"] != true || preview["reloaded"] != false {
		t.Fatal(preview)
	}
	after, _ := os.ReadFile(filepath.Join(config, "overrides", "default.yaml"))
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run wrote configuration")
	}
	result := runNodeChainJSON(t, "proxy", "chain", "set", "node", "Target A", "Target B", "--via", "First", "--via", "Second")
	if result["saved"] != true || result["reloaded"] != false {
		t.Fatal(result)
	}
	target := runNodeChainJSON(t, "proxy", "chain", "show", "node", "Target A")["chain"].(map[string]any)
	via := target["via"].([]any)
	if len(via) != 2 || via[0] != "First" || via[1] != "Second" {
		t.Fatal(target)
	}
	runNodeChainJSON(t, "proxy", "chain", "clear", "node", "Target A", "Target B")
}

func TestNodeChainMutationLocksAndFailureRollback(t *testing.T) {
	for _, args := range [][]string{{"proxy", "node", "add", "tailscale", "Tail", "exit"}, {"proxy", "node", "groups", "set", "Tail", "Choice"}, {"proxy", "chain", "set", "node", "Target", "--via", "First"}, {"proxy", "chain", "clear", "node", "Target"}} {
		if !configMutation(args) || !transactionalMutation(args) {
			t.Fatalf("mutation lacks lock/transaction: %v", args)
		}
	}
	for _, args := range [][]string{{"proxy", "node", "list"}, {"proxy", "node", "groups", "show", "Tail"}, {"proxy", "chain", "candidates", "node", "Target"}, {"proxy", "chain", "set", "node", "Target", "--via", "First", "--dry-run"}} {
		if configMutation(args) || transactionalMutation(args) {
			t.Fatalf("read enters mutation transaction: %v", args)
		}
	}
	config := nodeChainFixture(t)
	runNodeChainJSON(t, "proxy", "chain", "set", "node", "Target A", "Target B", "--via", "First")
	path := filepath.Join(config, "overrides", "default.yaml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "reject", "#!/bin/sh\necho PRIVATE-secret >&2\nexit 1\n"))
	var out, errs bytes.Buffer
	code := Run([]string{"--json", "proxy", "chain", "set", "node", "Target A", "Target B", "--via", "Second"}, &out, &errs, "test", "test")
	if code != 1 || !strings.Contains(errs.String(), `"code":"proxy_chain_error"`) || strings.Contains(errs.String(), "PRIVATE-secret") {
		t.Fatalf("failure contract: %d %s", code, &errs)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed batch changed configuration: %v", err)
	}
}
