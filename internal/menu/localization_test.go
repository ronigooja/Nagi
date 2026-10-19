package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestBilingualMenuNavigationAndImmutableSessions(t *testing.T) {
	var wg sync.WaitGroup
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		for range 8 {
			wg.Add(1)
			go func(language i18n.Language) {
				defer wg.Done()
				runner := &fakeRunner{results: map[string]Result{"status": {Data: map[string]any{"running": true, "profile": "running"}}}}
				var out bytes.Buffer
				session := NewSessionWithLanguage(runner, strings.NewReader("2\n1\n1\n0\n0\n0\n0\n"), &out, i18n.New(language))
				if code := Run(context.Background(), session); code != 0 {
					t.Errorf("exit=%d", code)
				}
				expected := "Nagi menu"
				mode := "Rule — follow routing rules"
				if language == i18n.SimplifiedChinese {
					expected = "Nagi 菜单"
					mode = "规则 — 遵循路由规则"
				}
				if !strings.Contains(out.String(), expected) || !strings.Contains(out.String(), mode) {
					t.Errorf("%s: %s", language, out.String())
				}
				if !strings.Contains(out.String(), "running") {
					t.Error("user profile translated")
				}
			}(language)
		}
	}
	wg.Wait()
}

func TestBilingualRetainedFormProtocolChoicesAndCancellation(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		t.Run(string(language), func(t *testing.T) {
			var out bytes.Buffer
			s := NewSessionWithLanguage(&fakeRunner{}, strings.NewReader("literal 0\n2\n1\n\n3\n"), &out, i18n.New(language))
			attempts := 0
			_, err := s.Form(context.Background(), []Field{{Key: "name", Label: "Name", Required: true}, {Key: "flag", Label: "Flag", Choices: []string{"true", "false"}, ChoiceLabels: []string{menuState(s.language, "true"), menuState(s.language, "false")}}}, nil, func(values map[string]string) Result {
				attempts++
				if values["name"] != "literal 0" || values["flag"] != "false" {
					t.Errorf("business values changed: %v", values)
				}
				if attempts == 1 {
					return Result{Code: 1, Error: &CLIError{Message: "external diagnostic"}}
				}
				return Result{}
			})
			if err != nil || attempts != 2 {
				t.Fatalf("attempts=%d err=%v output=%s", attempts, err, out.String())
			}
			s = NewSessionWithLanguage(&fakeRunner{}, strings.NewReader("0\n"), &out, i18n.New(language))
			if _, err = s.selectProtocol("Protocol", []string{"true", "false"}); err != ErrBack {
				t.Fatalf("cancel=%v", err)
			}
		})
	}
}

func TestMetadataEveryReadAndInputPathAndMalformedFallback(t *testing.T) {
	ctx := context.Background()
	for _, metadata := range []string{
		`{"/error/message":{"id":"menu.latency.failed"}}`,
		`{"/error/message":{"id":"unknown"}}`,
		`{"/error/message":{"id":"menu.latency.failed","args":{"unexpected":null}}}`,
		`{"/error/message":42}`,
		`"malformed"`,
		`{"/error/message":{"id":"menu.latency.failed","args":null}}`,
		`{"/error/message":{"id":"menu.latency.failed","args":[]}}`,
	} {
		runner := NewInputCommandRunner(func(argv []string, in io.Reader, out, errOut io.Writer) int {
			if strings.Join(argv[:2], " ") != "--lang zh-Hans" {
				t.Errorf("missing explicit session locale %v", argv)
			}
			io.WriteString(errOut, `{"ok":false,"error":{"code":"failed","message":"English fallback"},"messages":`+metadata+`}`)
			return 1
		})
		s := NewSessionWithLanguage(runner, strings.NewReader(""), io.Discard, i18n.New(i18n.SimplifiedChinese))
		results := s.fetchReads(ctx, [][]string{{"a"}, {"b"}})
		results = append(results, s.runner.(InputRunner).RunInput(ctx, []string{"input"}, strings.NewReader("private")))
		for _, result := range results {
			expected := "English fallback"
			if metadata == `{"/error/message":{"id":"menu.latency.failed"}}` {
				expected = "延迟测试失败"
			}
			if got := result.errorText(s.language); got != expected {
				t.Errorf("metadata %s: got %q want %q", metadata, got, expected)
			}
		}
	}
}

func TestMetadataPresentationPreservesBusinessAndRawOutput(t *testing.T) {
	descriptor, _ := json.Marshal(i18n.Descriptor{ID: "menu.latency.failed"})
	result := Result{Data: map[string]any{"message": "Original English", "profile": "running", "status": "failed", "node_delays": map[string]any{"running": map[string]any{"status": "failed"}}}, Messages: map[string]json.RawMessage{"/data/message": descriptor}}
	var out bytes.Buffer
	s := NewSessionWithLanguage(&fakeRunner{}, strings.NewReader(""), &out, i18n.New(i18n.SimplifiedChinese))
	s.styles = true
	s.ShowResult([]string{"status"}, result)
	if !strings.Contains(out.String(), "延迟测试失败") || !strings.Contains(out.String(), "running") {
		t.Fatalf("output=%s", out.String())
	}
	if result.Data.(map[string]any)["message"] != "Original English" {
		t.Error("business data mutated")
	}
	if !strings.Contains(out.String(), "\x1b[31m失败\x1b[0m") {
		t.Errorf("semantic state style: %q", out.String())
	}
	for _, args := range [][]string{{"config", "show"}, {"logs"}, {"profile", "override", "show", "work"}, {"profile", "diff", "work"}} {
		out.Reset()
		raw := "running https://private.example/path\n原文"
		s.ShowResult(args, Result{Data: raw, Messages: map[string]json.RawMessage{"/data": descriptor}})
		if out.String() != raw+"\n" {
			t.Errorf("raw changed for %v: %q", args, out.String())
		}
	}
}

func TestLocalizedRootSemanticStyleAndLiteralName(t *testing.T) {
	var out bytes.Buffer
	s := NewSessionWithLanguage(&fakeRunner{}, strings.NewReader(""), &out, i18n.New(i18n.SimplifiedChinese))
	s.styles = true
	page := localizedRootPage(s.language)
	s.render(page, PageView{Items: []Item{{Label: "running"}}}, 0, 1)
	if !strings.Contains(out.String(), "\x1b[1;96mNagi 菜单\x1b[0m") {
		t.Fatalf("root style=%q", out.String())
	}
	if strings.Contains(out.String(), "运行中") {
		t.Fatal("literal user name translated")
	}
	out.Reset()
	s.WriteLine("running")
	if out.String() != "running\n" {
		t.Fatal("raw text styled")
	}
}

func TestMissingOriginalErrorTargetIgnoresOptionalDescriptor(t *testing.T) {
	for _, raw := range []string{
		`{"ok":false,"messages":{"/error/message":{"id":"cli.language.missing"}}}`,
		`{"ok":false,"error":{"code":"failed"},"messages":{"/error/message":{"id":"cli.language.missing"}}}`,
		`{"ok":false,"error":{"code":"failed","message":null},"messages":{"/error/message":{"id":"cli.language.missing"}}}`,
	} {
		runner := NewCommandRunner(func(_ []string, _ io.Writer, stderr io.Writer) int { io.WriteString(stderr, raw); return 1 })
		result := runner.Run(context.Background(), []string{"status"})
		if _, exists := result.Messages["/error/message"]; exists {
			t.Fatalf("metadata annotated absent original target: %s", raw)
		}
		if result.Error == nil {
			t.Fatal("missing operation error")
		}
	}
}
