package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/proxy"
	"github.com/ronigooja/Nagi/internal/subscription"
)

type subscriptionSelectionClient struct {
	reloaded bool
	gets     []string
	puts     map[string]string
}

func (c *subscriptionSelectionClient) Get(_ context.Context, path string, out any) error {
	c.gets = append(c.gets, path)
	if path != "/proxies" {
		return errors.New("unexpected per-group query")
	}
	before := `{"proxies":{"Manual":{"type":"Selector","now":"B","all":["A","B"]},"Unchanged":{"type":"Selector","now":"C","all":["C"]},"Auto":{"type":"URLTest","now":"X","all":["X","Y"]},"Pinned":{"type":"URLTest","now":"Y","fixed":"Y","all":["X","Y"]},"Fallback":{"type":"Fallback","now":"Y","fixed":"Y","all":["X","Y"]}}}`
	after := `{"proxies":{"Manual":{"type":"Selector","now":"A","all":["A","B"]},"Unchanged":{"type":"Selector","now":"C","all":["C"]},"Auto":{"type":"URLTest","now":"X","all":["X","Y"]},"Pinned":{"type":"URLTest","now":"X","fixed":"X","all":["X","Y"]},"Fallback":{"type":"Fallback","now":"X","fixed":"X","all":["X","Y"]}}}`
	if c.reloaded {
		return json.Unmarshal([]byte(after), out)
	}
	return json.Unmarshal([]byte(before), out)
}

func (c *subscriptionSelectionClient) Put(_ context.Context, path string, body any, _ any) error {
	if c.puts == nil {
		c.puts = map[string]string{}
	}
	c.puts[path] = body.(map[string]string)["name"]
	return nil
}

