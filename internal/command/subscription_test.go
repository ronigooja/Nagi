package command

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestSubscriptionPreviewApplyAndRollback(t *testing.T) {
	source := "proxies:\n  - name: New\n    type: direct\n"
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
	old := "mixed-port: 7000\nrules:\n  - MATCH,DIRECT\nproxies:\n  - name: Old\n    type: direct\n"
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
	if !strings.Contains(string(data), "mixed-port: 7000") || !strings.Contains(string(data), "MATCH,DIRECT") || !strings.Contains(string(data), "name: New") {
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
