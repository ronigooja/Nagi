package server

import (
	"bytes"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderChainHTTPRootCacheRewrite(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root snapshot test")
	}
	source := t.TempDir()
	profiles := filepath.Join(source, "profiles")
	os.Mkdir(profiles, 0700)
	private, err := os.MkdirTemp("/root", "nagi-chain-helper-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	id := "nagi-chain-012345678901234567890123-source"
	userPath := filepath.Join(profiles, ".chain-cache", "work", id+".yaml")
	cfg := map[string]any{"proxy-providers": map[string]any{id: map[string]any{"type": "http", "url": "https://example.invalid/fixture.yaml", "path": userPath}}}
	data, _ := yaml.Marshal(cfg)
	h := &RuntimeHandler{SessionUID: os.Getuid(), SourceDir: source, PrivateDir: private}
	snapshot, resources, err := h.prepareConfig(data, filepath.Join(profiles, ".effective-work.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer removeSnapshots(snapshot, resources)
	out, _ := os.ReadFile(snapshot)
	if bytes.Contains(out, []byte(userPath)) || !bytes.Contains(out, []byte(filepath.Join(private, ".chain-cache"))) {
		t.Fatal(string(out))
	}
	var doc map[string]any
	yaml.Unmarshal(out, &doc)
	p := doc["proxy-providers"].(map[string]any)[id].(map[string]any)
	path := p["path"].(string)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	cfg["proxy-providers"].(map[string]any)[id].(map[string]any)["path"] = "/tmp/arbitrary.yaml"
	data, _ = yaml.Marshal(cfg)
	if _, _, err := h.prepareConfig(data, filepath.Join(profiles, ".effective-work.yaml")); err == nil {
		t.Fatal("arbitrary HTTP path accepted")
	}
}
