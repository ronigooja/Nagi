package command

import (
	"context"
	"encoding/json"
	"errors"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	child "github.com/ronigooja/Nagi/internal/requestrewriteservice"
	"io"
	"log"
	"net"
	"net/http"
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
	if dec.Decode(&run) != nil || run.Version != 2 || !rewriteGeneration(run.Generation) || (run.Backend != userBackend && run.Backend != privilegedBackend) || run.PID <= 0 || run.OwnerUID <= 0 || run.StartIdentity == "" || run.Previous != nil && run.Previous.Previous != nil {
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
	if rewriteSupervisorReady(paths, generation) {
		return nil
	}

	executable, e := os.Executable()
	if e != nil {
		return e
	}
	readyRead, readyWrite, e := os.Pipe()
	if e != nil {
		return e
	}
	defer readyRead.Close()
	cmd := exec.Command(executable, "__nagi-request-rewrite-supervisor", generation)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.ExtraFiles = []*os.File{readyWrite}
	if e = cmd.Start(); e != nil {
		readyWrite.Close()
		return e
	}
	readyWrite.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	readyRead.SetReadDeadline(time.Now().Add(5 * time.Second))
	var reply [1]byte
	if _, e = io.ReadFull(readyRead, reply[:]); e != nil || reply[0] != 1 {
		_ = cmd.Process.Kill()
		<-done
		// Another already authenticated supervisor may own this generation.
		if rewriteSupervisorReady(paths, generation) {
			return nil
		}
		return rewriteFailure("rewrite_activation_failed")
	}
	return nil
}

func rewriteSupervisorSocket(paths nagiruntime.Paths, generation string) string {
	return filepath.Join(paths.RuntimeDir, "rs-"+generation+".sock")
}
func rewriteSupervisorReady(paths nagiruntime.Paths, generation string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, e := child.Control(ctx, rewriteSupervisorSocket(paths, generation), "GET", "/status", generation)
	return e == nil && status.Version == 2 && status.Generation == generation && status.Ready
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
	socket := rewriteSupervisorSocket(paths, generation)
	if info, e := os.Lstat(socket); e == nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != os.Geteuid() || info.Mode()&os.ModeSocket == 0 {
			return 1
		}
		if os.Remove(socket) != nil {
			return 1
		}
	} else if !os.IsNotExist(e) {
		return 1
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		return 1
	}
	defer listener.Close()
	if os.Chmod(socket, 0600) != nil {
		return 1
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 4096, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/status" || r.URL.RawQuery != "" || r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(child.Status{Version: 2, Generation: generation, Ready: true})
	})}
	go server.Serve(listener)
	defer server.Close()
	// A private inherited descriptor acknowledges validated runtime ownership
	// before the caller can publish any engine branch.
	ready := os.NewFile(3, "rewrite-supervisor-ready")
	if ready == nil {
		return 1
	}
	if _, e = readRewriteRun(paths); e != nil {
		ready.Close()
		return 1
	}
	if _, e = ready.Write([]byte{1}); e != nil {
		ready.Close()
		return 1
	}
	ready.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		finished, _ := rewriteSupervisorTick(paths, generation)
		if finished {
			return 0
		}
	}

	return 0
}

