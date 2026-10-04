// Package platform contains the supported host checks used by the CLI runtime.
package platform

import (
	"errors"
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

func Terminate(pid int) error { return signalProcess(pid, syscall.SIGTERM) }
func ForceKill(pid int) error { return signalProcess(pid, syscall.SIGKILL) }

func signalProcess(pid int, signal syscall.Signal) error {
	err := syscall.Kill(pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// Lock obtains an advisory lock for the duration of one lifecycle operation.
func Lock(file *os.File) error   { return syscall.Flock(int(file.Fd()), syscall.LOCK_EX) }
func Unlock(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
