package menu

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/proxy"
)

type tailscalePathRunner struct {
	calls       [][]string
	status      proxy.TailscalePathStatus
	statusError bool
}

func newTailscalePathRunner() *tailscalePathRunner {
	exit := proxy.TailscalePathPeer{Hostname: "office-exit", TailscaleIPs: []string{"100.64.0.1"}, Online: true, Active: true, ExitNode: true, Path: "derp", Endpoint: "203.0.113.17:41641", DERPRegion: "sfo"}
	return &tailscalePathRunner{status: proxy.TailscalePathStatus{Available: true, State: "ok", BackendState: "Running", SelectedExitNode: &exit, Peers: []proxy.TailscalePathPeer{
		exit,
		{Hostname: "build-host", TailscaleIPs: []string{"100.64.0.2"}, Online: true, Active: true, Path: "direct", Endpoint: "192.0.2.12:41641"},
		{Hostname: "laptop", TailscaleIPs: []string{"100.64.0.3"}, Path: "idle"},
	}}}
}

func (r *tailscalePathRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string(nil), args...))
	switch strings.Join(args, " ") {
	case "proxy tailscale status work":
		if r.statusError {
			return Result{Code: 1, Error: &CLIError{Code: "proxy_tailscale_error", Message: "status unavailable"}}
		}
		return Result{Data: r.status}
	case "proxy tailscale probe work 100.64.0.1":
		return Result{Data: proxy.TailscalePathProbe{PeerIP: "100.64.0.1", NodeName: "office-exit", Transport: "direct", Endpoint: "198.51.100.8:41641", LatencyMS: 8}}
	}
	return Result{}
}
func (*tailscalePathRunner) RunInput(context.Context, []string, io.Reader) Result { return Result{} }

func TestTailscalePathMainPageKeepsExitPathAndActionsClear(t *testing.T) {
	r := newTailscalePathRunner()
	s := NewSessionWithLanguage(r, strings.NewReader(""), io.Discard, i18n.New(i18n.English))
	page := localizedtailscalePathPage(i18n.New(i18n.English), "work")
	view, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Running", "office-exit", "100.64.0.1", "DERP relay · sfo", "This device → DERP relay (sfo) → office-exit"} {
		if !strings.Contains(view.Context, want) {
			t.Errorf("context missing %q: %s", want, view.Context)
		}
	}
	if len(view.Items) != 3 || view.Items[0].ID != "node.tailscaleExitProbe" || view.Items[1].ID != "node.tailscaleRefresh" || view.Items[2].ID != "node.tailscalePeers" {
		t.Fatalf("main choices=%#v", view.Items)
	}
	if strings.Contains(view.Context, "203.0.113.17") {
		t.Fatalf("DERP summary displayed direct endpoint: %s", view.Context)
	}
	if strings.Contains(view.Items[2].Label, "office-exit") {
		t.Fatalf("selected exit duplicated in other-device link: %s", view.Items[2].Label)
	}
	if err := view.Items[0].Action(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	view, err = page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Context, "Last probe · ") || !strings.Contains(view.Context, "office-exit · 100.64.0.1 · DIRECT · Endpoint 198.51.100.8:41641 · 8 ms") || !strings.Contains(view.Context, "This device → DERP relay (sfo) → office-exit") {
		t.Fatalf("probe evidence not distinct and target-labeled: %s", view.Context)
	}
	if strings.Contains(view.Context, "Last probe · "+time.Now().Format("2006-01-02 15:04:05")) {
		t.Fatalf("probe timestamp includes seconds: %s", view.Context)
	}
	readsBefore := len(r.calls)
	if err := view.Items[1].Action(context.Background(), s); err != nil { t.Fatal(err) }
	if len(r.calls) != readsBefore {
		t.Fatalf("refresh action issued a read before page reload: %v", r.calls)
	}
	view, err = page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != readsBefore+1 || !strings.Contains(view.Context, "Last probe · ") {
		t.Fatalf("refresh did not perform one read and retain the labeled probe: calls=%v context=%s", r.calls, view.Context)
	}
	devices, err := view.Items[2].Next().Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices.Items) != 2 || containsLabel(devices.Items, "office-exit") {
		t.Fatalf("other devices=%#v", devices.Items)
	}
}

