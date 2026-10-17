package command

import (
	"context"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func subscriptionHealthcheckResult(entry subscription.Entry) profile.Healthcheck {
	r := profile.Healthcheck{Kind: "subscription", Name: entry.Name, Profile: entry.Profile, Lazy: true, Source: "profile", IntervalDefaulted: entry.HealthcheckIntervalSeconds == nil,
		Reason: "Follows the subscription configuration and mihomo defaults. Provider and group local intervals take precedence in their native schedulers; provider enable and URL remain unchanged."}
	if entry.HealthcheckIntervalSeconds != nil {
		r.IntervalSeconds = *entry.HealthcheckIntervalSeconds
		r.Source = "override"
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
