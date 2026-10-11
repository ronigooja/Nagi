package menu

import (
	"bytes"
	"strings"
	"testing"
)

func TestSubscriptionResultIsConciseWithoutPreview(t *testing.T) {
	for _, test := range []struct{ operation, feedback string }{
		{"update", "Cache downloaded."},
		{"apply", "Cached subscription applied."},
	} {
		var out bytes.Buffer
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
		names := make([]string, 300)
		for i := range names {
			names[i] = "secret-node-name"
		}
		s.ShowResult([]string{"subscription", test.operation, "work"}, Result{Data: map[string]any{"preview": map[string]any{"added": names}, "name": "work"}})
		if !strings.Contains(out.String(), test.feedback) || strings.Contains(out.String(), "secret-node-name") || strings.Contains(out.String(), "preview") || strings.Contains(out.String(), "name:") {
			t.Fatalf("%s result: %s", test.operation, out.String())
		}
	}
}

func TestRequestedConfigurationAndLogsKeepRawContent(t *testing.T) {
	for _, test := range []struct {
		args []string
		data any
		want string
	}{
		{[]string{"config", "show"}, map[string]any{"yaml": "url: https://example.invalid/config\n"}, "url: https://example.invalid/config\n"},
		{[]string{"profile", "override", "show", "work"}, map[string]any{"yaml": "mixed-port: 7890"}, "mixed-port: 7890\n"},
		{[]string{"logs", "25"}, map[string]any{"lines": []string{"first", "second https://example.invalid/log"}}, "first\nsecond https://example.invalid/log\n"},
	} {
		var out bytes.Buffer
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
		s.ShowResult(test.args, Result{Data: test.data})
		if strings.TrimRight(out.String(), "\n") != strings.TrimRight(test.want, "\n") {
			t.Fatalf("%v: %q", test.args, out.String())
		}
	}
}
