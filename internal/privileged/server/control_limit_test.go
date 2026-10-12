package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestPrivilegedControlResponseSizeLimit(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a root-owned controller and root-controlled ancestors")
	}
	// A socket below the world-writable temporary directory must be rejected
	// by VerifyControllerSocket, even when the test itself runs as root.
	root := "/var/lib"
	if runtime.GOOS == "darwin" {
		root = "/private/var/root"
	}
	dir, err := os.MkdirTemp(root, "nagi-limit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	for _, size := range []int{(1 << 20) + 1, control.MaxResponseBytes, control.MaxResponseBytes + 1} {
		for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
			t.Run(fmt.Sprintf("bytes_%d_status_%d", size, status), func(t *testing.T) {
				path := filepath.Join(dir, "controller.sock")
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				base := `{"connections":[],"message":"upstream failure"}`
				body := base + strings.Repeat(" ", size-len(base))
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(body))
				})}
				go server.Serve(listener)
				defer server.Close()
				resp, err := controlRequest(context.Background(), path, privileged.Request{Method: http.MethodGet, Path: "/connections"})
				if size > control.MaxResponseBytes {
					if !errors.Is(err, control.ErrResponseTooLarge) {
						t.Fatalf("expected explicit overflow, got %v", err)
					}
				} else if err != nil || len(resp.Body) != size || resp.Status != status || resp.OK != (status == http.StatusOK) {
					t.Fatalf("boundary response changed: bytes=%d status=%d ok=%v err=%v", len(resp.Body), resp.Status, resp.OK, err)
				}
			})
		}
	}
}
