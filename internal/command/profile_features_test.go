package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileFileCommandsAndOverrideCLI(t *testing.T) {
	root := t.TempDir()
	config := isolatedTestPaths(t, root).ConfigDir
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "profiles", "work.yaml"), []byte("mixed-port: 17890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", bin)
	run := func(args ...string) map[string]any {
		t.Helper()
		var out, errout bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errout, "test", "commit"); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, errout.String())
		}
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Data
	}
	export := filepath.Join(root, "export.yaml")
	if got := run("profile", "export", "work", export); got["exported"] != true {
		t.Fatal(got)
	}
	if got := run("profile", "backup", "work"); got["backed_up"] != true {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(config, "profiles", "work.yaml"), []byte("mixed-port: 17892\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := run("profile", "diff", "work"); got["changed"] != true || !strings.Contains(got["diff"].(string), "17890") {
		t.Fatal(got)
	}
	if got := run("profile", "restore", "work"); got["restored"] != true {
		t.Fatal(got)
	}
	patch := filepath.Join(root, "local.yaml")
	if err := os.WriteFile(patch, []byte("mixed-port: 17891\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := run("profile", "override", "set", "work", patch); got["override_saved"] != true {
		t.Fatal(got)
	}
	if got := run("profile", "override", "show", "work"); !strings.Contains(got["yaml"].(string), "17891") {
		t.Fatal(got)
	}
	if got := run("profile", "use", "work"); got["current"] != "work" {
		t.Fatal(got)
	}
	effectivePath := filepath.Join(config, "profiles", ".effective-work.yaml")
	effective, err := os.ReadFile(effectivePath)
	if err != nil || !strings.Contains(string(effective), "17891") {
		t.Fatalf("effective = %q, %v", effective, err)
	}
	if got := run("config", "validate"); got["valid"] != true {
		t.Fatal(got)
	}
	if got := run("profile", "override", "clear", "work"); got["override_cleared"] != true {
		t.Fatal(got)
	}
	var out, errout bytes.Buffer
	if code := Run([]string{"--json", "profile", "export", "work", export}, &out, &errout, "test", "commit"); code != 1 || !strings.Contains(errout.String(), "profile_error") {
		t.Fatalf("overwrite exit=%d stderr=%s", code, errout.String())
	}
}

func TestApplyDefaultOverrideCLI(t *testing.T) {
	root := t.TempDir()
	config := isolatedTestPaths(t, root).ConfigDir
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	defaultSource := "mixed-port: 17891\ndns:\n  enable: true\nproxies:\n  - name: Default Node\n    type: direct\nproxy-groups:\n  - name: Default Group\n    type: select\n    proxies: [Default Node]\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(filepath.Join(config, "profiles", "default.yaml"), []byte(defaultSource), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "profiles", "work.yaml"), []byte("mixed-port: 18000\nproxies:\n  - name: Subscription Node\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\nproxy-groups:\n  - name: Work Group\n    type: select\n    proxies: [Subscription Node, DIRECT]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", bin)
	run := func(args ...string) map[string]any {
		t.Helper()
		var out, errout bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errout, "test", "commit"); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, errout.String())
		}
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Data
	}
	if got := run("profile", "override", "apply-default", "work", "--dns-proxy", "Work Group"); got["default_override_applied"] != true || got["name"] != "work" {
		t.Fatal(got)
	}
	if got := run("profile", "override", "default-status", "work"); got["default_override_applied"] != true || got["dns_proxy_group"] != "Work Group" {
		t.Fatal(got)
	}
	override, err := os.ReadFile(filepath.Join(config, "overrides", "work.yaml"))
	if err != nil || !strings.Contains(string(override), "17891") || !strings.Contains(string(override), "enable: true") {
		t.Fatalf("work override = %q, %v", override, err)
	}
	if strings.Contains(string(override), "Default Node") || strings.Contains(string(override), "Default Group") || strings.Contains(string(override), "MATCH,DIRECT") {
		t.Fatalf("subscription fields copied into work override: %q", override)
	}
	source, err := os.ReadFile(filepath.Join(config, "profiles", "work.yaml"))
	if err != nil || !strings.Contains(string(source), "Subscription Node") || strings.Contains(string(source), "17891") {
		t.Fatalf("work source = %q, %v", source, err)
	}
	if got := run("profile", "override", "restore-default", "work"); got["default_override_restored"] != true {
		t.Fatal(got)
	}
	if got := run("profile", "override", "default-status", "work"); got["default_override_applied"] != false {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(config, "overrides", "work.yaml")); !os.IsNotExist(err) {
		t.Fatalf("override after restore: %v", err)
	}
}

func TestApplyDefaultDNSProxySyntaxBeforeRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{
		{"profile", "override", "apply-default", "work", "--dns-proxy"},
		{"profile", "override", "apply-default", "work", "--other", "Proxy"},
		{"profile", "override", "apply-default", "work", "--dns-proxy", ""},
	} {
		var out, errout bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errout, "test", "commit"); code != 2 || !strings.Contains(errout.String(), `"code":"usage"`) || !strings.Contains(errout.String(), "--dns-proxy GROUP") {
			t.Fatalf("args=%q exit=%d stderr=%s", args, code, errout.String())
		}
	}
	if err := validateInvocation([]string{"profile", "override", "apply-default", "work", "--dns-proxy", "Proxy Group"}); err != nil {
		t.Fatal(err)
	}
	if err := validateInvocation([]string{"completion", "candidates", "profile-groups", "work"}); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := Run([]string{"profile", "override", "apply-default", "--help"}, &out, &errout, "test", "commit"); code != 0 || !strings.Contains(out.String(), "--dns-proxy GROUP") {
		t.Fatalf("help exit=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
}
