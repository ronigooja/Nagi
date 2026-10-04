package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serviceResult(t *testing.T, manager string) Result {
	t.Helper()
	return Result{Path: filepath.Join(t.TempDir(), "service"), Manager: manager}
}

func TestDefinitionEscapesPaths(t *testing.T) {
	unit, err := definition("systemd", `/tmp/a $PATH% "name"\nagi`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ExecStart="/tmp/a $$PATH%% \"name\"\\nagi" start`, `ExecStop="/tmp/a $$PATH%% \"name\"\\nagi" stop`} {
		if !strings.Contains(unit, want) {
			t.Errorf("missing %q in %q", want, unit)
		}
	}
	plist, err := definition("launchd", `/tmp/a & <b> "name"`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plist, `/tmp/a &amp; &lt;b&gt; &quot;name&quot;`) {
		t.Fatal(plist)
	}
}

func TestInstallRestoresDefinitionAfterManagerFailure(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd"} {
		t.Run(manager, func(t *testing.T) {
			result := serviceResult(t, manager)
			if err := os.WriteFile(result.Path, []byte("old definition"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			_, err := install(result, "/tmp/nagi", 501, func(name string, args ...string) error {
				calls++
				if manager == "systemd" && calls == 1 {
					return nil
				}
				return errors.New("manager unavailable")
			})
			if err == nil || !strings.Contains(err.Error(), "previous service definition restored") || !strings.Contains(err.Error(), "check "+manager+" state") {
				t.Fatal(err)
			}
			content, readErr := os.ReadFile(result.Path)
			if readErr != nil || string(content) != "old definition" {
				t.Fatalf("restored %q: %v", content, readErr)
			}
		})
	}
}

func TestInstallRemovesFreshDefinitionAfterFailure(t *testing.T) {
	result := serviceResult(t, "systemd")
	_, err := install(result, "/tmp/nagi", 501, func(string, ...string) error { return errors.New("no bus") })
	if err == nil || !strings.Contains(err.Error(), "new service definition removed") {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Fatalf("definition remains: %v", err)
	}
}

func TestUninstallRetainsDefinitionOnManagerFailure(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd"} {
		t.Run(manager, func(t *testing.T) {
			result := serviceResult(t, manager)
			if err := os.WriteFile(result.Path, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := uninstall(result, 501, func(string, ...string) error { return errors.New("manager failed") })
			if err == nil || !strings.Contains(err.Error(), "definition retained") {
				t.Fatal(err)
			}
			if _, err := os.Stat(result.Path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUninstallAbsentIsIdempotent(t *testing.T) {
	result := serviceResult(t, "systemd")
	called := false
	_, err := uninstall(result, 501, func(string, ...string) error { called = true; return nil })
	if err != nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestUninstallReloadFailureReportsRemovedDefinition(t *testing.T) {
	result := serviceResult(t, "systemd")
	if err := os.WriteFile(result.Path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := uninstall(result, 501, func(string, ...string) error {
		calls++
		if calls == 2 {
			return errors.New("reload failed")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "definition removed") {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Fatalf("definition remains: %v", err)
	}
}
