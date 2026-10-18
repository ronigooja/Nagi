package command

import (
	"context"
	"errors"
	"fmt"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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
