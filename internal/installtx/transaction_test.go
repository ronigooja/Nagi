package installtx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	must(t, e)
	return p
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func write(t *testing.T, p, s string) { t.Helper(); must(t, os.WriteFile(p, []byte(s), 0600)) }
func check(t *testing.T, p, s string) {
	t.Helper()
	b, e := os.ReadFile(p)
	must(t, e)
	if string(b) != s {
		t.Fatalf("%s got %q want %q", p, b, s)
	}
}
func fixture(t *testing.T) (string, []Operation) {
	t.Helper()
	d := tempDir(t)
	root := filepath.Join(d, ".journal")
	var ops []Operation
	for _, name := range []string{"nagi", "mihomo", "state", "default"} {
		target := filepath.Join(d, name)
		source := filepath.Join(d, "source-"+name)
		write(t, target, "old-"+name)
		write(t, source, "new-"+name)
		ops = append(ops, Operation{Target: target, Source: source})
	}
	app := filepath.Join(d, "App")
	must(t, os.Mkdir(app, 0755))
	write(t, filepath.Join(app, "Info"), "old-App")
	next := filepath.Join(d, "new-App")
	must(t, os.Mkdir(next, 0755))
	write(t, filepath.Join(next, "Info"), "new-App")
	ops = append(ops, Operation{Target: app, Source: next})
	return root, ops
}
func assertVersion(t *testing.T, ops []Operation, version string) {
	t.Helper()
	for _, o := range ops {
		if filepath.Base(o.Target) == "App" {
			check(t, filepath.Join(o.Target, "Info"), version+"-App")
		} else {
			check(t, o.Target, version+"-"+filepath.Base(o.Target))
		}
	}
}
func TestCrashChild(t *testing.T) {
	if os.Getenv("INSTALLTX_TEST_CHILD") != "1" {
		return
	}
	root := os.Getenv("INSTALLTX_TEST_ROOT")
	stage := os.Getenv("INSTALLTX_TEST_STAGE")
	checkpoint = func(p string) {
		if p == stage {
			os.Exit(86)
		}
	}
	var e error
	if os.Getenv("INSTALLTX_TEST_RECOVER") == "1" {
		e = Recover(root)
	} else {
		var ops []Operation
		for _, name := range []string{"nagi", "mihomo", "state", "default"} {
			ops = append(ops, Operation{Target: filepath.Join(filepath.Dir(root), name), Source: filepath.Join(filepath.Dir(root), "source-"+name)})
		}
		ops = append(ops, Operation{Target: filepath.Join(filepath.Dir(root), "App"), Source: filepath.Join(filepath.Dir(root), "new-App")})
		e = Prepare(root, ops)
		if e == nil {
			e = Activate(root)
		}
		if e == nil {
			e = Commit(root)
		}
	}
	if e != nil {
		t.Fatal(e)
	}
}
func crash(t *testing.T, root, point string, recover bool) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestCrashChild$")
	c.Env = append(os.Environ(), "INSTALLTX_TEST_CHILD=1", "INSTALLTX_TEST_ROOT="+root, "INSTALLTX_TEST_STAGE="+point, fmt.Sprintf("INSTALLTX_TEST_RECOVER=%d", map[bool]int{true: 1, false: 0}[recover]))
	b, e := c.CombinedOutput()
	if ee, ok := e.(*exec.ExitError); !ok || ee.ExitCode() != 86 {
		t.Fatalf("child error %v: %s", e, b)
	}
}
func TestCrashRecoveryMatrix(t *testing.T) {
	for _, point := range []string{"prepared", "applied-0", "applied-1", "applied-2", "applied-3", "applied-4", "committed", "cleanup-detached"} {
		t.Run(point, func(t *testing.T) {
			root, ops := fixture(t)
			crash(t, root, point, false)
			must(t, Recover(root))
			must(t, Recover(root))
			want := "old"
			if point == "committed" || point == "cleanup-detached" {
				want = "new"
			}
			assertVersion(t, ops, want)
		})
	}
}
func TestInterruptedRollbackPreservesBackups(t *testing.T) {
	for i := 4; i >= 0; i-- {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			root, ops := fixture(t)
			must(t, Prepare(root, ops))
			must(t, Activate(root))
			crash(t, root, fmt.Sprintf("rollback-%d", i), true)
			must(t, Recover(root))
			assertVersion(t, ops, "old")
		})
	}
}
func TestRemoveAndNewFileRollback(t *testing.T) {
	d := tempDir(t)
	root := filepath.Join(d, ".journal")
	old := filepath.Join(d, "old")
	write(t, old, "original")
	src := filepath.Join(d, "src")
	write(t, src, "new")
	newPath := filepath.Join(d, "created")
	ops := []Operation{{Target: old, Source: "-"}, {Target: newPath, Source: src}}
	must(t, Prepare(root, ops))
	must(t, Activate(root))
	must(t, Recover(root))
	check(t, old, "original")
	if _, e := os.Stat(newPath); !os.IsNotExist(e) {
		t.Fatalf("created target remains: %v", e)
	}
	must(t, Prepare(root, ops))
	must(t, Activate(root))
	must(t, Commit(root))
	if _, e := os.Stat(old); !os.IsNotExist(e) {
		t.Fatal("removed target remains")
	}
	check(t, newPath, "new")
}
func TestCorruptionRetainsRecoveryJournal(t *testing.T) {
	for _, path := range []string{"old-0", "new-0"} {
		t.Run(path, func(t *testing.T) {
			root, ops := fixture(t)
			must(t, Prepare(root, ops))
			write(t, filepath.Join(root, path), "")
			if e := Activate(root); e == nil {
				t.Fatal("accepted corrupt staged content")
			}
			if e := Recover(root); e == nil {
				t.Fatal("accepted corrupt recovery content")
			}
			if _, e := os.Stat(filepath.Join(root, "manifest.json")); e != nil {
				t.Fatal("lost journal", e)
			}
			assertVersion(t, ops, "old")
		})
	}
}
func TestRejectUnsafePaths(t *testing.T) {
	for _, kind := range []string{"symlink-parent", "symlink-target", "hidden-journal", "overlap", "source-symlink"} {
		t.Run(kind, func(t *testing.T) {
			d := tempDir(t)
			root := filepath.Join(d, ".journal")
			src := filepath.Join(d, "src")
			write(t, src, "new")
			target := filepath.Join(d, "target")
			write(t, target, "old")
			ops := []Operation{{Target: target, Source: src}}
			switch kind {
			case "symlink-parent":
				must(t, os.Symlink(d, filepath.Join(d, "link")))
				ops[0].Target = filepath.Join(d, "link", "x")
			case "symlink-target":
				must(t, os.Remove(target))
				must(t, os.Symlink(src, target))
			case "hidden-journal":
				ops[0].Target = filepath.Join(root, ".hidden")
			case "overlap":
				ops = append(ops, Operation{Target: filepath.Join(target, "child"), Source: src})
			case "source-symlink":
				must(t, os.Symlink(src, filepath.Join(d, "link")))
				ops[0].Source = filepath.Join(d, "link")
			}
			if e := Prepare(root, ops); e == nil {
				t.Fatal("accepted unsafe path")
			}
		})
	}
}
func TestLockProtocol(t *testing.T) {
	d := tempDir(t)
	root := filepath.Join(d, ".journal")
	var out bytes.Buffer
	must(t, WithLock(root, func() error {
		if e := WithLock(root, func() error { return nil }); e == nil {
			t.Fatal("second lock succeeded")
		}
		return nil
	}))
	code := Run([]string{"lock", root, "--", os.Args[0], "-test.run=^TestInheritedLockChild$"}, &out, &out)
	if code != 0 {
		t.Fatal(code, out.String())
	}
	if e := run([]string{"--locked", "recover", root}, &out, &out); e == nil || !strings.Contains(e.Error(), "not held") {
		t.Fatal(e)
	}
}
func TestInheritedLockChild(t *testing.T) {
	root := os.Getenv("NAGI_INSTALLTX_LOCK_ROOT")
	if root == "" {
		return
	}
	must(t, WithLock(root, func() error { return nil }))
	must(t, run([]string{"--locked", "recover", root}, os.Stdout, os.Stderr))
}
func TestCommitRefusesChangedActivation(t *testing.T) {
	root, ops := fixture(t)
	must(t, Prepare(root, ops))
	must(t, Activate(root))
	write(t, ops[0].Target, "")
	if e := Commit(root); e == nil {
		t.Fatal("committed changed target")
	}
	must(t, Recover(root))
	assertVersion(t, ops, "old")
}

