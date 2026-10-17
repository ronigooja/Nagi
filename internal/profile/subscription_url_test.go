package profile

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/subscription"
	"gopkg.in/yaml.v3"
)

func TestSubscriptionURLPolicyCompositionAndRefresh(t *testing.T) {
	s, _, _ := providerFixture(t)
	ctx := context.Background()
	source := []byte(`proxy-providers:
  enabled: {type: inline, payload: [], health-check: {enable: true, url: 'https://source.invalid/check', interval: 180, lazy: false}}
  disabled: {type: inline, payload: [], health-check: {enable: false, interval: 180}}
  absent: {type: inline, payload: []}
proxy-groups:
  - {name: Auto, type: url-test, proxies: [DIRECT], url: 'https://source.invalid/check', interval: 180}
  - {name: Local, type: fallback, proxies: [DIRECT], url: 'https://source.invalid/check'}
  - {name: Manual, type: select, proxies: [DIRECT]}
  - {name: Checked, type: select, proxies: [DIRECT], interval: 90}
`)
	if err := s.Write(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	overlay := []byte(`proxy-providers:
  enabled: {health-check: {url: 'https://source.invalid/check'}}
proxy-groups:
  - {name: Auto, type: url-test, proxies: [DIRECT], url: 'https://source.invalid/check', interval: 180}
  - {name: Local, type: fallback, proxies: [DIRECT], url: 'https://local.invalid/custom'}
  - {name: Manual, type: select, proxies: [DIRECT]}
  - {name: Checked, type: select, proxies: [DIRECT], interval: 90}
`)
	if err := s.SetOverride(ctx, "default", overlay); err != nil {
		t.Fatal(err)
	}
	subs := subscription.NewStore(s.Dir, filepath.Join(t.TempDir(), "cache"), nil)
	testURL := "https://policy.invalid/private?token=PRIVATE"
	if err := subs.AddWithOptions("feed", "https://example.invalid/feed", subscription.AddOptions{HealthcheckURL: &testURL}); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("feed", "default", "cache", source); err != nil {
		t.Fatal(err)
	}
	inspect := func() {
		t.Helper()
		data, err := s.Effective("default")
		if err != nil {
			t.Fatal(err)
		}
		var effective map[string]any
		if err = yaml.Unmarshal(data, &effective); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ kind, name, want string }{
			{"provider", "enabled", "https://source.invalid/check"},
			{"provider", "disabled", testURL},
			{"provider", "absent", ""},
			{"group", "Auto", testURL},
			{"group", "Local", "https://local.invalid/custom"},
			{"group", "Manual", ""},
			{"group", "Checked", testURL},
		} {
			target, err := healthTarget(effective, tc.kind, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := healthFields(target, tc.kind)["url"].(string)
			if actual != tc.want {
				t.Fatalf("%s %s url=%q want=%q", tc.kind, tc.name, actual, tc.want)
			}
		}
		p, _ := healthTarget(effective, "provider", "disabled")
		if healthBool(healthFields(p, "provider")["enable"]) {
			t.Fatal("URL enabled a disabled provider")
		}
		p, _ = healthTarget(effective, "group", "Manual")
		if p["interval"] != nil {
			t.Fatal("URL enabled an unscheduled select group")
		}
	}
	inspect()
	next := []byte(string(source) + "  - {name: Fresh, type: load-balance, proxies: [DIRECT]}\n")
	if err := s.ApplySubscription(ctx, "default", next); err != nil {
		t.Fatal(err)
	}
	inspect()
	data, err := s.Effective("default")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: Fresh") || strings.Count(string(data), "url: https://policy.invalid/private?token=PRIVATE") != 4 {
		t.Fatal(string(data))
	}
	if err := subs.SetHealthcheckURL("feed", nil); err != nil {
		t.Fatal(err)
	}
	data, err = s.Effective("default")
	if err != nil || strings.Contains(string(data), "policy.invalid") {
		t.Fatal(string(data), err)
	}
}
