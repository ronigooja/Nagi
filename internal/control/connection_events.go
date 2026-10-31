package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ConnectionFailureError is the structured reason supplied by mihomo.
// Nagi preserves the engine's code, message, and optional detail.
type ConnectionFailureError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// ConnectionFailureEvent is one item from the dedicated mihomo connection
// failure event stream. It is separate from /connections snapshots so failed
// attempts that never become an active connection remain observable.
type ConnectionFailureEvent struct {
	Type         string                 `json:"type"`
	OccurredAt   string                 `json:"occurred_at"`
	ConnectionID string                 `json:"connection_id,omitempty"`
	Target       string                 `json:"target,omitempty"`
	Network      string                 `json:"network,omitempty"`
	Proxy        string                 `json:"proxy,omitempty"`
	Chains       []string               `json:"chains,omitempty"`
	Metadata     map[string]any         `json:"metadata,omitempty"`
	Error        ConnectionFailureError `json:"error"`
}

func (e ConnectionFailureEvent) Validate() error {
	if e.Type != "connection_failure" {
		return errors.New("invalid connection failure event type")
	}
	if strings.TrimSpace(e.OccurredAt) == "" {
		return errors.New("connection failure event is missing occurred_at")
	}
	if strings.TrimSpace(e.Error.Message) == "" {
		return errors.New("connection failure event is missing error.message")
	}
	return nil
}

// WatchConnectionFailures reads mihomo's continuous newline-delimited event
// stream. Like WatchTraffic, it deliberately has no whole-request timeout.
func (c *Client) WatchConnectionFailures(ctx context.Context, receive func(ConnectionFailureEvent) error) error {
	if c.eventsFunc != nil {
		return c.eventsFunc(ctx, receive)
	}
	if c.requestFunc != nil {
		return errors.New("connection failure event stream is unavailable through this bridge")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/connection-events?kind=failure", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: c.http.Transport}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return apiErrorFromBody(resp)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > MaxResponseBytes {
			return ErrResponseTooLarge
		}
		if len(line) != 0 {
			var event ConnectionFailureEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return err
			}
			if err := event.Validate(); err != nil {
				return err
			}
			if err := receive(event); err != nil {
				return err
			}
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(readErr, io.EOF) {
				return errors.New("mihomo connection failure event stream ended")
			}
			return readErr
		}
	}
}

func apiErrorFromBody(resp *http.Response) error {
	var detail struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&detail)
	message := detail.Message
	if message == "" {
		message = detail.Error
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Message: message}
}
