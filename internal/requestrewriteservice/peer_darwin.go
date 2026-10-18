//go:build darwin

package requestrewriteservice

import (
	"golang.org/x/sys/unix"
	"net"
)

func peerUID(conn net.Conn) (int, error) {
	c, ok := conn.(*net.UnixConn)
	if !ok {
		return -1, errAuth
	}
	raw, e := c.SyscallConn()
	if e != nil {
		return -1, errAuth
	}
	uid := -1
	var inner error
	e = raw.Control(func(fd uintptr) {
		v, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		inner = e
		if e == nil {
			uid = int(v.Uid)
		}
	})
	if e != nil || inner != nil {
		return -1, errAuth
	}
	return uid, nil
}
