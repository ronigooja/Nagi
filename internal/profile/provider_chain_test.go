package profile

import (
	"context"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func providerScopedFixture(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "profiles"), 0700)
	data := `proxies:
  - {name: Entry, type: socks5, server: 127.0.0.1, port: 1080}
  - {name: Exit, type: socks5, server: 127.0.0.1, port: 1081}
proxy-providers:
  P:
    type: inline
    payload:
      - {name: Shared, type: socks5, server: 127.0.0.1, port: 1082}
      - {name: Other, type: socks5, server: 127.0.0.1, port: 1083}
  Q:
    type: inline
    payload:
      - {name: Shared, type: socks5, server: 127.0.0.1, port: 1084}
proxy-groups:
  - {name: Pick, type: select, use: [P, Q]}
rules: [MATCH,Pick]
`
	os.WriteFile(filepath.Join(dir, "profiles", "default.yaml"), []byte(data), 0600)
	return NewStore(dir, func([]byte) error { return nil }, nil)
}
func TestProviderScopedChainMiddleAndOutlet(t *testing.T) {
	s := providerScopedFixture(t)
	ctx := context.Background()
	refs := []ChainNodeRef{{Kind: "node", Name: "Entry"}, {Kind: "provider-node", Provider: "P", Name: "Shared"}}
	p, err := s.MutateScopedProxyChain(ctx, "default", "node", []string{"Exit"}, "", refs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 2 {
		t.Fatal(p)
	}
	data, err := s.Effective("default")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), ProviderChainMembersKey) {
		t.Fatal("private metadata leaked")
	}
	var cfg map[string]any
	yaml.Unmarshal(data, &cfg)
	providers := cfg["proxy-providers"].(map[string]any)
	original := providers["P"].(map[string]any)
	if chainProviderDialer(original) != "" {
		t.Fatal("source mutated")
	}
	members, err := s.savedProviderMembers("default")
	if err != nil || len(members) != 1 || members[0].Dialer != "Entry" {
		t.Fatal(members, err)
	}
	chains, err := s.ProxyChains("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chains {
		if c.Name == "Exit" && (len(c.PathRefs) != 3 || c.PathRefs[1].Provider != "P") {
			t.Fatal(c)
		}
	}
	p, err = s.MutateScopedProxyChain(ctx, "default", "provider-node", []string{"Shared"}, "Q", []ChainNodeRef{{Kind: "provider-node", Provider: "P", Name: "Shared"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Outlet != "Chain: Q / Shared" || len(p.OutletGroups) != 1 {
		t.Fatal(p)
	}
	_, err = s.MutateScopedProxyChain(ctx, "default", "provider-node", []string{"Shared"}, "Q", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	members, _ = s.savedProviderMembers("default")
	if len(members) != 1 || members[0].Provider != "P" {
		t.Fatal(members)
	}
}
func TestProviderScopedDryRunPureAndRollback(t *testing.T) {
	s := providerScopedFixture(t)
	refs := []ChainNodeRef{{Kind: "provider-node", Provider: "P", Name: "Shared"}}
	if _, err := s.PlanScopedProxyChain("default", "node", []string{"Exit"}, "", refs, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Override("default"); !os.IsNotExist(err) {
		t.Fatal("dry run persisted", err)
	}
	s.Validate = func([]byte) error { return os.ErrPermission }
	if _, err := s.MutateScopedProxyChain(context.Background(), "default", "node", []string{"Exit"}, "", refs, false); err == nil {
		t.Fatal("expected validation failure")
	}
	if _, err := s.Override("default"); !os.IsNotExist(err) {
		t.Fatal("rollback retained overlay", err)
	}
}
func TestProviderInventoryDistinctSourcesAndNoGenerated(t *testing.T) {
	s := providerScopedFixture(t)
	_, err := s.MutateScopedProxyChain(context.Background(), "default", "node", []string{"Exit"}, "", []ChainNodeRef{{Kind: "provider-node", Provider: "P", Name: "Shared"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	values, unavailable, err := s.ProviderChainInventory("default")
	if err != nil || len(unavailable) != 0 || len(values) != 3 {
		t.Fatal(values, unavailable, err)
	}
	if values[0].Provider != "P" || values[0].Name != "Shared" || values[1].Name != "Other" || values[2].Provider != "Q" || values[0].ID == values[2].ID {
		t.Fatal(values)
	}
}
