package profile

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ronigooja/Nagi/internal/rules"
	"gopkg.in/yaml.v3"
)

func (s *Store) overridePath(name string) (string, error) {
	if !validName(name) {
		return "", i18n.Errorf("backend.profile.invalid_profile_name", "invalid profile name")
	}
	return filepath.Join(s.Dir, "overrides", name+".yaml"), nil
}

func (s *Store) Override(name string) ([]byte, error) {
	path, err := s.overridePath(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, i18n.Errorf("backend.profile.override_is_not_a_regular_file", "override is not a regular file")
	}
	return os.ReadFile(path)
}

func mergeMap(base, patch map[string]any) {
	for key, value := range patch {
		// DNS policy must replace the source policy so a plaintext source
		// resolver cannot remain active after a protected DNS override.
		if key == "nameserver-policy" || key == "proxy-server-nameserver-policy" {
			base[key] = value
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			if old, ok := base[key].(map[string]any); ok {
				mergeMap(old, nested)
				continue
			}
		}
		base[key] = value
	}
}

// EffectiveSource merges a candidate source profile with the named profile's
// local override and applies Nagi-owned rules. It is used while validating an
// update before replacing the source file.
func (s *Store) EffectiveSource(name string, source []byte) ([]byte, error) {
	data, err := s.effectiveSource(name, source)
	if err != nil || s.EffectiveOverlay == nil {
		return data, err
	}
	return s.EffectiveOverlay(data)
}

func (s *Store) effectiveSource(name string, source []byte) ([]byte, error) {
	patch, err := s.Override(name)
	return s.effectiveSourceWithOverride(name, source, patch, err)
}

