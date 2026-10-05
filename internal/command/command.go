package command

import (
	"context"
	"encoding/json"
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
	"github.com/ronigooja/Nagi/internal/runtimecontrol"
	"github.com/ronigooja/Nagi/internal/service"
	"github.com/ronigooja/Nagi/internal/subscription"
	"github.com/ronigooja/Nagi/internal/traffic"
)

type commandError struct {
	code string
	err  error
}

func (e *commandError) Error() string   { return e.err.Error() }
func fail(code string, err error) error { return &commandError{code, err} }

func Run(args []string, stdout, stderr io.Writer, version, commit string) int {
	if len(args) == 1 && args[0] == "__startup-watch" {
		return runStartupWatch(stderr, version, commit)
	}
	if len(args) == 2 && args[0] == "__traffic-watch" {
		pid, e := strconv.Atoi(args[1])
		if e != nil || pid <= 0 {
			return 2
		}
		paths, e := nagiruntime.Resolve()
		if e != nil {
			return 1
		}
		traffic.New(paths.StateDir).Watch(context.Background(), pid)
		return 0
	}
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
	if len(filtered) == 1 && filtered[0] == "tui" {
		if jsonMode {
			output.WriteError(stderr, true, "usage", "tui is interactive; omit --json and use the CLI commands for automation")
			return 2
		}
		return runTUI(stdout, stderr, version, commit)
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
	if follow, ok := data.(engine.FollowResult); ok {
		if jsonMode {
			return runJSONFollow(stdout, stderr, follow)
		}
		if err := follow.Manager.Follow(context.Background(), func(line string) error { _, err := fmt.Fprintln(stdout, line); return err }); err != nil && !errors.Is(err, context.Canceled) {
			output.WriteError(stderr, false, "output_error", err.Error())
			return 1
		}
		return 0
	}
	if err := output.Write(stdout, jsonMode, data); err != nil {
		output.WriteError(stderr, jsonMode, "output_error", err.Error())
		return 1
	}
	return 0
}

func runJSONFollow(stdout, stderr io.Writer, follow engine.FollowResult) int {
	encoder := json.NewEncoder(stdout)
	err := follow.Manager.Follow(context.Background(), func(line string) error {
		return encoder.Encode(output.Envelope{OK: true, Data: map[string]any{"line": line}})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		output.WriteError(stderr, true, "output_error", err.Error())
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
		if len(args) >= 2 && args[1] == "candidates" {
			if len(args) == 3 && (args[2] == "profile" || args[2] == "subscription") {
				paths, e := nagiruntime.Resolve()
				if e != nil {
					return nil, e
				}
				switch args[2] {
				case "profile":
					names, e := profile.NewStore(paths.ConfigDir, nil, nil).List()
					return output.Candidates(names), e
				case "subscription":
					entries, e := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil).List()
					if e != nil {
						return nil, e
					}
					names := make(output.Candidates, 0, len(entries))
					for _, entry := range entries {
						names = append(names, entry.Name)
					}
					return names, nil
				}
			}

			paths, err := nagiruntime.Resolve()
			if err != nil {
				return nil, err
			}
			service := proxy.NewService(control.New(paths.SocketPath))
			groups, err := service.Groups(ctx)
			if err != nil {
				return nil, err
			}
			names := []string{}
			if len(args) == 3 && args[2] == "groups" {
				for _, g := range groups {
					names = append(names, g.Name)
				}
				return proxy.Candidates(names), nil
			}
			if len(args) == 4 && args[2] == "nodes" {
				for _, g := range groups {
					if g.Name == args[3] {
						return proxy.Candidates(g.All), nil
					}
				}
				return proxy.Candidates(names), nil
			}
			if len(args) == 3 && args[2] == "nodes" {
				seen := map[string]bool{}
				for _, g := range groups {
					for _, n := range g.All {
						if !seen[n] {
							names = append(names, n)
							seen[n] = true
						}
					}
				}
				return proxy.Candidates(names), nil
			}
			return nil, usage("completion candidates groups|nodes [GROUP]")
		}
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
	if command == "diagnostics" {
		report := diagnostic.RunFull(ctx, paths, binary)
		if len(args) == 1 {
			return report, nil
		}
		if len(args) == 3 && args[1] == "export" {
			if err := diagnostic.Export(args[2], report); err != nil {
				return nil, fail("diagnostic_error", err)
			}
			return map[string]any{"exported": true, "path": args[2], "redacted": true}, nil
		}
		return nil, usage("diagnostics [export ABSOLUTE_FILE]")
	}
	if command == "kill-switch" {
		switch args[1] {
		case "status":
			return diagnostic.KillSwitchStatus(paths.StateDir), nil
		case "enable":
			if err := diagnostic.EnableKillSwitch(ctx, paths.StateDir, args[2], args[3:]); err != nil {
				return nil, fail("kill_switch_error", err)
			}
			return map[string]any{"enabled": true}, nil
		case "disable":
			if err := diagnostic.DisableKillSwitch(ctx, paths.StateDir); err != nil {
				return nil, fail("kill_switch_error", err)
			}
			return map[string]any{"enabled": false}, nil
		}
		return nil, usage("kill-switch status|enable|disable")
	}
	controlTimeout := 10 * time.Second
	if len(args) >= 2 && command == "proxy" && (args[1] == "delay" || args[1] == "delays") {
		controlTimeout = 31 * time.Second
	}
	client := control.NewWithTimeout(paths.SocketPath, controlTimeout)
	trafficManager := traffic.New(paths.StateDir)
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), &http.Client{Timeout: 30 * time.Second})
	proxies := proxy.NewService(client)
	needsProfile := command == "start" || command == "restart" || command == "config" || command == "profile" || command == "dns" || (command == "mode" && len(args) > 1 && (args[1] == "save" || args[1] == "saved")) || (command == "subscription" && len(args) > 1 && (args[1] == "apply" || args[1] == "preview"))
	readsProfile := needsProfile || command == "status"
	var profileName string
	var profileErr error
	if readsProfile {
		profileName, profileErr = profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if profileErr != nil && command != "status" && command != "profile" && !(command == "subscription" && len(args) > 1 && (args[1] == "apply" || args[1] == "preview")) {
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
		result, err := startAndVerify(ctx, m, client, configPath, profileName)
		if err == nil {
			_, _ = (proxy.SelectionStore{Path: filepath.Join(paths.ConfigDir, "proxy-selections.json")}).Restore(ctx, profileName, proxies)
		}
		return result, err
	case "stop":
		m, err := getManager()
		if err != nil {
			return nil, err
		}
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if trafficManager.Managed() {
			if _, e := trafficManager.Disable(ctx); e != nil {
				return nil, fail("system_proxy_error", e)
			}
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
		if trafficManager.Managed() {
			if _, e := trafficManager.Disable(ctx); e != nil {
				return nil, fail("system_proxy_error", e)
			}
		}
		if _, err := m.Stop(ctx); err != nil && !errors.Is(err, engine.ErrNotRunning) {
			return nil, err
		}
		result, err := startAndVerify(ctx, m, client, configPath, profileName)
		if err == nil {
			_, _ = (proxy.SelectionStore{Path: filepath.Join(paths.ConfigDir, "proxy-selections.json")}).Restore(ctx, profileName, proxies)
		}
		return result, err
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
		if !status.Running {
			if e := trafficManager.RestoreIfStopped(ctx, 0); e != nil {
				return nil, fail("system_proxy_error", e)
			}
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
		if len(args) == 2 && args[1] == "follow" {
			status, err := m.Status(ctx)
			if err != nil {
				return nil, err
			}
			if !status.Running {
				return nil, fail("not_running", errors.New("mihomo is not running; start it before following logs"))
			}
			return engine.FollowResult{Manager: m}, nil
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
		case "reload":
			if err := arity(args, 2); err != nil {
				return nil, err
			}
			if err := m.Validate(ctx); err != nil {
				return nil, fail("invalid_config", err)
			}
			status, err := m.Status(ctx)
			if err != nil {
				return nil, err
			}
			if !status.Running {
				return nil, fail("not_running", errors.New("mihomo is not running; start it before reloading configuration"))
			}
			if err := client.Put(ctx, "/configs?force=true", map[string]any{"path": configPath}, nil); err != nil {
				return nil, fail("mihomo_api_error", fmt.Errorf("configuration validated but reload failed; runtime state may still use the previous configuration: %w", err))
			}
			return map[string]any{"reloaded": true, "profile": profileName}, nil
		}
		return nil, usage("config validate|show|reload")
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
		if len(args) > 1 && (args[1] == "apply" || args[1] == "preview") {
			store, err := getProfiles()
			if err != nil {
				return nil, err
			}
			return subscriptionCommand(ctx, args, subs, store, proxies)
		}
		return subscriptionCommand(ctx, args, subs, nil, nil)
	case "dns":
		store, err := getProfiles()
		if err != nil {
			return nil, err
		}
		return dnsCommand(ctx, args, store, profileName, client)
	case "proxy":
		return proxyCommand(ctx, args, proxies, proxy.SelectionStore{Path: filepath.Join(paths.ConfigDir, "proxy-selections.json")}, paths.ConfigDir)
	case "rules":
		return rulesCommand(ctx, args, paths.ConfigDir, binary, client, proxies)
	case "connections":
		if len(args) < 2 {
			return nil, usage("connections list|show ID|close ID|close-all")
		}
		switch args[1] {
		case "show":
			if len(args) != 3 {
				return nil, usage("connections show ID")
			}
			connection, err := proxies.Connection(ctx, args[2])
			return connection, err
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
			return nil, usage("connections list|show ID|close ID|close-all")
		}
	case "system-proxy":
		if args[1] == "status" {
			state, e := trafficManager.Status(ctx)
			if e != nil {
				return nil, fail("system_proxy_error", e)
			}
			return state, nil
		}
		if args[1] == "disable" {
			state, e := trafficManager.Disable(ctx)
			if e != nil {
				return nil, fail("system_proxy_error", e)
			}
			return state, nil
		}
		var cfg map[string]any
		if e := client.Get(ctx, "/configs", &cfg); e != nil {
			return nil, fail("system_proxy_error", fmt.Errorf("start mihomo first: %w", e))
		}
		port := intField(cfg, "mixed-port")
		if port == 0 {
			port = intField(cfg, "port")
		}
		if port == 0 {
			return nil, fail("system_proxy_error", errors.New("no HTTP or mixed listener is configured; inspect `nagi ports status`"))
		}
		if cfg["allow-lan"] == true {
			return nil, fail("system_proxy_error", errors.New("disable LAN access before enabling the system proxy, or configure a loopback-only listener"))
		}
		status, e := getManager()
		if e != nil {
			return nil, e
		}
		engineStatus, e := status.Status(ctx)
		if e != nil || !engineStatus.Running {
			return nil, fail("system_proxy_error", errors.New("start mihomo before enabling the system proxy"))
		}
		state, e := trafficManager.Enable(ctx, engineStatus.PID, port)
		if e != nil {
			return nil, fail("system_proxy_error", e)
		}
		exe, e := os.Executable()
		if e == nil {
			watch := exec.Command(exe, "__traffic-watch", strconv.Itoa(engineStatus.PID))
			watch.Stdin = nil
			watch.Stdout = io.Discard
			watch.Stderr = io.Discard
			if e = watch.Start(); e == nil {
				_ = watch.Process.Release()
			}
		}
		if e != nil {
			_, _ = trafficManager.Disable(ctx)
			return nil, fail("system_proxy_error", fmt.Errorf("cannot start exit recovery watcher: %w", e))
		}
		return state, nil
	case "tun":
		cfg, e := trafficConfig(ctx, client)
		if e != nil {
			return nil, e
		}
		tun, _ := cfg["tun"].(map[string]any)
		enabled, _ := tun["enable"].(bool)
		if args[1] == "status" {
			return map[string]any{"enabled": enabled, "settings": tun}, nil
		}
		requested := args[1] == "enable"
		payload := map[string]any{"enable": requested}
		if requested {
			payload["auto-route"] = true
			payload["auto-detect-interface"] = true
		}
		if e := client.Patch(ctx, "/configs", map[string]any{"tun": payload}, nil); e != nil {
			return nil, e
		}
		cfg, e = trafficConfig(ctx, client)
		if e != nil {
			return nil, e
		}
		tun, _ = cfg["tun"].(map[string]any)
		enabled, _ = tun["enable"].(bool)
		if enabled != requested {
			return nil, fail("tun_error", errors.New("mihomo did not report the requested TUN state; inspect `nagi logs` and OS TUN permissions"))
		}
		return map[string]any{"enabled": enabled, "settings": tun}, nil
	case "ports":
		cfg, e := trafficConfig(ctx, client)
		if e != nil {
			return nil, e
		}
		return map[string]any{"http": intField(cfg, "port"), "https": intField(cfg, "port"), "socks": intField(cfg, "socks-port"), "mixed": intField(cfg, "mixed-port"), "bind_address": cfg["bind-address"], "allow_lan": cfg["allow-lan"]}, nil
	case "lan":
		cfg, e := trafficConfig(ctx, client)
		if e != nil {
			return nil, e
		}
		if args[1] != "status" {
			requested := args[1] == "enable"
			address := "127.0.0.1"
			if requested {
				address = "*"
				if len(args) == 3 {
					address = args[2]
				}
			}
			if e := client.Patch(ctx, "/configs", map[string]any{"allow-lan": requested, "bind-address": address}, nil); e != nil {
				return nil, e
			}
			cfg, e = trafficConfig(ctx, client)
			if e != nil {
				return nil, e
			}
		}
		enabled, _ := cfg["allow-lan"].(bool)
		return map[string]any{"enabled": enabled, "bind_address": cfg["bind-address"]}, nil
	case "mode":
		if len(args) == 1 {
			result, err := proxies.Mode(ctx, nil)
			if err != nil {
				return nil, err
			}
			result["persistent"] = false
			return result, nil
		}
		if args[1] == "saved" {
			profiles, err := getProfiles()
			if err != nil {
				return nil, err
			}
			data, err := profiles.Effective(profileName)
			if err != nil {
				return nil, err
			}
			mode, err := runtimecontrol.Mode(data)
			if err != nil {
				return nil, err
			}
			return map[string]any{"mode": mode, "persistent": true}, nil
		}
		if args[1] == "save" {
			profiles, err := getProfiles()
			if err != nil {
				return nil, err
			}
			if len(args) != 3 || !runtimecontrol.ValidMode(args[2]) {
				return nil, usage("mode save MODE")
			}
			data, overrideErr := profiles.Override(profileName)
			useOverride := overrideErr == nil
			if errors.Is(overrideErr, os.ErrNotExist) {
				data, err = profiles.Show(profileName)
			} else {
				err = overrideErr
			}
			if err != nil {
				return nil, err
			}
			updated, err := runtimecontrol.SetMode(data, args[2])
			if err != nil {
				return nil, err
			}
			if useOverride {
				err = profiles.SetOverride(ctx, profileName, updated)
			} else {
				err = profiles.Write(ctx, profileName, updated)
			}
			if err != nil {
				return nil, fail("invalid_config", fmt.Errorf("persistent mode was not saved: %w", err))
			}
			return map[string]any{"mode": args[2], "persistent": true, "changed": true}, nil
		}
		if len(args) != 2 || !runtimecontrol.ValidMode(args[1]) {
			return nil, usage("mode [rule|global|direct|save MODE|saved]")
		}
		result, err := proxies.Mode(ctx, &args[1])
		if err != nil {
			return nil, err
		}
		result["persistent"] = false
		return result, nil
	case "recover":
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		m, e := getManager()
		if e != nil {
			return nil, e
		}
		status, e := m.Recover(ctx)
		if e != nil {
			if errors.Is(e, engine.ErrNotRunning) {
				return map[string]any{"recovered": false, "reason": "no stale runtime state"}, nil
			}
			return nil, e
		}
		return map[string]any{"recovered": true, "stale_pid": status.StalePID, "socket_path": status.SocketPath}, nil
	case "startup":
		if args[1] == "status" {
			svc, e := service.Status()
			if e != nil {
				return nil, e
			}
			m, e := getManager()
			if e != nil {
				return nil, e
			}
			st, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			return map[string]any{"service": svc, "engine": st}, nil
		}
		if args[1] == "enable" {
			return service.Enable()
		}
		if args[1] == "disable" {
			return service.Disable()
		}
		if args[1] == "check" {
			m, e := getManager()
			if e != nil {
				return nil, e
			}
			st, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			if st.StalePID != 0 {
				rec, e := m.Recover(ctx)
				if e != nil {
					return nil, e
				}
				return map[string]any{"checked": true, "recovered": true, "stale_pid": rec.StalePID}, nil
			}
			reachable := false
			if st.Running {
				var v any
				reachable = client.Get(ctx, "/version", &v) == nil
			}
			return map[string]any{"checked": true, "running": st.Running, "control_api": reachable}, nil
		}
		return nil, usage("startup status|enable|disable|check")
	case "service":

		if len(args) != 2 {
			return nil, usage("service install|uninstall|status")
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
		case "status":
			return service.Status()
		default:
			return nil, usage("service install|uninstall|status")
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
		changes, warnings, parseErr := structuralDiff(before, after)
		if parseErr != nil {
			warnings = []string{parseErr.Error() + "; line diff remains available"}
			changes = []StructuralChange{}
		}
		return map[string]any{"name": args[2], "other": other, "diff": profileDiff(before, after), "changed": string(before) != string(after), "changes": changes, "warnings": warnings}, nil
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

func subscriptionCommand(ctx context.Context, args []string, store *subscription.Store, profiles *profile.Store, proxies *proxy.Service) (any, error) {
	if len(args) < 2 {
		return nil, usage("subscription list|add <name> <url>|update <name>|preview <name>|apply <name>|remove <name>")
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
	case "preview", "apply":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		data, err := store.Cached(args[2])
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		incoming, format, err := subscription.Document(data)
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		var existing map[string]any
		if old, showErr := profiles.Show(args[2]); showErr == nil {
			existing, _, err = subscription.Document(old)
			if err != nil {
				return nil, fail("subscription_error", errors.New("existing profile cannot be merged; inspect or remove it before applying"))
			}
		} else if !errors.Is(showErr, os.ErrNotExist) {
			return nil, fail("subscription_error", showErr)
		}
		preview := subscription.Compare(args[2], format, incoming, existing)
		if args[1] == "preview" {
			return preview, nil
		}
		var selected []proxy.Group
		if current, currentErr := profiles.Current(); currentErr == nil && current == args[2] && proxies != nil {
			selected, _ = proxies.Groups(ctx)
		}
		merged, err := subscription.Encode(subscription.Merge(incoming, existing))
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		if err := profiles.Apply(ctx, args[2], merged); err != nil {
			return nil, fail("subscription_error", err)
		}
		restored := 0
		for _, group := range selected {
			if group.Now != "" && proxies.Select(ctx, group.Name, group.Now) == nil {
				restored++
			}
		}
		return map[string]any{"name": args[2], "profile": args[2], "applied": true, "preview": preview, "selections_restored": restored}, nil
	default:
		return nil, usage("subscription list|add <name> <url>|update <name>|preview <name>|apply <name>|remove <name>")
	}
}

func proxyCommand(ctx context.Context, args []string, service *proxy.Service, selections proxy.SelectionStore, configDir string) (any, error) {
	if len(args) < 2 {
		return nil, usage("proxy groups|show GROUP|search QUERY|select GROUP NODE|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore")
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
		current, err := profile.NewStore(configDir, nil, nil).Current()
		if err != nil {
			return nil, fail("proxy_selection_error", fmt.Errorf("cannot read selected profile to save choice: %w", err))
		}
		if err := service.Select(ctx, args[2], args[3]); err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		if err := selections.Save(current, args[2], args[3]); err != nil {
			return nil, fail("proxy_selection_error", fmt.Errorf("selected node in running engine, but could not save choice: %w", err))
		}
		return map[string]any{"group": args[2], "node": args[3]}, nil
	case "restore":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		current, err := profile.NewStore(configDir, nil, nil).Current()
		if err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		return selections.Restore(ctx, current, service)
	case "search":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		groups, err := service.Search(ctx, args[2])
		return map[string]any{"query": args[2], "groups": groups}, err
	case "delays":
		target := "https://www.gstatic.com/generate_204"
		timeoutMS := 5000
		if len(args) >= 4 {
			target = args[3]
		}
		if len(args) == 5 {
			timeoutMS, _ = strconv.Atoi(args[4])
		}
		return service.Delays(ctx, args[2], target, timeoutMS)
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
		return nil, usage("proxy groups|show GROUP|search QUERY|select GROUP NODE|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore")
	}
}

func trafficConfig(ctx context.Context, client *control.Client) (map[string]any, error) {
	var cfg map[string]any
	if err := client.Get(ctx, "/configs", &cfg); err != nil {
		return nil, fmt.Errorf("mihomo must be running; run `nagi start` or inspect `nagi status`: %w", err)
	}
	return cfg, nil
}
func intField(m map[string]any, key string) int { v, _ := m[key].(float64); return int(v) }
