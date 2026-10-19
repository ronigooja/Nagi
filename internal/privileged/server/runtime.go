package server

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"golang.org/x/sys/unix"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// RuntimeHandler owns one root mihomo child for one authenticated login UID.
// BinaryPath, SourceDir and PrivateDir are supplied by the root daemon, never
// by a client request. SourceDir bounds user-controlled profile reads.
type RuntimeHandler struct {
	SessionUID        int
	BinaryPath        string
	SourceDir         string
	PrivateDir        string
	mu                sync.Mutex
	child             *exec.Cmd
	done              chan error
	snapshot          string
	resources         []string
	rewriteOwnerBound bool
	generation        uint64
	orphaned          bool
	dnsError          error
}

func (h *RuntimeHandler) Handle(ctx context.Context, peerUID int, req privileged.Request) (privileged.Response, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return privileged.Response{}, err
	}
	if req.Operation == privileged.Control {
		if err := h.validateRewriteOwner(peerUID, req); err != nil {
			return privileged.Response{}, err
		}
	}
	if req.Operation == privileged.Control && snapshotControl(req) {
		if !h.status().Running {
			return privileged.Response{}, i18n.Errorf("system.privileged.privileged.mihomo.is.not.running", "privileged mihomo is not running")
		}
		child, generation, socket := h.child, h.generation, h.controllerPath()
		h.mu.Unlock()
		response, err := controlRequest(ctx, socket, req)
		h.mu.Lock()
		// Traffic counters belong to the process, not a configuration generation.
		// Mode changes and reloads must not interrupt that process's traffic watch.
		if h.child != child || (req.Path != "/traffic" && h.generation != generation) || !h.status().Running {
			return privileged.Response{}, i18n.Errorf("system.privileged.stale.controller.response.privileged.engine.changed.during.request", "stale_controller_response: privileged engine changed during request")
		}
		return response, err
	}
	switch req.Operation {
	case privileged.TUNDNSSync, privileged.TUNDNSRestore, privileged.TUNDNSStatus:
		return h.handleDNS(ctx, req.Operation)
	case privileged.Start:
		if h.orphaned {
			return privileged.Response{}, i18n.Errorf("system.privileged.orphaned.privileged.engine.root.mihomo.survived.helper.restart.administrator.recovery", "orphaned_privileged_engine: root mihomo survived helper restart; administrator recovery is required")
		}
		return h.start(ctx, req)
	case privileged.Stop:
		if h.orphaned {
			return privileged.Response{}, i18n.Errorf("system.privileged.orphaned.privileged.engine.root.mihomo.survived.helper.restart.administrator.recovery", "orphaned_privileged_engine: root mihomo survived helper restart; administrator recovery is required")
		}
		return h.stop(ctx)
	case privileged.Status:
		if h.orphaned {
			return privileged.Response{}, i18n.Errorf("system.privileged.orphaned.privileged.engine.root.mihomo.survived.helper.restart.administrator.recovery", "orphaned_privileged_engine: root mihomo survived helper restart; administrator recovery is required")
		}
		// DNS recovery errors must not hide process liveness or its PID.
		// The dedicated DNS status operation reports those failures separately.
		return h.status(), nil
	case privileged.Control:
		if !(req.Method == "PUT" && req.Path == "/configs?force=true") {
			if err := validateControl(req); err != nil {
				return privileged.Response{}, err
			}
		}
		// Configuration activation can replace every snapshot field; ordinary
		// selection, measurement and cache operations retain the same engine.
		if (req.Method == "PATCH" && req.Path == "/configs") || (req.Method == "PUT" && req.Path == "/configs?force=true") {
			h.generation++
		}
		if h.child == nil {
			return privileged.Response{}, i18n.Errorf("system.privileged.privileged.mihomo.is.not.running", "privileged mihomo is not running")
		}
		if req.Method == "PUT" && req.Path == "/configs?force=true" {
			return h.reload(ctx, req)
		}
		response, err := controlRequest(ctx, h.controllerPath(), req)
		if err == nil && response.OK && req.Method == "PATCH" && req.Path == "/configs" {
			_, err = h.syncDNS(ctx)
			h.dnsError = err
		}
		return response, err
	case privileged.Logs:
		return h.logs(req)
	default:
		return privileged.Response{}, i18n.Errorf("system.privileged.unsupported.helper.operation", "unsupported helper operation")
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
		return privileged.Response{}, i18n.Errorf("system.privileged.invalid.helper.log", "invalid helper log")
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
		h.removeSnapshots()
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
		return privileged.Response{}, i18n.Errorf("system.privileged.orphaned.privileged.engine.prior.root.child.may.still.be.active", "orphaned_privileged_engine: prior root child may still be active")
	}
	if err := h.dnsManager().RestoreIfStopped(ctx, 0); err != nil {
		return privileged.Response{}, i18n.Errorf("system.privileged.restore.prior.tun.dns", "restore prior TUN DNS: %w", err)
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
	snapshot, resources, err := h.prepareConfig(data, req.ConfigPath)
	if err != nil {
		return privileged.Response{}, err
	}
	if err := h.checkNative(ctx, snapshot); err != nil {
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, err
	}
	controller := h.controllerPath()
	if err := os.Remove(controller); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, err
	}
	log, err := os.OpenFile(filepath.Join(h.PrivateDir, "mihomo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, err
	}
	defer log.Close()
	cmd := exec.Command(h.BinaryPath, "--config-policy", "restricted", "-f", snapshot, "-d", h.PrivateDir, "-ext-ctl-unix", controller)
	h.rewriteOwnerBound = h.SessionUID > 0 && rewriteOwnerBootstrapSupported(ctx, h.BinaryPath, h.PrivateDir)
	if h.rewriteOwnerBound {
		cmd.Args = append(cmd.Args, "--request-rewrite-owner-uid="+strconv.Itoa(h.SessionUID))
	}
	cmd.Dir = h.PrivateDir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + h.PrivateDir, "TMPDIR=" + h.PrivateDir}
	cmd.Stdout, cmd.Stderr = log, log
	marker, err := os.OpenFile(h.markerPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, i18n.Errorf("system.privileged.create.child.marker", "create child marker: %w", err)
	}
	if _, err := marker.WriteString("pending\n"); err != nil {
		marker.Close()
		os.Remove(h.markerPath())
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, err
	}
	if err := marker.Close(); err != nil {
		os.Remove(h.markerPath())
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, err
	}
	if err := cmd.Start(); err != nil {
		removeSnapshots(snapshot, resources)
		os.Remove(h.markerPath())
		return privileged.Response{}, i18n.Errorf("system.privileged.start.privileged.mihomo", "start privileged mihomo: %w", err)
	}
	h.child, h.done, h.snapshot = cmd, make(chan error, 1), snapshot
	h.resources = resources
	done := h.done
	go func() {
		done <- cmd.Wait()
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := h.dnsManager().RestoreIfStopped(recoveryCtx, cmd.Process.Pid)
		if err != nil {
			h.mu.Lock()
			h.dnsError = err
			h.mu.Unlock()
		}
	}()
	go h.watchDNS(cmd.Process.Pid)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		// Mihomo creates this socket with broad default permissions. Its
		// parent is root-only; narrow the socket before querying it.
		if info, statErr := os.Lstat(controller); statErr == nil && info.Mode()&os.ModeSocket != 0 {
			if owner, ok := info.Sys().(*syscall.Stat_t); ok && owner.Uid == 0 {
				_ = os.Chmod(controller, 0600)
			}
		}
		if _, err := QueryController(ctx, controller, ControllerVersion); err == nil {
			if _, err := h.syncDNS(ctx); err != nil {
				h.kill()
				_, restoreErr := h.dnsManager().Disable(context.Background())
				return privileged.Response{}, i18n.Errorf("system.privileged.start.tun.dns.restore", "start TUN DNS: %w; restore: %v", err, restoreErr)
			}
			h.dnsError = nil
			return privileged.Response{OK: true, Running: true, PID: cmd.Process.Pid}, nil
		}
		select {
		case <-ctx.Done():
			h.kill()
			return privileged.Response{}, ctx.Err()
		case <-deadline.C:
			h.kill()
			return privileged.Response{}, i18n.Errorf("system.privileged.privileged.mihomo.controller.did.not.become.ready", "privileged mihomo controller did not become ready")
		case <-h.done:
			h.child = nil
			h.removeSnapshots()
			os.Remove(h.markerPath())
			return privileged.Response{}, i18n.Errorf("system.privileged.privileged.mihomo.exited.during.startup", "privileged mihomo exited during startup")
		case <-tick.C:
		}
	}
}

