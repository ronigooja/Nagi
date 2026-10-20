package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var chainResourceName = regexp.MustCompile(`\Anagi-chain-[a-f0-9]{24}-source\z`)

// IsProviderChainCache verifies the generated identity and exact profile-local
// cache location. It never authorizes an arbitrary caller-provided write path.
func IsProviderChainCache(provider, path, profilesDir, profile string) bool {
	return validName(profile) && chainResourceName.MatchString(provider) && path == filepath.Join(profilesDir, ".chain-cache", profile, provider+".yaml")
}

// PrepareProviderChainCaches is called only during activation. Reads and plans
// never call it. Return cleanup removes only files created by this preparation.
func PrepareProviderChainCaches(data []byte, configDir, profile string) (func(), error) {
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	created := []string{}
	cleanup := func() {
		for _, path := range created {
			_ = os.Remove(path)
		}
	}
	for name, v := range providers {
		p, _ := v.(map[string]any)
		if !chainResourceName.MatchString(name) || tsString(p, "type") != "http" {
			continue
		}
		path := tsString(p, "path")
		if !IsProviderChainCache(name, path, filepath.Join(configDir, "profiles"), profile) {
			cleanup()
			return nil, fmt.Errorf("invalid generated provider cache path")
		}
		made, err := PreparePrivateChainCache(filepath.Join(configDir, "profiles"), path)
		if err != nil {
			cleanup()
			return nil, err
		}
		if made {
			created = append(created, path)
		}
	}
	return cleanup, nil
}

// PreparePrivateChainCache refuses symlinks on every generated path component.
func PreparePrivateChainCache(base, path string) (bool, error) {
	relative, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return false, fmt.Errorf("invalid provider cache boundary")
	}
	cursor := base
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		cursor = filepath.Join(cursor, part)
		if err := os.Mkdir(cursor, 0700); err != nil && !os.IsExist(err) {
			return false, err
		}
		info, err := os.Lstat(cursor)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("unsafe provider cache directory")
		}
		if err = os.Chmod(cursor, 0700); err != nil {
			return false, err
		}
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("unsafe provider cache file")
		}
		return false, os.Chmod(path, 0600)
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	return true, file.Close()
}

// CleanupProviderChainCaches removes only generated files no longer present in
// this profile's active metadata, after a successful mutation/activation.
func (s *Store) CleanupProviderChainCaches(name string) error {
	members, err := s.savedProviderMembers(name)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, m := range members {
		keep[m.providerID()+".yaml"] = true
	}
	dir := filepath.Join(s.Dir, "profiles", ".chain-cache", name)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if keep[entry.Name()] || !chainResourceName.MatchString(strings.TrimSuffix(entry.Name(), ".yaml")) {
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsafe provider cache entry")
		}
		if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
