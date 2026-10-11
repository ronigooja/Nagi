package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/ronigooja/Nagi/internal/control"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/output"
)

func TestProviderAddPreflightAndHelpWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/nonexistent/mihomo")
	base := []string{"proxy", "provider", "add", "work", "https://example.com/feed?token=PRIVATE", "My Proxy"}
	for _, change := range []struct {
		index int
		value string
	}{{3, ""}, {3, "has spaces"}, {3, "COMPATIBLE"}, {4, "file:///PRIVATE"}, {4, "https://user:PRIVATE@example.com/feed"}, {4, "https://example.com/feed#PRIVATE"}, {4, "https://example.com/feed#"}, {5, ""}, {5, "bad\nname"}, {6, "0"}, {6, "31536001"}, {6, "bad"}} {
		args := append([]string(nil), base...)
		if change.index == 6 {
			args = append(args, change.value)
		} else {
			args[change.index] = change.value
		}
		var out, errs bytes.Buffer
		code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit")
		if code != 2 || !strings.Contains(errs.String(), `"code":"usage"`) || strings.Contains(errs.String(), "PRIVATE") || !strings.Contains(errs.String(), "proxy provider add NAME URL GROUP") {
			t.Fatalf("args=%v code=%d err=%s", args, code, errs.String())
		}
	}
	for _, args := range [][]string{append(append([]string(nil), base...), "1"), append(append([]string(nil), base...), "31536000"), base} {
		if err := validateInvocation(args); err != nil {
			t.Fatalf("valid input: %v", err)
		}
	}
	for _, args := range [][]string{{"proxy", "provider", "add", "--help"}, {"help", "proxy", "provider", "add"}, {"proxy", "provider", "--help"}} {
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "add") {
			t.Fatalf("help code=%d out=%s err=%s", code, out.String(), errs.String())
		}
	}
}
func TestProviderAddStoppedCLIUsesSelectedProfileAndPreservesFailure(t *testing.T) {
	root := t.TempDir()
	paths := isolatedTestPaths(t, root)
	profiles := filepath.Join(paths.ConfigDir, "profiles")
	if err := os.MkdirAll(profiles, 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte("mixed-port: 17890\nproxies: []\nproxy-groups: []\nrules: [MATCH,DIRECT]\n")
	if err := os.WriteFile(filepath.Join(profiles, "work.yaml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	validator := filepath.Join(root, "mihomo")
	if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", validator)
	args := []string{"--json", "proxy", "provider", "add", "new", "https://example.com/feed?token=PRIVATE", "My Proxy"}
	var out, errs bytes.Buffer
	if code := Run(args, &out, &errs, "test", "commit"); code != 0 {
		t.Fatalf("code=%d err=%s", code, errs.String())
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data["provider"] != "new" || envelope.Data["profile"] != "work" || envelope.Data["interval_seconds"] != float64(3600) || envelope.Data["group_created"] != true || len(envelope.Data) != 6 {
		t.Fatalf("result=%s", out.String())
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), "example.com") {
		t.Fatalf("URL leaked: %s", out.String())
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatalf("engine started: %v", err)
	}
	overridePath := filepath.Join(paths.ConfigDir, "overrides", "work.yaml")
	before, _ := os.ReadFile(overridePath)
	out.Reset()
	errs.Reset()
	if code := Run(args, &out, &errs, "test", "commit"); code != 1 || !strings.Contains(errs.String(), `"code":"proxy_provider_error"`) {
		t.Fatalf("duplicate code=%d err=%s", code, errs.String())
	}
	after, _ := os.ReadFile(overridePath)
	if string(before) != string(after) {
		t.Fatal("duplicate changed configuration")
	}
	if err := os.WriteFile(validator, []byte("#!/bin/sh\necho PRIVATE >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	args[4] = "second"
	out.Reset()
	errs.Reset()
	if code := Run(args, &out, &errs, "test", "commit"); code != 1 || strings.Contains(errs.String(), "PRIVATE") {
		t.Fatalf("validation code=%d err=%s", code, errs.String())
	}
	after, _ = os.ReadFile(overridePath)
	if string(before) != string(after) {
		t.Fatal("validation failure changed override")
	}
	after, _ = os.ReadFile(filepath.Join(profiles, "work.yaml"))
	if string(source) != string(after) {
		t.Fatal("source changed")
	}
}
func TestProviderAddTransactionClassificationAndHumanResult(t *testing.T) {
	args := []string{"proxy", "provider", "add", "work", "https://example.com/feed", "My Proxy"}
	if !configMutation(args) || !transactionalMutation(args) {
		t.Fatal("provider addition must lock and transact")
	}
	var out bytes.Buffer
	if err := output.Write(&out, false, map[string]any{"provider": "work", "group": "My Proxy", "profile": "default", "interval_seconds": 3600, "added": true, "group_created": false}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Saved proxy provider work") || strings.Contains(out.String(), "running") {
		t.Fatalf("human output=%s", out.String())
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, ok := completion(shell)
		if !ok || !strings.Contains(script, "add show update healthcheck") {
			t.Fatalf("missing add completion: %s", shell)
		}
	}
}

func TestProviderAddTransactionRestoresChangedRuntime(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(filepath.Join(config, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(config, "profiles", "default.yaml")
	source := []byte("mixed-port: 17890\nproxy-groups: []\nrules: [MATCH,DIRECT]\n")
	if err := os.WriteFile(sourcePath, source, 0600); err != nil {
		t.Fatal(err)
	}
	run, err := os.MkdirTemp("", "nagi-provider-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	socket := filepath.Join(run, "mihomo.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	activeID := "old"
	restored := false
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "PUT" {
			var request struct {
				Path         string `json:"path"`
				ActivationID string `json:"activation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			bytes, err := os.ReadFile(request.Path)
			if err != nil {
				t.Error(err)
				return
			}
			restored = string(bytes) == string(source)
			activeID = request.ActivationID
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"activation_id": activeID, "mixed-port": 17890})
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("NAGI_MIHOMO_BIN", writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n"))
	paths := nagiruntime.Paths{ConfigDir: config, RuntimeDir: run, StateDir: filepath.Join(run, "state"), SocketPath: socket}
	client := control.New(socket)
	store := newProfileStore(paths, os.Getenv("NAGI_MIHOMO_BIN"), client, nil)
	store.Reload = func(context.Context) error {
		mu.Lock()
		activeID = "settled-candidate"
		mu.Unlock()
		return errors.New("settled reload failed")
	}
	_, err = applyTransaction(context.Background(), paths, []string{"proxy", "provider", "add"}, func() (any, error) {
		_, err := store.AddProvider(context.Background(), "default", "work", "https://example.com/feed", "My Proxy", 3600)
		if err != nil {
			return nil, fail("proxy_provider_error", err)
		}
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "prior running configuration restored with a confirmed activation ID") {
		t.Fatalf("transaction failure=%v", err)
	}
	var ce *commandError
	if !errors.As(err, &ce) || ce.code != "proxy_provider_error" {
		t.Fatalf("error classification lost: %v", err)
	}
	mu.Lock()
	runtimeRestored := restored && activeID != "old" && activeID != "settled-candidate"
	mu.Unlock()
	if !runtimeRestored {
		t.Fatal("prior configuration not restored")
	}
	bytes, _ := os.ReadFile(sourcePath)
	if string(bytes) != string(source) {
		t.Fatal("source changed")
	}
	if _, err := store.Override("default"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed override remains: %v", err)
	}
	store.Reload = func(context.Context) error { return errActivationMayStillApply }
	_, err = applyTransaction(context.Background(), paths, []string{"proxy", "provider", "add"}, func() (any, error) {
		_, e := store.AddProvider(context.Background(), "default", "pending", "https://example.com/feed", "My Proxy", 3600)
		return nil, fail("proxy_provider_error", e)
	})
	if err == nil || !errors.Is(err, errActivationMayStillApply) || !strings.Contains(err.Error(), "runtime recovery incomplete") {
		t.Fatalf("wrapped activation uncertainty did not reach transaction recovery: %v", err)
	}
	if _, err := store.Override("default"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncertain failed override remains: %v", err)
	}
}
