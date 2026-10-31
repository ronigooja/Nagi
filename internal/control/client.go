package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client talks to mihomo's HTTP API exclusively over a local Unix socket.
type Client struct {
	http        *http.Client
	requestFunc func(context.Context, string, string, any, any) error
	trafficFunc func(context.Context, func(TrafficSample) error) error
	eventsFunc  func(context.Context, func(ConnectionFailureEvent) error) error
}

// NewWithBridge sends control operations through an authenticated local
// helper. The bridge enforces its own allowlist before forwarding to mihomo.
func NewWithBridge(request func(context.Context, string, string, any, any) error, traffic func(context.Context, func(TrafficSample) error) error) *Client {
	return &Client{requestFunc: request, trafficFunc: traffic}
}

func NewWithBridgeAndEvents(request func(context.Context, string, string, any, any) error, traffic func(context.Context, func(TrafficSample) error) error, events func(context.Context, func(ConnectionFailureEvent) error) error) *Client {
	return &Client{requestFunc: request, trafficFunc: traffic, eventsFunc: events}
}

type APIError struct {
	Status  int
	Message string
}

// MaxResponseBytes bounds one ordinary mihomo control API response. Streaming
// traffic requests use their own per-sample limit instead.
const MaxResponseBytes = 4 << 20

var ErrResponseTooLarge = i18n.Errorf("backend.control.mihomo_response_exceeds_the_4_mib_limit", "mihomo response exceeds the 4 MiB limit")

func (e *APIError) Error() string { return fmt.Sprintf("mihomo API %d: %s", e.Status, e.Message) }

func (e *APIError) Unwrap() error {
	return i18n.Errorf("backend.control.api_response", "mihomo API %d: %s", e.Status, e.Message)
}

func New(socketPath string) *Client {
	return NewWithTimeout(socketPath, 10*time.Second)
}

func NewWithTimeout(socketPath string, timeout time.Duration) *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socketPath)
	}}
	return &Client{http: &http.Client{Transport: transport, Timeout: timeout}}
}

func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) Put(ctx context.Context, path string, body any, out any) error {
	return c.request(ctx, http.MethodPut, path, body, out)
}

func (c *Client) Post(ctx context.Context, path string, body any, out any) error {
	return c.request(ctx, http.MethodPost, path, body, out)
}

func (c *Client) Patch(ctx context.Context, path string, body any, out any) error {
	return c.request(ctx, http.MethodPatch, path, body, out)
}

func (c *Client) Delete(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodDelete, path, nil, out)
}

func (c *Client) request(ctx context.Context, method, path string, body, out any) error {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return i18n.Errorf("backend.control.invalid_mihomo_api_path", "invalid mihomo API path")
	}
	if c.requestFunc != nil {
		return c.requestFunc(ctx, method, path, body, out)
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return i18n.Errorf("backend.control.mihomo_control_unavailable_w", "mihomo control unavailable: %w", err)
	}
	defer resp.Body.Close()
	// Read one extra byte to distinguish the exact boundary from truncation.
	// Check the entire response even when the caller does not decode a body.
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return i18n.Errorf("backend.control.read_mihomo_response_w", "read mihomo response: %w", err)
	}
	if len(data) > MaxResponseBytes {
		return ErrResponseTooLarge
	}
	limited := bytes.NewReader(data)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var detail struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(limited).Decode(&detail)
		message := detail.Message
		if message == "" {
			message = detail.Error
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Message: message}
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(limited).Decode(out); err != nil {
			return i18n.Errorf("backend.control.decode_mihomo_response_w", "decode mihomo response: %w", err)
		}
	}
	return nil
}
