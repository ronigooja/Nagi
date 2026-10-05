package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/privileged/configpolicy"
	"golang.org/x/sys/unix"
)

// RuntimeHandler owns one root mihomo child for one authenticated login UID.
// BinaryPath, SourceDir and PrivateDir are supplied by the root daemon, never
// by a client request. SourceDir bounds user-controlled profile reads.
type RuntimeHandler struct {
	SessionUID int
	BinaryPath string
	SourceDir  string
	PrivateDir string
	mu         sync.Mutex
	child      *exec.Cmd
	done       chan error
	snapshot   string
	orphaned   bool
}

func (h *RuntimeHandler) Handle(ctx context.Context, _ int, req privileged.Request) (privileged.Response, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch req.Operation {
	case privileged.Start:
		if h.orphaned {
			return privileged.Response{}, errors.New("orphaned_privileged_engine: root mihomo survived helper restart; administrator recovery is required")
		}
		return h.start(ctx, req)
	case privileged.Stop:
		if h.orphaned {
			return privileged.Response{}, errors.New("orphaned_privileged_engine: root mihomo survived helper restart; administrator recovery is required")
		}
		return h.stop(ctx)
	case privileged.Status:
		if h.orphaned {
			return privileged.Response{}, errors.New("orphaned_privileged_engine: root mihomo survived helper restart; administrator recovery is required")
		}
		return h.status(), nil
	case privileged.Control:
		if h.child == nil {
			return privileged.Response{}, errors.New("privileged mihomo is not running")
		}
		if req.Method == "PUT" && req.Path == "/configs?force=true" {
			return h.reload(ctx, req)
		}
		return controlRequest(ctx, h.controllerPath(), req)
	case privileged.Logs:
		return h.logs(req)
	default:
		return privileged.Response{}, errors.New("unsupported helper operation")
	}
}

func (h *RuntimeHandler) logs(req privileged.Request) (privileged.Response, error) {
	f, err := os.OpenFile(filepath.Join(h.PrivateDir, "mihomo.log"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		body, _ := json.Marshal([]string{})
		return privileged.Response{OK: true, Body: body}, nil
	}
	if err != nil {
		return privileged.Response{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return privileged.Response{}, err
	}
	if !info.Mode().IsRegular() {
		return privileged.Response{}, errors.New("invalid helper log")
	}
	if req.Offset == -1 {
		body, _ := json.Marshal([]string{})
		return privileged.Response{OK: true, Body: body, Offset: info.Size()}, nil
	}
	if req.Offset > 0 {
		if req.Offset > info.Size() {
			req.Offset = 0
		}
		if _, err := f.Seek(req.Offset, io.SeekStart); err != nil {
			return privileged.Response{}, err
		}
		reader := bufio.NewReader(f)
		lines := make([]string, 0)
		pos := req.Offset
		for len(lines) < 1000 {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				break
			}
			pos += int64(len(line))
			lines = append(lines, strings.TrimSuffix(line, "\n"))
			if pos-req.Offset >= 1<<20 {
				break
			}
		}
		body, _ := json.Marshal(lines)
		return privileged.Response{OK: true, Body: body, Offset: pos}, nil
	}
	// Recent history is bounded to the final 1 MiB and the requested line count.
	start := info.Size() - 1<<20
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return privileged.Response{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return privileged.Response{}, err
	}
	lines := []string{}
	if len(data) != 0 {
		lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	count := req.Lines
	if count == 0 {
		count = 100
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	body, _ := json.Marshal(lines)
	return privileged.Response{OK: true, Body: body, Offset: info.Size()}, nil
}

func (h *RuntimeHandler) status() privileged.Response {
	if h.child == nil {
		return privileged.Response{OK: true}
	}
	select {
	case <-h.done:
		h.child = nil
		_ = os.Remove(h.controllerPath())
		_ = os.Remove(h.snapshot)
		_ = os.Remove(h.markerPath())
		h.snapshot = ""
		return privileged.Response{OK: true}
	default:
		return privileged.Response{OK: true, Running: true, PID: h.child.Process.Pid}
	}
}

func (h *RuntimeHandler) start(ctx context.Context, req privileged.Request) (privileged.Response, error) {
	if h.status().Running {
		return privileged.Response{}, errors.New("already_running")
	}
	if markerExists(h.markerPath()) || controllerLive(h.controllerPath()) {
		h.orphaned = true
		return privileged.Response{}, errors.New("orphaned_privileged_engine: prior root child may still be active")
	}
	if err := verifyTrustedBinary(h.BinaryPath); err != nil {
		return privileged.Response{}, err
	}
	if err := rootDirectory(h.PrivateDir, 0700); err != nil {
		return privileged.Response{}, err
	}
	data, err := readUserConfig(h.SourceDir, req.ConfigPath, h.SessionUID)
	if err != nil {
		return privileged.Response{}, err
	}
	snapshot, err := configpolicy.Snapshot(h.PrivateDir, data)
	if err != nil {
		return privileged.Response{}, err
	}
	controller := h.controllerPath()
	if err := os.Remove(controller); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(snapshot)
		return privileged.Response{}, err
	}
	log, err := os.OpenFile(filepath.Join(h.PrivateDir, "mihomo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		os.Remove(snapshot)
		return privileged.Response{}, err
	}
	defer log.Close()
	cmd := exec.Command(h.BinaryPath, "-f", snapshot, "-d", h.PrivateDir, "-ext-ctl-unix", controller)
	cmd.Dir = h.PrivateDir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + h.PrivateDir, "TMPDIR=" + h.PrivateDir}
	cmd.Stdout, cmd.Stderr = log, log
	marker, err := os.OpenFile(h.markerPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		os.Remove(snapshot)
		return privileged.Response{}, fmt.Errorf("create child marker: %w", err)
	}
	if _, err := marker.WriteString("pending\n"); err != nil {
		marker.Close()
		os.Remove(h.markerPath())
		os.Remove(snapshot)
		return privileged.Response{}, err
	}
	if err := marker.Close(); err != nil {
		os.Remove(h.markerPath())
		os.Remove(snapshot)
		return privileged.Response{}, err
	}
	if err := cmd.Start(); err != nil {
		os.Remove(snapshot)
		os.Remove(h.markerPath())
		return privileged.Response{}, fmt.Errorf("start privileged mihomo: %w", err)
	}
	h.child, h.done, h.snapshot = cmd, make(chan error, 1), snapshot
	go func() { h.done <- cmd.Wait() }()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := QueryController(ctx, controller, ControllerVersion); err == nil {
			return privileged.Response{OK: true, Running: true, PID: cmd.Process.Pid}, nil
		}
		select {
		case <-ctx.Done():
			h.kill()
			return privileged.Response{}, ctx.Err()
		case <-deadline.C:
			h.kill()
			return privileged.Response{}, errors.New("privileged mihomo controller did not become ready")
		case <-h.done:
			h.child = nil
			os.Remove(snapshot)
			os.Remove(h.markerPath())
			return privileged.Response{}, errors.New("privileged mihomo exited during startup")
		case <-tick.C:
		}
	}
}

func (h *RuntimeHandler) stop(ctx context.Context) (privileged.Response, error) {
	if !h.status().Running {
		return privileged.Response{}, errors.New("not_running")
	}
	_ = h.child.Process.Signal(unix.SIGTERM)
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		h.kill()
	case <-ctx.Done():
		return privileged.Response{}, ctx.Err()
	}
	h.child = nil
	_ = os.Remove(h.controllerPath())
	_ = os.Remove(h.snapshot)
	_ = os.Remove(h.markerPath())
	h.snapshot = ""
	return privileged.Response{OK: true}, nil
}

