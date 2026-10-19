package command

import (
	"context"
	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/control"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	child "github.com/ronigooja/Nagi/internal/requestrewriteservice"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func ensureCaptureService(ctx context.Context, p nagiruntime.Paths) error {
	if _, e := capture.Call(ctx, captureSocket(p), capture.Request{Action: "status"}); e == nil {
		return nil
	}
	if e := p.EnsureRuntime(); e != nil {
		return captureFailure("capture_storage")
	}
	exe, e := os.Executable()
	if e != nil {
		return captureFailure("capture_unavailable")
	}
	cmd := exec.Command(exe, "__nagi-capture-service")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() != nil {
		return captureFailure("capture_start_failed")
	}
	go cmd.Wait()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, e := capture.Call(ctx, captureSocket(p), capture.Request{Action: "status"}); e == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return captureFailure("capture_start_failed")
		case <-deadline.C:
			return captureFailure("capture_start_failed")
		case <-ticker.C:
		}
	}
}
func runCaptureService() int {
	if os.Geteuid() == 0 {
		return 1
	}
	p, e := nagiruntime.Resolve()
	if e != nil {
		return 1
	}
	if e = p.EnsureRuntime(); e != nil {
		return 1
	}
	fd, e := unix.Open(filepath.Join(p.RuntimeDir, "capture.lock"), unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return 1
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return 1
	}
	// The held lifetime lock proves no cooperating recorder owns a stale socket.
	if info, e := os.Lstat(captureSocket(p)); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return 1
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(owner.Uid) != os.Geteuid() {
			return 1
		}
		if os.Remove(captureSocket(p)) != nil {
			return 1
		}
	} else if !os.IsNotExist(e) {
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if capture.Serve(ctx, captureSocket(p), os.Geteuid()) != nil {
		return 1
	}
	return 0
}
func rewriteCapturePrerequisites(ctx context.Context, p nagiruntime.Paths, s *rewrite.Snapshot) (*control.Client, rewriteEngineStatus, error) {
	client, state, e := rewritePrerequisites(ctx, p, s)
	if e != nil {
		return nil, state, e
	}
	var c control.RewriteCapabilities
	if client.Get(ctx, "/request-rewrite/capabilities", &c) != nil || c.CaptureProtocol != capture.ProtocolVersion {
		return nil, state, captureFailure("capture_capability_missing")
	}
	return client, state, nil
}

var rewriteCaptureControl = child.ControlCapture

func capturePublish(ctx context.Context, p nagiruntime.Paths, s capture.Status) error {
	run, e := readRewriteRun(p)
	if e != nil {
		return captureFailure("capture_runtime_unavailable")
	}
	client, e := rewriteClient(p)
	if e != nil {
		return captureFailure("capture_runtime_unavailable")
	}
	policy := capturePolicy(&s)
	previous := s.Revision - 1
	var state rewriteEngineStatus
	// Identical replay after preparation is intentionally idempotent.
	if e = client.Put(ctx, "/request-rewrite/capture", control.RewriteCapture{Version: 2, Generation: run.Generation, PreviousRevision: previous, Capture: *policy}, &state); e != nil {
		return captureFailure("capture_state_uncertain")
	}
	if _, e = rewriteCaptureControl(ctx, rewriteSocket(p, run.Generation), run.Generation, *policy); e != nil {
		return captureFailure("capture_state_uncertain")
	}
	return nil
}
func captureLifecycleStop(ctx context.Context, p nagiruntime.Paths) {
	if s := captureStatus(ctx, p); s != nil && !s.ReadOnly && s.State != "stopped" {
		_, _ = capture.Call(ctx, captureSocket(p), capture.Request{Action: "stop", SessionID: s.SessionID})
	}
}
