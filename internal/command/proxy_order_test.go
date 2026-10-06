package command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type orderedGroupsClient struct{}

func (orderedGroupsClient) Get(_ context.Context, path string, out any) error {
	return json.Unmarshal([]byte(`{"proxies":{"First":{"type":"Selector","all":["A"],"now":"A"},"Second":{"type":"Selector","all":["B"],"now":"B"},"GLOBAL":{"type":"Selector","all":["DIRECT"],"now":"DIRECT"}}}`), out)
}

func (orderedGroupsClient) Put(context.Context, string, any, any) error { return nil }

func TestProxyGroupsFollowSelectedProfileOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proxy-groups:\n  - name: Second\n    type: select\n  - name: First\n    type: select\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	groups, err := orderedProxyService(orderedGroupsClient{}, dir).Groups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || groups[0].Name != "Second" || groups[1].Name != "First" || groups[2].Name != "GLOBAL" {
		t.Fatalf("group order: %+v", groups)
	}
}
