package command

import (
	"bytes"
	"strings"
	"testing"
)

func TestContextTestSyntaxAndHelpWithoutRuntime(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, kind := range []string{"group", "provider"} {
		var out, errOut bytes.Buffer
		args := []string{"proxy", "test", kind, "--help"}
		if code := Run(args, &out, &errOut, "test", "commit"); code != 0 || !strings.Contains(out.String(), "expected status") {
			t.Fatalf("%v code/output %s %s", args, out.String(), errOut.String())
		}
		out.Reset()
		errOut.Reset()
		if code := Run([]string{"--json", "proxy", "test", kind, "Owner"}, &out, &errOut, "test", "commit"); code != 2 || !strings.Contains(errOut.String(), `"code":"usage"`) || out.Len() != 0 {
			t.Fatalf("invalid args: %s %s", out.String(), errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"--json", "proxy", "test", "other", "Owner", "Node"}, &out, &errOut, "test", "commit"); code != 2 || !strings.Contains(errOut.String(), `"code":"usage"`) {
		t.Fatalf("unknown kind %s", errOut.String())
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, ok := completion(shell)
		if !ok || !strings.Contains(script, "group provider") || !strings.Contains(script, "test") {
			t.Fatal(shell)
		}
	}
}

func TestContextTestValidInvocation(t *testing.T) {
	for _, kind := range []string{"group", "provider"} {
		args := []string{"proxy", "test", kind, "Owner", "Node"}
		if err := validateInvocation(args); err != nil {
			t.Fatalf("valid command %v rejected: %v", args, err)
		}
	}
}
