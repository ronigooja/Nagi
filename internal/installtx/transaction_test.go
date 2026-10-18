package installtx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	d := t.TempDir()
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
	d := t.TempDir()
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
			d := t.TempDir()
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
	d := t.TempDir()
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
