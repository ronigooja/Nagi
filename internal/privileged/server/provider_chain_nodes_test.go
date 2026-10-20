package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

func TestProviderChainEvidencePrivateCacheAndBoundSource(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root cache ownership fixture")
	}
	root := t.TempDir()
	private := filepath.Join(root, "private")
	user := filepath.Join(root, "user")
	os.Mkdir(private, 0700)
	os.Mkdir(user, 0700)
	source := map[string]any{"type": "http", "url": "https://example.invalid/private-feed?token=FAKE", "override": map[string]any{"additional-prefix": "Prefix "}}
	config, _ := yaml.Marshal(map[string]any{"proxy-providers": map[string]any{"P": source}})
	snapshot := filepath.Join(private, "snapshot.yaml")
	os.WriteFile(snapshot, config, 0600)
	cache := filepath.Join(private, "proxies", fmt.Sprintf("%x", md5.Sum([]byte(source["url"].(string)))))
	os.Mkdir(filepath.Dir(cache), 0700)
	payload := []byte("proxies:\n  - {name: Raw B, type: socks5, server: example.invalid, password: FAKE_SECRET}\n  - {name: Raw A, type: socks5, server: example.invalid, port: 1080}\n")
	os.WriteFile(cache, payload, 0600)
	h := &RuntimeHandler{SessionUID: 501, PrivateDir: private}
	h.bindProviderEvidence("work", filepath.Join(user, ".effective-work.yaml"), snapshot, config)
	req := privileged.Request{Version: privileged.ProtocolVersion, Operation: privileged.ProviderChainNodes, Profile: "work", Provider: "P", SourceHash: profile.ProviderSourceHash(source)}
	before, _ := os.Stat(cache)
	response, err := h.providerChainNodes(req)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(response.Body, []byte("SECRET")) || bytes.Contains(response.Body, []byte("server")) || bytes.Contains(response.Body, []byte("token")) {
		t.Fatal("credential fields crossed boundary")
	}
	var nodes []map[string]string
	if json.Unmarshal(response.Body, &nodes) != nil || len(nodes) != 2 || nodes[0]["name"] != "Raw B" || len(nodes[0]) != 2 {
		t.Fatal(string(response.Body))
	}
	after, _ := os.Stat(cache)
	if !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatal("inspection changed cache")
	}
	for _, mutate := range []func(*privileged.Request){
		func(r *privileged.Request) { r.Profile = "other" },
		func(r *privileged.Request) { r.Provider = "missing" },
		func(r *privileged.Request) {
			r.SourceHash = profile.ProviderSourceHash(map[string]any{"type": "http", "url": "https://example.invalid/other"})
		},
	} {
		bad := req
		mutate(&bad)
		if _, err := h.providerChainNodes(bad); err == nil {
			t.Fatal("unbound source accepted")
		}
	}
	if _, err := h.Handle(context.Background(), 502, req); err == nil {
		t.Fatal("different peer accepted")
	}
	if _, err := h.Handle(context.Background(), 501, req); err == nil {
		t.Fatal("stopped engine served private evidence")
	}
	outside := filepath.Join(root, "outside.yaml")
	os.WriteFile(outside, payload, 0600)
	os.Remove(cache)
	os.Symlink(outside, cache)
	if _, err := h.providerChainNodes(req); err == nil {
		t.Fatal("symlink cache accepted")
	}
	os.Remove(cache)
	if _, err := h.providerChainNodes(req); err == nil {
		t.Fatal("missing cache accepted")
	}
	// A forwarded uncertain reload clears this binding; neither old nor new source
	// is trusted until activation has succeeded.
	h.providerEvidence = providerEvidence{}
	if _, err := h.providerChainNodes(req); err == nil {
		t.Fatal("unbound generation accepted")
	}
}

func TestProviderChainEvidenceInlineFileAndConfinement(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root cache ownership fixture")
	}
	private := t.TempDir()
	payload := []byte("proxies: [{name: Raw, type: socks5, password: FAKE_SECRET}]\n")
	file := filepath.Join(private, "resource.yaml")
	os.WriteFile(file, payload, 0600)
	for _, source := range []map[string]any{
		{"type": "inline", "payload": []any{map[string]any{"name": "Raw", "type": "socks5", "password": "FAKE_SECRET"}}},
		{"type": "file", "path": file},
		{"type": "file", "path": filepath.Join(t.TempDir(), "outside.yaml")},
	} {
		cfg, _ := yaml.Marshal(map[string]any{"proxy-providers": map[string]any{"P": source}})
		snapshot := filepath.Join(private, "snapshot.yaml")
		os.WriteFile(snapshot, cfg, 0600)
		h := &RuntimeHandler{PrivateDir: private}
		h.bindProviderEvidence("work", "/user/.effective-work.yaml", snapshot, cfg)
		req := privileged.Request{Profile: "work", Provider: "P", SourceHash: profile.ProviderSourceHash(source)}
		response, err := h.providerChainNodes(req)
		if source["path"] != nil && source["path"] != file {
			if err == nil {
				t.Fatal("outside private directory accepted")
			}
			continue
		}
		if err != nil || bytes.Contains(response.Body, []byte("SECRET")) {
			t.Fatal("identity projection", err)
		}
	}
}
