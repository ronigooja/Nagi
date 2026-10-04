package profile

import (
	"context"
	"errors"
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

func (s *Store) Effective(name string) ([]byte, error) {
	base, err := s.Show(name)
	if err != nil {
		return nil, err
	}
	patch, err := s.Override(name)
	if errors.Is(err, fs.ErrNotExist) {
		return rules.Apply(base, s.Dir)
	}
	if err != nil {
		return nil, err
	}
	var source, overlay map[string]any
	if err := yaml.Unmarshal(base, &source); err != nil {
		return nil, errors.New("profile YAML cannot be merged with override")
	}
	if err := yaml.Unmarshal(patch, &overlay); err != nil {
		return nil, errors.New("override is invalid YAML")
	}
	if source == nil || overlay == nil {
		return nil, errors.New("profile and override must be YAML mappings")
	}
	mergeMap(source, overlay)
	merged, err := yaml.Marshal(source)
	if err != nil {
		return nil, err
	}
	return rules.Apply(merged, s.Dir)
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
