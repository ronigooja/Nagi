package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInstallationHelpAndUsageWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{{"app"}, {"app", "install"}, {"app", "update"}, {"app", "uninstall"}, {"update"}, {"uninstall"}} {
		var out, errs bytes.Buffer
		if code := Run(append(args, "--help"), &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "Usage: nagi "+strings.Join(args, " ")) {
			t.Fatalf("%v: %d %s %s", args, code, &out, &errs)
		}
	}
	for _, args := range [][]string{{"app"}, {"app", "bad"}, {"app", "uninstall", "v1.0.0"}, {"update", "main"}, {"app", "install", "--prefix"}, {"uninstall", "v1.0.0"}} {
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 2 {
			t.Fatalf("%v: %d %s", args, code, &errs)
		}
		var response struct{ Error struct{ Code string } }
		if err := json.Unmarshal(errs.Bytes(), &response); err != nil || response.Error.Code != "usage" {
			t.Fatalf("%s %v", &errs, err)
		}
	}
}

func TestInstallationFailureUsesStandardEnvelope(t *testing.T) {
	var out, errs bytes.Buffer
	if code := Run([]string{"--json", "app", "status"}, &out, &errs, "test", "commit"); code != 1 {
		t.Fatalf("code=%d %s", code, &errs)
	}
	var response struct {
		OK    bool
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(errs.Bytes(), &response); err != nil || response.OK || response.Error.Code != "installation_error" || response.Error.Message == "" || out.Len() != 0 {
		t.Fatalf("%s %v", &errs, err)
	}
}
