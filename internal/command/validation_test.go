package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func validationStore(t *testing.T, binary string) (*profile.Store, string) {
	t.Helper()
	root := t.TempDir()
	paths := nagiruntime.Paths{
		ConfigDir: root, RuntimeDir: root,
		SocketPath: filepath.Join(root, "mihomo.sock"), PIDPath: filepath.Join(root, "mihomo.pid"),
		LockPath: filepath.Join(root, "mihomo.lock"), LogPath: filepath.Join(root, "mihomo.log"),
	}
	manager, err := engine.New(engine.Options{Binary: binary, ConfigPath: filepath.Join(root, "profiles", "default.yaml"), Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	return newProfileStore(paths, binary, control.New(paths.SocketPath), manager), root
}

func writeFakeMihomo(t *testing.T, directory, name, body string) string {
	t.Helper()
	binary := filepath.Join(directory, name)
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestProfileValidatorKeepsSourcePrivate(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\necho 'mihomo diagnostic SECRET' >&2\nexit 1\n")
	store, root := validationStore(t, binary)
	err := store.Write(context.Background(), "demo", []byte("proxy-password: SECRET\n"))
	if err == nil {
		t.Fatal("expected validation failure")
	}
	message := err.Error()
	for _, excluded := range []string{"SECRET", "mihomo diagnostic", ".nagi-validate-", root} {
		if strings.Contains(message, excluded) {
			t.Errorf("validation error exposed %q: %s", excluded, message)
		}
	}
	for _, want := range []string{"source YAML", "mihomo -t -f FILE -d DIRECTORY"} {
		if !strings.Contains(message, want) {
			t.Errorf("validation error missing %q: %s", want, message)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("validation left temporary files: %v", entries)
	}
}

func TestProfileValidatorResolvesBinaryOnPATH(t *testing.T) {
	binDir := t.TempDir()
	writeFakeMihomo(t, binDir, "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)
	store, root := validationStore(t, "mihomo")
	if err := store.Write(context.Background(), "demo", []byte("mixed-port: 17890\n")); err != nil {
		t.Fatalf("PATH executable validation failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "demo.yaml")); err != nil {
		t.Fatalf("validated profile was not saved: %v", err)
	}
}

func TestProfileValidatorMissingBinaryHint(t *testing.T) {
	store, root := validationStore(t, filepath.Join(t.TempDir(), "missing-mihomo"))
	err := store.Write(context.Background(), "demo", []byte("mixed-port: 17890\n"))
	if err == nil || !strings.Contains(err.Error(), "NAGI_MIHOMO_BIN") {
		t.Fatalf("missing executable guidance: %v", err)
	}
	if strings.Contains(err.Error(), ".nagi-validate-") {
		t.Fatalf("error mentions deleted temporary file: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "profiles", "demo.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("profile was unexpectedly saved: %v", statErr)
	}
}

func TestProfileValidatorDistinguishesLaunchFailure(t *testing.T) {
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "not an executable format\n")
	store, root := validationStore(t, binary)
	err := store.Write(context.Background(), "demo", []byte("mixed-port: 17890\n"))
	if err == nil || !strings.Contains(err.Error(), "could not run mihomo validation") || !strings.Contains(err.Error(), "executable permissions") {
		t.Fatalf("launch failure guidance: %v", err)
	}
	if strings.Contains(err.Error(), ".nagi-validate-") || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "source YAML") {
		t.Fatalf("launch failure presented as config failure or exposed a temporary path: %v", err)
	}
}
