package command

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMenuContextSurvivesInterruptCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), "key", "value"))
	cancel()

	got := menuContext(parent)
	if got.Err() != nil {
		t.Fatalf("menu context canceled: %v", got.Err())
	}
	if got.Value("key") != "value" {
		t.Fatalf("menu context lost value: %v", got.Value("key"))
	}
}

func TestMenuRequiresTerminal(t *testing.T) {
	for _, args := range [][]string{{"menu"}, {"--json", "menu"}} {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr, "dev", "test")
		if code != 2 {
			t.Fatalf("%v: exit status %d", args, code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "interactive terminal") {
			t.Fatalf("%v: stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestMenuHelpWithoutRuntime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"menu", "--help"}, &stdout, &stderr, "dev", "test")
	if code != 0 || !strings.Contains(stdout.String(), "Usage: nagi menu") || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
