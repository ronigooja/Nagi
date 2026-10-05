package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModeSaveUsesExistingOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	config := filepath.Join(root, "config", "nagi")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(config, "overrides"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	override := filepath.Join(config, "overrides", "default.yaml")
	if err := os.WriteFile(source, []byte("mode: rule\nproxies: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("mixed-port: 7890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	validator := filepath.Join(root, "mihomo")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", validator)
	var out, errs bytes.Buffer
	if code := Run([]string{"--json", "mode", "save", "global"}, &out, &errs, "test", "commit"); code != 0 {
		t.Fatalf("save code=%d stderr=%s", code, errs.String())
	}
	sourceData, _ := os.ReadFile(source)
	overrideData, _ := os.ReadFile(override)
	if !strings.Contains(string(sourceData), "mode: rule") || !strings.Contains(string(overrideData), "mode: global") || !strings.Contains(string(overrideData), "mixed-port: 7890") {
		t.Fatalf("source=%q override=%q", sourceData, overrideData)
	}
	out.Reset()
	errs.Reset()
	if code := Run([]string{"--json", "mode", "saved"}, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), `"mode":"global"`) {
		t.Fatalf("saved code=%d out=%s err=%s", code, out.String(), errs.String())
	}
}
