// Package platform contains the supported host checks used by the CLI runtime.
package platform

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

func Check() error {
	switch runtime.GOOS {
	case "darwin", "linux":
		return nil
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// Alive reports whether a process exists and can be signalled by this user.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func Terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
func ForceKill(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }

// Lock obtains an advisory lock for the duration of one lifecycle operation.
func Lock(file *os.File) error   { return syscall.Flock(int(file.Fd()), syscall.LOCK_EX) }
func Unlock(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
