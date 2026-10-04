// Package engine owns the mihomo child process and its runtime files.
package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Nagi/internal/platform"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

var ErrAlreadyRunning = errors.New("mihomo is already running")
var ErrNotRunning = errors.New("mihomo is not running")

type Options struct {
	Binary     string
	ConfigPath string
	Paths      nagiruntime.Paths
	// StartupTimeout is the maximum time spent waiting for the control socket.
	StartupTimeout time.Duration
	// StopTimeout is the maximum graceful shutdown period before SIGKILL.
	StopTimeout time.Duration
}

type Manager struct{ options Options }
type FollowResult struct{ Manager *Manager }

type Status struct {
	Running    bool   `json:"running"`
	PID        int    `json:"pid,omitempty"`
	SocketPath string `json:"socket_path"`
	LogPath    string `json:"log_path"`
	// StalePID is set if a PID file was present but the process is gone.
	StalePID       int  `json:"stale_pid,omitempty"`
	UnexpectedExit bool `json:"unexpected_exit,omitempty"`
}

func New(options Options) (*Manager, error) {
	if err := platform.Check(); err != nil {
		return nil, err
	}
	var err error
	if options.ConfigPath == "" {
		return nil, errors.New("configuration path is required")
	}
	options.ConfigPath, err = filepath.Abs(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	if options.Paths.RuntimeDir == "" {
		options.Paths, err = nagiruntime.Resolve()
		if err != nil {
			return nil, err
		}
	}
	if options.StartupTimeout <= 0 {
		options.StartupTimeout = 10 * time.Second
	}
	if options.StopTimeout <= 0 {
		options.StopTimeout = 5 * time.Second
	}
	return &Manager{options: options}, nil
}

func (m *Manager) Validate(ctx context.Context) error {
	stat, err := os.Stat(m.options.ConfigPath)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return errors.New("configuration must be a regular file")
	}
	binary, err := m.binary()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binary, "-t", "-f", m.options.ConfigPath, "-d", filepath.Dir(m.options.ConfigPath))
	// Validation output can contain configuration values. Return a fixed error instead.
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("configuration validation failed; inspect the selected profile with `nagi config show`, fix it, and retry (validate a local copy with mihomo -t -f FILE -d DIRECTORY): %w", err)
		}
		return fmt.Errorf("unable to run mihomo for configuration validation; check NAGI_MIHOMO_BIN and executable permissions: %w", err)
	}
	return nil
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	_ = ctx
	status := Status{SocketPath: m.options.Paths.SocketPath, LogPath: m.options.Paths.LogPath}
	pid, err := readPID(m.options.Paths.PIDPath)
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	if !platform.Alive(pid) {
		status.StalePID = pid
		status.UnexpectedExit = true
		return status, nil
	}
	status.PID = pid
	status.Running = true
	return status, nil
}

func (m *Manager) Start(ctx context.Context) (Status, error) {
	var status Status
	err := m.withLock(func() error {
		current, err := m.Status(ctx)
		if err != nil {
			return err
		}
		if current.Running {
			return ErrAlreadyRunning
		}
		if err := m.Validate(ctx); err != nil {
			return err
		}
		if err := os.Remove(m.options.Paths.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		log, err := os.OpenFile(m.options.Paths.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer log.Close()
		binary, err := m.binary()
		if err != nil {
			return err
		}
		cmd := exec.Command(binary, "-f", m.options.ConfigPath, "-d", filepath.Dir(m.options.ConfigPath), "-ext-ctl-unix", m.options.Paths.SocketPath)
		cmd.Stdout, cmd.Stderr = log, log
		// Clear inherited controller overrides; mihomo receives the explicit Unix path above.
		cmd.Env = filteredEnv(os.Environ())
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start mihomo: %w", err)
		}
		pid := cmd.Process.Pid
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		if err := writePID(m.options.Paths.PIDPath, pid); err != nil {
			_ = cmd.Process.Kill()
			<-done
			return err
		}
		deadline := time.NewTimer(m.options.StartupTimeout)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			if socketReady(m.options.Paths.SocketPath) {
				if err := os.Chmod(m.options.Paths.SocketPath, 0600); err != nil {
					_ = cmd.Process.Kill()
					<-done
					_ = os.Remove(m.options.Paths.PIDPath)
					return err
				}
				status = Status{Running: true, PID: pid, SocketPath: m.options.Paths.SocketPath, LogPath: m.options.Paths.LogPath}
				return nil
			}
			select {
			case err := <-done:
				_ = os.Remove(m.options.Paths.PIDPath)
				return fmt.Errorf("mihomo exited before socket became ready: %v", err)
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				<-done
				_ = os.Remove(m.options.Paths.PIDPath)
				return ctx.Err()
			case <-deadline.C:
				_ = cmd.Process.Kill()
				<-done
				_ = os.Remove(m.options.Paths.PIDPath)
				return errors.New("mihomo control socket did not become ready")
			case <-ticker.C:
			}
		}
	})
	return status, err
}

