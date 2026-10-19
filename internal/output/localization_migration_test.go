package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/proxy"
)

func TestLocalizedOutputPreservesRawAndResourceNames(t *testing.T) {
	zh := i18n.New(i18n.SimplifiedChinese)
	for _, data := range []any{Raw("original: Running\nsecret: fictional\n"), Candidates{"Running", "DIRECT", "代理组"}} {
		var en, localized bytes.Buffer
		if err := Write(&en, false, data); err != nil {
			t.Fatal(err)
		}
		if err := WriteLocalized(&localized, false, zh, data); err != nil {
			t.Fatal(err)
		}
		if en.String() != localized.String() {
			t.Fatalf("raw changed: %q %q", en.String(), localized.String())
		}
	}
	var out bytes.Buffer
	if err := WriteLocalized(&out, false, zh, proxy.Group{Name: "Running", All: []string{"Stopped", "DIRECT"}, Now: "Stopped"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Running -> Stopped") || !strings.Contains(out.String(), "* Stopped") || !strings.Contains(out.String(), "DIRECT") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := WriteLocalized(&out, false, zh, map[string]any{"profiles": []string{}, "current": ""}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "未找到配置方案") {
		t.Fatal(out.String())
	}
}

func TestLocalizedStructuredJSONIsIdentical(t *testing.T) {
	values := []any{map[string]any{"running": false, "profile": "Running"}, proxy.Group{Name: "Running", Now: "DIRECT", All: []string{"DIRECT"}}, Raw("raw\n")}
	for _, data := range values {
		var en, zh bytes.Buffer
		WriteLocalized(&en, true, i18n.New(i18n.English), data)
		WriteLocalized(&zh, true, i18n.New(i18n.SimplifiedChinese), data)
		if en.String() != zh.String() {
			t.Fatalf("JSON changed: %s %s", &en, &zh)
		}
	}
}

func TestJoinedErrorsKeepFullFallback(t *testing.T) {
	first := i18n.NewMessage("cli.language.missing", nil, "--lang requires a language; use en or zh-Hans.", nil)
	joined := errors.Join(first, errors.New("external recovery detail"))
	var out bytes.Buffer
	WriteErrorCause(&out, true, i18n.New(i18n.SimplifiedChinese), "usage", joined)
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Message != joined.Error() || len(env.Messages) != 0 {
		t.Fatal(out.String())
	}
	out.Reset()
	WriteErrorCause(&out, false, i18n.New(i18n.SimplifiedChinese), "usage", joined)
	if !strings.Contains(out.String(), "--lang 需要语言") || !strings.Contains(out.String(), "external recovery detail") {
		t.Fatal(out.String())
	}
	wrapped := fmt.Errorf("operation context: %w", first)
	out.Reset()
	WriteErrorCause(&out, true, i18n.New(i18n.SimplifiedChinese), "usage", wrapped)
	if !strings.Contains(out.String(), "operation context:") {
		t.Fatal(out.String())
	}
}

func TestConcurrentHumanOutputLanguages(t *testing.T) {
	var group sync.WaitGroup
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		group.Add(1)
		go func(lang i18n.Language) {
			defer group.Done()
			for i := 0; i < 20; i++ {
				var out bytes.Buffer
				WriteLocalized(&out, false, i18n.New(lang), map[string]any{"closed": true})
				want := "Closed all connections."
				if lang == i18n.SimplifiedChinese {
					want = "已关闭所有连接。"
				}
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s: %s", lang, out.String())
				}
			}
		}(language)
	}
	group.Wait()
}
