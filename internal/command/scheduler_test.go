package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
)

const schedulerTestYAML = "mixed-port: 17890\nproxies:\n  - name: node-a\n    type: ss\n    server: 127.0.0.1\n    port: 8388\n    cipher: aes-128-gcm\n    password: example\nproxy-groups:\n  - name: SELECT\n    type: select\n    proxies: [node-a, DIRECT]\nrules:\n  - MATCH,SELECT\n"

func schedulerFixture(t *testing.T, handler http.HandlerFunc) (nagiruntime.Paths, *subscription.Store, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	p := nagiruntime.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), RuntimeDir: filepath.Join(root, "run")}
	p.SocketPath = filepath.Join(p.RuntimeDir, "s.sock")
	p.PIDPath = filepath.Join(p.RuntimeDir, "pid")
	p.LockPath = filepath.Join(p.RuntimeDir, "lock")
	p.LogPath = filepath.Join(p.RuntimeDir, "logs", "mihomo.log")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s := subscription.NewStore(p.ConfigDir, filepath.Join(p.DataDir, "cache", "subscriptions"), server.Client())
	s.CommitUnderConfigLock = func(fn func() error) error {
		_, err := withNamedLock(p, "config.lock", func() (any, error) { return nil, fn() })
		return err
	}
	if err := s.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	return p, s, server
}
func dueNow(t *testing.T, s *subscription.Store) {
	t.Helper()
	if err := s.RecordSuccess("work", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
}
func TestSchedulerHostSingletonAndRestart(t *testing.T) {
	p, _, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	h, err := acquireScheduler(p)
	if err != nil {
		t.Fatal(err)
	}
	if !schedulerRunning(p) {
		t.Fatal("live lock not reported")
	}
	stale, _ := json.Marshal(schedulerHeartbeat{PID: os.Getpid(), At: time.Now().Add(-time.Minute)})
	if err := os.WriteFile(schedulerHeartbeatPath(p), stale, 0600); err != nil {
		t.Fatal(err)
	}
	if schedulerRunning(p) {
		t.Fatal("stale heartbeat reported running")
	}
	if err := h.writeHeartbeat(); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireScheduler(p); err == nil {
		t.Fatal("duplicate host acquired")
	}
	h.Close()
	h.Close()
	if schedulerRunning(p) {
		t.Fatal("closed host reported live")
	}
	h, err = acquireScheduler(p)
	if err != nil {
		t.Fatal("restart:", err)
	}
	h.Close()
}
func TestSchedulerCachesOtherAndAppliesAssociated(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	dueNow(t, s)
	var applied atomic.Int32
	if err := schedulerStep(context.Background(), p, s, func(context.Context, subscription.Entry) error { applied.Add(1); return nil }, time.Now()); err != nil {
		t.Fatal(err)
	}
	e, err := s.Find("work")
	if err != nil {
		t.Fatal(err)
	}
	if e.CacheVersion == "" || applied.Load() != 0 {
		t.Fatalf("cache-only subscription: %+v, applies %d", e, applied.Load())
	}
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(schedulerTestYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", e.CacheVersion, []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule("work", 1); err != nil {
		t.Fatal(err)
	}
	dueNow(t, s)
	// New bytes leave a pending version for the associated profile.
	s.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(schedulerTestYAML + "\n# next")), Header: make(http.Header)}, nil
	})}
	if err := schedulerStep(context.Background(), p, s, func(context.Context, subscription.Entry) error { applied.Add(1); return nil }, time.Now()); err != nil {
		t.Fatal(err)
	}
	if applied.Load() != 1 {
		t.Fatalf("expected one associated apply, got %d", applied.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSchedulerPausesChangedSource(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", e.CacheVersion, []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	dueNow(t, s)
	if err := schedulerStep(context.Background(), p, s, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	e, _ = s.Find("work")
	if e.LastFailureStage != "paused" || e.LastFailureReason != "profile_changed" {
		t.Fatalf("pause: %+v", e)
	}
	if due, err := s.Due(time.Now().Add(time.Hour)); err != nil || len(due) != 1 {
		t.Fatalf("paused subscription must remain due for download: %v %v", due, err)
	}
	if err := s.RecordSuccess("work", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	e, _ = s.Find("work")
	if e.LastFailureStage != "paused" {
		t.Fatal("successful download cleared persistent pause")
	}
}
func TestSchedulerOffDuringFetch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(schedulerTestYAML))
	})
	dueNow(t, s)
	done := make(chan error, 1)
	go func() { done <- schedulerStep(context.Background(), p, s, nil, time.Now()) }()
	<-started
	if err := s.Schedule("work", 0); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	if e.ScheduleSeconds != 0 || !e.NextDue.IsZero() {
		t.Fatalf("off overwritten: %+v", e)
	}
}
func TestScheduledApplyChecksCurrentProfileAndSource(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(schedulerTestYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "older", []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	e, _ = s.Find("work")
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, nil)
	if err := applyScheduledCache(context.Background(), p, s, profiles, e); err != nil {
		t.Fatal(err)
	}
	e2, _ := s.Find("work")
	if e2.AppliedCacheVersion != "older" {
		t.Fatal("applied noncurrent profile")
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyScheduledCache(context.Background(), p, s, profiles, e); !errors.Is(err, errSourceChanged) {
		t.Fatalf("source conflict: %v", err)
	}
}

func TestSchedulerRetriesPendingCacheAfterFetchFailure(t *testing.T) {
	var fail atomic.Bool
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(schedulerTestYAML))
	})
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(schedulerTestYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "previous-version", []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	dueNow(t, s)
	fail.Store(true)
	var applied atomic.Int32
	if err := schedulerStep(context.Background(), p, s, func(context.Context, subscription.Entry) error { applied.Add(1); return nil }, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Find("work")
	if applied.Load() != 1 || after.CacheVersion != e.CacheVersion || after.LastFailureReason != "network_error" {
		t.Fatalf("pending cache retry: %+v applies %d", after, applied.Load())
	}
}

func TestScheduledConflictPreservesSourceAndCache(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(schedulerTestYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "previous-version", []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	e, _ = s.Find("work")
	rule := []byte("entries:\n  - name: bad\n    enabled: true\n    kind: rule\n    type: DOMAIN\n    payload: example.com\n    target: removed-group\n")
	if err := os.MkdirAll(filepath.Join(p.ConfigDir, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules.Path(p.ConfigDir, "work"), rule, 0600); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, nil)
	err := applyScheduledCache(context.Background(), p, s, profiles, e)
	if !errors.Is(err, errRuleConflict) {
		t.Fatalf("rule conflict: %v", err)
	}
	source, readErr := os.ReadFile(path)
	if readErr != nil || string(source) != schedulerTestYAML {
		t.Fatal("source changed after conflict", readErr)
	}
	after, _ := s.Find("work")
	if after.AppliedCacheVersion != "previous-version" || after.CacheVersion != e.CacheVersion {
		t.Fatal("cache/applied version changed after conflict")
	}
}

func TestScheduledDNSConflictKeepsOldSourceAndOverride(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte(schedulerTestYAML + "\n# old source\n")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	override := []byte("dns:\n  enable: true\n  nameserver:\n    - https://1.1.1.1/dns-query#removed-node\n")
	overridePath := filepath.Join(p.ConfigDir, "overrides", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(overridePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, override, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "previous-version", old); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	var profiles *profile.Store
	profiles = profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, func(context.Context) error { _, err := materializeProfile(profiles, p.ConfigDir, "work"); return err })
	err := applyScheduledCache(context.Background(), p, s, profiles, e)
	if !errors.Is(err, errDNSConflict) {
		t.Fatalf("DNS conflict: %v", err)
	}
	source, _ := os.ReadFile(path)
	savedOverride, _ := os.ReadFile(overridePath)
	after, _ := s.Find("work")
	if string(source) != string(old) || string(savedOverride) != string(override) || after.AppliedCacheVersion != "previous-version" {
		t.Fatal("failed DNS apply changed source, override or applied version")
	}
}

func TestSchedulerWakesForShortInterval(t *testing.T) {
	_, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if err := s.Schedule("work", 1); err != nil {
		t.Fatal(err)
	}
	delay := schedulerDelay(s, time.Now())
	if delay < time.Second || delay > 1500*time.Millisecond {
		t.Fatalf("one-second schedule delayed %s", delay)
	}
}

func TestSchedulerSkipsProfileChangedDuringFetch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(schedulerTestYAML))
	})
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(schedulerTestYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "previous-version", []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dueNow(t, s)
	var applied atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- schedulerStep(context.Background(), p, s, func(ctx context.Context, e subscription.Entry) error {
			profiles := profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, nil)
			err := applyScheduledCache(ctx, p, s, profiles, e)
			if err == nil {
				applied.Add(1)
			}
			return err
		}, time.Now())
	}()
	<-started
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	if applied.Load() != 1 || e.AppliedCacheVersion != "previous-version" {
		t.Fatalf("changed selection activated: %+v", e)
	}
}

