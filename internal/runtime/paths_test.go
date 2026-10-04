package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAndEnsureRuntime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux XDG paths")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "xdg-run"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if paths.RuntimeDir != filepath.Join(dir, "xdg-run", "nagi") {
		t.Fatalf("runtime: %s", paths.RuntimeDir)
	}
	if paths.ConfigDir != filepath.Join(dir, "xdg-config", "nagi") {
		t.Fatalf("config: %s", paths.ConfigDir)
	}
	if err := paths.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.RuntimeDir, filepath.Dir(paths.LogPath)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("%s mode %v", path, info.Mode().Perm())
		}
	}
}
