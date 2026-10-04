package command

import (
	"bytes"
	"strings"
	"testing"
)

func TestTUIHelpAndJSONContract(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"tui", "--help"}, &out, &errOut, "dev", "pin"); code != 0 || !strings.Contains(out.String(), "Usage: nagi tui") {
		t.Fatalf("help: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"--json", "tui"}, &out, &errOut, "dev", "pin"); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), `"code":"usage"`) {
		t.Fatalf("json: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"--json", "version"}, &out, &errOut, "dev", "pin"); code != 0 || !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("other JSON changed: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
