package command

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestNodeListOfflineSourceOrderJSON(t *testing.T) {
	config := nodeChainFixture(t)
	source := []byte(`proxies:
 - {name: Z-sub, type: vmess}
 - {name: A-sub, type: socks5}
proxy-providers:
 Z-provider: {type: inline, payload: []}
 A-provider: {type: inline, payload: []}
proxy-groups: []
`)
	if err := os.WriteFile(filepath.Join(config, "profiles", "default.yaml"), source, 0600); err != nil {
		t.Fatal(err)
	}
	subs := subscription.NewStore(config, filepath.Join(t.TempDir(), "cache"), nil)
	if err := subs.Add("feed", "https://example.invalid/feed"); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("feed", "default", "applied", source); err != nil {
		t.Fatal(err)
	}
	result := runNodeChainJSON(t, "proxy", "node", "list")
	nodes := result["nodes"].([]any)
	if len(nodes) != 2 || nodes[0].(map[string]any)["name"] != "Z-sub" || nodes[1].(map[string]any)["name"] != "A-sub" || nodes[0].(map[string]any)["subscription"] != "feed" {
		t.Fatal(result)
	}
	if !reflect.DeepEqual(result["provider_candidates"], []any{"Z-provider", "A-provider"}) {
		t.Fatal(result)
	}
}
