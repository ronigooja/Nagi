package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ronigooja/Nagi/internal/control"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

func tunIntentPath(paths nagiruntime.Paths) string {
	return filepath.Join(paths.StateDir, "tun-intent")
}

// A missing or legacy empty preference leaves profile TUN settings unchanged.
func savedTUNPreference(paths nagiruntime.Paths) (*bool, error) {
	data, err := os.ReadFile(tunIntentPath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var enabled bool
	switch strings.TrimSpace(string(data)) {
	case "enabled":
		enabled = true
	case "disabled":
		enabled = false
	case "":
		return nil, nil
	default:
		return nil, errors.New("invalid saved TUN intent")
	}
	return &enabled, nil
}

func readTUNIntent(paths nagiruntime.Paths) (bool, error) {
	preference, err := savedTUNPreference(paths)
	return preference != nil && *preference, err
}

// Apply the durable user choice before mihomo validates or activates the full
// configuration. Source profiles and subscription caches remain untouched.
func applySavedTUN(paths nagiruntime.Paths, data []byte) ([]byte, error) {
	preference, err := savedTUNPreference(paths)
	if err != nil || preference == nil {
		return data, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, errors.New("profile YAML cannot be merged with saved TUN preference")
	}
	tun, ok := doc["tun"].(map[string]any)
	if !ok {
		if raw, exists := doc["tun"]; exists && raw != nil {
			return nil, errors.New("saved TUN preference requires a TUN mapping")
		}
		tun = map[string]any{}
		doc["tun"] = tun
	}
	tun["enable"] = *preference
	if *preference {
		tun["auto-route"] = true
		tun["auto-detect-interface"] = true
		tun["strict-route"] = true
		tun["dns-hijack"] = tunDNSHijack(tun["dns-hijack"])
	}
	return yaml.Marshal(doc)
}

func writeTUNIntent(paths nagiruntime.Paths, enabled bool) error {
	if err := os.MkdirAll(paths.StateDir, 0700); err != nil {
		return err
	}
	value := "disabled\n"
	if enabled {
		value = "enabled\n"
	}
	tmp, err := os.CreateTemp(paths.StateDir, ".tun-intent-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, tunIntentPath(paths))
}

// Mihomo reports enable=false when adapter creation fails, even though the
// full configuration's activation marker has advanced.
func confirmSavedTUN(ctx context.Context, paths nagiruntime.Paths, client *control.Client) error {
	preference, err := savedTUNPreference(paths)
	if err != nil || preference == nil {
		return err
	}
	cfg, err := trafficConfig(ctx, client)
	if err != nil {
		return err
	}
	tun, _ := cfg["tun"].(map[string]any)
	if (tun["enable"] == true) != *preference {
		return fmt.Errorf("mihomo did not activate saved TUN preference (enabled=%t); inspect `nagi tun status`, `nagi logs`, and OS TUN permissions", *preference)
	}
	return nil
}
