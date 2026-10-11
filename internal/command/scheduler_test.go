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
	if due, err := s.Due(time.Now().Add(time.Hour)); err != nil || len(due) != 0 {
		t.Fatalf("paused subscription due: %v %v", due, err)
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
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "rules.yaml"), rule, 0600); err != nil {
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
