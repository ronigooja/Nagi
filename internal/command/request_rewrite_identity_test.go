package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

func TestRewritePSProcessIdentity(t *testing.T) {
	const live = "Sun Oct 18 12:34:56 2026 1000\n"
	privateErr := errors.New("private ps failure output")
	for _, tc := range []struct {
		name      string
		probes    []error
		output    string
		readErr   error
		cancelAt  string
		wantGone  bool
		wantLive  bool
		wantReads int
	}{
		{name: "live", probes: []error{nil}, output: live, wantLive: true, wantReads: 1},
		{name: "initial ESRCH", probes: []error{syscall.ESRCH}, wantGone: true},
		{name: "EPERM with readable identity", probes: []error{syscall.EPERM}, output: live, wantLive: true, wantReads: 1},
		{name: "EPERM with failed ps", probes: []error{syscall.EPERM, syscall.EPERM}, readErr: privateErr, wantReads: 1},
		{name: "EPERM with malformed ps", probes: []error{syscall.EPERM}, output: "malformed", wantReads: 1},
		{name: "initial unexpected failure", probes: []error{privateErr}},
		{name: "exit during ps", probes: []error{nil, syscall.ESRCH}, readErr: privateErr, wantGone: true, wantReads: 1},
		{name: "ps exit one while live", probes: []error{nil, nil}, readErr: &exec.ExitError{}, wantReads: 1},
		{name: "ps error then EPERM", probes: []error{nil, syscall.EPERM}, readErr: privateErr, wantReads: 1},
		{name: "ps error then unknown", probes: []error{nil, privateErr}, readErr: privateErr, wantReads: 1},
		{name: "malformed", probes: []error{nil}, output: "secret malformed output", wantReads: 1},
		{name: "extra fields", probes: []error{nil}, output: live + "secret", wantReads: 1},
		{name: "invalid date", probes: []error{nil}, output: "Sun Oct 99 12:34:56 2026 1000", wantReads: 1},
		{name: "invalid uid", probes: []error{nil}, output: "Sun Oct 18 12:34:56 2026 secret", wantReads: 1},
		{name: "oversized", probes: []error{nil}, output: strings.Repeat("s", 257), wantReads: 1},
		{name: "already canceled", cancelAt: "before"},
		{name: "canceled initial probe", probes: []error{syscall.ESRCH}, cancelAt: "probe"},
		{name: "canceled successful ps", probes: []error{nil}, output: live, cancelAt: "read", wantReads: 1},
		{name: "runner deadline error", probes: []error{nil}, readErr: context.DeadlineExceeded, wantReads: 1},
		{name: "runner cancellation error", probes: []error{nil}, readErr: context.Canceled, wantReads: 1},
		{name: "ps timeout", probes: []error{nil}, readErr: context.DeadlineExceeded, cancelAt: "read", wantReads: 1},
		{name: "canceled recheck", probes: []error{nil, syscall.ESRCH}, readErr: privateErr, cancelAt: "recheck", wantReads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelAt == "before" {
				cancel()
			}
			probes, reads := 0, 0
			identity, err := rewritePSProcessIdentity(ctx, 123, func(pid int) error {
				if pid != 123 {
					t.Fatalf("unexpected pid %d", pid)
				}
				if probes >= len(tc.probes) {
					t.Fatal("unexpected existence probe")
				}
				result := tc.probes[probes]
				probes++
				if tc.cancelAt == "probe" || tc.cancelAt == "recheck" && probes == 2 {
					cancel()
				}
				return result
			}, func(got context.Context, pid int) ([]byte, error) {
				if got != ctx || pid != 123 {
					t.Fatal("lookup lost original context or PID")
				}
				reads++
				if tc.cancelAt == "read" {
					cancel()
				}
				return []byte(tc.output), tc.readErr
			})
			if reads != tc.wantReads {
				t.Fatalf("reads=%d want=%d", reads, tc.wantReads)
			}
			if tc.wantLive {
				if err != nil || identity != "darwin:Sun Oct 18 12:34:56 2026 1000" {
					t.Fatalf("identity=%q error=%v", identity, err)
				}
			} else if identity != "" || err == nil || errors.Is(err, os.ErrNotExist) != tc.wantGone {
				t.Fatalf("identity=%q error=%v gone=%v", identity, err, tc.wantGone)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private")) {
				t.Fatal("lookup error leaked private data")
			}
		})
	}
}

func TestRewritePSIdentityExitDecisions(t *testing.T) {
	previous := rewriteProcessIdentity
	defer func() { rewriteProcessIdentity = previous }()
	const recorded = "darwin:Sun Oct 18 12:34:56 2026 1000"
	for _, tc := range []struct {
		name, output      string
		probeErr, readErr error
		wantGone          bool
	}{
		{name: "matching process", output: "Sun Oct 18 12:34:56 2026 1000"},
		{name: "reused PID", output: "Sun Oct 18 12:34:57 2026 1000", wantGone: true},
		{name: "changed owner", output: "Sun Oct 18 12:34:56 2026 1001", wantGone: true},
		{name: "confirmed exit", probeErr: syscall.ESRCH, wantGone: true},
		{name: "permission unknown", probeErr: syscall.EPERM},
		{name: "ps failure unknown", readErr: &exec.ExitError{}},
		{name: "malformed unknown", output: "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rewriteProcessIdentity = func(pid int) (string, error) {
				return rewritePSProcessIdentity(context.Background(), pid, func(int) error { return tc.probeErr }, func(context.Context, int) ([]byte, error) { return []byte(tc.output), tc.readErr })
			}
			run := rewriteRun{PID: 123, StartIdentity: recorded, EnginePID: 123, EngineStartIdentity: recorded}
			if got := rewriteRunProcessGone(run); got != tc.wantGone {
				t.Fatalf("child gone=%v want=%v", got, tc.wantGone)
			}
			if got := rewriteEngineExited(context.Background(), nagiruntime.Paths{}, run); got != tc.wantGone {
				t.Fatalf("engine exited=%v want=%v", got, tc.wantGone)
			}
		})
	}
}
