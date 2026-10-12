package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/ronigooja/Nagi/scripts"
)

type Runner func(context.Context, string, ...string) ([]byte, error)

type Manager struct {
	OS, Arch, Executable, Version, ReleaseBase string
	Run                                        Runner
}

func New(version string) (*Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Manager{OS: runtime.GOOS, Arch: runtime.GOARCH, Executable: executable,
		Version: version, ReleaseBase: "https://github.com/ronigooja/Nagi/releases", Run: runProcess}, nil
}

func runProcess(ctx context.Context, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = os.Stdin
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if err != nil {
		message := strings.TrimSpace(output.String())
		if len(message) > 32768 {
			message = message[len(message)-32768:]
		}
		return output.Bytes(), fmt.Errorf("%s failed: %s: %w", filepath.Base(executable), message, err)
	}
	return output.Bytes(), nil
}

type record struct{ Prefix, App string }

func (m *Manager) record() (record, error) {
	r := record{Prefix: filepath.Dir(filepath.Dir(m.Executable))}
	if filepath.Base(m.Executable) != "nagi" || filepath.Base(filepath.Dir(m.Executable)) != "bin" {
		return r, fmt.Errorf("use the installed CLI at PREFIX/bin/nagi")
	}
	data, err := os.ReadFile(filepath.Join(r.Prefix, "share", "nagi", "install-state"))
	if err != nil {
		return r, fmt.Errorf("no readable installer record; install Nagi using the release installer first: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "app=") {
		return r, fmt.Errorf("invalid installer record; repair the installation before retrying")
	}
	current := len(lines) == 2 && lines[0] == "nagi-installer-v4"
	legacy := len(lines) == 5 && lines[0] == "nagi-installer-v3" &&
		strings.HasPrefix(lines[2], "nagi_sha256=") && strings.HasPrefix(lines[3], "mihomo_sha256=") && strings.HasPrefix(lines[4], "app_sha256=")
	if !current && !legacy {
		return r, fmt.Errorf("invalid installer record; repair the installation before retrying")
	}
	r.App = strings.TrimPrefix(lines[1], "app=")
	if r.App != "" && (!filepath.IsAbs(r.App) || filepath.Clean(r.App) != r.App || filepath.Base(r.App) != "Nagi.app" || strings.ContainsAny(r.App, "\n\r")) {
		return r, fmt.Errorf("invalid App path in installer record")
	}
	return r, nil
}

// Manage owns maintenance operations; the terminal menu and App only dispatch.
func (m *Manager) Manage(ctx context.Context, action, tag string, purgeDataOption ...bool) (map[string]any, error) {
	purgeData := len(purgeDataOption) > 0 && purgeDataOption[0]
	if purgeData && action != "uninstall" {
		return nil, fmt.Errorf("--purge-data is only valid with uninstall")
	}
	appAction := strings.HasPrefix(action, "app-")
	if appAction && m.OS != "darwin" {
		return nil, fmt.Errorf("App management is supported only on macOS")
	}
	r, err := m.record()
	if err != nil {
		return nil, err
	}
	if action == "app-status" {
		installed := false
		if r.App != "" {
			info, err := os.Lstat(r.App)
			installed = err == nil && info.IsDir()
		}
		return map[string]any{"app": r.App, "installed": installed, "managed": true}, nil
	}
	if action == "terminal-update" {
		return m.launchTerminal(ctx, r)
	}
	if action == "background-update" {
		return m.launchBackgroundUpdate(r)
	}
	if appAction && r.Prefix != "/usr/local" {
		return nil, fmt.Errorf("Nagi.app requires the CLI at /usr/local/bin/nagi")
	}
	if action == "app-install" && r.App != "" {
		return nil, fmt.Errorf("App is already installed; use nagi update")
	}
	if action == "app-update" && r.App == "" {
		return nil, fmt.Errorf("App is not installed; use nagi app install")
	}
	if action == "app-install" && tag == "" {
		if !ValidTag(m.Version) {
			return nil, fmt.Errorf("this CLI has no release version; specify nagi app install vX.Y.Z")
		}
		tag = m.Version
	}
	if tag != "" && !ValidTag(tag) {
		return nil, fmt.Errorf("invalid release tag; use vX.Y.Z")
	}
	if m.OS != "linux" && m.OS != "darwin" || m.Arch != "amd64" && m.Arch != "arm64" {
		return nil, fmt.Errorf("unsupported release platform")
	}
	// All CLI maintenance operations for this user share one nonblocking lock.
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(cache, "Nagi"), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(cache, "Nagi", "maintenance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("another Nagi maintenance operation is running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	// Reread after acquiring the lock; another operation may have finished.
	r, err = m.record()
	if err != nil {
		return nil, err
	}
	if action == "app-install" && r.App != "" {
		return nil, fmt.Errorf("App is already installed; use nagi update")
	}
	if action == "app-update" && r.App == "" {
		return nil, fmt.Errorf("App is not installed; use nagi app install")
	}
	artifact := ""
	if action != "uninstall" && action != "app-uninstall" {
		targetOS := m.OS
		if targetOS == "darwin" {
			targetOS = "macos"
		}
		var cleanup func()
		artifact, cleanup, err = Fetch(ctx, m.ReleaseBase, targetOS+"-"+m.Arch, tag)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		if action == "app-install" || r.App != "" {
			if info, err := os.Stat(filepath.Join(artifact, "Nagi.app")); err != nil || !info.IsDir() {
				return nil, fmt.Errorf("selected release does not contain Nagi.app")
			}
		} else {
			if err := os.RemoveAll(filepath.Join(artifact, "Nagi.app")); err != nil {
				return nil, err
			}
		}
	}
	// Use this CLI's embedded installer for App-only operations and removal.
	// Whole updates use the verified release's installer.
	dir, err := os.MkdirTemp("", "nagi-installer-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	installer := filepath.Join(dir, "installer.sh")
	if err := os.WriteFile(installer, []byte(scripts.Installer), 0700); err != nil {
		return nil, err
	}
	installerAction := action
	if action == "app-update" {
		installerAction = "update"
	}
	if artifact != "" && action != "app-install" {
		installer = filepath.Join(artifact, "installer.sh")
		installerAction = "update"
	}
	argv := []string{installer, installerAction, "--prefix", r.Prefix}
	if artifact != "" {
		argv = append(argv, "--artifact-dir", artifact)
	}
	if purgeData {
		argv = append(argv, "--purge-data")
	}
	// App-only transactions preserve the engine, service, binaries, and profiles.
	if action == "app-install" || action == "app-uninstall" {
		if _, err := m.Run(ctx, "bash", argv...); err != nil {
			return nil, err
		}
		app := r.App
		if action == "app-install" {
			app = "/Applications/Nagi.app"
		}
		return map[string]any{"action": action, "completed": true, "app": app}, nil
	}
	// Verify current files and candidate executables before stopping anything.
	// Older released installers may not support --verify; use the embedded one
	// for this read-only preflight, then the release installer for the transaction.
	verify := append([]string{filepath.Join(dir, "installer.sh"), installerAction, "--prefix", r.Prefix}, "--verify")
	if artifact != "" {
		verify = append(verify, "--artifact-dir", artifact)
	}
	if _, err := m.Run(ctx, "bash", verify...); err != nil {
		return nil, err
	}
	if m.OS == "darwin" && r.App != "" {
		if _, err := m.Run(ctx, "pgrep", "-x", "NagiApp"); err == nil {
			return nil, fmt.Errorf("quit Nagi.app before updating or uninstalling; its Update option does this automatically")
		}
	}
	var serviceState struct{ Installed, Enabled bool }
	if err := m.query(ctx, []string{"service", "status"}, &serviceState); err != nil {
		return nil, err
	}
	var engineState struct{ Running bool }
	if err := m.query(ctx, []string{"status"}, &engineState); err != nil {
		return nil, err
	}
	if _, err := m.Run(ctx, m.Executable, "--json", "quit"); err != nil {
		return nil, fmt.Errorf("cannot stop Nagi before maintenance: %w", err)
	}
	// Quit preserves next-login running intent. Record an explicit stop before
	// reinstalling the login monitor so it cannot start the engine mid-update.
	if data, err := m.Run(ctx, m.Executable, "--json", "stop"); err != nil {
		var response struct{ Error struct{ Code string } }
		if json.Unmarshal(data, &response) != nil || response.Error.Code != "not_running" {
			return nil, fmt.Errorf("cannot record a stopped engine before maintenance: %w", err)
		}
	}
	if serviceState.Installed {
		if _, err := m.Run(ctx, m.Executable, "--json", "service", "uninstall"); err != nil {
			return nil, fmt.Errorf("Nagi is stopped; cannot remove login service: %w", err)
		}
	}
	transactionOutput, transactionErr := m.Run(ctx, "bash", argv...)
	if action == "uninstall" && transactionErr == nil {
		return map[string]any{"action": action, "completed": true, "user_data_removed": purgeData}, nil
	}
	if action == "uninstall" && purgeData && bytes.Contains(transactionOutput, []byte("NAGI_PURGE_DATA_INCOMPLETE:")) {
		return nil, fmt.Errorf("Nagi was removed, but user data cleanup was incomplete; some user data may remain: %w", transactionErr)
	}
	// Restore login preferences after success or a rolled-back failure. A failed
	// transaction leaves mihomo stopped so recovery remains an explicit action.
	var restoreErr error
	if serviceState.Installed {
		_, restoreErr = m.Run(ctx, m.Executable, "--json", "service", "install")
		if restoreErr == nil && !serviceState.Enabled {
			_, restoreErr = m.Run(ctx, m.Executable, "--json", "startup", "disable")
		}
	}
	if transactionErr != nil {
		return nil, errors.Join(fmt.Errorf("installation failed; inspect installer details; Nagi remains stopped: %w", transactionErr), restoreErr)
	}
	if restoreErr != nil {
		return nil, fmt.Errorf("files updated, but login service restoration failed; run nagi service install: %w", restoreErr)
	}
	if engineState.Running {
		if _, err := m.Run(ctx, m.Executable, "--json", "start"); err != nil {
			return nil, fmt.Errorf("files updated, but engine restart failed; run nagi status and nagi logs: %w", err)
		}
	}
	return map[string]any{"action": "update", "completed": true, "engine_restarted": engineState.Running}, nil
}

func (m *Manager) query(ctx context.Context, argv []string, dest any) error {
	data, err := m.Run(ctx, m.Executable, append([]string{"--json"}, argv...)...)
	if err != nil {
		return err
	}
	var envelope struct {
		OK   bool
		Data json.RawMessage
	}
	if err := json.Unmarshal(data, &envelope); err != nil || !envelope.OK {
		return fmt.Errorf("installed CLI returned invalid %s response", strings.Join(argv, " "))
	}
	return json.Unmarshal(envelope.Data, dest)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func (m *Manager) launchTerminal(ctx context.Context, r record) (map[string]any, error) {
	if m.OS != "darwin" || r.App == "" {
		return nil, fmt.Errorf("terminal update requires an installed macOS App")
	}
	// No downloaded code is run by the launcher. The installed CLI downloads and
	// verifies the release after the App has completed its normal quit cleanup.
	command := "i=0; while pgrep -x NagiApp >/dev/null 2>&1; do i=$((i+1)); if [ \"$i\" -ge 60 ]; then echo 'Update cancelled: Nagi.app did not quit.'; exit 1; fi; sleep 1; done; " +
		shellQuote(m.Executable) + " update && open " + shellQuote(r.App)
	command = "/bin/bash -c " + shellQuote(command)
	script := "on run argv\n tell application \"Terminal\"\n activate\n do script (item 1 of argv)\n end tell\nend run"
	if _, err := m.Run(ctx, "/usr/bin/osascript", "-e", script, command); err != nil {
		return nil, fmt.Errorf("cannot open Terminal for update: %w", err)
	}
	return map[string]any{"action": "update", "scheduled": true, "terminal": true}, nil
}

// launchBackgroundUpdate starts the whole-update transaction after this App
// exits. It deliberately detaches from the App's CLI process so replacing the
// installed executable cannot interrupt the transaction.
func (m *Manager) launchBackgroundUpdate(r record) (map[string]any, error) {
	if m.OS != "darwin" || r.App == "" {
		return nil, fmt.Errorf("background update requires an installed macOS App")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(cache, "Nagi")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	command := "if [ ! -w /usr/local/bin ] || [ -e /Library/LaunchDaemons/io.nagi.privileged-helper.plist ]; then " +
		"askpass=$(mktemp -t nagi-askpass) || exit 1; trap 'rm -f \"$askpass\"' EXIT; " +
		"cat >\"$askpass\" <<'EOF'\n#!/bin/sh\nexec /usr/bin/osascript -e 'display dialog \"Nagi needs administrator authorization to install the update.\" default answer \"\" with hidden answer buttons {\"Cancel\", \"OK\"} default button \"OK\" cancel button \"Cancel\"' -e 'text returned of result'\nEOF\n" +
		"chmod 700 \"$askpass\"; SUDO_ASKPASS=\"$askpass\" sudo -A -v || exit 1; fi; " +
		"i=0; while pgrep -x NagiApp >/dev/null 2>&1; do i=$((i+1)); if [ \"$i\" -ge 60 ]; then echo 'Update cancelled: Nagi.app did not quit.'; exit 1; fi; sleep 1; done; " +
		shellQuote(m.Executable) + " update && open " + shellQuote(r.App)
	cmd := exec.Command("/bin/bash", "-c", command)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("cannot start background update: %w", err)
	}
	_ = logFile.Close()
	return map[string]any{"action": "update", "scheduled": true, "background": true}, nil
}
