package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
	"gopkg.in/yaml.v3"
)

func TestTUNPreferenceMaterialization(t *testing.T) {
	for _, tc := range []struct {
		name, intent, source string
		enabled, overlay     bool
	}{
		{"unset", "", "tun: {enable: true}\n", true, false},
		{"empty", "\n", "tun: {enable: true}\n", true, false},
		{"disabled", "disabled\n", "tun: {enable: true, device: custom}\n", false, true},
		{"enabled", "enabled\n", "tun: {device: custom, dns-hijack: ['127.0.0.1:53']}\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			paths := nagiruntime.Paths{ConfigDir: root, StateDir: root}
			binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
			if err := profile.NewStore(root, func([]byte) error { return nil }, nil).Apply(context.Background(), "work", []byte(tc.source)); err != nil {
				t.Fatal(err)
			}
			if tc.intent != "" {
				if err := os.WriteFile(tunIntentPath(paths), []byte(tc.intent), 0600); err != nil {
					t.Fatal(err)
				}
			}
			store := newProfileStore(paths, binary, nil, nil)
			// Explicit subscription replacement disables saved selections, but must
			// still materialize the TUN preference before activation.
			path, err := materializeProfileWithSelections(store, root, "work", false)
			if err != nil {
				t.Fatal(err)
			}
			if (filepath.Base(path) == ".effective-work.yaml") != tc.overlay {
				t.Fatalf("unexpected path %s", path)
			}
			data, _ := os.ReadFile(path)
			var doc map[string]any
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			tun := doc["tun"].(map[string]any)
			if tun["enable"] != tc.enabled {
				t.Fatalf("tun=%v", tun)
			}
			if tc.overlay && tun["device"] != "custom" {
				t.Fatalf("lost device: %v", tun)
			}
			if tc.name == "enabled" {
				for _, key := range []string{"auto-route", "auto-detect-interface", "strict-route"} {
					if tun[key] != true {
						t.Fatalf("missing %s", key)
					}
				}
				if len(tun["dns-hijack"].([]any)) != 3 {
					t.Fatalf("hijack=%v", tun)
				}
			}
			source, _ := store.Show("work")
			if string(source) != tc.source {
				t.Fatalf("source changed: %s", source)
			}
		})
	}
}

