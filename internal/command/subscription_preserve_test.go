package command

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/rules"
	"gopkg.in/yaml.v3"
)

func TestSubscriptionApplyRetainsRuleTargetAcrossEntryPoints(t *testing.T) {
	for _, entryPoint := range []string{"manual", "preserving", "scheduled"} {
		t.Run(entryPoint, func(t *testing.T) {
			ctx := context.Background()
			next := []byte("mode: rule\nproxies: [{name: New, type: direct}]\nrules: ['MATCH,DIRECT']\n")
			p, cache, _ := schedulerFixture(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(next) })
			if _, err := cache.Refresh(ctx, "work"); err != nil {
				t.Fatal(err)
			}
			validatedRule := false
			validator := func(data []byte) error {
				var cfg struct {
					Proxies []struct{ Name string }
					Rules   []string
				}
				if err := yaml.Unmarshal(data, &cfg); err != nil {
					return err
				}
				for _, line := range cfg.Rules {
					if line == "DOMAIN,example.test,Manual" {
						validatedRule = true
						for _, node := range cfg.Proxies {
							if node.Name == "Manual" {
								return nil
							}
						}
						return errors.New("rule target is missing")
					}
				}
				return nil
			}
			profiles := profile.NewStore(p.ConfigDir, validator, nil)
			old := []byte("proxies: [{name: Old, type: direct}]\nrules: ['MATCH,DIRECT']\n")
			if err := profiles.Apply(ctx, "work", old); err != nil {
				t.Fatal(err)
			}
			if err := profiles.SetOverride(ctx, "work", []byte("mode: global\nproxies: [{name: Old, type: direct}, {name: Manual, type: direct}]\n")); err != nil {
				t.Fatal(err)
			}
			if err := rules.New(p.ConfigDir, "work", nil).Save(ctx, rules.Document{Entries: []rules.Entry{{Name: "Personal", Enabled: true, Kind: "rule", Type: "DOMAIN", Payload: "example.test", Target: "Manual"}}}); err != nil {
				t.Fatal(err)
			}
			if err := cache.RecordApplySuccess("work", "work", "old-cache", old); err != nil {
				t.Fatal(err)
			}
			var err error
			switch entryPoint {
			case "manual":
				_, err = subscriptionCommand(ctx, []string{"subscription", "apply", "work"}, cache, profiles, nil)
			case "preserving":
				err = ApplyCachedSubscriptionPreservingSource(ctx, p, cache, profiles, "work")
			case "scheduled":
				entry, _ := cache.Find("work")
				err = applyScheduledCache(ctx, p, cache, profiles, entry)
			}
			if err != nil {
				t.Fatal(err)
			}
			effective, err := profiles.Effective("work")
			if err != nil || !validatedRule || !strings.Contains(string(effective), "name: New") || !strings.Contains(string(effective), "name: Manual") || strings.Contains(string(effective), "name: Old") || !strings.Contains(string(effective), "mode: global") || !strings.Contains(string(effective), "DOMAIN,example.test,Manual") {
				t.Fatalf("subscription update lost intent or retained old nodes: %s, %v", effective, err)
			}
		})
	}
}