func (s *Store) effectiveSourceWithOverride(name string, source, patch []byte, err error, skipChain ...bool) ([]byte, error) {
	unmanaged := len(skipChain) > 0 && skipChain[0]
	applyManaged := s.applySubscriptionChain
	if unmanaged {
		applyManaged = func(_ string, data []byte) ([]byte, error) { return data, nil }
	} else if checkErr := s.validateSubscriptionChainSource(name, source); checkErr != nil {
		return nil, checkErr
	}
	if errors.Is(err, fs.ErrNotExist) {
		source, err := s.applySubscriptionHealthcheckSource(name, source)
		if err != nil {
			return nil, err
		}
		managed, err := applyManaged(name, source)
		if err != nil {
			return nil, err
		}
		managed, err = s.applyProviderChainsData(name, managed)
		if err != nil {
			return nil, err
		}
		applied, err := rules.Apply(managed, s.Dir, name)
		if err != nil {
			return nil, err
		}
		rewritten, _, err := RewriteTailscaleState(applied, s.Dir, name, false)
		return rewritten, err
	}
	if err != nil {
		return nil, err
	}
	var base, overlay map[string]any
	if err := yaml.Unmarshal(source, &base); err != nil || base == nil {
		return nil, i18n.Errorf("backend.profile.profile_yaml_cannot_be_merged_with_override", "profile YAML cannot be merged with override")
	}
	if err := yaml.Unmarshal(patch, &overlay); err != nil || overlay == nil {
		return nil, i18n.Errorf("backend.profile.override_is_invalid_yaml", "override is invalid YAML")
	}
	if replace, exists := overlay[DNSReplaceKey]; exists {
		if replace != true {
			return nil, i18n.Errorf("backend.profile.nagi_dns_replace_must_be_true", "nagi-dns-replace must be true")
		}
		if _, ok := overlay["dns"].(map[string]any); !ok {
			return nil, i18n.Errorf("backend.profile.nagi_dns_replace_requires_a_dns_mapping", "nagi-dns-replace requires a DNS mapping")
		}
		delete(base, "dns")
	}
	delete(overlay, DNSReplaceKey)
	delete(overlay, DNSProxyGroupKey)
	additions, _ := overlay[proxyAdditionsKey].([]any)
	if raw, exists := overlay[proxyAdditionsKey]; exists {
		if _, ok := raw.([]any); !ok {
			return nil, i18n.Errorf("backend.profile.local_proxy_additions_must_be_a_list", "local proxy additions must be a list")
		}
	}
	delete(overlay, proxyAdditionsKey)
	urls := overlay[groupHealthcheckURLsKey]
	delete(overlay, groupHealthcheckURLsKey)
	intervals := overlay[groupHealthcheckIntervalsKey]
	delete(overlay, groupHealthcheckIntervalsKey)
	var original map[string]any
	if err := yaml.Unmarshal(source, &original); err != nil {
		return nil, err
	}
	mergeMap(base, overlay)
	if err := s.applySubscriptionHealthcheck(name, base, original, overlay); err != nil {
		return nil, err
	}
	if err := applyGroupHealthcheckIntervals(base, intervals); err != nil {
		return nil, err
	}
	if err := applyGroupHealthcheckURLs(base, urls); err != nil {
		return nil, err
	}
	for _, value := range additions {
		node, _ := value.(map[string]any)
		if node == nil || !chainHop(tsString(node, "type")) || ValidateTailscaleName(tsString(node, "name")) != nil {
			return nil, i18n.Errorf("backend.profile.chain_local_proxy_additions_must_contain_named_compatible_nodes", "local proxy additions must contain named compatible nodes")
		}
		providers, _ := base["proxy-providers"].(map[string]any)
		if _, exists := providers[tsString(node, "name")]; exists {
			return nil, i18n.Errorf("backend.profile.local_proxy_addition_conflicts_with_a_source_provider", "local proxy addition conflicts with a source provider")
		}
		groups, _ := base["proxy-groups"].([]any)
		for _, v := range groups {
			m, _ := v.(map[string]any)
			if tsString(m, "name") == tsString(node, "name") {
				return nil, i18n.Errorf("backend.profile.local_proxy_addition_conflicts_with_a_source_group", "local proxy addition conflicts with a source group")
			}
		}
		if existing := tsFind(base, tsString(node, "name")); existing != nil {
			if tsString(existing, "type") != tsString(node, "type") {
				return nil, i18n.Errorf("backend.profile.local_proxy_addition_conflicts_with_a_source_node", "local proxy addition conflicts with a source node")
			}
			for i, v := range tsProxies(base) {
				m, _ := v.(map[string]any)
				if tsString(m, "name") == tsString(node, "name") {
					tsProxies(base)[i] = node
				}
			}
		} else {
			base["proxies"] = append(tsProxies(base), node)
		}
	}
	merged, err := yaml.Marshal(base)
	if err != nil {
		return nil, err
	}
	managed, err := applyManaged(name, merged)
	if err != nil {
		return nil, err
	}
	managed, err = s.applyProviderChainsData(name, managed)
	if err != nil {
		return nil, err
	}
	applied, err := rules.Apply(managed, s.Dir, name)
	if err != nil {
		return nil, err
	}
	rewritten, _, err := RewriteTailscaleState(applied, s.Dir, name, false)
	return rewritten, err
}

func (s *Store) Effective(name string) ([]byte, error) {
	base, err := s.Show(name)
	if err != nil {
		return nil, err
	}
	return s.EffectiveSource(name, base)
}

