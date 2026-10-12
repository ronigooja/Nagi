package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func secureTestDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root-controlled socket ancestors")
	}
	root := "/var/lib"
	if runtime.GOOS == "darwin" {
		root = "/Library"
	}
	dir, err := os.MkdirTemp(root, "ng-c-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func runtimeController(t *testing.T, handler http.HandlerFunc) *RuntimeHandler {
	t.Helper()
	dir := secureTestDir(t)
	path := filepath.Join(dir, "mihomo.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return &RuntimeHandler{PrivateDir: dir, child: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, done: make(chan error, 1)}
}

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for synchronization")
	}
}

func TestTrafficDoesNotBlockRuntimeSnapshots(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := runtimeController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/traffic" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Write([]byte("{\"up\":1,\"down\":2}\n"))
			return
		}
		w.Write([]byte(`{"version":"test"}`))
	})
	defer close(release)
	finished := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/traffic"})
		finished <- err
	}()
	waitSignal(t, started)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, req := range []privileged.Request{{Operation: privileged.Status}, {Operation: privileged.Control, Method: "GET", Path: "/version"}, {Operation: privileged.Control, Method: "GET", Path: "/configs"}} {
		done := make(chan error, 1)
		go func() { _, err := h.Handle(ctx, 0, req); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("traffic blocked a snapshot")
		}
	}
	// A successful config mutation invalidates the still-pending traffic result.
	if _, err := h.Handle(ctx, 0, privileged.Request{Operation: privileged.Control, Method: "PATCH", Path: "/configs", Body: []byte(`{"mode":"direct"}`)}); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	if err := <-finished; err == nil || !strings.Contains(err.Error(), "stale_controller_response") {
		t.Fatalf("stale result accepted: %v", err)
	}
}

func TestProxySelectionDoesNotInterruptTraffic(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := runtimeController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/traffic" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Write([]byte("{\"up\":1,\"down\":2}\n"))
			return
		}
		w.Write([]byte(`{}`))
	})
	defer close(release)
	finished := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/traffic"})
		finished <- err
	}()
	waitSignal(t, started)
	for i := 0; i < 10; i++ {
		if _, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/proxies/select", Body: []byte(`{"name":"node"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/unauthorized"}); err == nil {
		t.Fatal("unauthorized endpoint accepted")
	}
	release <- struct{}{}
	if err := <-finished; err != nil {
		t.Fatalf("node selection interrupted traffic: %v", err)
	}
}

func TestTrafficControllerReadCancels(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	h := runtimeController(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(canceled) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := h.Handle(ctx, 0, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/traffic"})
		finished <- err
	}()
	waitSignal(t, started)
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled controller read succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("controller read did not cancel")
	}
	waitSignal(t, canceled)
}

func TestTrafficReplyRejectedAfterEngineExit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := runtimeController(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte("{\"up\":0,\"down\":0}\n"))
	})
	defer close(release)
	finished := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/traffic"})
		finished <- err
	}()
	waitSignal(t, started)
	h.done <- nil
	response, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Status})
	if err != nil || response.Running {
		t.Fatalf("exit status: %+v, %v", response, err)
	}
	release <- struct{}{}
	if err := <-finished; err == nil || !strings.Contains(err.Error(), "stale_controller_response") {
		t.Fatalf("exit result accepted: %v", err)
	}
}

func TestMeasurementAndLifecycleStaySerialized(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := runtimeController(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(`{"delay":1}`))
	})
	finished := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/group/auto/delay?url=https%3A%2F%2Fexample.com&timeout=100"})
		finished <- err
	}()
	waitSignal(t, started)
	stopped := make(chan struct{})
	// Mark an already-exited fake process: stop must wait for measurement before
	// consuming lifecycle completion, never signal the test process.
	h.done <- nil
	go func() {
		_, _ = h.Handle(context.Background(), 0, privileged.Request{Operation: privileged.Stop})
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("lifecycle bypassed measurement mutation lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	waitSignal(t, stopped)
}

func TestSnapshotClassificationKeepsSideEffectsSerial(t *testing.T) {
	for _, path := range []string{"/group/auto/delay?url=https://example.com&timeout=100", "/proxies/node/delay?url=https://example.com&timeout=100", "/providers/proxies/provider/healthcheck", "/providers/proxies/provider/node/healthcheck?url=https://example.com&timeout=100", "/dns/query?name=example.com&type=A", "/unknown", "/configs?unsafe=1"} {
		if snapshotControl(privileged.Request{Method: "GET", Path: path}) {
			t.Errorf("side-effect/unauthorized endpoint concurrent: %s", path)
		}
	}
}

type drainingHandler struct{ entered, canceled, release chan struct{} }

func (h *drainingHandler) Handle(ctx context.Context, _ int, req privileged.Request) (privileged.Response, error) {
	if req.Operation == privileged.Status {
		return privileged.Response{OK: true}, nil
	}
	close(h.entered)
	<-ctx.Done()
	close(h.canceled)
	<-h.release
	return privileged.Response{}, ctx.Err()
}

func TestServerConcurrentAndDrainsOnCancellation(t *testing.T) {
	dir := secureTestDir(t)
	// The subprocess is the authenticated non-root session client.
	if err := os.Chmod(dir, 0711); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "helper.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	const uid = 65534
	if err := os.Chown(path, uid, -1); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	h := &drainingHandler{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- (Server{SessionUID: uid, Handler: h}).Serve(ctx, listener) }()
	// A copy outside /root allows the session user to execute this test binary.
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(dir, "client-test")
	if err := os.WriteFile(childPath, binary, 0755); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(childPath, "-test.run=^TestHelperConcurrentClient$")
	child.Env = append(os.Environ(), "NAGI_TEST_HELPER_SOCKET="+path)
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	waitSignal(t, h.entered)
	select {
	case line := <-ready:
		if line != "ready\n" {
			t.Fatalf("client failed before ready: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("status could not bypass blocked connection")
	}
	cancel()
	waitSignal(t, h.canceled)
	select {
	case <-result:
		t.Fatal("Serve returned before draining handler")
	default:
	}
	close(h.release)
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve failed to drain")
	}
}

func TestHelperConcurrentClient(t *testing.T) {
	path := os.Getenv("NAGI_TEST_HELPER_SOCKET")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	blocked, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	if err := privileged.WriteRequest(blocked, privileged.Request{Version: privileged.ProtocolVersion, Operation: privileged.Logs}); err != nil {
		t.Fatal(err)
	}
	quick, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer quick.Close()
	quick.SetDeadline(time.Now().Add(3 * time.Second))
	if err := privileged.WriteRequest(quick, privileged.Request{Version: privileged.ProtocolVersion, Operation: privileged.Status}); err != nil {
		t.Fatal(err)
	}
	response, err := privileged.ReadResponse(quick)
	if err != nil || !response.OK {
		t.Fatalf("concurrent status: %+v, %v", response, err)
	}
	// Leave an incomplete request pending, exercising cancellation of
	// socket decoding independently of the blocked handler.
	idle, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	fmt.Println("ready")
	idle.SetReadDeadline(time.Now().Add(3 * time.Second))
	var b [1]byte
	if _, err := idle.Read(b[:]); err == nil {
		t.Fatal("idle socket received unexpected response")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("idle socket not canceled")
	}
}
