package profile

import (
	"context"
	"errors"
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
		return errors.New("NAME conflicts with a reserved mihomo provider name")
	}
	if !validName(name) {
		return errors.New("NAME must contain 1..128 ASCII letters, digits, _ or -")
	}
	for _, value := range []string{group} {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
			return errors.New("GROUP must contain 1..128 bytes without surrounding whitespace")
		}
		for _, r := range value {
			if !unicode.IsPrint(r) {
				return errors.New("GROUP must contain printable characters")
			}
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.Contains(rawURL, "#") {
		return errors.New("URL must be an absolute HTTP(S) URL without userinfo or fragment")
	}
	if interval < 0 || interval > 31536000 {
		return errors.New("SECONDS must be off or an integer from 1 to 31536000")
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
		return false, errors.New("selected profile must be a YAML mapping")
	}
	providers, ok := effective["proxy-providers"].(map[string]any)
	if value, exists := effective["proxy-providers"]; exists && value != nil && !ok {
		return false, errors.New("proxy-providers must be a mapping")
	}
	if _, exists := providers[name]; exists {
		return false, errors.New("provider already exists in the selected effective profile; choose another NAME")
	}
	// mihomo creates compatibility providers named after groups with explicit
	// proxies. Such names must not collide with an HTTP provider.
	groups := []any{}
	if value, exists := effective["proxy-groups"]; exists && value != nil {
		var ok bool
		groups, ok = value.([]any)
		if !ok {
			return false, errors.New("proxy-groups must be a list")
		}
	}
	var target map[string]any
	for _, value := range groups {
		item, ok := value.(map[string]any)
		if !ok {
			return false, errors.New("proxy-groups must contain mappings")
		}
		itemName, _ := item["name"].(string)
		if itemName == name {
			return false, errors.New("NAME conflicts with an existing proxy group; choose another provider name")
		}
		if itemName == group {
			if target != nil {
				return false, errors.New("GROUP is duplicated in the selected profile")
			}
			target = item
		}
	}
	created := target == nil
	if created {
		switch group {
		case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL":
			return false, errors.New("GROUP conflicts with a reserved mihomo proxy name")
		}
		if nodes, ok := effective["proxies"].([]any); ok {
			for _, value := range nodes {
				node, _ := value.(map[string]any)
				nodeName, _ := node["name"].(string)
				if nodeName == group {
					return false, errors.New("GROUP conflicts with an existing proxy node")
				}
			}
		}
		if group == name {
			return false, errors.New("NAME and a new GROUP must be different")
		}
		if _, exists := providers[group]; exists {
			return false, errors.New("GROUP conflicts with an existing provider name")
		}
		target = map[string]any{"name": group, "type": "select", "proxies": []any{"DIRECT"}}
		groups = append(groups, target)
	}
	switch target["type"] {
	case "select", "url-test", "fallback", "load-balance":
	default:
		return false, errors.New("GROUP must use select, url-test, fallback, or load-balance; choose a supported group or a new group name")
	}
	use := []any{}
	if value, exists := target["use"]; exists && value != nil {
		var ok bool
		use, ok = value.([]any)
		if !ok {
			return false, errors.New("GROUP use must be a list")
		}
		for _, value := range use {
			if _, ok := value.(string); !ok {
				return false, errors.New("GROUP use must contain provider names")
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
			return false, errors.New("local override must be a YAML mapping")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	localProviders, ok := overlay["proxy-providers"].(map[string]any)
	if value, exists := overlay["proxy-providers"]; exists && value != nil && !ok {
		return false, errors.New("local override proxy-providers must be a mapping")
	}
	if localProviders == nil {
		localProviders = map[string]any{}
	}
	healthcheck := map[string]any{"enable": true, "url": "https://www.gstatic.com/generate_204", "interval": 300}
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
		return false, errors.New("could not prepare provider override")
	}
	if err := s.SetOverride(ctx, profileName, updated); err != nil {
		return false, err
	}
	return created, nil
}
