package command

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPendingInstallationBlocksActivationButLeavesRecoveryCommandsUsable(t *testing.T) {
	prefix := t.TempDir()
	journal := filepath.Join(prefix, ".nagi-transaction")
	if err := os.Mkdir(journal, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(prefix, "bin/nagi")
	for _, args := range [][]string{{"start"}, {"restart"}, {"config", "reload"}, {"startup", "ensure"}, {"startup", "login"}} {
		err := checkInstallationStart(executable, args)
		var commandErr *commandError
		if !errors.As(err, &commandErr) || commandErr.code != "installation_recovery_required" {
			t.Errorf("%v did not report incomplete installation: %v", args, err)
		}
	}
	for _, args := range [][]string{{"status"}, {"stop"}, {"quit"}, {"recover"}, {"uninstall"}, {"update"}, {"startup", "shutdown"}} {
		if err := checkInstallationStart(executable, args); err != nil {
			t.Errorf("%v unexpectedly blocked: %v", args, err)
		}
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if err := checkInstallationStart(executable, []string{"start"}); err != nil {
		t.Fatal("start remained blocked after recovery:", err)
	}
}
