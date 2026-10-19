package command

import (
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
		return fail("installation_recovery_required", messageError("cli.error.installation_recovery.cannot_inspect_installation_recovery_state_w", "cannot inspect installation recovery state: %w", err))
	}
	if pending {
		return fail("installation_recovery_required", messageError("cli.error.installation_recovery.an_interrupted_installation_must_be_recovered_before_starting_or_rel", "an interrupted installation must be recovered before starting or reloading mihomo; run the current release's installer.sh recover --prefix %q, then check service and privileged-helper state", filepath.Dir(filepath.Dir(executable))))
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
