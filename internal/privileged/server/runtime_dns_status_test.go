package server

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestDNSFailureDoesNotHideLifecycleStatus(t *testing.T) {
	for _, running := range []bool{false, true} {
		name := "stopped"
		if running {
			name = "running"
		}
		t.Run(name, func(t *testing.T) {
			h := &RuntimeHandler{PrivateDir: t.TempDir(), dnsError: errors.New("DNS restoration failed")}
			if running {
				h.child = &exec.Cmd{Process: &os.Process{Pid: 12345}}
			}
			response, err := h.Handle(context.Background(), 501, privileged.Request{Operation: privileged.Status})
			if err != nil || !response.OK || response.Running != running {
				t.Fatalf("process status obscured by DNS error: response=%+v err=%v", response, err)
			}
			if running && response.PID != 12345 {
				t.Fatalf("running process PID lost: response=%+v", response)
			}
			if h.dnsError == nil {
				t.Fatal("process status silently cleared the DNS recovery error")
			}
		})
	}
}
