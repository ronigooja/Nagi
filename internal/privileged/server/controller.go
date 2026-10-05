package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// ControllerAction is deliberately an enum, not an HTTP method or URL from
// the client. Every future action needs an explicit authorization review.
type ControllerAction string

const ControllerVersion ControllerAction = "version"

// QueryController performs one fixed, read-only mihomo controller request.
// It never forwards client-supplied HTTP bytes, headers, URL, or body.
func QueryController(ctx context.Context, socketPath string, action ControllerAction) ([]byte, error) {
	var endpoint string
	switch action {
	case ControllerVersion:
		endpoint = "/version"
	default:
		return nil, errors.New("controller action is not allowed")
	}
	if err := VerifyControllerSocket(socketPath); err != nil {
		return nil, fmt.Errorf("unsafe controller socket: %w", err)
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://nagi.invalid"+endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("controller returned HTTP %d", resp.StatusCode)
	}
	const maxBody = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, errors.New("controller response too large")
	}
	return body, nil
}
