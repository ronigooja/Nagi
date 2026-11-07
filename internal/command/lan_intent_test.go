package command

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSavedLANPreferenceOverlaysProfiles(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	profile := []byte("mixed-port: 17890\nallow-lan: false\nbind-address: 127.0.0.1\n")
	unchanged, err := applySavedLAN(paths, profile)
	if err != nil || string(unchanged) != string(profile) {
		t.Fatalf("absent preference: %s %v", unchanged, err)
	}
	for _, preference := range []lanPreference{{true, "192.0.2.10"}, {true, "*"}, {false, "127.0.0.1"}} {
		if err := writeLANPreference(paths, preference); err != nil {
			t.Fatal(err)
		}
		for _, source := range [][]byte{profile, []byte("mixed-port: 17900\nallow-lan: true\nbind-address: '*'\n")} {
			data, err := applySavedLAN(paths, source)
			var decoded map[string]any
			if err != nil || yaml.Unmarshal(data, &decoded) != nil || decoded["allow-lan"] != preference.Enabled || decoded["bind-address"] != preference.Address {
				t.Fatalf("overlay %+v: %s %v", preference, data, err)
			}
		}
	}
	info, err := os.Stat(filepath.Join(paths.StateDir, "lan-intent"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("LAN preference permissions: %v %v", info, err)
	}
}

func TestSavedLANPreferenceRejectsInvalidData(t *testing.T) {
	paths := isolatedTestPaths(t, t.TempDir())
	if err := os.MkdirAll(paths.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `{"address":"127.0.0.1"}`, `{"enabled":null,"address":"127.0.0.1"}`, `{"enabled":true,"address":"bad"}`, `{"enabled":false,"address":"*"}`, `{"enabled":true,"address":"*","extra":1}`, `null`} {
		if err := os.WriteFile(filepath.Join(paths.StateDir, "lan-intent"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := savedLANPreference(paths); err == nil {
			t.Fatalf("accepted invalid preference %s", raw)
		}
	}
}
