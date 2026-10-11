package command

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// isolatedTestPaths gives CLI tests the same paths that production resolves on
// the current platform while keeping both HOME and XDG data inside the fixture.
func isolatedTestPaths(t *testing.T, root string) nagiruntime.Paths {
	t.Helper()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// invalidTestConfigDir makes the platform's actual configuration directory
// unusable, so help and argument tests prove they do not access runtime state.
func invalidTestConfigDir(t *testing.T, root string) {
	t.Helper()
	paths := isolatedTestPaths(t, root)
	bad := filepath.Join(root, "config-file")
	if runtime.GOOS == "darwin" {
		bad = paths.ConfigDir
		if err := os.MkdirAll(filepath.Dir(bad), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", bad)
}
