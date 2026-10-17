package profile

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestSubscriptionHealthcheckPriorityAndRefresh(t *testing.T) {
	s, _, _ := providerFixture(t)
	ctx := context.Background()
	source := []byte(`proxy-providers:
  enabled:
    type: inline
    payload: []
    health-check: {enable: true, url: 'https://example.com/check', interval: 180, lazy: false}
  disabled:
    type: inline
    payload: []
    health-check: {enable: false, interval: 180}
proxy-groups:
  - {name: Auto, type: url-test, proxies: [DIRECT], interval: 180}
  - {name: Manual, type: select, proxies: [DIRECT]}
  - {name: Checked, type: select, proxies: [DIRECT], interval: 90}
`)
	if err := s.Write(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	subs := subscription.NewStore(s.Dir, filepath.Join(t.TempDir(), "cache"), nil)
	if err := subs.Add("feed", "https://example.com/feed?secret=PRIVATE"); err != nil {
		t.Fatal(err)
	}
	interval := 180 // Explicit same-value provider intent must survive a later policy change.
	if err := s.SetHealthcheck(ctx, "default", "provider", "enabled", &interval); err != nil {
		t.Fatal(err)
	}
	interval = 19
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", &interval); err != nil {
		t.Fatal(err)
	}
	interval = 60
	if err := subs.SetHealthcheckInterval("feed", &interval); err != nil {
		t.Fatal(err)
	}
	// Saved policy without association must not alter a similarly named profile.
	if r, _ := s.Healthcheck("default", "provider", "disabled"); r.IntervalSeconds != 180 {
		t.Fatal(r)
	}
	if err := subs.RecordApplySuccess("feed", "default", "cache", source); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, name, source string
		seconds            int
	}{
		{"provider", "enabled", "override", 180}, {"provider", "disabled", "subscription", 60},
		{"group", "Auto", "override", 19}, {"group", "Manual", "profile", 0}, {"group", "Checked", "subscription", 60},
	} {
		r, err := s.Healthcheck("default", tc.kind, tc.name)
		if err != nil || r.IntervalSeconds != tc.seconds || r.Source != tc.source {
			t.Fatalf("%+v err=%v want=%+v", r, err, tc)
		}
	}
	if r, _ := s.Healthcheck("default", "provider", "disabled"); r.Enabled == nil || *r.Enabled {
		t.Fatal("enabled disabled provider", r)
	}
	if r, _ := s.Healthcheck("default", "provider", "enabled"); r.Lazy || r.TestURL != "https://example.com" {
		t.Fatal(r)
	}
	next := []byte(strings.Replace(string(source), "proxy-groups:", "  fresh:\n    type: inline\n    payload: []\n    health-check: {enable: true, interval: 999}\nproxy-groups:", 1) + "  - {name: New, type: fallback, proxies: [DIRECT]}\n")
	if err := s.ApplySubscription(ctx, "default", next); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "group", "New"); r.IntervalSeconds != 60 || r.Source != "subscription" {
		t.Fatal(r)
	}
	if r, _ := s.Healthcheck("default", "provider", "fresh"); r.IntervalSeconds != 60 || r.Source != "subscription" {
		t.Fatal("new feed provider missed policy", r)
	}
	if err := s.SetHealthcheck(ctx, "default", "provider", "enabled", nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "provider", "enabled"); r.IntervalSeconds != 60 || r.Source != "subscription" {
		t.Fatal(r)
	}
	// Choosing the currently inherited value is still explicit Provider intent.
	if err := s.SetHealthcheck(ctx, "default", "provider", "enabled", &interval); err != nil {
		t.Fatal(err)
	}
	nextInterval := 120
	if err := subs.SetHealthcheckInterval("feed", &nextInterval); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "provider", "enabled"); r.IntervalSeconds != 60 || r.Source != "override" {
		t.Fatal("same-value provider intent lost", r)
	}
	if err := s.SetHealthcheck(ctx, "default", "provider", "enabled", nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "provider", "enabled"); r.IntervalSeconds != 120 || r.Source != "subscription" {
		t.Fatal("provider reset missed inherited value", r)
	}
	if err := subs.SetHealthcheckInterval("feed", nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "provider", "enabled"); r.IntervalSeconds != 180 || r.Source != "profile" {
		t.Fatal(r)
	}
	if r, _ := s.Healthcheck("default", "group", "Auto"); r.IntervalSeconds != 19 {
		t.Fatal("subscription reset erased group intent", r)
	}
}

func TestSubscriptionHealthcheckAssociationChanges(t *testing.T) {
	s, source, _ := providerFixture(t)
	subs := subscription.NewStore(s.Dir, t.TempDir(), nil)
	for _, name := range []string{"first", "second"} {
		if err := subs.Add(name, "https://example.com/feed"); err != nil {
			t.Fatal(err)
		}
	}
	interval := 17
	if err := subs.SetHealthcheckInterval("first", &interval); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("first", "default", "cache", source); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "group", "Auto"); r.IntervalSeconds != 17 {
		t.Fatal(r)
	}
	if err := subs.RecordApplySuccess("second", "default", "cache", source); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "group", "Auto"); r.IntervalSeconds != 180 {
		t.Fatal("stale association", r)
	}
	if entry, _ := subs.Find("first"); entry.Profile != "" || entry.HealthcheckIntervalSeconds == nil {
		t.Fatal(entry)
	}
	if err := subs.SetHealthcheckInterval("second", &interval); err != nil {
		t.Fatal(err)
	}
	if err := subs.Remove("second"); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Healthcheck("default", "group", "Auto"); r.IntervalSeconds != 180 {
		t.Fatal("removed policy remains", r)
	}
}
