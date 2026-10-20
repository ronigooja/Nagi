package output

import (
	"bytes"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/profile"
	"strings"
	"testing"
)

func TestProxyNodeChainHumanAndJSON(t *testing.T) {
	for _, tc := range []struct {
		data map[string]any
		want string
	}{
		{map[string]any{"nodes": []profile.ProxyNode{}}, "No eligible top-level"},
		{map[string]any{"node": profile.ProxyNode{Name: "Tail", Type: "tailscale", Tailscale: &profile.TailscaleNode{ExitNode: "exit", AuthKeyConfigured: true}}}, "Exit node: exit"},
		{map[string]any{"chains": []profile.ProxyChainTarget{}}, "No eligible chain targets"},
		{map[string]any{"chain": profile.ProxyChainTarget{Kind: "node", Name: "Target", Via: []string{"First", "Second"}}}, "First -> Second -> Target"},
		{map[string]any{"plan": profile.ProxyChainPlan{Kind: "node", Targets: []string{"Target"}, Via: []string{"First"}, Conflicts: []string{"cycle"}}, "dry_run": true}, "Conflict: cycle"},
		{map[string]any{"profile": "work", "resource": "node", "action": "add", "saved": true, "reloaded": false}, "mihomo is stopped"},
	} {
		var out bytes.Buffer
		if err := Write(&out, false, tc.data); err != nil || !strings.Contains(out.String(), tc.want) {
			t.Fatalf("%v %s", err, &out)
		}
		out.Reset()
		if err := Write(&out, true, tc.data); err != nil || !strings.Contains(out.String(), `"ok":true`) {
			t.Fatalf("%v %s", err, &out)
		}
	}
	var out bytes.Buffer
	if err := WriteLocalized(&out, false, i18n.New(i18n.SimplifiedChinese), map[string]any{"nodes": []profile.ProxyNode{}}); err != nil || !strings.Contains(out.String(), "没有符合条件") {
		t.Fatalf("%v %s", err, &out)
	}
}