func TestTailscalePathUnavailableAndUnknownDoNotInventRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status proxy.TailscalePathStatus
		want   string
	}{
		{"not-running", proxy.TailscalePathStatus{State: "ok", Available: false, BackendState: "NeedsLogin"}, "Sign-in required"},
		{"not-started", proxy.TailscalePathStatus{State: "not_started"}, "No selected exit information"},
		{"no-exit", proxy.TailscalePathStatus{State: "ok", Available: true}, "No selected exit information"},
		{"idle", proxy.TailscalePathStatus{State: "ok", Available: true, SelectedExitNode: &proxy.TailscalePathPeer{Hostname: "office-exit", TailscaleIPs: []string{"100.64.0.1"}, Online: true, Path: "idle"}}, "No active path to exit"},
		{"unknown", proxy.TailscalePathStatus{State: "ok", Available: true, SelectedExitNode: &proxy.TailscalePathPeer{Hostname: "office-exit", TailscaleIPs: []string{"100.64.0.1"}, Online: true, Path: "unknown"}}, "Path to exit is unknown"},
		{"offline", proxy.TailscalePathStatus{State: "ok", Available: true, SelectedExitNode: &proxy.TailscalePathPeer{Hostname: "office-exit", TailscaleIPs: []string{"100.64.0.1"}, Online: false, Path: "direct"}}, "Exit is offline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &tailscalePathRunner{status: tc.status}
			s := NewSessionWithLanguage(r, strings.NewReader(""), io.Discard, i18n.New(i18n.English))
			view, err := localizedtailscalePathPage(i18n.New(i18n.English), "work").Load(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(view.Context, tc.want) {
				t.Fatalf("want %q in %q", tc.want, view.Context)
			}
			if tc.name == "idle" || tc.name == "unknown" {
				if len(view.Items) == 0 || view.Items[0].ID != "node.tailscaleExitProbe" || strings.Contains(view.Context, "This device →") {
					t.Fatalf("idle/unknown should remain unasserted but probeable: %+v", view)
				}
			} else if len(view.Items) == 0 || view.Items[0].ID == "node.tailscaleExitProbe" {
				t.Fatalf("unexpected probe action for unavailable exit: %#v", view.Items)
			}
		})
	}
	failed := newTailscalePathRunner()
	failed.statusError = true
	s := NewSessionWithLanguage(failed, strings.NewReader(""), io.Discard, i18n.New(i18n.English))
	view, err := localizedtailscalePathPage(i18n.New(i18n.English), "work").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 || view.Items[0].ID != "node.tailscaleRefresh" || strings.Contains(view.Context, "office-exit") {
		t.Fatalf("failed read retained stale exit or lost refresh: %+v", view)
	}
}

func TestTailscaleMenuRenderedFixtureEnglishAndChinese(t *testing.T) {
	for _, tc := range []struct {
		name     string
		language i18n.Language
		want     []string
	}{{"English", i18n.English, []string{"Tailscale", "Status · Running", "Current exit", "office-exit · 100.64.0.1", "DERP relay · sfo", "This device → DERP relay (sfo) → office-exit", "1. Probe connection to exit", "2. Refresh path status", "3. Other tailnet devices · 2"}}, {"Chinese", i18n.SimplifiedChinese, []string{"状态 · 运行中", "当前出口", "office-exit · 100.64.0.1", "DERP 中继 · sfo", "本机 → DERP 中继 (sfo) → office-exit", "1. 探测到出口的连接", "2. 刷新路径状态", "3. 其他 Tailnet 设备 · 2"}}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTailscalePathRunner()
			var out bytes.Buffer
			language := i18n.New(tc.language)
			s := NewSessionWithLanguage(r, strings.NewReader(""), &out, language)
			page := localizedtailscalePathPage(language, "work")
			view, err := page.Load(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			s.render(page, view, 0, 2)
			rendered := out.String()
			t.Logf("%s production render (plain output):\n%s", tc.name, rendered)
			for _, want := range tc.want {
				if !strings.Contains(rendered, want) {
					t.Errorf("render missing %q:\n%s", want, rendered)
				}
			}
		})
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
