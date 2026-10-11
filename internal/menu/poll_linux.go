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
	timeout := syscall.Timespec{Nsec: 50000000}
	n, _, err := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&poll)), 1, uintptr(unsafe.Pointer(&timeout)), 0, 0, 0)
	return n, err
}