func TestSourceParentAlias(t *testing.T) {
	d := tempDir(t)
	real := filepath.Join(d, "real")
	must(t, os.Mkdir(real, 0700))
	alias := filepath.Join(d, "alias")
	must(t, os.Symlink(real, alias))
	write(t, filepath.Join(real, "source"), "new")
	target := filepath.Join(d, "target")
	root := filepath.Join(d, ".journal")
	must(t, Prepare(root, []Operation{{Target: target, Source: filepath.Join(alias, "source")}}))
	must(t, Activate(root))
	must(t, Commit(root))
	check(t, target, "new")
	must(t, os.Symlink(filepath.Join(real, "source"), filepath.Join(real, "link")))
	if e := Prepare(root, []Operation{{Target: target, Source: filepath.Join(alias, "link")}}); e == nil {
		t.Fatal("accepted source leaf symlink")
	}
}

func TestStorageFailureBeforePrepareLeavesTargetsUnchanged(t *testing.T) {
	for _, phase := range []string{"file", "ancestor"} {
		t.Run(phase, func(t *testing.T) {
			root, ops := fixture(t)
			syncHook = func(p string) error {
				if phase == "file" && filepath.Base(p) == "old-0" || phase == "ancestor" && p == filepath.Dir(ops[0].Target) {
					return fmt.Errorf("injected sync failure")
				}
				return nil
			}
			defer func() { syncHook = func(string) error { return nil } }()
			if e := Prepare(root, ops); e == nil {
				t.Fatal("sync error ignored")
			}
			assertVersion(t, ops, "old")
			syncHook = func(string) error { return nil }
			must(t, Recover(root))
			assertVersion(t, ops, "old")
		})
	}
}
func TestStorageFailureAfterRenameRemainsRecoverable(t *testing.T) {
	root, ops := fixture(t)
	must(t, Prepare(root, ops))
	syncHook = func(p string) error {
		if p == filepath.Dir(ops[0].Target) {
			return fmt.Errorf("injected parent sync failure")
		}
		return nil
	}
	defer func() { syncHook = func(string) error { return nil } }()
	if e := Activate(root); e == nil {
		t.Fatal("sync error ignored")
	}
	check(t, ops[0].Target, "new-nagi")
	syncHook = func(string) error { return nil }
	must(t, Recover(root))
	assertVersion(t, ops, "old")
}
func TestPostCommitCleanupFailureReportsCommitted(t *testing.T) {
	root, ops := fixture(t)
	must(t, Prepare(root, ops))
	must(t, Activate(root))
	syncHook = func(p string) error {
		if p == filepath.Dir(root) {
			return fmt.Errorf("injected cleanup sync failure")
		}
		return nil
	}
	defer func() { syncHook = func(string) error { return nil } }()
	var out bytes.Buffer
	if got := Run([]string{"commit", root}, &out, &out); got != 4 {
		t.Fatalf("exit %d: %s", got, out.String())
	}
	assertVersion(t, ops, "new")
	syncHook = func(string) error { return nil }
	must(t, Recover(root))
	if p := garbagePaths(t, root); len(p) != 0 {
		t.Fatal("recovery did not finish cleanup", p)
	}
	assertVersion(t, ops, "new")
}

