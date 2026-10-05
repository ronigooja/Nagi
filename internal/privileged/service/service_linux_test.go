//go:build linux

package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxInstallFailClosed(t *testing.T) {
	result, err := Install("/usr/local/bin/nagi")
	if err == nil || !strings.Contains(err.Error(), "unavailable") || result.Path != linuxUnitPath {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestLinuxDefinition(t *testing.T) {
	unit, err := linuxDefinition(`/usr/local/a $b% "nagi"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ExecStart="/usr/local/a $$b%% \"nagi\"" __privileged-helper`, "WantedBy=multi-user.target", "Restart=on-failure"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("missing %q in %q", want, unit)
		}
	}
	for _, path := range []string{"relative/nagi", "/usr/local/nagi\nchanged"} {
		if _, err := linuxDefinition(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}

func TestRootControlledExecutable(t *testing.T) {
	if err := rootControlledExecutable("relative/nagi"); err == nil {
		t.Fatal("accepted relative path")
	}
	base := t.TempDir()
	path := filepath.Join(base, "nagi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := rootControlledExecutable(path); err == nil {
		t.Fatal("accepted binary below writable /tmp")
	}
}

func TestLinuxInstallRequiresRootBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nagi.service")
	called := false
	_, err := linuxInstall(path, "/usr/local/bin/nagi", 501, func(string, ...string) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "requires root") || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected unit: %v", err)
	}
}

func TestLinuxUninstallRetainsUnitOnManagerError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nagi.service")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := linuxUninstall(path, 0, func(string, ...string) error { return errors.New("systemd unavailable") })
	if err == nil || !strings.Contains(err.Error(), "unit retained") {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "old" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestLinuxUninstallCallsSystemManager(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nagi.service")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	_, err := linuxUninstall(path, 0, func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "systemctl disable --now nagi-privileged-helper.service" || calls[1] != "systemctl daemon-reload" {
		t.Fatalf("calls=%v", calls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unit remains: %v", err)
	}
}
