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
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/rules"
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
	if data, isHelp, err := requestedHelp(filtered); isHelp {
		if err != nil {
			output.WriteError(stderr, jsonMode, "usage", err.Error())
			return 2
		}
		if err := output.Write(stdout, jsonMode, data); err != nil {
			output.WriteError(stderr, jsonMode, "output_error", err.Error())
			return 1
		}
		return 0
	}
	if err := validateInvocation(filtered); err != nil {
		output.WriteError(stderr, jsonMode, "usage", err.Error())
		return 2
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
		return nil, fail("usage", errors.New("command required: start, stop, restart, status, doctor, logs, config, profile, subscription, dns, proxy, connections, service, version"))
	}
	binary := os.Getenv("NAGI_MIHOMO_BIN")
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		binary = filepath.Join(filepath.Dir(exe), "mihomo")
	}
	command := args[0]
	if command == "completion" {
		if len(args) != 2 {
			return nil, usage("completion bash|zsh|fish")
		}
		script, ok := completion(args[1])
		if !ok {
			return nil, usage("completion bash|zsh|fish")
		}
		return script, nil
	}
	if command == "version" {
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		mihomoVersion := "unavailable"
		if out, err := exec.Command(binary, "-v").Output(); err == nil {
			mihomoVersion = strings.TrimSpace(string(out))
		}
		return map[string]any{"nagi": version, "mihomo": mihomoVersion, "mihomo_commit": commit, "os": runtime.GOOS, "arch": runtime.GOARCH}, nil
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, err
	}
	if command == "doctor" {
		return diagnostic.Run(ctx, paths, binary), nil
	}
	controlTimeout := 10 * time.Second
	if len(args) >= 2 && command == "proxy" && args[1] == "delay" {
		controlTimeout = 31 * time.Second
	}
	client := control.NewWithTimeout(paths.SocketPath, controlTimeout)
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), &http.Client{Timeout: 30 * time.Second})
	proxies := proxy.NewService(client)
	needsProfile := command == "start" || command == "restart" || command == "config" || command == "profile" || command == "dns" || (command == "subscription" && len(args) > 1 && args[1] == "apply")
	readsProfile := needsProfile || command == "status"
	var profileName string
	var profileErr error
	if readsProfile {
		profileName, profileErr = profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if profileErr != nil && command != "status" && command != "profile" && !(command == "subscription" && len(args) > 1 && args[1] == "apply") {
			return nil, fmt.Errorf("cannot read selected profile; run `nagi profile list`, then `nagi profile use NAME` to select a valid profile: %w", profileErr)
		}
		if profileName == "" {
			profileName = "default"
		}
	}
	if profileName == "" {
		profileName = "default"
	}
	configPath := filepath.Join(paths.ConfigDir, "profiles", profileName+".yaml")
	var manager *engine.Manager
	getManager := func() (*engine.Manager, error) {
		if manager != nil {
			return manager, nil
		}
		if command == "start" || command == "restart" || command == "config" {
			store := newProfileStore(paths, binary, client, nil)
			if _, overrideErr := store.Override(profileName); overrideErr == nil || (errors.Is(overrideErr, os.ErrNotExist) && rules.Exists(paths.ConfigDir)) {
				configPath, err = materializeProfile(store, paths.ConfigDir, profileName)
				if err != nil {
					return nil, err
				}
			} else if !errors.Is(overrideErr, os.ErrNotExist) {
				return nil, overrideErr
			}
		}
		manager, err = engine.New(engine.Options{Binary: binary, ConfigPath: configPath, Paths: paths})
		return manager, err
	}
	var profiles *profile.Store
	getProfiles := func() (*profile.Store, error) {
		if profiles != nil {
			return profiles, nil
		}
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		profiles = newProfileStore(paths, binary, client, m)
		return profiles, nil
	}
	switch command {
	case "start":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if err := ensureDefaultProfile(filepath.Join(paths.ConfigDir, "profiles", profileName+".yaml"), profileName); err != nil {
			return nil, err
		}
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		return startAndVerify(ctx, m, client, configPath, profileName)
	case "stop":
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		return m.Stop(ctx)
	case "restart":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if err := ensureDefaultProfile(filepath.Join(paths.ConfigDir, "profiles", profileName+".yaml"), profileName); err != nil {
			return nil, err
		}
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		if _, err := m.Stop(ctx); err != nil && !errors.Is(err, engine.ErrNotRunning) {
			return nil, err
		}
		return startAndVerify(ctx, m, client, configPath, profileName)
	case "status":
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		status, err := m.Status(ctx)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"running": status.Running, "socket_path": status.SocketPath, "log_path": status.LogPath}
		if profileErr == nil {
			data["profile"] = profileName
		}
		if status.PID != 0 {
			data["pid"] = status.PID
		}
		if status.StalePID != 0 {
			data["stale_pid"] = status.StalePID
			data["unexpected_exit"] = true
		}
		if profileErr != nil {
			data["profile_error"] = profileErr.Error()
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
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		if len(args) > 2 {
			return nil, usage("logs [lines]")
		}
		count := 100
		if len(args) == 2 {
			var err error
			count, err = strconv.Atoi(args[1])
			if err != nil || count < 1 || count > 10000 {
				return nil, usage("logs [lines: 1..10000]")
			}
		}
		lines, err := m.Logs(count)
		return map[string]any{"lines": lines}, err
	case "config":
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		if len(args) != 2 {
			return nil, usage("config validate|show")
		}
		switch args[1] {
		case "validate":
			if err := m.Validate(ctx); err != nil {
				return nil, fail("invalid_config", fmt.Errorf("configuration validation failed; run `nagi config show` to inspect the selected profile and check the mihomo executable: %w", err))
			}
			return map[string]any{"valid": true, "profile": profileName}, nil
		case "show":
			profiles, err := getProfiles()
			if err != nil {
				return nil, err
			}
			data, err := profiles.Show(profileName)
			if err != nil {
				return nil, err
			}
			return map[string]any{"profile": profileName, "yaml": string(data)}, nil
		}
		return nil, usage("config validate|show")
	case "profile":
		if len(args) > 1 && args[1] == "list" {
			store := profile.NewStore(paths.ConfigDir, nil, nil)
			return profileCommandWithCurrentError(ctx, args, store, profileErr)
		}
		store, err := getProfiles()
		if err != nil {
			return nil, err
		}
		return profileCommand(ctx, args, store)
	case "subscription":
		if len(args) > 1 && args[1] == "apply" {
			store, err := getProfiles()
			if err != nil {
				return nil, err
			}
			return subscriptionCommand(ctx, args, subs, store)
		}
		return subscriptionCommand(ctx, args, subs, nil)
	case "dns":
		store, err := getProfiles()
		if err != nil {
			return nil, err
		}
		return dnsCommand(ctx, args, store, profileName, client)
	case "proxy":
		return proxyCommand(ctx, args, proxies)
	case "rules":
		return rulesCommand(ctx, args, paths.ConfigDir, binary, client, proxies)
	case "connections":
		if len(args) < 2 {
			return nil, usage("connections list|close ID|close-all")
		}
		switch args[1] {
		case "list":
			if len(args) != 2 {
				return nil, usage("connections list")
			}
			connections, err := proxies.Connections(ctx)
			return map[string]any{"connections": connections}, err
		case "close":
			if len(args) != 3 {
				return nil, usage("connections close ID")
			}
			if err := proxies.Close(ctx, args[2]); err != nil {
				return nil, err
			}
			return map[string]any{"id": args[2], "closed": true}, nil
		case "close-all":
			if len(args) != 2 {
				return nil, usage("connections close-all")
			}
			if err := proxies.CloseAll(ctx); err != nil {
				return nil, err
			}
			return map[string]any{"closed": true}, nil
		default:
			return nil, usage("connections list|close ID|close-all")
		}
	case "mode":
		if len(args) == 1 {
			return proxies.Mode(ctx, nil)
		}
		if len(args) != 2 {
			return nil, usage("mode [rule|global|direct]")
		}
		return proxies.Mode(ctx, &args[1])
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
		return nil, usage("profile list|use NAME|import NAME FILE|export NAME FILE|backup NAME|restore NAME|diff NAME [OTHER|backup]|remove NAME")
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
			return nil, usage("profile use NAME (run `nagi profile use --help` for details)")
		}
		if err := store.Use(ctx, args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"current": args[2]}, nil
	case "import":
		if err := arity(args, 4); err != nil {
			return nil, usage("profile import NAME FILE (run `nagi profile import --help` for details)")
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
	case "export":
		if len(args) != 4 {
			return nil, usage("profile export NAME FILE")
		}
		data, err := store.Show(args[2])
		if err != nil {
			return nil, fail("profile_error", err)
		}
		file, err := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, fail("profile_error", err)
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(args[3])
			return nil, fail("profile_error", err)
		}
		return map[string]any{"name": args[2], "exported": true, "file": args[3]}, nil
	case "backup":
		if len(args) != 3 {
			return nil, usage("profile backup NAME")
		}
		if err := store.SaveBackup(args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"name": args[2], "backed_up": true}, nil
	case "restore":
		if len(args) != 3 {
			return nil, usage("profile restore NAME")
		}
		if err := store.RestoreBackup(ctx, args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"name": args[2], "restored": true}, nil
	case "diff":
		if len(args) < 3 || len(args) > 4 {
			return nil, usage("profile diff NAME [OTHER|backup]")
		}
		before, err := store.Show(args[2])
		if err != nil {
			return nil, fail("profile_error", err)
		}
		other := "backup"
		if len(args) == 4 {
			other = args[3]
		}
		var after []byte
		if other == "backup" {
			after, err = store.Backup(args[2])
		} else {
			after, err = store.Show(other)
		}
		if err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"name": args[2], "other": other, "diff": profileDiff(before, after), "changed": string(before) != string(after)}, nil
	case "override":
		if len(args) < 4 {
			return nil, usage("profile override set NAME FILE|show NAME|clear NAME")
		}
		switch args[2] {
		case "set":
			if len(args) != 5 {
				return nil, usage("profile override set NAME FILE")
			}
			file, err := os.Open(args[4])
			if err != nil {
				return nil, err
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
			if err != nil {
				return nil, err
			}
			if err := store.SetOverride(ctx, args[3], data); err != nil {
				return nil, fail("profile_error", err)
			}
			return map[string]any{"name": args[3], "override_saved": true}, nil
		case "show":
			if len(args) != 4 {
				return nil, usage("profile override show NAME")
			}
			data, err := store.Override(args[3])
			if err != nil {
				return nil, fail("profile_error", err)
			}
			return map[string]any{"name": args[3], "yaml": string(data)}, nil
		case "clear":
			if len(args) != 4 {
				return nil, usage("profile override clear NAME")
			}
			if err := store.ClearOverride(ctx, args[3]); err != nil {
				return nil, fail("profile_error", err)
			}
			return map[string]any{"name": args[3], "override_cleared": true}, nil
		}
		return nil, usage("profile override set NAME FILE|show NAME|clear NAME")
	case "remove":
		if err := arity(args, 3); err != nil {
			return nil, usage("profile remove NAME (run `nagi profile remove --help` for details)")
		}
		if err := store.Remove(args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		return map[string]any{"name": args[2], "removed": true, "kind": "profile"}, nil
	default:
		return nil, usage("profile list|use NAME|import NAME FILE|export NAME FILE|backup NAME|restore NAME|diff NAME [OTHER|backup]|remove NAME")
	}
}

