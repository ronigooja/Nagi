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

func TestChainListsFollowAppliedSubscriptionOrder(t *testing.T) {
	config := nodeChainFixture(t)
	source := []byte(`proxies:
 - {name: Z-source, type: socks5}
 - {name: A-source, type: vmess}
 - {name: M-source, type: socks5}
proxy-groups: []
`)
	sourcePath := filepath.Join(config, "profiles", "default.yaml")
	if err := os.WriteFile(sourcePath, source, 0600); err != nil {
		t.Fatal(err)
	}
	subs := subscription.NewStore(config, filepath.Join(t.TempDir(), "cache"), nil)
	if err := subs.Add("feed", "https://example.invalid/feed"); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("feed", "default", "applied", source); err != nil {
		t.Fatal(err)
	}
	// A local snapshot must not override the subscription's display order.
	if err := os.MkdirAll(filepath.Join(config, "overrides"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "overrides", "default.yaml"), []byte(`proxies:
 - {name: M-source, type: socks5}
 - {name: B-local, type: socks5}
 - {name: A-source, type: vmess}
 - {name: Z-source, type: socks5}
`), 0600); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		args  []string
		field string
		want  []string
	}{
		{[]string{"proxy", "chain", "list"}, "chains", []string{"Z-source", "A-source", "M-source", "B-local"}},
		{[]string{"proxy", "chain", "candidates", "node", "A-source"}, "nodes", []string{"Z-source", "M-source", "B-local"}},
		{[]string{"proxy", "chain", "candidates", "provider", "Feed"}, "nodes", []string{"Z-source", "A-source", "M-source", "B-local"}},
		{[]string{"proxy", "chain", "candidates", "subscription", "feed"}, "nodes", []string{"Z-source", "A-source", "M-source", "B-local"}},
	}
	for _, check := range checks {
		result := runNodeChainJSON(t, check.args...)
		names := []string{}
		for _, value := range result[check.field].([]any) {
			node := value.(map[string]any)
			if check.field == "chains" && node["kind"] != "node" {
				continue
			}
			names = append(names, node["name"].(string))
		}
		if !reflect.DeepEqual(names, check.want) {
			t.Fatalf("%v: names=%v want=%v", check.args, names, check.want)
		}
	}
	// An external source edit invalidates ownership and restores ordinary name sorting.
	if err := os.WriteFile(sourcePath, append(source, []byte("mode: global\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	result := runNodeChainJSON(t, "proxy", "chain", "candidates", "node", "M-source")
	names := []string{}
	for _, value := range result["nodes"].([]any) {
		names = append(names, value.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"A-source", "B-local", "Z-source"}) {
		t.Fatalf("stale association order: %v", names)
	}
}
