// Package service installs the macOS privileged helper as a system LaunchDaemon.
// It deliberately does not manage the per-user login service.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
)

const (
	Label          = "io.nagi.privileged-helper"
	DefinitionPath = "/Library/LaunchDaemons/io.nagi.privileged-helper.plist"
	ExecutablePath = "/usr/local/bin/nagi"
	launchctlPath  = "/bin/launchctl"
)

type runner func(string, ...string) error

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil && len(out) != 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}

func result() Result {
	return Result{Path: DefinitionPath, Manager: "launchd-system", Executable: ExecutablePath}
}

func requireRoot(uid int) error {
	if uid != 0 {
		return i18n.Errorf("system.privileged.administrator.privileges.required.run.with.sudo", "administrator privileges required; run with sudo")
	}
	return nil
}

// Install installs the fixed, root-owned Nagi executable as a system daemon.
// The caller must be root. The executable argument is accepted only to verify
// that the invoked CLI is the expected installed binary; it never enters plist
// content without checking the fixed path's ownership and permissions.
func Install(executable string) (Result, error) {
	return install(result(), executable, os.Geteuid(), run)
}

func install(r Result, executable string, uid int, command runner) (Result, error) {
	if err := requireRoot(uid); err != nil {
		return r, err
	}
	if executable != ExecutablePath {
		return r, i18n.Errorf("system.privileged.run.the.installed.executable.to.install.its.helper", "run the installed %s executable to install its helper", ExecutablePath)
	}
	if err := secureExecutable(ExecutablePath); err != nil {
		return r, i18n.Errorf("system.privileged.unsafe.privileged.helper.executable", "unsafe privileged helper executable: %w", err)
	}
	if err := secureDirectory(filepath.Dir(r.Path)); err != nil {
		return r, i18n.Errorf("system.privileged.unsafe.launchdaemon.directory", "unsafe LaunchDaemon directory: %w", err)
	}
	sessionUID, err := sudoSessionUID()
	if err != nil {
		return r, err
	}
	if _, err := os.Lstat(r.Path); err == nil {
		return r, i18n.Errorf("system.privileged.helper.definition.already.exists.at.uninstall.before.reinstalling", "helper definition already exists at %s; uninstall before reinstalling", r.Path)
	} else if !os.IsNotExist(err) {
		return r, i18n.Errorf("system.privileged.inspect.helper.definition", "inspect helper definition: %w", err)
	}
	content := definition(sessionUID)
	if err := writeExclusive(r.Path, []byte(content)); err != nil {
		return r, i18n.Errorf("system.privileged.write.helper.definition", "write helper definition: %w", err)
	}
	if err := command(launchctlPath, "bootstrap", "system", r.Path); err != nil {
		if removeErr := os.Remove(r.Path); removeErr != nil {
			return r, i18n.Errorf("system.privileged.launchctl.bootstrap.removing.definition.failed", "launchctl bootstrap: %w; removing definition failed: %v", err, removeErr)
		}
		return r, i18n.Errorf("system.privileged.launchctl.bootstrap.definition.removed", "launchctl bootstrap: %w; definition removed", err)
	}
	r.Installed, r.Active = true, true
	return r, nil
}

// Uninstall stops the system daemon before deleting its definition.
func Uninstall() (Result, error) {
	r := result()
	return uninstall(r, os.Geteuid(), run)
}

func uninstall(r Result, uid int, command runner) (Result, error) {
	if err := requireRoot(uid); err != nil {
		return r, err
	}
	_, err := os.Lstat(r.Path)
	installed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return r, i18n.Errorf("system.privileged.inspect.helper.definition", "inspect helper definition: %w", err)
	}
	if command(launchctlPath, "print", "system/"+Label) == nil {
		if err := command(launchctlPath, "bootout", "system/"+Label); err != nil {
			if installed {
				return r, i18n.Errorf("system.privileged.launchctl.bootout.definition.retained.at", "launchctl bootout: %w; definition retained at %s", err, r.Path)
			}
			return r, i18n.Errorf("system.privileged.launchctl.bootout.no.definition.exists.at", "launchctl bootout: %w; no definition exists at %s", err, r.Path)
		}
		for attempt := 0; attempt < 20; attempt++ {
			if command(launchctlPath, "print", "system/"+Label) != nil {
				break
			}
			if attempt == 19 {
				return r, i18n.Errorf("system.privileged.helper.launchd.job.remains.active.after.bootout.inspect", "helper launchd job remains active after bootout; inspect %s", r.Path)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if installed {
		if err := os.Remove(r.Path); err != nil {
			return r, i18n.Errorf("system.privileged.helper.stopped.but.removing.definition.failed", "helper stopped but removing definition failed: %w", err)
		}
	}
	return r, nil
}

// Status reports the system daemon separately from the user login Agent.
func Status() (Result, error) {
	r := result()
	if _, err := os.Lstat(r.Path); err == nil {
		r.Installed = true
	} else if !os.IsNotExist(err) {
		return r, i18n.Errorf("system.privileged.inspect.helper.definition", "inspect helper definition: %w", err)
	}
	r.Active = run(launchctlPath, "print", "system/"+Label) == nil
	return r, nil
}

func sudoSessionUID() (int, error) {
	value := os.Getenv("SUDO_UID")
	uid, err := strconv.Atoi(value)
	if err != nil || uid <= 0 {
		return 0, i18n.Errorf("system.privileged.install.the.helper.with.sudo.from.the.intended.macos.user", "install the helper with sudo from the intended macOS user account")
	}
	return uid, nil
}

func definition(sessionUID int) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n" +
		"<plist version=\"1.0\"><dict>" +
		"<key>Label</key><string>" + Label + "</string>" +
		"<key>ProgramArguments</key><array><string>" + ExecutablePath + "</string><string>__privileged-helper</string><string>" + strconv.Itoa(sessionUID) + "</string></array>" +
		"<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>" +
		"<key>Umask</key><integer>63</integer>" +
		"</dict></plist>\n"
}

func writeExclusive(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

func secureExecutable(path string) error {
	if err := secureDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return i18n.Errorf("system.privileged.is.not.a.regular.file", "%s is not a regular file", path)
	}
	if info.Mode().Perm()&0111 == 0 {
		return i18n.Errorf("system.privileged.is.not.executable", "%s is not executable", path)
	}
	return secureOwnership(path, info)
}

func secureDirectory(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return i18n.Errorf("system.privileged.is.not.a.directory", "%s is not a directory", path)
		}
		if err := secureOwnership(path, info); err != nil {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func secureOwnership(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return i18n.Errorf("system.privileged.is.not.owned.by.root", "%s is not owned by root", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		return i18n.Errorf("system.privileged.is.writable.by.group.or.others", "%s is writable by group or others", path)
	}
	// An ACL can grant write access even when POSIX mode bits do not.
	out, err := exec.Command("/bin/ls", "-lde", path).Output()
	if err != nil {
		return i18n.Errorf("system.privileged.inspect.acl.for", "inspect ACL for %s: %w", path, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || len(fields[0]) < 10 || strings.Contains(fields[0], "+") {
		return i18n.Errorf("system.privileged.has.an.acl.or.unreadable.permission.record", "%s has an ACL or unreadable permission record", path)
	}
	return nil
}
