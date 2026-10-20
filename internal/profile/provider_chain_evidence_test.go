package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProviderIdentityResolverRawRegexMappingAndGeneratedSourceHash(t *testing.T) {
	s := providerScopedFixture(t)
	data, _ := s.Show("default")
	var cfg map[string]any
	yaml.Unmarshal(data, &cfg)
	source := map[string]any{"type": "http", "url": "https://example.invalid/feed", "override": map[string]any{"proxy-name": []any{map[string]any{"pattern": "^Raw (.*)$", "target": "$1"}}, "additional-prefix": "P ", "additional-suffix": " S"}}
	cfg["proxy-providers"].(map[string]any)["P"] = source
	data, _ = yaml.Marshal(cfg)
	os.WriteFile(filepath.Join(s.Dir, "profiles", "default.yaml"), data, 0600)
	calls := 0
	s.ProviderNodes = func(name, p string, got map[string]any) ([]map[string]any, error) {
		if p != "P" {
			return nil, nil
		}
		calls++
		if name != "default" || ProviderSourceHash(got) != ProviderSourceHash(source) {
			t.Fatal("logical source changed")
		}
		return FilterProviderChainEvidence(got, []map[string]any{{"name": "Raw B", "type": "socks5"}, {"name": "Raw A", "type": "socks5"}, {"name": "Raw Gone", "type": "socks5"}}, map[string]bool{"P B S": true, "P A S": true})
	}
	inventory, unavailable, err := s.ProviderChainInventory("default")
	if err != nil || len(unavailable) != 0 {
		t.Fatal(unavailable, err)
	}
	if inventory[0].Name != "P B S" || inventory[1].Name != "P A S" {
		t.Fatal("source order/mapping", inventory)
	}
	_, err = s.MutateScopedProxyChain(context.Background(), "default", "node", []string{"Exit"}, "", []ChainNodeRef{{Kind: "provider-node", Provider: "P", Name: "P B S"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	members, _ := s.savedProviderMembers("default")
	if len(members) != 1 || members[0].RawName != "Raw B" {
		t.Fatal(members)
	}
	// materialized data sent to helper contains generated resources, but its
	// original source provider must hash identically to chainLogicalConfig.
	effective, err := s.Effective("default")
	if err != nil {
		t.Fatal(err)
	}
	var active map[string]any
	yaml.Unmarshal(effective, &active)
	raw := active["proxy-providers"].(map[string]any)["P"].(map[string]any)
	logical, err := s.chainLogicalConfig("default")
	if err != nil {
		t.Fatal(err)
	}
	if ProviderSourceHash(raw) != ProviderSourceHash(logical["proxy-providers"].(map[string]any)["P"].(map[string]any)) {
		t.Fatal("helper and logical source hashes differ")
	}
	if calls < 2 {
		t.Fatal("resolver not shared by inventory and mutation")
	}
	// A running provider with no effective nodes must not fall back to stale
	// local payloads. Stopped providers deliberately delegate with nil.
	s.ProviderNodes = func(string, string, map[string]any) ([]map[string]any, error) { return []map[string]any{}, nil }
	inventory, _, err = s.ProviderChainInventory("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range inventory {
		if v.Available {
			t.Fatal("empty active provider fell back to stale payload")
		}
	}
	s.ProviderNodes = nil
	_, unavailable, err = s.ProviderChainInventory("default")
	if err != nil || len(unavailable) != 1 || unavailable[0] != "P" {
		t.Fatal("stopped cache availability", unavailable, err)
	}
}
