package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
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
		return Result{}, fmt.Errorf("unsupported service platform: %s", runtime.GOOS)
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
		return result, fmt.Errorf("CLI path must be absolute and contain no control characters")
	}
	content, err := definition(result.Manager, executable)
	if err != nil {
		return result, err
	}
	previous, readErr := os.ReadFile(result.Path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return result, fmt.Errorf("read existing service definition %s: %w", result.Path, readErr)
	}
	existed := readErr == nil
	if err := os.MkdirAll(filepath.Dir(result.Path), 0700); err != nil {
		return result, fmt.Errorf("create service directory: %w", err)
	}
	if err := replaceFile(result.Path, []byte(content)); err != nil {
		return result, fmt.Errorf("write service definition %s: %w", result.Path, err)
	}
	var managerErr error
	if result.Manager == "systemd" {
		if err := command("systemctl", "--user", "daemon-reload"); err != nil {
			managerErr = fmt.Errorf("systemctl --user daemon-reload: %w", err)
		} else if err := command("systemctl", "--user", "enable", "--now", "nagi.service"); err != nil {
			managerErr = fmt.Errorf("systemctl --user enable --now nagi.service: %w", err)
		}
	} else if err := command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), result.Path); err != nil {
		managerErr = fmt.Errorf("launchctl bootstrap: %w", err)
	}
	if managerErr == nil {
		return result, nil
	}
	var restoreErr error
	state := "new service definition removed"
	if existed {
		restoreErr = replaceFile(result.Path, previous)
		state = "previous service definition restored"
	} else {
		restoreErr = os.Remove(result.Path)
	}
	if restoreErr != nil {
		return result, fmt.Errorf("install failed: %w; restoring definition failed: %v; check %s and %s state manually", managerErr, restoreErr, result.Path, result.Manager)
	}
	return result, fmt.Errorf("install failed: %w; %s at %s; check %s state before retrying", managerErr, state, result.Path, result.Manager)
}

func definition(manager, executable string) (string, error) {
	switch manager {
	case "systemd":
		// systemd expands dollar signs in ExecStart and ExecStop even within quotes.
		path := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$").Replace(executable)
		return "[Unit]\nDescription=Nagi mihomo manager\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=\"" + path + "\" start\nExecStop=\"" + path + "\" stop\n\n[Install]\nWantedBy=default.target\n", nil
	case "launchd":
		return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>io.nagi.cli</string><key>ProgramArguments</key><array><string>" + xmlEscape(executable) + "</string><string>start</string></array><key>RunAtLoad</key><true/></dict></plist>\n", nil
	default:
		return "", fmt.Errorf("unsupported service manager: %s", manager)
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
		return result, fmt.Errorf("inspect service definition %s: %w", result.Path, err)
	}
	switch result.Manager {
	case "systemd":
		if err := command("systemctl", "--user", "disable", "--now", "nagi.service"); err != nil {
			return result, fmt.Errorf("systemctl --user disable --now nagi.service: %w; definition retained at %s; check systemctl --user status nagi.service and retry", err, result.Path)
		}
	case "launchd":
		if err := command("launchctl", "bootout", fmt.Sprintf("gui/%d", uid), result.Path); err != nil {
			return result, fmt.Errorf("launchctl bootout: %w; definition retained at %s; check launchctl print gui/%d/io.nagi.cli and retry", err, result.Path, uid)
		}
	default:
		return result, fmt.Errorf("unsupported service manager: %s", result.Manager)
	}
	if err := os.Remove(result.Path); err != nil {
		return result, fmt.Errorf("service stopped but removing definition %s failed: %w; remove the file and retry", result.Path, err)
	}
	if result.Manager == "systemd" {
		if err := command("systemctl", "--user", "daemon-reload"); err != nil {
			return result, fmt.Errorf("service disabled and definition removed, but systemctl --user daemon-reload failed: %w; retry daemon-reload", err)
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
		result.Enabled = result.Installed
	}
	return result, nil
}

func commandOK(name string, args ...string) bool { return exec.Command(name, args...).Run() == nil }

func Enable() (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	if _, e := os.Stat(result.Path); e != nil {
		return result, fmt.Errorf("service definition is not installed; run `nagi service install`: %w", e)
	}
	switch result.Manager {
	case "systemd":
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return result, err
		}
		if err := run("systemctl", "--user", "enable", "--now", "nagi.service"); err != nil {
			return result, fmt.Errorf("enable service: %w", err)
		}
	case "launchd":
		if err := run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), result.Path); err != nil {
			return result, fmt.Errorf("enable service: %w", err)
		}
	}
	return result, nil
}
func Disable() (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	switch result.Manager {
	case "systemd":
		if err := run("systemctl", "--user", "disable", "--now", "nagi.service"); err != nil {
			return result, fmt.Errorf("disable service: %w", err)
		}
	case "launchd":
		if err := run("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), result.Path); err != nil {
			return result, fmt.Errorf("disable service: %w", err)
		}
	}
	return result, nil
}
