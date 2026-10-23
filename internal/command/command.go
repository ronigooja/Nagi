package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/diagnostic"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/installation"
	"github.com/ronigooja/Nagi/internal/installtx"
	"github.com/ronigooja/Nagi/internal/menu"
	"github.com/ronigooja/Nagi/internal/output"
	privilegedserver "github.com/ronigooja/Nagi/internal/privileged/server"
	privilegedservice "github.com/ronigooja/Nagi/internal/privileged/service"
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
func (e *commandError) Unwrap() error   { return e.err }
func fail(code string, err error) error { return &commandError{code, err} }

func Run(args []string, stdout, stderr io.Writer, version, commit string) int {
	return RunWithInput(args, os.Stdin, stdout, stderr, version, commit)
}

// RunWithInput keeps command input separate from argv, including pasted keys.
func RunWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer, version, commit string) int {
	if len(args) == 1 && args[0] == "__nagi-capture-service" {
		return runCaptureService()
	}
	if len(args) == 2 && args[0] == "__nagi-request-rewrite-supervisor" {
		return runRewriteSupervisor(args[1])
	}

	if len(args) > 0 && args[0] == "__install-transaction" {
		return installtx.Run(args[1:], stdout, stderr)
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	if len(args) == 1 && args[0] == "__default-profile" {
		if _, err := io.WriteString(stdout, defaultProfile); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if len(args) > 0 && args[0] == "__privileged-helper" {
		if len(args) != 2 || os.Geteuid() != 0 {
			output.WriteError(stderr, false, "privileged_helper_error", "root helper requires a single target UID and root privileges")
			return 1
		}
		uid, err := strconv.Atoi(args[1])
		if err != nil || uid <= 0 {
			output.WriteError(stderr, false, "privileged_helper_error", "invalid target UID")
			return 1
		}
		account, err := user.LookupId(args[1])
		if err != nil || account.HomeDir == "" {
			output.WriteError(stderr, false, "privileged_helper_error", "target user is unavailable")
			return 1
		}
		sourceDir := filepath.Join(account.HomeDir, ".config", "nagi")
		if runtime.GOOS == "darwin" {
			sourceDir = filepath.Join(account.HomeDir, "Library", "Application Support", "Nagi", "config")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := privilegedserver.RunDaemon(ctx, uid, "/usr/local/bin/mihomo", sourceDir); err != nil {
			output.WriteError(stderr, false, "privileged_helper_error", err.Error())
			return 1
		}
		return 0
	}
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
		if e := runExitWatcher(context.Background(), paths, pid); e != nil {
			output.WriteError(stderr, false, "traffic_recovery_error", e.Error())
			return 1
		}
		return 0
	}
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
		}
	}
	languageArgs, locale, languageErr := parseLanguage(args, os.Getenv)
	if languageErr != nil {
		output.WriteMessageError(stderr, jsonMode, locale, "usage", languageErr)
		return 2
	}
	filtered := make([]string, 0, len(languageArgs))
	for _, arg := range languageArgs {
		if arg != "--json" {
			filtered = append(filtered, arg)
		}
	}
	ctx := context.WithValue(context.Background(), languageContextKey{}, locale)
	helpLanguage := locale
	if jsonMode {
		helpLanguage = i18n.New(i18n.English)
	}
	if data, isHelp, err := requestedHelp(filtered, helpLanguage); isHelp {
		if err != nil {
			output.WriteErrorCause(stderr, jsonMode, locale, "usage", err)
			return 2
		}
		if err := output.WriteLocalized(stdout, jsonMode, locale, data); err != nil {
			output.WriteErrorCause(stderr, jsonMode, locale, "output_error", err)
			return 1
		}
		return 0
	}
	// Normalize the public spelling before validation, sensitive input preparation,
	// and dispatch so every operation uses the existing implementation. Help above
	// retains the invoked spelling from its generated command specs.
	if len(filtered) > 0 && filtered[0] == "rewrite" {
		filtered[0] = "request-rewrite"
	}
	if err := validateInvocation(filtered); err != nil {
		output.WriteErrorCause(stderr, jsonMode, locale, "usage", err)
		return 2
	}
	if len(filtered) == 1 && filtered[0] == "menu" {
		inputFile, inputOK := stdin.(*os.File)
		if jsonMode || !inputOK || !menu.IsTerminal(inputFile) {
			output.WriteMessageError(stderr, jsonMode, locale, "usage", messageError("cli.error.menu.terminal_required", "menu requires interactive terminal input and output; use an ordinary command or --json for automation"))
			return 2
		}
		outFile, ok := stdout.(*os.File)
		if !ok || !menu.IsTerminal(outFile) {
			output.WriteMessageError(stderr, jsonMode, locale, "usage", messageError("cli.error.menu.terminal_required", "menu requires interactive terminal input and output; use an ordinary command or --json for automation"))
			return 2
		}
		runner := menu.NewInputCommandRunner(func(argv []string, input io.Reader, out, errOut io.Writer) int {
			return RunWithInput(argv, input, out, errOut, version, commit)
		})
		return menu.Run(ctx, menu.NewSessionWithLanguage(runner, stdin, stdout, locale))
	}
	if len(filtered) == 2 && filtered[0] == "subscription" && filtered[1] == "watch" {
		return runForegroundSubscriptionWatch(ctx, stdout, stderr, jsonMode, version, commit)
	}
	if len(filtered) >= 2 && filtered[0] == "connections" && filtered[1] == "watch" {
		return runConnectionsWatch(ctx, filtered, stdout, stderr, jsonMode)
	}
	if len(filtered) == 2 && filtered[0] == "traffic" && filtered[1] == "watch" {
		return runTrafficWatch(ctx, stdout, stderr, jsonMode)
	}
	var inputErr error
	if len(filtered) > 0 && filtered[0] == "request-rewrite" {
		o, _ := parseRewriteArguments(filtered)
		if jsonMode && (o.path == "request-rewrite export" || o.path == "request-rewrite ca export" || o.path == "request-rewrite capture export") {
			output.WriteMessageError(stderr, jsonMode, locale, "usage", messageError("cli.error.export.json_unsupported", "raw export does not accept --json; omit --json and use FILE or -"))
			return 2
		}
		var input []byte
		if o.path != "request-rewrite capture import" {
			input, inputErr = prepareRewriteInput(o, stdin)
		}
		ctx = context.WithValue(ctx, captureInputKey{}, stdin)
		ctx = context.WithValue(ctx, captureOutputKey{}, stdout)
		ctx = context.WithValue(ctx, rewriteInputKey{}, input)
	}
	if nodeInvocation(filtered) {
		options, _ := nodeArguments(filtered) // Invocation was validated above.
		if options.AuthKey != nil {
			var key *string
			key, inputErr = prepareTailscaleAuthInput(filtered, stdin, stderr, jsonMode, options.stdin, locale)
			ctx = context.WithValue(ctx, tailscaleAuthInputKey{}, tailscaleAuthInput{key: key})
		}
	}
	var data any
	err := inputErr
	if err == nil {
		data, err = execute(ctx, filtered, version, commit)
	}
	if err != nil {
		code := "internal_error"
		var ce *commandError
		var ae *control.APIError
		switch {
		case errors.Is(err, control.ErrResponseTooLarge):
			code = "response_too_large"
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
		output.WriteErrorCause(stderr, jsonMode, locale, code, err)
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
			output.WriteErrorCause(stderr, false, locale, "output_error", err)
			return 1
		}
		return 0
	}
	if follow, ok := data.(helperFollowResult); ok {
		if jsonMode {
			encoder := json.NewEncoder(stdout)
			err := follow.Follow(context.Background(), func(line string) error {
				return encoder.Encode(output.Envelope{OK: true, Data: map[string]any{"line": line}})
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				output.WriteErrorCause(stderr, true, locale, "output_error", err)
				return 1
			}
			return 0
		}
		err := follow.Follow(context.Background(), func(line string) error { _, err := fmt.Fprintln(stdout, line); return err })
		if err != nil && !errors.Is(err, context.Canceled) {
			output.WriteErrorCause(stderr, false, locale, "output_error", err)
			return 1
		}
		return 0
	}
	if err := output.WriteLocalized(stdout, jsonMode, locale, data); err != nil {
		output.WriteErrorCause(stderr, jsonMode, locale, "output_error", err)
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

func executeInternal(ctx context.Context, args []string, version, commit string) (any, error) {
	if len(args) == 0 {
		return nil, fail("usage", messageError("cli.error.command.command_required_start_stop_quit_restart_status_doctor_logs_config_p", "command required: start, stop, quit, restart, status, doctor, logs, config, profile, subscription, dns, proxy, connections, service, version"))
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
	if command == "request-rewrite" {
		paths, e := nagiruntime.Resolve()
		if e != nil {
			return nil, e
		}
		return requestRewriteCommand(ctx, args, paths)
	}
	if command == "app" || command == "update" || command == "uninstall" {
		manager, err := installation.New(version)
		if err != nil {
			return nil, fail("installation_error", err)
		}
		action, tag := command, ""
		purgeData := false
		if command == "app" {
			action = "app-" + args[1]
			if len(args) == 3 {
				tag = args[2]
			}
		} else if command == "uninstall" && len(args) == 2 {
			purgeData = true
		} else if len(args) == 2 {
			if args[1] == "--terminal" {
				action = "terminal-update"
			} else if args[1] == "--app" {
				action = "background-update"
			} else {
				tag = args[1]
			}
		}
		if action == "background-update" {
			var cancel context.CancelFunc
			ctx, cancel = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer cancel()
			var timeoutCancel context.CancelFunc
			ctx, timeoutCancel = context.WithTimeout(ctx, 14*time.Minute)
			defer timeoutCancel()
		}
		manager.Locale = LanguageContext(ctx)
		result, err := manager.ManageWithMessages(ctx, action, tag, purgeData)
		if manager.PreparedArtifact != "" && err != nil {
			_ = installation.SaveUpdateError(err)
		}
		if err != nil {
			return nil, fail("installation_error", err)
		}
		return result, nil
	}
	if command == "completion" {
		if len(args) >= 3 && args[1] == "candidates" && (args[2] == "rewrite-scripts" || args[2] == "rewrite-rules") {
			return rewriteCandidates(args)
		}
		if len(args) >= 2 && args[1] == "candidates" {
			if len(args) == 3 && args[2] == "rules" {
				paths, err := nagiruntime.Resolve()
				if err != nil {
					return nil, err
				}
				name, err := profile.NewStore(paths.ConfigDir, nil, nil).Current()
				if err != nil {
					return nil, fail("rule_error", err)
				}
				doc, err := rules.New(paths.ConfigDir, name, nil).List()
				if err != nil {
					return nil, fail("rule_error", err)
				}
				names := make(output.Candidates, 0, len(doc.Entries))
				for _, entry := range doc.Entries {
					names = append(names, entry.Name)
				}
				return names, nil
			}

			if args[2] == "healthcheck-targets" {
				paths, e := nagiruntime.Resolve()
				if e != nil {
					return nil, e
				}
				if args[3] == "subscription" {
					entries, e := subscription.NewStore(paths.ConfigDir, "", nil).List()
					names := output.Candidates{}
					for _, entry := range entries {
						names = append(names, entry.Name)
					}
					return names, e
				}
				store := profile.NewStore(paths.ConfigDir, nil, nil)
				name, e := store.Current()
				if e != nil {
					return nil, e
				}
				names, e := store.HealthcheckTargets(name, args[3])
				return output.Candidates(names), e
			}
			if args[2] == "proxy-nodes" || args[2] == "node-groups" || args[2] == "chain-targets" {
				paths, e := nagiruntime.Resolve()
				if e != nil {
					return nil, e
				}
				store := profile.NewStore(paths.ConfigDir, nil, nil)
				name, e := store.Current()
				if e != nil {
					return nil, e
				}
				names := output.Candidates{}
				if args[2] == "chain-targets" && len(args) >= 4 && args[3] == "provider-node" {
					if len(args) == 4 {
						providers, e := store.ProxyNodeProviderCandidates(name)
						return output.Candidates(providers), e
					}
					candidateClient, e := controlClientForCurrentBackend(paths)
					if e != nil {
						return nil, e
					}
					configureProviderChainNodes(ctx, store, paths, candidateClient)
					inventory, _, e := store.ProviderChainInventory(name)
					if e != nil {
						return nil, e
					}
					for _, node := range inventory {
						if node.Provider == args[4] {
							names = append(names, node.Name)
						}
					}
					return names, nil
				}

				if args[2] == "node-groups" {
					groups, e := store.ProxyNodeGroupCandidates(name)
					return output.Candidates(groups), e
				}
				if args[2] == "proxy-nodes" {
					nodes, e := store.ProxyNodes(name)
					if e != nil {
						return nil, e
					}
					for _, node := range nodes {
						names = append(names, node.Name)
					}
				} else {
					var targets []profile.ProxyChainTarget
					if len(args) == 4 && args[3] == "subscription" {
						targets, e = chainSubscriptions(subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil), store, name)
					} else {
						targets, e = store.ProxyChains(name)
					}
					if e != nil {
						return nil, e
					}
					for _, target := range targets {
						if len(args) == 3 || args[3] == target.Kind {
							names = append(names, target.Name)
						}
					}
				}
				return names, nil
			}

			if len(args) == 4 && args[2] == "profile-groups" {
				paths, e := nagiruntime.Resolve()
				if e != nil {
					return nil, e
				}
				groups, e := profile.NewStore(paths.ConfigDir, nil, nil).DNSProxyGroups(args[3])
				return output.Candidates(groups), e
			}
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
			client, e := controlClientForCurrentBackend(paths)
			if e != nil {
				return nil, e
			}
			service := orderedProxyService(client, paths.ConfigDir)
			names := []string{}
			if len(args) == 3 && args[2] == "providers" {
				providers, err := service.Providers(ctx)
				if err != nil {
					return nil, err
				}
				for _, p := range providers {
					names = append(names, p.Name)
				}
				return proxy.Candidates(names), nil
			}
			if len(args) == 4 && args[2] == "provider-nodes" {
				p, e := service.Provider(ctx, args[3])
				return proxy.Candidates(p.Nodes), e
			}

			groups, err := service.Groups(ctx)
			if err != nil {
				return nil, err
			}
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
			return nil, usage("completion candidates groups|nodes [GROUP]|providers")
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
	if command == "privileged-helper" {
		switch args[1] {
		case "status":
			status, err := privilegedservice.Status()
			if err != nil {
				return nil, fail("privileged_helper_error", err)
			}
			paths, err := nagiruntime.Resolve()
			if err != nil {
				return nil, err
			}
			mode, err := readBackend(paths)
			if err != nil {
				return nil, err
			}
			return map[string]any{"service": status, "selected_backend": mode, "available": status.Installed && status.Active}, nil
		case "install":
			executable, err := os.Executable()
			if err != nil {
				return nil, err
			}
			status, err := privilegedservice.Install(executable)
			if err != nil {
				return nil, fail("privileged_helper_error", err)
			}
			return status, nil
		case "uninstall":
			status, err := privilegedservice.Uninstall()
			if err != nil {
				return nil, fail("privileged_helper_error", err)
			}
			return status, nil
		case "enable", "disable":
			paths, err := nagiruntime.Resolve()
			if err != nil {
				return nil, err
			}
			mode, err := readBackend(paths)
			if err != nil {
				return nil, err
			}
			if args[1] == "enable" {
				status, err := privilegedservice.Status()
				if err != nil {
					return nil, fail("privileged_helper_error", err)
				}
				if !status.Installed || !status.Active {
					return nil, fail("privileged_helper_error", messageError("cli.error.command.install_and_start_the_privileged_helper_before_enabling_it", "install and start the privileged helper before enabling it"))
				}
				if _, err := (helperBackend{paths: paths}).Status(ctx); err != nil {
					return nil, fail("privileged_helper_error", messageError("cli.error.command.helper_service_is_active_but_its_authenticated_socket_is_unavailable", "helper service is active but its authenticated socket is unavailable: %w", err))
				}
				userEngine, err := engine.New(engine.Options{Binary: binary, ConfigPath: filepath.Join(paths.ConfigDir, "profiles", "default.yaml"), Paths: paths})
				if err != nil {
					return nil, err
				}
				userStatus, err := userEngine.Status(ctx)
				if err != nil {
					return nil, err
				}
				if userStatus.Running {
					return nil, fail("privileged_helper_error", messageError("cli.error.command.stop_the_user_mihomo_process_before_enabling_the_privileged_backend", "stop the user mihomo process before enabling the privileged backend"))
				}
				if err := writeBackend(paths, privilegedBackend); err != nil {
					return nil, err
				}
				return map[string]any{"selected_backend": privilegedBackend, "changed": mode != privilegedBackend}, nil
			}
			if mode == privilegedBackend {
				b := helperBackend{profile: "default", paths: paths}
				st, err := b.Status(ctx)
				if err != nil {
					return nil, fail("privileged_helper_error", err)
				}
				if st.Running {
					return nil, fail("privileged_helper_error", messageError("cli.error.command.stop_the_privileged_mihomo_process_before_disabling_the_backend", "stop the privileged mihomo process before disabling the backend"))
				}
			}
			if err := writeBackend(paths, userBackend); err != nil {
				return nil, err
			}
			return map[string]any{"selected_backend": userBackend, "changed": mode != userBackend}, nil
		}
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, err
	}
	backendMode, err := readBackend(paths)
	if err != nil {
		return nil, err
	}
	if command == "doctor" {
		if backendMode == privilegedBackend {
			b := helperBackend{profile: "default", paths: paths}
			st, err := b.Status(ctx)
			if err != nil {
				return nil, fail("privileged_helper_error", err)
			}
			return diagnostic.RunWithAccess(ctx, paths, binary, b.controlClient(), &st), nil
		}
		return diagnostic.Run(ctx, paths, binary), nil
	}
	if command == "diagnostics" {
		var report diagnostic.FullReport
		if backendMode == privilegedBackend {
			b := helperBackend{profile: "default", paths: paths}
			st, err := b.Status(ctx)
			if err != nil {
				return nil, fail("privileged_helper_error", err)
			}
			report = diagnostic.RunFullWithAccess(ctx, paths, binary, b.controlClient(), &st)
		} else {
			report = diagnostic.RunFull(ctx, paths, binary)
		}
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
	controlTimeout := 10 * time.Second
	if len(args) >= 2 && command == "proxy" && (args[1] == "delay" || args[1] == "delays" || args[1] == "test") {
		controlTimeout = 31 * time.Second
	}
	if len(args) >= 3 && command == "proxy" && args[1] == "tailscale" && args[2] == "probe" {
		controlTimeout = 20 * time.Second
	}
	client := control.NewWithTimeout(paths.SocketPath, controlTimeout)
	trafficManager := traffic.New(paths.StateDir)
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), &http.Client{Timeout: 30 * time.Second})
	subs.Binary = binary
	subs.CommitUnderConfigLock = func(fn func() error) error {
		_, err := withNamedLock(paths, "config.lock", func() (any, error) { return nil, fn() })
		return err
	}
	addingProvider := command == "proxy" && len(args) > 2 && args[1] == "provider" && args[2] == "add"
	removingProvider := command == "proxy" && len(args) > 2 && args[1] == "provider" && args[2] == "remove"
	healthcheck := command == "proxy" && len(args) > 1 && args[1] == "healthcheck"
	healthcheckURL := healthcheck && len(args) > 2 && args[2] == "url"
	subscriptionHealthcheck := healthcheck && (len(args) > 3 && args[3] == "subscription" || healthcheckURL && len(args) > 4 && args[4] == "subscription")
	providerInterval := command == "proxy" && len(args) > 2 && args[1] == "provider" && args[2] == "interval"
	needsProfile := groupInvocation(args) || healthcheck && !subscriptionHealthcheck || nodeInvocation(args) || chainInvocation(args) || addingProvider || removingProvider || providerInterval || command == "start" || command == "restart" || command == "config" || command == "profile" || command == "dns" || (command == "mode" && len(args) > 1 && (args[1] == "save" || args[1] == "saved")) || (command == "subscription" && len(args) > 1 && (args[1] == "apply" || args[1] == "preview"))
	readsProfile := needsProfile || command == "status"
	var profileName string
	var profileErr error
	if readsProfile {
		profileName, profileErr = profile.NewStore(paths.ConfigDir, nil, nil).Current()
		if profileErr != nil && command != "status" && command != "profile" && !(command == "subscription" && len(args) > 1 && (args[1] == "apply" || args[1] == "preview")) {
			return nil, messageError("cli.error.command.cannot_read_selected_profile_run_nagi_profile_list_then_nagi_profile", "cannot read selected profile; run `nagi profile list`, then `nagi profile use NAME` to select a valid profile: %w", profileErr)
		}
		if profileName == "" {
			profileName = "default"
		}
	}
	if profileName == "" {
		profileName = "default"
	}
	configPath := filepath.Join(paths.ConfigDir, "profiles", profileName+".yaml")
	if backendMode == privilegedBackend {
		client = (helperBackend{profile: profileName, binary: binary, configPath: configPath, paths: paths}).controlClient()
	}
	proxies := proxy.NewService(client)
	if command == "proxy" {
		proxies = orderedProxyService(client, paths.ConfigDir)
	}
	var manager *engine.Manager
	getManager := func() (*engine.Manager, error) {
		if manager != nil {
			return manager, nil
		}
		if command == "start" || command == "restart" || (command == "config" && len(args) > 1 && args[1] != "show") {
			store := newProfileStoreWithContext(ctx, paths, binary, client, nil)
			configPath, err = materializeProfile(store, paths.ConfigDir, profileName)
			if err != nil {
				return nil, err
			}
		}
		manager, err = engine.New(engine.Options{Binary: binary, ConfigPath: configPath, Paths: paths})
		return manager, err
	}
	getLifecycle := func() (lifecycleBackend, error) {
		if backendMode == privilegedBackend {
			if command == "start" || command == "restart" || command == "config" {
				if _, err := getManager(); err != nil {
					return nil, err
				}
			}
			return helperBackend{profile: profileName, binary: binary, configPath: configPath, paths: paths}, nil
		}
		return getManager()
	}
	var profiles *profile.Store
	getProfiles := func() (*profile.Store, error) {
		if profiles != nil {
			return profiles, nil
		}
		m, err := getLifecycle()
		if err != nil {
			return nil, err
		}
		profiles = newProfileStoreWithContext(ctx, paths, binary, client, m)
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
		m, err := getLifecycle()
		if err != nil {
			return nil, err
		}
		result, err := startWithTUNDNS(ctx, paths, m, client, configPath, profileName)
		if err == nil {
			if e := rewriteReconcile(ctx, paths); e != nil {
				return nil, e
			}
		}
		if err == nil {
			_ = recordRunningImage(paths.ConfigDir, client, configPath)
			if _, restoreErr := (proxy.SelectionStore{Path: filepath.Join(paths.ConfigDir, "proxy-selections.json")}).Restore(ctx, profileName, proxies); restoreErr != nil {
				return nil, fail("proxy_selection_error", messageError("cli.error.command.mihomo_started_but_saved_proxy_selections_were_not_fully_restored_w", "mihomo started, but saved proxy selections were not fully restored: %w", restoreErr))
			}
		}
		return result, err
	case "stop":
		if e := rewriteLifecycleStop(ctx, paths); e != nil {
			return nil, e
		}
		m, err := getLifecycle()
		if err != nil {
			return nil, err
		}
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if backendMode != privilegedBackend {
			if e := restoreTUNDNS(ctx, paths); e != nil {
				return nil, fail("tun_error", messageError("cli.error.command.system_dns_restoration_failed_engine_remains_running_retry_nagi_stop", "system DNS restoration failed; engine remains running; retry `nagi stop`: %w", e))
			}
		}
		if runtime.GOOS == "darwin" && trafficManager.Managed() {
			if _, e := trafficManager.Disable(ctx); e != nil {
				return nil, fail("system_proxy_error", e)
			}
		}
		return m.Stop(ctx)
	case "restart":
		if e := rewriteLifecycleStop(ctx, paths); e != nil {
			return nil, e
		}
		if err := arity(args, 1); err != nil {
			return nil, err
		}
		if err := ensureDefaultProfile(filepath.Join(paths.ConfigDir, "profiles", profileName+".yaml"), profileName); err != nil {
			return nil, err
		}
		m, err := getLifecycle()
		if err != nil {
			return nil, err
		}
		if backendMode != privilegedBackend {
			if e := restoreTUNDNS(ctx, paths); e != nil {
				return nil, fail("tun_error", messageError("cli.error.command.system_dns_restoration_failed_engine_remains_running_retry_nagi_stop_12", "system DNS restoration failed; engine remains running; retry `nagi stop`: %w", e))
			}
		}
		if runtime.GOOS == "darwin" && trafficManager.Managed() {
			if _, e := trafficManager.Disable(ctx); e != nil {
				return nil, fail("system_proxy_error", e)
			}
		}
		if _, err := m.Stop(ctx); err != nil && !errors.Is(err, engine.ErrNotRunning) {
			return nil, err
		}
		result, err := startWithTUNDNS(ctx, paths, m, client, configPath, profileName)
		if err == nil {
			if e := rewriteReconcile(ctx, paths); e != nil {
				return nil, e
			}
		}
		if err == nil {
			_ = recordRunningImage(paths.ConfigDir, client, configPath)
			if _, restoreErr := (proxy.SelectionStore{Path: filepath.Join(paths.ConfigDir, "proxy-selections.json")}).Restore(ctx, profileName, proxies); restoreErr != nil {
				return nil, fail("proxy_selection_error", messageError("cli.error.command.mihomo_restarted_but_saved_proxy_selections_were_not_fully_restored_", "mihomo restarted, but saved proxy selections were not fully restored: %w", restoreErr))
			}
		}
		return result, err
	case "status":
		m, err := getLifecycle()
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
			if e := restoreTUNDNS(ctx, paths); e != nil {
				return nil, fail("tun_error", e)
			}
		}
		if runtime.GOOS == "darwin" && !status.Running {
			if e := trafficManager.RestoreIfStopped(ctx, 0); e != nil {
				return nil, fail("system_proxy_error", e)
			}
		}
		data := map[string]any{"running": status.Running, "socket_path": status.SocketPath, "log_path": status.LogPath}
		messages := map[string]i18n.Descriptor{}
		if recoveryError, e := os.ReadFile(dnsRecoveryErrorPath(paths)); e == nil {
			data["system_dns_recovery_error"] = output.RedactError(string(recoveryError))
		}
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
			rememberError(messages, "/profile_error", profileErr)
		}
		var dnsStatus func(context.Context) (traffic.DNSState, error)
		if tunDNSOS == "darwin" {
			dnsStatus = func(ctx context.Context) (traffic.DNSState, error) { return statusTUNDNS(ctx, paths) }
		}
		for pointer, descriptor := range appendStatusReads(ctx, data, status.Running, client, dnsStatus) {
			messages[pointer] = descriptor
		}
		return resultWithMessages(data, messages), nil
	case "logs":
		if backendMode == privilegedBackend {
			b := helperBackend{profile: profileName, paths: paths}
			if len(args) == 2 && args[1] == "follow" {
				st, err := b.Status(ctx)
				if err != nil {
					return nil, err
				}
				if !st.Running {
					return nil, fail("not_running", messageError("cli.error.command.mihomo_is_not_running_start_it_before_following_logs", "mihomo is not running; start it before following logs"))
				}
				return helperFollowResult{backend: b}, nil
			}
			count := 100
			if len(args) == 2 {
				count, _ = strconv.Atoi(args[1])
			}
			lines, err := b.Logs(ctx, count)
			return map[string]any{"lines": lines}, err
		}
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
				return nil, fail("not_running", messageError("cli.error.command.mihomo_is_not_running_start_it_before_following_logs", "mihomo is not running; start it before following logs"))
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
				return nil, fail("invalid_config", messageError("cli.error.command.configuration_validation_failed_run_nagi_config_show_to_inspect_the_", "configuration validation failed; run `nagi config show` to inspect the selected profile and check the mihomo executable: %w", err))
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
			lifecycle, err := getLifecycle()
			if err != nil {
				return nil, err
			}
			status, err := lifecycle.Status(ctx)
			if err != nil {
				return nil, err
			}
			if !status.Running {
				return nil, fail("not_running", messageError("cli.error.command.mihomo_is_not_running_start_it_before_reloading_configuration", "mihomo is not running; start it before reloading configuration"))
			}
			if err := activateConfig(ctx, client, configPath); err != nil {
				return nil, fail("mihomo_api_error", messageError("cli.error.command.configuration_validated_but_reload_failed_runtime_state_may_still_us", "configuration validated but reload failed; runtime state may still use the previous configuration: %w", err))
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
		if len(args) >= 4 && args[1] == "override" && args[2] == "apply-default" {
			if err := validateDefaultDNSLiveTarget(ctx, args, store, profileName, client); err != nil {
				return nil, fail("profile_error", err)
			}
		}
		return profileCommand(ctx, args, store, subs)
	case "subscription":
		if len(args) > 1 && (args[1] == "apply" || args[1] == "preview") {
			store, err := getProfiles()
			if err != nil {
				return nil, err
			}
			return subscriptionCommand(ctx, args, subs, store, proxies)
		}
		if len(args) > 1 && args[1] == "remove" {
			entry, e := subs.Find(args[2])
			if e != nil {
				return nil, fail("subscription_error", e)
			}
			if entry.Profile != "" && (entry.HealthcheckIntervalSeconds != nil || entry.HealthcheckURL != nil) {
				store, e := getProfiles()
				if e != nil {
					return nil, fail("subscription_error", e)
				}
				return subscriptionCommand(ctx, args, subs, store, nil)
			}
		}
		return subscriptionCommand(ctx, args, subs, nil, nil)
	case "dns":
		store, err := getProfiles()
		if err != nil {
			return nil, err
		}
		if len(args) == 3 && args[1] == "tun" {
			preference, e := savedTUNPreference(paths)
			if e != nil {
				return nil, fail("dns_error", e)
			}
			if preference != nil && *preference != (args[2] == "on") {
				action := "disable"
				if args[2] == "on" {
					action = "enable"
				}
				return nil, fail("dns_error", messageError("cli.error.command.dns_tun_setting_conflicts_with_saved_tun_preference_with_mihomo_runn", "DNS TUN setting conflicts with saved TUN preference; with mihomo running, run `nagi tun %s` first, then retry", action))
			}
		}
		return dnsCommand(ctx, args, store, profileName, client)
	case "proxy":
		if healthcheckURL {
			var raw *string
			if args[3] == "set" {
				raw = &args[6]
			}
			if subscriptionHealthcheck {
				entry, e := subs.Find(args[5])
				if e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
				var store *profile.Store
				if entry.Profile != "" {
					store, e = getProfiles()
					if e != nil {
						return nil, fail("proxy_healthcheck_error", e)
					}
				}
				r, e := mutateSubscriptionHealthcheckURL(ctx, subs, store, args[5], raw)
				if e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
				return r, nil
			}
			store, e := getProfiles()
			if e != nil {
				return nil, fail("proxy_healthcheck_error", e)
			}
			if e = store.SetHealthcheckURL(ctx, profileName, args[4], args[5], raw); e != nil {
				return nil, fail("proxy_healthcheck_error", e)
			}
			r, e := store.Healthcheck(profileName, args[4], args[5])
			if e != nil {
				return nil, fail("proxy_healthcheck_error", e)
			}
			return r, nil
		}
		if providerInterval {
			store := profile.NewStore(paths.ConfigDir, nil, nil)
			if args[3] != "show" {
				var e error
				store, e = getProfiles()
				if e != nil {
					return nil, fail("proxy_provider_error", e)
				}
				var seconds *int
				if args[3] == "set" {
					n, _ := providerSeconds(args[5])
					seconds = &n
				}
				if e = store.SetProviderInterval(ctx, profileName, args[4], seconds); e != nil {
					return nil, fail("proxy_provider_error", e)
				}
			}
			r, e := store.ProviderInterval(profileName, args[4])
			if e != nil {
				return nil, fail("proxy_provider_error", e)
			}
			return r, nil
		}

		if healthcheck {
			if subscriptionHealthcheck {
				entry, e := subs.Find(args[4])
				if e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
				if args[2] == "show" {
					r := subscriptionHealthcheckResult(entry)
					if len(args) == 6 {
						r, e = healthcheckEditingResult(r, profile.NewStore(paths.ConfigDir, nil, nil))
						if e != nil {
							return nil, fail("proxy_healthcheck_error", e)
						}
					}
					return r, nil
				}
				var seconds *int
				if args[2] == "set" {
					v, _ := strconv.Atoi(args[5])
					seconds = &v
				}
				var store *profile.Store
				if entry.Profile != "" {
					store, e = getProfiles()
					if e != nil {
						return nil, fail("proxy_healthcheck_error", e)
					}
				}
				r, e := mutateSubscriptionHealthcheck(ctx, subs, store, args[4], seconds)
				if e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
				return r, nil
			}
			store := profile.NewStore(paths.ConfigDir, nil, nil)
			if args[2] != "show" {
				var e error
				store, e = getProfiles()
				if e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
				var seconds *int
				if args[2] == "set" {
					v, _ := strconv.Atoi(args[5])
					seconds = &v
				}
				if e = store.SetHealthcheck(ctx, profileName, args[3], args[4], seconds); e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
			}
			r, e := store.Healthcheck(profileName, args[3], args[4])
			if e != nil {
				return nil, fail("proxy_healthcheck_error", e)
			}
			if args[2] == "show" && len(args) == 6 {
				r, e = healthcheckEditingResult(r, store)
				if e != nil {
					return nil, fail("proxy_healthcheck_error", e)
				}
			}
			return r, nil
		}
		if groupInvocation(args) || nodeInvocation(args) || chainInvocation(args) {
			code := "proxy_node_error"
			if groupInvocation(args) {
				code = "proxy_group_error"
			}
			if chainInvocation(args) {
				code = "proxy_chain_error"
			}
			var store *profile.Store
			reloaded := false
			if groupInvocation(args) && (groupRead(args) || args[2] == "order") || nodeInvocation(args) && nodeRead(args) || chainInvocation(args) && chainRead(args) {
				store = profile.NewStore(paths.ConfigDir, nil, nil)
				if chainInvocation(args) {
					configureProviderChainNodes(ctx, store, paths, client)
				}
			} else {
				var e error
				store, e = getProfiles()
				if e != nil {
					return nil, fail(code, e)
				}
				backend, e := getLifecycle()
				if e != nil {
					return nil, fail(code, e)
				}
				status, e := backend.Status(ctx)
				if e != nil {
					return nil, fail(code, e)
				}
				reloaded = status.Running
			}
			var result any
			var e error
			if groupInvocation(args) {
				result, e = groupCommand(ctx, args, store, profileName, reloaded)
			} else if chainInvocation(args) {
				result, e = chainCommand(ctx, args, store, subs, profileName, reloaded)
			} else {
				result, e = nodeCommand(ctx, args, store, profileName, reloaded)
			}
			if e != nil {
				return nil, fail(code, e)
			}
			return result, nil
		}

		if removingProvider {
			store, err := getProfiles()
			if err != nil {
				return nil, fail("proxy_provider_error", err)
			}
			if err := store.RemoveProvider(ctx, profileName, args[3]); err != nil {
				return nil, fail("proxy_provider_error", messageError("cli.error.command.provider_removal_failed_inspect_the_selected_profile_and_override_w", "provider removal failed; inspect the selected profile and override: %w", err))
			}
			return map[string]any{"provider": args[3], "profile": profileName, "removed": true}, nil
		}
		if addingProvider {
			store, err := getProfiles()
			if err != nil {
				return nil, fail("proxy_provider_error", err)
			}
			interval, testURLs, _ := providerAddOptions(args)
			created, err := store.AddProvider(ctx, profileName, args[3], args[4], args[5], interval, testURLs...)
			if err != nil {
				return nil, fail("proxy_provider_error", messageError("cli.error.command.provider_addition_failed_inspect_nagi_profile_list_and_the_selected_", "provider addition failed; inspect `nagi profile list` and the selected profile override: %w", err))
			}
			return map[string]any{"provider": args[3], "group": args[5], "profile": profileName, "interval_seconds": interval, "added": true, "group_created": created}, nil
		}
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
		if runtime.GOOS == "linux" {
			return nil, fail("system_proxy_error", messageError("cli.error.command.system_proxy_is_unsupported_on_linux_configure_applications_to_use_t", "system proxy is unsupported on Linux; configure applications to use the mihomo listener"))
		}
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
			return nil, fail("system_proxy_error", messageError("cli.error.command.start_mihomo_first_w", "start mihomo first: %w", e))
		}
		port := intField(cfg, "mixed-port")
		if port == 0 {
			port = intField(cfg, "port")
		}
		if port == 0 {
			return nil, fail("system_proxy_error", messageError("cli.error.command.no_http_or_mixed_listener_is_configured_inspect_nagi_ports_status", "no HTTP or mixed listener is configured; inspect `nagi ports status`"))
		}
		if cfg["allow-lan"] == true {
			return nil, fail("system_proxy_error", messageError("cli.error.command.disable_lan_access_before_enabling_the_system_proxy_or_configure_a_l", "disable LAN access before enabling the system proxy, or configure a loopback-only listener"))
		}
		status, e := getLifecycle()
		if e != nil {
			return nil, e
		}
		engineStatus, e := status.Status(ctx)
		if e != nil || !engineStatus.Running {
			return nil, fail("system_proxy_error", messageError("cli.error.command.start_mihomo_before_enabling_the_system_proxy", "start mihomo before enabling the system proxy"))
		}
		state, e := trafficManager.Enable(ctx, engineStatus.PID, port)
		if e != nil {
			return nil, fail("system_proxy_error", e)
		}
		if e := startExitWatcher(paths, engineStatus.PID); e != nil {
			_, restoreErr := trafficManager.Disable(ctx)
			return nil, fail("system_proxy_error", errors.Join(messageError("cli.error.command.cannot_start_exit_recovery_watcher_w", "cannot start exit recovery watcher: %w", e), restoreErr))
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
			result := map[string]any{"enabled": enabled, "settings": tun}
			messages := map[string]i18n.Descriptor{}
			if tunDNSOS == "darwin" {
				state, dnsErr := statusTUNDNS(ctx, paths)
				result["system_dns"] = state
				if dnsErr != nil {
					result["system_dns_error"] = output.RedactError(dnsErr.Error())
					rememberError(messages, "/system_dns_error", dnsErr)
				}
			}
			for key, value := range tunAdapterReport(tun, net.Interfaces) {
				result[key] = value
			}
			return resultWithMessages(result, messages), nil
		}
		requested := args[1] == "enable"
		if !requested {
			if e := rewriteLifecycleStop(ctx, paths); e != nil {
				return nil, e
			}
		}
		payload := map[string]any{"enable": requested}
		if requested {
			payload["auto-route"] = true
			payload["auto-detect-interface"] = true
			payload["strict-route"] = true
			payload["dns-hijack"] = tunDNSHijack(tun["dns-hijack"])
		}
		cfg, e = changeRuntimeTUN(ctx, paths, client, tun, payload)
		if e != nil {
			return nil, fail("tun_error", e)
		}
		if e := writeTUNIntent(paths, requested); e != nil {
			return nil, fail("tun_error", messageError("cli.error.command.tun_state_changed_but_could_not_save_startup_preference_w", "TUN state changed but could not save startup preference: %w", e))
		}
		if requested {
			if e := rewriteReconcile(ctx, paths); e != nil {
				return nil, e
			}
		}
		tun, _ = cfg["tun"].(map[string]any)
		enabled, _ = tun["enable"].(bool)
		result := map[string]any{"enabled": enabled, "settings": tun}
		messages := map[string]i18n.Descriptor{}
		if tunDNSOS == "darwin" {
			state, dnsErr := statusTUNDNS(ctx, paths)
			result["system_dns"] = state
			if dnsErr != nil {
				result["system_dns_error"] = output.RedactError(dnsErr.Error())
				rememberError(messages, "/system_dns_error", dnsErr)
			}
		}
		for key, value := range tunAdapterReport(tun, net.Interfaces) {
			result[key] = value
		}
		return resultWithMessages(result, messages), nil
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
				return nil, fail("invalid_config", messageError("cli.error.command.persistent_mode_was_not_saved_w", "persistent mode was not saved: %w", err))
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
		if backendMode == privilegedBackend {
			return i18n.Result{Value: map[string]any{"recovered": false, "reason": "helper owns runtime state"}, Messages: map[string]i18n.Descriptor{"/reason": {ID: "integration.recover.helper_owned"}}}, nil
		}
		status, e := m.Recover(ctx)
		if e != nil {
			if errors.Is(e, engine.ErrNotRunning) {
				return i18n.Result{Value: map[string]any{"recovered": false, "reason": "no stale runtime state"}, Messages: map[string]i18n.Descriptor{"/reason": {ID: "integration.recover.no_stale_state"}}}, nil
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
			m, e := getLifecycle()
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
			m, e := getLifecycle()
			if e != nil {
				return nil, e
			}
			st, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			if !st.Running {
				if e := restoreTUNDNS(ctx, paths); e != nil {
					return nil, fail("tun_error", e)
				}
			}
			if st.StalePID != 0 && backendMode == userBackend {
				rec, e := getManager()
				if e != nil {
					return nil, e
				}
				recovered, e := rec.Recover(ctx)
				if e != nil {
					return nil, e
				}
				return map[string]any{"checked": true, "recovered": true, "stale_pid": recovered.StalePID}, nil
			}
			reachable := false
			if st.Running {
				var v any
				reachable = client.Get(ctx, "/version", &v) == nil
			}
			result := map[string]any{"checked": true, "running": st.Running, "control_api": reachable}
			messages := map[string]i18n.Descriptor{}
			if runtime.GOOS == "darwin" && st.Running && reachable {
				dnsState, dnsErr := syncTUNDNS(ctx, paths, client, st.PID)
				result["system_dns"] = dnsState
				if dnsErr != nil {
					result["system_dns_error"] = output.RedactError(dnsErr.Error())
					rememberError(messages, "/system_dns_error", dnsErr)
				}
				proxyCheck, checkErr := trafficManager.Recheck(ctx, st.PID)
				result["system_proxy"] = proxyCheck
				if checkErr != nil {
					result["system_proxy_error"] = output.RedactError(checkErr.Error())
					rememberError(messages, "/system_proxy_error", checkErr)
				}
			}
			return resultWithMessages(result, messages), nil
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
func usage(value any) error {
	if err, ok := value.(error); ok {
		return fail("usage", err)
	}
	return fail("usage", errors.New(fmt.Sprint(value)))
}

func profileCommand(ctx context.Context, args []string, store *profile.Store, subscriptions ...*subscription.Store) (any, error) {
	var subs *subscription.Store
	if len(subscriptions) > 0 {
		subs = subscriptions[0]
	}
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
			return nil, i18n.WithErrorContext("cli.profile.import.failed", "Read profile import file: %w", err, err)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
		if err != nil {
			return nil, i18n.WithErrorContext("cli.profile.import.failed", "Read profile import file: %w", err, err)
		}
		if len(data) > 8<<20 {
			return nil, fail("profile_error", messageError("cli.error.command.profile_exceeds_8_mib", "profile exceeds 8 MiB"))
		}
		if err := store.ClearSubscriptionChain(args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		if subs != nil {
			if err := subs.DetachProfile(args[2]); err != nil {
				return nil, fail("profile_error", err)
			}
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
			return nil, fail("profile_error", i18n.WithErrorContext("cli.profile.export.failed", "Write profile export file: %w", err, err))
		}
		file, err := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, fail("profile_error", i18n.WithErrorContext("cli.profile.export.failed", "Write profile export file: %w", err, err))
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(args[3])
			return nil, fail("profile_error", i18n.WithErrorContext("cli.profile.export.failed", "Write profile export file: %w", err, err))
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
		messages := map[string]i18n.Descriptor{}
		changes, warnings, parseErr := structuralDiff(before, after, messages)
		if parseErr != nil {
			warning := messageError("cli.diff.warning.invalid_yaml", "%s; line diff remains available", parseErr.Error())
			warnings = []string{warning.Error()}
			messages = map[string]i18n.Descriptor{"/warnings/0": warning.Descriptor()}
			changes = []StructuralChange{}
		}
		return resultWithMessages(map[string]any{"name": args[2], "other": other, "diff": profileDiff(before, after), "changed": string(before) != string(after), "changes": changes, "warnings": warnings}, messages), nil
	case "override":
		if len(args) < 3 {
			return nil, usage("profile override set NAME FILE|show NAME|clear NAME|apply-default NAME|default-status NAME|restore-default NAME")
		}
		switch args[2] {
		case "apply-default":
			if len(args) != 4 && (len(args) != 6 || args[4] != "--dns-proxy" || strings.TrimSpace(args[5]) == "") {
				return nil, usage("profile override apply-default NAME [--dns-proxy GROUP]")
			}
			name := args[3]
			var dnsProxy []string
			if len(args) == 6 {
				dnsProxy = []string{args[5]}
			}
			if err := store.ApplyDefaultOverride(ctx, name, dnsProxy...); err != nil {
				return nil, fail("profile_error", err)
			}
			return map[string]any{"name": name, "default_override_applied": true}, nil
		case "default-status":
			if len(args) != 4 {
				return nil, usage("profile override default-status NAME")
			}
			active, err := store.DefaultOverrideStatus(args[3])
			if err != nil {
				return nil, fail("profile_error", err)
			}
			groups, err := store.DNSProxyGroups(args[3])
			if err != nil {
				return nil, fail("profile_error", err)
			}
			group, err := store.DefaultDNSProxyGroup(args[3])
			if err != nil {
				return nil, fail("profile_error", err)
			}
			required, err := store.DefaultDNSProxyRequired(args[3])
			if err != nil {
				return nil, fail("profile_error", err)
			}
			return map[string]any{"name": args[3], "default_override_applied": active, "dns_proxy_groups": groups, "dns_proxy_group": group, "dns_proxy_required": required}, nil
		case "restore-default":
			if len(args) != 4 {
				return nil, usage("profile override restore-default NAME")
			}
			if err := store.RestoreDefaultOverride(ctx, args[3]); err != nil {
				return nil, fail("profile_error", err)
			}
			return map[string]any{"name": args[3], "default_override_applied": false, "default_override_restored": true}, nil
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
		return nil, usage("profile override set NAME FILE|show NAME|clear NAME|apply-default NAME|default-status NAME|restore-default NAME")
	case "remove":
		if err := arity(args, 3); err != nil {
			return nil, usage("profile remove NAME (run `nagi profile remove --help` for details)")
		}
		if err := store.Remove(args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		if err := (proxy.SelectionStore{Path: filepath.Join(store.Dir, "proxy-selections.json")}).Clear(args[2]); err != nil {
			return nil, fail("profile_error", err)
		}
		if subs != nil {
			if err := subs.DetachProfile(args[2]); err != nil {
				return nil, fail("profile_error", err)
			}
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
		messages := map[string]i18n.Descriptor{}
		if currentErr != nil {
			data["current_error"] = currentErr.Error()
			rememberError(messages, "/current_error", currentErr)
		} else {
			current, err := store.Current()
			if err != nil {
				data["current_error"] = err.Error()
				rememberError(messages, "/current_error", err)
			} else {
				data["current"] = current
			}
		}
		return resultWithMessages(data, messages), nil
	}
	return profileCommand(ctx, args, store)
}

func subscriptionCommand(ctx context.Context, args []string, store *subscription.Store, profiles *profile.Store, proxies *proxy.Service) (any, error) {
	if len(args) < 2 {
		return nil, usage("subscription list|add <name> <url>|key <name> <file|clear>|schedule <name> <seconds|off>|update <name>|preview <name>|apply <name>|remove <name>")
	}
	switch args[1] {
	case "key":
		if err := arity(args, 4); err != nil {
			return nil, err
		}
		if args[3] == "clear" {
			if err := store.RequireEntry(args[2]); err != nil {
				return nil, fail("subscription_error", err)
			}
			if err := store.ClearKey(args[2]); err != nil {
				return nil, fail("subscription_error", err)
			}
			return map[string]any{"name": args[2], "key_configured": false}, nil
		}
		if err := store.SetKey(args[2], args[3]); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2], "key_configured": true}, nil
	case "list":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		list, err := store.List()
		return map[string]any{"subscriptions": list, "scheduler_running": schedulerRunningForCurrentUser()}, err
	case "schedule":
		if err := arity(args, 4); err != nil {
			return nil, usage("subscription schedule NAME SECONDS|off")
		}
		seconds := int64(0)
		if args[3] != "off" {
			var parseErr error
			seconds, parseErr = strconv.ParseInt(args[3], 10, 64)
			if parseErr != nil || seconds < 1 || seconds > subscription.MaxScheduleSeconds {
				return nil, usage("schedule interval must be 1..31536000 seconds or off")
			}
		}
		if err := store.Schedule(args[2], seconds); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2], "schedule_seconds": seconds, "scheduler_running": schedulerRunningForCurrentUser()}, nil
	case "add":
		options, err := subscriptionAddOptions(args)
		if err != nil {
			return nil, usage(err)
		}
		if err := store.AddWithOptions(args[2], args[3], options); err != nil {
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
		entry, err := store.Find(args[2])
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		if entry.Profile != "" {
			policy, err := profile.NewStore(store.ConfigDir, nil, nil).SubscriptionChain(entry.Profile)
			if err != nil {
				return nil, fail("subscription_error", err)
			}
			if policy != nil && policy.Subscription == entry.Name {
				return nil, fail("subscription_error", messageError("cli.error.proxy_chain.subscription_remove_blocked", "subscription has a persistent chain; run `nagi proxy chain clear subscription NAME` before removing it"))
			}
		}
		if entry.Profile != "" && (entry.HealthcheckIntervalSeconds != nil || entry.HealthcheckURL != nil) && profiles != nil {
			// Validate and activate the inherited configuration before deleting
			// cache/key files, which live outside the config snapshot.
			if err := store.SetHealthcheckInterval(entry.Name, nil); err != nil {
				return nil, fail("subscription_error", err)
			}
			if err := store.SetHealthcheckURL(entry.Name, nil); err != nil {
				return nil, fail("subscription_error", err)
			}
			if err := profiles.ActivateHealthcheckPolicy(ctx, entry.Profile); err != nil {
				return nil, fail("subscription_error", err)
			}
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
				if args[1] == "preview" {
					return nil, fail("subscription_error", messageError("cli.error.command.existing_profile_cannot_be_parsed_for_preview_inspect_or_remove_it", "existing profile cannot be parsed for preview; inspect or remove it"))
				}
				existing = nil
			}
		} else if !errors.Is(showErr, os.ErrNotExist) {
			return nil, fail("subscription_error", showErr)
		}
		incoming, err = subscription.NormalizeChecked(incoming)
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		preview := subscription.Compare(args[2], format, incoming, existing)
		if args[1] == "preview" {
			return preview, nil
		}
		merged, err := subscription.Encode(incoming)
		if err != nil {
			return nil, fail("subscription_error", err)
		}
		// Stage association inside the outer transaction so initial application
		// uses saved interval intent; failed activation restores prior metadata.
		if err := store.StageProfileAssociation(args[2], args[2]); err != nil {
			return nil, fail("subscription_error", err)
		}
		if err := validateRuleTargets(profiles.Dir, args[2], merged); err != nil {
			return nil, fail("subscription_error", err)
		}
		if err := profiles.ApplySubscription(context.WithValue(ctx, skipSelectionRestoreKey{}, true), args[2], merged); err != nil {
			return nil, fail("subscription_error", err)
		}
		if err := (proxy.SelectionStore{Path: filepath.Join(profiles.Dir, "proxy-selections.json")}).Clear(args[2]); err != nil {
			return nil, fail("subscription_error", messageError("cli.error.command.subscription_applied_but_saved_proxy_choices_could_not_be_cleared_w", "subscription applied but saved proxy choices could not be cleared: %w", err))
		}
		if err := store.RecordApplySuccess(args[2], args[2], subscription.Hash(data), merged); err != nil {
			return nil, fail("subscription_error", err)
		}
		return map[string]any{"name": args[2], "profile": args[2], "applied": true, "preview": preview, "selections_restored": 0}, nil
	default:
		return nil, usage("subscription list|add <name> <url>|key <name> <file|clear>|schedule <name> <seconds|off>|update <name>|preview <name>|apply <name>|remove <name>")
	}
}

func proxyCommand(ctx context.Context, args []string, service *proxy.Service, selections proxy.SelectionStore, configDir string) (any, error) {
	if len(args) < 2 {
		return nil, usage("proxy groups|show GROUP|search QUERY|select GROUP NODE|auto GROUP|providers|provider show|healthcheck NAME|node SUBCOMMAND|tailscale SUBCOMMAND|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore")
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
			return nil, fail("proxy_selection_error", messageError("cli.error.command.cannot_read_selected_profile_to_save_choice_w", "cannot read selected profile to save choice: %w", err))
		}
		if err := service.Select(ctx, args[2], args[3]); err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		if err := selections.Save(current, args[2], args[3]); err != nil {
			return nil, fail("proxy_selection_error", messageError("cli.error.command.selected_node_in_running_engine_but_could_not_save_choice_w", "selected node in running engine, but could not save choice: %w", err))
		}
		return map[string]any{"group": args[2], "node": args[3]}, nil
	case "auto":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		current, err := profile.NewStore(configDir, nil, nil).Current()
		if err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		if err := service.Auto(ctx, args[2]); err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		if err := selections.RemoveGroup(current, args[2]); err != nil {
			return nil, fail("proxy_selection_error", messageError("cli.error.command.automatic_selection_resumed_in_running_engine_but_saved_choice_could", "automatic selection resumed in running engine, but saved choice could not be removed: %w", err))
		}
		return map[string]any{"group": args[2], "automatic": true, "saved_selection_removed": true}, nil
	case "providers":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		providers, err := service.Providers(ctx)
		if err != nil {
			return nil, fail("proxy_provider_error", err)
		}
		return map[string]any{"providers": providers}, nil
	case "provider":
		if len(args) != 4 {
			return nil, usage("proxy provider show|update|healthcheck NAME")
		}
		switch args[2] {
		case "show":
			p, err := service.Provider(ctx, args[3])
			if err != nil {
				return nil, fail("proxy_provider_error", err)
			}
			return p, nil
		case "update":
			if err := service.ProviderUpdate(ctx, args[3]); err != nil {
				return nil, fail("proxy_provider_error", err)
			}
			return map[string]any{"provider": args[3], "updated": true}, nil
		case "healthcheck":
			if err := service.ProviderHealthcheck(ctx, args[3]); err != nil {
				return nil, fail("proxy_provider_error", err)
			}
			return map[string]any{"provider": args[3], "healthcheck_started": true}, nil
		}
		return nil, usage("proxy provider show|update|healthcheck NAME")
	case "restore":
		if err := arity(args, 2); err != nil {
			return nil, err
		}
		current, err := profile.NewStore(configDir, nil, nil).Current()
		if err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		result, err := selections.Restore(ctx, current, service)
		if err != nil {
			return nil, fail("proxy_selection_error", err)
		}
		return result, nil
	case "tailscale":
		if len(args) < 4 {
			return nil, usage("proxy tailscale status NODE|probe NODE PEER_IP [TIMEOUT_MS]")
		}
		switch args[2] {
		case "status":
			if err := arity(args, 4); err != nil {
				return nil, err
			}
			result, err := service.TailscalePathStatus(ctx, args[3])
			if err != nil {
				return nil, fail("proxy_tailscale_error", err)
			}
			return result, nil
		case "probe":
			if len(args) < 5 || len(args) > 6 {
				return nil, usage("proxy tailscale probe NODE PEER_IP [TIMEOUT_MS]")
			}
			timeout := 5000
			if len(args) == 6 {
				var err error
				timeout, err = proxy.ParseTailscalePathTimeout(args[5])
				if err != nil {
					return nil, usage("proxy tailscale probe NODE PEER_IP [TIMEOUT_MS]")
				}
			}
			addr, parseErr := netip.ParseAddr(args[4])
			if parseErr != nil || addr.Zone() != "" {
				return nil, usage("proxy tailscale probe NODE PEER_IP [TIMEOUT_MS]")
			}
			if timeout < 1000 || timeout > 15000 {
				return nil, usage("proxy tailscale probe NODE PEER_IP [TIMEOUT_MS]")
			}
			result, err := service.TailscalePathProbe(ctx, args[3], addr.String(), timeout)
			if err != nil {
				return nil, fail("proxy_tailscale_error", err)
			}
			return result, nil
		default:
			return nil, usage("proxy tailscale status NODE|probe NODE PEER_IP [TIMEOUT_MS]")
		}
	case "search":
		if err := arity(args, 3); err != nil {
			return nil, err
		}
		groups, err := service.Search(ctx, args[2])
		return map[string]any{"query": args[2], "groups": groups}, err
	case "test":
		if len(args) != 5 || (args[2] != "group" && args[2] != "provider") {
			return nil, usage("proxy test group|provider OWNER NODE")
		}
		return service.TestNode(ctx, args[2], args[3], args[4])
	case "delays":
		target, timeoutMS, conditionErr := service.GroupTestConditions(args[2])
		if conditionErr != nil && len(args) < 5 {
			return nil, conditionErr
		}
		if len(args) >= 4 {
			target = args[3]
		}
		if len(args) == 5 {
			timeoutMS, _ = strconv.Atoi(args[4])
		}
		g, err := service.ShowState(ctx, args[2])
		if err != nil {
			return nil, err
		}
		automatic := g.Type == "URLTest" || g.Type == "Fallback"
		var current string
		if automatic {
			current, err = profile.NewStore(configDir, nil, nil).Current()
			if err != nil {
				return nil, fail("proxy_selection_error", messageError("cli.error.command.group_latency_test_was_not_started_because_selected_profile_could_no", "group latency test was not started because selected profile could not be read; saved choice is unchanged: %w", err))
			}
		}
		result, state, delayErr := service.DelaysKnown(ctx, g, target, timeoutMS)
		if automatic && state == proxy.DelayResetUnknown {
			// A transport failure may occur after the handler cleared the pin.
			// Only a visible transition from fixed to automatic proves the reset.
			observed, readErr := service.ShowState(ctx, args[2])
			if readErr == nil && g.Fixed != "" && observed.Fixed == "" {
				state = proxy.DelayResetConfirmed
			} else {
				return nil, messageError("cli.error.command.group_latency_test_failed_and_automatic_reset_is_unknown_saved_choic", "group latency test failed and automatic reset is unknown; saved choice was kept; inspect `nagi proxy show %q` before `nagi proxy restore`: %w", args[2], delayErr)
			}
		}
		if automatic && state == proxy.DelayResetConfirmed {
			if err := selections.RemoveGroup(current, args[2]); err != nil {
				return nil, fail("proxy_selection_error", messageError("cli.error.command.group_latency_test_resumed_automatic_selection_in_running_engine_but", "group latency test resumed automatic selection in running engine, but saved choice could not be removed: %w", err))
			}
			result.AutomaticReset = true
			result.SavedSelectionRemoved = true
		}
		if automatic && state == proxy.DelayResetConfirmed && delayErr != nil {
			return nil, messageError("cli.error.command.group_latency_test_failed_after_mihomo_resumed_automatic_selection_s", "group latency test failed after mihomo resumed automatic selection; saved choice was removed: %w", delayErr)
		}
		return result, delayErr
	case "delay":
		target := "https://cp.cloudflare.com/generate_204"
		timeoutMS := 5000
		if len(args) >= 4 {
			target = args[3]
		}
		if len(args) == 5 {
			timeoutMS, _ = strconv.Atoi(args[4])
		}
		return service.Delay(ctx, args[2], target, timeoutMS)
	default:
		return nil, usage("proxy groups|show GROUP|search QUERY|select GROUP NODE|auto GROUP|providers|provider show|healthcheck NAME|node SUBCOMMAND|tailscale SUBCOMMAND|delay NODE [URL] [TIMEOUT_MS]|delays GROUP [URL] [TIMEOUT_MS]|restore")
	}
}

func trafficConfig(ctx context.Context, client *control.Client) (map[string]any, error) {
	var cfg map[string]any
	if err := client.Get(ctx, "/configs", &cfg); err != nil {
		return nil, messageError("cli.error.command.mihomo_must_be_running_run_nagi_start_or_inspect_nagi_status_w", "mihomo must be running; run `nagi start` or inspect `nagi status`: %w", err)
	}
	return cfg, nil
}
func intField(m map[string]any, key string) int { v, _ := m[key].(float64); return int(v) }
