package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

// Exercise production profile mutation and the privileged control path,
// including the pinned restricted engine's preflight and actual reload parser.
func TestProviderChainMutationRealRestrictedReloadAndClear(t *testing.T) {
	binary := os.Getenv("NAGI_TS_TEST_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("requires root and explicit pinned NAGI_TS_TEST_ENGINE")
	}
	payload := []byte("proxies: [{name: Raw B, type: socks5, server: 127.0.0.1, port: 9}, {name: Raw A, type: socks5, server: 127.0.0.1, port: 9}]\n")
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer feed.Close()
	root := secureTestDir(t)
	config, private := filepath.Join(root, "source"), filepath.Join(root, "private")
	profiles := filepath.Join(config, "profiles")
	for _, dir := range []string{config, profiles, private} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(profiles, "work.yaml")
	source := map[string]any{"type": "http", "url": feed.URL}
	cfg := map[string]any{"mixed-port": 0, "mode": "direct", "log-level": "silent", "tun": map[string]any{"enable": false}, "dns": map[string]any{"enable": false}, "proxies": []any{map[string]any{"name": "Entry", "type": "socks5", "server": "127.0.0.1", "port": 9}}, "proxy-providers": map[string]any{"P": source}, "proxy-groups": []any{map[string]any{"name": "Pick", "type": "select", "use": []any{"P"}}}, "rules": []any{"MATCH,DIRECT"}}
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 501, 501); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{SessionUID: 501, BinaryPath: binary, SourceDir: config, PrivateDir: private}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	started, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Start, Profile: "work", ConfigPath: path})
	if err != nil || !started.Running {
		t.Fatal("start", err)
	}
	var store *profile.Store
	var sequence int
	var rejectNext bool
	reload := func(ctx context.Context) error {
		effective, err := store.Effective("work")
		if err != nil {
			return err
		}
		if rejectNext {
			rejectNext = false
			var rejected map[string]any
			if err := yaml.Unmarshal(effective, &rejected); err != nil {
				return err
			}
			rejected["proxy-providers"].(map[string]any)["P"].(map[string]any)["path"] = "/tmp/rejected-cache.yaml"
			effective, err = yaml.Marshal(rejected)
			if err != nil {
				return err
			}
		}
		next := filepath.Join(profiles, ".effective-work.yaml")
		if err = os.WriteFile(next, effective, 0600); err != nil {
			return err
		}
		if err := os.Chown(next, 501, 501); err != nil {
			return err
		}
		sequence++
		body, _ := json.Marshal(map[string]string{"path": next, "activation_id": fmt.Sprintf("%032x", sequence)})
		response, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: body})
		if err != nil {
			return err
		}
		if !response.OK || response.NotForwarded {
			return fmt.Errorf("reload not accepted")
		}
		return nil
	}
	store = profile.NewStore(config, func(candidate []byte) error {
		check := filepath.Join(profiles, ".check-work.yaml")
		if err := os.WriteFile(check, candidate, 0600); err != nil {
			return err
		}
		defer os.Remove(check)
		output, err := exec.CommandContext(ctx, binary, "-t", "--check-json", "-f", check, "-d", config).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ordinary validation: %s", output)
		}
		return nil
	}, nil)
	if err := store.Use(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	store.Reload = reload
	store.ProviderNodes = func(name, provider string, definition map[string]any) ([]map[string]any, error) {
		response, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.ProviderChainNodes, Profile: name, Provider: provider, SourceHash: profile.ProviderSourceHash(definition)})
		if err != nil {
			return nil, err
		}
		var nodes []map[string]any
		err = json.Unmarshal(response.Body, &nodes)
		return nodes, err
	}
	member := profile.ProviderChainMember{Provider: "P", Node: "Raw B", RawName: "Raw B", Type: "socks5", Dialer: "Entry", Outlet: true}
	generated := member.ID() + "-source"
	assertActive := func(chained bool) {
		t.Helper()
		current, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/configs"})
		if err != nil {
			t.Fatal(err)
		}
		var live struct {
			ActivationID string `json:"activation_id"`
		}
		json.Unmarshal(current.Body, &live)
		if live.ActivationID != fmt.Sprintf("%032x", sequence) || h.status().PID != started.PID {
			t.Fatal("reload did not activate in place")
		}
		snapshot, err := os.ReadFile(h.snapshot)
		if err != nil {
			t.Fatal(err)
		}
		var active map[string]any
		yaml.Unmarshal(snapshot, &active)
		providers := active["proxy-providers"].(map[string]any)
		if _, ok := providers[generated]; ok != chained {
			t.Fatal("derived provider lifecycle mismatch")
		}
		for _, value := range providers {
			if _, ok := value.(map[string]any)["path"]; ok {
				t.Fatal("explicit HTTP path reached native snapshot")
			}
		}
		raw := filepath.Join(private, "proxies", fmt.Sprintf("%x", md5.Sum([]byte(feed.URL))))
		cached, err := os.ReadFile(raw)
		if err != nil || !bytes.Equal(cached, payload) {
			t.Fatal("native raw payload cache changed", err)
		}
		for p, mode := range map[string]os.FileMode{raw: 0600, filepath.Dir(raw): 0700} {
			info, err := os.Stat(p)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("unsafe native cache", err)
			}
		}
		if chained {
			response, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/providers/proxies/" + generated})
			if err != nil {
				t.Fatal(err)
			}
			var p struct {
				Proxies []struct {
					Name string `json:"name"`
				} `json:"proxies"`
			}
			json.Unmarshal(response.Body, &p)
			if len(p.Proxies) != 1 || p.Proxies[0].Name != "Raw B" {
				t.Fatal("derived provider did not isolate selected node")
			}
			override := providers[generated].(map[string]any)["override"].(map[string]any)
			if override["dialer-proxy"] != "Entry" {
				t.Fatal("native dialer edge missing")
			}
		}
	}
	if _, err := store.MutateScopedProxyChain(ctx, "work", "provider-node", []string{"Raw B"}, "P", []profile.ChainNodeRef{{Kind: "node", Name: "Entry"}}, false); err != nil {
		t.Fatal("apply", err)
	}
	assertActive(true)
	if err := reload(ctx); err != nil {
		t.Fatal("repeat reload", err)
	}
	assertActive(true)
	// A failed actual helper activation rolls the saved production mutation back.
	saved, err := store.Override("work")
	if err != nil {
		t.Fatal(err)
	}
	acceptedSequence, acceptedSnapshot := sequence, h.snapshot
	rejectNext = true
	if _, err := store.MutateScopedProxyChain(ctx, "work", "provider-node", []string{"Raw A"}, "P", []profile.ChainNodeRef{{Kind: "node", Name: "Entry"}}, false); err == nil {
		t.Fatal("rejected mutation unexpectedly succeeded")
	}
	sequence = acceptedSequence
	restored, err := store.Override("work")
	if err != nil || !bytes.Equal(saved, restored) || h.snapshot != acceptedSnapshot {
		t.Fatal("failed activation did not restore override/root generation", err)
	}
	assertActive(true)
	// A user-controlled HTTP path must still fail before forwarding and preserve
	// both the confirmed root generation and the active engine configuration.
	before := h.snapshot
	invalid := filepath.Join(profiles, ".effective-work.yaml")
	effective, _ := store.Effective("work")
	var bad map[string]any
	yaml.Unmarshal(effective, &bad)
	bad["proxy-providers"].(map[string]any)[generated].(map[string]any)["path"] = "/tmp/arbitrary-private-subscription.yaml"
	broken, _ := yaml.Marshal(bad)
	os.WriteFile(invalid, broken, 0600)
	os.Chown(invalid, 501, 501)
	body, _ := json.Marshal(map[string]string{"path": invalid, "activation_id": "ffffffffffffffffffffffffffffffff"})
	response, err := h.Handle(ctx, 501, privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: body})
	if err == nil || !response.NotForwarded || h.snapshot != before {
		t.Fatal("arbitrary path was forwarded")
	}
	if strings.Contains(err.Error(), feed.URL) || strings.Contains(err.Error(), "arbitrary-private-subscription") {
		t.Fatal("rejection leaked sensitive input")
	}
	assertActive(true)
	if _, err := store.MutateScopedProxyChain(ctx, "work", "provider-node", []string{"Raw B"}, "P", nil, true); err != nil {
		t.Fatal("clear", err)
	}
	assertActive(false)
	if err := reload(ctx); err != nil {
		t.Fatal("reload after clear", err)
	}
	assertActive(false)
}
