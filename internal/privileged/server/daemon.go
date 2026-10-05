package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"golang.org/x/sys/unix"
)

// RunDaemon serves one login session. The caller must pass a UID obtained
// from root-owned launchd/systemd configuration, never from a client message.
func RunDaemon(ctx context.Context, uid int, binaryPath, sourceDir string) error {
	if os.Geteuid() != 0 {
		return errors.New("privileged helper requires root")
	}
	if uid <= 0 {
		return errors.New("invalid session uid")
	}
	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return fmt.Errorf("lookup session user: %w", err)
	}
	expectedSource := filepath.Join(account.HomeDir, ".config", "nagi")
	if runtime.GOOS == "darwin" {
		expectedSource = filepath.Join(account.HomeDir, "Library", "Application Support", "Nagi", "config")
	}
	if sourceDir != expectedSource {
		return errors.New("session source directory does not match user home")
	}
	if err := verifyTrustedBinary(binaryPath); err != nil {
		return err
	}
	root := privileged.RuntimeRoot()
	if err := rootDirectory(root, 0711); err != nil {
		return err
	}
	sessionDir := filepath.Dir(privileged.SocketPath(uid))
	if err := rootDirectory(sessionDir, 0711); err != nil {
		return err
	}
	privateRoot := filepath.Join(root, "private")
	if err := rootDirectory(privateRoot, 0700); err != nil {
		return err
	}
	privateDir := filepath.Join(privateRoot, "uid-"+strconv.Itoa(uid))
	if err := rootDirectory(privateDir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(privateDir, "daemon.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("privileged helper session is already active")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	path := privileged.SocketPath(uid)
	if _, err := os.Lstat(path); err == nil {
		conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return errors.New("privileged helper session is already active")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(path)
	if err := os.Chown(path, uid, -1); err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	handler := &RuntimeHandler{SessionUID: uid, BinaryPath: binaryPath, SourceDir: sourceDir, PrivateDir: privateDir}
	defer handler.Close()
	return (Server{SessionUID: uid, Policy: privileged.Policy{AllowedPaths: []string{sourceDir, binaryPath, root}}, Handler: handler}).Serve(ctx, listener)
}

func rootDirectory(path string, mode os.FileMode) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid root directory path")
	}
	if err := verifyAncestors(filepath.Dir(path), 0); err != nil {
		return err
	}
	if err := os.Mkdir(path, mode); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	} else if err := os.Chmod(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != 0 || info.Mode().Perm() != mode {
		return fmt.Errorf("unsafe root directory %s", path)
	}
	return nil
}

func verifyTrustedBinary(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("trusted mihomo path must be absolute and clean")
	}
	if err := verifyAncestors(filepath.Dir(path), 0); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("unsafe mihomo executable %s", path)
	}
	return nil
}
