package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/output"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type connectionFailureOutputError struct{ err error }

func (e *connectionFailureOutputError) Error() string { return e.err.Error() }
func (e *connectionFailureOutputError) Unwrap() error { return e.err }

func writeConnectionFailure(w io.Writer, jsonMode bool, event control.ConnectionFailureEvent) error {
	if jsonMode {
		return output.Write(w, true, event)
	}
	target := event.Target
	if target == "" {
		target = "unknown target"
	}
	proxyName := event.Proxy
	if proxyName == "" {
		proxyName = "direct"
	}
	when := event.OccurredAt
	if parsed, err := time.Parse(time.RFC3339Nano, event.OccurredAt); err == nil {
		when = parsed.Local().Format("2006-01-02 15:04 MST")
	}
	_, err := fmt.Fprintf(w, "%s connection failure: %s via %s: %s\n", when, target, proxyName, event.Error.Message)
	return err
}

func runConnectionFailures(parent context.Context, stdout, stderr io.Writer, jsonMode bool) int {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	paths, err := nagiruntime.Resolve()
	if err != nil {
		output.WriteErrorCause(stderr, jsonMode, LanguageContext(ctx), "internal_error", err)
		return 1
	}
	client, err := controlClientForCurrentBackend(paths)
	if err != nil {
		output.WriteErrorCause(stderr, jsonMode, LanguageContext(ctx), "internal_error", err)
		return 1
	}
	err = client.WatchConnectionFailures(ctx, func(event control.ConnectionFailureEvent) error {
		if err := writeConnectionFailure(stdout, jsonMode, event); err != nil {
			return &connectionFailureOutputError{err}
		}
		return nil
	})
	if err == nil || (ctx.Err() != nil && errors.Is(err, context.Canceled)) {
		return 0
	}
	code := "mihomo_api_error"
	var outputErr *connectionFailureOutputError
	if errors.As(err, &outputErr) {
		code = "output_error"
	}
	output.WriteErrorCause(stderr, jsonMode, LanguageContext(ctx), code, err)
	return 1
}
