package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/ronigooja/Nagi/internal/installation"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallationHelpAndUsageWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{{"app"}, {"app", "install"}, {"app", "update"}, {"app", "uninstall"}, {"app", "update-error"}, {"update"}, {"uninstall"}} {
		var out, errs bytes.Buffer
		if code := Run(append(args, "--help"), &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "Usage: nagi "+strings.Join(args, " ")) {
			t.Fatalf("%v: %d %s %s", args, code, &out, &errs)
		}
	}
	for _, args := range [][]string{{"app"}, {"app", "bad"}, {"app", "update-error", "extra"}, {"app", "uninstall", "v1.0.0"}, {"update", "main"}, {"app", "install", "--prefix"}, {"uninstall", "v1.0.0"}} {
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

func TestAppUpdateErrorJSONAndEmptyResult(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS App command")
	}
	t.Setenv("HOME", t.TempDir())
	invalidTestConfigDir(t, t.TempDir())
	if err := installation.SaveUpdateError(errors.New("update failed: https://user:secret@example.com/private")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"update failed: https://example.com/private", ""} {
		var out, errs bytes.Buffer
		if code := Run([]string{"--json", "app", "update-error"}, &out, &errs, "test", "commit"); code != 0 {
			t.Fatalf("%d %s", code, &errs)
		}
		var envelope struct {
			OK   bool
			Data struct{ Error string }
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !envelope.OK || envelope.Data.Error != want || errs.Len() != 0 {
			t.Fatalf("%s %s %v", &out, &errs, err)
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		var out, errs bytes.Buffer
		if code := Run([]string{"completion", shell}, &out, &errs, "test", "commit"); code != 0 || !strings.Contains(out.String(), "update-error") {
			t.Fatalf("%s %d %s", shell, code, &errs)
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "Nagi", "update-error")); !os.IsNotExist(err) {
		t.Fatalf("error not consumed: %v", err)
	}
}
