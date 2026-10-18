package installation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/scripts"
)

func TestLocalInstallerRejectsEmptyArtifacts(t *testing.T) {
	for _, kind := range []string{"cli", "engine", "engine-version", "default-profile"} {
		t.Run(kind, func(t *testing.T) {
			m, prefix := fixture(t)
			artifact := t.TempDir()
			cli, engine := cliArtifact("v2.0.0"), "#!/bin/sh\necho fixture\n"
			switch kind {
			case "cli":
				cli = ""
			case "engine":
				engine = ""
			case "engine-version":
				engine = "#!/bin/sh\nexit 0\n"
			case "default-profile":
				cli = strings.ReplaceAll(cli, "echo 'mixed-port: 17890'", ":")
			}
			for name, content := range map[string]string{"nagi": cli, "mihomo": engine, "installer.sh": scripts.Installer} {
				if err := os.WriteFile(filepath.Join(artifact, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			oldCLI, _ := os.ReadFile(m.Executable)
			state := filepath.Join(prefix, "share/nagi/install-state")
			oldState, _ := os.ReadFile(state)
			if _, err := runProcess(context.Background(), "bash", filepath.Join(artifact, "installer.sh"), "update", "--prefix", prefix, "--artifact-dir", artifact); err == nil {
				t.Fatal("empty artifact accepted")
			}
			if got, _ := os.ReadFile(m.Executable); !bytes.Equal(got, oldCLI) {
				t.Fatal("installed CLI changed")
			}
			if got, _ := os.ReadFile(state); !bytes.Equal(got, oldState) {
				t.Fatal("installation record changed")
			}
		})
	}
}

func TestLocalInstallerCommittedCleanupFailureDoesNotRollback(t *testing.T) {
	for _, action := range []string{"update", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			m, prefix := fixture(t)
			artifact := t.TempDir()
			for name, content := range map[string]string{"nagi": cliArtifact("v2.0.0"), "mihomo": "#!/bin/sh\necho new-engine\n", "installer.sh": scripts.Installer} {
				if err := os.WriteFile(filepath.Join(artifact, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(t.TempDir(), "phases")
			t.Setenv("NAGI_TEST_TX_LOG", log)
			t.Setenv("NAGI_TEST_FAIL_COMMIT_CLEANUP", "1")
			args := []string{filepath.Join(artifact, "installer.sh"), action, "--prefix", prefix}
			if action == "update" {
				args = append(args, "--artifact-dir", artifact)
			}
			output, err := runProcess(context.Background(), "bash", args...)
			if err == nil || !bytes.Contains(output, []byte("NAGI_INSTALL_FILES_COMMITTED:")) {
				t.Fatalf("missing committed failure: %s: %v", output, err)
			}
			if bytes.Contains(output, []byte("restoring previous files")) {
				t.Fatalf("committed operation attempted rollback: %s", output)
			}
			phases, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(string(phases), "activate\ncommit\n") {
				t.Fatalf("unexpected cleanup side effects after commit: %s", phases)
			}
			if action == "update" {
				got, err := os.ReadFile(m.Executable)
				if err != nil || string(got) != cliArtifact("v2.0.0") {
					t.Fatalf("committed CLI not retained: %v", err)
				}
				got, err = os.ReadFile(filepath.Join(prefix, "share/nagi/install-state"))
				if err != nil || string(got) != "nagi-installer-v4\napp=\n" {
					t.Fatalf("committed record not retained: %s: %v", got, err)
				}
			} else {
				for _, name := range []string{"bin/nagi", "bin/mihomo", "share/nagi/install-state"} {
					if _, err := os.Stat(filepath.Join(prefix, name)); !os.IsNotExist(err) {
						t.Fatalf("uninstalled target restored: %s: %v", name, err)
					}
				}
			}
		})
	}
}

func TestLocalInstallerUncertainCommitRetainsJournalForRecovery(t *testing.T) {
	for _, action := range []string{"update", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			m, prefix := fixture(t)
			artifact := t.TempDir()
			for name, content := range map[string]string{"nagi": cliArtifact("v2.0.0"), "mihomo": "#!/bin/sh\necho new-engine\n", "installer.sh": scripts.Installer} {
				if err := os.WriteFile(filepath.Join(artifact, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			oldCLI, _ := os.ReadFile(m.Executable)
			state := filepath.Join(prefix, "share/nagi/install-state")
			oldState, _ := os.ReadFile(state)
			log := filepath.Join(t.TempDir(), "phases")
			t.Setenv("NAGI_TEST_TX_LOG", log)
			t.Setenv("NAGI_TEST_COMMIT_UNCERTAIN", "1")
			installer := filepath.Join(artifact, "installer.sh")
			args := []string{installer, action, "--prefix", prefix}
			if action == "update" {
				args = append(args, "--artifact-dir", artifact)
			}
			output, err := runProcess(context.Background(), "bash", args...)
			if err == nil || !bytes.Contains(output, []byte("NAGI_INSTALL_COMMIT_UNCERTAIN:")) {
				t.Fatalf("missing uncertain failure: %s: %v", output, err)
			}
			if bytes.Contains(output, []byte("restoring previous files")) {
				t.Fatalf("uncertain operation attempted rollback: %s", output)
			}
			phases, _ := os.ReadFile(log)
			if !strings.HasSuffix(string(phases), "activate\ncommit\n") {
				t.Fatalf("unexpected compensation: %s", phases)
			}
			if _, err := os.Stat(filepath.Join(prefix, ".nagi-transaction")); err != nil {
				t.Fatalf("recovery journal not retained: %v", err)
			}
			if action == "update" {
				got, _ := os.ReadFile(m.Executable)
				if string(got) != cliArtifact("v2.0.0") {
					t.Fatal("activated CLI rolled back")
				}
			} else if _, err := os.Stat(m.Executable); !os.IsNotExist(err) {
				t.Fatalf("removed CLI restored: %v", err)
			}
			// The fixture leaves a prepared journal without publishing its commit
			// marker, so explicit recovery must restore the old installation.
			if _, err := runProcess(context.Background(), "bash", installer, "recover", "--prefix", prefix, "--artifact-dir", artifact); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(m.Executable); !bytes.Equal(got, oldCLI) {
				t.Fatal("explicit recovery did not restore CLI")
			}
			if got, _ := os.ReadFile(state); !bytes.Equal(got, oldState) {
				t.Fatal("explicit recovery did not restore record")
			}
		})
	}
}
