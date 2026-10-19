package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
)

func TestLocalizedValidationKeepsJSONContract(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	var english, chinese map[string]any
	for _, language := range []string{"en", "zh-Hans"} {
		var stdout, stderr bytes.Buffer
		if exit := Run([]string{"--lang", language, "--json", "logs", "0"}, &stdout, &stderr, "test", "commit"); exit != 2 || stdout.Len() != 0 {
			t.Fatalf("%s exit=%d stdout=%s stderr=%s", language, exit, &stdout, &stderr)
		}
		var envelope map[string]any
		if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if language == "en" {
			english = envelope
		} else {
			chinese = envelope
		}
	}
	if english["error"].(map[string]any)["message"] != chinese["error"].(map[string]any)["message"] {
		t.Fatal("translated JSON error values")
	}
	if chinese["error"].(map[string]any)["code"] != "usage" || chinese["ok"] != false {
		t.Fatal(chinese)
	}
	metadata := chinese["messages"].(map[string]any)["/error/message"].(map[string]any)
	if !strings.HasSuffix(metadata["id"].(string), ".usage") {
		t.Fatal(metadata)
	}
	var stdout, stderr bytes.Buffer
	if Run([]string{"--lang", "zh-Hans", "logs", "0"}, &stdout, &stderr, "test", "commit") != 2 || !strings.Contains(stderr.String(), "行数必须") || !strings.Contains(stderr.String(), "用法：nagi logs") {
		t.Fatal(stderr.String())
	}
}

func TestLocalizedOperationErrorsKeepEnglishAndUnwrap(t *testing.T) {
	sentinel := errors.New("external diagnostic https://example.invalid/private?secret=fictional")
	message := messageError("cli.error.command.start_mihomo_first_w", "start mihomo first: %w", sentinel)
	if !errors.Is(message, sentinel) {
		t.Fatal("lost wrapped cause")
	}
	var out bytes.Buffer
	output.WriteErrorCause(&out, true, i18n.New(i18n.SimplifiedChinese), "system_proxy_error", message)
	if !strings.Contains(out.String(), "start mihomo first") || strings.Contains(out.String(), "secret=fictional") || !strings.Contains(out.String(), "messages") {
		t.Fatal(out.String())
	}
	out.Reset()
	output.WriteErrorCause(&out, false, i18n.New(i18n.SimplifiedChinese), "system_proxy_error", message)
	if !strings.Contains(out.String(), "请先启动 mihomo") || strings.Contains(out.String(), "secret=fictional") {
		t.Fatal(out.String())
	}
}

func TestLanguagePrefixBashCompletion(t *testing.T) {
	script, _ := completion("bash")
	probe := script + `
nagi() { printf '%s\n' 'Group One' Other; }
COMP_WORDS=(nagi --lang zh); COMP_CWORD=2; _nagi_complete; printf 'LANG:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi --lang=zh); COMP_CWORD=1; _nagi_complete; printf 'INLINE:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi --json --lang zh-Hans proxy show Gro); COMP_CWORD=6; _nagi_complete; printf 'GROUP:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi --lang=en proxy show Gro); COMP_CWORD=4; _nagi_complete; printf 'INLINEGROUP:%s\n' "${COMPREPLY[*]}"
COMP_WORDS=(nagi profile use --lang); COMP_CWORD=3; _nagi_complete; printf 'OPERAND:%s\n' "${COMPREPLY[*]}"
`
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, want := range []string{"LANG:zh-Hans", "INLINE:--lang=zh-Hans", "GROUP:Group One", "INLINEGROUP:Group One", "OPERAND:"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
}

func TestLocalizedCompletionContentRemainsRaw(t *testing.T) {
	invalidTestConfigDir(t, t.TempDir())
	for _, shell := range []string{"bash", "zsh", "fish"} {
		var original bytes.Buffer
		for _, lang := range []string{"en", "zh-Hans"} {
			var stdout, stderr bytes.Buffer
			if exit := Run([]string{"--lang", lang, "completion", shell}, &stdout, &stderr, "test", "commit"); exit != 0 {
				t.Fatal(exit, stderr.String())
			}
			if lang == "en" {
				original.Write(stdout.Bytes())
			} else if original.String() != stdout.String() {
				t.Fatalf("translated %s shell content", shell)
			}
		}
	}
}
