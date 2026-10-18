package command

import (
	"context"
	"errors"
	"fmt"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Process identity is recovery metadata, never authority to signal a process.
// Cleanup always uses the kernel-UID-authenticated generation control socket.
var rewriteProcessIdentity = func(pid int) (string, error) {
	if pid <= 0 {
		return "", rewriteFailure("rewrite_auth")
	}
	if runtime.GOOS == "linux" {
		data, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if e != nil {
			return "", e
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return "", rewriteFailure("rewrite_auth")
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
			return "", os.ErrNotExist
		}
		if len(fields) < 20 {
			return "", rewriteFailure("rewrite_auth")
		}
		boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
		if e != nil {
			return "", e
		}
		return "linux:" + strings.TrimSpace(string(boot)) + ":" + fields[19], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, e := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "lstart=", "-o", "uid=").Output()
	if e != nil || len(data) > 256 {
		return "", rewriteFailure("rewrite_auth")
	}
	fields := strings.Fields(string(data))
	if len(fields) < 6 {
		return "", rewriteFailure("rewrite_auth")
	}
	return "darwin:" + strings.Join(fields, " "), nil
}

var rewriteEngineIdentity = func(ctx context.Context, paths nagiruntime.Paths) (int, string, error) {
	backend, e := readBackend(paths)
	if e != nil {
		return 0, "", e
	}
	pid := 0
	if backend == privilegedBackend {
		state, e := (helperBackend{paths: paths}).Status(ctx)
		if e != nil || !state.Running {
			return 0, "", rewriteFailure("rewrite_unavailable")
		}
		pid = state.PID
	} else {
		data, e := os.ReadFile(paths.PIDPath)
		if e != nil {
			return 0, "", e
		}
		pid, e = strconv.Atoi(strings.TrimSpace(string(data)))
		if e != nil || pid <= 0 {
			return 0, "", rewriteFailure("rewrite_state_uncertain")
		}
	}
	identity, e := rewriteProcessIdentity(pid)
	return pid, identity, e
}

func rewriteEngineExited(ctx context.Context, paths nagiruntime.Paths, run rewriteRun) bool {
	if run.EnginePID <= 0 || run.EngineStartIdentity == "" {
		return false
	}
	if run.Backend == privilegedBackend {
		state, e := (helperBackend{paths: paths}).Status(ctx)
		if e == nil && !state.Running {
			return true
		}
	}
	identity, e := rewriteProcessIdentity(run.EnginePID)
	return errors.Is(e, os.ErrNotExist) || e == nil && identity != run.EngineStartIdentity
}

var rewriteRunProcessGone = func(run rewriteRun) bool {
	identity, e := rewriteProcessIdentity(run.PID)
	return errors.Is(e, os.ErrNotExist) || e == nil && identity != run.StartIdentity
}

func rewriteShutdownRun(ctx context.Context, paths nagiruntime.Paths, run rewriteRun) error {
	// Request shutdown even when the PID has been recycled; only the private
	// generation socket can authorize cleanup of a surviving same-generation child.
	_, _ = rewriteChildControl(ctx, rewriteSocket(paths, run.Generation), "POST", "/shutdown", run.Generation)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if rewriteRunProcessGone(run) {
			dir, e := rewriteRuntimeDir(paths)
			if e != nil {
				return e
			}
			defer syscall.Close(dir)
			name := "rewrite-" + run.Generation + ".sock"
			var st unix.Stat_t
			e = unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW)
			if errors.Is(e, os.ErrNotExist) {
				return nil
			}
			if e != nil || int(st.Uid) != os.Geteuid() || st.Mode&unix.S_IFMT != unix.S_IFSOCK {
				return rewriteFailure("rewrite_auth")
			}
			return unix.Unlinkat(dir, name, 0)
		}
		select {
		case <-ctx.Done():
			return rewriteFailure("rewrite_state_uncertain")
		case <-ticker.C:
		}
	}
}