func TestTUNPreferenceSurvivesActivation(t *testing.T) {
	oldOS := tunDNSOS
	tunDNSOS = "linux"
	defer func() { tunDNSOS = oldOS }()
	for _, operation := range []string{"reload", "explicit-subscription", "scheduled-subscription", "failed-adapter"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			paths := nagiruntime.Paths{ConfigDir: filepath.Join(root, "config"), StateDir: filepath.Join(root, "state"), RuntimeDir: root, SocketPath: filepath.Join(root, "mihomo.sock"), PIDPath: filepath.Join(root, "mihomo.pid"), LockPath: filepath.Join(root, "mihomo.lock"), LogPath: filepath.Join(root, "mihomo.log")}
			old := []byte("mixed-port: 17890\ntun: {device: test-tun}\nrules: [MATCH,DIRECT]\n")
			initial := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
			if err := initial.Apply(ctx, "work", old); err != nil {
				t.Fatal(err)
			}
			if err := writeTUNIntent(paths, true); err != nil {
				t.Fatal(err)
			}
			binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
			t.Setenv("NAGI_MIHOMO_BIN", binary)
			var mu sync.Mutex
			id := "old"
			tun := map[string]any{"enable": true, "auto-route": true, "auto-detect-interface": true, "strict-route": true, "dns-hijack": []any{"any:53", "tcp://any:53"}}
			puts, patches := 0, 0
			listener, err := net.Listen("unix", paths.SocketPath)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case "PUT":
					var request struct {
						Path string `json:"path"`
						ID   string `json:"activation_id"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					data, err := os.ReadFile(request.Path)
					if err != nil {
						t.Error(err)
					}
					var doc map[string]any
					if err := yaml.Unmarshal(data, &doc); err != nil {
						t.Error(err)
					}
					tun = doc["tun"].(map[string]any)
					puts++
					if tun["enable"] != true {
						t.Errorf("candidate disabled TUN: %s", data)
					}
					if operation == "failed-adapter" && puts == 1 {
						tun["enable"] = false
					}
					id = request.ID
					w.WriteHeader(204)
				case "PATCH":
					patches++
					var request map[string]any
					_ = json.NewDecoder(r.Body).Decode(&request)
					tun = request["tun"].(map[string]any)
					w.WriteHeader(204)
				default:
					_ = json.NewEncoder(w).Encode(map[string]any{"activation_id": id, "tun": tun, "proxies": map[string]any{}})
				}
			})}
			go server.Serve(listener)
			t.Cleanup(func() { _ = server.Close() })
			if operation == "failed-adapter" {
				// A pre-fix image can omit or disable TUN while a runtime patch
				// enabled it without changing the activation ID. Rollback must
				// preserve live TUN in its PUT, before any recovery PATCH.
				image, e := json.Marshal(runningImage{ActivationID: "old", Profile: "work", Effective: []byte("mixed-port: 17890\ntun: {enable: false, device: test-tun}\nrules: [MATCH,DIRECT]\n")})
				if e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(runningImagePath(paths.ConfigDir), image, 0600); e != nil {
					t.Fatal(e)
				}
			}
			client := control.New(paths.SocketPath)
			store := newProfileStore(paths, binary, client, &dnsStartupBackend{})
			candidate := []byte("mixed-port: 17900\nproxies: []\nproxy-groups: []\ntun: {device: test-tun}\nrules: [MATCH,DIRECT]\n")
			var actionErr error
			if operation == "scheduled-subscription" {
				feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(candidate) }))
				defer feed.Close()
				cache := subscription.NewStore(paths.ConfigDir, filepath.Join(root, "cache"), nil)
				if err := cache.Add("work", feed.URL); err != nil {
					t.Fatal(err)
				}
				if _, err := cache.Refresh(ctx, "work"); err != nil {
					t.Fatal(err)
				}
				if err := cache.RecordApplySuccess("work", "work", "prior-cache", old); err != nil {
					t.Fatal(err)
				}
				if err := cache.Schedule("work", 3600); err != nil {
					t.Fatal(err)
				}
				entry, err := cache.Find("work")
				if err != nil {
					t.Fatal(err)
				}
				actionErr = applyScheduledCache(ctx, paths, cache, store, entry)
			} else {
				_, actionErr = applyTransaction(ctx, paths, []string{"config", "reload"}, func() (any, error) {
					if operation == "explicit-subscription" {
						return nil, store.ApplySubscription(context.WithValue(ctx, skipSelectionRestoreKey{}, true), "work", candidate)
					}
					if operation == "failed-adapter" {
						return nil, store.Apply(ctx, "work", candidate)
					}
					return nil, store.Reload(ctx)
				})
			}
			mu.Lock()
			defer mu.Unlock()
			if operation == "failed-adapter" {
				if actionErr == nil || !strings.Contains(actionErr.Error(), "did not activate saved TUN") {
					t.Fatalf("missing failure: %v", actionErr)
				}
				source, _ := store.Show("work")
				if string(source) != string(old) {
					t.Fatalf("disk not restored: %s", source)
				}
				if puts != 2 || patches != 1 {
					t.Fatalf("recovery puts=%d patches=%d", puts, patches)
				}
			} else if actionErr != nil {
				t.Fatal(actionErr)
			} else if puts != 1 || patches != 0 {
				t.Fatalf("puts=%d patches=%d", puts, patches)
			}
			if tun["enable"] != true {
				t.Fatalf("runtime tun=%v", tun)
			}
			preference, err := readTUNIntent(paths)
			if err != nil || !preference {
				t.Fatalf("preference changed: %v %v", preference, err)
			}
		})
	}
}

func TestSavedTUNStartupFailureStopsEngineOnLinux(t *testing.T) {
	oldOS := tunDNSOS
	defer func() { tunDNSOS = oldOS }()
	tunDNSOS = "linux"
	paths := nagiruntime.Paths{StateDir: t.TempDir()}
	if err := writeTUNIntent(paths, true); err != nil {
		t.Fatal(err)
	}
	client := control.NewWithBridge(func(_ context.Context, method, path string, body, out any) error {
		return json.Unmarshal([]byte(`{"tun":{"enable":false}}`), out)
	}, nil)
	manager := &dnsStartupBackend{}
	_, err := startWithTUNDNS(context.Background(), paths, manager, client, "missing-test-config", "work")
	if err == nil || !manager.stopped {
		t.Fatalf("stopped=%v err=%v", manager.stopped, err)
	}
}

func TestDNSTUNConflictLeavesOverrideUnchanged(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "off", false: "on"}[enabled], func(t *testing.T) {
			root := t.TempDir()
			paths := isolatedTestPaths(t, root)
			if err := ensureDefaultProfile(filepath.Join(paths.ConfigDir, "profiles", "default.yaml"), "default"); err != nil {
				t.Fatal(err)
			}
			if err := writeTUNIntent(paths, enabled); err != nil {
				t.Fatal(err)
			}
			binary := writeFakeMihomo(t, root, "mihomo", "#!/bin/sh\nexit 0\n")
			t.Setenv("NAGI_MIHOMO_BIN", binary)
			override := filepath.Join(paths.ConfigDir, "overrides", "default.yaml")
			if err := os.MkdirAll(filepath.Dir(override), 0700); err != nil {
				t.Fatal(err)
			}
			before := []byte("mode: direct\n")
			if err := os.WriteFile(override, before, 0600); err != nil {
				t.Fatal(err)
			}
			var out, errout bytes.Buffer
			code := Run([]string{"--json", "dns", "tun", map[bool]string{true: "off", false: "on"}[enabled]}, &out, &errout, "test", "commit")
			if code != 1 || !strings.Contains(errout.String(), "conflicts with saved TUN preference") {
				t.Fatalf("code=%d err=%s", code, errout.String())
			}
			after, _ := os.ReadFile(override)
			if string(after) != string(before) {
				t.Fatalf("override changed: %s", after)
			}
		})
	}
}

func TestMalformedTUNPreferencePreventsActivation(t *testing.T) {
	root := t.TempDir()
	paths := nagiruntime.Paths{ConfigDir: root, StateDir: root}
	initial := profile.NewStore(root, func([]byte) error { return nil }, nil)
	source := []byte("rules: [MATCH,DIRECT]\n")
	if err := initial.Apply(context.Background(), "work", source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tunIntentPath(paths), []byte("invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := newProfileStore(paths, "unavailable-mihomo", nil, nil)
	if _, err := materializeProfile(store, root, "work"); err == nil || !strings.Contains(err.Error(), "invalid saved TUN intent") {
		t.Fatalf("err=%v", err)
	}
	after, _ := store.Show("work")
	if string(after) != string(source) {
		t.Fatalf("source changed: %s", after)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", ".effective-work.yaml")); !os.IsNotExist(err) {
		t.Fatalf("unexpected effective file: %v", err)
	}
}
