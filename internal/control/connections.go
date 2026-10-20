package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// WatchConnections delivers complete snapshots sequentially. The helper bridge
// deliberately uses its existing GET allowlist rather than a new wire protocol.
func (c *Client) WatchConnections(ctx context.Context, interval time.Duration, receive func(json.RawMessage) error) error {
	if c.requestFunc != nil {
		for {
			var raw json.RawMessage
			if err := c.Get(ctx, "/connections", &raw); err != nil {
				return err
			}
			if len(raw) > MaxResponseBytes {
				return ErrResponseTooLarge
			}
			if err := receive(raw); err != nil {
				return err
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	conn, resp, err := websocket.Dial(ctx, "ws://unix/connections?interval="+strconv.FormatInt(interval.Milliseconds(), 10), &websocket.DialOptions{HTTPClient: &http.Client{Transport: c.http.Transport}})
	if err != nil {
		if resp != nil {
			return &APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		}
		return err
	}
	defer conn.CloseNow()
	// Bound decompressed message bytes ourselves, preserving the common error.
	conn.SetReadLimit(-1)
	for {
		_, reader, err := conn.Reader(ctx)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(reader, MaxResponseBytes+1))
		if len(raw) > MaxResponseBytes {
			return ErrResponseTooLarge
		}
		if err != nil {
			return err
		}
		if !json.Valid(raw) {
			return errors.New("invalid mihomo connections snapshot")
		}
		if err := receive(raw); err != nil {
			return err
		}
	}
}
