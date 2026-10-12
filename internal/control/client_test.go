package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
