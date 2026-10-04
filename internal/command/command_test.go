package command

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestJSONContractAndExitCodes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "status"}, &stdout, &stderr, "test", "commit"); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
	}
	var success struct {
		OK   bool `json:"ok"`
		Data struct {
			Running bool `json:"running"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &success); err != nil || !success.OK || success.Data.Running {
		t.Fatalf("status response=%s err=%v", stdout.String(), err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "unknown"}, &stdout, &stderr, "test", "commit"); code != 2 {
		t.Fatalf("usage exit=%d", code)
	}
	var failure struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.OK || failure.Error.Code != "usage" || failure.Error.Message == "" {
		t.Fatalf("error response=%s err=%v", stderr.String(), err)
	}
}
