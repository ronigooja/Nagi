package profile

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const acceptanceChainSource = `proxies:
 - {name: Tail, type: tailscale, auth-key: FICTITIOUS, exit-node: example-exit}
 - {name: Socks, type: socks5, server: 127.0.0.1, port: 1080}
 - {name: WG, type: wireguard, server: 192.0.2.1, port: 51820}
 - {name: Target, type: vmess, server: example.invalid, uuid: FICTITIOUS}
proxy-groups: [{name: Select, type: select, proxies: [Target]}]
`

func TestAcceptanceChainDirectionsAndNonrecursiveClear(t *testing.T) {
	for _, via := range [][]string{{"Tail", "Socks"}, {"Socks", "Tail"}, {"Tail", "WG", "Socks"}} {
		t.Run(via[0]+via[len(via)-1], func(t *testing.T) {
			s := tailscaleFixture(t, acceptanceChainSource)
			if _, err := s.MutateProxyChain(context.Background(), "work", "node", []string{"Target"}, via, false); err != nil {
				t.Fatal(err)
			}
			cfg, err := s.chainConfig("work")
			if err != nil {
				t.Fatal(err)
			}
			for i, hop := range via {
				want := ""
				if i > 0 {
					want = via[i-1]
				}
				if got := tsString(tsFind(cfg, hop), "dialer-proxy"); got != want {
					t.Fatalf("%s dialer=%s want %s", hop, got, want)
				}
			}
			if got := tsString(tsFind(cfg, "Target"), "dialer-proxy"); got != via[len(via)-1] {
				t.Fatal(got)
			}
			if _, err = s.MutateProxyChain(context.Background(), "work", "node", []string{"Target"}, nil, true); err != nil {
				t.Fatal(err)
			}
			cfg, _ = s.chainConfig("work")
			if tsString(tsFind(cfg, "Target"), "dialer-proxy") != "" {
				t.Fatal("target edge survived clear")
			}
			if len(via) > 1 && tsString(tsFind(cfg, via[1]), "dialer-proxy") != via[0] {
				t.Fatal("clear recursively removed hop edge")
			}
		})
	}
}

func TestAcceptanceSubscriptionSwitchRetainsPreviousChain(t *testing.T) {
	s := tailscaleFixture(t, acceptanceChainSource)
	ctx := context.Background()
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"feed"}, []string{"Tail", "Socks"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"feed"}, []string{"WG"}, false); err != nil {
		t.Fatalf("switching policy chain rejected its own previous edges: %v", err)
	}
	if _, err := s.MutateProxyChain(ctx, "work", "subscription", []string{"feed"}, nil, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.chainConfig("work")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Tail", "Socks", "WG"} {
		if tsFind(cfg, n) == nil {
			t.Fatalf("clear lost %s", n)
		}
	}
}

func TestAcceptancePlanHasNoDiskOrActivationEffects(t *testing.T) {
	s := tailscaleFixture(t, acceptanceChainSource)
	activated := 0
	s.Reload = func(context.Context) error { activated++; return nil }
	before, _ := os.ReadDir(s.Dir)
	plan, err := s.PlanProxyChain("work", "node", []string{"Target"}, []string{"Tail", "Socks"}, false)
	if err != nil || len(plan.Changes) != 2 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	after, _ := os.ReadDir(s.Dir)
	names := func(entries []os.DirEntry) []string {
		out := []string{}
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}
	if activated != 0 || !reflect.DeepEqual(names(before), names(after)) {
		t.Fatal("plan wrote state or activated")
	}
	if _, err = os.Stat(filepath.Join(s.Dir, "overrides", "work.yaml")); !os.IsNotExist(err) {
		t.Fatalf("plan created override: %v", err)
	}
}

func TestAcceptanceProviderPayloadExternalEdgeRejectsEntireBatch(t *testing.T) {
	s := tailscaleFixture(t, acceptanceChainSource+`proxy-providers:
 clean: {type: inline, payload: [{name: InnerClean, type: socks5}]}
 foreign: {type: inline, payload: [{name: InnerForeign, type: socks5, dialer-proxy: Socks}]}
`)
	p, err := s.PlanProxyChain("work", "provider", []string{"clean", "foreign"}, []string{"Tail"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) == 0 {
		t.Fatalf("provider payload foreign edge missing from conflicts: %+v", p)
	}
	if _, err = s.MutateProxyChain(context.Background(), "work", "provider", []string{"clean", "foreign"}, []string{"Tail"}, false); err == nil {
		t.Fatal("provider batch overwrote foreign payload edge")
	}
	if _, err = os.Stat(filepath.Join(s.Dir, "overrides", "work.yaml")); !os.IsNotExist(err) {
		t.Fatalf("failed provider batch wrote override: %v", err)
	}
}

func TestAcceptanceRemovalProtectsGroupDialerReference(t *testing.T) {
	s := tailscaleFixture(t, `proxies:
 - {name: Tail, type: tailscale, auth-key: FICTITIOUS, exit-node: example-exit}
proxy-groups: [{name: Select, type: select, proxies: [DIRECT], dialer-proxy: Tail}]
`)
	if err := s.RemoveProxyNode(context.Background(), "work", "Tail"); err == nil {
		t.Fatal("removed node referenced by group dialer-proxy")
	}
}

func TestAcceptanceGraphRejectsIncludeAndProviderCycles(t *testing.T) {
	for _, source := range []string{
		`proxies: [{name: Hop, type: socks5}, {name: Third, type: socks5}, {name: Target, type: socks5, dialer-proxy: All}]
proxy-groups: [{name: All, type: select, include-all-proxies: true}]
`,
		`proxies: [{name: Hop, type: socks5}, {name: Third, type: socks5}, {name: Target, type: socks5, dialer-proxy: All}]
proxy-groups: [{name: All, type: select, use: [Feed]}]
proxy-providers: {Feed: {type: inline, payload: [{name: Inner, type: socks5, dialer-proxy: Target}]}}
`,
	} {
		s := tailscaleFixture(t, source)
		if _, err := s.PlanProxyChain("work", "node", []string{"Hop"}, []string{"Third"}, false); err == nil {
			t.Fatal("accepted indirect dependency cycle")
		}
	}
}

func TestAcceptanceProviderGraphNamespaceDoesNotCollideWithNodeName(t *testing.T) {
	s := tailscaleFixture(t, `proxies:
 - {name: 'provider:Feed', type: socks5}
 - {name: Hop, type: socks5}
 - {name: Target, type: vmess}
proxy-providers:
 Feed: {type: inline, override: {dialer-proxy: 'provider:Feed'}, payload: [{name: Inner, type: socks5}]}
`)
	if _, err := s.PlanProxyChain("work", "node", []string{"Target"}, []string{"Hop"}, false); err != nil {
		t.Fatalf("valid node/provider names confused by dependency graph: %v", err)
	}
}
