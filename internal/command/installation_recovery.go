package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ronigooja/Nagi/internal/installation"
)

func checkInstallationStart(executable string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	starts := args[0] == "start" || args[0] == "restart" ||
		args[0] == "config" && len(args) > 1 && args[1] == "reload" ||
		args[0] == "startup" && len(args) > 1 && (args[1] == "ensure" || args[1] == "login")
	if !starts {
		return nil
	}
	pending, err := installation.PendingTransaction(executable)
	if err != nil {
		return fail("installation_recovery_required", fmt.Errorf("cannot inspect installation recovery state: %w", err))
	}
	if pending {
		return fail("installation_recovery_required", fmt.Errorf("an interrupted installation must be recovered before starting or reloading mihomo; run the current release's installer.sh recover --prefix %q, then check service and privileged-helper state", filepath.Dir(filepath.Dir(executable))))
	}
	return nil
}

func guardInstallationStart(args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return checkInstallationStart(executable, args)
}
