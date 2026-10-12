package profile

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

func ValidateProviderRemoval(name string) error {
	if name == "COMPATIBLE" || name == "" || len(name) > 128 || strings.TrimSpace(name) != name {
		return errors.New("NAME must contain 1..128 bytes without surrounding whitespace and must not be COMPATIBLE")
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return errors.New("NAME must contain printable characters")
		}
	}
	return nil
}

// RemoveProvider removes a provider and its use references from both layers.
// Callers serialize changes and recover runtime state through the activation transaction.
func (s *Store) RemoveProvider(ctx context.Context, profileName, name string) error {
	if err := ValidateProviderRemoval(name); err != nil {
		return err
	}
	data, err := s.Effective(profileName)
	if err != nil {
		return err
	}
	var effective map[string]any
	if yaml.Unmarshal(data, &effective) != nil || effective == nil {
		return errors.New("selected profile must be a YAML mapping")
	}
	providers, ok := effective["proxy-providers"].(map[string]any)
	if !ok {
		return errors.New("selected profile has no proxy providers")
	}
	if _, exists := providers[name]; !exists {
		return errors.New("provider not found in the selected profile")
	}
	source, err := s.Show(profileName)
	if err != nil {
		return err
	}
	var sourceDoc map[string]any
	if yaml.Unmarshal(source, &sourceDoc) != nil || sourceDoc == nil {
		return errors.New("selected profile must be a YAML mapping")
	}
	overlay := map[string]any{}
	old, err := s.Override(profileName)
	if err == nil {
		if yaml.Unmarshal(old, &overlay) != nil || overlay == nil {
			return errors.New("local override must be a YAML mapping")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	sourceChanged, err := removeProviderReferences(sourceDoc, name)
	if err != nil {
		return err
	}
	if _, err := removeProviderReferences(overlay, name); err != nil {
		return err
	}
	updated, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	path, err := s.path(profileName)
	if err != nil {
		return err
	}
	if sourceChanged {
		candidate, err := yaml.Marshal(sourceDoc)
		if err != nil {
			return err
		}
		if err := writeReplace(path, candidate); err != nil {
			return err
		}
	}
	// Validate and reload after both layers are updated. SetOverride restores
	// its file on failure; the outer transaction recovers the running engine.
	if err := s.SetOverride(ctx, profileName, updated); err != nil {
		if sourceChanged {
			return errors.Join(err, restore(path, source, false))
		}
		return err
	}
	return nil
}

func removeProviderReferences(doc map[string]any, name string) (bool, error) {
	changed := false
	if value, exists := doc["proxy-providers"]; exists && value != nil {
		providers, ok := value.(map[string]any)
		if !ok {
			return false, errors.New("proxy-providers must be a mapping")
		}
		if _, exists := providers[name]; exists {
			delete(providers, name)
			changed = true
		}
	}
	if value, exists := doc["proxy-groups"]; exists && value != nil {
		groups, ok := value.([]any)
		if !ok {
			return false, errors.New("proxy-groups must be a list")
		}
		for _, value := range groups {
			group, ok := value.(map[string]any)
			if !ok {
				return false, errors.New("proxy-groups must contain mappings")
			}
			if value, exists := group["use"]; exists && value != nil {
				use, ok := value.([]any)
				if !ok {
					return false, errors.New("GROUP use must be a list")
				}
				remaining := make([]any, 0, len(use))
				for _, value := range use {
					candidate, ok := value.(string)
					if !ok {
						return false, errors.New("GROUP use must contain provider names")
					}
					if candidate != name {
						remaining = append(remaining, value)
					}
				}
				if len(remaining) != len(use) {
					group["use"] = remaining
					changed = true
				}
			}
		}
	}
	return changed, nil
}
