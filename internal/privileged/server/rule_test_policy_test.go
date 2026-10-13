package server

import (
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestRuleTestControlPolicy(t *testing.T) {
	for _, body := range []string{`{"target":"154.19.187.117"}`, `{"target":"example.com","ip":"192.0.2.1","port":80,"network":"udp","process":"Browser"}`} {
		req := privileged.Request{Method: "POST", Path: "/rules/test", Body: []byte(body)}
		if err := validateControl(req); err != nil {
			t.Fatalf("rejected: %v", err)
		}
		if snapshotControl(req) {
			t.Fatal("DNS-capable test should retain the helper's serial lock")
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/rules/test", `{"target":"example.com"}`},
		{"POST", "/rules/test?force=true", `{"target":"example.com"}`},
		{"POST", "/rules/test", `{"target":"https://example.com"}`},
		{"POST", "/rules/test", `{"target":"example.com","ip":"invalid"}`},
		{"POST", "/rules/test", `{"target":"example.com","port":65536}`},
		{"POST", "/rules/test", `{"target":"example.com","specialProxy":"DIRECT"}`},
		{"POST", "/rules/test", `{"target":"example.com"}{}`},
		{"POST", "/rules/test", `{"target":"example.com"}` + strings.Repeat(" ", 4096)},
	} {
		if err := validateControl(privileged.Request{Method: tc.method, Path: tc.path, Body: []byte(tc.body)}); err == nil {
			t.Fatalf("accepted: %s %s", tc.method, tc.path)
		}
	}
}
