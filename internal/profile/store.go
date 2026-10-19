package profile

import (
	"context"
	"errors"
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ronigooja/Nagi/internal/requestrewrite"
	"github.com/ronigooja/Nagi/internal/rules"
)

// Store manages profile files and the selected profile under a configuration directory.
type Store struct {
	Dir      string
	Validate func([]byte) error
	Reload   func(context.Context) error
	// EffectiveOverlay applies durable user preferences after profile layers.
	EffectiveOverlay func([]byte) ([]byte, error)
}

var ErrInvalidSelectedProfile = i18n.Errorf("backend.profile.invalid_selected_profile", "invalid selected profile")

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
		return "", i18n.Errorf("backend.profile.invalid_profile_name", "invalid profile name")
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
			return "", ErrInvalidSelectedProfile
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
		return nil, i18n.Errorf("backend.profile.profile_is_not_a_regular_file", "profile is not a regular file")
	}
	return os.ReadFile(path)
}

// Backup reads the most recent replacement copy of a profile.
func (s *Store) Backup(name string) ([]byte, error) {
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path + ".bak")
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, i18n.Errorf("backend.profile.backup_is_not_a_regular_file", "backup is not a regular file")
	}
	return os.ReadFile(path + ".bak")
}

// SaveBackup explicitly snapshots the current profile without changing it.
func (s *Store) SaveBackup(name string) error {
	data, err := s.Show(name)
	if err != nil {
		return err
	}
	path, _ := s.path(name)
	return writeReplace(path+".bak", data)
}

// RestoreBackup validates the backup and activates it through the normal write path.
func (s *Store) RestoreBackup(ctx context.Context, name string) error {
	data, err := s.Backup(name)
	if err != nil {
		return err
	}
	if err := s.Write(ctx, name, data); err != nil {
		path, _ := s.path(name)
		return errors.Join(err, writeReplace(path+".bak", data))
	}
	return nil
}

// Remove deletes an unselected profile and its backup, local override state, and custom rules.
// Callers use a configuration transaction to recover from cleanup failures.
func (s *Store) Remove(name string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return i18n.Errorf("backend.profile.profile_is_not_a_regular_file", "profile is not a regular file")
	}
	current, err := s.Current()
	if err != nil {
		return err
	}
	if current == name {
		return i18n.Errorf("backend.profile.cannot_remove_the_selected_profile_select_another_profile_first", "cannot remove the selected profile; select another profile first")
	}
	override, _ := s.overridePath(name)
	state, _ := s.defaultOverrideStatePath(name)
	policy, _ := s.subscriptionTailscalePath(name)
	paths := []string{rules.Path(s.Dir, name), policy, path + ".bak", override, override + ".bak", state, filepath.Join(s.Dir, "profiles", ".effective-"+name+".yaml"), path}
	// Check all targets before deleting anything; never remove directories or links.
	for _, target := range paths {
		info, err := os.Lstat(target)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return i18n.Errorf("backend.profile.profile_cleanup_target_is_not_a_regular_file_s", "profile cleanup target is not a regular file: %s", target)
		}
	}
	if err := requestrewrite.RemoveProfile(s.Dir, name); err != nil {
		return err
	}
	for _, target := range paths {
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) Use(ctx context.Context, name string) error {
	data, err := s.Show(name)
	if err != nil {
		return err
	}
	if err = s.validate(name, data); err != nil {
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
	if oldErr != nil && !errors.Is(oldErr, fs.ErrNotExist) {
		return oldErr
	}
	if err := s.validate(name, data); err != nil {
		return err
	}
	current, err := s.Current()
	if err != nil {
		if errors.Is(err, ErrInvalidSelectedProfile) {
			return i18n.Errorf("backend.profile.w_use_nagi_profile_use_name_or_nagi_subscription_apply_name_to_repair_the_selection", "%w; use `nagi profile use NAME` or `nagi subscription apply NAME` to repair the selection", err)
		}
		return err
	}
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	if s.Reload != nil && current == name {
		if err := s.Reload(ctx); err != nil {
			return errors.Join(err, restore(path, old, errors.Is(oldErr, fs.ErrNotExist)))
		}
	}
	return nil
}

// Apply writes and selects a profile, then reloads it once. Failed activation
// restores both the profile file and the previous selection.
func (s *Store) Apply(ctx context.Context, name string, data []byte) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	current, err := s.Current()
	if err != nil && !errors.Is(err, ErrInvalidSelectedProfile) {
		return err
	}
	oldProfile, profileErr := s.Show(name)
	if profileErr != nil && !errors.Is(profileErr, fs.ErrNotExist) {
		return profileErr
	}
	if err := s.validate(name, data); err != nil {
		return err
	}
	policy, err := s.SubscriptionTailscale(name)
	if err != nil {
		return err
	}
	if policy != nil {
		effective, err := s.EffectiveSource(name, data)
		if err != nil {
			return err
		}
		if err = s.validate(name, effective); err != nil {
			return err
		}
	}

	settings := filepath.Join(s.Dir, "settings.yaml")
	oldSettings, settingsErr := os.ReadFile(settings)
	if settingsErr != nil && !errors.Is(settingsErr, fs.ErrNotExist) {
		return settingsErr
	}
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	selectedChanged := current != name
	rollbackProfile := func(cause error) error {
		return errors.Join(cause, restore(path, oldProfile, errors.Is(profileErr, fs.ErrNotExist)))
	}
	if selectedChanged {
		if err := atomicWrite(settings, []byte("profile: "+name+"\n")); err != nil {
			return rollbackProfile(err)
		}
	}
	if s.Reload != nil {
		if err := s.Reload(ctx); err != nil {
			if selectedChanged {
				err = errors.Join(err, restore(settings, oldSettings, errors.Is(settingsErr, fs.ErrNotExist)))
			}
			return rollbackProfile(err)
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

// ValidationError carries the engine's redacted diagnostic. The store adds the
// candidate profile name before persistence or selection changes.
type ValidationError struct {
	Profile string
	Path    string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	prefix := "profile"
	if e.Profile != "" {
		prefix = fmt.Sprintf("profile %q", e.Profile)
	}
	message := fmt.Sprintf("%s validation failed at %s (%s): %s", prefix, e.Path, e.Code, e.Message)
	if strings.HasPrefix(e.Path, "rules[") && !strings.HasPrefix(e.Code, "geodata_") {
		if e.Message == "configuration validation failed" {
			message += "; mihomo did not provide a specific cause"
		}
		message += "; inspect rule syntax, proxy targets, and referenced rule providers or sub-rules in the candidate configuration; validate a local copy with `mihomo -t -f FILE -d DIRECTORY` for detailed diagnostics (may contain private configuration values)"
	}
	return message
}

func (s *Store) validate(name string, data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return i18n.Errorf("backend.profile.empty_profile", "empty profile")
	}
	if s.Validate == nil {
		return i18n.Errorf("backend.profile.profile_validator_is_required", "profile validator is required")
	}
	err := s.Validate(data)
	var diagnostic *ValidationError
	if errors.As(err, &diagnostic) {
		named := *diagnostic
		named.Profile = name
		return &named
	}
	return err
}

// atomicWrite keeps one backup of the previous regular file.
func atomicWrite(path string, data []byte) (err error) {
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return i18n.Errorf("backend.profile.target_is_not_a_regular_file", "target is not a regular file")
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
