package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"golang.org/x/sys/unix"
)

// Open runtime state relative to no-follow descriptors. Every ancestor must be
// controlled by root/the owner; a root sticky temporary directory is permitted.
func rewriteRuntimeDir(paths nagiruntime.Paths) (int, error) {
	path := paths.RuntimeDir
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return -1, rewriteFailure("rewrite_storage")
	}
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return -1, e
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		next, e := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return -1, e
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil {
			unix.Close(fd)
			return -1, rewriteFailure("rewrite_storage")
		}
		if i == len(parts)-1 {
			if int(st.Uid) != os.Geteuid() || st.Mode&077 != 0 {
				unix.Close(fd)
				return -1, rewriteFailure("rewrite_auth")
			}
		} else if int(st.Uid) != os.Geteuid() && st.Uid != 0 || st.Mode&022 != 0 && !(st.Uid == 0 && st.Mode&unix.S_ISVTX != 0) {
			unix.Close(fd)
			return -1, rewriteFailure("rewrite_auth")
		}
	}
	return fd, nil
}
func readRewriteRun(paths nagiruntime.Paths) (rewriteRun, error) {
	dir, e := rewriteRuntimeDir(paths)
	if e != nil {
		return rewriteRun{}, e
	}
	defer unix.Close(dir)
	fd, e := unix.Openat(dir, "request-rewrite-run.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return rewriteRun{}, e
	}
	f := os.NewFile(uintptr(fd), "rewrite-run")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Geteuid() || st.Mode&077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return rewriteRun{}, rewriteFailure("rewrite_auth")
	}
	data, e := io.ReadAll(io.LimitReader(f, 4097))
	if e != nil || len(data) > 4096 {
		return rewriteRun{}, rewriteFailure("rewrite_storage")
	}
	var run rewriteRun
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if dec.Decode(&run) != nil || run.Version != 1 || !rewriteGeneration(run.Generation) || (run.Backend != userBackend && run.Backend != privilegedBackend) || run.PID <= 0 || run.OwnerUID <= 0 || run.StartIdentity == "" || run.Previous != nil && run.Previous.Previous != nil {
		return run, rewriteFailure("rewrite_storage")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return run, rewriteFailure("rewrite_storage")
	}
	return run, nil
}
func writeRewriteRun(paths nagiruntime.Paths, run rewriteRun) error {
	dir, e := rewriteRuntimeDir(paths)
	if e != nil {
		return e
	}
	defer unix.Close(dir)
	suffix, e := rewriteRandom(16, false)
	if e != nil {
		return e
	}
	name := ".rewrite-" + suffix
	fd, e := unix.Openat(dir, name, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return e
	}
	defer unix.Unlinkat(dir, name, 0)
	f := os.NewFile(uintptr(fd), "rewrite-run")
	data, e := json.Marshal(run)
	if e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = unix.Renameat(dir, name, dir, "request-rewrite-run.json"); e != nil {
		return e
	}
	return unix.Fsync(dir)
}
func removeRewriteRun(paths nagiruntime.Paths) error {
	dir, e := rewriteRuntimeDir(paths)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	defer unix.Close(dir)
	e = unix.Unlinkat(dir, "request-rewrite-run.json", 0)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	return e
}
func rewriteGeneration(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func startRewriteSupervisor(paths nagiruntime.Paths, generation string) error {
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	cmd := exec.Command(executable, "__nagi-request-rewrite-supervisor", generation)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return e
	}
	go cmd.Wait()
	return nil
}

// A generation supervisor is independent of menu/window/CLI lifetimes. It
// never automatically recovers a crashed service: recognized traffic remains
// blocked until an explicit user operation. Only certificate renewal is staged.
func runRewriteSupervisor(generation string) int {
	if os.Geteuid() == 0 || !rewriteGeneration(generation) {
		return 1
	}
	paths, e := nagiruntime.Resolve()
	if e != nil {
		return 1
	}
	dir, e := rewriteRuntimeDir(paths)
	if e != nil {
		return 1
	}
	fd, e := unix.Openat(dir, "rewrite-supervisor-"+generation+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	unix.Close(dir)
	if e != nil {
		return 1
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Geteuid() || st.Mode&077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return 1
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		finished := false
		_, e = withNamedLock(paths, "config.lock", func() (any, error) {
			run, e := readRewriteRun(paths)
			if e != nil || run.Generation != generation {
				finished = true
				return nil, e
			}
			backend, e := readBackend(paths)
			if e != nil || backend != run.Backend {
				finished = true
				return nil, rewriteFailure("rewrite_state_uncertain")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, e := rewriteClient(paths)
			if e != nil {
				return nil, e
			}
			var state rewriteEngineStatus
			if e = client.Get(ctx, "/request-rewrite/status", &state); e != nil {
				// No authenticated heartbeat eventually blocks in a surviving engine.
				// Close our child on proven controller disappearance only.
				if rewriteEngineExited(ctx, paths, run) {
					finished = true
					_ = rewriteShutdownChild(paths, generation)
					if run.Previous != nil {
						_ = rewriteShutdownChild(paths, run.Previous.Generation)
					}
					_ = removeRewriteRun(paths)
				}
				return nil, e
			}
			if state.Generation != generation {
				// A candidate may still be queued/prepared after lost acknowledgement.
				// It can never publish itself. Retain ownership through its expiry window.
				if rewriteNow().Sub(run.CreatedAt) < 35*time.Second {
					return nil, nil
				}
				_ = rewriteDeleteGeneration(client, generation)
				_ = rewriteShutdownChild(paths, generation)
				if run.Previous != nil && state.Generation == run.Previous.Generation {
					_ = writeRewriteRun(paths, *run.Previous)
					_ = startRewriteSupervisor(paths, run.Previous.Generation)
				} else {
					_ = removeRewriteRun(paths)
				}
				finished = true
				return nil, nil
			}
			if state.EngineEpoch != run.EngineEpoch && run.EngineEpoch != "" {
				finished = true
				_ = rewriteShutdownChild(paths, generation)
				_ = removeRewriteRun(paths)
				return nil, nil
			}
			if !state.TUNRunning {
				finished = true
				return nil, rewriteTeardown(ctx, paths)
			}
			health, e := rewriteChildControl(ctx, rewriteSocket(paths, generation), "GET", "/status", generation)
			ca, caErr := requestRewriteCAStatus(paths)
			if caErr != nil || ca.Trust != "trusted" {
				_, _ = rewriteChildControl(ctx, rewriteSocket(paths, generation), "POST", "/drain", generation)
				return nil, nil
			}
			if state.State == "blocked" || e != nil || !health.Ready {
				return nil, nil
			}
			if !rewriteNow().Before(run.RenewAt) {
				if e = rewriteReconcile(ctx, paths); e != nil {
					return nil, e
				}
				finished = true
			}
			return nil, nil
		})
		if finished {
			return 0
		}
	}
	return 0
}
