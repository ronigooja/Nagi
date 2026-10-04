package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/engine"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/service"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestHumanSuccessShapes(t *testing.T) {
	updated := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		data any
		want []string
		not  []string
	}{
		{"start", engine.Status{Running: true, PID: 42, SocketPath: "/tmp/mihomo.sock", LogPath: "/tmp/mihomo.log"}, []string{"Mihomo: running", "PID: 42", "Socket: /tmp/mihomo.sock"}, nil},
		{"stop", engine.Status{SocketPath: "/tmp/mihomo.sock"}, []string{"Mihomo: stopped"}, nil},
		{"unexpected exit", engine.Status{StalePID: 42, UnexpectedExit: true}, []string{"Previous PID: 42", "nagi logs"}, nil},
		{"status", map[string]any{"running": true, "profile": "work", "pid": 42, "version": "v1", "mixed_port": 17890, "socket_path": "/s", "log_path": "/l"}, []string{"Mihomo: running", "Profile: work", "Mixed port: 17890"}, nil},
		{"status unavailable", map[string]any{"running": false, "profile": "default", "socket_path": "/s", "log_path": "/l"}, []string{"Mihomo: stopped", "Profile: default"}, []string{"Mihomo version", "Mixed port"}},
		{"profile list", map[string]any{"profiles": []string{"default", "work"}, "current": "work"}, []string{"  default", "* work"}, nil},
		{"profile empty", map[string]any{"profiles": []string{}, "current": "default"}, []string{"No profiles found", "nagi profile import"}, nil},
		{"profile use", map[string]any{"current": "work"}, []string{"Selected profile work", "If mihomo was running"}, []string{"started"}},
		{"profile import", map[string]any{"name": "work", "imported": true}, []string{"Imported profile work", "nagi profile use work"}, nil},
		{"config valid", map[string]any{"valid": true, "profile": "work"}, []string{"Profile work is valid"}, nil},
		{"subscription list", map[string]any{"subscriptions": []subscription.Entry{{Name: "fresh"}, {Name: "cached", UpdatedAt: updated}}}, []string{"fresh  Never updated", "cached  Updated: 2026-10-04"}, nil},
		{"subscription empty", map[string]any{"subscriptions": []subscription.Entry{}}, []string{"No subscriptions configured", "nagi subscription add"}, nil},
		{"subscription add", map[string]any{"name": "my-sub"}, []string{"Saved subscription my-sub", "nagi subscription update my-sub"}, []string{"Downloaded"}},
		{"subscription update", subscription.RefreshResult{Name: "my-sub", Bytes: 123, UpdatedAt: updated}, []string{"Downloaded subscription my-sub (123 bytes)", "nagi subscription apply my-sub"}, nil},
		{"subscription remove", map[string]any{"name": "my-sub", "removed": true}, []string{"Removed subscription my-sub"}, nil},
		{"subscription apply", map[string]any{"name": "my-sub", "profile": "my-sub", "applied": true}, []string{"selected profile my-sub", "If mihomo was running"}, []string{"started"}},
		{"proxy groups", map[string]any{"groups": []proxy.Group{{Name: "Main", Type: "Selector", Now: "B", All: []string{"A", "B"}}}}, []string{"Main (Selector) -> B", "  A", "* B"}, []string{"down", "up"}},
		{"proxy groups empty", map[string]any{"groups": []proxy.Group{}}, []string{"No proxy groups configured", "nagi config show"}, []string{"Start mihomo"}},
		{"proxy show", proxy.Group{Name: "Main", Now: "A", All: []string{"A", "B"}}, []string{"Main -> A", "* A", "  B"}, nil},
		{"proxy show empty", proxy.Group{Name: "Main"}, []string{"No nodes available"}, nil},
		{"proxy select", map[string]any{"group": "Main", "node": "A"}, []string{"Selected A in proxy group Main"}, nil},
		{"connections", map[string]any{"connections": []proxy.Connection{{ID: "1", Metadata: map[string]any{"host": "example.org", "destinationPort": "443"}, Upload: 12, Download: 34}, {ID: "2", Metadata: map[string]any{"destinationIP": "203.0.113.1", "destinationPort": 80}}}}, []string{"example.org:443", "ID: 1", "Upload: 12 B", "Download: 34 B", "ID: 2", "203.0.113.1:80"}, nil},
		{"connections empty", map[string]any{"connections": []proxy.Connection{}}, []string{"No active connections"}, nil},
		{"service", service.Result{Manager: "systemd", Path: "/tmp/nagi.service"}, []string{"Service manager: systemd", "Service file: /tmp/nagi.service"}, nil},
		{"version", map[string]any{"nagi": "1.0", "mihomo": "2.0", "mihomo_commit": "abc", "os": "linux", "arch": "arm64"}, []string{"Nagi: 1.0", "Mihomo: 2.0", "Mihomo commit: abc", "OS: linux", "Architecture: arm64"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Write(&out, false, tt.data); err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q missing %q", out.String(), want)
				}
			}
			for _, excluded := range tt.not {
				if strings.Contains(out.String(), excluded) {
					t.Errorf("output %q contains %q", out.String(), excluded)
				}
			}
		})
	}
}

func TestRawContent(t *testing.T) {
	for _, tt := range []struct {
		name string
		data any
		want string
	}{
		{"logs", map[string]any{"lines": []string{"first", "", "third"}}, "first\n\nthird\n"},
		{"logs empty", map[string]any{"lines": []string{}}, "No log lines available.\n"},
		{"yaml", map[string]any{"profile": "default", "yaml": "a: 1\n\nb: 2\n"}, "a: 1\n\nb: 2\n"},
		{"yaml missing newline", map[string]any{"yaml": "a: 1"}, "a: 1\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Write(&out, false, tt.data); err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("got %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestJSONPreservesShapeAndPrivacy(t *testing.T) {
	for _, tt := range []struct {
		name string
		data any
		want string
	}{
		{"subscription", map[string]any{"subscriptions": []subscription.Entry{{Name: "private", URL: "https://secret.example/token"}}}, `{"ok":true,"data":{"subscriptions":[{"name":"private"}]}}`},
		{"refresh", subscription.RefreshResult{Name: "private", Bytes: 2, UpdatedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}, `{"ok":true,"data":{"name":"private","bytes":2,"updated_at":"2026-10-04T00:00:00Z"}}`},
		{"group", proxy.Group{Name: "Main", Type: "Selector", Alive: false}, `{"ok":true,"data":{"name":"Main","type":"Selector","alive":false}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Write(&out, true, tt.data); err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out.String()) != tt.want {
				t.Fatalf("got %q, want %q", out.String(), tt.want)
			}
			if !json.Valid(out.Bytes()) {
				t.Fatal("invalid JSON")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

func TestWritePropagatesWriterFailure(t *testing.T) {
	for _, data := range []any{
		engine.Status{Running: true}, proxy.Group{Name: "Main", All: []string{"A"}},
		service.Result{Path: "/p"}, subscription.RefreshResult{Name: "s"},
		map[string]any{"yaml": "key: value"}, map[string]any{"groups": []proxy.Group{{Name: "Main"}}},
		map[string]any{"connections": []proxy.Connection{{}}}, map[string]any{"subscriptions": []subscription.Entry{{Name: "s"}}},
		map[string]any{"profiles": []string{"default"}}, map[string]any{"running": true},
	} {
		if err := Write(failingWriter{}, false, data); err == nil {
			t.Fatalf("missing writer error for %T", data)
		}
	}
	if err := Write(failingWriter{}, true, map[string]any{"name": "s"}); err == nil {
		t.Fatal("missing JSON writer error")
	}
}
