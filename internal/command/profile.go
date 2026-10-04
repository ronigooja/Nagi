package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

const defaultProfile = "mixed-port: 17890\nallow-lan: false\nipv6: true\nmode: rule\nlog-level: info\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\ndns:\n  enable: true\n  listen: 127.0.0.1:1053\n  ipv6: true\n  enhanced-mode: fake-ip\n  default-nameserver:\n    - https://1.1.1.1/dns-query\n    - https://8.8.8.8/dns-query\n  nameserver:\n    - https://1.1.1.1/dns-query\n    - https://8.8.8.8/dns-query\n  fallback: []\n  respect-rules: false\n"

func ensureDefaultProfile(path, selected string) error {
	if selected != "default" {
		return nil
	}
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = file.WriteString(defaultProfile); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func newProfileStore(paths nagiruntime.Paths, binary string, client *control.Client, manager *engine.Manager) *profile.Store {
	validate := func(data []byte) error {
		if strings.TrimSpace(string(data)) == "" {
			return errors.New("empty profile")
		}
		resolved, err := exec.LookPath(binary)
		if err != nil {
			return fmt.Errorf("mihomo executable unavailable; set NAGI_MIHOMO_BIN to a mihomo executable: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Join(paths.ConfigDir, "profiles"), ".nagi-validate-*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if err := file.Chmod(0600); err != nil {
			_ = file.Close()
			return err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		cmd := exec.Command(resolved, "-t", "-f", file.Name(), "-d", filepath.Dir(file.Name()))
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				// mihomo diagnostics can contain credentials from the YAML. The
				// temporary file is removed before this error reaches the user.
				return errors.New("profile validation failed; check the source YAML being imported/applied or the profile being selected; validate a local copy with mihomo -t -f FILE -d DIRECTORY")
			}
			return errors.New("could not run mihomo validation; check NAGI_MIHOMO_BIN and executable permissions, then retry")
		}
		return nil
	}
	var store *profile.Store
	reload := func(ctx context.Context) error {
		current, err := profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if err != nil {
			return err
		}
		path, err := materializeProfile(store, paths.ConfigDir, current)
		if err != nil {
			return err
		}
		status, err := manager.Status(ctx)
		if err != nil {
			return err
		}
		if !status.Running {
			return nil
		}
		return client.Put(ctx, "/configs?force=true", map[string]any{"path": path}, nil)
	}
	store = profile.NewStore(paths.ConfigDir, validate, reload)
	return store
}

func materializeProfile(store *profile.Store, configDir, name string) (string, error) {
	path := filepath.Join(configDir, "profiles", name+".yaml")
	if _, err := store.Override(name); errors.Is(err, os.ErrNotExist) && !rules.Exists(configDir) {
		return path, nil
	} else if err != nil {
		return "", err
	}
	data, err := store.Effective(name)
	if err != nil {
		return "", err
	}
	if override, err := store.Override(name); err == nil {
		if err := validateManagedDNSProxyTargets(override, data); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := store.Validate(data); err != nil {
		return "", err
	}
	// Keep the generated file beside the source so mihomo resolves relative
	// providers and other paths from the same configuration directory.
	path = filepath.Join(configDir, "profiles", ".effective-"+name+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".nagi-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

var mixedPortPattern = regexp.MustCompile(`(?m)^mixed-port:\s*([0-9]+)\s*$`)

func startAndVerify(ctx context.Context, manager *engine.Manager, client *control.Client, configPath, profileName string) (any, error) {
	status, err := manager.Start(ctx)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return status, nil
	}
	match := mixedPortPattern.FindSubmatch(data)
	if len(match) != 2 {
		return status, nil
	}
	expected, err := strconv.Atoi(string(match[1]))
	if err != nil || expected == 0 {
		return status, nil
	}
	var actual struct {
		MixedPort int `json:"mixed-port"`
	}
	if err := client.Get(ctx, "/configs", &actual); err != nil {
		return status, nil
	}
	if actual.MixedPort != expected {
		_, _ = manager.Stop(ctx)
		return nil, fail("startup_error", fmt.Errorf("profile %s requested mixed port %d, but mihomo reports %d; inspect nagi logs", profileName, expected, actual.MixedPort))
	}
	return status, nil
}
