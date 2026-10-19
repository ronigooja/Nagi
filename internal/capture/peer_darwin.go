//go:build darwin

package capture

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
	"syscall"
)

func ownedBy(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}
func peerUID(c net.Conn) (int, error) {
	uc, err := c.(*net.UnixConn).SyscallConn()
	if err != nil {
		return -1, err
	}
	var cred *unix.Xucred
	var inner error
	err = uc.Control(func(fd uintptr) { cred, inner = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED) })
	if err != nil {
		return -1, err
	}
	if inner != nil {
		return -1, inner
	}
	return int(cred.Uid), nil
}
