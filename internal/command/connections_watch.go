package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/proxy"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func connectionsInterval(args []string) (time.Duration, error) {
	if len(args) == 2 {
		return 250 * time.Millisecond, nil
	}
	if len(args) == 4 && args[2] == "--interval" {
		n, err := strconv.Atoi(args[3])
		if err == nil && n >= 250 && n <= 60000 {
			return time.Duration(n) * time.Millisecond, nil
		}
	}
	return 0, messageError("cli.error.connections.interval", "expected --interval MILLISECONDS (250–60000; default 250)")
}

func runConnectionsWatch(parent context.Context, args []string, stdout, stderr io.Writer, jsonMode bool) int {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	interval, _ := connectionsInterval(args)
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
	return watchConnections(ctx, client, interval, stdout, stderr, jsonMode)
}
func watchConnections(ctx context.Context, client *control.Client, interval time.Duration, stdout, stderr io.Writer, jsonMode bool) int {
	err := client.WatchConnections(ctx, interval, func(raw json.RawMessage) error {
		var sample struct {
			Connections []proxy.Connection `json:"connections"`
			SampledAt   string             `json:"sampled_at"`
		}
		if err := json.Unmarshal(raw, &sample); err != nil {
			return err
		}
		if sample.Connections == nil {
			sample.Connections = []proxy.Connection{}
		}
		sample.SampledAt = time.Now().UTC().Format(time.RFC3339Nano)
		var err error
		if jsonMode {
			err = output.Write(stdout, true, sample)
		} else {
			message := messageError("cli.stream.connections.sample", "Sampled: %s", time.Now().Format("2006-01-02 15:04 MST"))
			_, err = io.WriteString(stdout, LanguageContext(ctx).Render(output.SafeMessage(message))+"\n")
			if err == nil {
				err = output.WriteLocalized(stdout, false, LanguageContext(ctx), map[string]any{"connections": sample.Connections})
			}
		}
		if err != nil {
			return &trafficOutputError{err}
		}
		return nil
	})
	if err == nil || (ctx.Err() != nil && errors.Is(err, context.Canceled)) {
		return 0
	}
	code := "mihomo_api_error"
	var writeErr *trafficOutputError
	if errors.As(err, &writeErr) {
		code = "output_error"
	} else if errors.Is(err, control.ErrResponseTooLarge) {
		code = "response_too_large"
	}
	output.WriteErrorCause(stderr, jsonMode, LanguageContext(ctx), code, err)
	return 1
}
