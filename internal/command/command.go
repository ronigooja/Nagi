package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/service"
	"github.com/ronigooja/Nagi/internal/subscription"
)

type commandError struct {
	code string
	err  error
}

func (e *commandError) Error() string   { return e.err.Error() }
func fail(code string, err error) error { return &commandError{code, err} }

func Run(args []string, stdout, stderr io.Writer, version, commit string) int {
	jsonMode := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	data, err := execute(context.Background(), filtered, version, commit)
	if err != nil {
		code := "internal_error"
		var ce *commandError
		var ae *control.APIError
		switch {
		case errors.As(err, &ce):
			code = ce.code
		case errors.Is(err, engine.ErrAlreadyRunning):
			code = "already_running"
		case errors.Is(err, engine.ErrNotRunning):
			code = "not_running"
		case errors.As(err, &ae):
			code = "mihomo_api_error"
		case errors.Is(err, os.ErrNotExist):
			code = "not_found"
		}
		output.WriteError(stderr, jsonMode, code, err.Error())
		if code == "usage" {
			return 2
		}
		return 1
	}
	if err := output.Write(stdout, jsonMode, data); err != nil {
		output.WriteError(stderr, jsonMode, "output_error", err.Error())
		return 1
	}
	return 0
}

func execute(ctx context.Context, args []string, version, commit string) (any, error) {
	if len(args) == 0 {
		return nil, fail("usage", errors.New("command required: start, stop, restart, status, logs, config, profile, subscription, proxy, connections, service, version"))
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, err
	}
	binary := os.Getenv("NAGI_MIHOMO_BIN")
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		binary = filepath.Join(filepath.Dir(exe), "mihomo")
	}
	profileName, err := profile.NewStore(paths.ConfigDir, nil, nil).Current()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(paths.ConfigDir, "profiles", profileName+".yaml")
	manager, err := engine.New(engine.Options{Binary: binary, ConfigPath: configPath, Paths: paths})
	if err != nil {
		return nil, err
	}
	client := control.New(paths.SocketPath)
	profiles := newProfileStore(paths, binary, client, manager)
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), &http.Client{Timeout: 30 * time.Second})
	proxies := proxy.NewService(client)
	command := args[0]
	switch command {
	case "version":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		mihomoVersion := "unavailable"
		if out, err := exec.Command(binary, "-v").Output(); err == nil {
			mihomoVersion = strings.TrimSpace(string(out))
		}
		return map[string]any{"nagi": version, "mihomo": mihomoVersion, "mihomo_commit": commit, "os": runtime.GOOS, "arch": runtime.GOARCH}, nil
	case "start":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if err := ensureDefaultProfile(configPath, profileName); err != nil {
			return nil, err
		}
		return startAndVerify(ctx, manager, client, configPath, profileName)
	case "stop":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		return manager.Stop(ctx)
	case "restart":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if err := ensureDefaultProfile(configPath, profileName); err != nil {
			return nil, err
		}
		if _, err := manager.Stop(ctx); err != nil && !errors.Is(err, engine.ErrNotRunning) {
			return nil, err
		}
		return startAndVerify(ctx, manager, client, configPath, profileName)
	case "status":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		status, err := manager.Status(ctx)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"running": status.Running, "profile": profileName, "socket_path": status.SocketPath, "log_path": status.LogPath}
		if status.PID != 0 {
			data["pid"] = status.PID
		}
		if status.StalePID != 0 {
			data["stale_pid"] = status.StalePID
			data["unexpected_exit"] = true
		}
		if status.Running {
			var v struct {
				Version string `json:"version"`
			}
			if client.Get(ctx, "/version", &v) == nil {
				data["version"] = v.Version
			}
			var cfg map[string]any
			if client.Get(ctx, "/configs", &cfg) == nil {
				data["mixed_port"] = cfg["mixed-port"]
			}
		}
		return data, nil
	case "logs":
		if len(args) > 2 {
			return nil, usage("logs [lines]")
		}
		count := 100
		if len(args) == 2 {
			if _, err := fmt.Sscanf(args[1], "%d", &count); err != nil || count < 1 || count > 10000 {
				return nil, usage("logs [lines: 1..10000]")
			}
		}
		lines, err := manager.Logs(count)
		return map[string]any{"lines": lines}, err
	case "config":
		if len(args) != 2 {
			return nil, usage("config validate|show")
		}
		switch args[1] {
		case "validate":
			if err := manager.Validate(ctx); err != nil {
				return nil, fail("invalid_config", err)
			}
			return map[string]any{"valid": true, "profile": profileName}, nil
		case "show":
			data, err := profiles.Show(profileName)
			if err != nil {
				return nil, err
			}
			return map[string]any{"profile": profileName, "yaml": string(data)}, nil
		}
		return nil, usage("config validate|show")
	case "profile":
		return profileCommand(ctx, args, profiles)
	case "subscription":
		return subscriptionCommand(ctx, args, subs, profiles)
	case "proxy":
		return proxyCommand(ctx, args, proxies)
	case "connections":
		if len(args) != 2 || args[1] != "list" {
			return nil, usage("connections list")
		}
		connections, err := proxies.Connections(ctx)
		return map[string]any{"connections": connections}, err
	case "service":
		if len(args) != 2 {
			return nil, usage("service install|uninstall")
		}
		switch args[1] {
		case "install":
			exe, err := os.Executable()
			if err != nil {
				return nil, err
			}
			return service.Install(exe)
		case "uninstall":
			return service.Uninstall()
		default:
			return nil, usage("service install|uninstall")
		}
	default:
		return nil, usage("unknown command: " + command)
	}
}

