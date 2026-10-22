package command

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyGroupSyntaxAndHelpWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/missing")
	for _, tail := range [][]string{
		{"add", "New", "--type", "relay"}, {"edit", "New"}, {"add", "New", "--node", "DIRECT", "--node", "DIRECT"},
		{"add", "New", "--node", "DIRECT", "--clear-nodes"}, {"add", "New", "--clear-nodes", "--node", "DIRECT"},
		{"add", "New", "--interval", "-1"}, {"add", "New", "--tolerance", "65536"}, {"add", "New", "--unknown", "value"},
		{"remove", "GLOBAL"}, {"list", "extra"}, {"add", "New", "--type"},
	} {
		var out, errs bytes.Buffer
		args := append([]string{"--json", "proxy", "group"}, tail...)
		if code := Run(args, &out, &errs, "test", "test"); code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) {
			t.Fatalf("%v code=%d err=%s", tail, code, errs.String())
		}
	}
	for _, action := range []string{"list", "show", "add", "edit", "remove"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"proxy", "group", action, "--help"}, &out, &errs, "test", "test"); code != 0 || !strings.Contains(out.String(), "proxy group") {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), errs.String())
		}
	}
}
func TestProxyGroupStoppedCommandCRUDAndFailure(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := "proxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n"
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	validator := filepath.Join(root, "mihomo")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", validator)
	run := func(tail ...string) (int, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := Run(append([]string{"--json", "proxy", "group"}, tail...), &out, &errs, "test", "test")
		return code, out.String() + errs.String()
	}
	for _, args := range [][]string{{"add", "My Group", "--type", "url-test", "--node", "DIRECT", "--tolerance", "42"}, {"list"}, {"show", "My Group"}, {"edit", "My Group", "--type", "select"}} {
		code, out := run(args...)
		if code != 0 {
			t.Fatalf("%v code=%d: %s", args, code, out)
		}
		if args[0] == "show" && (!strings.Contains(out, `"tolerance":42`) || !strings.Contains(out, `"interval":600`)) {
			t.Fatal(out)
		}
	}
	choicePath := filepath.Join(paths.ConfigDir, "proxy-selections.json")
	choices := []byte(`{"work":{"My Group":"DIRECT"}}`)
	if err := os.WriteFile(choicePath, choices, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(paths.ConfigDir, "overrides", "work.yaml"))
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if code, out := run("remove", "My Group"); code != 1 || !strings.Contains(out, `"code":"proxy_group_error"`) {
		t.Fatalf("code=%d %s", code, out)
	}
	after, _ := os.ReadFile(filepath.Join(paths.ConfigDir, "overrides", "work.yaml"))
	if !bytes.Equal(before, after) {
		t.Fatal("failed delete changed override")
	}
	retainedChoices, _ := os.ReadFile(choicePath)
	if !bytes.Equal(retainedChoices, choices) {
		t.Fatal("failed deletion lost saved selection")
	}
	// Offline reads do not need even an installed validator.
	t.Setenv("NAGI_MIHOMO_BIN", "/missing")
	if code, out := run("list"); code != 0 {
		t.Fatalf("offline read: %s", out)
	}
	t.Setenv("NAGI_MIHOMO_BIN", validator)
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if code, out := run("remove", "My Group"); code != 0 {
		t.Fatalf("code=%d %s", code, out)
	}
	retainedChoices, _ = os.ReadFile(choicePath)
	if strings.Contains(string(retainedChoices), "My Group") {
		t.Fatal("deleted group's selection retained")
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("engine started", err)
	}
	data, _ := os.ReadFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"))
	if string(data) != source {
		t.Fatal("changed source")
	}
}

func TestProxyGroupCompletionPreservesSpacedLocalNames(t *testing.T) {
	script, _ := completion("bash")
	probe := `nagi() { case "$3" in node-groups) printf '%s\n' 'My Group';; proxy-nodes) printf '%s\n' 'My Node';; chain-targets) printf '%s\n' 'My Feed';; esac; }
` + script + `
COMP_WORDS=(nagi proxy group ''); COMP_CWORD=3; _nagi_complete; printf 'ACTIONS:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy group edit My); COMP_CWORD=4; _nagi_complete; printf 'GROUP:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy group add New --node My); COMP_CWORD=6; _nagi_complete; printf 'NODE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi proxy group add New --provider My); COMP_CWORD=6; _nagi_complete; printf 'PROVIDER:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy group order First My); COMP_CWORD=5; _nagi_complete; printf 'ORDER:%s:%s\n' "${#COMPREPLY[@]}" "${COMPREPLY[0]}"
COMP_WORDS=(nagi proxy group add New --type ''); COMP_CWORD=6; _nagi_complete; printf 'TYPES:%s\n' "${COMPREPLY[*]}"
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, expected := range []string{"ACTIONS:list show add edit remove order", "GROUP:1:My Group", "ORDER:1:My Group", "NODE:My Node My Group", "PROVIDER:1:My Feed", "TYPES:select url-test fallback load-balance"} {
		if !strings.Contains(string(out), expected) {
			t.Fatalf("missing %s: %s", expected, out)
		}
	}
}
