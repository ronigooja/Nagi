package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
