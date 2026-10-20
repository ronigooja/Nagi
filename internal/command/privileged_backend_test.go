package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/proxy"
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

func TestHelperBackendMapsResponseTooLarge(t *testing.T) {
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
		defer conn.Close()
		_, _ = privileged.ReadRequest(conn)
		_ = privileged.WriteResponse(conn, privileged.Response{Version: privileged.ProtocolVersion, Error: "response_too_large"})
	}()
	err = (helperBackend{socketPath: path}).controlClient().Get(context.Background(), "/connections", nil)
	if !errors.Is(err, control.ErrResponseTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestHelperBackendLargeConnectionLists(t *testing.T) {
	for _, size := range []int{(1 << 20) + 1, control.MaxResponseBytes} {
		t.Run(fmt.Sprintf("bytes_%d", size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper.sock")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			prefix := `{"connections":[{"id":"one","metadata":{"processPath":"`
			suffix := `"},"upload":2,"download":3}]}`
			payloadSize := size - len(prefix) - len(suffix)
			body := prefix + strings.Repeat("a", payloadSize) + suffix
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = privileged.ReadRequest(conn)
				_ = privileged.WriteResponse(conn, privileged.Response{Version: privileged.ProtocolVersion, OK: true, Status: 200, Body: json.RawMessage(body)})
			}()
			connections, err := proxy.NewService((helperBackend{socketPath: path}).controlClient()).Connections(context.Background())
			if err != nil || len(connections) != 1 {
				t.Fatalf("connection list: count=%d err=%v", len(connections), err)
			}
			if connections[0].ID != "one" || len(connections[0].Metadata["processPath"].(string)) != payloadSize {
				t.Fatal("helper connection data was truncated or changed")
			}
		})
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

func TestHelperReloadPreForwardRejectionSkipsActivationPolling(t *testing.T) {
	for _, marked := range []bool{false, true} {
		t.Run(fmt.Sprint(marked), func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "helper.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			reads := 0
			go func() {
				defer close(done)
				id := ""
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					request, err := privileged.ReadRequest(conn)
					if err != nil {
						conn.Close()
						return
					}
					var response privileged.Response
					if request.Method == "PUT" {
						var body struct {
							ActivationID string `json:"activation_id"`
						}
						_ = json.Unmarshal(request.Body, &body)
						id = body.ActivationID
						response = privileged.Response{Version: privileged.ProtocolVersion, Error: "restricted validation rejected at proxies[0].dialer-proxy", NotForwarded: marked}
					} else {
						reads++
						body, _ := json.Marshal(map[string]string{"activation_id": id})
						response = privileged.Response{Version: privileged.ProtocolVersion, OK: true, Status: 200, Body: body}
					}
					_ = privileged.WriteResponse(conn, response)
					conn.Close()
				}
			}()
			err = activateConfig(context.Background(), (helperBackend{socketPath: socket}).controlClient(), "/fixture.yaml")
			listener.Close()
			<-done
			var notForwarded *helperControlNotForwardedError
			if marked {
				if !errors.As(err, &notForwarded) || errors.Is(err, errActivationMayStillApply) || reads != 0 || !strings.Contains(err.Error(), "proxies[0].dialer-proxy") {
					t.Fatalf("reads=%d error=%v", reads, err)
				}
			} else if err != nil || reads == 0 {
				t.Fatalf("unmarked error must still observe the activation ID: reads=%d error=%v", reads, err)
			}
		})
	}
}