// Failed commit-directory sync withdraws the visible marker and syncs its removal.
// Once withdrawal succeeds, the prepared backups still define a safe rollback.
func TestCommitMarkerSyncFailureRollsBack(t *testing.T) {
	root, ops := fixture(t)
	must(t, Prepare(root, ops))
	must(t, Activate(root))
	failed := false
	syncHook = func(p string) error {
		if p == root && !failed {
			failed = true
			return fmt.Errorf("injected commit sync failure")
		}
		return nil
	}
	defer func() { syncHook = func(string) error { return nil } }()
	if e := Commit(root); e == nil {
		t.Fatal("commit sync error ignored")
	}
	syncHook = func(string) error { return nil }
	must(t, Recover(root))
	assertVersion(t, ops, "old")
}

func garbagePaths(t *testing.T, root string) []string {
	t.Helper()
	p, e := filepath.Glob(root + ".garbage-*")
	must(t, e)
	return p
}
func TestRecoverFinishesDetachedCleanup(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			root, ops := fixture(t)
			crash(t, root, "cleanup-detached", false)
			garbage := garbagePaths(t, root)
			if len(garbage) != 1 {
				t.Fatal(garbage)
			}
			if partial {
				must(t, os.Remove(filepath.Join(garbage[0], "manifest.json")))
				must(t, os.Remove(filepath.Join(garbage[0], "committed")))
			}
			must(t, Recover(root))
			if p := garbagePaths(t, root); len(p) != 0 {
				t.Fatal("garbage remains", p)
			}
			assertVersion(t, ops, "new")
		})
	}
}
func TestGarbageSweepRefusesLinksAndKeepsTargets(t *testing.T) {
	root, ops := fixture(t)
	crash(t, root, "cleanup-detached", false)
	garbage := garbagePaths(t, root)[0]
	must(t, os.Symlink(ops[0].Target, filepath.Join(garbage, "link")))
	if e := Recover(root); e == nil {
		t.Fatal("accepted garbage link")
	}
	assertVersion(t, ops, "new")
	must(t, os.Remove(filepath.Join(garbage, "link")))
	must(t, Recover(root))
	assertVersion(t, ops, "new")
}
func TestGarbageNamespaceCannotBeTarget(t *testing.T) {
	root, ops := fixture(t)
	ops[0].Target = root + ".garbage-1234"
	if e := Prepare(root, ops); e == nil {
		t.Fatal("allowed transaction target in garbage namespace")
	}
}
func TestFailedMarkerWithdrawalRetainsJournal(t *testing.T) {
	root, ops := fixture(t)
	must(t, Prepare(root, ops))
	must(t, Activate(root))
	syncHook = func(p string) error {
		if p == root {
			return fmt.Errorf("commit and withdrawal sync failed")
		}
		return nil
	}
	defer func() { syncHook = func(string) error { return nil } }()
	if e := Commit(root); e == nil {
		t.Fatal("ignored storage failure")
	} else {
		var uncertain *CommitUncertainError
		if !errors.As(e, &uncertain) {
			t.Fatalf("expected uncertain outcome: %v", e)
		}
	}
	if _, e := os.Stat(filepath.Join(root, "manifest.json")); e != nil {
		t.Fatal("journal not retained", e)
	}
	if _, e := os.Stat(filepath.Join(root, "old-0")); e != nil {
		t.Fatal("backup not retained", e)
	}
	syncHook = func(string) error { return nil }
	must(t, Recover(root))
	assertVersion(t, ops, "old")
}