func TestRefreshRejectsKeyChangedDuringDownload(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	_, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(schedulerTestYAML))
	})
	identity := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(identity, []byte("identity-one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey("work", identity); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Refresh(context.Background(), "work"); done <- err }()
	<-started
	if err := os.WriteFile(identity, []byte("identity-two"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey("work", identity); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale key result committed")
	}
	e, _ := s.Find("work")
	if e.CacheVersion != "" {
		t.Fatalf("stale cache written: %+v", e)
	}
}

func TestSchedulerPausesUnreadableSource(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if err := s.RecordApplySuccess("work", "work", "previous-version", []byte(schedulerTestYAML)); err != nil {
		t.Fatal(err)
	}
	dueNow(t, s)
	if err := schedulerStep(context.Background(), p, s, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	if e.LastFailureStage != "paused" || e.LastFailureReason != "source_unreadable" {
		t.Fatalf("unreadable source did not pause: %+v", e)
	}
}

func TestScheduledApplyUsesEffectiveOverrideForRulesAndSelections(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	old := []byte(schedulerTestYAML + "\n# prior source\n")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	override := []byte("proxies:\n  - name: local-node\n    type: ss\n    server: 127.0.0.1\n    port: 8388\n    cipher: aes-128-gcm\n    password: example\nproxy-groups:\n  - name: LocalGroup\n    type: select\n    proxies: [local-node]\n")
	overridePath := filepath.Join(p.ConfigDir, "overrides", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(overridePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, override, 0600); err != nil {
		t.Fatal(err)
	}
	rule := []byte("entries:\n  - name: local-rule\n    enabled: true\n    kind: rule\n    type: DOMAIN\n    payload: example.com\n    target: LocalGroup\n")
	if err := os.MkdirAll(filepath.Join(p.ConfigDir, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules.Path(p.ConfigDir, "work"), rule, 0600); err != nil {
		t.Fatal(err)
	}
	selections := filepath.Join(p.ConfigDir, "proxy-selections.json")
	if err := os.WriteFile(selections, []byte(`{"work":{"LocalGroup":"local-node","Gone":"node-z"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "prior-cache", old); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	profiles := profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, nil)
	if err := applyScheduledCache(context.Background(), p, s, profiles, e); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(selections)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "local-node") || strings.Contains(string(got), "Gone") {
		t.Fatalf("choices after prune: %s", got)
	}
	if gotOverride, _ := os.ReadFile(overridePath); string(gotOverride) != string(override) {
		t.Fatal("override changed")
	}
}

func TestScheduledApplyUnknownRuleTargetPreservesOldConfig(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	old := []byte(schedulerTestYAML + "\n# prior source\n")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rule := []byte("entries:\n  - name: bad\n    enabled: true\n    kind: rule\n    type: DOMAIN\n    payload: example.com\n    target: removed\n")
	if err := os.MkdirAll(filepath.Join(p.ConfigDir, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules.Path(p.ConfigDir, "work"), rule, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "prior-cache", old); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Find("work")
	profiles := profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, nil)
	if err := applyScheduledCache(context.Background(), p, s, profiles, e); !errors.Is(err, errRuleConflict) {
		t.Fatalf("target conflict: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(old) {
		t.Fatal("old source changed after conflict")
	}
}

func TestSchedulerPauseSurvivesFetchFailureAndSourceRestoration(t *testing.T) {
	var fail atomic.Bool
	var downloads atomic.Int32
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(schedulerTestYAML))
	})
	old := []byte(schedulerTestYAML)
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "old-cache", old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	dueNow(t, s)
	var applies atomic.Int32
	apply := func(context.Context, subscription.Entry) error { applies.Add(1); return nil }
	if err := schedulerStep(context.Background(), p, s, apply, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Find("work")
	if after.PauseReason != "profile_changed" || after.CacheVersion == "" || !after.NextDue.After(time.Now()) || downloads.Load() != 1 || applies.Load() != 0 {
		t.Fatalf("paused refresh: %+v, downloads %d applies %d", after, downloads.Load(), applies.Load())
	}
	fail.Store(true)
	if err := s.RecordDownloadSuccess("work", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := schedulerStep(context.Background(), p, s, apply, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, _ = s.Find("work")
	if after.PauseReason != "profile_changed" || after.LastFailureStage != "fetch" || after.LastFailureReason != "network_error" || !after.NextDue.After(time.Now()) || downloads.Load() != 2 || applies.Load() != 0 {
		t.Fatalf("failure while paused: %+v, downloads %d applies %d", after, downloads.Load(), applies.Load())
	}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	fail.Store(false)
	if err := s.RecordDownloadSuccess("work", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := schedulerStep(context.Background(), p, s, apply, time.Now()); err != nil {
		t.Fatal(err)
	}
	after, _ = s.Find("work")
	if after.PauseReason != "profile_changed" || after.LastFailureStage != "paused" || downloads.Load() != 3 || applies.Load() != 0 {
		t.Fatalf("restored bytes resumed apply: %+v, downloads %d applies %d", after, downloads.Load(), applies.Load())
	}
	if err := s.RecordApplySuccess("work", "work", after.CacheVersion, old); err != nil {
		t.Fatal(err)
	}
	after, _ = s.Find("work")
	if after.PauseReason != "" || after.LastFailureStage != "" {
		t.Fatalf("explicit apply failed to resume: %+v", after)
	}
}

func TestSchedulerMetadataCASRejectsSameIntervalRescheduleAndReplacement(t *testing.T) {
	p, s, server := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	old, _ := s.Find("work")
	if err := s.Schedule("work", old.ScheduleSeconds); err != nil {
		t.Fatal(err)
	}
	if err := recordSchedulerFailure(p, s, old, "fetch", "network_error", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Find("work")
	if !current.NextDue.After(time.Now()) || current.LastFailureStage != "" {
		t.Fatalf("stale reschedule overwritten: %+v", current)
	}
	if err := s.Remove("work"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if err := recordSchedulerFailure(p, s, old, "fetch", "network_error", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Find("work")
	if current.Generation == old.Generation || current.LastFailureStage != "" {
		t.Fatalf("replacement overwritten: %+v", current)
	}
}

func TestRuleTargetKnownInlineProviderAndUnknownRemoteProvider(t *testing.T) {
	dir := t.TempDir()
	rule := []byte("entries:\n  - name: provider-node-rule\n    enabled: true\n    kind: rule\n    type: DOMAIN\n    payload: example.com\n    target: provider-node\n")
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules.Path(dir, "work"), rule, 0600); err != nil {
		t.Fatal(err)
	}
	inline := []byte("proxy-providers:\n  local:\n    type: inline\n    payload:\n      - name: provider-node\n        type: direct\n")
	if err := validateRuleTargets(dir, "work", inline); err != nil {
		t.Fatal(err)
	}
	remote := []byte("proxy-providers:\n  remote:\n    type: http\n    url: https://example.invalid/nodes.yaml\n    path: ./remote.yaml\n")
	if err := validateRuleTargets(dir, "work", remote); err != nil {
		t.Fatalf("remote provider target cannot be ruled out statically: %v", err)
	}
}

func TestScheduledApplyBuildsBackendAfterConfigLock(t *testing.T) {
	p, s, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(schedulerTestYAML)) })
	if _, err := s.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	old := []byte(schedulerTestYAML + "\n# prior\n")
	path := filepath.Join(p.ConfigDir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplySuccess("work", "work", "prior-cache", old); err != nil {
		t.Fatal(err)
	}
	entry, _ := s.Find("work")
	var seen string
	ready := make(chan struct{})
	done := make(chan error, 1)
	_, err := withNamedLock(p, "config.lock", func() (any, error) {
		go func() {
			close(ready)
			done <- applyScheduledCacheFactory(context.Background(), p, s, entry, func() (*profile.Store, error) {
				mode, err := readBackend(p)
				seen = mode
				return profile.NewStore(p.ConfigDir, func([]byte) error { return nil }, nil), err
			})
		}()
		<-ready
		return nil, writeBackend(p, privilegedBackend)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if seen != privilegedBackend {
		t.Fatalf("backend selected before config lock: %s", seen)
	}
}

func TestEffectiveGroupsUseKnownInlineNodesAndPreserveUnknownRemoteChoices(t *testing.T) {
	candidate := []byte("proxy-providers:\n  local:\n    type: inline\n    payload:\n      - name: inline-node\n        type: direct\n  remote:\n    type: http\n    url: https://example.invalid/nodes.yaml\n    path: ./remote.yaml\nproxy-groups:\n  - name: local-group\n    type: select\n    use: [local]\n  - name: remote-group\n    type: select\n    use: [remote]\n")
	groups, err := effectiveGroups(t.TempDir(), "work", candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !groups["local-group"]["inline-node"] || groups["local-group"]["*"] || !groups["remote-group"]["*"] {
		t.Fatalf("provider option knowledge: %+v", groups)
	}
}

func TestRuleBundleTargetPreflight(t *testing.T) {
	dir := t.TempDir()
	store := rules.New(dir, "work", nil)
	bundle := rules.Entry{Name: "bundle", Enabled: true, Kind: "rules", Items: []string{"DOMAIN,a.example,Work", "IP-CIDR,192.0.2.0/24,DIRECT,no-resolve", "AND,((DOMAIN,a.example),(DST-PORT,443)),Work", "DOMAIN-REGEX,^a,b$,Work", "SUB-RULE,(NETWORK,TCP),sub"}}
	if err := store.Add(context.Background(), bundle); err != nil {
		t.Fatal(err)
	}
	candidate := []byte("proxy-groups:\n  - name: Work\n    type: select\n    proxies: [DIRECT]\n")
	if err := validateRuleTargets(dir, "work", candidate); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	if err := validateRuleTargets(dir, "work", []byte("rules: []")); !errors.Is(err, errRuleConflict) {
		t.Fatalf("missing bundle target not rejected: %v", err)
	}
	if err := store.SetEnabled(context.Background(), "bundle", false); err != nil {
		t.Fatal(err)
	}
	if err := validateRuleTargets(dir, "work", []byte("rules: []")); err != nil {
		t.Fatalf("disabled bundle rejected: %v", err)
	}
}

func TestRefreshFailureUsesSourceStageNotEnglish(t *testing.T) {
	for _, test := range []struct {
		err           error
		stage, reason string
	}{
		{errors.New("invalid yaml content identity decrypt"), "fetch", "network_error"},
		{&subscription.RefreshFailure{Stage: "validate", Cause: errors.New("tunnels must be a list")}, "validate", "invalid_content"},
		{&subscription.RefreshFailure{Stage: "decrypt", Cause: errors.New("取消")}, "decrypt", "identity_unavailable"},
	} {
		stage, reason := classifyRefreshFailure(test.err)
		if stage != test.stage || reason != test.reason {
			t.Fatalf("%v: %s %s", test.err, stage, reason)
		}
	}
	_, store, _ := schedulerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "tunnels: {}\n") })
	_, err := store.Refresh(context.Background(), "work")
	var failure *subscription.RefreshFailure
	if !errors.As(err, &failure) || failure.Stage != "validate" {
		t.Fatalf("normalization did not retain stage: %v", err)
	}
}
