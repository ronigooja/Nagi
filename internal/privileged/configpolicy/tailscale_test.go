package configpolicy

import "testing"

func TestTailscaleAllowedOnlyAsTopLevelProxy(t *testing.T) {
	if err := Validate([]byte("proxies: [{name: Tail, type: tailscale, exit-node: exit, auth-key: secret, state-dir: /user/state}]")); err != nil {
		t.Fatal("top-level TS rejected", err)
	}
	if err := Validate([]byte("proxy-providers: {local: {type: inline, payload: [{name: Tail, type: tailscale}]}}")); err == nil {
		t.Fatal("provider TS accepted")
	}
}
