package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can install the helper")
	}
	got, err := Install(ExecutablePath)
	if err == nil || !strings.Contains(err.Error(), "administrator privileges") {
		t.Fatalf("Install = %+v, %v; wanted a root requirement", got, err)
	}
}

func TestDefinitionHasFixedRootCommand(t *testing.T) {
	content := definition(501)
	for _, item := range []string{Label, "<string>" + ExecutablePath + "</string>", "<string>__privileged-helper</string>", "<string>501</string>", "<key>RunAtLoad</key><true/>", "<key>Umask</key><integer>63</integer>"} {
		if !strings.Contains(content, item) {
			t.Fatalf("plist missing %q", item)
		}
	}
	if strings.Contains(content, "__startup-watch") {
		t.Fatal("privileged daemon must not run the user login monitor")
	}
}

func TestInstallRejectsUntrustedCallerAndPath(t *testing.T) {
	r := Result{Path: filepath.Join(t.TempDir(), "helper.plist")}
	for _, tc := range []struct {
		name, executable string
		uid              int
		want             string
	}{
		{"user", ExecutablePath, 501, "administrator privileges"},
		{"different executable", "/tmp/nagi", 0, "installed " + ExecutablePath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			_, err := install(r, tc.executable, tc.uid, func(string, ...string) error { called = true; return nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) || called {
				t.Fatalf("install error = %v; launchctl called = %t", err, called)
			}
		})
	}
}

func TestUninstallRetainsDefinitionWhenBootoutFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper.plist")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	r := Result{Path: path}
	_, err := uninstall(r, 0, func(_ string, args ...string) error {
		if args[0] == "bootout" {
			return errors.New("denied")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "definition retained") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("definition lost: %v", err)
	}
}

func TestSecureExecutableRejectsWritableAndSymlink(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "nagi")
	if err := os.WriteFile(path, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	// A temporary directory is normally writable to its owner; the check also
	// refuses symlinked or non-root-owned ancestors.
	if err := secureExecutable(path); err == nil {
		t.Fatal("temporary executable accepted")
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := secureExecutable(link); err == nil {
		t.Fatal("symlink executable accepted")
	}
}
