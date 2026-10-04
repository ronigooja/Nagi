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
	Path    string `json:"path"`
	Manager string `json:"manager"`
}

func location() (Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	switch runtime.GOOS {
	case "darwin":
		return Result{filepath.Join(home, "Library", "LaunchAgents", "io.nagi.cli.plist"), "launchd"}, nil
	case "linux":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return Result{filepath.Join(base, "systemd", "user", "nagi.service"), "systemd"}, nil
	default:
		return Result{}, fmt.Errorf("unsupported service platform: %s", runtime.GOOS)
	}
}

func Install(executable string) (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	if !filepath.IsAbs(executable) {
		return result, fmt.Errorf("CLI path must be absolute")
	}
	if err = os.MkdirAll(filepath.Dir(result.Path), 0700); err != nil {
		return result, err
	}
	var content string
	if result.Manager == "systemd" {
		if strings.ContainsAny(executable, "\n\r\x00") {
			return result, fmt.Errorf("invalid CLI path")
		}
		commandPath := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(executable)
		content = "[Unit]\nDescription=Nagi mihomo manager\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=\"" + commandPath + "\" start\nExecStop=\"" + commandPath + "\" stop\n\n[Install]\nWantedBy=default.target\n"
	} else {
		content = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>io.nagi.cli</string><key>ProgramArguments</key><array><string>" + xmlEscape(executable) + "</string><string>start</string></array><key>RunAtLoad</key><true/></dict></plist>\n"
	}
	if err = os.WriteFile(result.Path, []byte(content), 0600); err != nil {
		return result, err
	}
	if result.Manager == "systemd" {
		if err = exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return result, fmt.Errorf("systemctl daemon-reload: %w", err)
		}
		if err = exec.Command("systemctl", "--user", "enable", "--now", "nagi.service").Run(); err != nil {
			return result, fmt.Errorf("systemctl enable: %w", err)
		}
	} else {
		if err = exec.Command("launchctl", "bootstrap", "gui/"+fmt.Sprint(os.Getuid()), result.Path).Run(); err != nil {
			return result, fmt.Errorf("launchctl bootstrap: %w", err)
		}
	}
	return result, nil
}

func Uninstall() (Result, error) {
	result, err := location()
	if err != nil {
		return result, err
	}
	if result.Manager == "systemd" {
		_ = exec.Command("systemctl", "--user", "disable", "--now", "nagi.service").Run()
	} else {
		_ = exec.Command("launchctl", "bootout", "gui/"+fmt.Sprint(os.Getuid()), result.Path).Run()
	}
	if err = os.Remove(result.Path); err != nil && !os.IsNotExist(err) {
		return result, err
	}
	if result.Manager == "systemd" {
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	return result, nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return r.Replace(s)
}
