package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/ronigooja/Nagi/internal/profile"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"github.com/ronigooja/Nagi/internal/subscription"
)

// ApplyCachedSubscriptionPreservingSource is the reusable activation path for
// automatic updates. It preserves the local override and saved proxy choices;
// explicit `subscription apply` intentionally uses replacement semantics.
// The caller refreshes the cache separately and supplies a profile store with
// its normal runtime reload callback. Refresh metadata and cache written before
// this call are the transaction baseline and remain after a failed apply. The
// caller must ensure name is the currently selected profile associated with
// the automatic update; this function does not implement that association.
func ApplyCachedSubscriptionPreservingSource(ctx context.Context, paths nagiruntime.Paths, cache *subscription.Store, profiles *profile.Store, name string) error {
	_, err := withNamedLock(paths, "config.lock", func() (any, error) {
		return applyTransaction(ctx, paths, []string{"subscription", "apply-preserving"}, func() (any, error) {
			data, err := cache.Cached(name)
			if err != nil {
				return nil, err
			}
			doc, _, err := subscription.Document(data)
			if err != nil {
				return nil, err
			}
			doc, err = subscription.NormalizeChecked(doc)
			if err != nil {
				return nil, err
			}
			encoded, err := subscription.Encode(doc)
			if err != nil {
				return nil, err
			}
			if profiles == nil {
				return nil, errors.New("profile store is required")
			}
			if err := profiles.Apply(ctx, name, encoded); err != nil {
				return nil, fmt.Errorf("apply cached subscription while preserving local settings: %w", err)
			}
			return nil, nil
		})
	})
	return err
}
