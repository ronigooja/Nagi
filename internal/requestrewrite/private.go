package requestrewrite

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// privateDir anchors all operations to no-follow descriptors. Ancestors cannot
// be redirected through symlinks; managed directories/files are owned privately.
type privateDir struct{ fd int }

func openPrivate(config string, ca, create bool) (*privateDir, error) {
	abs, e := filepath.Abs(config)
	if e != nil {
		return nil, invalid()
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, invalid()
	}
	parts := strings.Split(strings.TrimPrefix(abs, "/"), "/")
	parts = append(parts, "request-rewrite")
	if ca {
		parts = append(parts, "ca")
	}
	managedStart := len(strings.Split(strings.TrimPrefix(abs, "/"), "/"))
	for i, p := range parts {
		if p == "" || p == "." || p == ".." {
			unix.Close(fd)
			return nil, invalid()
		}
		next, e := unix.Openat(fd, p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) && create {
			if e = unix.Mkdirat(fd, p, 0700); e == nil || errors.Is(e, unix.EEXIST) {
				next, e = unix.Openat(fd, p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			if errors.Is(e, unix.ENOENT) {
				return nil, os.ErrNotExist
			}
			return nil, &Error{Code: "rewrite_storage"}
		}
		fd = next
		if i >= managedStart-1 {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || st.Mode&0022 != 0 || i >= managedStart && st.Mode&0077 != 0 {
				unix.Close(fd)
				return nil, &Error{Code: "rewrite_storage"}
			}
		}
	}
	return &privateDir{fd}, nil
}
func (d *privateDir) close() { unix.Close(d.fd) }
func (d *privateDir) openRegular(name string) (*os.File, error) {
	fd, e := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(e, unix.ENOENT) {
		return nil, os.ErrNotExist
	}
	if e != nil {
		return nil, &Error{Code: "rewrite_storage"}
	}
	f := os.NewFile(uintptr(fd), name)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 || st.Nlink != 1 {
		f.Close()
		return nil, &Error{Code: "rewrite_storage"}
	}
	return f, nil
}
func (d *privateDir) read(name string, max int) ([]byte, error) {
	f, e := d.openRegular(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if e != nil || len(data) > max {
		return nil, &Error{Code: "rewrite_storage"}
	}
	return data, nil
}
func (d *privateDir) remove(name string) error {
	if _, e := d.read(name, MaxDocumentBytes); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	if unix.Unlinkat(d.fd, name, 0) != nil || unix.Fsync(d.fd) != nil {
		return &Error{Code: "rewrite_storage"}
	}
	return nil
}
func (d *privateDir) write(name string, data []byte, exclusive bool) error {
	if !exclusive {
		if _, e := d.read(name, MaxDocumentBytes); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	suffix, e := randomID()
	if e != nil {
		return e
	}
	tmp := ".transaction-" + suffix
	fd, e := unix.Openat(d.fd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return &Error{Code: "rewrite_storage"}
	}
	f := os.NewFile(uintptr(fd), tmp)
	defer unix.Unlinkat(d.fd, tmp, 0)
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		return &Error{Code: "rewrite_storage"}
	}
	if exclusive {
		e = unix.Linkat(d.fd, tmp, d.fd, name, 0)
	} else {
		e = unix.Renameat(d.fd, tmp, d.fd, name)
	}
	if exclusive && e == nil {
		e = unix.Unlinkat(d.fd, tmp, 0)
	}
	if e != nil || unix.Fsync(d.fd) != nil {
		return &Error{Code: "rewrite_storage"}
	}
	return nil
}
func (d *privateDir) lock() (func(), error) {
	fd, e := unix.Openat(d.fd, ".lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, &Error{Code: "rewrite_storage"}
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 || st.Nlink != 1 {
		unix.Close(fd)
		return nil, &Error{Code: "rewrite_storage"}
	}
	if unix.Flock(fd, unix.LOCK_EX) != nil {
		unix.Close(fd)
		return nil, &Error{Code: "rewrite_storage"}
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
