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
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

const defaultProfile = "mixed-port: 17890\nallow-lan: false\nmode: rule\nlog-level: info\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"

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
		cmd := exec.Command(binary, "-t", "-f", file.Name(), "-d", filepath.Dir(file.Name()))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("mihomo configuration validation failed: %w", err)
		}
		return nil
	}
	reload := func(ctx context.Context) error {
		status, err := manager.Status(ctx)
		if err != nil {
			return err
		}
		if !status.Running {
			return nil
		}
		current, err := profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if err != nil {
			return err
		}
		path := filepath.Join(paths.ConfigDir, "profiles", current+".yaml")
		return client.Put(ctx, "/configs?force=true", map[string]any{"path": path}, nil)
	}
	return profile.NewStore(paths.ConfigDir, validate, reload)
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
