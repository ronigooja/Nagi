package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store manages profile files and the selected profile under a configuration directory.
type Store struct {
	Dir      string
	Validate func([]byte) error
	Reload   func(context.Context) error
}

func NewStore(configDir string, validate func([]byte) error, reload func(context.Context) error) *Store {
	return &Store{Dir: configDir, Validate: validate, Reload: reload}
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (s *Store) path(name string) (string, error) {
	if !validName(name) {
		return "", errors.New("invalid profile name")
	}
	return filepath.Join(s.Dir, "profiles", name+".yaml"), nil
}

func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "profiles"))
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if !entry.IsDir() && name != entry.Name() && validName(name) && entry.Type().IsRegular() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *Store) Current() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "settings.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return "default", nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "profile" {
			name := strings.TrimSpace(value)
			if validName(name) {
				return name, nil
			}
			return "", errors.New("invalid selected profile")
		}
	}
	return "default", nil
}

func (s *Store) Show(name string) ([]byte, error) {
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("profile is not a regular file")
	}
	return os.ReadFile(path)
}

func (s *Store) Use(ctx context.Context, name string) error {
	data, err := s.Show(name)
	if err != nil {
		return err
	}
	if err = s.validate(data); err != nil {
		return err
	}
	if _, err = s.Current(); err != nil {
		return err
	}
	settings := filepath.Join(s.Dir, "settings.yaml")
	old, oldErr := os.ReadFile(settings)
	if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
		return oldErr
	}
	if err = atomicWrite(settings, []byte("profile: "+name+"\n")); err != nil {
		return err
	}
	if s.Reload != nil {
		if err := s.Reload(ctx); err != nil {
			return errors.Join(err, restore(settings, old, errors.Is(oldErr, fs.ErrNotExist)))
		}
	}
	return nil
}

func (s *Store) Write(ctx context.Context, name string, data []byte) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	old, oldErr := s.Show(name)
	if oldErr == nil {
		if err := s.validate(old); err != nil {
			return fmt.Errorf("existing profile invalid: %w", err)
		}
	} else if !errors.Is(oldErr, fs.ErrNotExist) {
		return oldErr
	}
	if err := s.validate(data); err != nil {
		return err
	}
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	if s.Reload != nil {
		if err := s.Reload(ctx); err != nil {
			return errors.Join(err, restore(path, old, errors.Is(oldErr, fs.ErrNotExist)))
		}
	}
	return nil
}

func restore(path string, old []byte, absent bool) error {
	if absent {
		return os.Remove(path)
	}
	return writeReplace(path, old)
}

func (s *Store) validate(data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return errors.New("empty profile")
	}
	if s.Validate == nil {
		return errors.New("profile validator is required")
	}
	return s.Validate(data)
}

// atomicWrite keeps one backup of the previous regular file.
func atomicWrite(path string, data []byte) (err error) {
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return errors.New("target is not a regular file")
		}
		old, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if err = writeReplace(path+".bak", old); err != nil {
			return err
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	return writeReplace(path, data)
}

func writeReplace(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".nagi-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
