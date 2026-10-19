package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/output"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type trafficWatchSample struct {
	control.TrafficSample
	SampledAt string `json:"sampled_at"`
}

type trafficOutputError struct{ err error }

func (e *trafficOutputError) Error() string { return e.err.Error() }

func runTrafficWatch(parent context.Context, stdout, stderr io.Writer, jsonMode bool) int {
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
	return watchTraffic(ctx, client, stdout, stderr, jsonMode)
}

func watchTraffic(ctx context.Context, client *control.Client, stdout, stderr io.Writer, jsonMode bool) int {
	encoder := json.NewEncoder(stdout)
	err := client.WatchTraffic(ctx, func(sample control.TrafficSample) error {
		item := trafficWatchSample{TrafficSample: sample, SampledAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if jsonMode {
			if err := encoder.Encode(output.Envelope{OK: true, Data: item}); err != nil {
				return &trafficOutputError{err}
			}
			return nil
		}
		sampleTime := time.Now().UTC().Format("2006-01-02 15:04 MST")
		message := messageError("cli.stream.traffic.sample", "Upload: %d B/s (total %d B)  Download: %d B/s (total %d B)  Sampled: %s", item.UploadBPS, item.UploadTotal, item.DownloadBPS, item.DownloadTotal, sampleTime)
		if _, err := stdout.Write([]byte(LanguageContext(ctx).Render(output.SafeMessage(message)) + "\n")); err != nil {
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
	}
	output.WriteErrorCause(stderr, jsonMode, LanguageContext(ctx), code, err)
	return 1
}
