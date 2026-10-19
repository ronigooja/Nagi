package i18n

import (
	"errors"
	"sync"
	"testing"
)

func TestResolve(t *testing.T) {
	for _, test := range []struct {
		explicit string
		env      map[string]string
		want     Language
		bad      bool
	}{
		{"zh-CN", map[string]string{"NAGI_LANG": "en"}, SimplifiedChinese, false},
		{"", map[string]string{"NAGI_LANG": "", "LC_ALL": "zh_SG.UTF-8", "LANG": "en"}, SimplifiedChinese, false},
		{"", map[string]string{"LC_MESSAGES": "zh-Hans", "LANG": "en"}, SimplifiedChinese, false},
		{"", map[string]string{"LC_ALL": "zh_TW.UTF-8", "LANG": "zh-CN"}, English, false},
		{"", map[string]string{"LANG": "POSIX"}, English, false},
		{"", nil, English, false}, {"fr", nil, English, true},
	} {
		got, err := Resolve(test.explicit, func(k string) string { return test.env[k] })
		if got != test.want || (err != nil) != test.bad {
			t.Fatalf("%+v: %s %v", test, got, err)
		}
	}
	for _, alias := range []string{"zh", "zh-Hans", "zh_CN.UTF-8", "zh_SG.UTF-8"} {
		if lang, ok := Normalize(alias); !ok || lang != SimplifiedChinese {
			t.Fatal(alias)
		}
	}
}
func TestMessageFallbackAndImmutability(t *testing.T) {
	cause := errors.New("cause")
	args := map[string]any{"value": "fr"}
	message := NewMessage("cli.language.invalid", args, "English fallback", cause)
	args["value"] = "changed"
	descriptor := message.Descriptor()
	descriptor.Args["value"] = "also changed"
	if !errors.Is(message, cause) || message.Error() != "English fallback" {
		t.Fatal(message)
	}
	if got := New(SimplifiedChinese).Render(message); got != "不支持语言 fr；请使用 en 或 zh-Hans。" {
		t.Fatal(got)
	}
	for _, d := range []Descriptor{{ID: "missing"}, {ID: "cli.language.invalid"}, {ID: "cli.language.invalid", Args: map[string]any{"value": 1}}, {ID: "cli.language.invalid", Args: map[string]any{"value": "fr", "extra": true}}} {
		if got := New(SimplifiedChinese).RenderDescriptor(d, "fallback"); got != "fallback" {
			t.Fatal(got)
		}
	}
}
func TestPluralForms(t *testing.T) {
	en := entry{forms: map[string]string{"one": "{count} item", "other": "{count} items"}}
	zh := entry{forms: map[string]string{"other": "{count} 项"}}
	for _, count := range []int{0, 1, 2} {
		args := map[string]any{"count": count}
		want := "{count} items"
		if count == 1 {
			want = "{count} item"
		}
		if en.text(args) != want || zh.text(args) != "{count} 项" {
			t.Fatal(count)
		}
	}
}
func TestIndependentContexts(t *testing.T) {
	var group sync.WaitGroup
	for _, lang := range []Language{English, SimplifiedChinese} {
		group.Add(1)
		go func(lang Language) {
			defer group.Done()
			ctx := New(lang)
			for i := 0; i < 100; i++ {
				if ctx.Language() != lang {
					t.Error("shared mutable language")
				}
				ctx.Render(NewMessage("cli.language.missing", nil, "fallback", nil))
			}
		}(lang)
	}
	group.Wait()
}
func TestCatalogParity(t *testing.T) {
	for id, en := range catalogs[English] {
		zh, ok := catalogs[SimplifiedChinese][id]
		if !ok {
			t.Fatal(id)
		}
		for _, item := range []entry{en, zh} {
			if item.literal == "" && item.forms["other"] == "" {
				t.Fatal(id)
			}
		}
	}
}
