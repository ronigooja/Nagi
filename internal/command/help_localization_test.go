package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func hasChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// Exercise every public command through both help entrypoints. Invalid runtime
// paths deliberately make accidental runtime resolution fail.
func TestEveryCommandHelpLocalizedAndJSONInvariant(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(name, filepath.Join(root, "absent", name))
	}
	t.Setenv("NAGI_MIHOMO_BIN", filepath.Join(root, "absent", "mihomo"))
	for _, spec := range commandSpecs {
		t.Run(spec.path, func(t *testing.T) {
			if got := spec.helpText(i18n.New(i18n.English), "detail"); got != spec.detail {
				t.Fatalf("English detail changed: %q != %q", got, spec.detail)
			}
			for _, field := range []string{"summary", "detail", "hint"} {
				en := spec.helpText(i18n.New(i18n.English), field)
				zh := spec.helpText(i18n.New(i18n.SimplifiedChinese), field)
				if en != "" && (!hasChinese(zh) || en == zh) {
					t.Fatalf("untranslated %s: %q", field, zh)
				}
			}
			var originalJSON string
			for _, lang := range []string{"en", "zh-Hans"} {
				for _, suffix := range []bool{false, true} {
					args := []string{"--lang", lang}
					if !suffix {
						args = append(args, "help")
					}
					args = append(args, strings.Fields(spec.path)...)
					if suffix {
						args = append(args, "--help")
					}
					var stdout, stderr bytes.Buffer
					if code := Run(args, &stdout, &stderr, "test", "commit"); code != 0 || stderr.Len() != 0 {
						t.Fatalf("help %v: %d %s", args, code, stderr.String())
					}
					text := stdout.String()
					if !strings.Contains(text, "nagi "+spec.syntax) || !strings.Contains(text, spec.example) {
						t.Fatalf("syntax/example changed: %q", text)
					}
					if lang == "zh-Hans" && (!strings.HasPrefix(text, "用法：") || !strings.Contains(text, "示例：")) {
						t.Fatalf("untranslated headings: %q", text)
					}
					stdout.Reset()
					stderr.Reset()
					if code := Run(append([]string{"--json"}, args...), &stdout, &stderr, "test", "commit"); code != 0 || stderr.Len() != 0 {
						t.Fatalf("JSON help: %d %s", code, stderr.String())
					}
					if originalJSON == "" {
						originalJSON = stdout.String()
					} else if stdout.String() != originalJSON {
						t.Fatalf("JSON help changed with language/entrypoint: %s", stdout.String())
					}
					var envelope struct {
						OK   bool   `json:"ok"`
						Data string `json:"data"`
					}
					if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || !envelope.OK || envelope.Data != specHelp(&spec) {
						t.Fatalf("JSON help contract: %s %v", stdout.String(), err)
					}
				}
			}
		})
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help created runtime directories: %v %v", entries, err)
	}
}

func TestLocalizedHelpSyntaxFailures(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(name, filepath.Join(root, "absent", name))
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"help", "missing"}, "未知帮助主题"},
		{[]string{"missing"}, "未知命令"},
		{[]string{"profile", "use"}, "查找配置方案名称"},
		{[]string{"proxy", "show"}, "查找组名"},
		{[]string{"proxy"}, "缺少子命令"},
		{[]string{"rewrite", "rules", "show"}, "查看就绪状态和恢复指引"},
		{[]string{"request-rewrite", "capture", "show"}, "查看就绪状态和恢复指引"},
	}
	for _, item := range cases {
		var englishMessage, originalJSON string
		for _, lang := range []string{"en", "zh-Hans"} {
			args := append([]string{"--lang", lang}, item.args...)
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr, "test", "commit"); code != 2 || stdout.Len() != 0 {
				t.Fatalf("%v: code %d stdout %s stderr %s", args, code, stdout.String(), stderr.String())
			}
			if lang == "zh-Hans" && !strings.Contains(stderr.String(), item.want) {
				t.Fatalf("untranslated failure %v: %s", args, stderr.String())
			}
			stdout.Reset()
			stderr.Reset()
			if code := Run(append([]string{"--json"}, args...), &stdout, &stderr, "test", "commit"); code != 2 || stdout.Len() != 0 {
				t.Fatalf("JSON %v: %d %s", args, code, stderr.String())
			}
			if originalJSON == "" {
				originalJSON = stderr.String()
			} else if originalJSON != stderr.String() {
				t.Fatalf("JSON error metadata changed with language: %s", stderr.String())
			}
			var envelope struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil || envelope.Error.Code != "usage" {
				t.Fatalf("JSON error: %s %v", stderr.String(), err)
			}
			if englishMessage == "" {
				englishMessage = envelope.Error.Message
			} else if envelope.Error.Message != englishMessage {
				t.Fatalf("JSON English error changed: %s", stderr.String())
			}
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("usage created runtime directories: %v %v", entries, err)
	}
}

func TestTopHelpLanguageSyntaxAndCommandIdentity(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"--lang=" + lang, "--help"}, &stdout, &stderr, "test", "commit"); code != 0 || stderr.Len() != 0 {
			t.Fatalf("top help: %d %s", code, stderr.String())
		}
		text := stdout.String()
		for _, want := range []string{globalHelpSyntax, "--lang=zh-Hans", "NAGI_LANG", "LC_ALL", "LC_MESSAGES", "LANG", "JSON"} {
			if !strings.Contains(text, want) {
				t.Fatalf("top help lacks %s: %s", want, text)
			}
		}
		for _, spec := range commandSpecs {
			if !strings.Contains(spec.path, " ") && !strings.Contains(text, spec.syntax) {
				t.Fatalf("missing stable command %s", spec.syntax)
			}
		}
		if lang == "zh-Hans" && (!strings.Contains(text, "命令：") || !strings.Contains(text, "管理本地 mihomo")) {
			t.Fatalf("untranslated top help: %s", text)
		}
	}
}

func TestMalformedLanguageHelpDoesNotResolveRuntime(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(name, filepath.Join(root, "absent", name))
	}
	t.Setenv("NAGI_LANG", "zh-Hans")
	for _, args := range [][]string{{"--lang"}, {"--lang="}, {"--lang=fr", "--help"}, {"--lang=en", "--lang=zh-Hans", "help"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "test", "commit"); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("malformed language %v: %d %s %s", args, code, stdout.String(), stderr.String())
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("language errors created runtime directories: %v %v", entries, err)
	}
}

func TestSyntaxHintDescriptorRetainsSourceIdentity(t *testing.T) {
	err := validateInvocation([]string{"profile", "use"})
	message, ok := err.(*i18n.Message)
	if !ok {
		t.Fatalf("usage lost typed source: %T", err)
	}
	descriptor := message.Descriptor()
	if descriptor.ID != "help.usage.cli.error.validation.missing_argument.profile.use" {
		t.Fatalf("unexpected usage identity: %s", descriptor.ID)
	}
	if _, exists := descriptor.Args["hint"]; exists {
		t.Fatal("localized guidance leaked into descriptor args")
	}
	if descriptor.Args["syntax"] != "profile use NAME" || descriptor.Args["path"] != "profile use" {
		t.Fatalf("syntax identity changed: %#v", descriptor.Args)
	}
	if !strings.Contains(i18n.New(i18n.SimplifiedChinese).Render(message), "查找配置方案名称") {
		t.Fatal("typed hint did not render")
	}
}
