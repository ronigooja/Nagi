package subscription

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const maxIdentitySize = 1 << 20

func (s *Store) keyDir() string { return filepath.Join(s.ConfigDir, "subscription-keys") }

func (s *Store) keyPath(name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(s.keyDir(), hex.EncodeToString(sum[:])+".txt")
}

func (s *Store) hasKey(name string) bool {
	dir, err := os.Lstat(s.keyDir())
	if err != nil || !dir.IsDir() || dir.Mode().Perm() != 0700 {
		return false
	}
	info, err := os.Lstat(s.keyPath(name))
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600
}

// SetKey copies a bounded local identity file. Its contents never enter CLI
// arguments or the subscription index.
func (s *Store) SetKey(name, source string) error {
	if !validName(name) {
		return errors.New("invalid subscription name")
	}
	if err := s.requireEntry(name); err != nil {
		return err
	}
	fd, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("cannot open identity file; use a readable regular file")
	}
	f := os.NewFile(uintptr(fd), "identity")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxIdentitySize {
		return errors.New("identity must be a nonempty regular file of at most 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxIdentitySize+1))
	if err != nil || len(data) == 0 || len(data) > maxIdentitySize {
		return errors.New("cannot read bounded identity file")
	}
	if err := os.MkdirAll(s.keyDir(), 0700); err != nil {
		return errors.New("cannot create private identity directory")
	}
	if info, err := os.Lstat(s.keyDir()); err != nil || !info.IsDir() {
		return errors.New("identity directory must be a private regular directory")
	}
	if err := os.Chmod(s.keyDir(), 0700); err != nil {
		return errors.New("cannot secure identity directory")
	}
	if err := writeReplace(s.keyPath(name), data); err != nil {
		return errors.New("cannot store private identity file")
	}
	return nil
}

func (s *Store) ClearKey(name string) error {
	if !validName(name) {
		return errors.New("invalid subscription name")
	}
	if info, err := os.Lstat(s.keyDir()); err == nil && !info.IsDir() {
		return errors.New("identity directory must be a private regular directory")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.New("cannot inspect private identity directory")
	}
	err := os.Remove(s.keyPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot remove private identity file")
	}
	return nil
}

func (s *Store) requireEntry(name string) error {
	entries, err := s.read()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return nil
		}
	}
	return errors.New("subscription not found")
}

func (s *Store) RequireEntry(name string) error { return s.requireEntry(name) }

type limitedPlaintext struct {
	bytes.Buffer
	overflow bool
}

func (w *limitedPlaintext) Write(p []byte) (int, error) {
	if len(p) > maxSubscriptionSize-w.Len() {
		w.overflow = true
		return 0, errors.New("decrypted content exceeds limit")
	}
	return w.Buffer.Write(p)
}

func (s *Store) decrypt(ctx context.Context, name string, encrypted []byte) ([]byte, error) {
	if !s.hasKey(name) {
		return nil, errors.New("encrypted subscription needs an identity; run `nagi subscription key NAME FILE`")
	}
	if s.Binary == "" {
		return nil, errors.New("mihomo executable unavailable; set NAGI_MIHOMO_BIN to the pinned mihomo executable")
	}
	binary, err := exec.LookPath(s.Binary)
	if err != nil {
		return nil, errors.New("mihomo executable unavailable; check NAGI_MIHOMO_BIN")
	}
	decryptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(decryptCtx, binary, "--decrypt-config", "-", "--age-identity-file", s.keyPath(name))
	cmd.Stdin = bytes.NewReader(encrypted)
	cmd.Stderr = io.Discard
	var plain limitedPlaintext
	cmd.Stdout = &plain
	if err := cmd.Run(); err != nil {
		if plain.overflow {
			return nil, errors.New("decrypted subscription exceeds 8 MiB; previous cache retained")
		}
		return nil, errors.New("mihomo could not decrypt subscription; check the configured identity and encrypted source; previous cache retained")
	}
	if plain.Len() == 0 {
		return nil, errors.New("mihomo returned empty decrypted subscription; previous cache retained")
	}
	return plain.Bytes(), nil
}
