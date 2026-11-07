package server

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestLANConfigPatchWithTUN(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, address := range []string{"127.0.0.1", "::1", "localhost", "*", "0.0.0.0", "192.0.2.10", "::", "2001:db8::10"} {
			t.Run(fmt.Sprintf("%t/%s", enabled, address), func(t *testing.T) {
				body := fmt.Sprintf(`{"allow-lan":%t,"bind-address":%q,"tun":{"enable":true,"auto-route":true,"strict-route":true,"dns-hijack":["any:53","tcp://any:53"]}}`, enabled, address)
				if err := validateControl(privileged.Request{Method: http.MethodPatch, Path: "/configs", Body: []byte(body)}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	for _, body := range []string{`{"allow-lan":true}`, `{"allow-lan":false}`, `{"bind-address":"*"}`} {
		if err := validateConfigPatch([]byte(body)); err != nil {
			t.Fatalf("partial LAN patch %s: %v", body, err)
		}
	}
}

func TestLANConfigPatchRejectsInvalidAndUnsafeFields(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"allow-lan":null}`, `{"allow-lan":1}`, `{"allow-lan":"true"}`,
		`{"allow-lan":[]}`, `{"allow-lan":{}}`, `{"bind-address":null}`, `{"bind-address":true}`,
		`{"bind-address":""}`, `{"bind-address":"proxy.example.test"}`, `{"bind-address":"localhost.example.test"}`,
		`{"bind-address":"fe80::1%en0"}`, `{"bind-address":"[::1]"}`, `{"bind-address":"127.0.0.1:7890"}`,
		`{"bind-address":"192.0.2.1/24"}`, `{"bind-address":" * "}`, `{"allow-lan":true} {}`,
		`{"allow-lan":true,"external-controller":"0.0.0.0:9090"}`,
		`{"allow-lan":true,"external-controller-unix":"/tmp/controller.sock"}`,
		`{"allow-lan":true,"external-controller-tls":"0.0.0.0:9090"}`,
		`{"allow-lan":true,"dns":{"listen":"0.0.0.0:53"}}`,
		`{"allow-lan":true,"listeners":[{"type":"mixed","listen":"0.0.0.0","port":17890}]}`,
		`{"allow-lan":true,"mixed-port":80}`, `{"allow-lan":true,"unknown":true}`,
		`{"allow-lan":true,"tun":{"enable":true,"device":"en0"}}`,
		`{"allow-lan":true,"tun":{"enable":true,"dns-hijack":["any:443"]}}`,
		`{"allow-lan":true,"tun":{"enable":true,"dns-hijack":["resolver.example.test:53"]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			if err := validateControl(privileged.Request{Method: http.MethodPatch, Path: "/configs", Body: []byte(body)}); err == nil {
				t.Fatal("invalid or unsafe LAN patch accepted")
			}
		})
	}
}
