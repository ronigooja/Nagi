package command

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
)

func TestLanguagePrefix(t *testing.T) {
	for _, test := range []struct {
		args     []string
		want     []string
		language i18n.Language
		id       string
	}{
		{[]string{"--json", "--lang", "zh-CN", "version"}, []string{"--json", "version"}, i18n.SimplifiedChinese, ""},
		{[]string{"--lang=en", "version", "--json"}, []string{"version", "--json"}, i18n.English, ""},
		{[]string{"--lang"}, nil, i18n.English, "cli.language.missing"},
		{[]string{"--lang="}, nil, i18n.English, "cli.language.missing"},
		{[]string{"--lang", "--json", "zh-Hans"}, nil, i18n.English, "cli.language.missing"},
		{[]string{"--lang", "fr"}, nil, i18n.English, "cli.language.invalid"},
		{[]string{"--lang=en", "--lang=zh"}, nil, i18n.English, "cli.language.duplicate"},
		{[]string{"profile", "use", "--lang=zh"}, []string{"profile", "use", "--lang=zh"}, i18n.English, ""},
	} {
		got, locale, err := parseLanguage(test.args, func(string) string { return "" })
		if test.id != "" {
			if err == nil || err.Descriptor().ID != test.id {
				t.Fatalf("%v: %v", test.args, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, test.want) || locale.Language() != test.language {
			t.Fatalf("%v: %v %v", test.args, got, err)
		}
	}
}
func TestLanguageInvocationJSONCompatibility(t *testing.T) {
	t.Setenv("NAGI_LANG", "zh-CN")
	var out, errout bytes.Buffer
	if code := Run([]string{"--json", "--lang=en", "--lang=zh"}, &out, &errout, "test", "test"); code != 2 {
		t.Fatal(code)
	}
	var env output.Envelope
	if err := json.Unmarshal(errout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "usage" || env.Error.Message != "--lang may be specified only once." || env.Messages["/error/message"].ID != "cli.language.duplicate" {
		t.Fatal(errout.String())
	}
	out.Reset()
	errout.Reset()
	if code := Run([]string{"--json", "--lang=zh", "version"}, &out, &errout, "test", "test"); code != 0 {
		t.Fatalf("%d %s", code, errout.String())
	}
	if !json.Valid(out.Bytes()) {
		t.Fatal(out.String())
	}
}
