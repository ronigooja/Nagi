package command

import (
	"context"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

// Subscription candidates are already-applied associations; binding never fetches
// a feed or selects a different profile as a hidden side effect.
func subscriptionTailscaleTargets(cache *subscription.Store, profiles *profile.Store, profileName string) ([]profile.TailscaleTarget, error) {
	entries, err := cache.List()
	if err != nil {
		return nil, err
	}
	policy, err := profiles.SubscriptionTailscale(profileName)
	if err != nil {
		return nil, err
	}
	result := []profile.TailscaleTarget{}
	for _, entry := range entries {
		if entry.Profile != profileName {
			continue
		}
		dialer := ""
		if policy != nil && policy.Subscription == entry.Name {
			dialer, _ = policy.Node["name"].(string)
		}
		result = append(result, profile.TailscaleTarget{Kind: "subscription", Name: entry.Name, Type: "subscription", DialerProxy: dialer})
	}
	return result, nil
}
func mutateSubscriptionTailscale(ctx context.Context, cache *subscription.Store, profiles *profile.Store, profileName, action, node string, targets []string) error {
	if len(targets) != 1 {
		return messageError("cli.error.subscription_tailscale.select_exactly_one_subscription_associated_with_the_selected_profile", "select exactly one subscription associated with the selected profile")
	}
	entry, err := cache.Find(targets[0])
	if err != nil {
		return err
	}
	if entry.Profile != profileName {
		return messageError("cli.error.subscription_tailscale.subscription_is_not_associated_with_the_selected_profile_run_nagi_su", "subscription is not associated with the selected profile; run `nagi subscription apply NAME` first")
	}
	source, err := profiles.Show(profileName)
	if err != nil {
		return err
	}
	if action == "bind" && subscription.Hash(source) != entry.SourceHash {
		return messageError("cli.error.subscription_tailscale.subscription_source_profile_changed_run_nagi_subscription_apply_name", "subscription source profile changed; run `nagi subscription apply NAME` before binding")
	}
	return profiles.MutateSubscriptionTailscale(ctx, profileName, entry.Name, action, node)
}
