package profile

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestHealthcheckSetResetAndRebase(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	n := 17
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", &n); err != nil {
		t.Fatal(err)
	}
	r, err := s.Healthcheck("default", "group", "Auto")
	if err != nil || r.IntervalSeconds != 17 || r.Source != "override" || !strings.Contains(r.Reason, "provider") {
		t.Fatalf("%+v %v", r, err)
	}
	next := strings.Replace(string(source), "interval: 180", "interval: 240", 1)
	if err := s.ApplySubscription(ctx, "default", []byte(next)); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "group", "Auto")
	if r.IntervalSeconds != 17 {
		t.Fatal(r)
	}
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", nil); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "group", "Auto")
	if r.IntervalSeconds != 240 || r.Source != "profile" {
		t.Fatal(r)
	}
	before, _ := s.Override("default")
	s.Validate = func([]byte) error { return errors.New("invalid") }
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", &n); err == nil {
		t.Fatal("validation accepted")
	}
	after, _ := s.Override("default")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed validation changed override")
	}
}
func TestHealthcheckProviderResetRetainsLocalSettings(t *testing.T) {
	s, _, _ := providerFixture(t)
	ctx := context.Background()
	if _, err := s.AddProvider(ctx, "default", "work", "https://example.com/feed", "Auto", 7200); err != nil {
		t.Fatal(err)
	}
	n := 19
	if err := s.SetHealthcheck(ctx, "default", "provider", "work", &n); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Healthcheck("default", "provider", "work")
	if r.IntervalSeconds != 19 || r.Source != "override" || !*r.Enabled {
		t.Fatal(r)
	}
	if err := s.SetHealthcheck(ctx, "default", "provider", "work", nil); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "provider", "work")
	if r.IntervalSeconds != 300 || r.Source != "profile" || !*r.Enabled {
		t.Fatal(r)
	}
	b, _ := s.Effective("default")
	m := yamlMapping(t, b)
	p := m["proxy-providers"].(map[string]any)["work"].(map[string]any)
	if p["url"] != "https://example.com/feed" || p["interval"] != 7200 || m["mode"] != "global" {
		t.Fatal(m)
	}
}

func TestHealthcheckProviderSourceAndResetPreserveOtherOverrides(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	source = []byte(strings.Replace(string(source), "payload: []", "payload: []\n    health-check:\n      enable: false\n      interval: 123\n      lazy: false\n      url: https://example.com/health?token=secret", 1))
	if err := s.Write(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	n := 19
	if err := s.SetHealthcheck(ctx, "default", "provider", "original", &n); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Healthcheck("default", "provider", "original")
	if r.IntervalSeconds != 19 || r.Source != "override" || *r.Enabled || r.Lazy || r.TestURL != "https://example.com" {
		t.Fatal(r)
	}
	if err := s.SetHealthcheck(ctx, "default", "provider", "original", nil); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "provider", "original")
	if r.IntervalSeconds != 123 || r.Source != "profile" || *r.Enabled || r.Lazy {
		t.Fatal(r)
	}
	b, _ := s.Override("default")
	m := yamlMapping(t, b)
	if m["mode"] != "global" || m["dns"].(map[string]any)["ipv6"] != true {
		t.Fatal(m)
	}
	before := append([]byte{}, b...)
	s.Reload = func(context.Context) error { return errors.New("reload failed") }
	if err := s.SetHealthcheck(ctx, "default", "provider", "original", &n); err == nil {
		t.Fatal("reload failure accepted")
	}
	after, _ := s.Override("default")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reload failure changed override")
	}
}

func TestExplicitEqualGroupIntervalSurvivesSubscriptionAndResetReleasesIntent(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	n := 180
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", &n); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Healthcheck("default", "group", "Auto")
	if r.Source != "override" || r.IntervalSeconds != 180 {
		t.Fatal(r)
	}
	b, _ := s.Override("default")
	overlay := yamlMapping(t, b)
	if _, snapshot := overlay["proxy-groups"]; snapshot {
		t.Fatal("interval created full group snapshot")
	}
	next := []byte(strings.Replace(string(source), "interval: 180", "interval: 240", 1))
	if err := s.ApplySubscription(ctx, "default", next); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "group", "Auto")
	if r.Source != "override" || r.IntervalSeconds != 180 {
		t.Fatal(r)
	}
	effective, _ := s.Effective("default")
	if strings.Contains(string(effective), groupHealthcheckIntervalsKey) {
		t.Fatal("managed marker reached engine")
	}
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", nil); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "group", "Auto")
	if r.Source != "profile" || r.IntervalSeconds != 240 {
		t.Fatal(r)
	}
	next = []byte(strings.Replace(string(source), "interval: 180", "interval: 300", 1))
	if err := s.ApplySubscription(ctx, "default", next); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Healthcheck("default", "group", "Auto")
	if r.IntervalSeconds != 300 || r.Source != "profile" {
		t.Fatal(r)
	}
}
func TestGroupIntervalIntentDormantWhenSubscriptionRemovesGroup(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	n := 19
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", &n); err != nil {
		t.Fatal(err)
	}
	next := []byte("proxy-groups: []\n")
	if err := s.ApplySubscription(ctx, "default", next); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Effective("default")
	m := yamlMapping(t, b)
	if len(m["proxy-groups"].([]any)) != 0 {
		t.Fatal("deleted group resurrected")
	}
	if err := s.ApplySubscription(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Healthcheck("default", "group", "Auto")
	if r.IntervalSeconds != 19 || r.Source != "override" {
		t.Fatal(r)
	}
}
func TestHealthcheckIncludedNodesAndQuotedInterval(t *testing.T) {
	for _, flag := range []string{"include-all", "include-all-proxies"} {
		s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
		source := []byte("proxies:\n - name: Local\n   type: direct\nproxy-groups:\n - name: Auto\n   type: url-test\n   " + flag + ": true\n")
		if err := s.Write(context.Background(), "default", source); err != nil {
			t.Fatal(err)
		}
		r, err := s.Healthcheck("default", "group", "Auto")
		if err != nil || r.IntervalSeconds != 300 || r.Enabled == nil || !*r.Enabled || !r.IntervalDefaulted {
			t.Fatalf("%s %+v %v", flag, r, err)
		}
		source = append(source, []byte("   interval: \"123\"\n")...)
		if err := s.Write(context.Background(), "default", source); err != nil {
			t.Fatal(err)
		}
		r, err = s.Healthcheck("default", "group", "Auto")
		if err != nil || r.IntervalSeconds != 123 || r.IntervalDefaulted {
			t.Fatalf("quoted %+v %v", r, err)
		}
	}
}

func TestResetOneGroupRetainsOtherExplicitInterval(t *testing.T) {
	s, _, _ := providerFixture(t)
	ctx := context.Background()
	n := 180
	other := 55
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", &n); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHealthcheck(ctx, "default", "group", "My Proxy", &other); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHealthcheck(ctx, "default", "group", "Auto", nil); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Healthcheck("default", "group", "My Proxy")
	if r.IntervalSeconds != 55 || r.Source != "override" {
		t.Fatal(r)
	}
	r, _ = s.Healthcheck("default", "group", "Auto")
	if r.IntervalSeconds != 180 || r.Source != "profile" {
		t.Fatal(r)
	}
}
