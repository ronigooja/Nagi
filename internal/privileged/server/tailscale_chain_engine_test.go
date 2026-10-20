package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
)

// The reviewed engine must be explicitly selected. All endpoints are loopback;
// there is no Auth Key, real tailnet, external proxy, or TUN in this fixture.
func TestTailscaleChainRealRestrictedReloadAndRejection(t *testing.T) {
	binary := os.Getenv("NAGI_TS_TEST_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("requires root and explicit NAGI_TS_TEST_ENGINE")
	}
	root := secureTestDir(t)
	source, private := filepath.Join(root, "source"), filepath.Join(root, "private")
	for _, dir := range []string{source, private} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(source, "work.yaml")
	plain := "mixed-port: 0\nmode: direct\nlog-level: silent\ntun: {enable: false}\ndns: {enable: false}\nrules: ['MATCH,DIRECT']\n"
	if err := os.WriteFile(path, []byte(plain), 0600); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{SessionUID: os.Getuid(), BinaryPath: binary, SourceDir: source, PrivateDir: private}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Errorf("stop isolated engine: %v", err)
		}
	})
	started, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Start, ConfigPath: path, Profile: "work"})
	if err != nil || !started.Running {
		t.Fatalf("start: %+v %v", started, err)
	}
	activeID := func() string {
		t.Helper()
		response, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/configs"})
		if err != nil {
			t.Fatal(err)
		}
		var current struct {
			ActivationID string `json:"activation_id"`
		}
		if err := json.Unmarshal(response.Body, &current); err != nil {
			t.Fatal(err)
		}
		return current.ActivationID
	}
	prior := activeID()
	first := plain + "proxies:\n  - {name: Tail, type: tailscale, exit-node: 100.64.0.10, control-url: 'https://127.0.0.1:1', dialer-proxy: ''}\n  - {name: Node, type: socks5, server: 127.0.0.1, port: 9, dialer-proxy: Tail}\n"
	later := plain + "proxies:\n  - {name: Tail, type: tailscale, exit-node: 100.64.0.10, control-url: 'https://127.0.0.1:1', dialer-proxy: Node}\n  - {name: Node, type: socks5, server: 127.0.0.1, port: 9, dialer-proxy: ''}\n"
	for i, body := range []string{first, later} {
		id := []string{"0123456789abcdef0123456789abcdef", "1123456789abcdef0123456789abcdef"}[i]
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]string{"path": path, "activation_id": id})
		response, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: payload})
		if err != nil || !response.OK || response.NotForwarded {
			t.Fatalf("direction %d: %+v %v", i, response, err)
		}
		if got := activeID(); got != id || got == prior {
			t.Fatalf("direction %d activation %q", i, got)
		}
		if h.status().PID != started.PID {
			t.Fatal("reload restarted engine")
		}
		prior = id
	}
	// A native-policy failure must be reported as definitely not forwarded and
	// must not replace the active marker or the retained running snapshot.
	beforeSnapshot := h.snapshot

	for _, value := range []string{"[Node]", "Missing", "Tail"} {
		invalid := strings.Replace(later, "dialer-proxy: Node", "dialer-proxy: "+value, 1)
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]string{"path": path, "activation_id": "2123456789abcdef0123456789abcdef"})
		response, err := h.Handle(ctx, os.Getuid(), privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: payload})
		if err == nil || !response.NotForwarded {
			t.Fatalf("rejection %s: %+v %v", value, response, err)
		}
		if value == "[Node]" && !strings.Contains(err.Error(), "proxies[0].dialer-proxy") {
			t.Fatalf("missing field location: %v", err)
		}
		if activeID() != prior || h.snapshot != beforeSnapshot {
			t.Fatal("rejected reload changed active configuration")
		}
	}
}
