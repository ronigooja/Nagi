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
	"syscall"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
)

// Exercise the complete authenticated helper path while the root controller
// holds a traffic response open. The fake child never starts an engine or
// invokes DNS recovery, so this fixture is safe on a running macOS installation.
func TestAuthenticatedRuntimeSnapshotsBypassSlowTraffic(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := runtimeController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/traffic" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprintln(w, `{"up":1,"down":2,"upTotal":3,"downTotal":4}`)
			return
		}
		fmt.Fprintln(w, `{"version":"fixture","mixed-port":7890}`)
	})
	defer close(release)
	if err := os.Chmod(h.PrivateDir, 0711); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.PrivateDir, "helper.sock")
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- (Server{SessionUID: uid, Handler: h}).Serve(ctx, listener) }()
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(h.PrivateDir, "runtime-client-test")
	if err := os.WriteFile(childPath, binary, 0755); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(childPath, "-test.run=^TestAuthenticatedRuntimeClient$")
	child.Env = append(os.Environ(), "NAGI_TEST_RUNTIME_SOCKET="+path)
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
	child.Stderr = os.Stderr
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill() }()
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	waitSignal(t, started)
	// Only let the client issue snapshots once the controller has blocked.
	if _, err := fmt.Fprintln(input, "controller-blocked"); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("authenticated client exited before snapshots completed")
			}
			if line == "ready" {
				goto complete
			}
			t.Log(line)
		case <-deadline.C:
			t.Fatal("authenticated snapshots waited for the traffic sample")
		}
	}
complete:
	release <- struct{}{}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("helper did not drain after authenticated requests completed")
	}
}

func TestAuthenticatedRuntimeClient(t *testing.T) {
	path := os.Getenv("NAGI_TEST_RUNTIME_SOCKET")
	if path == "" {
		t.Skip("authenticated subprocess fixture")
	}
	request := func(req privileged.Request) privileged.Response {
		t.Helper()
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		req.Version = privileged.ProtocolVersion
		if err := privileged.WriteRequest(conn, req); err != nil {
			t.Fatal(err)
		}
		response, err := privileged.ReadResponse(conn)
		if err != nil || !response.OK {
			t.Fatalf("authenticated %s %s: %+v, %v", req.Operation, req.Path, response, err)
		}
		return response
	}
	// Send the traffic request before issuing snapshots. The parent additionally
	// waits for the fake controller to observe it before accepting completion.
	traffic, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer traffic.Close()
	_ = traffic.SetDeadline(time.Now().Add(5 * time.Second))
	if err := privileged.WriteRequest(traffic, privileged.Request{Version: privileged.ProtocolVersion, Operation: privileged.Control, Method: "GET", Path: "/traffic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	for _, req := range []privileged.Request{{Operation: privileged.Status}, {Operation: privileged.Control, Method: "GET", Path: "/version"}, {Operation: privileged.Control, Method: "GET", Path: "/configs"}} {
		start := time.Now()
		response := request(req)
		if req.Operation == privileged.Status && !response.Running {
			t.Fatal("fake child liveness was lost")
		}
		fmt.Printf("authenticated %s %s: %s\n", req.Operation, req.Path, time.Since(start))
	}
	fmt.Println("ready")
	response, err := privileged.ReadResponse(traffic)
	if err != nil || !response.OK {
		t.Fatalf("traffic sample failed after concurrent snapshots: %+v, %v", response, err)
	}
}
