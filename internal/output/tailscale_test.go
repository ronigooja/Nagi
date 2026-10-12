package output

import (
	"bytes"
	"github.com/ronigooja/Nagi/internal/profile"
	"strings"
	"testing"
)

func TestTailscaleHumanResults(t *testing.T) {
	for _, test := range []struct {
		data map[string]any
		want string
	}{
		{map[string]any{"profile": "work", "nodes": []profile.TailscaleNode{}}, "No Tailscale nodes"},
		{map[string]any{"profile": "work", "nodes": []profile.TailscaleNode{{Name: "Tail", ExitNode: "exit", AuthKeyConfigured: true}}}, "Tail: exit node exit"},
		{map[string]any{"profile": "work", "node": profile.TailscaleNode{Name: "Tail"}, "bindings": []profile.TailscaleBinding{}}, "No targets bound"},
		{map[string]any{"profile": "work", "targets": []profile.TailscaleTarget{}}, "No eligible proxy nodes"},
		{map[string]any{"profile": "work", "targets": []profile.TailscaleTarget{{Kind: "node", Name: "Proxy", Type: "socks5"}}}, "dialer proxy none"},
		{map[string]any{"profile": "work", "action": "add", "name": "Tail", "saved": true, "reloaded": false}, "mihomo is stopped"},
		{map[string]any{"profile": "work", "action": "bind", "name": "Tail", "kind": "node", "target": "Proxy", "saved": true, "reloaded": true}, "Saved and reloaded"},
		{map[string]any{"profile": "work", "action": "bind", "name": "Tail", "kind": "provider", "targets": []string{"first", "second"}, "saved": true, "reloaded": false}, "Tailscale bind: 2 provider targets in profile work."},
	} {
		var out bytes.Buffer
		if err := Write(&out, false, test.data); err != nil || !strings.Contains(out.String(), test.want) || strings.Contains(out.String(), `"profile"`) {
			t.Fatalf("human result %s: %v %s", test.want, err, out.String())
		}
	}
}
