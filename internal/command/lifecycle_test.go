package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/engine"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func TestExplicitStopIntentSuppressesMonitorStart(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeIntent(paths, "stopped"); err != nil {
		t.Fatal(err)
	}
	result, err := execute(context.Background(), []string{"startup", "ensure"}, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["reason"] != "stopped" {
		t.Fatal(result)
	}
	data, err := os.ReadFile(intentPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "stopped\n" {
		t.Fatal(string(data))
	}
	info, err := os.Stat(filepath.Join(paths.StateDir, "engine-intent"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("intent file: %v, %v", info, err)
	}
}

func TestStopRecordsIntentWhenAlreadyStopped(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	_, err = execute(context.Background(), []string{"stop"}, "test", "test")
	if !errors.Is(err, engine.ErrNotRunning) {
		t.Fatalf("stop error = %v", err)
	}
	if intent, err := readIntent(paths); err != nil || intent != "stopped" {
		t.Fatalf("intent = %q, %v", intent, err)
	}
}

func TestIntentDefaultsToRunningAndRejectsCorruption(t *testing.T) {
	paths := nagiruntime.Paths{StateDir: t.TempDir()}
	if got, err := readIntent(paths); err != nil || got != "running" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := os.WriteFile(intentPath(paths), []byte("unknown\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readIntent(paths); err == nil {
		t.Fatal("invalid intent accepted")
	}
}

func TestFreshMonitorResumesCompletedQuitButPreservesExplicitStop(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ before, after string }{
		{"quit-pending", "quit-pending"},
		{"quit-ready", "running"},
		{"stopped", "stopped"},
	} {
		if err := writeIntent(paths, tc.before); err != nil {
			t.Fatal(err)
		}
		if _, err := execute(context.Background(), []string{"startup", "login"}, "test", "test"); err != nil {
			t.Fatal(err)
		}
		if got, err := readIntent(paths); err != nil || got != tc.after {
			t.Fatalf("%s -> %s, %v; want %s", tc.before, got, err, tc.after)
		}
	}
}

func TestFailedQuitLeavesRecoverableStoppedIntent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	previous := pauseMonitor
	pauseMonitor = func() error { return errors.New("service manager unavailable") }
	t.Cleanup(func() { pauseMonitor = previous })
	_, err := execute(context.Background(), []string{"quit"}, "test", "test")
	if err == nil {
		t.Fatal("expected pause failure")
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readIntent(paths); err != nil || got != "stopped" {
		t.Fatalf("intent = %q, %v", got, err)
	}
}

func TestQuitReturnsSuccessAfterCleanupAndResumesAtNextLogin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	previous := pauseMonitor
	pauseMonitor = func() error { return nil }
	t.Cleanup(func() { pauseMonitor = previous })
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "quit"}, &stdout, &stderr, "test", "test"); code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"quit":true`)) {
		t.Fatal(stdout.String())
	}
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readIntent(paths); err != nil || got != "quit-ready" {
		t.Fatalf("intent = %q, %v", got, err)
	}
	if _, err := execute(context.Background(), []string{"startup", "login"}, "test", "test"); err != nil {
		t.Fatal(err)
	}
	if got, err := readIntent(paths); err != nil || got != "running" {
		t.Fatalf("next-login intent = %q, %v", got, err)
	}
}

func TestQuitRollbackErrorExplainsRecovery(t *testing.T) {
	cause := errors.New("monitor pause failed")
	err := quitRollbackError(cause, errors.New("permission denied"))
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "pending quit state") || !strings.Contains(err.Error(), "retry `nagi quit`") {
		t.Fatalf("error = %v", err)
	}
}

func TestInterruptedQuitCanBeRetried(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeIntent(paths, "quit-pending"); err != nil {
		t.Fatal(err)
	}
	_, err = execute(context.Background(), []string{"start"}, "test", "test")
	var commandErr *commandError
	if !errors.As(err, &commandErr) || commandErr.code != "lifecycle_busy" || !strings.Contains(err.Error(), "`nagi quit` again") {
		t.Fatalf("start error = %v", err)
	}
	previous := pauseMonitor
	pauseMonitor = func() error { return nil }
	t.Cleanup(func() { pauseMonitor = previous })
	if _, err := execute(context.Background(), []string{"quit"}, "test", "test"); err != nil {
		t.Fatal(err)
	}
	if got, err := readIntent(paths); err != nil || got != "quit-ready" {
		t.Fatalf("intent = %q, %v", got, err)
	}
}

func TestStartWaitsForWholeQuitTransaction(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	quitDone := make(chan error, 1)
	go func() {
		_, err := withQuitLock(paths, func() (any, error) {
			close(entered)
			<-release
			return nil, nil
		})
		quitDone <- err
	}()
	<-entered
	startDone := make(chan error, 1)
	go func() {
		_, err := execute(context.Background(), []string{"start"}, "test", "test")
		startDone <- err
	}()
	select {
	case err := <-startDone:
		t.Fatalf("start crossed quit lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-quitDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-startDone:
	case <-time.After(2 * time.Second):
		t.Fatal("start did not proceed after quit lock released")
	}
}
