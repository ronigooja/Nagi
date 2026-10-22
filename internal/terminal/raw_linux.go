//go:build linux

package terminal

import (
	"os"
	"syscall"
	"unsafe"
)

// Raw enables character input without echo. The returned function restores the
// exact previous terminal state and must be called on every exit path.
func Raw(file *os.File) (func(), error) {
	var old syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x5401), uintptr(unsafe.Pointer(&old)))
	if errno != 0 {
		return nil, errno
	}
	next := old
	next.Lflag &^= syscall.ICANON | syscall.ECHO
	next.Cc[syscall.VMIN], next.Cc[syscall.VTIME] = 0, 0
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x5402), uintptr(unsafe.Pointer(&next)))
	if errno != 0 {
		return nil, errno
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x5402), uintptr(unsafe.Pointer(&old)))
	}, nil
}

// FlushInput discards bytes already queued for the terminal without waiting for
// future user input.
func FlushInput(file *os.File) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x540b), 0) // TCFLSH, TCIFLUSH
	if errno != 0 {
		return errno
	}
	return nil
}
