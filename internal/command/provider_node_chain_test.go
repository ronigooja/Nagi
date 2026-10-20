package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
