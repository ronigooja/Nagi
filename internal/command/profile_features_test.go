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
	config := filepath.Join(root, "config", "nagi")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "profiles", "work.yaml"), []byte("mixed-port: 17890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", bin)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
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
