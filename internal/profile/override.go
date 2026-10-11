package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ronigooja/Nagi/internal/rules"
	"gopkg.in/yaml.v3"
)

func (s *Store) overridePath(name string) (string, error) {
	if !validName(name) {
		return "", errors.New("invalid profile name")
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
		return nil, errors.New("override is not a regular file")
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
	patch, err := s.Override(name)
	if errors.Is(err, fs.ErrNotExist) {
		return rules.Apply(source, s.Dir)
	}
	if err != nil {
		return nil, err
	}
	var base, overlay map[string]any
	if err := yaml.Unmarshal(source, &base); err != nil || base == nil {
		return nil, errors.New("profile YAML cannot be merged with override")
	}
	if err := yaml.Unmarshal(patch, &overlay); err != nil || overlay == nil {
		return nil, errors.New("override is invalid YAML")
	}
	mergeMap(base, overlay)
	merged, err := yaml.Marshal(base)
	if err != nil {
		return nil, err
	}
	return rules.Apply(merged, s.Dir)
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
func (s *Store) ApplyDefaultOverride(ctx context.Context, name string) error {
	data, err := s.Show("default")
	if err != nil {
		return fmt.Errorf("default profile is unavailable: %w", err)
	}
	var source map[string]any
	if err := yaml.Unmarshal(data, &source); err != nil || source == nil {
		return errors.New("default profile must be a YAML mapping")
	}
	for key := range defaultOverrideExcludedKeys {
		delete(source, key)
	}
	filtered, err := yaml.Marshal(source)
	if err != nil {
		return fmt.Errorf("default profile settings could not be prepared: %w", err)
	}
	return s.SetOverride(ctx, name, filtered)
}

var defaultOverrideExcludedKeys = map[string]struct{}{
	"proxies": {}, "proxy-providers": {}, "proxy-groups": {}, "rules": {}, "rule-providers": {},
	"external-controller": {}, "external-controller-unix": {}, "external-controller-tls": {},
	"external-controller-pipe": {}, "external-controller-cors": {}, "external-controller-routing-mark": {},
	"external-ui": {}, "external-ui-url": {}, "secret": {}, "authentication": {},
}

// SetOverride stores a user-owned YAML mapping separately from the source profile.
func (s *Store) SetOverride(ctx context.Context, name string, data []byte) error {
	if len(data) == 0 || len(data) > 8<<20 {
		return errors.New("override must contain 1 byte to 8 MiB")
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
		return errors.New("override must be a YAML mapping")
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
	if err := s.validate(effective); err != nil {
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
	return nil
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
	if err := s.validate(base); err != nil {
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
	return nil
}
