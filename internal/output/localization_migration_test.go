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
	"github.com/ronigooja/Nagi/internal/profile"
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
	values := []any{map[string]any{"running": false, "profile": "Running"}, proxy.Group{Name: "Running", Now: "DIRECT", All: []string{"DIRECT"}}, Raw("raw\n"), RewriteResult{"profile": nil, "generation": "", "enabled_intent": nil}}
	for _, data := range values {
		var en, zh bytes.Buffer
		WriteLocalized(&en, true, i18n.New(i18n.English), data)
		WriteLocalized(&zh, true, i18n.New(i18n.SimplifiedChinese), data)
		if en.String() != zh.String() {
			t.Fatalf("JSON changed: %s %s", &en, &zh)
		}
	}
}

func TestTechnicalPlaceholdersStayLiteralWhileStatesAreLocalized(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		t.Run(string(language), func(t *testing.T) {
			locale := i18n.New(language)
			var out bytes.Buffer
			data := RewriteResult{"actual": "running", "profile": nil, "generation": "", "path": nil, "fingerprint": "", "enabled_intent": nil}
			if err := WriteLocalized(&out, false, locale, data); err != nil {
				t.Fatal(err)
			}
			want := []string{"Profile: unknown", "Generation: unknown", "File: unknown", "Fingerprint: unknown", "Enabled intent: unknown", "Request rewrite: running"}
			if language == i18n.SimplifiedChinese {
				want = []string{"配置方案: unknown", "代次: unknown", "文件: unknown", "指纹: unknown", "启用意图: 未知", "请求改写：运行中"}
			}
			for _, line := range want {
				if !strings.Contains(out.String(), line) {
					t.Fatalf("missing %q in %s", line, &out)
				}
			}
			out.Reset()
			targets := map[string]any{"targets": []profile.TailscaleTarget{{Kind: "node", Name: "running", Type: "vless"}}}
			if err := WriteLocalized(&out, false, locale, targets); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "running") || !strings.Contains(out.String(), "none") || strings.Contains(out.String(), "无") {
				t.Fatalf("technical name or placeholder translated: %s", &out)
			}
		})
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

func TestRewriteProtocolPresentationPreservesIdentity(t *testing.T) {
	value := RewriteResult{"actual": "blocked", "state": "generated", "trust": "untrusted", "not_before": "2026-10-19T12:34:56Z", "rules": []any{map[string]any{"id": "request", "enabled": true, "stage": "request", "range_mode": "fragment", "on_error": "forward_original"}}}
	var human, machine bytes.Buffer
	locale := i18n.New(i18n.SimplifiedChinese)
	if err := WriteLocalized(&human, false, locale, value); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"受阻", "已生成", "未信任", "生效时间", "请求", "片段", "转发原始内容", "request"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("missing %q in %s", want, human.String())
		}
	}
	if strings.Contains(human.String(), ":56") {
		t.Fatal("human timestamp includes seconds")
	}
	if err := WriteLocalized(&machine, true, locale, value); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"actual":"blocked"`, `"id":"request"`, `"stage":"request"`, `2026-10-19T12:34:56Z`} {
		if !strings.Contains(machine.String(), want) {
			t.Fatalf("changed original JSON %q: %s", want, machine.String())
		}
	}
}
