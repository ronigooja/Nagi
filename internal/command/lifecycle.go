package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/platform"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/service"
)

var pauseMonitor = service.PauseIfActive

// The lifecycle lock spans intent changes and engine operations. The engine's
// own lock still protects its PID and socket from other engine callers.
func withLifecycleLock(paths nagiruntime.Paths, fn func() (any, error)) (any, error) {
	if err := paths.EnsureRuntime(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(paths.RuntimeDir, "lifecycle.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := platform.Lock(file); err != nil {
		return nil, err
	}
	defer platform.Unlock(file)
	return fn()
}

func intentPath(paths nagiruntime.Paths) string {
	return filepath.Join(paths.StateDir, "engine-intent")
}

func readIntent(paths nagiruntime.Paths) (string, error) {
	data, err := os.ReadFile(intentPath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return "running", nil
	}
	if err != nil {
		return "", err
	}
	intent := strings.TrimSpace(string(data))
	if intent != "running" && intent != "stopped" && intent != "quit-pending" && intent != "quit-ready" {
		return "", fmt.Errorf("invalid engine intent at %s", intentPath(paths))
	}
	return intent, nil
}

func writeIntent(paths nagiruntime.Paths, intent string) error {
	if err := os.MkdirAll(paths.StateDir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(paths.StateDir, ".engine-intent-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.WriteString(intent + "\n"); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), intentPath(paths))
}

func execute(ctx context.Context, args []string, version, commit string) (any, error) {
	if len(args) == 0 {
		return executeInternal(ctx, args, version, commit)
	}
	command := args[0]
	locked := command == "start" || command == "stop" || command == "restart" ||
		(command == "startup" && len(args) == 2 && (args[1] == "ensure" || args[1] == "shutdown" || args[1] == "check" || args[1] == "login"))
	if command == "quit" {
		return quit(ctx, args, version, commit)
	}
	if !locked {
		return executeInternal(ctx, args, version, commit)
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, err
	}
	result, err := withLifecycleLock(paths, func() (any, error) {
		switch command {
		case "start", "restart":
			if intent, err := readIntent(paths); err != nil {
				return nil, err
			} else if intent == "quit-pending" {
				return nil, fail("lifecycle_busy", errors.New("Nagi quit is in progress; retry after it completes, or run `nagi quit` again if the prior quit was interrupted"))
			}
			if err := writeIntent(paths, "running"); err != nil {
				return nil, err
			}
		case "stop":
			if intent, err := readIntent(paths); err != nil {
				return nil, err
			} else if intent == "quit-pending" {
				return nil, fail("lifecycle_busy", errors.New("Nagi quit is in progress; retry after it completes, or run `nagi quit` again if the prior quit was interrupted"))
			}
			if err := writeIntent(paths, "stopped"); err != nil {
				return nil, err
			}
		case "startup":
			if args[1] == "login" {
				intent, err := readIntent(paths)
				if err != nil {
					return nil, err
				}
				if intent == "quit-ready" {
					if err := writeIntent(paths, "running"); err != nil {
						return nil, err
					}
				}
				return map[string]any{"intent": intent}, nil
			}
			if args[1] == "ensure" {
				intent, err := readIntent(paths)
				if err != nil {
					return nil, err
				}
				if intent != "running" {
					return map[string]any{"started": false, "reason": intent}, nil
				}
				return executeInternal(ctx, []string{"start"}, version, commit)
			}
			if args[1] == "shutdown" {
				return executeInternal(ctx, []string{"stop"}, version, commit)
			}
		}
		return executeInternal(ctx, args, version, commit)
	})
	if command == "start" && (err == nil || errors.Is(err, engine.ErrAlreadyRunning)) {
		if resumeErr := service.ResumeIfEnabled(); resumeErr != nil {
			return nil, fmt.Errorf("mihomo is running but login monitor could not resume: %w", resumeErr)
		}
	}
	return result, err
}

func quit(ctx context.Context, args []string, version, commit string) (result any, err error) {
	if len(args) != 1 {
		return nil, usage("quit")
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		return nil, err
	}
	// Mark stopped before asking the service manager to terminate its monitor.
	// launchctl/systemctl can wait for that monitor, so do not hold the lock here.
	if _, err := withLifecycleLock(paths, func() (any, error) { return nil, writeIntent(paths, "quit-pending") }); err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			_, rollbackErr := withLifecycleLock(paths, func() (any, error) { return nil, writeIntent(paths, "stopped") })
			err = quitRollbackError(err, rollbackErr)
		}
	}()
	if err := pauseMonitor(); err != nil {
		return nil, fmt.Errorf("quit could not pause the login monitor; run `nagi startup status` and retry: %w", err)
	}
	_, err = withLifecycleLock(paths, func() (any, error) { return executeInternal(ctx, []string{"stop"}, version, commit) })
	if err != nil && !errors.Is(err, engine.ErrNotRunning) {
		return nil, err
	}
	if _, err := withLifecycleLock(paths, func() (any, error) { return nil, writeIntent(paths, "quit-ready") }); err != nil {
		return nil, err
	}
	completed = true
	return map[string]any{"quit": true}, nil
}

func quitRollbackError(cause, rollbackErr error) error {
	if rollbackErr == nil {
		return cause
	}
	return fmt.Errorf("%w; could not clear pending quit state: %v; repair Nagi's state directory and retry `nagi quit`", cause, rollbackErr)
}
