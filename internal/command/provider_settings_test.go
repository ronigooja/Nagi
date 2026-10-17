package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderSettingsHelpAndPreflight(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/nonexistent/mihomo")
	for _, path := range [][]string{{"proxy", "healthcheck", "url"}, {"proxy", "healthcheck", "url", "set"}, {"proxy", "healthcheck", "url", "reset"}, {"proxy", "provider", "interval"}, {"proxy", "provider", "interval", "set"}, {"proxy", "provider", "interval", "reset"}, {"proxy", "provider", "interval", "show"}} {
		var out, errs bytes.Buffer
		if code := Run(append(path, "--help"), &out, &errs, "test", "commit"); code != 0 {
			t.Fatal(path, code, errs.String())
		}
	}
	for _, args := range [][]string{{"proxy", "healthcheck", "url", "set", "group", "Auto", "file:///secret"}, {"proxy", "healthcheck", "url", "set", "invalid", "Auto", "https://example.com"}, {"proxy", "provider", "interval", "set", "work", "0"}, {"proxy", "provider", "add", "work", "https://example.com", "Auto", "--test-url"}} {
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) {
			t.Fatal(args, code, errs.String())
		}
	}
}

func TestProviderSettingsCLIStoppedPersistenceAndRollback(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700)
	os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"), []byte("proxy-providers:\n  feed:\n    type: http\n    url: https://feed.example/nodes\n    interval: 600\n    health-check:\n      enable: true\n      url: https://original.example/204\n      interval: 300\nproxy-groups:\n  - name: Auto\n    type: url-test\n    use: [feed]\n    url: https://original.example/204\n"), 0600)
	os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600)
	validator := filepath.Join(root, "mihomo")
	os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700)
	t.Setenv("NAGI_MIHOMO_BIN", validator)
	run := func(args ...string) string {
		t.Helper()
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 0 {
			t.Fatal(args, code, errs.String())
		}
		return out.String()
	}
	out := run("proxy", "provider", "interval", "set", "feed", "off")
	if !strings.Contains(out, `"enabled":false`) || !strings.Contains(out, `"can_reset":true`) || !strings.Contains(out, `"source_interval_seconds":600`) {
		t.Fatal(out)
	}
	out = run("proxy", "healthcheck", "url", "set", "provider", "feed", "https://new.example/PRIVATE?token=SECRET")
	if !strings.Contains(out, `"url_source":"override"`) || strings.Contains(out, "SECRET") || strings.Contains(out, "PRIVATE") {
		t.Fatal(out)
	}
	out = run("proxy", "healthcheck", "show", "provider", "feed")
	if !strings.Contains(out, `"interval_seconds":300`) {
		t.Fatal(out)
	}
	out = run("proxy", "provider", "interval", "reset", "feed")
	if !strings.Contains(out, `"interval_seconds":600`) {
		t.Fatal(out)
	}
	override := filepath.Join(paths.ConfigDir, "overrides", "work.yaml")
	before, _ := os.ReadFile(override)
	os.WriteFile(validator, []byte("#!/bin/sh\nexit 1\n"), 0700)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "proxy", "healthcheck", "url", "reset", "provider", "feed"}, &stdout, &stderr, "test", "commit"); code != 1 {
		t.Fatal(code, stderr.String())
	}
	after, _ := os.ReadFile(override)
	if string(before) != string(after) {
		t.Fatal("failed URL reset changed disk")
	}
	t.Setenv("NAGI_MIHOMO_BIN", "/nonexistent/mihomo")
	run("proxy", "provider", "interval", "show", "feed")
}

func TestProviderSettingsUseConfigurationTransaction(t *testing.T) {
	for _, args := range [][]string{{"proxy", "healthcheck", "url", "set", "provider", "feed", "https://example.com"}, {"proxy", "healthcheck", "url", "reset", "group", "Auto"}, {"proxy", "provider", "interval", "set", "feed", "off"}, {"proxy", "provider", "interval", "reset", "feed"}} {
		if !configMutation(args) || !transactionalMutation(args) {
			t.Fatal("missing transaction", args)
		}
	}
	args := []string{"proxy", "provider", "interval", "show", "feed"}
	if configMutation(args) || transactionalMutation(args) {
		t.Fatal("read uses mutation", args)
	}
}
