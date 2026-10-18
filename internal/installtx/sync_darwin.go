//go:build darwin

package installtx

import (
	"golang.org/x/sys/unix"
	"os"
)

func fullSync(f *os.File) error {
	if err := f.Sync(); err != nil {
		return err
	}
	_, err := unix.FcntlInt(f.Fd(), unix.F_FULLFSYNC, 0)
	return err
}
