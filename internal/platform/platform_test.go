package platform

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestSignalExitedProcess(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Wait has reaped this process; the OS reports ESRCH rather than Go's
	// ErrProcessDone. Lifecycle code uses the latter for successful cleanup.
	for name, send := range map[string]func(int) error{"terminate": Terminate, "kill": ForceKill} {
		if err := send(cmd.Process.Pid); !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("%s exited process: %v", name, err)
		}
	}
}
