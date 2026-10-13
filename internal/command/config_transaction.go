package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/traffic"
	"gopkg.in/yaml.v3"
)

// configMutation covers operations that can replace the selected effective
// configuration. Every CLI process takes config.lock before entering these
// paths; lifecycle commands use the same outer lock before their quit and
// lifecycle locks.
func configMutation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "start", "stop", "restart", "quit", "system-proxy":
		return true
	case "config":
		return len(args) > 1 && args[1] == "reload"
	case "profile":
		return len(args) > 1 && (args[1] == "use" || args[1] == "import" || args[1] == "restore" || args[1] == "remove" || args[1] == "backup" || args[1] == "override" && (len(args) < 3 || args[2] != "default-status"))
	case "subscription":
		return len(args) > 1 && args[1] != "list" && args[1] != "preview" && args[1] != "update"
	case "rules":
		return len(args) > 1 && args[1] != "list" && args[1] != "providers" && args[1] != "custom" && args[1] != "conflicts" && args[1] != "connection"
	case "dns":
		return len(args) > 1 && (args[1] == "set" || args[1] == "exception" || args[1] == "tun")
	case "proxy":
		if len(args) < 2 {
			return false
		}
		if args[1] == "tailscale" {
			return !tailscaleRead(args)
		}
		if args[1] == "provider" {
			return len(args) > 2 && (args[2] == "add" || args[2] == "remove" || args[2] == "update" || args[2] == "healthcheck")
		}
		return args[1] == "select" || args[1] == "auto" || args[1] == "delays" || args[1] == "restore"
	case "privileged-helper":
		return len(args) > 1 && (args[1] == "enable" || args[1] == "disable")
	case "mode", "tun", "lan":
		return true
	case "startup":
		return len(args) > 1 && (args[1] == "ensure" || args[1] == "shutdown" || args[1] == "check" || args[1] == "login")
	}
	return false
}

func transactionalMutation(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "proxy":
		return tailscaleInvocation(args) && !tailscaleRead(args) || len(args) > 2 && args[1] == "provider" && (args[2] == "add" || args[2] == "remove")
	case "profile":
		return args[1] == "use" || args[1] == "import" || args[1] == "restore" || args[1] == "remove" || args[1] == "override" && (len(args) < 3 || args[2] != "default-status")
	case "subscription":
		return args[1] == "apply"
	case "rules":
		return args[1] != "list" && args[1] != "providers" && args[1] != "custom" && args[1] != "conflicts" && args[1] != "connection"
	case "dns":
		return args[1] == "set" || args[1] == "exception" || args[1] == "tun"
	case "config":
		return args[1] == "reload"
	case "mode":
		return args[1] == "save"
	}
	return false
}

type fileImage struct {
	data []byte
	mode fs.FileMode
}

type runningImage struct {
	ActivationID string `json:"activation_id"`
	Effective    []byte `json:"effective"`
	Profile      string `json:"profile,omitempty"`
}

func runningImagePath(dir string) string { return filepath.Join(dir, ".running-effective.json") }

func recordRunningImage(dir string, client *control.Client, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	return recordRunningImageBytes(dir, client, data)
}

func recordRunningImageBytes(dir string, client *control.Client, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _, err := configID(ctx, client)
	if err != nil {
		return err
	}
	selected, _ := profile.NewStore(dir, nil, nil).Current()
	encoded, err := json.Marshal(runningImage{ActivationID: id, Effective: data, Profile: selected})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".running-effective-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), runningImagePath(dir))
}

