package command

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

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
			encoded, _, err := normalizedCache(data)
			if err != nil {
				return nil, err
			}
			if err = validateRuleTargets(paths.ConfigDir, name, encoded); err != nil {
				return nil, err
			}
			if profiles == nil {
				return nil, errors.New("profile store is required")
			}
			if err = profiles.ApplySubscription(ctx, name, encoded); err != nil {
				return nil, classifyScheduledApply(err)
			}
			if err = pruneSavedChoices(ctx, paths, name, encoded); err != nil {
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
	return applyScheduledCacheFactory(ctx, p, s, expected, func() (*profile.Store, error) { return profiles, nil })
}

func applyScheduledCacheFactory(ctx context.Context, p nagiruntime.Paths, s *subscription.Store, expected subscription.Entry, makeProfiles func() (*profile.Store, error)) error {
	_, err := withNamedLock(p, "config.lock", func() (any, error) {
		current, err := profile.NewStore(p.ConfigDir, nil, nil).Current()
		if err != nil {
			return nil, err
		}
		latest, err := s.Find(expected.Name)
		if err != nil {
			return nil, err
		}
		if latest.Generation != expected.Generation || latest.ScheduleSeconds == 0 || latest.ScheduleSeconds != expected.ScheduleSeconds || !latest.NextDue.Equal(expected.NextDue) || latest.PauseReason != "" || latest.Profile == "" || current != latest.Profile || latest.Profile != expected.Profile || latest.SourceHash != expected.SourceHash || latest.CacheVersion != expected.CacheVersion {
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
		encoded, _, err := normalizedCache(data)
		if err != nil {
			return nil, err
		}
		profiles, err := makeProfiles()
		if err != nil {
			return nil, err
		}
		if err = validateRuleTargets(p.ConfigDir, latest.Profile, encoded); err != nil {
			return nil, err
		}
		return applyTransaction(ctx, p, []string{"subscription", "apply-preserving"}, func() (any, error) {
			if err := profiles.ApplySubscription(ctx, latest.Profile, encoded); err != nil {
				return nil, classifyScheduledApply(err)
			}
			if err := pruneSavedChoices(ctx, p, latest.Profile, encoded); err != nil {
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
func validateRuleTargets(dir, profileName string, candidate []byte) error {
	profiles := profile.NewStore(dir, nil, nil)
	effective, err := profiles.EffectiveSubscriptionSource(profileName, candidate)
	if err != nil {
		return fmt.Errorf("%w: effective profile unavailable: %v", errRuleConflict, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(effective, &doc); err != nil || doc == nil {
		return fmt.Errorf("%w: effective profile is invalid", errRuleConflict)
	}
	saved, err := rules.New(dir, profileName, nil).List()
	if err != nil {
		return fmt.Errorf("%w: rules file unreadable", errRuleConflict)
	}
	valid := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "GLOBAL": true}
	remoteProvider := false
	for _, raw := range mapsOf(doc["proxies"]) {
		if name, _ := raw["name"].(string); name != "" {
			valid[name] = true
		}
	}
	for _, raw := range mapsOf(doc["proxy-groups"]) {
		if name, _ := raw["name"].(string); name != "" {
			valid[name] = true
		}
	}
	if providers, ok := doc["proxy-providers"].(map[string]any); ok {
		for _, value := range providers {
			provider, _ := value.(map[string]any)
			if provider["type"] != "inline" {
				remoteProvider = true // Its runtime node names cannot be inferred from the YAML.
			}
			for _, node := range mapsOf(provider["payload"]) {
				if name, _ := node["name"].(string); name != "" {
					valid[name] = true
				}
			}
		}
	}
	for _, rule := range saved.Entries {
		if !rule.Enabled {
			continue
		}
		for _, target := range rules.Targets(rule) {
			if !valid[target] && !remoteProvider {
				return fmt.Errorf("%w: rule %s targets a missing choice; inspect `nagi rules custom` and update the rule or restore its target before retrying", errRuleConflict, rule.Name)
			}
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
func pruneSavedChoices(ctx context.Context, p nagiruntime.Paths, name string, candidate []byte) error {
	path := filepath.Join(p.ConfigDir, "proxy-selections.json")
	store := proxy.SelectionStore{Path: path}
	groups, err := effectiveGroups(p.ConfigDir, name, candidate)
	if err != nil {
		return err
	}
	if live, err := proxy.NewService(mutationClient(p)).Groups(ctx); err == nil {
		// A successful mihomo response is authoritative, including provider nodes.
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

func effectiveGroups(dir, name string, candidate []byte) (map[string]map[string]bool, error) {
	profiles := profile.NewStore(dir, nil, nil)
	effective, err := profiles.EffectiveSource(name, candidate)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(effective, &doc); err != nil || doc == nil {
		return nil, errors.New("effective profile is invalid")
	}
	providers := map[string]map[string]bool{}
	if raw, ok := doc["proxy-providers"].(map[string]any); ok {
		for providerName, value := range raw {
			m, _ := value.(map[string]any)
			nodes := map[string]bool{}
			known := false
			if payload, ok := m["payload"].([]any); ok {
				for _, item := range payload {
					if node, ok := item.(map[string]any); ok {
						if n, _ := node["name"].(string); n != "" {
							nodes[n] = true
							known = true
						}
					}
				}
			}
			if typ, _ := m["type"].(string); typ != "inline" || !known {
				nodes["*"] = true
			}
			providers[providerName] = nodes
		}
	}
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
		if uses, ok := g["use"].([]any); ok {
			for _, item := range uses {
				if provider, ok := item.(string); ok {
					for node := range providers[provider] {
						options[node] = true
					}
				}
			}
		}
		groups[n] = options
	}
	return groups, nil
}
