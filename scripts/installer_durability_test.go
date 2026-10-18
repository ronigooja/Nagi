package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func buildTransactionHelper(t *testing.T) string {
	t.Helper()
	helper := filepath.Join(t.TempDir(), "transaction-helper")
	build := exec.Command("go", "build", "-o", helper, "./cmd/nagi")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real transaction helper: %v\n%s", err, out)
	}
	return helper
}

// Each command runs in a fresh process, so recovery cannot depend on the
// installing process's memory or EXIT trap. Fine-grained process termination
// and failed fsync tests live beside the transaction implementation.
func TestTransactionRecoveryAcrossProcesses(t *testing.T) {
	helper := buildTransactionHelper(t)
	for _, action := range []string{"install", "update", "uninstall", "app-install", "app-uninstall"} {
		for _, boundary := range []string{"prepared", "activated", "committed"} {
			t.Run(action+"/"+boundary, func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				journal := filepath.Join(root, "prefix", ".nagi-transaction")
				if err := os.MkdirAll(filepath.Dir(journal), 0755); err != nil {
					t.Fatal(err)
				}
				command := func(verb string, args ...string) {
					t.Helper()
					argv := append([]string{"__install-transaction", verb, journal}, args...)
					if out, err := exec.Command(helper, argv...).CombinedOutput(); err != nil {
						t.Fatalf("%s: %v\n%s", verb, err, out)
					}
				}
				var pairs []string
				var targets []string
				old := map[string]bool{}
				removed := map[string]bool{}
				write := func(path, contents string) {
					t.Helper()
					if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
						t.Fatal(err)
					}
				}
				components := []string{"bin/nagi", "bin/mihomo", "share/nagi/install-state", "config/profiles/default.yaml", "Applications/Nagi.app/Contents/MacOS/Nagi"}
				if action == "uninstall" {
					components = []string{"bin/nagi", "bin/mihomo", "share/nagi/install-state", "Applications/Nagi.app/Contents/MacOS/Nagi"}
				}
				if action == "app-install" || action == "app-uninstall" {
					components = []string{"share/nagi/install-state", "Applications/Nagi.app/Contents/MacOS/Nagi"}
				}
				for _, component := range components {
					// App bundles are transaction directory targets, not individual files.
					target := filepath.Join(root, "installed", component)
					source := filepath.Join(root, "candidate", component)
					hadOld := action != "install" && (action != "app-install" || component == "share/nagi/install-state")
					if hadOld {
						write(target, "old")
					}
					write(source, "new")
					if component == "Applications/Nagi.app/Contents/MacOS/Nagi" {
						target = filepath.Join(root, "installed", "Applications/Nagi.app")
						source = filepath.Join(root, "candidate", "Applications/Nagi.app")
					}
					if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
						t.Fatal(err)
					}
					remove := action == "uninstall" || (action == "app-uninstall" && component != "share/nagi/install-state")
					if remove {
						source = "-"
					}
					old[target], removed[target] = hadOld, remove
					pairs = append(pairs, target, source)
					targets = append(targets, target)
				}
				command("prepare", pairs...)
				if boundary != "prepared" {
					command("activate")
				}
				if boundary == "committed" {
					command("commit")
				}
				command("recover")
				command("recover")
				for _, target := range targets {
					want := "old"
					absent := !old[target]
					if boundary == "committed" {
						want = "new"
						absent = removed[target]
					}
					if absent {
						if _, err := os.Lstat(target); !os.IsNotExist(err) {
							t.Fatalf("%s should be absent; stat: %v", target, err)
						}
						continue
					}
					if filepath.Ext(target) == ".app" {
						target = filepath.Join(target, "Contents/MacOS/Nagi")
					}
					got, err := os.ReadFile(target)
					if err != nil || string(got) != want {
						t.Fatalf("%s: got %q, %v; want %q", target, got, err, want)
					}
				}
			})
		}
	}
}

func TestTransactionRejectsDamagedRecoveryData(t *testing.T) {
	helper := buildTransactionHelper(t)
	for _, damage := range []string{"manifest", "missing-backup", "changed-backup", "changed-candidate", "symlink-marker", "symlink-journal"} {
		t.Run(damage, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(root, ".nagi-transaction")
			target, source := filepath.Join(root, "target"), filepath.Join(root, "source")
			mustWrite := func(path, contents string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			mustWrite(target, "old")
			mustWrite(source, "new")
			run := func(verb string, args ...string) ([]byte, error) {
				return exec.Command(helper, append([]string{"__install-transaction", verb, journal}, args...)...).CombinedOutput()
			}
			if out, err := run("prepare", target, source); err != nil {
				t.Fatalf("prepare: %v\n%s", err, out)
			}
			verb := "recover"
			switch damage {
			case "manifest":
				mustWrite(filepath.Join(journal, "manifest.json"), "{broken")
			case "missing-backup":
				if err := os.Remove(filepath.Join(journal, "old-0")); err != nil {
					t.Fatal(err)
				}
			case "changed-backup":
				mustWrite(filepath.Join(journal, "old-0"), "damaged")
			case "changed-candidate":
				mustWrite(filepath.Join(journal, "new-0"), "damaged")
				verb = "activate"
			case "symlink-marker":
				if err := os.Symlink(source, filepath.Join(journal, "committed")); err != nil {
					t.Fatal(err)
				}
			case "symlink-journal":
				if err := os.Rename(journal, journal+"-real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(journal+"-real", journal); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := run(verb); err == nil {
				t.Fatalf("%s unexpectedly accepted %s: %s", verb, damage, out)
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != "old" {
				t.Fatalf("damaged journal changed target: %q, %v", got, err)
			}
			if _, err := os.Lstat(journal); err != nil {
				t.Fatalf("damaged journal discarded: %v", err)
			}
		})
	}
}
