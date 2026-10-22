package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupOrderPreflightAndHelpDoNotReadRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing")
	for _, names := range [][]string{nil, {"Duplicate", "Duplicate"}, {""}, {"bad\nname"}} {
		args := append([]string{"--json", "proxy", "group", "order"}, names...)
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "test"); code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) {
			t.Fatalf("%v: %d %s", names, code, &errs)
		}
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"--lang", lang, "proxy", "group", "order", "--help"}, &out, &errs, "test", "test"); code != 0 || !strings.Contains(out.String(), "proxy group order") {
			t.Fatalf("%s: %d %s %s", lang, code, &out, &errs)
		}
	}
	if !configMutation([]string{"proxy", "group", "order", "B", "A"}) || transactionalMutation([]string{"proxy", "group", "order", "B", "A"}) {
		t.Fatal("display sorting must lock persistence without a runtime transaction")
	}
}
func TestGroupOrderCLIIsOfflinePersistsAndRejectsIncompleteLists(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte("proxy-groups: [{name: A Group, type: select, proxies: [DIRECT]}, {name: B Group, type: select, proxies: [DIRECT]}]\nrules: ['MATCH,A Group']\n")
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", "/missing")
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := Run(args, &out, &errs, "test", "test")
		return code, out.String() + errs.String()
	}
	code, text := run("--json", "proxy", "group", "order", "B Group", "A Group")
	if code != 0 {
		t.Fatalf("%d %s", code, text)
	}
	var result struct {
		Data struct {
			Order    []string `json:"group_order"`
			Reloaded bool     `json:"reloaded"`
			Saved    bool     `json:"saved"`
		}
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Data.Order, ",") != "B Group,A Group" || result.Data.Reloaded || !result.Data.Saved {
		t.Fatal(text)
	}
	override := filepath.Join(paths.ConfigDir, "overrides", "work.yaml")
	before, _ := os.ReadFile(override)
	for _, names := range [][]string{{"A Group"}, {"A Group", "Missing"}} {
		code, text = run(append([]string{"--json", "proxy", "group", "order"}, names...)...)
		if code != 1 || !strings.Contains(text, `"code":"proxy_group_error"`) {
			t.Fatalf("%d %s", code, text)
		}
		after, _ := os.ReadFile(override)
		if !bytes.Equal(before, after) {
			t.Fatal("incomplete/stale order changed override")
		}
	}
	code, text = run("proxy", "group", "order", "A Group", "B Group")
	if code != 0 || !strings.Contains(text, "display order saved") || strings.Contains(text, "stopped") {
		t.Fatalf("%d %s", code, text)
	}
	code, text = run("--json", "proxy", "group", "list")
	if code != 0 || strings.Index(text, `"name":"A Group"`) > strings.Index(text, `"name":"B Group"`) {
		t.Fatal(text)
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("engine started", err)
	}
	raw, _ := os.ReadFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"))
	if !bytes.Equal(source, raw) {
		t.Fatal("source changed")
	}
}
