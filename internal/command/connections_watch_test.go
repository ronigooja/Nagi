package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestConnectionsWatchWebSocketSnapshotsAndLimit(t *testing.T) {
	for _, size := range []int{32, control.MaxResponseBytes, control.MaxResponseBytes + 1} {
		path := trafficServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/connections" || r.URL.Query().Get("interval") != "250" {
				t.Error(r.URL)
			}
			ws, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer ws.CloseNow()
			body := `{"connections":[]}` + strings.Repeat(" ", size-len(`{"connections":[]}`))
			if err := ws.Write(r.Context(), websocket.MessageText, []byte(body)); err != nil {
				return
			}
			// A second sample proves line framing and retention of intermediate snapshots.
			_ = ws.Write(r.Context(), websocket.MessageText, []byte(`{"connections":[{"id":"brief"}]}`))
			_, _, _ = ws.Read(r.Context())
		})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		var out, errOut bytes.Buffer
		writer := &sampleCancelWriter{Writer: &out, cancel: cancel, remaining: 2}
		code := watchConnections(ctx, control.New(path), 250*time.Millisecond, writer, &errOut, true)
		cancel()
		if size > control.MaxResponseBytes {
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "response_too_large") {
				t.Fatalf("code %d output %d error %s", code, out.Len(), &errOut)
			}
			continue
		}
		if code != 0 || errOut.Len() != 0 {
			t.Fatalf("code %d error %s", code, &errOut)
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 2 {
			t.Fatal(out.String())
		}
		for _, line := range lines {
			var envelope struct {
				OK   bool
				Data struct {
					Connections []any  `json:"connections"`
					SampledAt   string `json:"sampled_at"`
				}
			}
			if err := json.Unmarshal([]byte(line), &envelope); err != nil || !envelope.OK || envelope.Data.Connections == nil {
				t.Fatal(line, err)
			}
			if _, err := time.Parse(time.RFC3339Nano, envelope.Data.SampledAt); err != nil {
				t.Fatal(err)
			}
		}
	}
}

type sampleCancelWriter struct {
	io.Writer
	cancel    context.CancelFunc
	remaining int
}

func (w *sampleCancelWriter) Write(p []byte) (int, error) {
	n, e := w.Writer.Write(p)
	w.remaining--
	if w.remaining == 0 {
		w.cancel()
	}
	return n, e
}

type brokenConnectionWriter struct{}

func (brokenConnectionWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestConnectionsWatchBridgeTextOutputFailureAndCancellation(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		client := control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
			calls++
			if method != "GET" || path != "/connections" {
				t.Fatal(method, path)
			}
			return json.Unmarshal([]byte(`{"connections":[]}`), out)
		}, nil)
		var out, errOut bytes.Buffer
		writer := &sampleCancelWriter{Writer: &out, cancel: cancel, remaining: 1}
		if code := watchConnections(ctx, client, 250*time.Millisecond, writer, &errOut, jsonMode); code != 0 || calls != 1 {
			t.Fatal(code, calls, &errOut)
		}
		cancel()
		if !jsonMode && !strings.Contains(out.String(), "No active connections") {
			t.Fatal(out.String())
		}
		errOut.Reset()
		if code := watchConnections(context.Background(), client, 250*time.Millisecond, brokenConnectionWriter{}, &errOut, jsonMode); code != 1 || !strings.Contains(errOut.String(), "output_error") {
			t.Fatal(code, &errOut)
		}
	}
}
func TestConnectionsWatchHelpValidationAndCompletion(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{{"connections", "watch", "--help"}, {"connections", "watch", "--interval", "249"}, {"connections", "watch", "--interval", "60001"}, {"connections", "watch", "--interval"}, {"connections", "watch", "--invalid", "250"}} {
		var out, errOut bytes.Buffer
		want := 2
		if args[2] == "--help" {
			want = 0
		}
		if code := Run(args, &out, &errOut, "test", "commit"); code != want {
			t.Fatal(args, code, &out, &errOut)
		}
	}
	for _, s := range []string{"250", "60000"} {
		if _, err := connectionsInterval([]string{"connections", "watch", "--interval", s}); err != nil {
			t.Fatal(err)
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, _ := completion(shell)
		if !strings.Contains(script, "watch") || !strings.Contains(script, "interval") {
			t.Fatal(shell)
		}
	}
}

func TestConnectionsWatchBridgeOverflowPausesWithoutPartialOutput(t *testing.T) {
	client := control.NewWithBridge(func(ctx context.Context, method, path string, body, out any) error {
		raw := out.(*json.RawMessage)
		*raw = json.RawMessage(`{"connections":[]}` + strings.Repeat(" ", control.MaxResponseBytes))
		return nil
	}, nil)
	var out, errOut bytes.Buffer
	if code := watchConnections(context.Background(), client, 250*time.Millisecond, &out, &errOut, true); code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "response_too_large") {
		t.Fatal(code, out.Len(), errOut.String())
	}
}

func TestConnectionsWatchChineseText(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := context.WithValue(parent, languageContextKey{}, i18n.New(i18n.SimplifiedChinese))
	client := control.NewWithBridge(func(_ context.Context, _, _ string, _, out any) error {
		return json.Unmarshal([]byte(`{"connections":null}`), out)
	}, nil)
	var out, errOut bytes.Buffer
	writer := &sampleCancelWriter{Writer: &out, cancel: cancel, remaining: 1}
	if code := watchConnections(ctx, client, 250*time.Millisecond, writer, &errOut, false); code != 0 || !strings.Contains(out.String(), "采样：") || strings.Contains(out.String(), "No active connections") {
		t.Fatal(code, out.String(), errOut.String())
	}
}
