package command

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
)

func tailscalePasteFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "profiles", "work.yaml"), []byte("proxies: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n"))
	return paths.ConfigDir
}

func TestTailscaleStdinPastePersistsAndKeepsPublicResultsRedacted(t *testing.T) {
	config := tailscalePasteFixture(t)
	key := "tskey-PASTE-fixture"
	run := func(input string, args ...string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := RunWithInput(append([]string{"--json", "proxy", "node"}, args...), strings.NewReader(input), &out, &errs, "test", "test")
		if code != 0 || strings.Contains(out.String()+errs.String(), "tskey-") {
			t.Fatalf("result %d: %s %s", code, out.String(), errs.String())
		}
	}
	run(" "+key+"\n", "add", "tailscale", "Paste Tail", "exit", "--auth-key-stdin")
	overridePath := filepath.Join(config, "overrides", "work.yaml")
	data, err := os.ReadFile(overridePath)
	if err != nil || !bytes.Contains(data, []byte(key)) {
		t.Fatal("pasted key was not persisted", err)
	}
	info, _ := os.Stat(overridePath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("override is not private")
	}
	run("tskey-UPDATED\n", "edit", "Paste Tail", "--auth-key-stdin")
	run("", "edit", "Paste Tail", "--hostname", "new-host")
	data, err = os.ReadFile(overridePath)
	if err != nil || !bytes.Contains(data, []byte("tskey-UPDATED")) || bytes.Contains(data, []byte(key)) {
		t.Fatal("edit failed to update or retain the key", err)
	}
	run("", "show", "Paste Tail")
	before := append([]byte(nil), data...)
	for _, input := range []string{"", "one\ntwo", "PRIVATE\x00key", strings.Repeat("k", profile.MaxTailscaleAuthKeyBytes+1)} {
		var out, errs bytes.Buffer
		code := RunWithInput([]string{"--json", "proxy", "node", "edit", "Paste Tail", "--auth-key-stdin"}, strings.NewReader(input), &out, &errs, "test", "test")
		if code != 1 || !strings.Contains(errs.String(), `"code":"proxy_node_error"`) || strings.Contains(out.String()+errs.String(), "PRIVATE") {
			t.Fatalf("invalid input result: %d %s", code, errs.String())
		}
		current, err := os.ReadFile(overridePath)
		if err != nil || !bytes.Equal(current, before) {
			t.Fatal("invalid input modified the override", err)
		}
	}
}

type tailscaleUnreadableInput struct{ reads int }

func (r *tailscaleUnreadableInput) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("PRIVATE input failure")
}

func TestTailscalePastePreflightAndHelpNeverReadUnexpectedInput(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{
		{"proxy", "node", "add", "tailscale", "Tail", "exit"},                                             // nonterminal paste
		{"--json", "proxy", "node", "add", "tailscale", "Tail", "exit", "--hostname", "--auth-key-stdin"}, // option value, not stdin mode
		{"--json", "proxy", "node", "edit", "Tail", "--auth-key"},
		{"--json", "proxy", "node", "add", "tailscale", "Tail", "exit", "file", "--auth-key-stdin"},
		{"--json", "proxy", "node", "edit", "Tail", "--auth-key", "--auth-key-file", "file"},
		{"--json", "proxy", "node", "edit", "Tail", "--auth-key-stdin", "--auth-key-stdin"},
		{"--json", "proxy", "node", "edit", "Tail", "--auth-key", "PRIVATE"},
	} {
		input := &tailscaleUnreadableInput{}
		var out, errs bytes.Buffer
		code := RunWithInput(args, input, &out, &errs, "test", "test")
		if code != 2 || input.reads != 0 || strings.Contains(out.String()+errs.String(), "PRIVATE") {
			t.Fatalf("preflight %v: %d, reads=%d: %s", args[:3], code, input.reads, errs.String())
		}
	}
	input := &tailscaleUnreadableInput{}
	var out, errs bytes.Buffer
	if code := RunWithInput([]string{"proxy", "node", "add", "--help"}, input, &out, &errs, "test", "test"); code != 0 || input.reads != 0 {
		t.Fatal("help read input", code)
	}
	out.Reset()
	errs.Reset()
	if code := RunWithInput([]string{"--json", "proxy", "node", "add", "tailscale", "Tail", "exit", "--auth-key-stdin"}, input, &out, &errs, "test", "test"); code != 1 || strings.Contains(errs.String(), "PRIVATE") {
		t.Fatal("input failure was leaked", code, errs.String())
	}
}
