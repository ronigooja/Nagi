package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPruneRetainsOnlyValidManualSelections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selections.json")
	store := SelectionStore{Path: path}
	for group, node := range map[string]string{"keep": "node-a", "remove-group": "node-b", "remove-node": "node-c", "provider": "dynamic-node"} {
		if err := store.Save("work", group, node); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Prune("work", map[string]map[string]bool{"keep": {"node-a": true}, "remove-node": {"node-d": true}, "provider": {"*": true}}); err != nil {
		t.Fatal(err)
	}
	all, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	if len(all["work"]) != 2 || all["work"]["keep"] != "node-a" || all["work"]["provider"] != "dynamic-node" {
		t.Fatalf("wrong pruned choices: %+v", all)
	}
}

func TestApplyDefaultsUsesSavedSelectorsWithoutChangingSource(t *testing.T) {
	store := SelectionStore{Path: filepath.Join(t.TempDir(), "selections.json")}
	for group, node := range map[string]string{"manual": "nested", "provider": "remote-node", "missing": "old", "automatic": "node-a"} {
		if err := store.Save("work", group, node); err != nil {
			t.Fatal(err)
		}
	}
	source := []byte("profile:\n  store-selected: true\nproxy-groups:\n  - name: manual\n    type: select\n    proxies: [DIRECT, nested]\n  - name: provider\n    type: select\n    use: [remote]\n  - name: automatic\n    type: url-test\n    proxies: [node-a]\n")
	result, changed, err := store.ApplyDefaults("work", source)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if strings.Contains(string(source), "default-selected") {
		t.Fatal("source was mutated")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(result, &doc); err != nil {
		t.Fatal(err)
	}
	groups := doc["proxy-groups"].([]any)
	if groups[0].(map[string]any)["default-selected"] != "nested" || groups[1].(map[string]any)["default-selected"] != "remote-node" {
		t.Fatalf("saved choices missing: %v", groups)
	}
	if _, ok := groups[2].(map[string]any)["default-selected"]; ok {
		t.Fatal("automatic group gained selector default")
	}
	if doc["profile"].(map[string]any)["store-selected"] != false {
		t.Fatal("mihomo cache still overrides saved choices")
	}
}

type failingSelectionClient struct{}

func (failingSelectionClient) Get(_ context.Context, _ string, out any) error {
	return json.Unmarshal([]byte(`{"proxies":{"A":{"name":"A","type":"Selector","all":["a","DIRECT"],"now":"DIRECT"},"B":{"name":"B","type":"Selector","all":["b","DIRECT"],"now":"DIRECT"}}}`), out)
}
func (failingSelectionClient) Put(_ context.Context, path string, _ any, _ any) error {
	if strings.Contains(path, "/A") {
		return errors.New("API refused choice")
	}
	return nil
}

func TestRestoreReportsFailedChoiceAndContinues(t *testing.T) {
	store := SelectionStore{Path: filepath.Join(t.TempDir(), "selections.json")}
	for group, node := range map[string]string{"A": "a", "B": "b"} {
		if err := store.Save("work", group, node); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.Restore(context.Background(), "work", NewService(failingSelectionClient{}))
	if err == nil || len(result.Failed) != 1 || len(result.Restored) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
