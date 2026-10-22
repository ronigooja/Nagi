package menu

import (
	"bytes"
	"strings"
	"testing"
)

func TestMenuErrorPreservesURLContextAndRemovesCredentials(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	s.ShowResult([]string{"subscription", "update", "work"}, Result{Code: 1, Error: &CLIError{
		Message: "fetch https://user:pass@example.invalid/private-feed?accessToken=query-secret&region=west#auth=fragment-secret failed",
	}})

	got := out.String()
	for _, want := range []string{"https://example.invalid/private-feed", "region=west"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in menu error %q", want, got)
		}
	}
	for _, secret := range []string{"user:pass", "accessToken=query-secret", "auth=fragment-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("menu error exposed %q: %q", secret, got)
		}
	}
}

func TestMenuRequestedRawConfigPreservesCredentials(t *testing.T) {
	raw := "proxy-providers:\n  feed:\n    url: https://user:pass@example.invalid/private-feed?token=raw-secret\n"
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	s.ShowResult([]string{"config", "show"}, Result{Data: map[string]any{"content": raw}})
	if strings.TrimRight(out.String(), "\n") != strings.TrimRight(raw, "\n") {
		t.Fatalf("explicit raw configuration changed: got %q want %q", out.String(), raw)
	}
}
