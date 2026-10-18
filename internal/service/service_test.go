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
	for _, want := range []string{`Type=simple`, `ExecStart="/tmp/a $$PATH%% \"name\"\\nagi" __startup-watch`, `Restart=on-failure`} {
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
	if !strings.Contains(plist, "<string>__startup-watch</string>") || !strings.Contains(plist, "<key>KeepAlive</key><true/>") {
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

func TestUninstallLaunchdDefinitionAfterQuit(t *testing.T) {
	result := serviceResult(t, "launchd")
	if err := os.WriteFile(result.Path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := uninstall(result, 501, func(name string, args ...string) error {
		calls++
		if name != "launchctl" || len(args) != 2 || args[0] != "print" || args[1] != "gui/501/io.nagi.cli" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return errors.New(`exit status 113: Could not find service "io.nagi.cli" in domain for user gui: 501`)
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Fatalf("definition remains: %v", err)
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

func TestDisableOnlyChangesNextLogin(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd"} {
		t.Run(manager, func(t *testing.T) {
			result := serviceResult(t, manager)
			if err := os.WriteFile(result.Path, []byte("existing definition"), 0600); err != nil {
				t.Fatal(err)
			}
			result.Active = true
			var calls []string
			got, err := disable(result, 501, func(name string, args ...string) error {
				calls = append(calls, name+" "+strings.Join(args, " "))
				return nil
			})
			if !got.Active || got.Enabled || !got.Installed {
				t.Fatalf("incorrect resulting service state: %+v", got)
			}
			want := "systemctl --user disable nagi.service"
			if manager == "launchd" {
				want = "launchctl disable gui/501/io.nagi.cli"
			}
			if err != nil || len(calls) != 1 || calls[0] != want {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
			data, err := os.ReadFile(result.Path)
			if err != nil || string(data) != "existing definition" {
				t.Fatalf("definition=%q err=%v", data, err)
			}
		})
	}
}

func TestDisableAbsentAndFailure(t *testing.T) {
	result := serviceResult(t, "systemd")
	_, err := disable(result, 501, func(string, ...string) error { t.Fatal("absent service must be a no-op"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.Path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = disable(result, 501, func(string, ...string) error { return errors.New("manager unavailable") })
	if err == nil || !strings.Contains(err.Error(), "disable login startup") {
		t.Fatal(err)
	}
}

func TestEnableInstallsMissingService(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd"} {
		t.Run(manager, func(t *testing.T) {
			result := serviceResult(t, manager)
			var calls []string
			_, err := enable(result, "/tmp/nagi", 501, func(name string, args ...string) error {
				calls = append(calls, name+" "+strings.Join(args, " "))
				return nil
			})
			data, readErr := os.ReadFile(result.Path)
			if err != nil || readErr != nil || !strings.Contains(string(data), "__startup-watch") {
				t.Fatalf("err=%v read=%v definition=%q", err, readErr, data)
			}
			want := "systemctl --user enable --now nagi.service"
			if manager == "launchd" {
				want = "launchctl enable gui/501/io.nagi.cli"
			}
			if !strings.Contains(strings.Join(calls, "\n"), want) {
				t.Fatalf("missing enable: %v", calls)
			}
		})
	}
}

func TestEnableExistingLaunchdMonitorDoesNotReload(t *testing.T) {
	for _, active := range []bool{true, false} {
		result := serviceResult(t, "launchd")
		if err := os.WriteFile(result.Path, []byte("existing"), 0600); err != nil {
			t.Fatal(err)
		}
		var calls []string
		_, err := enable(result, "/tmp/nagi", 501, func(name string, args ...string) error {
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "print" && !active {
				return errors.New("not loaded")
			}
			return nil
		})
		wantCalls := 2
		if !active {
			wantCalls = 3
		}
		if err != nil || len(calls) != wantCalls || calls[0] != "enable gui/501/io.nagi.cli" || calls[1] != "print gui/501/io.nagi.cli" {
			t.Fatalf("active=%v calls=%v err=%v", active, calls, err)
		}
		if !active && calls[2] != "bootstrap gui/501 "+result.Path {
			t.Fatal(calls)
		}
		data, _ := os.ReadFile(result.Path)
		if string(data) != "existing" {
			t.Fatalf("definition rewritten: %q", data)
		}
	}
}

func TestLaunchdDisabledPreference(t *testing.T) {
	for _, tt := range []struct {
		output   string
		disabled bool
	}{
		{"disabled services = {\n\t\"io.nagi.cli\" => true\n}", true},
		{"disabled services = {\n\t\"io.nagi.cli\" => false\n}", false},
		{"disabled services = {\n\t\"io.other\" => true\n}", false},
		{"disabled services = {\n}", false},
	} {
		if got := launchdDisabled(tt.output); got != tt.disabled {
			t.Fatalf("%q: got %v", tt.output, got)
		}
	}
}
