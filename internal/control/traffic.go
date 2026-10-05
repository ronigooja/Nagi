package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// TrafficSample contains one sample from mihomo's GET /traffic stream.
type TrafficSample struct {
	UploadBPS     int64 `json:"upload_bps"`
	DownloadBPS   int64 `json:"download_bps"`
	UploadTotal   int64 `json:"upload_total"`
	DownloadTotal int64 `json:"download_total"`
}

// WatchTraffic reads mihomo's continuous newline-delimited JSON stream. Unlike
// ordinary control requests, the HTTP client has no whole-request timeout.
func (c *Client) WatchTraffic(ctx context.Context, receive func(TrafficSample) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/traffic", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: c.http.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mihomo control unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
	decoder := json.NewDecoder(resp.Body)
	for {
		var raw struct {
			Up        *int64 `json:"up"`
			Down      *int64 `json:"down"`
			UpTotal   *int64 `json:"upTotal"`
			DownTotal *int64 `json:"downTotal"`
		}
		if err := decoder.Decode(&raw); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return errors.New("mihomo traffic stream ended; check whether mihomo is running")
			}
			return fmt.Errorf("decode mihomo traffic stream: %w", err)
		}
		if raw.Up == nil || raw.Down == nil || raw.UpTotal == nil || raw.DownTotal == nil || *raw.Up < 0 || *raw.Down < 0 || *raw.UpTotal < 0 || *raw.DownTotal < 0 {
			return errors.New("mihomo traffic stream contains missing or negative counters")
		}
		if err := receive(TrafficSample{UploadBPS: *raw.Up, DownloadBPS: *raw.Down, UploadTotal: *raw.UpTotal, DownloadTotal: *raw.DownTotal}); err != nil {
			return err
		}
	}
}
