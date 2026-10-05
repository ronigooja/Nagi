package command

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestHelperBackendLifecycleAndControlContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requests := make(chan privileged.Request, 3)
	go func() {
		for i := 0; i < 3; i++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			req, err := privileged.ReadRequest(conn)
			if err != nil {
				conn.Close()
				return
			}
			requests <- req
			resp := privileged.Response{Version: privileged.ProtocolVersion, OK: true}
			switch req.Operation {
			case privileged.Start:
				resp.Running, resp.PID = true, 123
			case privileged.Control:
				resp.Status, resp.Body = 200, json.RawMessage(`{"mixed-port":7890}`)
			}
			_ = privileged.WriteResponse(conn, resp)
			conn.Close()
		}
	}()
	b := helperBackend{profile: "work", configPath: "/tmp/work.yaml", socketPath: path}
	status, err := b.Start(context.Background())
	if err != nil || !status.Running || status.PID != 123 {
		t.Fatalf("start: %+v, %v", status, err)
	}
	var cfg map[string]any
	if err := b.controlClient().Get(context.Background(), "/configs", &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["mixed-port"] != float64(7890) {
		t.Fatalf("config: %+v", cfg)
	}
	status, err = b.Status(context.Background())
	if err != nil || status.Running {
		t.Fatalf("status: %+v, %v", status, err)
	}
	start := <-requests
	if start.Operation != privileged.Start || start.Profile != "work" || start.ConfigPath != "/tmp/work.yaml" || start.BinaryPath != "" || start.SocketPath != "" {
		t.Fatalf("unsafe start request: %+v", start)
	}
	ctl := <-requests
	if ctl.Operation != privileged.Control || ctl.Method != "GET" || ctl.Path != "/configs" || ctl.Profile != "" {
		t.Fatalf("control request: %+v", ctl)
	}
	read := <-requests
	if read.Operation != privileged.Status || read.Profile != "" {
		t.Fatalf("status request: %+v", read)
	}
}

func TestHelperBackendMapsAlreadyRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_, _ = privileged.ReadRequest(conn)
		_ = privileged.WriteResponse(conn, privileged.Response{Version: privileged.ProtocolVersion, Error: "already_running"})
		conn.Close()
	}()
	b := helperBackend{profile: "work", configPath: "/tmp/work.yaml", socketPath: path}
	_, err = b.Start(context.Background())
	if !errors.Is(err, engine.ErrAlreadyRunning) {
		t.Fatalf("error = %v", err)
	}
}
