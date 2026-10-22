//go:build darwin

package terminal

import (
	"os"
	"syscall"
	"unsafe"
)

func Raw(file *os.File) (func(), error) {
	var old syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x40487413), uintptr(unsafe.Pointer(&old))) // TIOCGETA
	if errno != 0 {
		return nil, errno
	}
	next := old
	next.Lflag &^= syscall.ICANON | syscall.ECHO
	next.Cc[syscall.VMIN], next.Cc[syscall.VTIME] = 0, 0
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x80487414), uintptr(unsafe.Pointer(&next))) // TIOCSETA
	if errno != 0 {
		return nil, errno
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x80487414), uintptr(unsafe.Pointer(&old)))
	}, nil
}

func FlushInput(file *os.File) error {
	selector := int32(1)                                                                                                  // FREAD
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(0x80047410), uintptr(unsafe.Pointer(&selector))) // TIOCFLUSH
	if errno != 0 {
		return errno
	}
	return nil
}
