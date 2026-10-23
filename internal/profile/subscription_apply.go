package profile

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io/fs"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

// subscriptionOverride rebases list snapshots against the previous feed. Other
// override fields retain their existing explicit replacement/merge semantics.
func (s *Store) subscriptionOverride(name string, source []byte) ([]byte, error) {
	patch, err := s.Override(name)
	if err != nil {
		return nil, err
	}
	old, err := s.Show(name)
	if errors.Is(err, fs.ErrNotExist) {
		old = []byte("{}")
	} else if err != nil {
		return nil, err
	}
	return rebaseSubscriptionOverride(old, source, patch)
}

func rebaseSubscriptionOverride(old, next, patch []byte) ([]byte, error) {
	var overlay map[string]any
	if yaml.Unmarshal(patch, &overlay) != nil || overlay == nil {
		return nil, i18n.Errorf("backend.profile.subscription_override_must_be_a_yaml_mapping", "subscription override must be a YAML mapping")
	}
	// No list snapshot means no dependency on the old source. This also permits
	// repairing an unreadable old feed when only runtime settings are overridden.
	_, nodes := overlay["proxies"]
	_, groups := overlay["proxy-groups"]
	if !nodes && !groups {
		return patch, nil
	}
	var previous, incoming map[string]any
	if yaml.Unmarshal(old, &previous) != nil || previous == nil {
		return nil, i18n.Errorf("backend.profile.cannot_retain_local_node_or_group_changes_from_an_invalid_previous_subscription_repair_the_source_or", "cannot retain local node or group changes from an invalid previous subscription; repair the source or edit the local override and retry")
	}
	if yaml.Unmarshal(next, &incoming) != nil || incoming == nil {
		return nil, i18n.Errorf("backend.profile.subscription_must_be_a_yaml_mapping", "subscription must be a YAML mapping")
	}
	changed := false
	for _, field := range []string{"proxies", "proxy-groups"} {
		local, exists := overlay[field]
		if !exists {
			continue
		}
		merged, err := rebaseNamedList(previous[field], incoming[field], local, field == "proxy-groups")
		if err != nil {
			return nil, i18n.Errorf("backend.profile.cannot_retain_local_s_w", "cannot retain local %s: %w", field, err)
		}
		changed = changed || !reflect.DeepEqual(local, merged)
		overlay[field] = merged
	}
	if !changed {
		return patch, nil
	}
	return yaml.Marshal(overlay)
}

func namedList(value any) ([]any, map[string]map[string]any, error) {
	if value == nil {
		return nil, map[string]map[string]any{}, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, nil, i18n.Errorf("backend.profile.expected_a_list_of_named_mappings", "expected a list of named mappings")
	}
	names := make(map[string]map[string]any, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		name, _ := entry["name"].(string)
		if !ok || name == "" {
			return nil, nil, i18n.Errorf("backend.profile.expected_a_list_of_named_mappings", "expected a list of named mappings")
		}
		if _, exists := names[name]; exists {
			return nil, nil, i18n.Errorf("backend.profile.duplicate_name_edit_the_subscription_or_local_override_and_retry", "duplicate name; edit the subscription or local override and retry")
		}
		names[name] = entry
	}
	return list, names, nil
}

func rebaseNamedList(old, next, patch any, groups bool) ([]any, error) {
	_, previous, err := namedList(old)
	if err != nil {
		return nil, err
	}
	incomingList, incoming, err := namedList(next)
	if err != nil {
		return nil, err
	}
	localList, local, err := namedList(patch)
	if err != nil {
		return nil, err
	}
	merged := make([]any, 0, len(incomingList)+len(localList))
	for _, item := range incomingList {
		node := item.(map[string]any)
		name := node["name"].(string)
		before, existed := previous[name]
		custom, retained := local[name]
		if existed && !retained { // Explicit local removal.
			continue
		}
		if !existed && retained && !reflect.DeepEqual(node, custom) {
			return nil, i18n.Errorf("backend.profile.a_new_subscription_entry_conflicts_with_a_local_entry_of_the_same_name_rename_the_local_entry_or_edi", "a new subscription entry conflicts with a local entry of the same name; rename the local entry or edit its override and retry")
		}
		if existed && retained {
			node = rebaseMapping(before, node, custom)
			if groups {
				for _, field := range []string{"proxies", "use"} {
					previousMembers, hadMembers := before[field]
					localMembers, keptMembers := custom[field]
					if hadMembers && keptMembers && !reflect.DeepEqual(previousMembers, localMembers) {
						node[field] = rebaseGroupMembers(previousMembers, incoming[name][field], localMembers)
					}
				}
			}
		}
		merged = append(merged, node)
	}
	for _, item := range localList {
		node := item.(map[string]any)
		name := node["name"].(string)
		if _, exists := incoming[name]; exists {
			continue
		}
		if before, existed := previous[name]; !existed || !sameSubscriptionEntry(before, node) {
			merged = append(merged, node)
		}
	}
	return merged, nil
}