func (h *RuntimeHandler) kill() {
	if h.child != nil {
		_ = h.child.Process.Kill()
		<-h.done
		h.child = nil
	}
	_ = os.Remove(h.controllerPath())
	_ = os.Remove(h.snapshot)
	_ = os.Remove(h.markerPath())
	h.snapshot = ""
}

func (h *RuntimeHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.child == nil {
		return nil
	}
	_, err := h.stop(context.Background())
	return err
}

func (h *RuntimeHandler) controllerPath() string { return filepath.Join(h.PrivateDir, "mihomo.sock") }
func (h *RuntimeHandler) markerPath() string     { return filepath.Join(h.PrivateDir, "child.marker") }
func markerExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func controllerLive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (h *RuntimeHandler) reload(ctx context.Context, req privileged.Request) (privileged.Response, error) {
	var body struct {
		Path string `json:"path"`
	}
	if strictJSON(req.Body, &body) != nil || body.Path == "" {
		return privileged.Response{}, errors.New("invalid configuration reload")
	}
	data, err := readUserConfig(h.SourceDir, body.Path, h.SessionUID)
	if err != nil {
		return privileged.Response{}, err
	}
	snapshot, err := configpolicy.Snapshot(h.PrivateDir, data)
	if err != nil {
		return privileged.Response{}, err
	}
	encoded, _ := json.Marshal(map[string]string{"path": snapshot})
	req.Body = encoded
	// The only reload URL is built here; clients cannot supply an arbitrary
	// controller path or a root-readable configuration filename.
	response, err := controlRequestReload(ctx, h.controllerPath(), req)
	if err != nil || !response.OK {
		_ = os.Remove(snapshot)
		return response, err
	}
	_ = os.Remove(h.snapshot)
	h.snapshot = snapshot
	return response, nil
}

// readUserConfig traverses every component with openat and O_NOFOLLOW so a
// symlink replacement between request validation and reading cannot redirect
// a root file read. Only regular files under the configured source tree pass.
func readUserConfig(root, path string, uid int) ([]byte, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) || filepath.Clean(root) != root || filepath.Clean(path) != path {
		return nil, errors.New("invalid configuration path")
	}
	// macOS maps /var to /private/var. Resolve only this fixed system alias;
	// the openat walk below still rejects links in user-controlled components.
	if runtime.GOOS == "darwin" {
		if strings.HasPrefix(root, "/var/") {
			root = "/private" + root
		}
		if strings.HasPrefix(path, "/var/") {
			path = "/private" + path
		}
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("configuration is outside session directory")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, part, flags, 0)
		if err != nil {
			return nil, err
		}
		unix.Close(fd)
		fd = next
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size < 1 || stat.Size > 8<<20 || stat.Uid != uint32(uid) || stat.Mode&0022 != 0 {
		return nil, errors.New("configuration is not a supported regular file")
	}
	data := make([]byte, stat.Size)
	for offset := 0; offset < len(data); {
		n, err := unix.Read(fd, data[offset:])
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrUnexpectedEOF
		}
		offset += n
	}
	return data, nil
}
