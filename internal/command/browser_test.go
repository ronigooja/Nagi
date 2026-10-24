package command

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestBrowserInvocationValidationAndHelpWithoutEngine(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, args := range [][]string{{"browser"}, {"browser", "chrome-webrtc"}, {"browser", "chrome-webrtc", "reset"}, {"browser", "chrome-webrtc", "status", "extra"}} {
		var out, errs bytes.Buffer
		if code := Run(append([]string{"--json"}, args...), &out, &errs, "test", "commit"); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), `"code":"usage"`) {
			t.Fatalf("%v: stdout=%s stderr=%s exit=%d", args, out.String(), errs.String(), code)
		}
	}
	for _, args := range [][]string{{"browser", "--help"}, {"browser", "chrome-webrtc", "--help"}, {"browser", "chrome-webrtc", "enable", "--help"}} {
		var out, errs bytes.Buffer
		if code := Run(args, &out, &errs, "test", "commit"); code != 0 || errs.Len() != 0 || !strings.Contains(out.String(), "browser") {
			t.Fatalf("%v: stdout=%s stderr=%s exit=%d", args, out.String(), errs.String(), code)
		}
	}
}
func TestBrowserUnsupportedPlatformIsLocalizedAndDoesNotReadProfile(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("unsupported-platform contract")
	}
	invalidTestConfigDir(t, t.TempDir())
	for _, op := range []string{"status", "enable", "disable", "restore"} {
		var out, errs bytes.Buffer
		code := Run([]string{"--json", "browser", "chrome-webrtc", op}, &out, &errs, "test", "commit")
		var failure struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal(errs.Bytes(), &failure); err != nil || code != 1 || out.Len() != 0 || failure.Error.Code != "browser_preference_error" || !strings.Contains(failure.Error.Message, "only on macOS") {
			t.Fatalf("op=%s exit=%d stdout=%s stderr=%s", op, code, out.String(), errs.String())
		}
	}
}
func TestBrowserBashCompletionThirdLevel(t *testing.T) {
	script, _ := completion("bash")
	probe := script + "\n" + `COMP_WORDS=(nagi browser chrome-webrtc ''); COMP_CWORD=3; _nagi_complete; printf '%s\n' "${COMPREPLY[*]}"`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil || string(out) != "status enable disable restore\n" {
		t.Fatalf("completion: %s %v", out, err)
	}
	for _, shell := range []string{"zsh", "fish"} {
		script, _ := completion(shell)
		if !strings.Contains(script, "chrome-webrtc") || !strings.Contains(script, "status enable disable restore") {
			t.Fatalf("missing browser completion for %s", shell)
		}
	}
}
