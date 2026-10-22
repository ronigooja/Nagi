package sensitive

import (
	"strings"
	"testing"
)

func TestRedactURLCredentialsPreservesUsefulDetails(t *testing.T) {
	got := RedactURLCredentials("request https://user:password@example.test/private/feed?region=west&access_token=secret failed")
	for _, want := range []string{"https://example.test/private/feed", "region=west", "access_token=%5Bredacted%5D", "failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
	for _, secret := range []string{"user", "password", "secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q leaked in %q", secret, got)
		}
	}
}

func TestRedactURLCredentialsMasksCommonParameterNames(t *testing.T) {
	got := RedactURLCredentials("https://example.test/path?key=k&auth=a&jwt=j&sig=s&accessToken=at&apiKey=ak&clientSecret=cs&region=west#access_token=f")
	for _, secret := range []string{"key=k", "auth=a", "jwt=j", "sig=s", "accessToken=at", "apiKey=ak", "clientSecret=cs", "access_token=f"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q leaked in %q", secret, got)
		}
	}
	if !strings.Contains(got, "region=west") {
		t.Fatalf("benign query was lost: %q", got)
	}
}

func TestRedactURLDetailsRemovesShareSensitiveParts(t *testing.T) {
	got := RedactURLDetails("failed https://user:pass@example.test/sub/private-token?jwt=secret#access_token=private")
	if got != "failed https://example.test" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactURLDetailsFailsClosedOnMalformedURL(t *testing.T) {
	got := RedactURLDetails("https://user:pass@example.test:badport/private?token=secret")
	if strings.Contains(got, "user") || strings.Contains(got, "private") || strings.Contains(got, "token") || got != "[redacted URL]" {
		t.Fatalf("malformed URL was not fully removed: %q", got)
	}
}
