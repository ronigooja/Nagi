package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/profile"
	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestSubscriptionChainAssociationHashAndDetach(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cache := subscription.NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := cache.Add("work", "https://example.com/feed"); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewStore(dir, func([]byte) error { return nil }, nil)
	source := []byte("proxies: [{name: A, type: socks5, server: example.com, port: 1080}]\n")
	if err := profiles.Write(ctx, "work", source); err != nil {
		t.Fatal(err)
	}
	key, exit := "fixture-key", "100.64.0.1"
	if err := profiles.AddProxyNode(ctx, "work", "TS", profile.TailscaleOptions{AuthKey: &key, ExitNode: &exit}, nil); err != nil {
		t.Fatal(err)
	}
	args := []string{"proxy", "chain", "set", "subscription", "work", "--via", "TS"}
	if _, err := chainCommand(ctx, args, profiles, cache, "work", false); err == nil {
		t.Fatal("detached subscription accepted")
	}
	if err := cache.RecordApplySuccess("work", "work", "cached-version", source); err != nil {
		t.Fatal(err)
	}
	targets, err := chainSubscriptions(cache, profiles, "work")
	if err != nil || len(targets) != 1 || len(targets[0].Via) != 0 {
		t.Fatalf("unbound associated target missing: %v %v", targets, err)
	}
	if _, err := chainCommand(ctx, args, profiles, cache, "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := subscriptionCommand(ctx, []string{"subscription", "remove", "work"}, cache, nil, nil); err == nil {
		t.Fatal("bound subscription removed")
	}
	changed := []byte("proxies: [{name: C, type: socks5, server: example.com, port: 1080}]\n")
	if err := os.WriteFile(filepath.Join(dir, "profiles", "work.yaml"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := chainCommand(ctx, args, profiles, cache, "work", false); err == nil {
		t.Fatal("source hash mismatch accepted")
	}
	if _, err := chainCommand(ctx, []string{"proxy", "chain", "clear", "subscription", "work"}, profiles, cache, "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := subscriptionCommand(ctx, []string{"subscription", "remove", "work"}, cache, nil, nil); err != nil {
		t.Fatal(err)
	}
}
