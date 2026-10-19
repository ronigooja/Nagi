package command

import (
	"context"
	"github.com/ronigooja/Nagi/internal/i18n"
	"net/url"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func subscriptionHealthcheckResult(entry subscription.Entry) profile.Healthcheck {
	r := profile.Healthcheck{Kind: "subscription", Name: entry.Name, Profile: entry.Profile, Lazy: true, Source: "profile", CanResetURL: true, IntervalDefaulted: entry.HealthcheckIntervalSeconds == nil,
		Reason: "Follows the subscription configuration and mihomo defaults. Provider and group local intervals take precedence in their native schedulers; provider enablement and separate local settings are retained."}
	r.Messages = map[string]i18n.Descriptor{"/reason": {ID: "cli.subscription.healthcheck.inherited"}}
	if entry.HealthcheckIntervalSeconds != nil {
		r.IntervalSeconds = *entry.HealthcheckIntervalSeconds
		r.Source = "override"
	}
	r.URLSource = "profile"
	if entry.HealthcheckURL != nil {
		u, _ := url.Parse(*entry.HealthcheckURL)
		r.TestURL = u.Scheme + "://" + u.Host
		r.FullTestURL = *entry.HealthcheckURL
		r.URLSource = "override"
	}
	return r
}

// The enclosing configuration transaction restores both metadata and effective
// configuration if validation or activation fails. No feed fetch or probe runs.
func mutateSubscriptionHealthcheck(ctx context.Context, subscriptions *subscription.Store, profiles *profile.Store, name string, seconds *int) (profile.Healthcheck, error) {
	entry, err := subscriptions.Find(name)
	if err != nil {
		return profile.Healthcheck{}, err
	}
	if err = subscriptions.SetHealthcheckInterval(name, seconds); err != nil {
		return profile.Healthcheck{}, err
	}
	if entry.Profile != "" {
		if err = profiles.ActivateHealthcheckPolicy(ctx, entry.Profile); err != nil {
			return profile.Healthcheck{}, err
		}
	}
	entry.HealthcheckIntervalSeconds = seconds
	return subscriptionHealthcheckResult(entry), nil
}

func mutateSubscriptionHealthcheckURL(ctx context.Context, subscriptions *subscription.Store, profiles *profile.Store, name string, value *string) (profile.Healthcheck, error) {
	entry, err := subscriptions.Find(name)
	if err != nil {
		return profile.Healthcheck{}, err
	}
	if err = subscriptions.SetHealthcheckURL(name, value); err != nil {
		return profile.Healthcheck{}, err
	}
	if entry.Profile != "" {
		if err = profiles.ActivateHealthcheckPolicy(ctx, entry.Profile); err != nil {
			return profile.Healthcheck{}, err
		}
	}
	entry.HealthcheckURL = value
	return subscriptionHealthcheckResult(entry), nil
}

// Editing responses include an inherited value only when all applicable
// checkers agree. Default inspection and mutation responses retain their shape.
func healthcheckEditingResult(r profile.Healthcheck, store *profile.Store) (profile.Healthcheck, error) {
	r.TestURL = r.FullTestURL
	r.InheritedIntervalStatus, r.InheritedURLStatus = "unavailable", "unavailable"
	if r.Profile == "" {
		r.InheritedIntervalStatus, r.InheritedURLStatus = "not_applied", "not_applied"
		return r, nil
	}
	values, err := store.InheritedHealthchecks(r.Profile, r.Kind, r.Name)
	if err != nil {
		return r, err
	}
	if len(values) == 0 {
		return r, nil
	}
	seconds, testURL := values[0].IntervalSeconds, values[0].FullTestURL
	sameInterval, sameURL := true, true
	for _, value := range values[1:] {
		sameInterval = sameInterval && value.IntervalSeconds == seconds
		sameURL = sameURL && value.FullTestURL == testURL
	}
	if !sameInterval {
		r.InheritedIntervalStatus = "mixed"
	} else if seconds > 0 {
		r.InheritedIntervalSeconds = &seconds
		r.InheritedIntervalStatus = "known"
	}
	if !sameURL {
		r.InheritedURLStatus = "mixed"
	} else if testURL != "" {
		r.InheritedTestURL = testURL
		r.InheritedURLStatus = "known"
	}
	return r, nil
}