func profileCommandWithCurrentError(ctx context.Context, args []string, store *profile.Store, currentErr error) (any, error) {
	if len(args) == 2 && args[1] == "list" {
		list, err := store.List()
		if err != nil {
			return nil, err
		}
		data := map[string]any{"profiles": list}
		if currentErr != nil {
			data["current_error"] = currentErr.Error()
		} else {
			current, err := store.Current()
			if err != nil {
				data["current_error"] = err.Error()
			} else {
				data["current"] = current
			}
		}
		return data, nil
	}
	return profileCommand(ctx, args, store)
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
		if err := profiles.Apply(ctx, args[2], data); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2], "profile": args[2], "applied": true}, nil
	default:
		return nil, usage("subscription list|add <name> <url>|update <name>|apply <name>|remove <name>")
	}
}

func proxyCommand(ctx context.Context, args []string, service *proxy.Service) (any, error) {
	if len(args) < 2 {
		return nil, usage("proxy groups|show <group>|select <group> <node>|delay NODE [URL] [TIMEOUT_MS]")
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
	case "delay":
		target := "https://www.gstatic.com/generate_204"
		timeoutMS := 5000
		if len(args) >= 4 {
			target = args[3]
		}
		if len(args) == 5 {
			timeoutMS, _ = strconv.Atoi(args[4])
		}
		return service.Delay(ctx, args[2], target, timeoutMS)
	default:
		return nil, usage("proxy groups|show <group>|select <group> <node>|delay NODE [URL] [TIMEOUT_MS]")
	}
}
