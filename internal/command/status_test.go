package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/traffic"
)

func TestStatusEnrichmentReadsOverlap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan string, 3)
	release := make(chan struct{})
	defer close(release)
	client := control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
		started <- path
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		response := `{"version":"test-engine"}`
		if path == "/configs" {
			response = `{"mixed-port":7890}`
		}
		return json.Unmarshal([]byte(response), out)
	}, nil)
	data := map[string]any{"running": true, "pid": 123}
	done := make(chan struct{})
	go func() {
		appendStatusReads(ctx, data, true, client, func(ctx context.Context) (traffic.DNSState, error) {
			started <- "dns"
			select {
			case <-release:
			case <-ctx.Done():
				return traffic.DNSState{}, ctx.Err()
			}
			return traffic.DNSState{Supported: true, Managed: true, Status: "managed"}, nil
		})
		close(done)
	}()
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case name := <-started:
			seen[name] = true
		case <-ctx.Done():
			t.Fatal("DNS, /version, and /configs did not start before any read was released")
		}
	}
	// All reads are blocked; merging cannot have begun.
	if !reflect.DeepEqual(data, map[string]any{"running": true, "pid": 123}) {
		t.Fatalf("response changed while reads were pending: %v", data)
	}
	release <- struct{}{}
	release <- struct{}{}
	release <- struct{}{}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("reads did not finish")
	}
	if data["version"] != "test-engine" || data["mixed_port"] != float64(7890) || data["system_dns"].(traffic.DNSState).Status != "managed" {
		t.Fatalf("status enrichment = %v", data)
	}
}

func TestStatusOptionalReadFailuresPreserveFields(t *testing.T) {
	for _, failed := range []string{"/version", "/configs", "both"} {
		t.Run(failed, func(t *testing.T) {
			client := control.NewWithBridge(func(_ context.Context, _, path string, _, out any) error {
				if path == failed || failed == "both" {
					return errors.New("unavailable")
				}
				return json.Unmarshal([]byte(`{"version":"engine","mixed-port":7890}`), out)
			}, nil)
			data := map[string]any{"running": true, "pid": 42, "profile_error": "invalid selection"}
			appendStatusReads(context.Background(), data, true, client, func(context.Context) (traffic.DNSState, error) {
				return traffic.DNSState{Supported: true, Status: "attention"}, errors.New("failed https://private.example/token")
			})
			_, version := data["version"]
			_, port := data["mixed_port"]
			if version != (failed != "/version" && failed != "both") || port != (failed != "/configs" && failed != "both") {
				t.Fatalf("optional fields = %v", data)
			}
			if data["running"] != true || data["pid"] != 42 || data["profile_error"] != "invalid selection" || data["system_dns"].(traffic.DNSState).Status != "attention" {
				t.Fatalf("required fields changed: %v", data)
			}
			if message, ok := data["system_dns_error"].(string); !ok || !strings.Contains(message, "https://private.example/token") {
				t.Fatalf("DNS failure lost actionable URL context: %v", data)
			}
		})
	}
}

func TestStoppedStatusSkipsControlReads(t *testing.T) {
	client := control.NewWithBridge(func(context.Context, string, string, any, any) error {
		t.Error("stopped engine must not be queried")
		return nil
	}, nil)
	data := map[string]any{"running": false, "stale_pid": 42, "unexpected_exit": true}
	appendStatusReads(context.Background(), data, false, client, func(context.Context) (traffic.DNSState, error) {
		return traffic.DNSState{Supported: true, Status: "unmanaged"}, nil
	})
	if _, found := data["version"]; found {
		t.Fatalf("stopped version = %v", data)
	}
	if _, found := data["mixed_port"]; found {
		t.Fatalf("stopped port = %v", data)
	}
	if data["unexpected_exit"] != true || data["system_dns"].(traffic.DNSState).Status != "unmanaged" {
		t.Fatalf("stopped state = %v", data)
	}
}

// Exercise the public JSON command against a Unix control API, including an
// optional API failure, rather than only testing the enrichment helper.
func TestStatusJSONConcurrentAPIReads(t *testing.T) {
	oldOS := tunDNSOS
	tunDNSOS = "linux"
	defer func() { tunDNSOS = oldOS }()
	// Keep the macOS runtime socket path below Darwin's Unix path limit.
	root, err := os.MkdirTemp("/tmp", "ngs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	paths := isolatedTestPaths(t, root)
	if err := paths.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PIDPath, []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if r.URL.Path == "/configs" {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"version":"fixture-engine"}`)
	})}
	go server.Serve(listener)
	defer server.Close()
	defer close(release)
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run([]string{"--json", "status"}, &stdout, &stderr, "test", "commit") }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case path := <-started:
			if path != "/version" && path != "/configs" {
				t.Fatalf("unexpected status query: %s", path)
			}
			seen[path] = true
		case <-timer.C:
			t.Fatal("public status did not overlap version/config API reads")
		}
	}
	release <- struct{}{}
	release <- struct{}{}
	select {
	case code := <-done:
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
		}
	case <-timer.C:
		t.Fatal("status did not finish")
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data["running"] != true || envelope.Data["pid"] != float64(os.Getpid()) || envelope.Data["version"] != "fixture-engine" {
		t.Fatalf("status JSON = %s", stdout.String())
	}
	if _, found := envelope.Data["mixed_port"]; found {
		t.Fatalf("failed optional config field present: %s", stdout.String())
	}
	if _, found := envelope.Data["system_dns"]; found {
		t.Fatalf("non-macOS DNS field present: %s", stdout.String())
	}
}
