package command

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/rules"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
)

var (
	errSourceChanged    = errors.New("source profile changed; run `nagi subscription apply NAME` to resume automatic updates")
	errSourceUnreadable = errors.New("source profile cannot be read; inspect the profile and reapply the subscription")
	errDNSConflict      = errors.New("local DNS proxy target is unavailable in the updated subscription; update DNS settings")
	errRuleConflict     = errors.New("enabled custom rule target is unavailable in the updated subscription; update custom rules")
	errChoiceConflict   = errors.New("saved proxy selections could not be reconciled with the updated subscription")
)

// ApplyCachedSubscriptionPreservingSource preserves local overrides and valid
// choices. It is for callers which already verified association; scheduled
// operation uses applyScheduledCache for the required lock-internal CAS.
func ApplyCachedSubscriptionPreservingSource(ctx context.Context, paths nagiruntime.Paths, cache *subscription.Store, profiles *profile.Store, name string) error {
	_, err := withNamedLock(paths, "config.lock", func() (any, error) {
		return applyTransaction(ctx, paths, []string{"subscription", "apply-preserving"}, func() (any, error) {
			data, err := cache.Cached(name)
			if err != nil {
				return nil, err
			}
			encoded, doc, err := normalizedCache(data)
			if err != nil {
				return nil, err
			}
			if err = validateRuleTargets(paths.ConfigDir, doc); err != nil {
				return nil, err
			}
			if profiles == nil {
				return nil, errors.New("profile store is required")
			}
			if err = profiles.Apply(ctx, name, encoded); err != nil {
				return nil, classifyScheduledApply(err)
			}
			if err = pruneSavedChoices(ctx, paths, name, doc); err != nil {
				return nil, fmt.Errorf("%w: %v", errChoiceConflict, err)
			}
			return nil, nil
		})
	})
	return err
}
func normalizedCache(data []byte) ([]byte, map[string]any, error) {
	doc, _, err := subscription.Document(data)
	if err != nil {
		return nil, nil, err
	}
	doc, err = subscription.NormalizeChecked(doc)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := subscription.Encode(doc)
	return encoded, doc, err
}

// The source, association, current selection, generation, schedule and cache
// version are all re-read with config.lock held. A manual import detaches the
// association; a concurrent update may only replace cache between transactions.
func applyScheduledCache(ctx context.Context, p nagiruntime.Paths, s *subscription.Store, profiles *profile.Store, expected subscription.Entry) error {
	_, err := withNamedLock(p, "config.lock", func() (any, error) {
		current, err := profile.NewStore(p.ConfigDir, nil, nil).Current()
		if err != nil {
			return nil, err
		}
		latest, err := s.Find(expected.Name)
		if err != nil {
			return nil, err
		}
		if latest.Generation != expected.Generation || latest.ScheduleSeconds == 0 || latest.ScheduleSeconds != expected.ScheduleSeconds || !latest.NextDue.Equal(expected.NextDue) || latest.LastFailureStage == "paused" || latest.Profile == "" || current != latest.Profile || latest.Profile != expected.Profile || latest.SourceHash != expected.SourceHash || latest.CacheVersion != expected.CacheVersion {
			return nil, nil
		}
		if reason := sourceConflict(s, latest); reason != "" {
			if reason == "source_unreadable" {
				return nil, errSourceUnreadable
			}
			return nil, errSourceChanged
		}
		data, err := s.Cached(latest.Name)
		if err != nil {
			return nil, err
		}
		if subscription.Hash(data) != latest.CacheVersion {
			return nil, errors.New("cache version changed during automatic apply")
		}
		encoded, doc, err := normalizedCache(data)
		if err != nil {
			return nil, err
		}
		if err = validateRuleTargets(p.ConfigDir, doc); err != nil {
			return nil, err
		}
		return applyTransaction(ctx, p, []string{"subscription", "apply-preserving"}, func() (any, error) {
			if err := profiles.Apply(ctx, latest.Profile, encoded); err != nil {
				return nil, classifyScheduledApply(err)
			}
			if err := pruneSavedChoices(ctx, p, latest.Profile, doc); err != nil {
				return nil, fmt.Errorf("%w: %v", errChoiceConflict, err)
			}
			if err := s.RecordApplySuccess(latest.Name, latest.Profile, latest.CacheVersion, encoded); err != nil {
				return nil, err
			}
			return nil, nil
		})
	})
	return err
}
func classifyScheduledApply(err error) error {
	if strings.Contains(err.Error(), "DNS proxy target is missing") {
		return fmt.Errorf("%w: %v", errDNSConflict, err)
	}
	return err
}
func validateRuleTargets(dir string, doc map[string]any) error {
	saved, err := rules.New(dir, nil).List()
	if err != nil {
		return fmt.Errorf("%w: rules file unreadable", errRuleConflict)
	}
	valid := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "GLOBAL": true}
	for _, key := range []string{"proxies", "proxy-groups"} {
		for _, raw := range mapsOf(doc[key]) {
			if name, _ := raw["name"].(string); name != "" {
				valid[name] = true
			}
		}
	}
	for _, rule := range saved.Entries {
		if rule.Enabled && !valid[rule.Target] {
			return fmt.Errorf("%w: rule %s targets a missing choice", errRuleConflict, rule.Name)
		}
	}
	return nil
}
func mapsOf(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func pruneSavedChoices(ctx context.Context, p nagiruntime.Paths, name string, doc map[string]any) error {
	path := filepath.Join(p.ConfigDir, "proxy-selections.json")
	store := proxy.SelectionStore{Path: path}
	groups := map[string]map[string]bool{}
	for _, g := range mapsOf(doc["proxy-groups"]) {
		n, _ := g["name"].(string)
		if n == "" {
			continue
		}
		options := map[string]bool{}
		if items, ok := g["proxies"].([]any); ok {
			for _, item := range items {
				if value, ok := item.(string); ok {
					options[value] = true
				}
			}
		}
		if _, usesProviders := g["use"]; usesProviders {
			options["*"] = true
		}
		groups[n] = options
	}
	if live, err := proxy.NewService(mutationClient(p)).Groups(ctx); err == nil {
		groups = map[string]map[string]bool{}
		for _, g := range live {
			options := map[string]bool{}
			for _, node := range g.All {
				options[node] = true
			}
			groups[g.Name] = options
		}
	}
	return store.Prune(name, groups)
}
