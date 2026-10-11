package server

import (
	"context"
	"github.com/ronigooja/Nagi/internal/privileged"
	"testing"
)

func TestDNSOperationsRefuseOrphanedChild(t *testing.T) {
	h := &RuntimeHandler{PrivateDir: t.TempDir(), orphaned: true}
	for _, op := range []privileged.Operation{privileged.TUNDNSSync, privileged.TUNDNSRestore, privileged.TUNDNSStatus} {
		if _, err := h.Handle(context.Background(), 501, privileged.Request{Operation: op}); err == nil {
			t.Fatalf("orphan operation %s accepted", op)
		}
	}
	if err := h.Close(); err == nil {
		t.Fatal("orphaned Close accepted")
	}
}

func TestTUNPatchDNSAllowlist(t *testing.T) {
	for _, raw := range []string{`{"tun":{"enable":true,"auto-route":true,"auto-detect-interface":true,"strict-route":true,"dns-hijack":["any:53","tcp://any:53"]}}`, `{"tun":{"enable":false,"strict-route":false,"dns-hijack":[]}}`, `{"tun":{"enable":true,"dns-hijack":["udp://0.0.0.0:53","tcp://[::]:53"]}}`} {
		if err := validateConfigPatch([]byte(raw)); err != nil {
			t.Fatalf("rejected valid patch %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"tun":{"enable":true,"device":"en0"}}`, `{"tun":{"enable":true,"inet4-address":["1.1.1.1/30"]}}`, `{"tun":{"enable":true,"dns-hijack":["example.com:53"]}}`, `{"tun":{"enable":true,"dns-hijack":["any:443"]}}`, `{"tun":{"enable":true,"dns-hijack":["http://1.1.1.1:53"]}}`, `{"tun":{"enable":true,"dns-hijack":true}}`} {
		if err := validateConfigPatch([]byte(raw)); err == nil {
			t.Fatalf("accepted unsafe patch %s", raw)
		}
	}
}
