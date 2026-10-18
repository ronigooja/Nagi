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