func (m *Manager) Stop(ctx context.Context) (Status, error) {
	var status Status
	err := m.withLock(func() error {
		current, err := m.Status(ctx)
		if err != nil {
			return err
		}
		if !current.Running {
			if current.StalePID != 0 {
				_ = os.Remove(m.options.Paths.PIDPath)
				_ = os.Remove(m.options.Paths.SocketPath)
			}
			status = Status{SocketPath: m.options.Paths.SocketPath, LogPath: m.options.Paths.LogPath}
			return ErrNotRunning
		}
		if err := platform.Terminate(current.PID); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		deadline := time.NewTimer(m.options.StopTimeout)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
	waitLoop:
		for platform.Alive(current.PID) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				if err := platform.ForceKill(current.PID); err != nil && !errors.Is(err, os.ErrProcessDone) {
					return err
				}
				break waitLoop
			case <-ticker.C:
			}
		}
		if err := os.Remove(m.options.Paths.PIDPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_ = os.Remove(m.options.Paths.SocketPath)
		status = Status{SocketPath: m.options.Paths.SocketPath, LogPath: m.options.Paths.LogPath}
		return nil
	})
	return status, err
}

func (m *Manager) Restart(ctx context.Context) (Status, error) {
	_, err := m.Stop(ctx)
	if err != nil && !errors.Is(err, ErrNotRunning) {
		return Status{}, err
	}
	return m.Start(ctx)
}

func (m *Manager) Logs(lines int) ([]string, error) {
	if lines < 0 {
		return nil, errors.New("line count must be nonnegative")
	}
	if lines == 0 {
		lines = 100
	}
	file, err := os.Open(m.options.Paths.LogPath)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	ring := make([]string, lines)
	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		ring[count%lines] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	n := count
	if n > lines {
		n = lines
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = ring[(count-n+i)%lines]
	}
	return result, nil
}

// Follow streams complete log lines until ctx is canceled. It starts at the
// current end of the log, so callers receive only newly appended entries.
func (m *Manager) Follow(ctx context.Context, emit func(string) error) error {
	if emit == nil {
		return errors.New("log callback is required")
	}
	file, err := os.OpenFile(m.options.Paths.LogPath, os.O_CREATE|os.O_RDONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(0, 2); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr == nil {
			if err := emit(strings.TrimSuffix(line, "\n")); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if len(line) > 0 {
			if _, err := file.Seek(-int64(len(line)), io.SeekCurrent); err != nil {
				return err
			}
			reader.Reset(file)
		}
		if info, err := file.Stat(); err == nil {
			if offset, err := file.Seek(0, io.SeekCurrent); err == nil && info.Size() < offset {
				if _, err := file.Seek(0, io.SeekStart); err != nil {
					return err
				}
				reader.Reset(file)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (m *Manager) withLock(fn func() error) error {
	if err := m.options.Paths.EnsureRuntime(); err != nil {
		return err
	}
	file, err := os.OpenFile(m.options.Paths.LockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := platform.Lock(file); err != nil {
		return err
	}
	defer platform.Unlock(file)
	return fn()
}

func socketReady(path string) bool {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID file %s", path)
	}
	return pid, nil
}

func writePID(path string, pid int) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".mihomo-pid-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := fmt.Fprintln(file, pid); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func filteredEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "CLASH_OVERRIDE_EXTERNAL_CONTROLLER") || key == "CLASH_OVERRIDE_SECRET" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (m *Manager) binary() (string, error) {
	if m.options.Binary == "" {
		return "", errors.New("mihomo binary is required")
	}
	binary, err := exec.LookPath(m.options.Binary)
	if err != nil {
		return "", fmt.Errorf("find mihomo: %w; set NAGI_MIHOMO_BIN to a mihomo executable", err)
	}
	return filepath.Abs(binary)
}

// Recover removes stale runtime markers after an unexpected process exit. It
// refuses to touch a live process, preserving duplicate-start protection.
func (m *Manager) Recover(ctx context.Context) (Status, error) {
	var result Status
	err := m.withLock(func() error {
		status, err := m.Status(ctx)
		if err != nil {
			return err
		}
		if status.Running {
			return ErrAlreadyRunning
		}
		if status.StalePID == 0 {
			return ErrNotRunning
		}
		if err := os.Remove(m.options.Paths.PIDPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(m.options.Paths.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		result = Status{SocketPath: m.options.Paths.SocketPath, LogPath: m.options.Paths.LogPath, UnexpectedExit: true, StalePID: status.StalePID}
		return nil
	})
	return result, err
}
