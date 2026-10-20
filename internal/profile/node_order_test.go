package profile

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ronigooja/Nagi/internal/subscription"
)

func TestProxyNodeGroupsFollowEffectiveProfileOrder(t *testing.T) {
	const groups = `proxy-groups:
 - {name: Z-explicit, type: select, proxies: [Alpha]}
 - {name: A-explicit, type: select, proxies: [Alpha]}
 - {name: Z-auto, type: select, include-all-proxies: true}
 - {name: A-auto, type: select, include-all-proxies: true}
 - {name: Z-excluded, type: select, include-all-proxies: true, exclude-filter: '^Alpha$'}
 - {name: A-excluded, type: select, include-all-proxies: true, exclude-filter: '^Alpha$'}
`
	want := ProxyNodeGroups{
		Name:      "Alpha",
		Explicit:  []string{"Z-explicit", "A-explicit"},
		Automatic: []string{"Z-auto", "A-auto"},
		Excluded:  []string{"Z-excluded", "A-excluded"},
		Available: []string{"Z-explicit", "A-explicit", "Z-auto", "A-auto", "Z-excluded", "A-excluded"},
	}
	for _, override := range []bool{false, true} {
		name := "source"
		if override {
			name = "override"
		}
		t.Run(name, func(t *testing.T) {
			source := "proxies: [{name: Alpha, type: socks5}]\n"
			if !override {
				source += groups
			} else {
				source += "proxy-groups: [{name: Source-only, type: select, proxies: [Alpha]}]\n"
			}
			s := tailscaleFixture(t, source)
			if override {
				if err := os.MkdirAll(filepath.Join(s.Dir, "overrides"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(s.Dir, "overrides", "work.yaml"), []byte(groups), 0600); err != nil {
					t.Fatal(err)
				}
			}
			candidates, err := s.ProxyNodeGroupCandidates("work")
			if err != nil || !reflect.DeepEqual(candidates, want.Available) {
				t.Fatalf("candidates=%v err=%v", candidates, err)
			}
			got, err := s.ProxyNodeGroups("work", "Alpha")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("groups=%+v want=%+v err=%v", got, want, err)
			}
		})
	}
}

func TestProxyNodesFollowAppliedSubscriptionThenTypeAndName(t *testing.T) {
	source := []byte(`proxies:
 - {name: Z-source, type: vmess}
 - {name: A-source, type: socks5}
proxy-groups: []
`)
	s := tailscaleFixture(t, string(source))
	subs := subscription.NewStore(s.Dir, filepath.Join(t.TempDir(), "cache"), nil)
	if err := subs.Add("feed", "https://example.invalid/feed"); err != nil {
		t.Fatal(err)
	}
	if err := subs.RecordApplySuccess("feed", "work", "applied-version", source); err != nil {
		t.Fatal(err)
	}
	// Local snapshots can reorder source nodes; only the applied source defines their order.
	patch := []byte(`proxies:
 - {name: A-source, type: socks5}
 - {name: Z-tail, type: tailscale, exit-node: exit}
 - {name: Z-source, type: vmess}
 - {name: Z-local, type: socks5}
 - {name: A-local, type: socks5}
`)
	if err := s.SetOverride(context.Background(), "work", patch); err != nil {
		t.Fatal(err)
	}
	assertOrder := func(want []string, subscriptionCount int) {
		t.Helper()
		nodes, err := s.ProxyNodes("work")
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for i, n := range nodes {
			names = append(names, n.Name)
			if (i < subscriptionCount) != (n.Subscription == "feed") {
				t.Fatalf("incorrect source: %+v", nodes)
			}
		}
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("names=%v want=%v", names, want)
		}
	}
	assertOrder([]string{"Z-source", "A-source", "A-local", "Z-local", "Z-tail"}, 2)
	// A changed source must not inherit stale subscription ownership.
	if err := os.WriteFile(filepath.Join(s.Dir, "profiles", "work.yaml"), append(source, []byte("mode: global\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	assertOrder([]string{"A-local", "A-source", "Z-local", "Z-tail", "Z-source"}, 0)
}

func TestProxyNodeProviderCandidatesKeepSourceThenOverrideOrder(t *testing.T) {
	s := tailscaleFixture(t, `proxy-providers:
  Z-source: {type: inline, payload: []}
  A-source: {type: inline, payload: []}
proxy-groups: []
`)
	if err := s.SetOverride(context.Background(), "work", []byte(`proxy-providers:
  Z-local: {type: inline, payload: []}
  A-source: {interval: 60}
  A-local: {type: inline, payload: []}
`)); err != nil {
		t.Fatal(err)
	}
	names, err := s.ProxyNodeProviderCandidates("work")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Z-source", "A-source", "Z-local", "A-local"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names=%v want=%v", names, want)
	}
}
