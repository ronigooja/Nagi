//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const linuxUnitPath = "/etc/systemd/system/nagi-privileged-helper.service"
const linuxUnitName = "nagi-privileged-helper.service"
const linuxExecutablePath = "/usr/local/bin/nagi"

type commandRunner func(string, ...string) error

func linuxRun(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}

// Install registers the root helper as a system service. It does not alter the
// existing per-user service, which remains responsible for login monitoring.
func Install(executable string) (Result, error) {
	return linuxResult(), fmt.Errorf("privileged Linux helper installation is unavailable until restricted controller access and configuration validation are implemented")
}

func linuxResult() Result {
	return Result{Path: linuxUnitPath, Manager: "systemd-system", Executable: linuxExecutablePath}
}

func linuxInstall(path, executable string, uid int, run commandRunner) (Result, error) {
	result := Result{Path: path, Manager: "systemd-system", Executable: linuxExecutablePath}
	if uid != 0 {
		return result, fmt.Errorf("installing the privileged helper requires root; rerun with sudo")
	}
	if executable != linuxExecutablePath {
		return result, fmt.Errorf("run the installed %s executable to install its helper", linuxExecutablePath)
	}
	if err := rootControlledExecutable(executable); err != nil {
		return result, fmt.Errorf("unsafe helper executable: %w", err)
	}
	unit, err := linuxDefinition(executable)
	if err != nil {
		return result, err
	}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("read existing helper unit: %w", err)
	}
	existed := err == nil
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return result, fmt.Errorf("helper unit path is a symlink: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("inspect helper unit: %w", err)
	}
	if err := writeLinuxUnit(path, []byte(unit)); err != nil {
		return result, err
	}
	managerErr := run("systemctl", "daemon-reload")
	if managerErr == nil {
		managerErr = run("systemctl", "enable", "--now", linuxUnitName)
	}
	if managerErr == nil {
		result.Installed, result.Active = true, true
		return result, nil
	}
	var rollbackErr error
	if existed {
		rollbackErr = writeLinuxUnit(path, old)
	} else {
		rollbackErr = os.Remove(path)
	}
	if rollbackErr == nil {
		rollbackErr = run("systemctl", "daemon-reload")
	}
	if rollbackErr != nil {
		return result, fmt.Errorf("activate helper unit: %w; rollback failed: %v; inspect %s and systemctl status %s", managerErr, rollbackErr, path, linuxUnitName)
	}
	return result, fmt.Errorf("activate helper unit: %w; previous unit definition restored; inspect systemctl status %s", managerErr, linuxUnitName)
}

func linuxDefinition(executable string) (string, error) {
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "\n\r\x00") {
		return "", fmt.Errorf("helper executable path must be absolute and contain no control characters")
	}
	// systemd expands specifiers and dollar signs even inside quoted values.
	quoted := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$").Replace(executable)
	return "[Unit]\nDescription=Nagi privileged mihomo helper\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=\"" + quoted + "\" __privileged-helper\nRestart=on-failure\nRestartSec=2s\n\n[Install]\nWantedBy=multi-user.target\n", nil
}

func writeLinuxUnit(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create helper unit directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nagi-helper-unit-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Uninstall stops and removes the system service without touching the user's
// login service or profile data.
func Uninstall() (Result, error) {
	return linuxUninstall(linuxUnitPath, os.Geteuid(), linuxRun)
}

func linuxUninstall(path string, uid int, run commandRunner) (Result, error) {
	result := Result{Path: path, Manager: "systemd-system", Executable: linuxExecutablePath}
	if uid != 0 {
		return result, fmt.Errorf("uninstalling the privileged helper requires root; rerun with sudo")
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	if err := run("systemctl", "disable", "--now", linuxUnitName); err != nil {
		return result, fmt.Errorf("disable helper service: %w; unit retained at %s", err, path)
	}
	if err := os.Remove(path); err != nil {
		return result, fmt.Errorf("helper stopped but unit removal failed: %w", err)
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return result, fmt.Errorf("helper unit removed but systemd reload failed: %w", err)
	}
	return result, nil
}

func Status() (Result, error) {
	result := linuxResult()
	if _, err := os.Lstat(result.Path); err == nil {
		result.Installed = true
	} else if !os.IsNotExist(err) {
		return result, err
	}
	result.Active = exec.Command("systemctl", "is-active", linuxUnitName).Run() == nil
	return result, nil
}

func rootControlledExecutable(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\n\r\x00") {
		return fmt.Errorf("path must be absolute, clean, and contain no control characters")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in executable path: %s", current)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("%s must be root-owned and not group/world writable", current)
		}
		if current == path {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return fmt.Errorf("%s must be an executable regular file", current)
			}
		} else if !info.IsDir() {
			return fmt.Errorf("%s must be a directory", current)
		}
		if current == string(filepath.Separator) {
			break
		}
	}
	return nil
}
