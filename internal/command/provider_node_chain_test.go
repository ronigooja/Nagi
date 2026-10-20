package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
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

func TestGenericOverrideRetiresOnlyRemovedOutletChoices(t *testing.T) {
	for _, action := range []string{"set", "clear"} {
		t.Run(action, func(t *testing.T) {
			dir := providerChainCommandFixture(t)
			runNodeChainJSON(t, "proxy", "chain", "set", "provider-node", "Q", "Shared", "--via", "First")
			path := filepath.Join(dir, "proxy-selections.json")
			choices := `{"default":{"Choice":"Chain: Q / Shared","Chain: Q / Shared":"Shared","Keep":"First"},"other":{"Choice":"Chain: Q / Shared"}}`
			if err := os.WriteFile(path, []byte(choices), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"profile", "override", action, "default"}
			if action == "set" {
				patch := filepath.Join(t.TempDir(), "override.yaml")
				os.WriteFile(patch, []byte("mode: rule\n"), 0600)
				args = append(args, patch)
			}
			runNodeChainJSON(t, args...)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]map[string]string
			json.Unmarshal(data, &got)
			if len(got["default"]) != 1 || got["default"]["Keep"] != "First" || got["other"]["Choice"] != "Chain: Q / Shared" {
				t.Fatal(string(data))
			}
			runNodeChainJSON(t, "proxy", "chain", "set", "provider-node", "Q", "Shared", "--via", "First")
			effective, err := profile.NewStore(dir, nil, nil).Effective("default")
			if err != nil {
				t.Fatal(err)
			}
			from := proxy.SelectionStore{Path: path}
			replay, _, err := from.ApplyDefaults("default", effective)
			if err != nil || bytes.Contains(replay, []byte("default-selected: 'Chain:")) || bytes.Contains(replay, []byte("default-selected: Chain:")) {
				t.Fatalf("unexpected replay %s: %v", replay, err)
			}
		})
	}
}

func TestRetiredOutletChoicesRollbackOnReloadFailure(t *testing.T) {
	dir := providerChainCommandFixture(t)
	runNodeChainJSON(t, "proxy", "chain", "set", "provider-node", "Q", "Shared", "--via", "First")
	path := filepath.Join(dir, "proxy-selections.json")
	saved := []byte(`{"default":{"Choice":"Chain: Q / Shared","Keep":"First"}}`)
	os.WriteFile(path, saved, 0600)
	old, _ := profile.NewStore(dir, nil, nil).Override("default")
	paths, err := nagiruntime.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	store := newProfileStore(paths, "", nil, nil)
	store.Validate = func([]byte) error { return nil }
	store.Reload = func(context.Context) error {
		data, _ := os.ReadFile(path)
		if bytes.Contains(data, []byte("Chain:")) {
			t.Fatal("removed outlet replayed during reload")
		}
		return errors.New("activation failed")
	}
	for _, clear := range []bool{false, true} {
		if clear {
			err = store.ClearOverride(context.Background(), "default")
		} else {
			err = store.SetOverride(context.Background(), "default", []byte("mode: rule\n"))
		}
		if err == nil {
			t.Fatal("expected failed reload")
		}
		got, _ := os.ReadFile(path)
		override, _ := store.Override("default")
		if !bytes.Equal(got, saved) || !bytes.Equal(override, old) {
			t.Fatalf("rollback changed state: %s %s", got, override)
		}
	}
}
