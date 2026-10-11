package command

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// CLI tests must never resolve the developer's installed Nagi configuration.
// A short macOS home also keeps runtime Unix socket paths within Darwin's limit.
func TestMain(m *testing.M) {
	tempRoot := ""
	if runtime.GOOS == "darwin" {
		tempRoot = "/private/tmp"
	}
	root, err := os.MkdirTemp(tempRoot, "ngc-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for key, path := range map[string]string{
		"HOME":            root,
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"XDG_RUNTIME_DIR": filepath.Join(root, "run"),
	} {
		if err := os.Setenv(key, path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = os.RemoveAll(root)
			os.Exit(1)
		}
	}
	code := m.Run()
	if err := os.RemoveAll(root); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
