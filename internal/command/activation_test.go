package command

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/rules"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestActivationConfirmsTimeoutAfterApply(t *testing.T) {
	var id string
	client := control.NewWithBridge(func(_ context.Context, method, path string, body, out any) error {
		if method == "PUT" {
			id = body.(map[string]any)["activation_id"].(string)
			return context.DeadlineExceeded
		}
		out.(*struct {
			ActivationID string `json:"activation_id"`
		}).ActivationID = id
		return nil
	}, nil)
	if err := activateConfig(context.Background(), client, "/tmp/effective.yaml"); err != nil {
		t.Fatal(err)
	}
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		t.Fatalf("invalid activation ID %q", id)
	}
}

func TestActivationWaitsForInProgressApply(t *testing.T) {
	var mu sync.Mutex
	id := "old"
	var pending string
	client := control.NewWithBridge(func(_ context.Context, method, path string, body, out any) error {
		mu.Lock()
		defer mu.Unlock()
		if method == "PUT" {
			pending = body.(map[string]any)["activation_id"].(string)
			return context.DeadlineExceeded
		}
		out.(*struct {
			ActivationID string `json:"activation_id"`
		}).ActivationID = id
		return nil
	}, nil)
	go func() { time.Sleep(300 * time.Millisecond); mu.Lock(); id = pending; mu.Unlock() }()
	if err := activateConfig(context.Background(), client, "/tmp/effective.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestFailedMutationRestoresFilesButDoesNotClaimInvalidPriorRuntime(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	runtimeDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte("invalid: [\n")
	profilePath := filepath.Join(config, "profiles", "default.yaml")
	if err := os.WriteFile(profilePath, old, 0600); err != nil {
		t.Fatal(err)
	}
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: runtimeDir, SocketPath: filepath.Join(runtimeDir, "mihomo.sock")}
	// No running API is present. File restoration still happens without a
	// success claim about the previous in-memory configuration.
	_, err := applyTransaction(context.Background(), paths, []string{"profile", "import"}, func() (any, error) {
		if e := os.WriteFile(profilePath, []byte("mixed-port: 17900\n"), 0600); e != nil {
			return nil, e
		}
		return nil, errors.New("activation failed")
	})
	if err == nil || !strings.Contains(err.Error(), "activation failed") {
		t.Fatalf("error: %v", err)
	}
	got, readErr := os.ReadFile(profilePath)
	if readErr != nil || string(got) != string(old) {
		t.Fatalf("restored file: %q %v", got, readErr)
	}
}

func TestConfigLockSerializesMutations(t *testing.T) {
	dir := t.TempDir()
	paths := nagiruntime.Paths{RuntimeDir: dir, SocketPath: filepath.Join(dir, "socket"), PIDPath: filepath.Join(dir, "pid"), LockPath: filepath.Join(dir, "engine.lock"), LogPath: filepath.Join(dir, "logs", "mihomo.log")}
	var inside, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := withNamedLock(paths, "config.lock", func() (any, error) {
				n := inside.Add(1)
				if n > peak.Load() {
					peak.Store(n)
				}
				time.Sleep(5 * time.Millisecond)
				inside.Add(-1)
				return nil, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Fatalf("concurrent mutations entered critical section: %d", got)
	}
}

func TestProxyMutationsUseConfigurationLock(t *testing.T) {
	for _, tc := range []struct {
		args []string
		locked bool
	}{
		{[]string{"proxy", "select", "Auto", "Node"}, true},
		{[]string{"proxy", "auto", "Auto"}, true},
		{[]string{"proxy", "delays", "Auto"}, true},
		{[]string{"proxy", "restore"}, true},
		{[]string{"proxy", "provider", "update", "work"}, true},
		{[]string{"proxy", "provider", "healthcheck", "work"}, true},
		{[]string{"proxy", "provider", "show", "work"}, false},
		{[]string{"proxy", "groups"}, false},
		{[]string{"proxy", "delay", "Node"}, false},
		{[]string{"traffic", "watch"}, false},
	} {
		if got := configMutation(tc.args); got != tc.locked {
			t.Errorf("configMutation(%v) = %v, want %v", tc.args, got, tc.locked)
		}
	}
}

func TestMaterializeRulesWithoutOverride(t *testing.T) {
	store, dir := validationStore(t, writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n"))
	source := []byte("mixed-port: 17890\nrules:\n  - MATCH,DIRECT\n")
	if err := profile.NewStore(dir, func([]byte) error { return nil }, nil).Apply(context.Background(), "work", source); err != nil {
		t.Fatal(err)
	}
	if err := rules.New(dir, nil).Add(context.Background(), rules.Entry{Name: "local", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "DIRECT"}); err != nil {
		t.Fatal(err)
	}
	path, err := materializeProfile(store, dir, "work")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "DOMAIN,example.com,DIRECT") {
		t.Fatalf("effective rules: %q %v", data, err)
	}
}

func TestChangedRuntimeWithInvalidOldConfigIsNotRestored(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	run := filepath.Join(dir, "run")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run, 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte("invalid: [\n")
	source := filepath.Join(config, "profiles", "default.yaml")
	if err := os.WriteFile(source, old, 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(run, "mihomo.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	id := "old"
	puts := 0
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "PUT" {
			puts++
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"activation_id": id, "mixed-port": 17890})
	})}
	go server.Serve(listener)
	defer server.Close()
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: run, SocketPath: socket}
	_, err = applyTransaction(context.Background(), paths, []string{"profile", "import"}, func() (any, error) {
		if e := os.WriteFile(source, []byte("mixed-port: 17900\n"), 0600); e != nil {
			return nil, e
		}
		mu.Lock()
		id = "new"
		mu.Unlock()
		return nil, errors.New("failed after runtime activation")
	})
	if err == nil || !strings.Contains(err.Error(), "runtime restoration unavailable") {
		t.Fatalf("missing actual state: %v", err)
	}
	mu.Lock()
	count := puts
	mu.Unlock()
	if count != 0 {
		t.Fatalf("attempted to restore invalid old content: %d PUTs", count)
	}
	got, e := os.ReadFile(source)
	if e != nil || string(got) != string(old) {
		t.Fatalf("files not restored: %q %v", got, e)
	}
}

