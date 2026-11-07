package command

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"

	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"gopkg.in/yaml.v3"
)

type lanPreference struct {
	Enabled bool   `json:"enabled"`
	Address string `json:"address"`
}

func savedLANPreference(paths nagiruntime.Paths) (*lanPreference, error) {
	data, err := os.ReadFile(filepath.Join(paths.StateDir, "lan-intent"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var preference lanPreference
	if err := json.Unmarshal(data, &preference); err != nil || (preference.Address != "*" && net.ParseIP(preference.Address) == nil) || (!preference.Enabled && preference.Address != "127.0.0.1") {
		return nil, messageError("cli.error.lan.invalid_preference", "invalid saved LAN preference")
	}
	return &preference, nil
}

func applySavedLAN(paths nagiruntime.Paths, data []byte) ([]byte, error) {
	preference, err := savedLANPreference(paths)
	if err != nil || preference == nil {
		return data, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, messageError("cli.error.lan.invalid_profile", "profile YAML cannot be merged with saved LAN preference")
	}
	doc["allow-lan"] = preference.Enabled
	doc["bind-address"] = preference.Address
	return yaml.Marshal(doc)
}

func writeLANPreference(paths nagiruntime.Paths, preference lanPreference) error {
	if err := os.MkdirAll(paths.StateDir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(paths.StateDir, ".lan-intent-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	data, err := json.Marshal(preference)
	if err == nil {
		_, err = file.Write(data)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(paths.StateDir, "lan-intent"))
}