// Group membership snapshots record local additions/removals, not ownership of
// every old subscription member. Keep incoming source order, then append local
// additions in their saved order. Invalid lists retain ordinary validation.
func rebaseGroupMembers(old, next, local any) any {
	stringsIn := func(value any) ([]string, bool) {
		if value == nil {
			return nil, true
		}
		list, ok := value.([]any)
		if !ok {
			return nil, false
		}
		out := make([]string, 0, len(list))
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, name)
		}
		return out, true
	}
	previous, oldOK := stringsIn(old)
	incoming, nextOK := stringsIn(next)
	custom, localOK := stringsIn(local)
	if !oldOK || !nextOK || !localOK {
		return local
	}
	oldNames, localNames := map[string]bool{}, map[string]bool{}
	for _, name := range previous {
		oldNames[name] = true
	}
	for _, name := range custom {
		localNames[name] = true
	}
	merged := make([]any, 0, len(incoming)+len(custom))
	seen := map[string]bool{}
	appendName := func(name string) {
		if !seen[name] {
			merged = append(merged, name)
			seen[name] = true
		}
	}
	for _, name := range incoming {
		if !oldNames[name] || localNames[name] {
			appendName(name)
		}
	}
	for _, name := range custom {
		if !oldNames[name] {
			appendName(name)
		}
	}
	return merged
}

// Unbinding a Tailscale front proxy can leave an explicit empty dialer-proxy
// in a local snapshot. When the source had no dialer, that empty value does
// not change the node and must not retain it after the feed removes it.
func sameSubscriptionEntry(source, local map[string]any) bool {
	if reflect.DeepEqual(source, local) {
		return true
	}
	emptyDialer, hasDialer := local["dialer-proxy"]
	if _, hadDialer := source["dialer-proxy"]; hadDialer || !hasDialer || emptyDialer != "" {
		return false
	}
	withoutEmptyDialer := make(map[string]any, len(local)-1)
	for key, value := range local {
		if key != "dialer-proxy" {
			withoutEmptyDialer[key] = value
		}
	}
	return reflect.DeepEqual(source, withoutEmptyDialer)
}

// rebaseMapping applies only local changes to the new source, including field
// removals. Nested mappings recurse; lists and scalars remain explicit values.
func rebaseMapping(old, next, patch map[string]any) map[string]any {
	merged := make(map[string]any, len(next))
	for key, value := range next {
		merged[key] = value
	}
	for key := range old {
		if _, exists := patch[key]; !exists {
			delete(merged, key)
		}
	}
	for key, value := range patch {
		before, existed := old[key]
		if existed && reflect.DeepEqual(before, value) {
			continue
		}
		previousMap, wasMap := before.(map[string]any)
		incomingMap, isMap := next[key].(map[string]any)
		localMap, localIsMap := value.(map[string]any)
		if wasMap && isMap && localIsMap {
			value = rebaseMapping(previousMap, incomingMap, localMap)
		}
		merged[key] = value
	}
	return merged
}

// EffectiveSubscriptionSource previews the same composition as ApplySubscription
// without writing source, override, or restore-point files.
func (s *Store) EffectiveSubscriptionSource(name string, source []byte) ([]byte, error) {
	patch, patchErr := s.subscriptionOverride(name, source)
	data, err := s.effectiveSourceWithOverride(name, source, patch, patchErr)
	if err != nil || s.EffectiveOverlay == nil {
		return data, err
	}
	return s.EffectiveOverlay(data)
}

// ApplySubscription replaces the feed while retaining local intent. The caller
// holds the configuration lock; failed validation/activation restores local files.
func (s *Store) ApplySubscription(ctx context.Context, name string, data []byte) error {
	patch, err := s.subscriptionOverride(name, data)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	effective, err := s.EffectiveSubscriptionSource(name, data)
	if err != nil {
		return err
	}
	if err := s.validate(name, effective); err != nil {
		return err
	}
	var undo []func() error
	rollback := func(cause error) error {
		for i := len(undo) - 1; i >= 0; i-- {
			cause = errors.Join(cause, undo[i]())
		}
		return cause
	}
	if patch != nil {
		old, err := s.Override(name)
		if err != nil {
			return err
		}
		path, _ := s.overridePath(name)
		statePath, _ := s.defaultOverrideStatePath(name)
		stateData, stateErr := os.ReadFile(statePath)
		if stateErr != nil && !errors.Is(stateErr, fs.ErrNotExist) {
			return stateErr
		}
		if stateErr == nil {
			var state defaultOverrideState
			if json.Unmarshal(stateData, &state) != nil {
				return i18n.Errorf("backend.profile.saved_default_override_state_is_invalid_inspect_the_local_default_settings_before_retrying", "saved default override state is invalid; inspect the local default settings before retrying")
			}
			if state.AppliedHash == sha256.Sum256(old) {
				if state.HadPrevious {
					previous, err := s.Show(name)
					if err != nil {
						return err
					}
					state.Previous, err = rebaseSubscriptionOverride(previous, data, state.Previous)
					if err != nil {
						return err
					}
				}
				state.AppliedHash = sha256.Sum256(patch)
				encoded, err := json.Marshal(state)
				if err != nil {
					return err
				}
				if err := writeReplace(statePath, encoded); err != nil {
					return err
				}
				undo = append(undo, func() error { return writeReplace(statePath, stateData) })
			}
		}
		if err := writeReplace(path, patch); err != nil {
			return rollback(err)
		}
		undo = append(undo, func() error { return writeReplace(path, old) })
	}
	if err := s.apply(ctx, name, data, true); err != nil {
		return rollback(err)
	}
	return nil
}
