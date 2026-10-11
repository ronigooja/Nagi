package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/platform"
	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
)

const schedulerHeartbeatInterval = 5 * time.Second

type schedulerHeartbeat struct {
	PID int       `json:"pid"`
	At  time.Time `json:"heartbeat"`
}

func schedulerLockPath(p nagiruntime.Paths) string {
	return filepath.Join(p.RuntimeDir, "subscription-scheduler.lock")
}
func schedulerHeartbeatPath(p nagiruntime.Paths) string {
	return filepath.Join(p.RuntimeDir, "subscription-scheduler.json")
}

// A live advisory lock is authoritative. A heartbeat is diagnostic only: a
// paused process still owns the scheduler, even if its timestamp is old.
func schedulerRunning(p nagiruntime.Paths) bool {
	f, err := os.Open(schedulerLockPath(p))
	if err != nil {
		return false
	}
	defer f.Close()
	err = platform.TryLock(f)
	if err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return false
		}
		data, readErr := os.ReadFile(schedulerHeartbeatPath(p))
		if readErr != nil {
			return false
		}
		var beat schedulerHeartbeat
		if json.Unmarshal(data, &beat) != nil || beat.PID <= 0 || !platform.Alive(beat.PID) {
			return false
		}
		age := time.Since(beat.At)
		return age >= 0 && age < 3*schedulerHeartbeatInterval
	}
	_ = platform.Unlock(f)
	return false
}

type schedulerHost struct {
	file    *os.File
	paths   nagiruntime.Paths
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func acquireScheduler(p nagiruntime.Paths) (*schedulerHost, error) {
	if err := p.EnsureRuntime(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(schedulerLockPath(p), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = platform.TryLock(f); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.New("subscription scheduler is already running")
		}
		return nil, err
	}
	h := &schedulerHost{file: f, paths: p, done: make(chan struct{}), stopped: make(chan struct{})}
	if err = h.writeHeartbeat(); err != nil {
		_ = platform.Unlock(f)
		_ = f.Close()
		return nil, err
	}
	go h.heartbeat()
	return h, nil
}
func (h *schedulerHost) writeHeartbeat() error {
	b, _ := json.Marshal(schedulerHeartbeat{PID: os.Getpid(), At: time.Now().UTC()})
	tmp := schedulerHeartbeatPath(h.paths) + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, schedulerHeartbeatPath(h.paths))
}
func (h *schedulerHost) heartbeat() {
	defer close(h.stopped)
	t := time.NewTicker(schedulerHeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-t.C:
			_ = h.writeHeartbeat()
		}
	}
}
func (h *schedulerHost) Close() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		close(h.done)
		<-h.stopped
		_ = os.Remove(schedulerHeartbeatPath(h.paths))
		_ = platform.Unlock(h.file)
		_ = h.file.Close()
	})
}

func retryDelay(seconds int64) time.Duration {
	d := time.Duration(seconds) * time.Second
	if d <= 0 {
		d = time.Minute
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}
func classifyRefreshFailure(err error) (string, string) {
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "decrypt") || strings.Contains(s, "identity") {
		return "decrypt", "identity_unavailable"
	}
	if strings.Contains(s, "invalid") || strings.Contains(s, "yaml") || strings.Contains(s, "content") {
		return "validate", "invalid_content"
	}
	return "fetch", "network_error"
}

type schedulerApply func(context.Context, subscription.Entry) error

// Each metadata write uses config.lock so a concurrent configuration rollback
// cannot silently restore an old index. No lock is held during Refresh I/O.
func schedulerStep(ctx context.Context, p nagiruntime.Paths, store *subscription.Store, apply schedulerApply, now time.Time) error {
	due, err := store.Due(now)
	if err != nil {
		return err
	}
	for _, entry := range due {
		if err = ctx.Err(); err != nil {
			return err
		}
		// A source edit or unreadable source is a persistent pause. Check before
		// fetching and recheck under config.lock immediately before activation.
		if entry.Profile != "" {
			reason := sourceConflict(store, entry)
			if reason != "" {
				if err = recordSchedulerFailure(p, store, entry, "paused", reason, time.Time{}); err != nil {
					return err
				}
				continue
			}
		}
		_, fetchErr := store.Refresh(ctx, entry.Name)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		latest, err := store.Find(entry.Name)
		if err != nil {
			continue
		}
		if latest.Generation != entry.Generation || latest.ScheduleSeconds != entry.ScheduleSeconds || !latest.NextDue.Equal(entry.NextDue) || latest.LastFailureStage == "paused" {
			continue
		}
		if fetchErr != nil {
			stage, reason := classifyRefreshFailure(fetchErr)
			if err = recordSchedulerFailure(p, store, latest, stage, reason, now.Add(retryDelay(latest.ScheduleSeconds))); err != nil {
				return err
			}
		} else if err = recordSchedulerSuccess(p, store, latest, now.Add(time.Duration(latest.ScheduleSeconds)*time.Second)); err != nil {
			return err
		}
		latest, err = store.Find(entry.Name)
		if err != nil {
			continue
		}
		if latest.Profile == "" || latest.CacheVersion == "" || latest.CacheVersion == latest.AppliedCacheVersion || apply == nil {
			continue
		}
		if err = apply(ctx, latest); err != nil {
			reason := applyFailureReason(err)
			if reason == "profile_changed" || reason == "source_unreadable" {
				if e := recordSchedulerFailure(p, store, latest, "paused", reason, time.Time{}); e != nil {
					return e
				}
			} else if e := recordSchedulerFailure(p, store, latest, "apply", reason, now.Add(retryDelay(latest.ScheduleSeconds))); e != nil {
				return e
			}
		}
	}
	return nil
}
func sourceConflict(store *subscription.Store, e subscription.Entry) string {
	if e.SourceHash == "" {
		return "profile_changed"
	}
	path := filepath.Join(store.ConfigDir, "profiles", e.Profile+".yaml")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "source_unreadable"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "source_unreadable"
	}
	if subscription.Hash(data) != e.SourceHash {
		return "profile_changed"
	}
	return ""
}
func metadataIfSame(p nagiruntime.Paths, store *subscription.Store, expected subscription.Entry, fn func() error) error {
	_, err := withNamedLock(p, "config.lock", func() (any, error) {
		now, e := store.Find(expected.Name)
		if e != nil {
			return nil, nil
		}
		if now.Generation != expected.Generation || now.ScheduleSeconds != expected.ScheduleSeconds || now.Profile != expected.Profile || now.SourceHash != expected.SourceHash || now.CacheVersion != expected.CacheVersion || now.LastFailureStage == "paused" && expected.LastFailureStage != "paused" {
			return nil, nil
		}
		return nil, fn()
	})
	return err
}
func recordSchedulerFailure(p nagiruntime.Paths, s *subscription.Store, e subscription.Entry, stage, reason string, next time.Time) error {
	return metadataIfSame(p, s, e, func() error {
		if stage == "paused" && sourceConflict(s, e) != reason {
			return nil
		}
		return s.RecordFailure(e.Name, stage, reason, next)
	})
}
func recordSchedulerSuccess(p nagiruntime.Paths, s *subscription.Store, e subscription.Entry, next time.Time) error {
	return metadataIfSame(p, s, e, func() error { return s.RecordSuccess(e.Name, next) })
}
func applyFailureReason(err error) string {
	switch {
	case errors.Is(err, errSourceChanged):
		return "profile_changed"
	case errors.Is(err, errSourceUnreadable):
		return "source_unreadable"
	case errors.Is(err, errDNSConflict):
		return "dns_target_missing"
	case errors.Is(err, errRuleConflict):
		return "custom_rule_conflict"
	case errors.Is(err, errChoiceConflict):
		return "selection_conflict"
	default:
		return "activation_failed"
	}
}