func (h *RuntimeHandler) stop(ctx context.Context) (privileged.Response, error) {
	if !h.status().Running {
		_, err := h.dnsManager().Disable(ctx)
		h.dnsError = err
		if err != nil {
			return privileged.Response{}, err
		}
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
	h.removeSnapshots()
	_ = os.Remove(h.markerPath())
	h.snapshot = ""
	_, err := h.dnsManager().Disable(ctx)
	h.dnsError = err
	if err != nil {
		return privileged.Response{}, err
	}
	return privileged.Response{OK: true}, nil
}

func (h *RuntimeHandler) kill() {
	if h.child != nil {
		_ = h.child.Process.Kill()
		<-h.done
		h.child = nil
	}
	_ = os.Remove(h.controllerPath())
	h.removeSnapshots()
	_ = os.Remove(h.markerPath())
	h.snapshot = ""
}

func (h *RuntimeHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.orphaned {
		return i18n.Errorf("system.privileged.orphaned.privileged.engine.refusing.dns.cleanup.for.unobserved.child", "orphaned_privileged_engine: refusing DNS cleanup for unobserved child")
	}
	if h.child == nil {
		_, err := h.dnsManager().Disable(context.Background())
		return err
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
		Path         string `json:"path"`
		ActivationID string `json:"activation_id"`
	}
	if strictJSON(req.Body, &body) != nil || body.Path == "" {
		return privileged.Response{}, i18n.Errorf("system.privileged.invalid.configuration.reload", "invalid configuration reload")
	}
	data, err := readUserConfig(h.SourceDir, body.Path, h.SessionUID)
	if err != nil {
		return privileged.Response{}, err
	}
	if !validActivationID(req.Body) {
		return privileged.Response{}, i18n.Errorf("system.privileged.invalid.activation.id", "invalid activation_id")
	}
	snapshot, resources, err := h.prepareConfig(data, body.Path)
	if err != nil {
		return privileged.Response{}, err
	}
	if err := h.checkNative(ctx, snapshot); err != nil {
		removeSnapshots(snapshot, resources)
		return privileged.Response{}, err
	}
	encoded, _ := json.Marshal(map[string]string{"path": snapshot, "activation_id": body.ActivationID})
	req.Body = encoded
	// The only reload URL is built here; clients cannot supply an arbitrary
	// controller path or a root-readable configuration filename.
	response, err := controlRequestReload(ctx, h.controllerPath(), req)
	// A timed-out controller request can still apply later. Keep every snapshot
	// and referenced resource until the child has exited.
	h.resources = append(h.resources, h.snapshot)
	h.resources = append(h.resources, resources...)
	h.snapshot = snapshot
	if err == nil && response.OK {
		_, err = h.syncDNS(ctx)
		h.dnsError = err
	}
	return response, err
}

func validActivationID(data []byte) bool {
	var body struct {
		Path         string `json:"path"`
		ActivationID string `json:"activation_id"`
	}
	if strictJSON(data, &body) != nil || len(body.ActivationID) != 32 {
		return false
	}
	_, err := hex.DecodeString(body.ActivationID)
	if err != nil {
		return false
	}
	return strings.ToLower(body.ActivationID) == body.ActivationID
}

func removeSnapshots(snapshot string, resources []string) {
	if snapshot != "" {
		_ = os.Remove(snapshot)
	}
	for _, path := range resources {
		_ = os.Remove(path)
	}
}

func (h *RuntimeHandler) removeSnapshots() {
	removeSnapshots(h.snapshot, h.resources)
	h.snapshot = ""
	h.resources = nil
}

func (h *RuntimeHandler) checkNative(ctx context.Context, snapshot string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, h.BinaryPath, "-t", "--check-json", "--config-policy", "restricted", "-f", snapshot, "-d", h.PrivateDir)
	cmd.Dir = h.PrivateDir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + h.PrivateDir, "TMPDIR=" + h.PrivateDir}
	output, err := cmd.Output()
	var result struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Path    string `json:"path"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(output, &result) != nil {
		return i18n.Errorf("system.privileged.restricted.configuration.failed.native.validation", "restricted configuration failed native validation")
	}
	if !result.Valid || err != nil {
		if len(result.Errors) != 0 {
			issue := result.Errors[0]
			if safeValidationField.MatchString(issue.Path) && safeValidationField.MatchString(issue.Code) && len(issue.Path) <= 128 && len(issue.Code) <= 64 {
				return i18n.Errorf("system.privileged.restricted.configuration.failed.native.validation.at", "restricted configuration failed native validation at %s (%s)", issue.Path, issue.Code)
			}
		}
		return i18n.Errorf("system.privileged.restricted.configuration.failed.native.validation", "restricted configuration failed native validation")
	}
	return nil
}

var safeValidationField = regexp.MustCompile(`^[a-zA-Z0-9_.:\[\]-]+$`)

// readUserConfig traverses every component with openat and O_NOFOLLOW so a
// symlink replacement between request validation and reading cannot redirect
// a root file read. Only regular files under the configured source tree pass.
func readUserConfig(root, path string, uid int) ([]byte, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) || filepath.Clean(root) != root || filepath.Clean(path) != path {
		return nil, i18n.Errorf("system.privileged.invalid.configuration.path", "invalid configuration path")
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
		return nil, i18n.Errorf("system.privileged.configuration.is.outside.session.directory", "configuration is outside session directory")
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
		return nil, i18n.Errorf("system.privileged.configuration.is.not.a.supported.regular.file", "configuration is not a supported regular file")
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