func TestRollbackFailureReportsUnconfirmedRuntime(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	run := filepath.Join(dir, "run")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	old := []byte("mixed-port: 17890\nrules: [MATCH,DIRECT]\n")
	if err := os.WriteFile(source, old, 0600); err != nil {
		t.Fatal(err)
	}
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	socket := filepath.Join(run, "mihomo.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	id := "old"
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "PUT" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"message":"unable to restore"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"activation_id": id, "mixed-port": 17900})
	})}
	go server.Serve(listener)
	defer server.Close()
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: run, SocketPath: socket}
	_, err = applyTransaction(context.Background(), paths, []string{"profile", "import"}, func() (any, error) {
		if e := os.WriteFile(source, []byte("mixed-port: 17900\n"), 0600); e != nil {
			return nil, e
		}
		mu.Lock()
		id = "changed"
		mu.Unlock()
		return nil, errors.New("candidate activation failed")
	})
	if err == nil || !strings.Contains(err.Error(), "runtime restoration failed") || !strings.Contains(err.Error(), "current state is uncertain") {
		t.Fatalf("rollback failure: %v", err)
	}
}

func TestLateCandidateAfterConfirmedRollbackReportsUncertainRuntime(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	run := filepath.Join(dir, "run")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	override := filepath.Join(config, "overrides", "default.yaml")
	choices := filepath.Join(config, "proxy-selections.json")
	if err := os.MkdirAll(filepath.Dir(override), 0700); err != nil {
		t.Fatal(err)
	}
	prior := map[string]string{
		source:   "mixed-port: 17890\nrules: [MATCH,DIRECT]\n",
		override: "mode: rule\n",
		choices:  `{"default":{"group":"Manual","node":"A"}}`,
	}
	for path, data := range prior {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	socket := filepath.Join(run, "mihomo.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	activeID := "old"
	var candidateID, rollbackID string
	puts := 0
	lateApply := make(chan struct{})
	lateDone := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			var request struct {
				ActivationID string `json:"activation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode PUT: %v", err)
				return
			}
			mu.Lock()
			puts++
			if puts == 1 {
				activeID = request.ActivationID
				mu.Unlock()
			} else if puts == 2 {
				candidateID = request.ActivationID
				mu.Unlock()
				<-lateApply
				mu.Lock()
				activeID = candidateID
				mu.Unlock()
				close(lateDone)
			} else {
				rollbackID = request.ActivationID
				activeID = rollbackID
				mu.Unlock()
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mu.Lock()
		id := activeID
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"activation_id": id, "mixed-port": 17890})
	})}
	go server.Serve(listener)
	defer server.Close()
	defer func() {
		select {
		case <-lateDone:
		default:
			close(lateApply)
			<-lateDone
		}
	}()
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: run, SocketPath: socket}
	_, err = applyTransaction(context.Background(), paths, []string{"profile", "import"}, func() (any, error) {
		if e := os.WriteFile(source, []byte("mixed-port: 17900\n"), 0600); e != nil {
			return nil, e
		}
		if e := os.WriteFile(override, []byte("mode: direct\n"), 0600); e != nil {
			return nil, e
		}
		if e := os.Remove(choices); e != nil {
			return nil, e
		}
		client := control.NewWithTimeout(socket, 40*time.Millisecond)
		if e := client.Put(context.Background(), "/configs?force=true", map[string]any{"path": source, "activation_id": strings.Repeat("a", 32)}, nil); e != nil {
			return nil, e
		}
		return nil, activateConfig(context.Background(), client, source)
	})
	if err == nil || !errors.Is(err, errActivationMayStillApply) || !strings.Contains(err.Error(), "runtime recovery incomplete") {
		t.Fatalf("expected outstanding apply and incomplete recovery: %v", err)
	}
	mu.Lock()
	confirmedRollback := rollbackID != "" && activeID == rollbackID
	confirmedID := rollbackID
	mu.Unlock()
	if !confirmedRollback || !strings.Contains(err.Error(), confirmedID) {
		t.Fatalf("rollback observation not reported: %v, rollback %q", err, confirmedID)
	}
	for path, want := range prior {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("disk rollback %s: %q %v", path, got, readErr)
		}
	}
	var jsonError strings.Builder
	output.WriteError(&jsonError, true, "profile_error", err.Error())
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if e := json.Unmarshal([]byte(jsonError.String()), &envelope); e != nil || envelope.OK || envelope.Error.Code != "profile_error" || !strings.Contains(envelope.Error.Message, "runtime recovery incomplete") {
		t.Fatalf("JSON failure omits recovery status: %q, %v", jsonError.String(), e)
	}
	close(lateApply)
	<-lateDone
	mu.Lock()
	lateID := activeID
	mu.Unlock()
	if lateID != candidateID || lateID == confirmedID {
		t.Fatalf("original PUT did not overwrite confirmed rollback: candidate %q, active %q, rollback %q", candidateID, lateID, confirmedID)
	}
}

func TestSettledApplyFailureConfirmsRuntimeRestoration(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	run := filepath.Join(dir, "run")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config, "profiles", "default.yaml")
	old := []byte("mixed-port: 17890\nrules: [MATCH,DIRECT]\n")
	if err := os.WriteFile(source, old, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n"))
	socket := filepath.Join(run, "mihomo.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	activeID := "old"
	rollbackID := ""
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "PUT" {
			var request struct {
				ActivationID string `json:"activation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode PUT: %v", err)
				return
			}
			rollbackID = request.ActivationID
			activeID = rollbackID
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"activation_id": activeID, "mixed-port": 17890})
	})}
	go server.Serve(listener)
	defer server.Close()
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: run, SocketPath: socket}
	_, err = applyTransaction(context.Background(), paths, []string{"profile", "import"}, func() (any, error) {
		if e := os.WriteFile(source, []byte("mixed-port: 17900\n"), 0600); e != nil {
			return nil, e
		}
		mu.Lock()
		activeID = "settled-candidate"
		mu.Unlock()
		return nil, errors.New("known settled failure")
	})
	if err == nil || !strings.Contains(err.Error(), "prior running configuration restored with a confirmed activation ID") || strings.Contains(err.Error(), "runtime recovery incomplete") {
		t.Fatalf("settled recovery: %v", err)
	}
	mu.Lock()
	restored := rollbackID != "" && activeID == rollbackID
	mu.Unlock()
	if !restored {
		t.Fatal("prior runtime was not reactivated")
	}
	if got, e := os.ReadFile(source); e != nil || string(got) != string(old) {
		t.Fatalf("source after rollback: %q, %v", got, e)
	}
}