func runSubscriptionWatch(ctx context.Context, stdout, stderr io.Writer, jsonMode bool, version, commit string) int {
	p, err := nagiruntime.Resolve()
	if err != nil {
		output.WriteError(stderr, jsonMode, "subscription_error", err.Error())
		return 1
	}
	h, err := acquireScheduler(p)
	if err != nil {
		output.WriteError(stderr, jsonMode, "subscription_error", err.Error())
		return 1
	}
	defer h.Close()
	if jsonMode {
		if err := output.Write(stdout, true, map[string]any{"scheduler_running": true}); err != nil {
			output.WriteError(stderr, true, "output_error", err.Error())
			return 1
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "Subscription scheduler running; interrupt to stop."); err != nil {
			output.WriteError(stderr, false, "output_error", err.Error())
			return 1
		}
	}
	binary := os.Getenv("NAGI_MIHOMO_BIN")
	if binary == "" {
		exe, e := os.Executable()
		if e != nil {
			output.WriteError(stderr, jsonMode, "subscription_error", e.Error())
			return 1
		}
		binary = filepath.Join(filepath.Dir(exe), "mihomo")
	}
	s := subscription.NewStore(p.ConfigDir, filepath.Join(p.DataDir, "cache", "subscriptions"), nil)
	s.Binary = binary
	s.CommitUnderConfigLock = func(fn func() error) error {
		_, e := withNamedLock(p, "config.lock", func() (any, error) { return nil, fn() })
		return e
	}
	apply := func(ctx context.Context, e subscription.Entry) error {
		client, err := controlClientForCurrentBackend(p)
		if err != nil {
			return err
		}
		current, err := profile.NewStore(p.ConfigDir, nil, nil).Current()
		if err != nil {
			return err
		}
		mode, err := readBackend(p)
		if err != nil {
			return err
		}
		var manager lifecycleBackend
		if mode == privilegedBackend {
			manager = helperBackend{profile: current, binary: binary, configPath: filepath.Join(p.ConfigDir, "profiles", current+".yaml"), paths: p}
		} else {
			manager, err = engine.New(engine.Options{Binary: binary, ConfigPath: filepath.Join(p.ConfigDir, "profiles", current+".yaml"), Paths: p})
			if err != nil {
				return err
			}
		}
		profiles := newProfileStore(p, binary, client, manager)
		return applyScheduledCache(ctx, p, s, profiles, e)
	}
	for {
		if ctx.Err() != nil {
			return 0
		}
		if err := schedulerStep(ctx, p, s, apply, time.Now().UTC()); err != nil && ctx.Err() == nil {
			output.WriteError(stderr, jsonMode, "subscription_error", err.Error())
		}
		timer := time.NewTimer(schedulerDelay(s, time.Now().UTC()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
}
func schedulerDelay(s *subscription.Store, now time.Time) time.Duration {
	entries, err := s.List()
	if err != nil {
		return time.Minute
	}
	delay := time.Minute
	for _, entry := range entries {
		if entry.ScheduleSeconds == 0 || entry.LastFailureStage == "paused" {
			continue
		}
		until := entry.NextDue.Sub(now)
		if until < time.Second {
			until = time.Second
		}
		if until < delay {
			delay = until
		}
	}
	return delay
}

func runForegroundSubscriptionWatch(stdout, stderr io.Writer, jsonMode bool, version, commit string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runSubscriptionWatch(ctx, stdout, stderr, jsonMode, version, commit)
}
func schedulerRunningForCurrentUser() bool {
	p, err := nagiruntime.Resolve()
	if err != nil {
		return false
	}
	return schedulerRunning(p)
}
