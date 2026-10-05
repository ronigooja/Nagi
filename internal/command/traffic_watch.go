package command

import (
	"context"
	"encoding/json"
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

type trafficWatchSample struct {
	control.TrafficSample
	SampledAt string `json:"sampled_at"`
}

type trafficOutputError struct{ err error }

func (e *trafficOutputError) Error() string { return e.err.Error() }

func runTrafficWatch(stdout, stderr io.Writer, jsonMode bool) int {
	paths, err := nagiruntime.Resolve()
	if err != nil {
		output.WriteError(stderr, jsonMode, "internal_error", err.Error())
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return watchTraffic(ctx, control.New(paths.SocketPath), stdout, stderr, jsonMode)
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
		if _, err := fmt.Fprintf(stdout, "Upload: %d B/s (total %d B)  Download: %d B/s (total %d B)  Sampled: %s\n", item.UploadBPS, item.UploadTotal, item.DownloadBPS, item.DownloadTotal, item.SampledAt); err != nil {
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
	output.WriteError(stderr, jsonMode, code, err.Error())
	return 1
}
