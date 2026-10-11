package proxy

import (
	"path/filepath"
	"testing"
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
