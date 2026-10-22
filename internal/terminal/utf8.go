package terminal

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// UTF8 enables UTF-8 character erasure in canonical terminal input. The caller
// must restore the previous settings after reading, including on cancellation.
func UTF8(file *os.File) (restore func(), err error) {
	get, set := uintptr(0x5401), uintptr(0x5402) // Linux TCGETS, TCSETS
	if runtime.GOOS == "darwin" {
		get, set = 0x40487413, 0x80487414 // TIOCGETA, TIOCSETA
	}
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), get, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return nil, errno
	}
	next := old
	next.Iflag |= 0x4000 // IUTF8 on Linux and macOS
	if next == old {
		return func() {}, nil
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), set, uintptr(unsafe.Pointer(&next))); errno != 0 {
		return nil, errno
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), set, uintptr(unsafe.Pointer(&old)))
	}, nil
}
