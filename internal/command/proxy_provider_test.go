package command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/proxy"
)

type autoClient struct {
	deleted   bool
	groupTest bool
}

func (f *autoClient) Get(_ context.Context, path string, out any) error {
	switch {
	case path == "/proxies/Auto":
		return json.Unmarshal([]byte(`{"name":"Auto","type":"URLTest","all":["Node"],"fixed":"Node","now":"Node"}`), out)
	case strings.HasPrefix(path, "/group/Auto/delay?"):
		f.groupTest = true
		return json.Unmarshal([]byte(`{"Node":15}`), out)
	}
	return nil
}
func (*autoClient) Put(context.Context, string, any, any) error { return nil }
func (f *autoClient) Delete(_ context.Context, path string, _ any) error {
	f.deleted = path == "/proxies/Auto"
	return nil
}
func TestAutoClearsSavedSelectionAndReportsPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	store := proxy.SelectionStore{Path: filepath.Join(dir, "proxy-selections.json")}
	client := &autoClient{}
	if err := store.Save("default", "Auto", "Node"); err != nil {
		t.Fatal(err)
	}
	result, err := proxyCommand(context.Background(), []string{"proxy", "auto", "Auto"}, proxy.NewService(client), store, dir)
	if err != nil || !client.deleted || result.(map[string]any)["saved_selection_removed"] != true {
		t.Fatalf("result=%v err=%v deleted=%v", result, err, client.deleted)
	}
	b, _ := os.ReadFile(store.Path)
	if strings.Contains(string(b), "Auto") {
		t.Fatalf("saved pin remains: %s", b)
	}
	if err := store.Save("default", "Auto", "Node"); err != nil {
		t.Fatal(err)
	}
	_, err = proxyCommand(context.Background(), []string{"proxy", "delays", "Auto"}, proxy.NewService(client), store, dir)
	if err != nil || !client.groupTest {
		t.Fatalf("group test err=%v used=%v", err, client.groupTest)
	}
	b, _ = os.ReadFile(store.Path)
	if strings.Contains(string(b), "Auto") {
		t.Fatalf("group test retained saved pin: %s", b)
	}
	bad := proxy.SelectionStore{Path: filepath.Join(dir, "missing", "choices.json")}
	if err := os.MkdirAll(filepath.Dir(bad.Path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad.Path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = proxyCommand(context.Background(), []string{"proxy", "auto", "Auto"}, proxy.NewService(client), bad, dir)
	if err == nil || !strings.Contains(err.Error(), "running engine") {
		t.Fatalf("missing partial-state error: %v", err)
	}
}
func TestDNSBindingAllowsGroupsAndRejectsMissing(t *testing.T) {
	cfg := []byte("proxies:\n  - name: Node\n    type: ss\n  - name: DirectNode\n    type: direct\nproxy-groups:\n  - name: Auto\n    type: url-test\ndns:\n  nameserver:\n    - https://example.test/dns-query#Auto\n")
	if err := validateDNSProxyTargets(cfg); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Gone", "DirectNode"} {
		if err := validateDNSProxyTargets([]byte(strings.ReplaceAll(string(cfg), "#Auto", "#"+name))); err == nil {
			t.Fatalf("accepted invalid binding %s", name)
		}
	}
}
func TestProviderAndAutoInvocationPreflight(t *testing.T) {
	for _, args := range [][]string{{"proxy", "auto"}, {"proxy", "provider", "show"}, {"proxy", "provider", "remove", "name"}, {"completion", "candidates", "providers", "extra"}} {
		if err := validateInvocation(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"proxy", "auto", "Auto"}, {"proxy", "provider", "healthcheck", "work"}, {"completion", "candidates", "providers"}} {
		if err := validateInvocation(args); err != nil {
			t.Fatalf("rejected %v: %v", args, err)
		}
	}
}