func snapshotConfig(dir string) (map[string]fileImage, error) {
	images := map[string]fileImage{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if path == filepath.Join(dir, ".tailscale-state") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("configuration path is not a regular file: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		images[path] = fileImage{data, info.Mode().Perm()}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return images, nil
	}
	return images, err
}

func restoreConfig(dir string, images map[string]fileImage) error {
	current, err := snapshotConfig(dir)
	if err != nil {
		return err
	}
	var problems []error
	for path := range current {
		if _, exists := images[path]; !exists {
			problems = append(problems, os.Remove(path))
		}
	}
	for path, image := range images {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			problems = append(problems, err)
			continue
		}
		if err := os.WriteFile(path, image.data, image.mode); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func mutationClient(paths nagiruntime.Paths) *control.Client {
	if mode, err := readBackend(paths); err == nil && mode == privilegedBackend {
		return (helperBackend{paths: paths}).controlClient()
	}
	return control.New(paths.SocketPath)
}

func configID(ctx context.Context, client *control.Client) (string, map[string]any, error) {
	var cfg map[string]any
	err := client.Get(ctx, "/configs", &cfg)
	if err != nil {
		return "", nil, err
	}
	id, _ := cfg["activation_id"].(string)
	return id, cfg, nil
}

func applyTransaction(ctx context.Context, paths nagiruntime.Paths, args []string, fn func() (any, error)) (any, error) {
	images, err := snapshotConfig(paths.ConfigDir)
	if err != nil {
		return nil, err
	}
	client := mutationClient(paths)
	beforeID, beforeCfg, activeErr := configID(ctx, client)
	var oldEffective []byte
	var oldErr error
	// Keep the original profile identity across recovery. The helper uses the
	// normalized basename to allocate persistent Tailscale state.
	previousProfile, _ := profile.NewStore(paths.ConfigDir, nil, nil).Current()
	if activeErr == nil {
		var previous runningImage
		if saved, e := os.ReadFile(runningImagePath(paths.ConfigDir)); e == nil && json.Unmarshal(saved, &previous) == nil && previous.ActivationID == beforeID && len(previous.Effective) > 0 {
			oldEffective = previous.Effective
			if previous.Profile != "" && len(previous.Profile) <= 128 && strings.Trim(previous.Profile, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == "" {
				previousProfile = previous.Profile
			}
		}
		store := profile.NewStore(paths.ConfigDir, nil, nil)
		var name string
		if len(oldEffective) == 0 {
			name, oldErr = store.Current()
		}
		if oldErr == nil && len(oldEffective) == 0 {
			oldEffective, oldErr = store.Effective(name)
			if oldErr == nil {
				oldEffective, oldErr = applySavedTUN(paths, oldEffective)
			}
		}
		// Runtime TUN patches do not advance the activation marker. Preserve the
		// actual prior settings in the rollback PUT itself so an older recorded
		// image cannot momentarily close the adapter before the recovery PATCH.
		if oldErr == nil {
			if priorTUN, ok := beforeCfg["tun"].(map[string]any); ok {
				var doc map[string]any
				if e := yaml.Unmarshal(oldEffective, &doc); e != nil || doc == nil {
					oldErr = errors.New("prior effective configuration is invalid YAML")
				} else {
					tun, _ := doc["tun"].(map[string]any)
					if tun == nil {
						tun = map[string]any{}
						doc["tun"] = tun
					}
					for key, value := range tunRuntimeSnapshot(priorTUN) {
						tun[key] = value
					}
					oldEffective, oldErr = yaml.Marshal(doc)
				}
			}
		}
		if oldErr == nil {
			binary := os.Getenv("NAGI_MIHOMO_BIN")
			if binary == "" {
				exe, e := os.Executable()
				if e != nil {
					oldErr = e
				} else {
					binary = filepath.Join(filepath.Dir(exe), "mihomo")
				}
			}
			if oldErr == nil {
				oldErr = newProfileStore(paths, binary, client, nil).Validate(oldEffective)
			}
		}
	}
	result, actionErr := fn()
	if actionErr == nil && activeErr == nil {
		if currentID, _, e := configID(ctx, client); e != nil {
			actionErr = e
		} else if currentID != beforeID {
			actionErr = confirmSavedTUN(ctx, paths, client)
		}
	}
	if actionErr == nil {
		if activeErr == nil && runtime.GOOS == "darwin" {
			_, cfg, e := configID(ctx, client)
			if e != nil {
				actionErr = fmt.Errorf("configuration changed but current listener could not be read: %w", e)
			} else {
				port := intField(cfg, "mixed-port")
				if port == 0 {
					port = intField(cfg, "port")
				}
				if e := traffic.New(paths.StateDir).SyncPort(ctx, port); e != nil {
					actionErr = fmt.Errorf("configuration changed but managed system proxy could not be synchronized: %w", e)
				}
			}
		}
		if actionErr == nil && activeErr == nil {
			_, actionErr = synchronizeTUNDNS(ctx, paths, client, 0)
		}
		if actionErr == nil {
			if activeErr == nil {
				store := profile.NewStore(paths.ConfigDir, nil, nil)
				if name, e := store.Current(); e == nil {
					if effective, e := store.Effective(name); e == nil {
						effective, e = applySavedTUN(paths, effective)
						if e == nil {
							actionErr = recordRunningImageBytes(paths.ConfigDir, client, effective)
						} else {
							actionErr = e
						}
					} else {
						actionErr = e
					}
				}
			}
			if actionErr == nil {
				return result, nil
			}
		}
	}
	fileErr := restoreConfig(paths.ConfigDir, images)
	if activeErr != nil {
		return nil, errors.Join(actionErr, fileErr)
	}
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	currentID, _, checkErr := configID(recoveryCtx, client)
	if checkErr != nil {
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restoration unconfirmed: %w", checkErr))
	}
	if currentID == beforeID {
		if _, dnsErr := synchronizeTUNDNS(recoveryCtx, paths, client, 0); dnsErr != nil {
			return nil, errors.Join(actionErr, fileErr, fmt.Errorf("prior runtime unchanged but DNS restoration failed: %w", dnsErr))
		}
		if errors.Is(actionErr, errActivationMayStillApply) {
			return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime recovery incomplete: observed activation ID %q matches the prior configuration, but the original PUT may still apply later; after it settles, inspect `nagi status` and retry `nagi config reload`", currentID))
		}
		return nil, errors.Join(actionErr, fileErr)
	}
	if oldErr != nil {
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restoration unavailable: prior effective configuration did not validate: %w; current activation ID %s", oldErr, currentID))
	}
	if len(oldEffective) == 0 {
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restoration unavailable: prior effective configuration is empty; current activation ID %s", currentID))
	}
	file, e := os.CreateTemp(filepath.Join(paths.ConfigDir, "profiles"), ".nagi-restore-*.yaml")
	if e != nil {
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restoration failed: %w", e))
	}
	defer os.Remove(file.Name())
	_, e = file.Write(oldEffective)
	e = errors.Join(e, file.Close())
	recoveryPath := file.Name()
	if e == nil && previousProfile != "" {
		recoveryPath = filepath.Join(paths.ConfigDir, "profiles", ".restore-"+previousProfile+".yaml")
		e = os.Rename(file.Name(), recoveryPath)
		defer os.Remove(recoveryPath)
	}
	if e == nil {
		e = activateConfig(recoveryCtx, client, recoveryPath)
	}
	if e != nil {
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restoration failed; current state is uncertain: %w", e))
	}
	if priorTUN, ok := beforeCfg["tun"].(map[string]any); ok {
		if e := restoreRuntimeTUN(recoveryCtx, client, priorTUN); e != nil {
			return nil, errors.Join(actionErr, fileErr, fmt.Errorf("configuration restored but prior temporary TUN settings could not be restored: %w", e))
		}
	}
	if runtime.GOOS == "darwin" {
		_, cfg, e := configID(recoveryCtx, client)
		if e == nil {
			port := intField(cfg, "mixed-port")
			if port == 0 {
				port = intField(cfg, "port")
			}
			e = traffic.New(paths.StateDir).SyncPort(recoveryCtx, port)
		}
		if e != nil {
			if errors.Is(actionErr, errActivationMayStillApply) {
				return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime recovery incomplete: rollback PUT was confirmed, but managed system proxy restoration failed and the original PUT may still apply later: %w; after it settles, retry `nagi config reload`", e))
			}
			return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restored but system proxy restoration failed: %w", e))
		}
	}
	if _, dnsErr := synchronizeTUNDNS(recoveryCtx, paths, client, 0); dnsErr != nil {
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime restored but system DNS restoration failed: %w", dnsErr))
	}
	if errors.Is(actionErr, errActivationMayStillApply) {
		observedID, _, observeErr := configID(recoveryCtx, client)
		if observeErr != nil {
			return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime recovery incomplete: rollback PUT was confirmed, but the original PUT may still apply later; current activation ID could not be read: %w; after it settles, retry `nagi config reload`", observeErr))
		}
		return nil, errors.Join(actionErr, fileErr, fmt.Errorf("runtime recovery incomplete: rollback PUT was confirmed; latest observed activation ID is %q, but the original PUT may still apply later; after it settles, inspect `nagi status` and retry `nagi config reload`", observedID))
	}
	return nil, errors.Join(actionErr, fileErr, fmt.Errorf("prior running configuration restored with a confirmed activation ID"))
}
