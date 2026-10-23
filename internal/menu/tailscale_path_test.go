package menu

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
)

type tailscalePathRunner struct{ calls [][]string }

func (r *tailscalePathRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string(nil), args...))
	if strings.Join(args, " ") == "proxy tailscale status work" {
		return Result{Data: map[string]any{"available": true, "state": "ok", "backend_state": "Running", "selected_exit_node": map[string]any{"hostname": "office-exit", "tailscale_ips": []string{"100.64.0.1"}, "path": "derp", "endpoint": "192.0.2.8:41641", "derp_region": "sea"}, "peers": []any{map[string]any{"hostname": "build-host", "tailscale_ips": []string{"100.64.0.2"}, "path": "direct", "endpoint": "192.0.2.12:41641"}, map[string]any{"hostname": "laptop", "path": "idle"}}}}
	}
	if strings.Join(args, " ") == "proxy tailscale probe work 100.64.0.2" {
		return Result{Data: map[string]any{"peer_ip": "100.64.0.2", "node_name": "build-host", "transport": "direct", "endpoint": "192.0.2.12:41641", "latency_ms": 8}}
	}
	return Result{}
}

func TestTailscaleMenuRenderedFixture(t *testing.T) {
	r := &tailscalePathRunner{}
	var out bytes.Buffer
	s := NewSessionWithLanguage(r, strings.NewReader(""), &out, i18n.New(i18n.English))
	status, err := localizedtailscalePathPage(i18n.New(i18n.English), "work").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	s.render(&Page{Title: "Tailscale path status"}, status, 0, 2)
	t.Logf("PASSIVE MENU\n%s", out.String())
	out.Reset()
	peer := status.Items[0].Next()
	view, err := peer.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	s.render(peer, view, 0, 3)
	t.Logf("PEER DETAIL\n%s", out.String())
	if len(view.Actions) != 1 {
		t.Fatalf("expected explicit probe action, got %d", len(view.Actions))
	}
	out.Reset()
	if err := view.Actions[0].Action(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	view, err = peer.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	s.render(peer, view, 0, 3)
	t.Logf("AFTER PROBE\n%s", out.String())
	if !strings.Contains(view.Context, "Last probe · ") || strings.Contains(view.Context, "Last probe · "+time.Now().Format("2006-01-02 15:04:05")) {
		t.Fatalf("probe history lacks minute precision: %q", view.Context)
	}
	out.Reset()
	zh := i18n.New(i18n.SimplifiedChinese)
	zhSession := NewSessionWithLanguage(r, strings.NewReader(""), &out, zh)
	zhView, err := localizedtailscalePathPage(zh, "work").Load(context.Background(), zhSession)
	if err != nil {
		t.Fatal(err)
	}
	zhSession.render(&Page{Title: "Tailscale 路径状态"}, zhView, 0, 2)
	t.Logf("CHINESE MENU\n%s", out.String())
}
func (*tailscalePathRunner) RunInput(context.Context, []string, io.Reader) Result { return Result{} }

func TestTailscalePathPageShowsPassiveSnapshotWithoutInventedLatency(t *testing.T) {
	r := &tailscalePathRunner{}
	s := NewSessionWithLanguage(r, strings.NewReader(""), io.Discard, i18n.New(i18n.English))
	v, err := localizedtailscalePathPage(i18n.New(i18n.English), "work").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"READY", "Selected exit", "office-exit", "DERP", "sea", "DIRECT", "build-host", "IDLE", "laptop"} {
		if !strings.Contains(v.Context, want) && !containsLabel(v.Items, want) {
			t.Errorf("missing %q from context/items: %q %#v", want, v.Context, v.Items)
		}
	}
	if strings.Contains(v.Context, "192.0.2.8") || strings.Contains(v.Context, "ms") {
		t.Fatalf("passive status rendered endpoint for DERP or invented latency: %q", v.Context)
	}
	if len(r.calls) != 1 || strings.Join(r.calls[0], " ") != "proxy tailscale status work" {
		t.Fatalf("calls=%v", r.calls)
	}
}
func containsLabel(items []Item, want string) bool {
	for _, item := range items {
		if strings.Contains(item.Label, want) {
			return true
		}
	}
	return false
}