func TestSubscriptionApplyDoesNotRestoreOldChoices(t *testing.T) {
	ctx := context.Background()
	source := []byte("proxies:\n  - name: A\n    type: direct\nproxy-groups:\n  - name: Manual\n    type: select\n    proxies: [A, DIRECT]\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(source) }))
	defer server.Close()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Refresh(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	validator := func([]byte) error { return nil }
	if err := profile.NewStore(dir, validator, nil).Apply(ctx, "work", source); err != nil {
		t.Fatal(err)
	}
	selections := proxy.SelectionStore{Path: filepath.Join(dir, "proxy-selections.json")}
	if err := selections.Save("work", "Manual", "B"); err != nil {
		t.Fatal(err)
	}
	client := &subscriptionSelectionClient{}
	profiles := profile.NewStore(dir, validator, func(context.Context) error { client.reloaded = true; return nil })
	result, err := subscriptionCommand(ctx, []string{"subscription", "apply", "work"}, cache, profiles, proxy.NewService(client))
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["selections_restored"] != 0 {
		t.Fatalf("result: %+v", result)
	}
	if len(client.gets) != 0 {
		t.Fatalf("unexpected queries: %v", client.gets)
	}
	if len(client.puts) != 0 {
		t.Fatalf("unexpected selections: %v", client.puts)
	}
	stored, err := selections.Restore(ctx, "work", proxy.NewService(client))
	if err != nil || len(stored.Restored) != 0 || len(stored.Unavailable) != 0 {
		t.Fatalf("old choices remain: %+v, %v", stored, err)
	}
}

func TestSubscriptionPreviewApplyAndRollback(t *testing.T) {
	source := "mixed-port: 7890\nrules:\n  - MATCH,Proxy\nproxies:\n  - name: New\n    type: direct\nproxy-groups:\n  - name: Proxy\n    type: select\n    proxies: [New, DIRECT]\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(source)) }))
	defer server.Close()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	old := "mixed-port: 7000\nrules:\n  - MATCH,Subscription\ndns:\n  enable: true\nproxies:\n  - name: Old\n    type: direct\nproxy-groups:\n  - name: Subscription\n    type: select\n    proxies: [Old, DIRECT]\n"
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	result, err := subscriptionCommand(context.Background(), []string{"subscription", "preview", "work"}, cache, profiles, nil)
	if err != nil {
		t.Fatal(err)
	}
	preview := result.(subscription.Preview)
	if len(preview.Added) != 1 || preview.Added[0] != "New" || len(preview.Removed) != 1 || preview.Removed[0] != "Old" {
		t.Fatalf("preview: %+v", preview)
	}
	data, _ := os.ReadFile(path)
	if string(data) != old {
		t.Fatal("preview changed profile")
	}
	if _, err := subscriptionCommand(context.Background(), []string{"subscription", "apply", "work"}, cache, profiles, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "mixed-port: 7890") || !strings.Contains(string(data), "MATCH,Proxy") || strings.Contains(string(data), "MATCH,Subscription") || strings.Contains(string(data), "dns:") || !strings.Contains(string(data), "name: New") {
		t.Fatalf("merged profile:\n%s", data)
	}
	settings, _ := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if !strings.Contains(string(settings), "work") {
		t.Fatalf("selection: %s", settings)
	}
	failProfiles := profile.NewStore(dir, func([]byte) error { return nil }, func(context.Context) error { return errors.New("reload failed") })
	before := string(data)
	if _, err := subscriptionCommand(context.Background(), []string{"subscription", "apply", "work"}, cache, failProfiles, nil); err == nil {
		t.Fatal("expected reload failure")
	}
	data, _ = os.ReadFile(path)
	if string(data) != before {
		t.Fatal("failed apply changed profile")
	}
}

func TestSubscriptionApplyPreservesExistingLocalOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mixed-port: 7890\nproxies:\n  - name: New\n    type: direct\n"))
	}))
	defer server.Close()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Refresh(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	if err := profiles.Write(context.Background(), "work", []byte("mixed-port: 7000\nproxies:\n  - name: Old\n    type: direct\n")); err != nil {
		t.Fatal(err)
	}
	if err := profiles.SetOverride(context.Background(), "work", []byte("mixed-port: 7999\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := subscriptionCommand(context.Background(), []string{"subscription", "apply", "work"}, cache, profiles, nil); err != nil {
		t.Fatal(err)
	}
	source, err := profiles.Show("work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "7999") {
		t.Fatalf("subscription apply wrote local override into source: %s", source)
	}
	effective, err := profiles.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(effective), "mixed-port: 7999") {
		t.Fatalf("local override lost during subscription apply: %s", effective)
	}
	if saved, err := profiles.Override("work"); err != nil || string(saved) != "mixed-port: 7999\n" {
		t.Fatalf("override changed: %s, %v", saved, err)
	}
	if err := profiles.SetOverride(context.Background(), "work", []byte("mixed-port: 7999\n")); err != nil {
		t.Fatal(err)
	}
	failing := profile.NewStore(dir, func([]byte) error { return nil }, func(context.Context) error { return errors.New("reload failed") })
	if _, err := subscriptionCommand(context.Background(), []string{"subscription", "apply", "work"}, cache, failing, nil); err == nil {
		t.Fatal("expected reload failure")
	}
	if saved, err := failing.Override("work"); err != nil || string(saved) != "mixed-port: 7999\n" {
		t.Fatalf("failed apply did not restore override: %s, %v", saved, err)
	}
}

func TestSubscriptionApplyReplacesUnparseableOldProfile(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxies:\n  - name: New\n    type: direct\n"))
	}))
	defer server.Close()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Refresh(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	path := filepath.Join(dir, "profiles", "work.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid old profile"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := subscriptionCommand(ctx, []string{"subscription", "preview", "work"}, cache, profiles, nil); err == nil {
		t.Fatal("preview accepted unparseable old profile")
	}
	if _, err := subscriptionCommand(ctx, []string{"subscription", "apply", "work"}, cache, profiles, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "invalid old profile") || !strings.Contains(string(data), "name: New") {
		t.Fatalf("replaced profile: %s, %v", data, err)
	}
}