func arity(args []string, n int) error {
	if len(args) != n {
		return usage("invalid number of arguments")
	}
	return nil
}
func usage(message string) error { return fail("usage", errors.New(message)) }

func profileCommand(ctx context.Context, args []string, store *profile.Store) (any, error) {
	if len(args) < 2 {
		return nil, usage("profile list|use <name>|import <name> <file>")
	}
	switch args[1] {
	case "list":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		list, err := store.List()
		if err != nil {
			return nil, err
		}
		current, err := store.Current()
		return map[string]any{"profiles": list, "current": current}, err
	case "use":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		if err := store.Use(ctx, args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"current": args[2]}, nil
	case "import":
		if err := arity(args, 4); err != nil {
			return nil, err
		}
		file, err := os.Open(args[3])
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 8<<20 {
			return nil, fail("profile_error", errors.New("profile exceeds 8 MiB"))
		}
		if err := store.Write(ctx, args[2], data); err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"name": args[2], "imported": true}, nil
	default:
		return nil, usage("profile list|use <name>|import <name> <file>")
	}
}

func subscriptionCommand(ctx context.Context, args []string, store *subscription.Store, profiles *profile.Store) (any, error) {
	if len(args) < 2 {
		return nil, usage("subscription list|add <name> <url>|update <name>|apply <name>|remove <name>")
	}
	switch args[1] {
	case "list":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		list, err := store.List()
		return map[string]any{"subscriptions": list}, err
	case "add":
		if err := arity(args, 4); err != nil {
			return nil, err
		}
		if err := store.Add(args[2], args[3]); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2]}, nil
	case "update":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		result, err := store.Refresh(ctx, args[2])
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		return result, nil
	case "remove":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		if err := store.Remove(args[2]); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2], "removed": true}, nil
	case "apply":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		data, err := store.Cached(args[2])
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		if err := profiles.Write(ctx, args[2], data); err != nil {
			return nil, fail("subscription_error", err)
		}
		if err := profiles.Use(ctx, args[2]); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2], "profile": args[2], "applied": true}, nil
	default:
		return nil, usage("subscription list|add <name> <url>|update <name>|apply <name>|remove <name>")
	}
}

func proxyCommand(ctx context.Context, args []string, service *proxy.Service) (any, error) {
	if len(args) < 2 {
		return nil, usage("proxy groups|show <group>|select <group> <node>")
	}
	switch args[1] {
	case "groups":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		groups, err := service.Groups(ctx)
		return map[string]any{"groups": groups}, err
	case "show":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		return service.Show(ctx, args[2])
	case "select":
		if err := arity(args, 4); err != nil {
			return nil, err
		}
		if err := service.Select(ctx, args[2], args[3]); err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		return map[string]any{"group": args[2], "node": args[3]}, nil
	default:
		return nil, usage("proxy groups|show <group>|select <group> <node>")
	}
}
