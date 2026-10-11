package command

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/platform"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/traffic"
)

func TestRuntimeTUNDNSFailureRestoresTemporaryRuntime(t *testing.T) {
	oldSync := synchronizeTUNDNS
	defer func() { synchronizeTUNDNS = oldSync }()
	running := map[string]any{"enable": false, "auto-route": false, "auto-detect-interface": false, "dns-hijack": []any{"any:53"}}
	before := running
	patches := 0
	client := control.NewWithBridge(func(_ context.Context, method, path string, body, out any) error {
		switch method {
		case http.MethodPatch:
			patches++
			running = body.(map[string]any)["tun"].(map[string]any)
		case http.MethodGet:
			*out.(*map[string]any) = map[string]any{"tun": running}
		}
		return nil
	}, nil)
	syncs := 0
	synchronizeTUNDNS = func(_ context.Context, _ nagiruntime.Paths, _ *control.Client, _ int) (traffic.DNSState, error) {
		syncs++
		if running["enable"] == true {
			return traffic.DNSState{}, errors.New("administrator access denied")
		}
		return traffic.DNSState{}, nil
	}
	_, err := changeRuntimeTUN(context.Background(), nagiruntime.Paths{}, client, before, map[string]any{"enable": true, "auto-route": true, "auto-detect-interface": true})
	if err == nil || patches != 2 || syncs != 2 || running["enable"] != false || running["auto-route"] != false {
		t.Fatalf("state=%v patches=%d syncs=%d err=%v", running, patches, syncs, err)
	}
}

func TestRuntimeTUNDisableSynchronizesDNSWithoutChangingProfile(t *testing.T) {
	oldSync := synchronizeTUNDNS
	defer func() { synchronizeTUNDNS = oldSync }()
	enabled := true
	client := control.NewWithBridge(func(_ context.Context, method, path string, body, out any) error {
		if method == http.MethodPatch {
			enabled = body.(map[string]any)["tun"].(map[string]any)["enable"] == true
		}
		if method == http.MethodGet {
			*out.(*map[string]any) = map[string]any{"tun": map[string]any{"enable": enabled}}
		}
		return nil
	}, nil)
	called := false
	synchronizeTUNDNS = func(_ context.Context, _ nagiruntime.Paths, _ *control.Client, _ int) (traffic.DNSState, error) {
		called = true
		return traffic.DNSState{}, nil
	}
	cfg, err := changeRuntimeTUN(context.Background(), nagiruntime.Paths{}, client, map[string]any{"enable": true}, map[string]any{"enable": false})
	if err != nil || !called || cfg["tun"].(map[string]any)["enable"] != false {
		t.Fatalf("cfg=%v sync=%v err=%v", cfg, called, err)
	}
}

type dnsStartupBackend struct{ stopped bool }

func (b *dnsStartupBackend) Start(context.Context) (engine.Status, error) {
	return engine.Status{Running: true, PID: 123}, nil
}
func (b *dnsStartupBackend) Status(context.Context) (engine.Status, error) {
	return engine.Status{Running: !b.stopped, PID: 123}, nil
}
func (b *dnsStartupBackend) Stop(context.Context) (engine.Status, error) {
	b.stopped = true
	return engine.Status{}, nil
}

func TestTUNDNSStartupFailureStopsEngine(t *testing.T) {
	oldOS, oldSync := tunDNSOS, synchronizeTUNDNS
	defer func() { tunDNSOS = oldOS; synchronizeTUNDNS = oldSync }()
	tunDNSOS = "darwin"
	synchronizeTUNDNS = func(context.Context, nagiruntime.Paths, *control.Client, int) (traffic.DNSState, error) {
		return traffic.DNSState{}, errors.New("DNS takeover denied")
	}
	root := t.TempDir()
	paths := nagiruntime.Paths{StateDir: root, ConfigDir: root}
	backend := &dnsStartupBackend{}
	_, err := startWithTUNDNS(context.Background(), paths, backend, nil, "missing-test-config", "default")
	if err == nil || !backend.stopped {
		t.Fatalf("stopped=%v err=%v", backend.stopped, err)
	}
}

func TestTUNHijackRetainsCustomInterception(t *testing.T) {
	values := tunDNSHijack([]any{"192.0.2.53:53", "any:53"})
	if len(values) != 3 || values[0] != "192.0.2.53:53" || values[1] != "any:53" || values[2] != "tcp://any:53" {
		t.Fatalf("hijack=%v", values)
	}
}

