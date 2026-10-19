package service

import (
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Result struct {
	Path      string `json:"path"`
	Manager   string `json:"manager"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
}

type runner func(string, ...string) error

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return i18n.Errorf("backend.service.w_s", "%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}

func location() (Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	switch runtime.GOOS {
	case "darwin":
		return Result{Path: filepath.Join(home, "Library", "LaunchAgents", "io.nagi.cli.plist"), Manager: "launchd"}, nil
	case "linux":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return Result{Path: filepath.Join(base, "systemd", "user", "nagi.service"), Manager: "systemd"}, nil
	default:
		return Result{}, i18n.Errorf("backend.service.unsupported_service_platform_s", "unsupported service platform: %s", runtime.GOOS)
	}
}

func Install(executable string) (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	return install(result, executable, os.Getuid(), run)
}

func install(result Result, executable string, uid int, command runner) (Result, error) {
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "\n\r\x00") {
		return result, i18n.Errorf("backend.service.cli_path_must_be_absolute_and_contain_no_control_characters", "CLI path must be absolute and contain no control characters")
	}
	content, err := definition(result.Manager, executable)
	if err != nil {
		return result, err
	}
	previous, readErr := os.ReadFile(result.Path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return result, i18n.Errorf("backend.service.read_existing_service_definition_s_w", "read existing service definition %s: %w", result.Path, readErr)
	}
	existed := readErr == nil
	if err := os.MkdirAll(filepath.Dir(result.Path), 0700); err != nil {
		return result, i18n.Errorf("backend.service.create_service_directory_w", "create service directory: %w", err)
	}
	if err := replaceFile(result.Path, []byte(content)); err != nil {
		return result, i18n.Errorf("backend.service.write_service_definition_s_w", "write service definition %s: %w", result.Path, err)
	}
	var managerErr error
	if result.Manager == "systemd" {
		if err := command("systemctl", "--user", "daemon-reload"); err != nil {
			managerErr = i18n.Errorf("backend.service.systemctl_user_daemon_reload_w", "systemctl --user daemon-reload: %w", err)
		} else if err := command("systemctl", "--user", "enable", "--now", "nagi.service"); err != nil {
			managerErr = i18n.Errorf("backend.service.systemctl_user_enable_now_nagi_service_w", "systemctl --user enable --now nagi.service: %w", err)
		}
	} else {
		target := fmt.Sprintf("gui/%d/io.nagi.cli", uid)
		if err := command("launchctl", "enable", target); err != nil {
			managerErr = i18n.Errorf("backend.service.launchctl_enable_w", "launchctl enable: %w", err)
		} else if err := command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), result.Path); err != nil {
			managerErr = i18n.Errorf("backend.service.launchctl_bootstrap_w", "launchctl bootstrap: %w", err)
		}
	}
	if managerErr == nil {
		result.Installed, result.Enabled, result.Active = true, true, true
		return result, nil
	}
	var restoreErr error
	if existed {
		restoreErr = replaceFile(result.Path, previous)
	} else {
		restoreErr = os.Remove(result.Path)
	}
	if restoreErr != nil {
		return result, i18n.Errorf("backend.service.install_failed_w_restoring_definition_failed_v_check_s_and_s_state_manually", "install failed: %w; restoring definition failed: %v; check %s and %s state manually", managerErr, restoreErr, result.Path, result.Manager)
	}
	if existed {
		return result, i18n.Errorf("backend.service.install_failed_definition_restored", "install failed: %w; previous service definition restored at %s; check %s state before retrying", managerErr, result.Path, result.Manager)
	}
	return result, i18n.Errorf("backend.service.install_failed_definition_removed", "install failed: %w; new service definition removed at %s; check %s state before retrying", managerErr, result.Path, result.Manager)
}

func definition(manager, executable string) (string, error) {
	switch manager {
	case "systemd":
		// systemd expands dollar signs in ExecStart even within quotes.
		path := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$").Replace(executable)
		return "[Unit]\nDescription=Nagi mihomo manager\n\n[Service]\nType=simple\nExecStart=\"" + path + "\" __startup-watch\nRestart=on-failure\nRestartSec=10s\nTimeoutStopSec=20s\n\n[Install]\nWantedBy=default.target\n", nil
	case "launchd":
		return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>io.nagi.cli</string><key>ProgramArguments</key><array><string>" + xmlEscape(executable) + "</string><string>__startup-watch</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>\n", nil
	default:
		return "", i18n.Errorf("backend.service.unsupported_service_manager_s", "unsupported service manager: %s", manager)
	}
}

func replaceFile(path string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nagi-service-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func Uninstall() (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	return uninstall(result, os.Getuid(), run)
}

func uninstall(result Result, uid int, command runner) (Result, error) {
	if _, err := os.Stat(result.Path); err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, i18n.Errorf("backend.service.inspect_service_definition_s_w", "inspect service definition %s: %w", result.Path, err)
	}
	switch result.Manager {
	case "systemd":
		if err := command("systemctl", "--user", "disable", "--now", "nagi.service"); err != nil {
			return result, i18n.Errorf("backend.service.systemctl_user_disable_now_nagi_service_w_definition_retained_at_s_check_systemctl_user_status_nagi_", "systemctl --user disable --now nagi.service: %w; definition retained at %s; check systemctl --user status nagi.service and retry", err, result.Path)
		}
	case "launchd":
		service := fmt.Sprintf("gui/%d/io.nagi.cli", uid)
		if err := command("launchctl", "print", service); err != nil {
			if !strings.Contains(err.Error(), `Could not find service "io.nagi.cli"`) {
				return result, i18n.Errorf("backend.service.launchctl_print_w_definition_retained_at_s_check_launchctl_print_s_and_retry", "launchctl print: %w; definition retained at %s; check launchctl print %s and retry", err, result.Path, service)
			}
		} else if err := command("launchctl", "bootout", fmt.Sprintf("gui/%d", uid), result.Path); err != nil {
			return result, i18n.Errorf("backend.service.launchctl_bootout_w_definition_retained_at_s_check_launchctl_print_s_and_retry", "launchctl bootout: %w; definition retained at %s; check launchctl print %s and retry", err, result.Path, service)
		}
	default:
		return result, i18n.Errorf("backend.service.unsupported_service_manager_s", "unsupported service manager: %s", result.Manager)
	}
	if err := os.Remove(result.Path); err != nil {
		return result, i18n.Errorf("backend.service.service_stopped_but_removing_definition_s_failed_w_remove_the_file_and_retry", "service stopped but removing definition %s failed: %w; remove the file and retry", result.Path, err)
	}
	if result.Manager == "systemd" {
		if err := command("systemctl", "--user", "daemon-reload"); err != nil {
			return result, i18n.Errorf("backend.service.service_disabled_and_definition_removed_but_systemctl_user_daemon_reload_failed_w_retry_daemon_reloa", "service disabled and definition removed, but systemctl --user daemon-reload failed: %w; retry daemon-reload", err)
		}
	}
	return result, nil
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(s)
}

// Status reports whether the per-user service definition is installed and whether
// the platform manager currently considers it enabled and active.
func Status() (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	result.Installed = false
	if _, err := os.Stat(result.Path); err == nil {
		result.Installed = true
	} else if !os.IsNotExist(err) {
		return result, err
	}
	switch result.Manager {
	case "systemd":
		result.Enabled = commandOK("systemctl", "--user", "is-enabled", "nagi.service")
		result.Active = commandOK("systemctl", "--user", "is-active", "nagi.service")
	case "launchd":
		result.Active = commandOK("launchctl", "print", fmt.Sprintf("gui/%d/io.nagi.cli", os.Getuid()))
		if result.Installed {
			out, err := exec.Command("launchctl", "print-disabled", fmt.Sprintf("gui/%d", os.Getuid())).CombinedOutput()
			if err != nil {
				return result, i18n.Errorf("backend.service.read_login_startup_preference_w_inspect_launchctl_print_disabled_gui_d", "read login startup preference: %w; inspect `launchctl print-disabled gui/%d`", err, os.Getuid())
			}
			result.Enabled = !launchdDisabled(string(out))
		}
	}
	return result, nil
}

func commandOK(name string, args ...string) bool { return exec.Command(name, args...).Run() == nil }

// Enable installs the login service when needed and enables it without
// reloading a monitor that is already running.
func Enable() (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	executable, err := os.Executable()
	if err != nil {
		return result, err
	}
	return enable(result, executable, os.Getuid(), run)
}

func enable(result Result, executable string, uid int, command runner) (Result, error) {
	if _, err := os.Stat(result.Path); err != nil {
		if os.IsNotExist(err) {
			return install(result, executable, uid, command)
		}
		return result, i18n.Errorf("backend.service.inspect_service_definition_w", "inspect service definition: %w", err)
	}
	switch result.Manager {
	case "systemd":
		if err := command("systemctl", "--user", "daemon-reload"); err != nil {
			return result, err
		}
		if err := command("systemctl", "--user", "enable", "--now", "nagi.service"); err != nil {
			return result, i18n.Errorf("backend.service.enable_service_w", "enable service: %w", err)
		}
	case "launchd":
		target := fmt.Sprintf("gui/%d/io.nagi.cli", uid)
		if err := command("launchctl", "enable", target); err != nil {
			return result, i18n.Errorf("backend.service.enable_service_w", "enable service: %w", err)
		}
		if err := command("launchctl", "print", target); err != nil {
			if err := command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), result.Path); err != nil {
				return result, i18n.Errorf("backend.service.login_startup_enabled_but_loading_service_failed_w_inspect_nagi_startup_status", "login startup enabled but loading service failed: %w; inspect `nagi startup status`", err)
			}
		}
	default:
		return result, i18n.Errorf("backend.service.unsupported_service_manager_s", "unsupported service manager: %s", result.Manager)
	}
	result.Installed, result.Enabled, result.Active = true, true, true
	return result, nil
}

// Disable changes next-login activation only. The current monitor and mihomo
// remain running, preserving connections and scheduled subscription updates.
func Disable() (Result, error) {
	result, err := Status()
	if err != nil {
		return result, err
	}
	return disable(result, os.Getuid(), run)
}

func disable(result Result, uid int, command runner) (Result, error) {
	if _, err := os.Stat(result.Path); err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, i18n.Errorf("backend.service.inspect_service_definition_w", "inspect service definition: %w", err)
	}
	var err error
	switch result.Manager {
	case "systemd":
		err = command("systemctl", "--user", "disable", "nagi.service")
	case "launchd":
		err = command("launchctl", "disable", fmt.Sprintf("gui/%d/io.nagi.cli", uid))
	default:
		err = i18n.Errorf("backend.service.unsupported_service_manager_s", "unsupported service manager: %s", result.Manager)
	}
	if err != nil {
		return result, i18n.Errorf("backend.service.disable_login_startup_w", "disable login startup: %w", err)
	}
	result.Installed, result.Enabled = true, false
	return result, nil
}

var launchdDisabledEntry = regexp.MustCompile(`(?m)^\s*"io\.nagi\.cli"\s*=>\s*true\s*$`)

func launchdDisabled(output string) bool { return launchdDisabledEntry.MatchString(output) }

// PauseIfActive stops this login session's monitor without changing whether
// the service starts at the next login.
func PauseIfActive() error {
	result, err := Status()
	if err != nil || !result.Active {
		return err
	}
	switch result.Manager {
	case "systemd":
		return run("systemctl", "--user", "stop", "nagi.service")
	case "launchd":
		return run("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), result.Path)
	default:
		return i18n.Errorf("backend.service.unsupported_service_manager_s", "unsupported service manager: %s", result.Manager)
	}
}

// ResumeIfEnabled restores monitoring after a previous quit when the user's
// login-start preference remains enabled.
func ResumeIfEnabled() error {
	result, err := Status()
	if err != nil || !result.Enabled || result.Active {
		return err
	}
	switch result.Manager {
	case "systemd":
		return run("systemctl", "--user", "start", "nagi.service")
	case "launchd":
		return run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), result.Path)
	default:
		return i18n.Errorf("backend.service.unsupported_service_manager_s", "unsupported service manager: %s", result.Manager)
	}
}
