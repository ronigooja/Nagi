package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/control"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestUnappliedSubscriptionHealthcheckOfflineCLI(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	t.Setenv("NAGI_MIHOMO_BIN", "/nonexistent/mihomo")
	subs := subscription.NewStore(paths.ConfigDir, filepath.Join(paths.DataDir, "cache", "subscriptions"), nil)
	if err := subs.Add("saved", "https://example.invalid/feed?secret=PRIVATE"); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "commit"); code != 0 {
			t.Fatalf("%v code=%d err=%s", args, code, errs.String())
		}
		if strings.Contains(out.String(), "PRIVATE") {
			t.Fatal("credential leak")
		}
		return out.String()
	}
	if out := run("--json", "proxy", "healthcheck", "show", "subscription", "saved"); !strings.Contains(out, `"interval_seconds":0`) || !strings.Contains(out, `"profile":""`) {
		t.Fatal(out)
	}
	if out := run("proxy", "healthcheck", "show", "subscription", "saved"); !strings.Contains(out, "follows subscription configuration") || strings.Contains(out, "0 seconds") {
		t.Fatal(out)
	}
	if out := run("--json", "proxy", "healthcheck", "set", "subscription", "saved", "45"); !strings.Contains(out, `"interval_seconds":45`) || !strings.Contains(out, `"source":"override"`) {
		t.Fatal(out)
	}
	if out := run("--json", "subscription", "list"); !strings.Contains(out, `"healthcheck_interval_seconds":45`) {
		t.Fatal(out)
	}
	if out := run("completion", "candidates", "healthcheck-targets", "subscription"); out != "saved\n" {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "profiles")); !os.IsNotExist(err) {
		t.Fatal("offline policy created profiles", err)
	}
	if _, err := os.Stat(paths.PIDPath); !os.IsNotExist(err) {
		t.Fatal("offline policy started engine", err)
	}
	run("proxy", "healthcheck", "reset", "subscription", "saved")
	if e, err := subs.Find("saved"); err != nil || e.HealthcheckIntervalSeconds != nil {
		t.Fatal(e, err)
	}
	for _, value := range []string{"off", "0", "-1", "31536001", "1.5"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"--json", "proxy", "healthcheck", "set", "subscription", "saved", value}, &out, &errs, "test", "commit"); code != 2 {
			t.Fatalf("%s %d %s", value, code, errs.String())
		}
	}
}