// ApplyDefaultOverride copies safe local runtime settings from the default profile
// onto the named profile, excluding nodes, groups, rules, and Nagi-managed fields.
func (s *Store) ApplyDefaultOverride(ctx context.Context, name string, dnsProxy ...string) error {
	if len(dnsProxy) > 1 {
		return i18n.Errorf("backend.profile.at_most_one_dns_proxy_group_is_allowed", "at most one DNS proxy group is allowed")
	}
	group := ""
	if len(dnsProxy) == 1 {
		group = dnsProxy[0]
	} else {
		var err error
		group, err = s.DefaultDNSProxyGroup(name)
		if err != nil {
			return err
		}
	}
	data, err := s.Show("default")
	if err != nil {
		return i18n.Errorf("backend.profile.default_profile_is_unavailable_w", "default profile is unavailable: %w", err)
	}
	var source map[string]any
	if err := yaml.Unmarshal(data, &source); err != nil || source == nil {
		return i18n.Errorf("backend.profile.default_profile_must_be_a_yaml_mapping", "default profile must be a YAML mapping")
	}
	for key := range defaultOverrideExcludedKeys {
		delete(source, key)
	}
	source["dns"] = DefaultDNS(group)
	source["preserve-proxy-hostname"] = false
	source[DNSReplaceKey] = true
	if group != "" {
		source[DNSProxyGroupKey] = group
	}
	// Applying defaults to an existing profile must not disconnect applications
	// that currently reach mihomo through its TUN adapter.
	current, err := s.Effective(name)
	if err != nil {
		return err
	}
	var target map[string]any
	if err := yaml.Unmarshal(current, &target); err != nil || target == nil {
		return i18n.Errorf("backend.profile.target_profile_must_be_a_yaml_mapping", "target profile must be a YAML mapping")
	}
	if group == "" {
		_, nodes, providers, err := dnsGroups(current)
		if err != nil {
			return err
		}
		if len(nodes) > 0 || len(providers) > 0 {
			return i18n.Errorf("backend.profile.choose_a_dns_proxy_group_for_a_profile_with_proxy_nodes_or_providers", "choose a DNS proxy group for a profile with proxy nodes or providers")
		}
	}
	if group != "" {
		if err := dnsGroupWithSavedSelection(current, s.Dir, name, group); err != nil {
			return err
		}
	}
	currentTUN, _ := target["tun"].(map[string]any)
	if enabled, ok := currentTUN["enable"].(bool); ok {
		defaultTUN, _ := source["tun"].(map[string]any)
		if defaultTUN == nil {
			defaultTUN = map[string]any{}
			source["tun"] = defaultTUN
		}
		defaultTUN["enable"] = enabled
	}
	filtered, err := yaml.Marshal(source)
	if err != nil {
		return i18n.Errorf("backend.profile.default_profile_settings_could_not_be_prepared_w", "default profile settings could not be prepared: %w", err)
	}
	old, oldErr := s.Override(name)
	if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
		return oldErr
	}
	if oldErr == nil {
		var previous map[string]any
		if err := yaml.Unmarshal(old, &previous); err != nil || previous == nil {
			return i18n.Errorf("backend.profile.existing_override_must_be_a_yaml_mapping", "existing override must be a YAML mapping")
		}
		for _, key := range []string{"proxies", "proxy-providers", "proxy-groups", "rules", "rule-providers", proxyAdditionsKey, groupHealthcheckIntervalsKey, groupHealthcheckURLsKey, ProviderChainMembersKey} {
			if value, ok := previous[key]; ok {
				source[key] = value
			}
		}
		filtered, err = yaml.Marshal(source)
		if err != nil {
			return i18n.Errorf("backend.profile.default_profile_settings_could_not_be_prepared_w", "default profile settings could not be prepared: %w", err)
		}
	}
	statePath, err := s.defaultOverrideStatePath(name)
	if err != nil {
		return err
	}
	previousState, stateErr := os.ReadFile(statePath)
	if stateErr != nil && !errors.Is(stateErr, fs.ErrNotExist) {
		return stateErr
	}
	if stateErr == nil {
		active, err := s.DefaultOverrideStatus(name)
		if err == nil && active {
			var saved defaultOverrideState
			if err := json.Unmarshal(previousState, &saved); err != nil {
				return err
			}
			old, oldErr = saved.Previous, nil
			if !saved.HadPrevious {
				old, oldErr = nil, fs.ErrNotExist
			}
		}
	}
	state := defaultOverrideState{HadPrevious: oldErr == nil, Previous: old, AppliedHash: sha256.Sum256(filtered)}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return err
	}
	if err := writeReplace(statePath, encoded); err != nil {
		return err
	}
	if err := s.SetOverride(ctx, name, filtered); err != nil {
		return errors.Join(err, restore(statePath, previousState, errors.Is(stateErr, fs.ErrNotExist)))
	}
	return nil
}

type defaultOverrideState struct {
	HadPrevious bool     `json:"had_previous"`
	Previous    []byte   `json:"previous"`
	AppliedHash [32]byte `json:"applied_hash"`
}

func (s *Store) defaultOverrideStatePath(name string) (string, error) {
	path, err := s.overridePath(name)
	if err != nil {
		return "", err
	}
	return path + ".default-state.json", nil
}

