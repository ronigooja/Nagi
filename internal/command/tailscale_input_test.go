package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/menu"
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
		code := RunWithInput(append([]string{"--json", "proxy", "tailscale"}, args...), strings.NewReader(input), &out, &errs, "test", "test")
		if code != 0 || strings.Contains(out.String()+errs.String(), "tskey-") {
			t.Fatalf("result %d: %s %s", code, out.String(), errs.String())
		}
	}
	run(" "+key+"\n", "add", "Paste Tail", "exit", "--auth-key-stdin")
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
		code := RunWithInput([]string{"--json", "proxy", "tailscale", "edit", "Paste Tail", "--auth-key-stdin"}, strings.NewReader(input), &out, &errs, "test", "test")
		if code != 1 || !strings.Contains(errs.String(), `"code":"proxy_tailscale_error"`) || strings.Contains(out.String()+errs.String(), "PRIVATE") {
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
		{"proxy", "tailscale", "add", "Tail", "exit"},                                             // nonterminal paste
		{"--json", "proxy", "tailscale", "add", "Tail", "exit", "--hostname", "--auth-key-stdin"}, // option value, not stdin mode
		{"--json", "proxy", "tailscale", "edit", "Tail", "--auth-key"},
		{"--json", "proxy", "tailscale", "add", "Tail", "exit", "file", "--auth-key-stdin"},
		{"--json", "proxy", "tailscale", "edit", "Tail", "--auth-key", "--auth-key-file", "file"},
		{"--json", "proxy", "tailscale", "edit", "Tail", "--auth-key-stdin", "--auth-key-stdin"},
		{"--json", "proxy", "tailscale", "edit", "Tail", "--auth-key", "PRIVATE"},
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
	if code := RunWithInput([]string{"proxy", "tailscale", "add", "--help"}, input, &out, &errs, "test", "test"); code != 0 || input.reads != 0 {
		t.Fatal("help read input", code)
	}
	out.Reset()
	errs.Reset()
	if code := RunWithInput([]string{"--json", "proxy", "tailscale", "add", "Tail", "exit", "--auth-key-stdin"}, input, &out, &errs, "test", "test"); code != 1 || strings.Contains(errs.String(), "PRIVATE") {
		t.Fatal("input failure was leaked", code, errs.String())
	}
}

func TestTailscaleMenuPasteUsesRealCommandInputChannel(t *testing.T) {
	config := tailscalePasteFixture(t)
	const key = "tskey-MENU-fixture"
	runner := menu.NewInputCommandRunner(func(args []string, input io.Reader, out, errOut io.Writer) int {
		for _, arg := range args {
			if strings.Contains(arg, key) {
				t.Fatal("menu passed key in argv")
			}
		}
		return RunWithInput(args, input, out, errOut, "test", "test")
	})
	var out bytes.Buffer
	input := "1\n3\n1\nPaste Tail\n100.64.0.1\n\n\n" + key + "\n0\n0\n0\n"
	if code := menu.Run(context.Background(), menu.NewSession(runner, strings.NewReader(input), &out)); code != 0 {
		t.Fatal(code)
	}
	data, err := os.ReadFile(filepath.Join(config, "overrides", "work.yaml"))
	if err != nil || !bytes.Contains(data, []byte(key)) {
		t.Fatalf("menu did not save pasted key: %v; %s", err, out.String())
	}
	if strings.Contains(out.String(), key) || !strings.Contains(out.String(), "input is visible") {
		t.Fatal("unexpected menu result output", out.String())
	}
}
