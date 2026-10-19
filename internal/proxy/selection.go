package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SelectionStore records successful user choices per profile. The engine remains
// authoritative: a stored choice is replayed only when its group and node exist.
type SelectionStore struct{ Path string }

// ApplyDefaults places saved selector choices in the generated runtime profile.
// mihomo reads these before its DNS and TUN listeners can accept traffic.
// A provider may supply a saved node later, so provider groups retain their
// default even when the node is not among the statically listed members.
func (s SelectionStore) ApplyDefaults(profile string, data []byte) ([]byte, bool, error) {
	all, err := s.read()
	if err != nil {
		return nil, false, err
	}
	choices := all[profile]
	if len(choices) == 0 {
		return data, false, nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, false, i18n.Errorf("backend.proxy.effective_profile_is_invalid_yaml", "effective profile is invalid YAML")
	}
	groups, _ := doc["proxy-groups"].([]any)
	changed := false
	for _, entry := range groups {
		group, ok := entry.(map[string]any)
		if !ok || group["type"] != "select" {
			continue
		}
		name, _ := group["name"].(string)
		choice := choices[name]
		if choice == "" {
			continue
		}
		valid := false
		for _, member := range stringList(group["proxies"]) {
			if member == choice {
				valid = true
				break
			}
		}
		if !valid && (len(stringList(group["use"])) > 0 || group["include-all"] == true || group["include-all-providers"] == true || group["include-all-proxies"] == true) {
			valid = true
		}
		if !valid {
			continue
		}
		group["default-selected"] = choice
		changed = true
	}
	if !changed {
		return data, false, nil
	}
	settings, _ := doc["profile"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		doc["profile"] = settings
	}
	// Nagi's per-profile selections must win over mihomo's process-wide cache.
	settings["store-selected"] = false
	encoded, err := yaml.Marshal(doc)
	return encoded, true, err
}

func stringList(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			result = append(result, value)
		}
	}
	return result
}

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
		return nil, i18n.Errorf("backend.proxy.invalid_saved_proxy_selections", "invalid saved proxy selections")
	}
	if all == nil {
		all = map[string]map[string]string{}
	}
	return all, nil
}
func (s SelectionStore) Save(profile, group, node string) error {
	if profile == "" || group == "" || node == "" {
		return i18n.Errorf("backend.proxy.profile_group_and_node_required", "profile, group, and node required")
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
	return writeSelections(s.Path, data)
}

// Prune removes choices whose group or selected node disappeared after an update.
func (s SelectionStore) Prune(profile string, groups map[string]map[string]bool) error {
	all, err := s.read()
	if err != nil {
		return err
	}
	changed := false
	for group, node := range all[profile] {
		if !groups[group][node] && !groups[group]["*"] {
			delete(all[profile], group)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if len(all[profile]) == 0 {
		delete(all, profile)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeSelections(s.Path, data)
}

// RemoveGroup clears one saved pin after mihomo has resumed automatic selection.
func (s SelectionStore) RemoveGroup(profile, group string) error {
	all, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := all[profile][group]; !ok {
		return nil
	}
	delete(all[profile], group)
	if len(all[profile]) == 0 {
		delete(all, profile)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeSelections(s.Path, data)
}

func writeSelections(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".nagi-selections-*")
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
	return os.Rename(file.Name(), path)
}

// Clear removes choices for one profile after a replacement subscription applies.
func (s SelectionStore) Clear(profile string) error {
	all, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := all[profile]; !ok {
		return nil
	}
	delete(all, profile)
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeSelections(s.Path, data)
}

type RestoreResult struct {
	Restored    []string `json:"restored"`
	Unavailable []string `json:"unavailable"`
	Failed      []string `json:"failed"`
}

func (s SelectionStore) Restore(ctx context.Context, profile string, service *Service) (RestoreResult, error) {
	result := RestoreResult{Restored: []string{}, Unavailable: []string{}, Failed: []string{}}
	all, err := s.read()
	if err != nil {
		return result, err
	}
	choices := all[profile]
	if len(choices) == 0 {
		return result, nil
	}
	groups, err := service.GroupsState(ctx)
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
		if _, err := service.SelectKnown(ctx, g, node); err != nil {
			result.Failed = append(result.Failed, group+" -> "+node)
			continue
		}
		result.Restored = append(result.Restored, group+" -> "+node)
	}
	if len(result.Failed) > 0 {
		return result, i18n.Errorf("backend.proxy.saved_proxy_selections_could_not_be_applied_s_check_nagi_proxy_groups_and_retry_", "saved proxy selections could not be applied (%s); check `nagi proxy groups` and retry `nagi proxy restore`", strings.Join(result.Failed, ", "))
	}
	return result, nil
}
