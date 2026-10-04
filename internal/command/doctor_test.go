package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorJSONAndTextWithCorruptSettings(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config", "nagi")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "settings.yaml"), []byte("profile: bad/name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(config))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	t.Setenv("NAGI_MIHOMO_BIN", filepath.Join(root, "missing"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "doctor"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Healthy bool                                     `json:"healthy"`
			Checks  []struct{ Name, Status, Message string } `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Healthy || len(response.Data.Checks) != 8 {
		t.Fatal(stdout.String())
	}
	found := false
	for _, check := range response.Data.Checks {
		if check.Name == "selected_profile" && check.Status == "error" {
			found = true
		}
		if strings.Contains(check.Message, "bad/name") {
			t.Fatal("configuration value leaked")
		}
	}
	if !found {
		t.Fatal(stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Nagi doctor: needs attention") || !strings.Contains(stdout.String(), "[ERROR] selected_profile") {
		t.Fatal(stdout.String())
	}
}
