package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixSocketAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mihomo.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "test"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"missing"}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	defer os.Remove(path)
	client := New(path)
	var version struct {
		Version string `json:"version"`
	}
	if err := client.Get(context.Background(), "/version", &version); err != nil {
		t.Fatal(err)
	}
	if version.Version != "test" {
		t.Fatalf("version=%q", version.Version)
	}
	if err := client.Get(context.Background(), "/absent", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestResponseSizeLimit(t *testing.T) {
	for _, size := range []int{(1 << 20) + 1, MaxResponseBytes, MaxResponseBytes + 1} {
		for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
			t.Run(fmt.Sprintf("bytes_%d_status_%d", size, status), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "mihomo.sock")
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				// Padding is valid JSON and exercises the complete response size,
				// including bytes following an otherwise complete JSON value.
				base := `{"connections":[],"message":"upstream failure"}`
				body := base + strings.Repeat(" ", size-len(base))
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(body))
				})}
				go server.Serve(listener)
				defer server.Close()
				var result struct {
					Connections []any `json:"connections"`
				}
				err = New(path).Get(context.Background(), "/connections", &result)
				if size > MaxResponseBytes {
					if !errors.Is(err, ErrResponseTooLarge) {
						t.Fatalf("expected explicit overflow, got %v", err)
					}
				} else if status == http.StatusOK {
					if err != nil || result.Connections == nil {
						t.Fatalf("boundary list failed: %+v, %v", result, err)
					}
				} else {
					var api *APIError
					if !errors.As(err, &api) || api.Status != status || api.Message != "upstream failure" {
						t.Fatalf("API error changed: %v", err)
					}
				}
			})
		}
	}
}

func TestPatchAndDeleteMethods(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mihomo.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/configs" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/connections/abc" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})}
	go server.Serve(listener)
	defer server.Close()
	client := New(path)
	if err := client.Patch(context.Background(), "/configs", map[string]string{"mode": "rule"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), "/connections/abc", nil); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionFailureEventJSONContract(t *testing.T) {
	input := `{"type":"connection_failure","occurred_at":"2026-10-31T12:34:56Z","connection_id":"abc","target":"example.test:443","network":"tcp","proxy":"Node A","chains":["Group","Node A"],"metadata":{"process":"curl"},"error":{"code":"dial_timeout","message":"dial tcp: i/o timeout","detail":"upstream timed out"}}`
	var event ConnectionFailureEvent
	if err := json.Unmarshal([]byte(input), &event); err != nil {
		t.Fatal(err)
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"type", "occurred_at", "connection_id", "target", "network", "proxy", "chains", "metadata", "error"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing JSON field %q: %s", key, encoded)
		}
	}
	if _, ok := got["occurredAt"]; ok {
		t.Fatal("camelCase timestamp leaked into contract")
	}
}

func TestWatchConnectionFailuresReadsNDJSON(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "nce-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "mihomo.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connection-events" || r.URL.Query().Get("kind") != "failure" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"type":"connection_failure","occurred_at":"2026-10-31T12:34:56Z","target":"example.test:443","error":{"code":"refused","message":"connection refused"}}`+"\n")
	})}
	go server.Serve(listener)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []ConnectionFailureEvent
	err = New(path).WatchConnectionFailures(ctx, func(event ConnectionFailureEvent) error {
		events = append(events, event)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || len(events) != 1 || events[0].Error.Code != "refused" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}
