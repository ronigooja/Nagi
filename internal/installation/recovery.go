package installation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ronigooja/Nagi/scripts"
)

// PendingTransaction does not read install-state: that file may be one of the
// targets of an interrupted installation. Development binaries have no prefix.
func PendingTransaction(executable string) (bool, error) {
	if filepath.Base(executable) != "nagi" || filepath.Base(filepath.Dir(executable)) != "bin" {
		return false, nil
	}
	prefix := filepath.Dir(filepath.Dir(executable))
	_, err := os.Lstat(filepath.Join(prefix, ".nagi-transaction"))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Recover before parsing the installer record. The standalone installer owns
// prefix locking and any authorization required for protected installation paths.
func (m *Manager) recoverPending(ctx context.Context, action string) error {
	pending, err := PendingTransaction(m.Executable)
	if err != nil {
		return fmt.Errorf("cannot inspect installation recovery state: %w", err)
	}
	if !pending {
		return nil
	}
	if action == "app-status" {
		return fmt.Errorf("an installation transaction needs recovery; run the current release's installer.sh recover --prefix %q before checking the App", filepath.Dir(filepath.Dir(m.Executable)))
	}
	dir, err := os.MkdirTemp("", "nagi-installation-recovery-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "installer.sh")
	if err := os.WriteFile(path, []byte(scripts.Installer), 0700); err != nil {
		return err
	}
	prefix := filepath.Dir(filepath.Dir(m.Executable))
	if _, err := m.Run(ctx, "bash", path, "recover", "--prefix", prefix, "--transaction-helper", m.Executable); err != nil {
		return fmt.Errorf("installation file recovery failed; retained transaction backups must be recovered before maintenance: %w", err)
	}
	return nil
}
