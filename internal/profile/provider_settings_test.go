package profile

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/subscription"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentURLsIndependentFromIntervalsAndRebase(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	source = []byte(strings.Replace(string(source), "payload: []", "payload: []\n    health-check:\n      enable: true\n      url: https://source.example/check", 1))
	if err := s.Write(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"group", "provider"} {
		name := "Auto"
		if kind == "provider" {
			name = "feed"
		}
		// fixture provider name is read locally to keep the journey tied to actual configuration.
		if kind == "provider" {
			names, err := s.HealthcheckTargets("default", kind)
			if err != nil || len(names) == 0 {
				t.Fatal(names, err)
			}
			name = names[0]
		}
		raw := "https://probe.example/check?private=value"
		if err := s.SetHealthcheckURL(ctx, "default", kind, name, &raw); err != nil {
			t.Fatal(err)
		}
		n := 27
		if err := s.SetHealthcheck(ctx, "default", kind, name, &n); err != nil {
			t.Fatal(err)
		}
		if err := s.ApplySubscription(ctx, "default", source); err != nil {
			t.Fatal(err)
		}
		r, err := s.Healthcheck("default", kind, name)
		if err != nil || r.TestURL != "https://probe.example" || r.URLSource != "override" || r.IntervalSeconds != 27 {
			t.Fatal(r, err)
		}
		if err := s.SetHealthcheck(ctx, "default", kind, name, nil); err != nil {
			t.Fatal(err)
		}
		r, _ = s.Healthcheck("default", kind, name)
		if r.TestURL != "https://probe.example" {
			t.Fatal(r)
		}
		if err := s.SetHealthcheckURL(ctx, "default", kind, name, nil); err != nil {
			t.Fatal(err)
		}
		r, _ = s.Healthcheck("default", kind, name)
		if r.URLSource == "override" {
			t.Fatal(r)
		}
	}
}

func TestProviderIntervalAndURLValidationRollback(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	if _, err := s.AddProvider(ctx, "default", "work", "https://feed.example/nodes", "Auto", 0, "https://probe.example/204"); err != nil {
		t.Fatal(err)
	}
	n := 71
	if err := s.SetProviderInterval(ctx, "default", "work", &n); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySubscription(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	r, err := s.ProviderInterval("default", "work")
	if err != nil || r.IntervalSeconds != 71 || r.Source != "override" || r.CanReset || r.SourceIntervalSeconds != nil {
		t.Fatal(r, err)
	}
	h, _ := s.Healthcheck("default", "provider", "work")
	if h.TestURL != "https://probe.example" || h.IntervalSeconds != 300 {
		t.Fatal(h)
	}
	before, _ := s.Override("default")
	s.Validate = func([]byte) error { return errors.New("invalid") }
	raw := "https://replacement.example/204"
	if err := s.SetHealthcheckURL(ctx, "default", "provider", "work", &raw); err == nil {
		t.Fatal("accepted invalid config")
	}
	if err := s.SetProviderInterval(ctx, "default", "work", nil); err == nil {
		t.Fatal("accepted invalid config")
	}
	after, _ := s.Override("default")
	if string(before) != string(after) {
		t.Fatal("failed changes modified disk")
	}
	for _, raw := range []string{"file:///private", "https://user:pass@example.com", "https://example.com/#private", " https://example.com", "https://"} {
		if err := ValidateHealthcheckURL(raw); err == nil || strings.Contains(err.Error(), raw) {
			t.Fatal(raw, err)
		}
	}
}

func TestProviderURLResetRequiresUsableInheritedURL(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		s, _, _ := providerFixture(t)
		ctx := context.Background()
		urls := []string{}
		if explicit {
			urls = append(urls, "https://created.example/check")
		}
		if _, err := s.AddProvider(ctx, "default", "local", "https://feed.example/nodes", "Auto", 3600, urls...); err != nil {
			t.Fatal(err)
		}
		state, err := s.Healthcheck("default", "provider", "local")
		if err != nil || state.CanResetURL || state.TestURL == "" {
			t.Fatal(state, err)
		}
		before, _ := s.Override("default")
		if err := s.SetHealthcheckURL(ctx, "default", "provider", "local", nil); err == nil || !strings.Contains(err.Error(), "set an explicit test URL") {
			t.Fatal(err)
		}
		after, _ := s.Override("default")
		if string(before) != string(after) {
			t.Fatal("refused reset changed provider")
		}
		state, _ = s.Healthcheck("default", "provider", "local")
		if state.TestURL == "" || !*state.Enabled {
			t.Fatal("reset disabled provider checks", state)
		}
	}
}

func TestProviderURLResetInheritsSourceAndRetainsOtherFields(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	source = []byte(strings.Replace(string(source), "payload: []", "payload: []\n    health-check:\n      enable: true\n      interval: 123\n      url: https://source.example/check", 1))
	if err := s.Write(ctx, "default", source); err != nil {
		t.Fatal(err)
	}
	names, _ := s.HealthcheckTargets("default", "provider")
	name := names[0]
	raw := "https://local.example/check"
	if err := s.SetHealthcheckURL(ctx, "default", "provider", name, &raw); err != nil {
		t.Fatal(err)
	}
	n := 71
	if err := s.SetHealthcheck(ctx, "default", "provider", name, &n); err != nil {
		t.Fatal(err)
	}
	state, _ := s.Healthcheck("default", "provider", name)
	if !state.CanResetURL {
		t.Fatal(state)
	}
	if err := s.SetHealthcheckURL(ctx, "default", "provider", name, nil); err != nil {
		t.Fatal(err)
	}
	state, _ = s.Healthcheck("default", "provider", name)
	if state.TestURL != "https://source.example" || state.URLSource != "profile" || state.IntervalSeconds != 71 || !*state.Enabled {
		t.Fatal(state)
	}
	data, _ := s.Effective("default")
	effective := yamlMapping(t, data)
	target, _ := healthTarget(effective, "provider", name)
	if healthFields(target, "provider")["url"] != "https://source.example/check" {
		t.Fatal(target)
	}
}

func TestLocalProviderURLResetInheritsSubscriptionPolicy(t *testing.T) {
	s, source, _ := providerFixture(t)
	ctx := context.Background()
	if _, err := s.AddProvider(ctx, "default", "local", "https://feed.example/nodes", "Auto", 3600, "https://created.example/check"); err != nil {
		t.Fatal(err)
	}
	subs := subscription.NewStore(s.Dir, filepath.Join(t.TempDir(), "cache"), nil)
	policy := "https://policy.example/private?token=SECRET"
	if err := subs.AddWithOptions("feed", "https://feed.example/sub", subscription.AddOptions{HealthcheckURL: &policy}); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("feed", "default", "cache", source); err != nil {
		t.Fatal(err)
	}
	state, err := s.Healthcheck("default", "provider", "local")
	if err != nil || !state.CanResetURL || state.URLSource != "override" {
		t.Fatal(state, err)
	}
	if err := s.SetHealthcheckURL(ctx, "default", "provider", "local", nil); err != nil {
		t.Fatal(err)
	}
	state, err = s.Healthcheck("default", "provider", "local")
	if err != nil || state.TestURL != "https://policy.example" || state.URLSource != "subscription" || !*state.Enabled {
		t.Fatal(state, err)
	}
	data, _ := s.Effective("default")
	effective := yamlMapping(t, data)
	target, _ := healthTarget(effective, "provider", "local")
	if healthFields(target, "provider")["url"] != policy || healthFields(target, "provider")["interval"] != 300 || target["interval"] != 3600 {
		t.Fatal(target)
	}
}
