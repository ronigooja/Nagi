package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestEngineLatencyDisplayedWithoutImplicitMeasurement(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "group", true: "provider"}[provider], func(t *testing.T) {
			delays := map[string]any{
				"Fast":   map[string]any{"status": "ok", "delay_ms": 9, "measured_at": "2026-10-16T20:00:00Z", "test_url": "https://example.invalid"},
				"Failed": map[string]any{"status": "failed", "measured_at": "2026-10-16T20:01:00Z"},
				"New":    map[string]any{"status": "untested"},
			}
			key := "proxy show Group"
			page := proxyGroupPage("Group")
			data := map[string]any{"all": []string{"Fast", "Failed", "New"}, "now": "Fast", "node_delays": delays}
			if provider {
				key = "proxy provider show Group"
				page = providerDetailPage("Group")
				data["nodes"] = data["all"]
			}
			r := &fakeRunner{results: map[string]Result{key: {Data: data}}}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(r, strings.NewReader("0\n"), &out), page)
			if !reflect.DeepEqual(r.calls, [][]string{strings.Split(key, " ")}) {
				t.Fatalf("page initiated extra work: %v", r.calls)
			}
			for _, want := range []string{"Fast — 9 ms", "Failed — latency test failed", "New — untested"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
		})
	}
}

func TestLatestEngineFailureOverridesBatchValueKeepsOrdering(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{
		"proxy show Group":   {Data: map[string]any{"all": []string{"A", "B"}}},
		"proxy delays Group": {Data: map[string]any{"results": []any{map[string]any{"proxy": "B", "delay_ms": 1}, map[string]any{"proxy": "A", "delay_ms": 2}}}},
	}}
	s := NewSession(r, strings.NewReader(""), &bytes.Buffer{})
	page := proxyGroupPage("Group")
	view, _ := page.Load(context.Background(), s)
	for _, a := range view.Actions {
		if a.Label == "Test all nodes and select from results" {
			if err := a.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.results["proxy show Group"] = Result{Data: map[string]any{"all": []string{"A", "B"}, "node_delays": map[string]any{"B": map[string]any{"status": "failed"}, "A": map[string]any{"status": "ok", "delay_ms": 8}}}}
	view, _ = page.Load(context.Background(), s)
	if view.Items[0].Name != "B" || !strings.Contains(view.Items[0].Label, "failed") || !strings.Contains(view.Items[1].Label, "8 ms") {
		t.Fatalf("latest results=%#v", view.Items)
	}
}

func TestMeasurementDetailsShowTimeAndOnlyEndpoint(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	s.ShowResult([]string{"proxy", "show", "Group"}, Result{Data: map[string]any{"node_delays": map[string]any{"Node": map[string]any{"status": "ok", "delay_ms": 5, "measured_at": "2026-10-16T20:00:00Z", "test_url": "https://user:secret@example.invalid/private?token=secret"}}}})
	for _, want := range []string{"2026-10-16T20:00:00Z", "example.invalid (https)", "test endpoint"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	for _, secret := range []string{"user:", "secret", "private", "token"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("exposed %q: %s", secret, out.String())
		}
	}
}

func TestHealthcheckRetainedFormAndRestorationJourney(t *testing.T) {
	for _, kind := range []string{"group", "provider"} {
		t.Run(kind, func(t *testing.T) {
			r := &sequenceRunner{results: map[string][]Result{
				"proxy healthcheck show " + kind + " Work":    {{Data: map[string]any{"profile": "work", "interval_seconds": 300, "enabled": true, "lazy": true, "source": "profile", "test_url": "https://example.invalid"}}},
				"proxy healthcheck set " + kind + " Work 60":  {{Code: 1, Error: &CLIError{Message: "activation failed; inspect profile"}}},
				"proxy healthcheck set " + kind + " Work 120": {{Data: map[string]any{"source": "override"}}},
				"proxy healthcheck reset " + kind + " Work":   {{Data: map[string]any{"source": "profile"}}},
			}}
			var out bytes.Buffer
			// Invalid seconds remain in the form. A failed submit requires explicit retry.
			RunPage(context.Background(), NewSession(r, strings.NewReader("1\n0\n31536001\n60\n1\n120\n2\n2\n0\n"), &out), healthcheckPage(kind, "Work"))
			counts := map[string]int{}
			for _, call := range r.calls {
				if len(call) > 2 && (call[2] == "set" || call[2] == "reset") {
					counts[strings.Join(call, " ")]++
				}
			}
			for _, cmd := range []string{"proxy healthcheck set " + kind + " Work 60", "proxy healthcheck set " + kind + " Work 120", "proxy healthcheck reset " + kind + " Work"} {
				if counts[cmd] != 1 {
					t.Fatalf("dispatch %s=%d calls=%v", cmd, counts[cmd], r.calls)
				}
			}
			for _, want := range []string{"Values are retained", "Configuration source: profile", "Lazy checks may skip", "subscription download intervals are separate", "Local healthcheck interval saved.", "Profile healthcheck interval restored."} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if kind == "group" && !strings.Contains(out.String(), "Attached providers use their own") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestHealthcheckFailureKeepsSettingsActionsAndCancelDoesNotMutate(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{"proxy healthcheck show group Work": {Code: 1, Error: &CLIError{Message: "profile unavailable"}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n"), &out), healthcheckPage("group", "Work"))
	if len(r.calls) != 1 || !strings.Contains(out.String(), "unknown") || !strings.Contains(out.String(), "Restore profile interval") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}

func TestUnavailableMeasurementsRemainUnknownInNodeListsAndDetails(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "group", true: "provider"}[provider], func(t *testing.T) {
			reason := "ambiguous node ownership"
			data := map[string]any{"all": []string{"Unknown", "Untested"}, "nodes": []string{"Unknown", "Untested"}, "node_delays": map[string]any{
				"Unknown":  map[string]any{"status": "untested", "reason": reason},
				"Untested": map[string]any{"status": "untested"},
			}}
			key := "proxy show Group"
			page := proxyGroupPage("Group")
			if provider {
				key = "proxy provider show Group"
				page = providerDetailPage("Group")
			}
			r := &fakeRunner{results: map[string]Result{key: {Data: data}}}
			var out bytes.Buffer
			s := NewSession(r, strings.NewReader("0\n"), &out)
			RunPage(context.Background(), s, page)
			for _, want := range []string{"Unknown — latency unknown", "Untested — untested"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			s.ShowResult(strings.Split(key, " "), Result{Data: data})
			if !strings.Contains(out.String(), "reason: "+reason) {
				t.Fatalf("missing measurement reason: %s", out.String())
			}
			if len(r.calls) != 1 {
				t.Fatalf("implicit operation: %v", r.calls)
			}
		})
	}
}

func TestHealthcheckSchedulingReasonDisplayedWithUnknownAndDisabledFields(t *testing.T) {
	for _, tc := range []struct {
		kind, reason string
		enabled      any
	}{
		{"group", "provider-only group has no native checker", nil},
		{"provider", "provider healthchecks are disabled", false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			r := &fakeRunner{results: map[string]Result{"proxy healthcheck show " + tc.kind + " Work": {Data: map[string]any{
				"profile": "work", "interval_seconds": nil, "lazy": nil, "enabled": tc.enabled, "reason": tc.reason, "source": "profile",
			}}}}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(r, strings.NewReader("0\n"), &out), healthcheckPage(tc.kind, "Work"))
			for _, want := range []string{"Interval: unknown", "lazy: unknown", "Scheduling note: " + tc.reason} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if len(r.calls) != 1 {
				t.Fatalf("implicit mutation: %v", r.calls)
			}
		})
	}
}
