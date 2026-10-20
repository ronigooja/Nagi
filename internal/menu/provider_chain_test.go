package menu

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

type providerMenuRunner struct {
	nodeChainRunner
	unavailable []string
	outlet      string
	groups      []string
}

func (r *providerMenuRunner) Run(ctx context.Context, args []string) Result {
	result := r.nodeChainRunner.Run(ctx, args)
	if strings.Join(args, " ") == "proxy chain list" {
		result.Data = map[string]any{"chains": r.chains, "unavailable_providers": r.unavailable}
	}
	if len(args) > 2 && args[2] == "set" && args[len(args)-1] != "--dry-run" && result.Code == 0 {
		result.Data = map[string]any{"saved": true, "plan": map[string]any{"outlet": r.outlet, "outlet_groups": r.groups}}
	}
	return result
}
func TestProviderChainNumericMixedIdentityOrderAndOutletFeedback(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		r := &providerMenuRunner{nodeChainRunner: nodeChainRunner{chains: []menuChainTarget{
			{Kind: "node", Name: "Same", Type: "ss"},
			{Kind: "provider-node", Provider: "Feed B", Name: "Same", Type: "socks5"},
			{Kind: "provider-node", Provider: "Feed A", Name: "Same", Type: "ss"},
		}}, outlet: "Same [Feed A] chain", groups: []string{"Manual"}}
		var out bytes.Buffer
		s := NewSessionWithLanguage(r, strings.NewReader("2\n1\n3\n4\n0\n"), &out, i18n.New(language))
		RunPage(context.Background(), s, localizedchainDraftPage(s.language))
		want := []string{"proxy", "chain", "set", "provider-node", "Feed A", "Same", "--via-provider", "Feed B", "Same", "--via", "Same"}
		if countMenuCall(r.calls, want) != 1 {
			t.Fatalf("%v\n%s", r.calls, out.String())
		}
		for _, label := range []string{"Same (socks5) — Feed B", "Same (ss) — Feed A", "Same [Feed B] → Same → Same [Feed A]", "Same [Feed A] chain (Manual)"} {
			if !strings.Contains(out.String(), label) {
				t.Fatalf("missing %q: %s", label, out.String())
			}
		}
	}
}
func TestProviderChainListAndDisconnectUseSourceIdentity(t *testing.T) {
	r := &providerMenuRunner{nodeChainRunner: nodeChainRunner{chains: []menuChainTarget{
		{Kind: "node", Name: "Entry", Type: "ss"},
		{Kind: "provider-node", Provider: "First", Name: "Same", Type: "ss", PathRefs: []menuChainRef{{Kind: "node", Name: "Entry"}, {Kind: "provider-node", Provider: "First", Name: "Same"}}},
		{Kind: "provider-node", Provider: "Second", Name: "Same", Type: "ss", PathRefs: []menuChainRef{{Kind: "node", Name: "Entry"}, {Kind: "provider-node", Provider: "Second", Name: "Same"}}},
	}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	v := loadChainPage(t, localizedchainScopePage(s.language), s)
	if len(v.Items) != 2 || !strings.Contains(v.Items[0].Label, "Same [First]") || !strings.Contains(v.Items[1].Label, "Same [Second]") {
		t.Fatal(v)
	}
	detail := loadChainPage(t, v.Items[1].Next(), s)
	invokeChainAction(t, detail, "chain.clear", s)
	if countMenuCall(r.calls, []string{"proxy", "chain", "clear", "provider-node", "Second", "Same"}) != 1 {
		t.Fatal(r.calls)
	}
}
func TestProviderUnavailableAndUnsupportedCannotEnterDraft(t *testing.T) {
	supported := false
	r := &providerMenuRunner{nodeChainRunner: nodeChainRunner{chains: []menuChainTarget{
		{Kind: "node", Name: "Entry", Type: "ss"},
		{Kind: "provider-node", Provider: "Feed", Name: "Identity", Type: "tailscale", Supported: &supported, UnsupportedReason: "Tailscale identity cannot be duplicated."},
	}}, unavailable: []string{"No local cache"}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	v := loadChainPage(t, p, s)
	chooseChainItem(t, v, "Identity", s)
	v = loadChainPage(t, p, s)
	if v.Items[1].Selected || !strings.HasPrefix(v.Context, "This device → Destination") || !strings.Contains(v.Context, "Provider nodes unavailable: No local cache") {
		t.Fatal(v)
	}
	invokeChainAction(t, v, "chain.apply", s)
	assertNoChainMutation(t, r.calls)
}
func TestProviderDraftUnavailableReadRetainsIdentityWithoutSubmitting(t *testing.T) {
	r := &providerMenuRunner{nodeChainRunner: nodeChainRunner{chains: []menuChainTarget{{Kind: "provider-node", Name: "Same", Provider: "Feed", Type: "ss"}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	chooseChainItem(t, loadChainPage(t, p, s), "Same", s)
	r.chains = nil
	if v, err := p.Load(context.Background(), s); err != nil || len(v.Items) > 0 || len(v.Actions) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	r.chains = []menuChainTarget{{Kind: "provider-node", Name: "Same", Provider: "Feed", Type: "ss"}}
	if !loadChainPage(t, p, s).Items[0].Selected {
		t.Fatal("draft lost")
	}
	assertNoChainMutation(t, r.calls)
}

func TestIndependentProviderOutletIsRetainedAlongsideLongerPath(t *testing.T) {
	outlet := menuChainTarget{Kind: "provider-node", Provider: "Feed", Name: "Transit", Outlet: "Transit chain", PathRefs: []menuChainRef{{Kind: "node", Name: "Entry"}, {Kind: "provider-node", Provider: "Feed", Name: "Transit"}}}
	longer := menuChainTarget{Kind: "node", Name: "Exit", PathRefs: append(append([]menuChainRef{}, outlet.PathRefs...), menuChainRef{Kind: "node", Name: "Exit"})}
	if got := terminalNodeChains([]menuChainTarget{outlet, longer}); len(got) != 2 {
		t.Fatal(got)
	}
	outlet.Outlet = ""
	if got := terminalNodeChains([]menuChainTarget{outlet, longer}); len(got) != 1 || got[0].Name != "Exit" {
		t.Fatal(got)
	}
}

type transientProviderMenuRunner struct {
	providerMenuRunner
	reads int
}

func (r *transientProviderMenuRunner) Run(ctx context.Context, args []string) Result {
	result := r.providerMenuRunner.Run(ctx, args)
	if strings.Join(args, " ") == "proxy chain list" {
		r.reads++
		// After choosing provider A, the next read loses only that source.
		if r.reads == 2 {
			result.Data = map[string]any{"chains": r.chains[1:], "unavailable_providers": []string{"Feed A"}}
		}
	}
	return result
}
func TestProviderTransientLossRetainsDraftThroughRealNumericJourney(t *testing.T) {
	r := &transientProviderMenuRunner{providerMenuRunner: providerMenuRunner{nodeChainRunner: nodeChainRunner{chains: []menuChainTarget{
		{Kind: "provider-node", Name: "A", Provider: "Feed A", Type: "ss"},
		{Kind: "node", Name: "B", Type: "socks5"},
	}}}}
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader("1\n1\n3\n0\n"), &out)
	RunPage(context.Background(), s, localizedchainDraftPage(s.language))
	want := []string{"proxy", "chain", "set", "node", "B", "--via-provider", "Feed A", "A"}
	if countMenuCall(r.calls, want) != 1 || !strings.Contains(out.String(), "A selected node is unavailable. The draft is retained.") || !strings.Contains(out.String(), "A [Feed A] → B → Destination") {
		t.Fatalf("%v\n%s", r.calls, out.String())
	}
}
