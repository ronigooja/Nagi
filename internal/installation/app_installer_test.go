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

// Exercise the actual App transaction on an isolated prefix. Only the platform
// probe and fixed /usr/local boundary are replaced; no system paths are touched.
func TestAppOnlyInstallerPreservesBinariesAndRollsBack(t *testing.T) {
	for _, mode := range []string{"normal", "current", "running", "state-write-failure", "uninstall-state-write-failure"} {
		t.Run(mode, func(t *testing.T) {
			m, prefix := fixture(t)
			root := filepath.Dir(prefix)
			artifact := filepath.Join(root, "artifact")
			app := filepath.Join(root, "Applications/Nagi.app")
			os.MkdirAll(filepath.Dir(app), 0755)
			os.MkdirAll(filepath.Join(artifact, "Nagi.app/Contents/MacOS"), 0755)
			for name, content := range map[string]string{"nagi": cliArtifact("v1.0.0"), "mihomo": "#!/bin/sh\necho fixture\n", "Nagi.app/Contents/Info.plist": "plist", "Nagi.app/Contents/MacOS/NagiApp": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(artifact, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			tools := filepath.Join(root, "tools")
			os.MkdirAll(tools, 0755)
			os.WriteFile(filepath.Join(tools, "uname"), []byte("#!/bin/sh\necho Darwin\n"), 0755)
			pgrep := "#!/bin/sh\nexit 1\n"
			if mode == "running" {
				pgrep = "#!/bin/sh\nexit 0\n"
			}
			os.WriteFile(filepath.Join(tools, "pgrep"), []byte(pgrep), 0755)
			if mode == "state-write-failure" {
				os.WriteFile(filepath.Join(tools, "mv"), []byte("#!/bin/sh\ncase \"$*\" in *new-state*) exit 1;; esac\nexec /bin/mv \"$@\"\n"), 0755)
			}
			t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			installer := filepath.Join(root, "installer.sh")
			source := strings.ReplaceAll(scripts.Installer, "/usr/local", prefix)
			if err := os.WriteFile(installer, []byte(source), 0755); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(prefix, "share/nagi/install-state")
			if mode == "current" {
				if err := os.WriteFile(statePath, []byte("nagi-installer-v4\napp=\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			beforeState, _ := os.ReadFile(statePath)
			if mode == "normal" {
				cli, _ := os.ReadFile(m.Executable)
				if err := os.WriteFile(m.Executable, append(cli, []byte("# outside modification\n")...), 0755); err != nil {
					t.Fatal(err)
				}
			}
			beforeCLI, _ := os.ReadFile(m.Executable)
			beforeEngine, _ := os.ReadFile(filepath.Join(prefix, "bin/mihomo"))
			args := []string{installer, "app-install", "--prefix", prefix, "--app-dest", app, "--artifact-dir", artifact}
			_, err := runProcess(context.Background(), "bash", args...)
			if mode == "running" || mode == "state-write-failure" {
				if err == nil {
					t.Fatal("expected App operation failure")
				}
				if after, _ := os.ReadFile(statePath); !bytes.Equal(beforeState, after) {
					t.Fatal("installation record not restored")
				}
				if _, err := os.Stat(app); !os.IsNotExist(err) {
					t.Fatal("failed App installation remained")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(app, "Contents/MacOS/NagiApp")); err != nil {
					t.Fatal(err)
				}
				state, _ := os.ReadFile(statePath)
				if !strings.Contains(string(state), "app="+app+"\n") {
					t.Fatalf("state %s", state)
				}
				if mode == "uninstall-state-write-failure" {
					os.WriteFile(filepath.Join(tools, "mv"), []byte("#!/bin/sh\ncase \"$*\" in *new-state*) exit 1;; esac\nexec /bin/mv \"$@\"\n"), 0755)
					if _, err := runProcess(context.Background(), "bash", installer, "app-uninstall", "--prefix", prefix); err == nil {
						t.Fatal("expected uninstall state failure")
					}
					if after, _ := os.ReadFile(statePath); !bytes.Equal(state, after) {
						t.Fatal("App removal did not restore its record")
					}
					if _, err := os.Stat(filepath.Join(app, "Contents/MacOS/NagiApp")); err != nil {
						t.Fatal("App removal did not restore the bundle")
					}
					os.Remove(filepath.Join(tools, "mv"))
				}
				if mode == "normal" {
					if err := os.WriteFile(filepath.Join(app, "Contents/Info.plist"), []byte("modified plist"), 0755); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := runProcess(context.Background(), "bash", installer, "app-uninstall", "--prefix", prefix); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(app); !os.IsNotExist(err) {
					t.Fatal("App not removed")
				}
				if after, _ := os.ReadFile(statePath); string(after) != "nagi-installer-v4\napp=\n" {
					t.Fatalf("App removal did not migrate record: %s", after)
				}
			}
			if after, _ := os.ReadFile(m.Executable); !bytes.Equal(beforeCLI, after) {
				t.Fatal("CLI changed during App-only transaction")
			}
			if after, _ := os.ReadFile(filepath.Join(prefix, "bin/mihomo")); !bytes.Equal(beforeEngine, after) {
				t.Fatal("mihomo changed during App-only transaction")
			}
			profile := filepath.Join(os.Getenv("HOME"), "Library/Application Support/Nagi/config/profiles/default.yaml")
			if _, err := os.Stat(profile); !os.IsNotExist(err) {
				t.Fatal("App-only operation wrote the default profile")
			}
		})
	}
}
