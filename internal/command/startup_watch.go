package command

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/output"
)

const startupWatchInterval = 30 * time.Second
const startupWatchMaxDelay = 5 * time.Minute

// The login service owns this foreground process. Lifecycle commands continue
// to use the same lock as interactive CLI calls.
func runStartupWatch(stderr io.Writer, version, commit string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := execute(shutdown, []string{"stop"}, version, commit); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, engine.ErrNotRunning) {
			output.WriteError(stderr, false, "service_stop_error", err.Error())
		}
	}()
	delay := startupWatchInterval
	for {
		if err := startupWatchStep(ctx, func(args ...string) (any, error) {
			return execute(ctx, args, version, commit)
		}); err != nil {
			if ctx.Err() != nil {
				return 0
			}
			output.WriteError(stderr, false, "service_check_error", err.Error())
			delay *= 2
			if delay > startupWatchMaxDelay {
				delay = startupWatchMaxDelay
			}
		} else {
			delay = startupWatchInterval
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
}

func startupWatchStep(ctx context.Context, call func(...string) (any, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := call("startup", "check")
	if err != nil {
		return err
	}
	check, ok := result.(map[string]any)
	if !ok {
		return errors.New("startup check returned an unexpected result")
	}
	if check["running"] == true {
		if check["control_api"] != true {
			return errors.New("mihomo is running but its control API is unavailable; inspect `nagi logs`")
		}
		return nil
	}
	_, err = call("start")
	return err
}
