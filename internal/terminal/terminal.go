package terminal

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// IsTerminal reports whether file refers to an interactive terminal.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	request := uintptr(0x5401) // Linux TCGETS
	if runtime.GOOS == "darwin" {
		request = 0x40487413 // macOS TIOCGETA
	}
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}
