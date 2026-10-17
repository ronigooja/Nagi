package profile

import (
	"context"
	"errors"
	"reflect"

	"github.com/ronigooja/Nagi/internal/subscription"
	"gopkg.in/yaml.v3"
)

// subscriptionHealthcheck resolves only a persisted source association. Reading
// effective configuration does not create a subscription index or default profile.
func (s *Store) subscriptionHealthcheck(name string) (*int, error) {
	entries, err := subscription.NewStore(s.Dir, "", nil).List()
	if err != nil {
		return nil, err
	}
	var interval *int
	associations := 0
	for _, entry := range entries {
		if entry.Profile != name {
			continue
		}
		associations++
		if entry.HealthcheckIntervalSeconds != nil {
			interval = entry.HealthcheckIntervalSeconds
		}
	}
	if associations > 1 && interval != nil {
		return nil, errors.New("profile has multiple subscription associations; reapply the intended subscription")
	}
	return interval, nil
}

func subscriptionGroupEligible(group map[string]any) bool {
	switch group["type"] {
	case "url-test", "fallback", "load-balance":
		return true
	case "select":
		n, _ := healthInterval(group["interval"])
		return n > 0
	}
	return false
}

// subscriptionHealthcheckURL resolves persistent test URL intent independently
// of interval policy, without creating profile or subscription state.
func (s *Store) subscriptionHealthcheckURL(name string) (*string, error) {
	entries, err := subscription.NewStore(s.Dir, "", nil).List()
	if err != nil {
		return nil, err
	}
	var value *string
	associations := 0
	for _, entry := range entries {
		if entry.Profile != name {
			continue
		}
		associations++
		if entry.HealthcheckURL != nil {
			value = entry.HealthcheckURL
		}
	}
	if associations > 1 && value != nil {
		return nil, errors.New("profile has multiple subscription associations; reapply the intended subscription")
	}
	return value, nil
}

// Subscription policies supply native fields while preserving explicit local
// provider leaves and group differences. Explicit group metadata applies later.
func (s *Store) applySubscriptionHealthcheck(name string, base, source, overlay map[string]any) error {
	interval, err := s.subscriptionHealthcheck(name)
	if err != nil {
		return err
	}
	testURL, err := s.subscriptionHealthcheckURL(name)
	if err != nil {
		return err
	}
	return applyHealthcheckPolicy(base, source, overlay, interval, testURL)
}

func applyHealthcheckPolicy(base, source, overlay map[string]any, interval *int, testURL *string) error {
	if interval == nil && testURL == nil {
		return nil
	}
	policy := map[string]any{}
	if interval != nil {
		policy["interval"] = *interval
	}
	if testURL != nil {
		policy["url"] = *testURL
	}
	providers, _ := base["proxy-providers"].(map[string]any)
	for providerName, value := range providers {
		provider, _ := value.(map[string]any)
		check := healthFields(provider, "provider")
		local, _ := healthTarget(overlay, "provider", providerName)
		if check == nil {
			continue
		}
		for field, value := range policy {
			if _, explicit := healthFields(local, "provider")[field]; !explicit {
				check[field] = value
			}
		}
	}
	_, groups, err := namedList(base["proxy-groups"])
	if err != nil {
		return err
	}
	for groupName, group := range groups {
		if !subscriptionGroupEligible(group) {
			continue
		}
		local, _ := healthTarget(overlay, "group", groupName)
		original, _ := healthTarget(source, "group", groupName)
		for field, value := range policy {
			if existing, explicit := local[field]; explicit && !reflect.DeepEqual(existing, original[field]) {
				continue
			}
			group[field] = value
		}
	}
	return nil
}

func (s *Store) applySubscriptionHealthcheckSource(name string, data []byte) ([]byte, error) {
	hasPolicy, err := s.HasSubscriptionHealthcheck(name)
	if err != nil || !hasPolicy {
		return data, err
	}
	var base map[string]any
	if yaml.Unmarshal(data, &base) != nil || base == nil {
		return nil, errors.New("subscription profile must be a YAML mapping")
	}
	if err := s.applySubscriptionHealthcheck(name, base, base, nil); err != nil {
		return nil, err
	}
	return yaml.Marshal(base)
}

// HasSubscriptionHealthcheck reports whether effective composition adds a policy.
func (s *Store) HasSubscriptionHealthcheck(name string) (bool, error) {
	interval, err := s.subscriptionHealthcheck(name)
	if err != nil {
		return false, err
	}
	testURL, err := s.subscriptionHealthcheckURL(name)
	return interval != nil || testURL != nil, err
}

// ActivateHealthcheckPolicy validates changed metadata and reloads only its
// selected profile. The caller owns the transaction and configuration lock.
func (s *Store) ActivateHealthcheckPolicy(ctx context.Context, name string) error {
	effective, err := s.Effective(name)
	if err != nil {
		return err
	}
	if err = s.validate(effective); err != nil {
		return err
	}
	current, err := s.Current()
	if err != nil {
		return err
	}
	if current == name && s.Reload != nil {
		return s.Reload(ctx)
	}
	return nil
}
