package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// Opt in with an explicitly built engine. Ordinary tests must never launch an
// installed binary or authenticate to a tailnet. This test keeps TUN disabled.
func TestTailscaleRealRestrictedEngineLifecycle(t *testing.T) {
	binary := os.Getenv("NAGI_TS_TEST_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("requires root and explicit NAGI_TS_TEST_ENGINE")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".nagi-ts-engine-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	private := filepath.Join(root, "private")
	sourceDir := filepath.Join(root, "source")
	for _, dir := range []string{private, sourceDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(sourceDir, "work.yaml")
	data := []byte("mixed-port: 0\nlog-level: silent\nproxies:\n  - {name: Tail, type: tailscale, exit-node: 100.64.0.10, udp: true, state-dir: /ignored/user/path}\n  - {name: Node, type: socks5, server: 127.0.0.1, port: 19001, dialer-proxy: Tail}\nrules: ['MATCH,DIRECT']\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{SessionUID: os.Getuid(), BinaryPath: binary, SourceDir: sourceDir, PrivateDir: private}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if _, err := h.Handle(stopCtx, os.Getuid(), privileged.Request{Operation: privileged.Stop}); err != nil {
			t.Errorf("stop isolated root child: %v", err)
		}
	}()
	start := func() {
		t.Helper()
		response, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Start, ConfigPath: path, Profile: "work"})
		if err != nil || !response.Running {
			t.Fatalf("restricted start: %+v, %v", response, err)
		}
	}
	start()
	state := nagiruntime.TailscaleStateName(private, "work", "Tail", "")
	marker := filepath.Join(state, "lifetime-fixture")
	if err := os.WriteFile(marker, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	reloadPath := filepath.Join(sourceDir, ".effective-work.yaml")
	if err := os.WriteFile(reloadPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"path": reloadPath, "activation_id": "0123456789abcdef0123456789abcdef"})
	response, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: body})
	if err != nil || response.Status < 200 || response.Status >= 300 {
		t.Fatalf("restricted reload: %+v, %v", response, err)
	}
	if _, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Stop}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "retained" {
		t.Fatalf("snapshot cleanup removed persistent state: %q, %v", got, err)
	}
	start()
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("restart changed state identity", err)
	}
}