func TestSubscriptionHealthcheckInitialApplyAndRollback(t *testing.T) {
	ctx := context.Background()
	source := []byte("proxies: [{name: Local, type: direct}]\nproxy-groups: [{name: Auto, type: url-test, proxies: [DIRECT], interval: 180}]\nrules: ['MATCH,DIRECT']\n")
	paths, subs, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { w.Write(source) })
	interval := 45
	if err := subs.SetHealthcheckInterval("work", &interval); err != nil {
		t.Fatal(err)
	}
	if _, err := subs.Refresh(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	validatedPolicy := false
	profiles := profile.NewStore(paths.ConfigDir, func(data []byte) error {
		validatedPolicy = validatedPolicy || strings.Contains(string(data), "interval: 45")
		return nil
	}, nil)
	_, err := applyTransaction(ctx, paths, []string{"subscription", "apply", "work"}, func() (any, error) {
		return subscriptionCommand(ctx, []string{"subscription", "apply", "work"}, subs, profiles, nil)
	})
	if err != nil || !validatedPolicy {
		t.Fatal("saved policy was not validated", err)
	}
	if r, err := profiles.Healthcheck("work", "group", "Auto"); err != nil || r.IntervalSeconds != 45 || r.Source != "subscription" {
		t.Fatal(r, err)
	}
	before, _ := subs.Find("work")
	profiles.Validate = func([]byte) error { return errors.New("reject candidate") }
	interval = 90
	_, err = applyTransaction(ctx, paths, []string{"proxy", "healthcheck", "set", "subscription", "work", "90"}, func() (any, error) { return mutateSubscriptionHealthcheck(ctx, subs, profiles, "work", &interval) })
	after, _ := subs.Find("work")
	if err == nil || after.HealthcheckIntervalSeconds == nil || *after.HealthcheckIntervalSeconds != *before.HealthcheckIntervalSeconds {
		t.Fatal("failed policy mutation persisted", after, err)
	}
	// Inactive associated profiles validate but never reload the selected profile.
	profiles.Validate = func([]byte) error { return nil }
	profiles.Reload = func(context.Context) error { t.Fatal("inactive profile reloaded"); return nil }
	os.WriteFile(filepath.Join(paths.ConfigDir, "settings.yaml"), []byte("profile: other\n"), 0600)
	if _, err := mutateSubscriptionHealthcheck(ctx, subs, profiles, "work", &interval); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionHealthcheckRemovalAndImport(t *testing.T) {
	for _, operation := range []string{"remove", "import", "remove-failure", "import-failure"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			paths, subs, _ := schedulerFixture(t, func(http.ResponseWriter, *http.Request) { t.Fatal("policy operation fetched subscription") })
			source := []byte("proxy-groups: [{name: Auto, type: url-test, proxies: [DIRECT], interval: 180}]\n")
			profiles := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
			if err := profiles.Apply(ctx, "work", source); err != nil {
				t.Fatal(err)
			}
			if err := subs.RecordApplySuccess("work", "work", "cache", source); err != nil {
				t.Fatal(err)
			}
			interval := 45
			if err := subs.SetHealthcheckInterval("work", &interval); err != nil {
				t.Fatal(err)
			}
			reloaded := false
			profiles.Reload = func(context.Context) error {
				reloaded = true
				if strings.HasSuffix(operation, "failure") {
					return errors.New("activation failed")
				}
				result, err := profiles.Healthcheck("work", "group", "Auto")
				if err != nil || result.Source != "profile" || result.IntervalSeconds == 45 {
					t.Fatalf("old policy survived %s activation: %+v %v", operation, result, err)
				}
				return nil
			}
			imported := filepath.Join(t.TempDir(), "import.yaml")
			if err := os.WriteFile(imported, []byte("proxy-groups: [{name: Auto, type: url-test, proxies: [DIRECT], interval: 90}]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := applyTransaction(ctx, paths, []string{"subscription", "remove", "work"}, func() (any, error) {
				if strings.HasPrefix(operation, "remove") {
					return subscriptionCommand(ctx, []string{"subscription", "remove", "work"}, subs, profiles, nil)
				}
				return profileCommand(ctx, []string{"profile", "import", "work", imported}, profiles, subs)
			})
			if !reloaded {
				t.Fatal("selected profile was not reloaded")
			}
			if strings.HasSuffix(operation, "failure") {
				if err == nil {
					t.Fatal("failed activation accepted")
				}
				entry, err := subs.Find("work")
				if err != nil || entry.Profile != "work" || entry.HealthcheckIntervalSeconds == nil || *entry.HealthcheckIntervalSeconds != 45 {
					t.Fatal("failed activation lost policy or association", entry, err)
				}
				if result, err := profiles.Healthcheck("work", "group", "Auto"); err != nil || result.IntervalSeconds != 45 {
					t.Fatal(result, err)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if operation == "remove" {
				if _, err := subs.Find("work"); err == nil {
					t.Fatal("removed subscription remains")
				}
			} else if entry, err := subs.Find("work"); err != nil || entry.Profile != "" || entry.HealthcheckIntervalSeconds == nil {
				t.Fatal("import did not detach while retaining subscription intent", entry, err)
			}
		})
	}
}

func TestSubscriptionHealthcheckRunningActivationRecovery(t *testing.T) {
	ctx := context.Background()
	paths, subs, _ := schedulerFixture(t, func(http.ResponseWriter, *http.Request) { t.Fatal("mutation fetched feed") })
	profiles := profile.NewStore(paths.ConfigDir, func([]byte) error { return nil }, nil)
	source := []byte("mixed-port: 17890\nproxy-groups: [{name: Auto, type: url-test, proxies: [DIRECT], interval: 180}]\n")
	if err := profiles.Apply(ctx, "work", source); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("work", "work", "cache", source); err != nil {
		t.Fatal(err)
	}
	interval := 45
	if err := subs.SetHealthcheckInterval("work", &interval); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.RuntimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	id := "before"
	var activated [][]byte
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/configs" {
			t.Errorf("unexpected API call %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		switch r.Method {
		case "GET":
			json.NewEncoder(w).Encode(map[string]any{"activation_id": id, "mixed-port": 17890})
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
			activated = append(activated, data)
			id = request.ID
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected API method %s", r.Method)
			http.Error(w, "unexpected", 500)
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	binary := writeFakeMihomo(t, t.TempDir(), "mihomo", "#!/bin/sh\nexit 0\n")
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	profiles.Reload = func(context.Context) error {
		data, err := profiles.Effective("work")
		if err != nil {
			return err
		}
		path := filepath.Join(paths.ConfigDir, "profiles", ".effective-work.yaml")
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
		if err := activateConfig(ctx, control.New(paths.SocketPath), path); err != nil {
			return err
		}
		return errors.New("failure after activation")
	}
	interval = 90
	_, err = applyTransaction(ctx, paths, []string{"proxy", "healthcheck", "set", "subscription", "work", "90"}, func() (any, error) { return mutateSubscriptionHealthcheck(ctx, subs, profiles, "work", &interval) })
	if err == nil || strings.Contains(err.Error(), "uncertain") || strings.Contains(err.Error(), "unavailable") {
		t.Fatal("runtime rollback did not confirm", err)
	}
	entry, err := subs.Find("work")
	if err != nil || entry.HealthcheckIntervalSeconds == nil || *entry.HealthcheckIntervalSeconds != 45 {
		t.Fatal("policy not restored", entry, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(activated) != 2 || !strings.Contains(string(activated[0]), "interval: 90") || !strings.Contains(string(activated[1]), "interval: 45") {
		t.Fatalf("native activation/recovery did not restore previous interval: %q", activated)
	}
}
