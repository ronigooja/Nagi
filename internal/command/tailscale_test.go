package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailscaleHelpAndPreflightWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	for _, action := range []string{"list", "show", "targets", "add", "edit", "bind", "unbind", "remove"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"proxy", "tailscale", action, "--help"}, &out, &errs, "test", "test"); code != 0 || !strings.Contains(out.String(), "proxy tailscale "+action) {
			t.Fatalf("help %s: %d %s", action, code, errs.String())
		}
	}
	for _, args := range [][]string{
		{"proxy", "tailscale", "add"},
		{"proxy", "tailscale", "add", "DIRECT", "exit", "PRIVATE"},
		{"proxy", "tailscale", "add", "Tail", "", "PRIVATE"},
		{"proxy", "tailscale", "add", "Tail", "exit", "PRIVATE", "--control-url", "https://user:PRIVATE@example.com"},
		{"proxy", "tailscale", "add", "Tail", "exit", "PRIVATE", "--control-url", "http://example.com"},
		{"proxy", "tailscale", "add", "Tail", "exit", "PRIVATE", "--hostname"},
		{"proxy", "tailscale", "edit", "Tail"},
		{"proxy", "tailscale", "edit", "Tail", "--hostname", "one", "--hostname", "two"},
		{"proxy", "tailscale", "bind", "Tail", "invalid", "Target"},
		{"proxy", "tailscale", "unbind", "invalid", "Target"},
	} {
		var out, errs bytes.Buffer
		code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "test")
		if code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) || strings.Contains(errs.String(), "PRIVATE") {
			t.Fatalf("preflight=%d %s", code, errs.String())
		}
	}
}
func TestTailscaleStoppedCLIQueriesAndMutations(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte("proxies: [{name: Target, type: socks5, server: localhost, port: 1080}]\nproxy-providers: {remote: {type: http}}\n")
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	key := filepath.Join(root, "PRIVATE-key-file")
	if err := os.WriteFile(key, []byte("tskey-PRIVATE-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) map[string]any {
		t.Helper()
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json", "proxy", "tailscale"}, args...), &out, &errs, "test", "test"); code != 0 {
			t.Fatalf("%v code=%d %s", args, code, errs.String())
		}
		if strings.Contains(out.String(), "PRIVATE") {
			t.Fatal("secret leaked", out.String())
		}
		var envelope struct {
			OK   bool           `json:"ok"`
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(out.Bytes(), &envelope) != nil || !envelope.OK {
			t.Fatal(out.String())
		}
		return envelope.Data
	}
	added := run("add", "My Tail", "exit", key)
	if added["profile"] != "work" || added["action"] != "add" || added["saved"] != true || added["reloaded"] != false || len(added) != 5 {
		t.Fatal(added)
	}
	run("edit", "My Tail", "--hostname", "device")
	run("bind", "My Tail", "node", "Target")
	run("bind", "My Tail", "provider", "remote")
	t.Setenv("NAGI_MIHOMO_BIN", "/missing/mihomo")
	nodes := run("list")
	if len(nodes["nodes"].([]any)) != 1 {
		t.Fatal(nodes)
	}
	shown := run("show", "My Tail")
	if len(shown["bindings"].([]any)) != 2 {
		t.Fatal(shown)
	}
	targets := run("targets")
	if len(targets["targets"].([]any)) != 2 {
		t.Fatal(targets)
	}
	var out, errs bytes.Buffer
	if code := Run([]string{"completion", "candidates", "tailscale-nodes"}, &out, &errs, "test", "test"); code != 0 || out.String() != "My Tail\n" {
		t.Fatalf("offline names: %d %s %s", code, out.String(), errs.String())
	}
	out.Reset()
	if code := Run([]string{"completion", "candidates", "tailscale-targets", "provider"}, &out, &errs, "test", "test"); code != 0 || out.String() != "remote\n" {
		t.Fatalf("offline target names: %d %s %s", code, out.String(), errs.String())
	}
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	run("unbind", "node", "Target")
	run("unbind", "provider", "remote")
	run("remove", "My Tail")
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("stopped mutation launched engine")
	}
	actual, _ := os.ReadFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"))
	if string(actual) != string(source) {
		t.Fatal("source changed")
	}
}
func TestTailscaleValidationFailurePrivacyAndRollback(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "default.yaml"), []byte("proxies: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\necho '{\"errors\":[{\"path\":\"proxies\",\"code\":\"bad\",\"message\":\"PRIVATE-key-content\"}]}'\nexit 1\n")
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	key := filepath.Join(root, "PRIVATE-key-path")
	if err := os.WriteFile(key, []byte("PRIVATE-key-content"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	code := Run([]string{"--json", "proxy", "tailscale", "add", "Tail", "exit", key}, &out, &errs, "test", "test")
	if code != 1 || !strings.Contains(errs.String(), `"code":"proxy_tailscale_error"`) || strings.Contains(errs.String(), "PRIVATE") {
		t.Fatalf("validation privacy: %d %s", code, errs.String())
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "overrides", "default.yaml")); !os.IsNotExist(err) {
		t.Fatal("failed mutation persisted")
	}
}
func TestTailscaleTransactionExcludesOnlyIdentitySubtree(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, ".tailscale-state", "identity")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "secret"), []byte("old identity"), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "settings.yaml")
	if err := os.WriteFile(source, []byte("old config"), 0600); err != nil {
		t.Fatal(err)
	}
	images, err := snapshotConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatal("identity entered rollback snapshot")
	}
	if err := os.WriteFile(filepath.Join(state, "secret"), []byte("new identity"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreConfig(root, images); err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(filepath.Join(state, "secret"))
	if string(value) != "new identity" {
		t.Fatal("rollback overwrote live identity")
	}
	value, _ = os.ReadFile(source)
	if string(value) != "old config" {
		t.Fatal("configuration was not restored")
	}
	for _, action := range []string{"add", "edit", "bind", "unbind", "remove"} {
		args := []string{"proxy", "tailscale", action}
		if !configMutation(args) || !transactionalMutation(args) {
			t.Fatal("mutation omitted lock or transaction", action)
		}
	}
	for _, action := range []string{"list", "show", "targets"} {
		args := []string{"proxy", "tailscale", action}
		if configMutation(args) || transactionalMutation(args) {
			t.Fatal("query treated as mutation", action)
		}
	}
}
