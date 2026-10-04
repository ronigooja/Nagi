package command

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfileUseRepairsInvalidSelectionThroughCLI(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses isolated Linux XDG paths")
	}
	root := t.TempDir()
	for key, dir := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_RUNTIME_DIR": "run"} {
		t.Setenv(key, filepath.Join(root, dir))
	}
	config := filepath.Join(root, "config", "nagi")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	invalid := []byte("profile: invalid/name\n")
	settings := filepath.Join(config, "settings.yaml")
	if err := os.WriteFile(settings, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"profile", "list"}, &stdout, &stderr, "test", "test"); code != 0 || !strings.Contains(stdout.String(), "Warning:") {
		t.Fatalf("empty list omitted invalid selection: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if err := os.WriteFile(filepath.Join(config, "profiles", "work.yaml"), []byte("mode: rule\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n"))
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "profile", "use", "work"}, &stdout, &stderr, "test", "test"); code != 0 || !strings.Contains(stdout.String(), `"current":"work"`) {
		t.Fatalf("recovery failed: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	current, err := os.ReadFile(settings)
	if err != nil || string(current) != "profile: work\n" {
		t.Fatalf("selection=%q err=%v", current, err)
	}
	previous, err := os.ReadFile(settings + ".bak")
	if err != nil || !bytes.Equal(previous, invalid) {
		t.Fatalf("original selection was not backed up: %q, %v", previous, err)
	}
}
