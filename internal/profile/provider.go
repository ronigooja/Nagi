package profile

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io/fs"
	"net/url"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const DefaultProviderInterval = 3600

// ValidateProviderInput checks values without exposing the subscription URL.
func ValidateProviderInput(name, rawURL, group string, interval int) error {
	if name == "COMPATIBLE" {
		return i18n.Errorf("backend.profile.name_conflicts_with_a_reserved_mihomo_provider_name", "NAME conflicts with a reserved mihomo provider name")
	}
	if !validName(name) {
		return i18n.Errorf("backend.profile.name_must_contain_1_128_ascii_letters_digits_or", "NAME must contain 1..128 ASCII letters, digits, _ or -")
	}
	for _, value := range []string{group} {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
			return i18n.Errorf("backend.profile.group_must_contain_1_128_bytes_without_surrounding_whitespace", "GROUP must contain 1..128 bytes without surrounding whitespace")
		}
		for _, r := range value {
			if !unicode.IsPrint(r) {
				return i18n.Errorf("backend.profile.group_must_contain_printable_characters", "GROUP must contain printable characters")
			}
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.Contains(rawURL, "#") {
		return i18n.Errorf("backend.profile.url_must_be_an_absolute_http_s_url_without_userinfo_or_fragment", "URL must be an absolute HTTP(S) URL without userinfo or fragment")
	}
	if interval < 0 || interval > 31536000 {
		return i18n.Errorf("backend.profile.seconds_must_be_off_or_an_integer_from_1_to_31536000", "SECONDS must be off or an integer from 1 to 31536000")
	}
	return nil
}

// AddProvider persists an HTTP provider and group attachment in the local
// override. Downloading and healthchecking remain mihomo responsibilities.
// Callers serialize mutations and wrap activation in their configuration transaction.
func (s *Store) AddProvider(ctx context.Context, profileName, name, rawURL, group string, interval int, testURLs ...string) (bool, error) {
	if err := ValidateProviderInput(name, rawURL, group, interval); err != nil {
		return false, err
	}
	if len(testURLs) > 0 {
		if err := ValidateHealthcheckURL(testURLs[0]); err != nil {
			return false, err
		}
	}
	data, err := s.Effective(profileName)
	if err != nil {
		return false, err
	}
	var effective map[string]any
	if yaml.Unmarshal(data, &effective) != nil || effective == nil {
		return false, i18n.Errorf("backend.profile.selected_profile_must_be_a_yaml_mapping", "selected profile must be a YAML mapping")
	}
	providers, ok := effective["proxy-providers"].(map[string]any)
	if value, exists := effective["proxy-providers"]; exists && value != nil && !ok {
		return false, i18n.Errorf("backend.profile.proxy_providers_must_be_a_mapping", "proxy-providers must be a mapping")
	}
	if _, exists := providers[name]; exists {
		return false, i18n.Errorf("backend.profile.provider_already_exists_in_the_selected_effective_profile_choose_another_name", "provider already exists in the selected effective profile; choose another NAME")
	}
	// mihomo creates compatibility providers named after groups with explicit
	// proxies. Such names must not collide with an HTTP provider.
	groups := []any{}
	if value, exists := effective["proxy-groups"]; exists && value != nil {
		var ok bool
		groups, ok = value.([]any)
		if !ok {
			return false, i18n.Errorf("backend.profile.proxy_groups_must_be_a_list", "proxy-groups must be a list")
		}
	}
	var target map[string]any
	for _, value := range groups {
		item, ok := value.(map[string]any)
		if !ok {
			return false, i18n.Errorf("backend.profile.proxy_groups_must_contain_mappings", "proxy-groups must contain mappings")
		}
		itemName, _ := item["name"].(string)
		if itemName == name {
			return false, i18n.Errorf("backend.profile.name_conflicts_with_an_existing_proxy_group_choose_another_provider_name", "NAME conflicts with an existing proxy group; choose another provider name")
		}
		if itemName == group {
			if target != nil {
				return false, i18n.Errorf("backend.profile.group_is_duplicated_in_the_selected_profile", "GROUP is duplicated in the selected profile")
			}
			target = item
		}
	}
	created := target == nil
	if created {
		switch group {
		case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL":
			return false, i18n.Errorf("backend.profile.group_conflicts_with_a_reserved_mihomo_proxy_name", "GROUP conflicts with a reserved mihomo proxy name")
		}
		if nodes, ok := effective["proxies"].([]any); ok {
			for _, value := range nodes {
				node, _ := value.(map[string]any)
				nodeName, _ := node["name"].(string)
				if nodeName == group {
					return false, i18n.Errorf("backend.profile.group_conflicts_with_an_existing_proxy_node", "GROUP conflicts with an existing proxy node")
				}
			}
		}
		if group == name {
			return false, i18n.Errorf("backend.profile.name_and_a_new_group_must_be_different", "NAME and a new GROUP must be different")
		}
		if _, exists := providers[group]; exists {
			return false, i18n.Errorf("backend.profile.group_conflicts_with_an_existing_provider_name", "GROUP conflicts with an existing provider name")
		}
		target = map[string]any{"name": group, "type": "select", "proxies": []any{"DIRECT"}}
		groups = append(groups, target)
	}
	switch target["type"] {
	case "select", "url-test", "fallback", "load-balance":
	default:
		return false, i18n.Errorf("backend.profile.group_must_use_select_url_test_fallback_or_load_balance_choose_a_supported_group_or_a_new_group_name", "GROUP must use select, url-test, fallback, or load-balance; choose a supported group or a new group name")
	}
	use := []any{}
	if value, exists := target["use"]; exists && value != nil {
		var ok bool
		use, ok = value.([]any)
		if !ok {
			return false, i18n.Errorf("backend.profile.group_use_must_be_a_list", "GROUP use must be a list")
		}
		for _, value := range use {
			if _, ok := value.(string); !ok {
				return false, i18n.Errorf("backend.profile.group_use_must_contain_provider_names", "GROUP use must contain provider names")
			}
		}
	}
	found := false
	for _, value := range use {
		if value == name {
			found = true
		}
	}
	if !found {
		use = append(use, name)
	}
	target["use"] = use
	overlay := map[string]any{}
	old, err := s.Override(profileName)
	if err == nil {
		if yaml.Unmarshal(old, &overlay) != nil || overlay == nil {
			return false, i18n.Errorf("backend.profile.local_override_must_be_a_yaml_mapping", "local override must be a YAML mapping")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	localProviders, ok := overlay["proxy-providers"].(map[string]any)
	if value, exists := overlay["proxy-providers"]; exists && value != nil && !ok {
		return false, i18n.Errorf("backend.profile.local_override_proxy_providers_must_be_a_mapping", "local override proxy-providers must be a mapping")
	}
	if localProviders == nil {
		localProviders = map[string]any{}
	}
	healthcheck := map[string]any{"enable": true, "url": "https://cp.cloudflare.com/generate_204", "interval": 300}
	switch target["type"] {
	case "url-test", "fallback", "load-balance":
		// Match mihomo's group URL selection, including groups that inherit
		// their test URL from the first provider already attached to them.
		testURL, _ := target["url"].(string)
		if testURL == "" {
			for _, value := range use {
				existing, _ := providers[value.(string)].(map[string]any)
				check, _ := existing["health-check"].(map[string]any)
				testURL, _ = check["url"].(string)
				if testURL != "" {
					break
				}
			}
		}
		if testURL != "" {
			healthcheck["url"] = testURL
		}
		if expected, exists := target["expected-status"]; exists {
			healthcheck["expected-status"] = expected
		}
	}
	if len(testURLs) > 0 {
		healthcheck["url"] = testURLs[0]
	}
	localProviders[name] = map[string]any{
		"type": "http", "url": rawURL, "interval": interval,
		"health-check": healthcheck,
	}
	overlay["proxy-providers"] = localProviders
	overlay["proxy-groups"] = groups
	updated, err := yaml.Marshal(overlay)
	if err != nil {
		return false, i18n.Errorf("backend.profile.could_not_prepare_provider_override", "could not prepare provider override")
	}
	if err := s.SetOverride(ctx, profileName, updated); err != nil {
		return false, err
	}
	return created, nil
}