func TestPreservingSubscriptionApplyKeepsOverrideSelectionsAndRules(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	run := filepath.Join(dir, "run")
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: run, StateDir: filepath.Join(dir, "state"), SocketPath: filepath.Join(run, "mihomo.sock"), PIDPath: filepath.Join(run, "mihomo.pid"), LockPath: filepath.Join(run, "engine.lock"), LogPath: filepath.Join(run, "logs", "mihomo.log")}
	old := []byte("mixed-port: 17890\nrules:\n  - MATCH,DIRECT\n")
	profiles := profile.NewStore(config, func([]byte) error { return nil }, nil)
	if err := profiles.Apply(ctx, "work", old); err != nil {
		t.Fatal(err)
	}
	if err := profiles.SetOverride(ctx, "work", []byte("mode: direct\n")); err != nil {
		t.Fatal(err)
	}
	if err := rules.New(config, nil).Add(ctx, rules.Entry{Name: "local", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.com", Target: "DIRECT"}); err != nil {
		t.Fatal(err)
	}
	choices := proxy.SelectionStore{Path: filepath.Join(config, "proxy-selections.json")}
	if err := choices.Save("work", "Manual", "A"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mixed-port: 17900\nproxies:\n  - name: A\n    type: direct\nproxy-groups:\n  - name: Manual\n    type: select\n    proxies: [A, DIRECT]\nrules:\n  - MATCH,Manual\n"))
	}))
	defer server.Close()
	cache := subscription.NewStore(config, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Refresh(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyCachedSubscriptionPreservingSource(ctx, paths, cache, profiles, "work"); err != nil {
		t.Fatal(err)
	}
	source, e := profiles.Show("work")
	if e != nil || !strings.Contains(string(source), "17900") {
		t.Fatalf("source: %s %v", source, e)
	}
	override, e := profiles.Override("work")
	if e != nil || string(override) != "mode: direct\n" {
		t.Fatalf("override: %q %v", override, e)
	}
	if name, e := profiles.Current(); e != nil || name != "work" {
		t.Fatalf("selection: %s %v", name, e)
	}
	if data, e := os.ReadFile(choices.Path); e != nil || !strings.Contains(string(data), "Manual") {
		t.Fatalf("choices: %q %v", data, e)
	}
	if doc, e := rules.New(config, nil).List(); e != nil || len(doc.Entries) != 1 {
		t.Fatalf("rules: %+v %v", doc, e)
	}
}
