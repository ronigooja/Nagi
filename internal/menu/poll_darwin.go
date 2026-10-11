package menu

import (
	"syscall"
	"unsafe"
)

func terminalReady(fd uintptr) (uintptr, syscall.Errno) {
	poll := struct {
		FD       int32
		Events   int16
		Returned int16
	}{FD: int32(fd), Events: 1}
	n, _, err := syscall.Syscall(syscall.SYS_POLL, uintptr(unsafe.Pointer(&poll)), 1, 50)
	return n, err
}
