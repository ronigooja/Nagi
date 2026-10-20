package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

func providerChainCommandFixture(t *testing.T) string {
	dir := nodeChainFixture(t)
	path := filepath.Join(dir, "profiles", "default.yaml")
	data, _ := os.ReadFile(path)
	var cfg map[string]any
	yaml.Unmarshal(data, &cfg)
	cfg["proxy-providers"] = map[string]any{"P": map[string]any{"type": "inline", "payload": []any{map[string]any{"name": "Shared", "type": "socks5", "server": "127.0.0.1", "port": 1084}}}, "Q": map[string]any{"type": "inline", "payload": []any{map[string]any{"name": "Shared", "type": "socks5", "server": "127.0.0.1", "port": 1085}}}}
	cfg["proxy-groups"] = []any{map[string]any{"name": "Choice", "type": "select", "proxies": []any{"DIRECT"}, "use": []any{"P", "Q"}}}
	data, _ = yaml.Marshal(cfg)
	os.WriteFile(path, data, 0600)
	return dir
}
func TestProviderNodeChainActualCLIIdentityOrderRecovery(t *testing.T) {
	dir := providerChainCommandFixture(t)
	result := runNodeChainJSON(t, "proxy", "chain", "set", "provider-node", "Q", "Shared", "--via-provider", "P", "Shared", "--via", "First")
	p := result["plan"].(map[string]any)
	if p["outlet"] != "Chain: Q / Shared" || result["reloaded"] != false {
		t.Fatal(result)
	}
	got := runNodeChainJSON(t, "proxy", "chain", "show", "provider-node", "Q", "Shared")["chain"].(map[string]any)
	refs := got["path_refs"].([]any)
	if len(refs) != 3 || refs[0].(map[string]any)["provider"] != "P" || refs[1].(map[string]any)["name"] != "First" || refs[2].(map[string]any)["provider"] != "Q" {
		t.Fatal(got)
	}
	// Simulate an external source rename; listing and disconnect must still work.
	path := filepath.Join(dir, "profiles", "default.yaml")
	data, _ := os.ReadFile(path)
	var cfg map[string]any
	yaml.Unmarshal(data, &cfg)
	providers := cfg["proxy-providers"].(map[string]any)
	providers["Q"].(map[string]any)["override"] = map[string]any{"additional-prefix": "Changed "}
	data, _ = yaml.Marshal(cfg)
	os.WriteFile(path, data, 0600)
	got = runNodeChainJSON(t, "proxy", "chain", "show", "provider-node", "Q", "Shared")["chain"].(map[string]any)
	if got["available"] != false || got["outlet"] != "Chain: Q / Shared" {
		t.Fatal(got)
	}
	selections := map[string]any{"default": map[string]string{"Choice": "Chain: Q / Shared", "Keep": "First"}}
	data, _ = json.Marshal(selections)
	os.WriteFile(filepath.Join(dir, "proxy-selections.json"), data, 0600)
	runNodeChainJSON(t, "proxy", "chain", "clear", "provider-node", "Q", "Shared")
	data, _ = os.ReadFile(filepath.Join(dir, "proxy-selections.json"))
	if bytes.Contains(data, []byte("Chain:")) || !bytes.Contains(data, []byte("First")) {
		t.Fatal(string(data))
	}
}
func TestProviderNodeChainMixedArgumentOrderAndUsage(t *testing.T) {
	o, err := chainArguments([]string{"proxy", "chain", "set", "node", "Target", "--via-provider", "P", "Shared", "--via", "First", "--via-provider", "Q", "Shared"})
	if err != nil || len(o.refs) != 3 || o.refs[0].Provider != "P" || o.refs[1].Name != "First" || o.refs[2].Provider != "Q" {
		t.Fatal(o, err)
	}
	for _, args := range [][]string{{"proxy", "chain", "set", "provider-node", "P", "--via", "First"}, {"proxy", "chain", "set", "node", "Target", "--via-provider", "P"}} {
		if _, err := chainArguments(args); err == nil {
			t.Fatal(args)
		}
	}
}
func TestProviderNodeChainPinnedNativeValidation(t *testing.T) {
	binary := "/root/Nagi/bin/mihomo"
	if _, err := os.Stat(binary); err != nil {
		t.Skip("matching pinned local engine unavailable")
	}
	providerChainCommandFixture(t)
	t.Setenv("NAGI_MIHOMO_BIN", binary)
	runNodeChainJSON(t, "proxy", "chain", "set", "node", "Target A", "--via-provider", "P", "Shared", "--via", "First")
	runNodeChainJSON(t, "proxy", "chain", "set", "provider-node", "Q", "Shared", "--via", "First")
}

func TestThreeProviderChainClearActualCLI(t *testing.T) {
	dir := providerChainCommandFixture(t)
	path := filepath.Join(dir, "profiles", "default.yaml")
	data, _ := os.ReadFile(path)
	var cfg map[string]any
	yaml.Unmarshal(data, &cfg)
	providers := cfg["proxy-providers"].(map[string]any)
	providers["R"] = providers["Q"]
	data, _ = yaml.Marshal(cfg)
	os.WriteFile(path, data, 0600)
	runNodeChainJSON(t, "proxy", "chain", "set", "provider-node", "R", "Shared", "--via-provider", "P", "Shared", "--via-provider", "Q", "Shared")
	runNodeChainJSON(t, "proxy", "chain", "clear", "provider-node", "R", "Shared")
	list := runNodeChainJSON(t, "proxy", "chain", "list")["chains"].([]any)
	for _, v := range list {
		entry := v.(map[string]any)
		if entry["kind"] == "provider-node" {
			if len(entry["via"].([]any)) != 0 || entry["outlet"] != nil {
				t.Fatal(entry)
			}
		}
	}
	store := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	effective, err := store.Effective("default")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(effective, []byte("nagi-chain-")) {
		t.Fatal("unreferenced resources retained", string(effective))
	}
}

func TestProviderChainCachesParticipateInConfigRollback(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "profiles", ".chain-cache", "default")
	os.MkdirAll(cache, 0700)
	old := filepath.Join(cache, "old.yaml")
	os.WriteFile(old, []byte("last-good fixture"), 0600)
	images, err := snapshotConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(cache, "new.yaml")
	os.WriteFile(added, []byte("candidate fixture"), 0600)
	os.WriteFile(old, []byte("changed fixture"), 0600)
	if err = restoreConfig(dir, images); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(added); !os.IsNotExist(err) {
		t.Fatal("candidate cache survived rollback", err)
	}
	data, _ := os.ReadFile(old)
	info, _ := os.Stat(old)
	if string(data) != "last-good fixture" || info.Mode().Perm() != 0600 {
		t.Fatal("last-good cache not restored privately", string(data), info.Mode())
	}
}
