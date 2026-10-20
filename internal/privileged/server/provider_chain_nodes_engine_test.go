package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

func TestProviderChainPrivateHTTPRealEngineEvidence(t *testing.T) {
	binary := os.Getenv("NAGI_TS_TEST_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("requires root and explicit pinned NAGI_TS_TEST_ENGINE")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxies: [{name: Raw B, type: socks5, server: 127.0.0.1, port: 9}, {name: Raw A, type: socks5, server: 127.0.0.1, port: 9}]\n"))
	}))
	defer server.Close()
	root := secureTestDir(t)
	sourceDir, private := filepath.Join(root, "source"), filepath.Join(root, "private")
	os.Mkdir(sourceDir, 0700)
	os.Mkdir(private, 0700)
	source := map[string]any{"type": "http", "url": server.URL, "override": map[string]any{"proxy-name": []any{map[string]any{"pattern": "^Raw (.*)$", "target": "$1"}}, "additional-prefix": "P ", "additional-suffix": " S"}}
	cfg := map[string]any{"mixed-port": 0, "mode": "direct", "log-level": "silent", "tun": map[string]any{"enable": false}, "dns": map[string]any{"enable": false}, "proxy-providers": map[string]any{"P": source}, "proxy-groups": []any{map[string]any{"name": "Pick", "type": "select", "use": []any{"P"}}}, "rules": []any{"MATCH,DIRECT"}}
	data, _ := yaml.Marshal(cfg)
	path := filepath.Join(sourceDir, "work.yaml")
	os.WriteFile(path, data, 0600)
	os.Chown(path, 501, 501)
	h := &RuntimeHandler{SessionUID: 501, BinaryPath: binary, SourceDir: sourceDir, PrivateDir: private}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer h.Close()
	start, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Start, Profile: "work", ConfigPath: path})
	if err != nil || !start.Running {
		t.Fatal("start", err)
	}
	request := privileged.Request{Operation: privileged.ProviderChainNodes, Profile: "work", Provider: "P", SourceHash: profile.ProviderSourceHash(source)}
	response, err := h.Handle(ctx, 501, request)
	if err != nil {
		t.Fatal("private provider cache unavailable", err)
	}
	var nodes []map[string]string
	if json.Unmarshal(response.Body, &nodes) != nil || len(nodes) != 2 || nodes[0]["name"] != "Raw B" {
		t.Fatal(string(response.Body))
	}
	live, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/providers/proxies/P"})
	if err != nil {
		t.Fatal(err)
	}
	var active struct {
		Proxies []struct {
			Name string `json:"name"`
		} `json:"proxies"`
	}
	json.Unmarshal(live.Body, &active)
	if len(active.Proxies) != 2 || active.Proxies[0].Name != "P B S" {
		t.Fatal("effective rename mismatch", string(live.Body))
	}
	// A real reload binds the new profile and keeps the same private-cache source.
	next := filepath.Join(sourceDir, "other.yaml")
	os.WriteFile(next, data, 0600)
	os.Chown(next, 501, 501)
	body, _ := json.Marshal(map[string]string{"path": next, "activation_id": "0123456789abcdef0123456789abcdef"})
	reloaded, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: body})
	if err != nil || !reloaded.OK {
		t.Fatal("reload", err)
	}
	if _, err = h.Handle(ctx, 501, request); err == nil {
		t.Fatal("old profile read new profile evidence")
	}
	request.Profile = "other"
	if _, err = h.Handle(ctx, 501, request); err != nil {
		t.Fatal("new profile not bound", err)
	}
	// Forwarding a reload to an unavailable controller must invalidate metadata,
	// even though RuntimeHandler retains that candidate snapshot for safety.
	os.Remove(h.controllerPath())
	if _, err = h.reload(ctx, privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: body}); err == nil {
		t.Fatal("missing controller unexpectedly accepted reload")
	}
	if h.providerEvidence.snapshot != "" {
		t.Fatal("uncertain reload retained identity evidence")
	}
}
