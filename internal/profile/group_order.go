package profile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Display order is local intent, independent of subscription group definitions.
const groupOrderKey = "nagi-group-order"

func ValidateProxyGroupOrder(names []string) error {
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || len(name) > 128 || strings.TrimSpace(name) != name {
			return groupError("order_name", "group order requires printable names of 1..128 bytes without surrounding whitespace")
		}
		for _, r := range name {
			if !unicode.IsPrint(r) {
				return groupError("order_name", "group order requires printable names of 1..128 bytes without surrounding whitespace")
			}
		}
		if seen[name] {
			return groupError("order_duplicate", "duplicate group in display order: %s", name)
		}
		seen[name] = true
	}
	return nil
}

// Applying retained intent skips absent groups and appends new groups in source
// order. Saving a new order, in contrast, requires an exact current permutation.
func applyProxyGroupOrder(data []byte, raw any) ([]byte, error) {
	if raw == nil {
		return data, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, groupError("order_list", "group display order must be a list of names")
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			return nil, groupError("order_list", "group display order must be a list of names")
		}
		names = append(names, name)
	}
	if err := ValidateProxyGroupOrder(names); err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	groups, byName, err := namedList(cfg["proxy-groups"])
	if err != nil {
		return nil, err
	}
	ordered := make([]any, 0, len(groups))
	seen := map[string]bool{}
	for _, name := range names {
		if group, ok := byName[name]; ok {
			ordered = append(ordered, group)
			seen[name] = true
		}
	}
	for _, group := range groups {
		name := tsString(group.(map[string]any), "name")
		if !seen[name] {
			ordered = append(ordered, group)
		}
	}
	if cfg["proxy-groups"] != nil {
		cfg["proxy-groups"] = ordered
	}
	return yaml.Marshal(cfg)
}

// OrderProxyGroups persists only display metadata. It deliberately avoids a
// mihomo reload or validation process: no native definition is changed here.
func (s *Store) OrderProxyGroups(ctx context.Context, profileName string, names []string) error {
	if err := ValidateProxyGroupOrder(names); err != nil {
		return err
	}
	effective, _, overlay, err := s.healthMaps(profileName)
	if err != nil {
		return err
	}
	groups, byName, err := namedList(effective["proxy-groups"])
	if err != nil {
		return err
	}
	if len(names) != len(groups) {
		return groupError("order_complete", "provide every configured group exactly once; run nagi proxy group list and retry")
	}
	for _, name := range names {
		if _, ok := byName[name]; !ok {
			return groupError("order_unknown", "group is not in the current configuration: %s; run nagi proxy group list and retry", name)
		}
	}
	values := make([]any, len(names))
	for i, name := range names {
		values[i] = name
	}
	overlay[groupOrderKey] = values
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	// Only metadata is written. Native settings and group definitions came
	// unchanged from the existing override, so no engine validation is needed.
	path, err := s.overridePath(profileName)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeReplace(path, data)
}
