package command

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"

	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/privileged"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

type helperBackend struct {
	profile, binary, configPath string
	paths                       nagiruntime.Paths
	socketPath                  string
}

func controlClientForCurrentBackend(paths nagiruntime.Paths) (*control.Client, error) {
	mode, err := readBackend(paths)
	if err != nil {
		return nil, err
	}
	if mode == userBackend {
		return control.New(paths.SocketPath), nil
	}
	return (helperBackend{paths: paths}).controlClient(), nil
}

func (b helperBackend) call(ctx context.Context, req privileged.Request) (privileged.Response, error) {
	var empty privileged.Response
	req.Version = privileged.ProtocolVersion
	if req.Operation == privileged.Start {
		req.Profile = b.profile
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	socketPath := b.socketPath
	if socketPath == "" {
		socketPath = privileged.SocketPath(os.Getuid())
	}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return empty, messageError("cli.error.privileged_backend.privileged_helper_unavailable_run_nagi_privileged_helper_status_w", "privileged helper unavailable; run `nagi privileged-helper status`: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(31 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return empty, err
	}
	if err := privileged.WriteRequest(conn, req); err != nil {
		return empty, err
	}
	resp, err := privileged.ReadResponse(conn)
	if err != nil {
		return empty, messageError("cli.error.privileged_backend.read_privileged_helper_response_w", "read privileged helper response: %w", err)
	}
	if err := resp.Validate(); err != nil {
		return empty, err
	}
	if req.Operation == privileged.Control && resp.Status != 0 {
		return resp, nil
	}
	if !resp.OK {
		switch resp.Error {
		case "already_running":
			return resp, engine.ErrAlreadyRunning
		case "not_running":
			return resp, engine.ErrNotRunning
		case "response_too_large":
			return resp, control.ErrResponseTooLarge
		}
		var cause error = errors.New(resp.Error)
		if descriptor, ok := resp.Messages["/error"]; ok && i18n.ValidDescriptor(descriptor) {
			cause = i18n.MessageFromDescriptor(descriptor, resp.Error, cause)
		}
		return resp, messageError("cli.error.privileged_backend.privileged_helper_rejected_s_s", "privileged helper rejected %s: %s", req.Operation, cause)
	}
	return resp, nil
}

func (b helperBackend) Start(ctx context.Context) (engine.Status, error) {
	resp, err := b.call(ctx, privileged.Request{Operation: privileged.Start, ConfigPath: b.configPath})
	if err != nil {
		return engine.Status{}, err
	}
	if !resp.Running {
		return engine.Status{}, messageError("cli.error.privileged_backend.privileged_helper_did_not_report_a_running_mihomo_process", "privileged helper did not report a running mihomo process")
	}
	return b.statusFromResponse(resp), nil
}

func (b helperBackend) Stop(ctx context.Context) (engine.Status, error) {
	resp, err := b.call(ctx, privileged.Request{Operation: privileged.Stop})
	if err != nil {
		return engine.Status{}, err
	}
	return b.statusFromResponse(resp), nil
}

func (b helperBackend) Status(ctx context.Context) (engine.Status, error) {
	resp, err := b.call(ctx, privileged.Request{Operation: privileged.Status})
	if err != nil {
		return engine.Status{}, err
	}
	return b.statusFromResponse(resp), nil
}

func (b helperBackend) statusFromResponse(resp privileged.Response) engine.Status {
	socketPath := b.socketPath
	if socketPath == "" {
		socketPath = privileged.SocketPath(os.Getuid())
	}
	return engine.Status{Running: resp.Running, PID: resp.PID, SocketPath: socketPath}
}

func (b helperBackend) Logs(ctx context.Context, lines int) ([]string, error) {
	resp, err := b.call(ctx, privileged.Request{Operation: privileged.Logs, Lines: lines})
	if err != nil {
		return nil, err
	}
	var result []string
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, messageError("cli.error.privileged_backend.decode_helper_logs_w", "decode helper logs: %w", err)
	}
	return result, nil
}

type helperFollowResult struct{ backend helperBackend }

func (f helperFollowResult) Follow(ctx context.Context, emit func(string) error) error {
	resp, err := f.backend.call(ctx, privileged.Request{Operation: privileged.Logs, Offset: -1})
	if err != nil {
		return err
	}
	offset := resp.Offset
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		resp, err := f.backend.call(ctx, privileged.Request{Operation: privileged.Logs, Offset: offset})
		if err != nil {
			return err
		}
		var lines []string
		if err := json.Unmarshal(resp.Body, &lines); err != nil {
			return messageError("cli.error.privileged_backend.decode_helper_logs_w_95", "decode helper logs: %w", err)
		}
		for _, line := range lines {
			if err := emit(strings.TrimSuffix(line, "\n")); err != nil {
				return err
			}
		}
		offset = resp.Offset
	}
}

func (b helperBackend) controlClient() *control.Client {
	request := func(ctx context.Context, method, path string, body, out any) error {
		var raw json.RawMessage
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				return err
			}
			raw = encoded
		}
		resp, err := b.call(ctx, privileged.Request{Operation: privileged.Control, Method: method, Path: path, Body: raw})
		if err != nil {
			return err
		}
		if resp.Status < 200 || resp.Status >= 300 {
			message := http.StatusText(resp.Status)
			var detail struct {
				Message string `json:"message"`
				Error   string `json:"error"`
			}
			_ = json.Unmarshal(resp.Body, &detail)
			if detail.Message != "" {
				message = detail.Message
			} else if detail.Error != "" {
				message = detail.Error
			}
			return &control.APIError{Status: resp.Status, Message: message}
		}
		if out != nil && resp.Status != http.StatusNoContent && len(resp.Body) != 0 {
			if err := json.Unmarshal(resp.Body, out); err != nil {
				return messageError("cli.error.privileged_backend.decode_mihomo_response_w", "decode mihomo response: %w", err)
			}
		}
		return nil
	}
	traffic := func(ctx context.Context, receive func(control.TrafficSample) error) error {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			var raw struct {
				Up        *int64 `json:"up"`
				Down      *int64 `json:"down"`
				UpTotal   *int64 `json:"upTotal"`
				DownTotal *int64 `json:"downTotal"`
			}
			if err := request(ctx, http.MethodGet, "/traffic", nil, &raw); err != nil {
				return err
			}
			if raw.Up == nil || raw.Down == nil || raw.UpTotal == nil || raw.DownTotal == nil || *raw.Up < 0 || *raw.Down < 0 || *raw.UpTotal < 0 || *raw.DownTotal < 0 {
				return messageError("cli.error.privileged_backend.mihomo_traffic_sample_contains_missing_or_negative_counters", "mihomo traffic sample contains missing or negative counters")
			}
			if err := receive(control.TrafficSample{UploadBPS: *raw.Up, DownloadBPS: *raw.Down, UploadTotal: *raw.UpTotal, DownloadTotal: *raw.DownTotal}); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	return control.NewWithBridge(request, traffic)
}
