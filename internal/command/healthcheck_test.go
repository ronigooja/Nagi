package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthcheckHelpAndPreflightWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/nonexistent/mihomo")
	for _, action := range []string{"show", "set", "reset"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"proxy", "healthcheck", action, "--help"}, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "group|provider") {
			t.Fatalf("help code=%d out=%s err=%s", code, out.String(), errs.String())
		}
	}
	for _, args := range [][]string{{"proxy", "healthcheck"}, {"proxy", "healthcheck", "show"}, {"proxy", "healthcheck", "unknown"}, {"proxy", "healthcheck", "set", "node", "Auto", "300"}, {"proxy", "healthcheck", "set", "group", "Auto", "off"}, {"proxy", "healthcheck", "set", "group", "Auto", "0"}, {"proxy", "healthcheck", "set", "group", "Auto", "31536001"}} {
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) {
			t.Fatalf("args=%v code=%d err=%s", args, code, errs.String())
		}
	}
}
func TestHealthcheckStoppedCLITransactionAndOfflineRead(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	profiles := filepath.Join(paths.ConfigDir, "profiles")
	os.MkdirAll(profiles, 0700)
	source := []byte("proxy-groups:\n  - name: Auto\n    type: url-test\n    proxies: [DIRECT]\n    interval: 180\n    url: https://example.com/private?token=PRIVATE\n")
	os.WriteFile(filepath.Join(profiles, "work.yaml"), source, 0600)
	os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600)
	validator := filepath.Join(root, "mihomo")
	os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700)
	t.Setenv("NAGI_MIHOMO_BIN", validator)
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := Run(args, &out, &errs, "test", "commit")
		return code, out.String(), errs.String()
	}
	if code, out, err := run("--json", "proxy", "healthcheck", "set", "group", "Auto", "19"); code != 0 || !strings.Contains(out, `"interval_seconds":19`) || !strings.Contains(out, `"source":"override"`) || strings.Contains(out, "PRIVATE") {
		t.Fatalf("%d %s %s", code, out, err)
	}
	override := filepath.Join(paths.ConfigDir, "overrides", "work.yaml")
	before, _ := os.ReadFile(override)
	os.WriteFile(validator, []byte("#!/bin/sh\necho PRIVATE >&2\nexit 1\n"), 0700)
	if code, out, err := run("--json", "proxy", "healthcheck", "set", "group", "Auto", "20"); code != 1 || strings.Contains(err, "PRIVATE") || !strings.Contains(err, "proxy_healthcheck_error") {
		t.Fatalf("%d %s %s", code, out, err)
	}
	after, _ := os.ReadFile(override)
	if string(after) != string(before) {
		t.Fatal("failed mutation changed override")
	}
	t.Setenv("NAGI_MIHOMO_BIN", "/nonexistent/mihomo")
	if code, out, err := run("proxy", "healthcheck", "show", "group", "Auto"); code != 0 || !strings.Contains(out, "19 seconds (override)") || strings.Contains(out, "PRIVATE") {
		t.Fatalf("offline read %d %s %s", code, out, err)
	}
	if code, out, err := run("completion", "candidates", "healthcheck-targets", "group"); code != 0 || out != "Auto\n" {
		t.Fatalf("offline completion %d %s %s", code, out, err)
	}

	t.Setenv("NAGI_MIHOMO_BIN", validator)
	os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700)
	if code, out, err := run("--json", "proxy", "healthcheck", "reset", "group", "Auto"); code != 0 || !strings.Contains(out, `"interval_seconds":180`) || !strings.Contains(out, `"source":"profile"`) {
		t.Fatalf("reset %d %s %s", code, out, err)
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("engine started", err)
	}
}
func TestHealthcheckCompletions(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, ok := completion(shell)
		if !ok || !strings.Contains(script, "show set reset") || !strings.Contains(script, "group provider") {
			t.Fatalf("completion %s missing actions and kinds", shell)
		}
	}
}
