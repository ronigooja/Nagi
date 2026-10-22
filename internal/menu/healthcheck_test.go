package menu

import (
	"bytes"
	"context"
	"reflect"
	"strconv"
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
			data := map[string]any{"node_info": ordinaryNodeInfo([]string{"Fast", "Failed", "New"}), "all": []string{"Fast", "Failed", "New"}, "now": "Fast", "node_delays": delays}
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
		"proxy show Group":   {Data: map[string]any{"node_info": ordinaryNodeInfo([]string{"A", "B"}), "all": []string{"A", "B"}}},
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
	r.results["proxy show Group"] = Result{Data: map[string]any{"node_info": ordinaryNodeInfo([]string{"A", "B"}), "all": []string{"A", "B"}, "node_delays": map[string]any{"B": map[string]any{"status": "failed"}, "A": map[string]any{"status": "ok", "delay_ms": 8}}}}
	view, _ = page.Load(context.Background(), s)
	if view.Items[0].Name != "B" || !strings.Contains(view.Items[0].Label, "failed") || !strings.Contains(view.Items[1].Label, "8 ms") {
		t.Fatalf("latest results=%#v", view.Items)
	}
}

func TestMeasurementDetailsShowTimeAndOnlyEndpoint(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	s.ShowResult([]string{"proxy", "show", "Group"}, Result{Data: map[string]any{"node_info": ordinaryNodeInfo([]string{"Node"}), "node_delays": map[string]any{"Node": map[string]any{"status": "ok", "delay_ms": 5, "measured_at": "2026-10-16T20:00:00Z", "test_url": "https://user:secret@example.invalid/private?token=secret"}}}})
	for _, want := range []string{subscriptionTime("2026-10-16T20:00:00Z"), "example.invalid (https)", "test endpoint"} {
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
	for _, kind := range []string{"subscription", "provider", "group"} {
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
			wants := []string{"Values are retained", "Interval: 300 seconds (profile)", "Local healthcheck interval saved.", "Inherited healthcheck interval restored."}
			if kind == "subscription" {
				wants = append(wants, "Enabled/lazy: per checker")
			} else {
				wants = append(wants, "Enabled: true; lazy: true")
			}
			if kind == "group" {
				wants = append(wants, "Provider nodes use their provider interval.")
			} else if kind == "provider" {
				wants = append(wants, "Groups can check provider nodes independently.")
			}
			for _, want := range wants {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "Lazy checks may skip") || strings.Contains(out.String(), "download intervals are separate") || strings.Contains(out.String(), "Provider local settings take precedence") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestHealthcheckFailureKeepsSettingsActionsAndCancelDoesNotMutate(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{"proxy healthcheck show group Work": {Code: 1, Error: &CLIError{Message: "profile unavailable"}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n"), &out), healthcheckPage("group", "Work"))
	if len(r.calls) != 1 || !strings.Contains(out.String(), "unknown") || !strings.Contains(out.String(), "Reset interval") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}

func TestUnavailableMeasurementsRemainUnknownInNodeListsAndDetails(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "group", true: "provider"}[provider], func(t *testing.T) {
			reason := "ambiguous node ownership"
			data := map[string]any{"node_info": ordinaryNodeInfo([]string{"Unknown", "Untested"}), "all": []string{"Unknown", "Untested"}, "nodes": []string{"Unknown", "Untested"}, "node_delays": map[string]any{
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
		kind, note string
		enabled    any
	}{
		{"group", "Provider nodes use their provider interval.", nil},
		{"provider", "Groups can check provider nodes independently.", false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			r := &fakeRunner{results: map[string]Result{"proxy healthcheck show " + tc.kind + " Work": {Data: map[string]any{
				"profile": "work", "interval_seconds": nil, "lazy": nil, "enabled": tc.enabled, "reason": "long backend description should not be shown", "source": "profile",
			}}}}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(r, strings.NewReader("0\n"), &out), healthcheckPage(tc.kind, "Work"))
			for _, want := range []string{"Interval: unknown (profile)", "lazy: unknown", tc.note} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "long backend description") {
				t.Fatalf("verbose scheduling reason shown: %s", out.String())
			}
			if len(r.calls) != 1 {
				t.Fatalf("implicit mutation: %v", r.calls)
			}
		})
	}

	for _, enabled := range []bool{false, true} {
		r := &fakeRunner{results: map[string]Result{"proxy healthcheck show group Work": {Data: map[string]any{
			"interval_seconds": 0, "interval_defaulted": true, "enabled": enabled, "lazy": false, "source": "profile",
		}}}}
		view, err := healthcheckPage("group", "Work").Load(context.Background(), NewSession(r, strings.NewReader(""), &bytes.Buffer{}))
		if err != nil || !strings.Contains(view.Context, "Interval: default (profile)") {
			t.Fatalf("defaulted interval inferred an exact value: context=%q err=%v", view.Context, err)
		}
		if strings.Contains(view.Context, "all automatic checks") || strings.Contains(view.Context, "automatic checks are off") {
			t.Fatalf("group state was generalized: %q", view.Context)
		}
		if !strings.Contains(view.Context, "Enabled: "+strconv.FormatBool(enabled)) {
			t.Fatalf("enabled field lost: %q", view.Context)
		}
	}
}

func ordinaryNodeInfo(nodes []string) map[string]menuNodeInfo {
	info := map[string]menuNodeInfo{}
	for _, name := range nodes {
		info[name] = menuNodeInfo{Kind: "node"}
	}
	return info
}

func TestNodeIdentityControlsLatencyAndIndividualTests(t *testing.T) {
	nodes := []string{"Child", "Balance", "DIRECT", "REJECT", "Fast", "Mystery"}
	info := map[string]menuNodeInfo{"Child": {Kind: "group", Status: "unavailable"}, "Balance": {Kind: "group"}, "Fast": {Kind: "node"}}
	zero := 0
	delays := map[string]menuNodeDelay{}
	for _, name := range nodes {
		delays[name] = menuNodeDelay{Status: "failed"}
	}
	delays["Fast"] = menuNodeDelay{Status: "ok", DelayMS: &zero}
	data := map[string]any{"all": nodes, "nodes": nodes, "now": "DIRECT", "node_info": info, "node_delays": delays}
	r := &fakeRunner{results: map[string]Result{"proxy show Group": {Data: data}, "proxy provider show Feed": {Data: data}}}
	s := NewSession(r, strings.NewReader(""), &bytes.Buffer{})
	view, err := proxyGroupPage("Group").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Child — unavailable", "Balance", "DIRECT (selected)", "REJECT", "Fast — <1 ms", "Mystery"}
	for i, item := range view.Items {
		if item.Label != want[i] {
			t.Fatalf("item=%+v want=%s", item, want[i])
		}
	}
	action := profileAction(t, view, "Automatic latency checks")
	if action.Next == nil || hasProxyCall(r.calls, []string{"proxy", "healthcheck", "show", "group", "Group"}) {
		t.Fatal("group settings should load only on entry")
	}
	if _, err := action.Next().Load(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !hasProxyCall(r.calls, []string{"proxy", "healthcheck", "show", "group", "Group"}) {
		t.Fatal("group settings lost source context")
	}
	tests, _ := proxyNodesPage("Group", true).Load(context.Background(), s)
	if len(tests.Items) != 1 || !reflect.DeepEqual(tests.Items[0].Args, []string{"proxy", "test", "group", "Group", "Fast"}) {
		t.Fatalf("unsafe tests: %+v", tests.Items)
	}
	provider, _ := providerDetailPage("Feed").Load(context.Background(), s)
	for _, item := range provider.Items {
		if item.Name == "Fast" {
			if !reflect.DeepEqual(item.Args, []string{"proxy", "test", "provider", "Feed", "Fast"}) {
				t.Fatal(item.Args)
			}
		} else if len(item.Args) > 0 {
			t.Fatal(item)
		}
	}
	info["Child"] = menuNodeInfo{Kind: "group", Status: "available"}
	view, _ = proxyGroupPage("Group").Load(context.Background(), s)
	if view.Items[0].Label != "Child — available" {
		t.Fatal(view.Items[0])
	}
	var out bytes.Buffer
	NewSession(r, strings.NewReader(""), &out).ShowResult([]string{"proxy", "show", "Group"}, Result{Data: data})
	if strings.Contains(out.String(), "latency test failed") || strings.Contains(out.String(), "DIRECT:\n") || strings.Contains(out.String(), "Child:\n") {
		t.Fatal(out.String())
	}
}

func TestSubscriptionLatencyEntryIsExplicitAndUsesOriginalDefault(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{"proxy healthcheck show subscription Feed": {Data: map[string]any{"interval_seconds": 0, "interval_defaulted": true, "source": "profile"}}}}
	s := NewSession(r, strings.NewReader(""), &bytes.Buffer{})
	view, _ := subscriptionDetailPage("Feed").Load(context.Background(), s)
	action := profileAction(t, view, "Automatic latency checks")
	if hasProxyCall(r.calls, []string{"proxy", "healthcheck", "show", "subscription", "Feed"}) {
		t.Fatal("implicit settings read")
	}
	settings, _ := action.Next().Load(context.Background(), s)
	if settings.Actions[0].Impact != "" || settings.Actions[0].Label != "Set interval" || !strings.Contains(settings.Context, "Profile: not applied") || !strings.Contains(settings.Context, "Enabled/lazy: per checker") || strings.Contains(settings.Context, "Enabled: false") {
		t.Fatalf("misleading subscription settings: %+v", settings)
	}
	if !strings.Contains(settings.Context, "Interval: default") || strings.Contains(settings.Context, "0 seconds") || settings.Actions[1].Label != "Reset interval" {
		t.Fatalf("%+v", settings)
	}
}

func TestExplicitProviderNodeIdentityOverridesReservedNameFallback(t *testing.T) {
	zero := 0
	data := map[string]any{"nodes": []string{"DIRECT"}, "node_info": ordinaryNodeInfo([]string{"DIRECT"}), "node_delays": map[string]menuNodeDelay{"DIRECT": {Status: "ok", DelayMS: &zero}}}
	r := &fakeRunner{results: map[string]Result{"proxy provider show Feed": {Data: data}}}
	view, err := providerDetailPage("Feed").Load(context.Background(), NewSession(r, strings.NewReader(""), &bytes.Buffer{}))
	if err != nil || len(view.Items) != 1 || view.Items[0].Label != "DIRECT — <1 ms" || !reflect.DeepEqual(view.Items[0].Args, []string{"proxy", "test", "provider", "Feed", "DIRECT"}) {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if nodeKind("DIRECT", nil) != "builtin" {
		t.Fatal("missing reserved fallback")
	}
}
