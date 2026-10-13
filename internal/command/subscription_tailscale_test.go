package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestSubscriptionTailscaleAssociationHashAndDetach(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", "https://example.com/feed"); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	source := []byte("proxies: [{name: A,type: socks5,server: example.com,port: 1080}]\n")
	if err := profiles.Write(ctx, "work", source); err != nil {
		t.Fatal(err)
	}
	key, exit := "test-key", "100.64.0.1"
	if err := profiles.MutateTailscale(ctx, "work", "add", "TS", "", "", profile.TailscaleOptions{AuthKey: &key, ExitNode: &exit}); err != nil {
		t.Fatal(err)
	}
	if err := mutateSubscriptionTailscale(ctx, cache, profiles, "work", "bind", "TS", []string{"work"}); err == nil {
		t.Fatal("binding detached feed succeeded")
	}
	if err := cache.RecordApplySuccess("work", "work", "cached-version", source); err != nil {
		t.Fatal(err)
	}
	if err := mutateSubscriptionTailscale(ctx, cache, profiles, "work", "bind", "TS", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Find("work")
	if err != nil {
		t.Fatal(err)
	}
	if entry.SourceHash != subscription.Hash(source) {
		t.Fatal("binding altered source hash")
	}
	if _, err := subscriptionCommand(ctx, []string{"subscription", "remove", "work"}, cache, nil, nil); err == nil {
		t.Fatal("bound feed removal succeeded")
	}
	// External source edits block a new binding but do not trap unbind recovery.
	changed := []byte("proxies: [{name: C,type: socks5,server: example.com,port: 1080}]\n")
	if err := os.WriteFile(filepath.Join(dir, "profiles", "work.yaml"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := mutateSubscriptionTailscale(ctx, cache, profiles, "work", "bind", "TS", []string{"work"}); err == nil {
		t.Fatal("changed source bind succeeded")
	}
	if err := mutateSubscriptionTailscale(ctx, cache, profiles, "work", "unbind", "", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := subscriptionCommand(ctx, []string{"subscription", "remove", "work"}, cache, nil, nil); err != nil {
		t.Fatal(err)
	}
}
func TestProfileImportClearsSubscriptionTailscalePolicy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", "https://example.com/feed"); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	source := []byte("proxies: [{name: A,type: socks5}]\n")
	if err := profiles.Write(ctx, "work", source); err != nil {
		t.Fatal(err)
	}
	if err := cache.RecordApplySuccess("work", "work", "cache-version", source); err != nil {
		t.Fatal(err)
	}
	key, exit := "test-key", "100.64.0.1"
	if err := profiles.MutateTailscale(ctx, "work", "add", "TS", "", "", profile.TailscaleOptions{AuthKey: &key, ExitNode: &exit}); err != nil {
		t.Fatal(err)
	}
	if err := mutateSubscriptionTailscale(ctx, cache, profiles, "work", "bind", "TS", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "import.yaml")
	if err := os.WriteFile(file, []byte("proxies: [{name: NEW,type: socks5}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := profileCommand(ctx, []string{"profile", "import", "work", file}, profiles, cache); err != nil {
		t.Fatal(err)
	}
	policy, err := profiles.SubscriptionTailscale("work")
	if err != nil || policy != nil {
		t.Fatalf("policy retained: %v", err)
	}
	entry, _ := cache.Find("work")
	if entry.Profile != "" {
		t.Fatal("association retained")
	}
}
