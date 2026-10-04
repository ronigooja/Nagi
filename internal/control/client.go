package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client talks to mihomo's HTTP API exclusively over a local Unix socket.
type Client struct {
	http *http.Client
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("mihomo API %d: %s", e.Status, e.Message) }

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

func (c *Client) Patch(ctx context.Context, path string, body any, out any) error {
	return c.request(ctx, http.MethodPatch, path, body, out)
}

func (c *Client) Delete(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodDelete, path, nil, out)
}

func (c *Client) request(ctx context.Context, method, path string, body, out any) error {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("invalid mihomo API path")
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
		return fmt.Errorf("mihomo control unavailable: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 4<<20)
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
			return fmt.Errorf("decode mihomo response: %w", err)
		}
	}
	return nil
}
