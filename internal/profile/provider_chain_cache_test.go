package profile

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateProviderCachePrepareCleanupAndSymlink(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "profiles"), 0700)
	id := (ProviderChainMember{Provider: "P", Node: "Shared"}).providerID()
	path := filepath.Join(dir, "profiles", ".chain-cache", "work", id+".yaml")
	cfg := map[string]any{"proxy-providers": map[string]any{id: map[string]any{"type": "http", "path": path}}}
	data, _ := yaml.Marshal(cfg)
	cleanup, err := PrepareProviderChainCaches(data, dir, "work")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	parent, _ := os.Stat(filepath.Dir(path))
	if info.Mode().Perm() != 0600 || parent.Mode().Perm() != 0700 {
		t.Fatal(info.Mode(), parent.Mode())
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "target")
	os.WriteFile(outside, []byte("keep"), 0600)
	os.Symlink(outside, path)
	if _, err := PrepareProviderChainCaches(data, dir, "work"); err == nil {
		t.Fatal("accepted symlink")
	}
	content, _ := os.ReadFile(outside)
	if string(content) != "keep" {
		t.Fatal("modified target")
	}
}