func TestTUNDNSRequiresRuntimeDNSEnabled(t *testing.T) {
	tun := map[string]any{"auto-route": true, "dns-hijack": []any{"any:53"}}
	paths := nagiruntime.Paths{ConfigDir: t.TempDir()}
	if err := validateTUNDNS(paths, map[string]any{"dns": map[string]any{"enable": false}}, tun); err == nil {
		t.Fatal("disabled runtime DNS accepted")
	}
	if err := validateTUNDNS(paths, map[string]any{"dns": map[string]any{"enable": true}}, tun); err != nil {
		t.Fatal(err)
	}
}

func TestTUNRollbackSnapshotContainsOnlyWritableFields(t *testing.T) {
	snapshot := tunRuntimeSnapshot(map[string]any{"enable": true, "device": "utun9", "stack": "mixed", "inet4-address": []any{"198.18.0.1/30"}})
	if len(snapshot) != 5 || snapshot["enable"] != true || snapshot["auto-route"] != false || snapshot["strict-route"] != false {
		t.Fatalf("snapshot=%v", snapshot)
	}
	if values, ok := snapshot["dns-hijack"].([]string); !ok || len(values) != 0 {
		t.Fatalf("dns-hijack=%v", snapshot["dns-hijack"])
	}
}

func TestConfigTUNDNSFailureRestoresRuntimeAndTemporaryTUN(t *testing.T) {
	oldOS, oldSync := tunDNSOS, synchronizeTUNDNS
	defer func() { tunDNSOS = oldOS; synchronizeTUNDNS = oldSync }()
	tunDNSOS = "darwin"
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	prior := []byte("mixed-port: 17890\ntun:\n  enable: false\n")
	if err := os.WriteFile(source, prior, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n"))
	run, err := os.MkdirTemp("", "nagi-dns-run-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(run)
	socket := filepath.Join(run, "mihomo.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	id := "old"
	tun := map[string]any{"enable": true, "auto-route": true, "auto-detect-interface": true, "strict-route": true, "dns-hijack": []any{"any:53", "tcp://any:53"}}
	patches := 0
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			id = body["activation_id"].(string)
			tun = map[string]any{"enable": false}
			w.WriteHeader(204)
			return
		}
		if r.Method == http.MethodPatch {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			tun = body["tun"].(map[string]any)
			patches++
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"activation_id": id, "tun": tun, "mixed-port": 17890})
	})}
	go server.Serve(listener)
	defer server.Close()
	synchronizeTUNDNS = func(_ context.Context, _ nagiruntime.Paths, _ *control.Client, _ int) (traffic.DNSState, error) {
		mu.Lock()
		defer mu.Unlock()
		if id == "candidate" {
			return traffic.DNSState{}, errors.New("DNS target update failed")
		}
		if tun["enable"] != true {
			return traffic.DNSState{}, errors.New("prior temporary TUN not restored")
		}
		return traffic.DNSState{}, nil
	}
	paths := nagiruntime.Paths{ConfigDir: config, StateDir: filepath.Join(dir, "state"), RuntimeDir: run, SocketPath: socket}
	_, err = applyTransaction(context.Background(), paths, []string{"config", "reload"}, func() (any, error) {
		if err := os.WriteFile(source, []byte("mixed-port: 17900\ntun:\n  enable: true\n"), 0600); err != nil {
			return nil, err
		}
		mu.Lock()
		id = "candidate"
		mu.Unlock()
		return map[string]any{"reloaded": true}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "prior running configuration restored") {
		t.Fatalf("err=%v", err)
	}
	restored, readErr := os.ReadFile(source)
	mu.Lock()
	defer mu.Unlock()
	if readErr != nil || string(restored) != string(prior) || tun["enable"] != true || patches != 1 {
		t.Fatalf("source=%q tun=%v patches=%d read=%v", restored, tun, patches, readErr)
	}
}

func TestStartupWatchReportsDNSFailureWithoutRestartingLiveEngine(t *testing.T) {
	calls := 0
	err := startupWatchStep(context.Background(), func(args ...string) (any, error) {
		calls++
		return map[string]any{"running": true, "control_api": true, "system_dns_error": "restore conflict"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "system DNS recheck failed") || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestExitWatcherDuplicateDoesNotWaitForLiveEngine(t *testing.T) {
	dir := t.TempDir()
	paths := nagiruntime.Paths{RuntimeDir: dir, SocketPath: filepath.Join(dir, "socket"), PIDPath: filepath.Join(dir, "pid"), LockPath: filepath.Join(dir, "lock"), LogPath: filepath.Join(dir, "logs", "mihomo.log")}
	lock, err := os.OpenFile(exitWatcherLock(paths, os.Getpid()), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := platform.Lock(lock); err != nil {
		t.Fatal(err)
	}
	defer platform.Unlock(lock)
	// A duplicate exits even though the engine PID is still alive and the caller
	// has already cancelled its context; the existing watcher owns restoration.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runExitWatcher(ctx, paths, os.Getpid()); err != nil {
		t.Fatal(err)
	}
}
