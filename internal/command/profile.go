package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/rules"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

const defaultProfileBase = `mixed-port: 17890
allow-lan: false
ipv6: true
mode: rule
log-level: info
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
tun:
  enable: false
  stack: system
  auto-route: true
  strict-route: true
  auto-detect-interface: true
  dns-hijack:
    - any:53
    - tcp://any:53
`

// The installer and first-run profile use the same DNS generator as overrides.
var defaultProfile = func() string {
	var config map[string]any
	if err := yaml.Unmarshal([]byte(defaultProfileBase), &config); err != nil {
		panic(err)
	}
	config["dns"] = profile.DefaultDNS("")
	data, err := yaml.Marshal(config)
	if err != nil {
		panic(err)
	}
	return string(data)
}()

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

func newProfileStore(paths nagiruntime.Paths, binary string, client *control.Client, manager lifecycleBackend) *profile.Store {
	validate := func(data []byte) error {
		current, e := profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if e != nil {
			current = "default"
		}
		var hasTailscale bool
		data, hasTailscale, e = profile.RewriteTailscaleState(data, paths.ConfigDir, current, true)
		if e != nil {
			return e
		}
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
		cmd := exec.Command(resolved, "-t", "--check-json", "-f", file.Name(), "-d", filepath.Dir(file.Name()))
		cmd.Env = nagiruntime.TailscaleEnv(os.Environ(), paths.ConfigDir)
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if hasTailscale {
					return errors.New("profile validation failed; Tailscale diagnostics are private; validate a local copy with mihomo -t --check-json")
				}
				var result struct {
					Errors []struct {
						Path    string `json:"path"`
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"errors"`
				}
				if json.Unmarshal(out, &result) == nil && len(result.Errors) > 0 {
					first := result.Errors[0]
					return fmt.Errorf("profile validation failed at %s (%s): %s", first.Path, first.Code, first.Message)
				}
				return errors.New("profile validation failed; validate a local copy with mihomo -t --check-json -f FILE -d DIRECTORY")
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
		path, err := materializeProfileWithSelections(store, paths.ConfigDir, current, ctx.Value(skipSelectionRestoreKey{}) != true)
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
		if err := activateConfig(ctx, client, path); err != nil {
			return err
		}
		if ctx.Value(skipSelectionRestoreKey{}) != true {
			if _, err := (proxy.SelectionStore{Path: filepath.Join(paths.ConfigDir, "proxy-selections.json")}).Restore(ctx, current, proxy.NewService(client)); err != nil {
				return fmt.Errorf("mihomo reloaded, but saved proxy selections were not fully restored: %w", err)
			}
		}
		return nil
	}
	store = profile.NewStore(paths.ConfigDir, validate, reload)
	store.EffectiveOverlay = func(data []byte) ([]byte, error) { return applySavedTUN(paths, data) }
	return store
}

type skipSelectionRestoreKey struct{}

func orderedProxyService(client proxy.Client, configDir string) *proxy.Service {
	service := proxy.NewService(client)
	store := profile.NewStore(configDir, nil, nil)
	selected, err := store.Current()
	if err != nil {
		return service
	}
	effective, err := store.Effective(selected)
	if err != nil {
		return service
	}
	service.GroupOrder, _ = proxy.GroupOrderFromYAML(effective)
	return service
}

func materializeProfile(store *profile.Store, configDir, name string) (string, error) {
	return materializeProfileWithSelections(store, configDir, name, true)
}

func materializeProfileWithSelections(store *profile.Store, configDir, name string, includeSelections bool) (string, error) {
	path := filepath.Join(configDir, "profiles", name+".yaml")
	selectionStore := proxy.SelectionStore{Path: filepath.Join(configDir, "proxy-selections.json")}
	_, overrideErr := store.Override(name)
	if overrideErr != nil && !errors.Is(overrideErr, os.ErrNotExist) {
		return "", overrideErr
	}
	source, err := store.Show(name)
	if err != nil {
		return "", err
	}
	_, hasTailscale, err := profile.RewriteTailscaleState(source, configDir, name, false)
	if err != nil {
		return "", err
	}
	needsOverlay := hasTailscale || overrideErr == nil || rules.Exists(configDir)
	if !needsOverlay && !includeSelections && store.EffectiveOverlay == nil {
		return path, nil
	}
	data, err := store.Effective(name)
	if err != nil {
		return "", err
	}
	needsOverlay = needsOverlay || !bytes.Equal(data, source)
	if !needsOverlay && !includeSelections {
		return path, nil
	}
	if includeSelections {
		var selected bool
		data, selected, err = selectionStore.ApplyDefaults(name, data)
		if err != nil {
			return "", err
		}
		if !needsOverlay && !selected {
			return path, nil
		}
	}
	if override, err := store.Override(name); err == nil {
		if err := validateManagedDNSProxyTargets(override, data); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	data, _, err = profile.RewriteTailscaleState(data, configDir, name, true)
	if err != nil {
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

func startAndVerify(ctx context.Context, manager lifecycleBackend, client *control.Client, configPath, profileName string) (any, error) {
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