// DefaultOverrideStatus reports whether the saved default overlay still matches the active override.
func (s *Store) DefaultOverrideStatus(name string) (bool, error) {
	path, err := s.defaultOverrideStatePath(name)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var state defaultOverrideState
	if err := json.Unmarshal(data, &state); err != nil {
		return false, i18n.Errorf("backend.profile.saved_default_override_state_is_invalid_w", "saved default override state is invalid: %w", err)
	}
	current, err := s.Override(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sha256.Sum256(current) == state.AppliedHash, nil
}

// RestoreDefaultOverride restores the exact local override that preceded apply-default.
func (s *Store) RestoreDefaultOverride(ctx context.Context, name string) error {
	active, err := s.DefaultOverrideStatus(name)
	if err != nil {
		return err
	}
	if !active {
		return i18n.Errorf("backend.profile.local_default_settings_are_not_active_current_override_was_changed_or_no_restore_point_exists", "local default settings are not active; current override was changed or no restore point exists")
	}
	path, err := s.defaultOverrideStatePath(name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var state defaultOverrideState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.HadPrevious {
		err = s.SetOverride(ctx, name, state.Previous)
	} else {
		err = s.ClearOverride(ctx, name)
	}
	if err != nil {
		return err
	}
	return os.Remove(path)
}

var defaultOverrideExcludedKeys = map[string]struct{}{
	"proxies": {}, "proxy-providers": {}, "proxy-groups": {}, "rules": {}, "rule-providers": {}, proxyAdditionsKey: {}, ProviderChainMembersKey: {}, groupHealthcheckIntervalsKey: {}, groupHealthcheckURLsKey: {},
	"external-controller": {}, "external-controller-unix": {}, "external-controller-tls": {},
	"external-controller-pipe": {}, "external-controller-cors": {}, "external-controller-routing-mark": {},
	"external-ui": {}, "external-ui-url": {}, "secret": {}, "authentication": {},
}

// SetOverride stores a user-owned YAML mapping separately from the source profile.
func (s *Store) SetOverride(ctx context.Context, name string, data []byte) error {
	if len(data) == 0 || len(data) > 8<<20 {
		return i18n.Errorf("backend.profile.override_must_contain_1_byte_to_8_mib", "override must contain 1 byte to 8 MiB")
	}
	path, err := s.overridePath(name)
	if err != nil {
		return err
	}
	if _, err := s.Show(name); err != nil {
		return err
	}
	var patch map[string]any
	if err := yaml.Unmarshal(data, &patch); err != nil || patch == nil {
		return i18n.Errorf("backend.profile.override_must_be_a_yaml_mapping", "override must be a YAML mapping")
	}
	policy, err := s.SubscriptionChain(name)
	if err != nil {
		return err
	}
	if policy != nil {
		if err := s.sanitizeManagedSnapshot(name, patch, policy); err != nil {
			return err
		}
		data, err = yaml.Marshal(patch)
		if err != nil {
			return err
		}
	}

	old, oldErr := s.Override(name)
	if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
		return oldErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := writeReplace(path, data); err != nil {
		return err
	}
	rollback := func(cause error) error {
		return errors.Join(cause, restore(path, old, errors.Is(oldErr, fs.ErrNotExist)))
	}
	effective, err := s.Effective(name)
	if err != nil {
		return rollback(err)
	}
	if err := s.validate(name, effective); err != nil {
		return rollback(err)
	}
	current, err := s.Current()
	if err != nil {
		return rollback(err)
	}
	if current == name && s.Reload != nil {
		if err := s.Reload(ctx); err != nil {
			return rollback(err)
		}
	}
	return s.CleanupProviderChainCaches(name)
}

func (s *Store) ClearOverride(ctx context.Context, name string) error {
	path, err := s.overridePath(name)
	if err != nil {
		return err
	}
	old, err := s.Override(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	rollback := func(cause error) error { return errors.Join(cause, writeReplace(path, old)) }
	base, err := s.Show(name)
	if err != nil {
		return rollback(err)
	}
	if err := s.validate(name, base); err != nil {
		return rollback(err)
	}
	if err := s.validateManagedCandidate(name, base); err != nil {
		return rollback(err)
	}
	current, err := s.Current()
	if err != nil {
		return rollback(err)
	}
	if current == name && s.Reload != nil {
		if err := s.Reload(ctx); err != nil {
			return rollback(err)
		}
	}
	return s.CleanupProviderChainCaches(name)
}
