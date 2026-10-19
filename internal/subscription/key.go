package subscription

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
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
		return i18n.Errorf("backend.subscription.invalid_subscription_name", "invalid subscription name")
	}
	if err := s.requireEntry(name); err != nil {
		return err
	}
	fd, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return i18n.Errorf("backend.subscription.cannot_open_identity_file_use_a_readable_regular_file", "cannot open identity file; use a readable regular file")
	}
	f := os.NewFile(uintptr(fd), "identity")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxIdentitySize {
		return i18n.Errorf("backend.subscription.identity_must_be_a_nonempty_regular_file_of_at_most_1_mib", "identity must be a nonempty regular file of at most 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxIdentitySize+1))
	if err != nil || len(data) == 0 || len(data) > maxIdentitySize {
		return i18n.Errorf("backend.subscription.cannot_read_bounded_identity_file", "cannot read bounded identity file")
	}
	if err := os.MkdirAll(s.keyDir(), 0700); err != nil {
		return i18n.Errorf("backend.subscription.cannot_create_private_identity_directory", "cannot create private identity directory")
	}
	if info, err := os.Lstat(s.keyDir()); err != nil || !info.IsDir() {
		return i18n.Errorf("backend.subscription.identity_directory_must_be_a_private_regular_directory", "identity directory must be a private regular directory")
	}
	if err := os.Chmod(s.keyDir(), 0700); err != nil {
		return i18n.Errorf("backend.subscription.cannot_secure_identity_directory", "cannot secure identity directory")
	}
	if err := writeReplace(s.keyPath(name), data); err != nil {
		return i18n.Errorf("backend.subscription.cannot_store_private_identity_file", "cannot store private identity file")
	}
	return nil
}

func (s *Store) ClearKey(name string) error {
	if !validName(name) {
		return i18n.Errorf("backend.subscription.invalid_subscription_name", "invalid subscription name")
	}
	if info, err := os.Lstat(s.keyDir()); err == nil && !info.IsDir() {
		return i18n.Errorf("backend.subscription.identity_directory_must_be_a_private_regular_directory", "identity directory must be a private regular directory")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf("backend.subscription.cannot_inspect_private_identity_directory", "cannot inspect private identity directory")
	}
	err := os.Remove(s.keyPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return i18n.Errorf("backend.subscription.cannot_remove_private_identity_file", "cannot remove private identity file")
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
	return i18n.Errorf("backend.subscription.subscription_not_found", "subscription not found")
}

func (s *Store) RequireEntry(name string) error { return s.requireEntry(name) }

type limitedPlaintext struct {
	bytes.Buffer
	overflow bool
}

func (w *limitedPlaintext) Write(p []byte) (int, error) {
	if len(p) > maxSubscriptionSize-w.Len() {
		w.overflow = true
		return 0, i18n.Errorf("backend.subscription.decrypted_content_exceeds_limit", "decrypted content exceeds limit")
	}
	return w.Buffer.Write(p)
}

func (s *Store) decrypt(ctx context.Context, name string, encrypted []byte) ([]byte, error) {
	if !s.hasKey(name) {
		return nil, i18n.Errorf("backend.subscription.encrypted_subscription_needs_an_identity_run_nagi_subscription_key_name_file", "encrypted subscription needs an identity; run `nagi subscription key NAME FILE`")
	}
	if s.Binary == "" {
		return nil, i18n.Errorf("backend.subscription.mihomo_executable_unavailable_set_nagi_mihomo_bin_to_the_pinned_mihomo_executable", "mihomo executable unavailable; set NAGI_MIHOMO_BIN to the pinned mihomo executable")
	}
	binary, err := exec.LookPath(s.Binary)
	if err != nil {
		return nil, i18n.Errorf("backend.subscription.mihomo_executable_unavailable_check_nagi_mihomo_bin", "mihomo executable unavailable; check NAGI_MIHOMO_BIN")
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
			return nil, i18n.Errorf("backend.subscription.decrypted_subscription_exceeds_8_mib_previous_cache_retained", "decrypted subscription exceeds 8 MiB; previous cache retained")
		}
		return nil, i18n.Errorf("backend.subscription.mihomo_could_not_decrypt_subscription_check_the_configured_identity_and_encrypted_source_previous_ca", "mihomo could not decrypt subscription; check the configured identity and encrypted source; previous cache retained")
	}
	if plain.Len() == 0 {
		return nil, i18n.Errorf("backend.subscription.mihomo_returned_empty_decrypted_subscription_previous_cache_retained", "mihomo returned empty decrypted subscription; previous cache retained")
	}
	return plain.Bytes(), nil
}
