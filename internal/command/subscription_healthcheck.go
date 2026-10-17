package command

import (
	"context"
	"net/url"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func subscriptionHealthcheckResult(entry subscription.Entry) profile.Healthcheck {
	r := profile.Healthcheck{Kind: "subscription", Name: entry.Name, Profile: entry.Profile, Lazy: true, Source: "profile", CanResetURL: true, IntervalDefaulted: entry.HealthcheckIntervalSeconds == nil,
		Reason: "Follows the subscription configuration and mihomo defaults. Provider and group local intervals take precedence in their native schedulers; provider enablement and separate local settings are retained."}
	if entry.HealthcheckIntervalSeconds != nil {
		r.IntervalSeconds = *entry.HealthcheckIntervalSeconds
		r.Source = "override"
	}
	r.URLSource = "profile"
	if entry.HealthcheckURL != nil {
		u, _ := url.Parse(*entry.HealthcheckURL)
		r.TestURL = u.Scheme + "://" + u.Host
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
