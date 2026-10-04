// Package runtime resolves and creates Nagi's per-user storage directories.
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ronigooja/Nagi/internal/platform"
)

type Paths struct {
	ConfigDir  string
	DataDir    string
	StateDir   string
	RuntimeDir string
	SocketPath string
	PIDPath    string
	LockPath   string
	LogPath    string
}

func Resolve() (Paths, error) {
	if err := platform.Check(); err != nil {
		return Paths{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	var p Paths
	if runtime.GOOS == "darwin" {
		base := filepath.Join(home, "Library", "Application Support", "Nagi")
		p.ConfigDir = filepath.Join(base, "config")
		p.DataDir = base
		p.StateDir = filepath.Join(base, "state")
		p.RuntimeDir = filepath.Join(base, "runtime")
	} else {
		p.ConfigDir = filepath.Join(xdg("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "nagi")
		p.DataDir = filepath.Join(xdg("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "nagi")
		p.StateDir = filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "nagi")
		if value := os.Getenv("XDG_RUNTIME_DIR"); value != "" && filepath.IsAbs(value) {
			p.RuntimeDir = filepath.Join(value, "nagi")
		} else {
			p.RuntimeDir = filepath.Join(p.StateDir, "runtime")
		}
	}
	p.SocketPath = filepath.Join(p.RuntimeDir, "mihomo.sock")
	p.PIDPath = filepath.Join(p.RuntimeDir, "mihomo.pid")
	p.LockPath = filepath.Join(p.RuntimeDir, "mihomo.lock")
	p.LogPath = filepath.Join(p.RuntimeDir, "logs", "mihomo.log")
	return p, nil
}

func xdg(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" || !filepath.IsAbs(value) {
		return fallback
	}
	return value
}

func (p Paths) EnsureRuntime() error {
	if p.RuntimeDir == "" || p.SocketPath == "" || p.PIDPath == "" || p.LockPath == "" || p.LogPath == "" {
		return fmt.Errorf("incomplete runtime paths")
	}
	for _, dir := range []string{p.RuntimeDir, filepath.Dir(p.LogPath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}
