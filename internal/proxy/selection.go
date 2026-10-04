package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// SelectionStore records successful user choices per profile. The engine remains
// authoritative: a stored choice is replayed only when its group and node exist.
type SelectionStore struct{ Path string }

func (s SelectionStore) read() (map[string]map[string]string, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var all map[string]map[string]string
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, errors.New("invalid saved proxy selections")
	}
	if all == nil {
		all = map[string]map[string]string{}
	}
	return all, nil
}
func (s SelectionStore) Save(profile, group, node string) error {
	if profile == "" || group == "" || node == "" {
		return errors.New("profile, group, and node required")
	}
	all, err := s.read()
	if err != nil {
		return err
	}
	if all[profile] == nil {
		all[profile] = map[string]string{}
	}
	all[profile][group] = node
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.Path), ".nagi-selections-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.Path)
}

type RestoreResult struct {
	Restored    []string `json:"restored"`
	Unavailable []string `json:"unavailable"`
}

func (s SelectionStore) Restore(ctx context.Context, profile string, service *Service) (RestoreResult, error) {
	result := RestoreResult{Restored: []string{}, Unavailable: []string{}}
	all, err := s.read()
	if err != nil {
		return result, err
	}
	choices := all[profile]
	if len(choices) == 0 {
		return result, nil
	}
	groups, err := service.Groups(ctx)
	if err != nil {
		return result, err
	}
	available := map[string]Group{}
	for _, g := range groups {
		available[g.Name] = g
	}
	keys := make([]string, 0, len(choices))
	for k := range choices {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, group := range keys {
		node := choices[group]
		g, ok := available[group]
		found := false
		for _, candidate := range g.All {
			if candidate == node {
				found = true
				break
			}
		}
		if !ok || !found {
			result.Unavailable = append(result.Unavailable, group+" -> "+node)
			continue
		}
		if err := service.Select(ctx, group, node); err != nil {
			result.Unavailable = append(result.Unavailable, group+" -> "+node)
			continue
		}
		result.Restored = append(result.Restored, group+" -> "+node)
	}
	return result, nil
}