func rewriteSupervisorTick(paths nagiruntime.Paths, generation string) (bool, error) {
	finished := false
	_, err := withNamedLock(paths, "config.lock", func() (any, error) {
		run, e := readRewriteRun(paths)
		if errors.Is(e, os.ErrNotExist) {
			finished = true
			return nil, nil
		}
		if e != nil {
			return nil, e
		}
		if run.Generation != generation {
			finished = true
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), rewriteSupervisorTimeout)
		defer cancel()
		backend, e := readBackend(paths)
		if e != nil || backend != run.Backend {
			_, _ = rewriteChildControl(ctx, rewriteSocket(paths, generation), "POST", "/drain", generation)
			return nil, rewriteFailure("rewrite_state_uncertain")
		}
		client, e := rewriteClient(paths)
		if e != nil {
			return nil, e
		}
		var state rewriteEngineStatus
		if e = client.Get(ctx, "/request-rewrite/status", &state); e != nil {
			if rewriteEngineExited(ctx, paths, run) {
				if e = rewriteCleanupOwned(ctx, paths, run); e != nil {
					return nil, e
				}
				finished = true
				return nil, nil
			}
			return nil, e
		}
		if state.State == "prepared" && state.Generation == generation && rewriteNow().Sub(run.CreatedAt) >= 35*time.Second {
			if e = rewriteDeleteGenerationContext(ctx, client, generation); e != nil {
				return nil, e
			}
			if e = rewriteRetireCandidate(ctx, paths, run, run.Previous != nil); e != nil {
				return nil, e
			}
			finished = true
			return nil, nil
		}
		if state.Generation != generation {
			// An uncertain prepare never publishes itself. Keep ownership through the
			// engine's 30-second candidate expiry, then clean only confirmed records.
			if rewriteNow().Sub(run.CreatedAt) < 35*time.Second {
				return nil, nil
			}
			if state.PreparedGeneration == generation {
				if e = rewriteDeleteGenerationContext(ctx, client, generation); e != nil {
					return nil, e
				}
			}
			restore := run.Previous != nil && state.Generation == run.Previous.Generation
			if e = rewriteRetireCandidate(ctx, paths, run, restore); e != nil {
				return nil, e
			}
			finished = true
			return nil, nil
		}
		if state.EngineEpoch != run.EngineEpoch && run.EngineEpoch != "" {
			if e = rewriteCleanupOwned(ctx, paths, run); e != nil {
				return nil, e
			}
			finished = true
			return nil, nil
		}
		if state.BranchEnabled && (state.State == "active" || state.State == "blocked") && run.Previous != nil {
			// CAS revokes old contexts even if the newly published child has crashed.
			if e = rewriteShutdownRun(ctx, paths, *run.Previous); e != nil {
				return nil, e
			}
			run.Previous = nil
			if e = writeRewriteRun(paths, run); e != nil {
				return nil, e
			}
		}
		if !state.TUNRunning {
			e = rewriteTeardown(ctx, paths)
			finished = e == nil
			return nil, e
		}
		selected, e := rewriteSelected(paths)
		if e != nil || selected != run.Profile {
			_, _ = rewriteChildControl(ctx, rewriteSocket(paths, generation), "POST", "/drain", generation)
			return nil, rewriteFailure("rewrite_state_uncertain")
		}
		snapshot, e := (rewrite.Store{ConfigDir: paths.ConfigDir, Profile: selected}).Load()
		if e != nil {
			return nil, e
		}
		if !snapshot.Document().Enabled {
			e = rewriteTeardown(ctx, paths)
			finished = e == nil
			return nil, e
		}
		if snapshot.Version() != run.RuleVersion {
			_, _ = rewriteChildControl(ctx, rewriteSocket(paths, generation), "POST", "/drain", generation)
			return nil, rewriteFailure("rewrite_state_uncertain")
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
	return finished, err
}

var rewriteSupervisorTimeout = 5 * time.Second

func rewriteCleanupOwned(ctx context.Context, paths nagiruntime.Paths, run rewriteRun) error {
	if e := rewriteShutdownRun(ctx, paths, run); e != nil {
		return e
	}
	if run.Previous != nil {
		if e := rewriteShutdownRun(ctx, paths, *run.Previous); e != nil {
			return e
		}
	}
	return removeRewriteRun(paths)
}
func rewriteRetireCandidate(ctx context.Context, paths nagiruntime.Paths, run rewriteRun, restore bool) error {
	if e := rewriteShutdownRun(ctx, paths, run); e != nil {
		return e
	}
	if restore && run.Previous != nil {
		if e := writeRewriteRun(paths, *run.Previous); e != nil {
			return e
		}
		return rewriteStartSupervisor(paths, run.Previous.Generation)
	}
	if run.Previous != nil {
		if e := rewriteShutdownRun(ctx, paths, *run.Previous); e != nil {
			return e
		}
	}
	return removeRewriteRun(paths)
}
