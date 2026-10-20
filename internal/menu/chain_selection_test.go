package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func loadChainPage(t *testing.T, page *Page, s *Session) PageView {
	t.Helper()
	v, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func chooseChainItem(t *testing.T, v PageView, name string, s *Session) {
	t.Helper()
	for _, item := range v.Items {
		if item.Name == name {
			if item.Navigate != nil {
				if _, err := item.Navigate(context.Background(), s); err != nil {
					t.Fatal(err)
				}
			} else if err := item.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("node %q unavailable: %+v", name, v.Items)
}

func invokeChainAction(t *testing.T, v PageView, id string, s *Session) {
	t.Helper()
	for _, item := range v.Actions {
		if item.ID == id {
			if item.Navigate != nil {
				if _, err := item.Navigate(context.Background(), s); err != nil {
					t.Fatal(err)
				}
			} else if err := item.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("action %q unavailable: %+v", id, v.Actions)
}

func TestChainSelectionCancellationAndReselectionChangeTrafficOrder(t *testing.T) {
	r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "A", Type: "ss"}, {Name: "B", Type: "socks5"}, {Name: "C", Type: "ss"}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	for _, name := range []string{"A", "B", "A", "C", "A"} {
		chooseChainItem(t, loadChainPage(t, p, s), name, s)
	}
	v := loadChainPage(t, p, s)
	if !strings.HasPrefix(v.Context, "This device → B → C → A → Destination") {
		t.Fatal(v.Context)
	}
	invokeChainAction(t, v, "chain.apply", s)
	want := []string{"proxy", "chain", "set", "node", "A", "--via", "B", "--via", "C"}
	if countMenuCall(r.calls, want) != 1 {
		t.Fatal(r.calls)
	}
}

func TestChainNewDraftRetainsOrderedGreenSelectionAndReset(t *testing.T) {
	r := &nodeChainRunner{chains: []menuChainTarget{{Kind: "node", Name: "A", Type: "ss"}, {Kind: "node", Name: "B", Type: "ss", Via: []string{"A"}}, {Kind: "node", Name: "C", Type: "ss", Via: []string{"A", "B"}}}}
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader(""), &out)
	p := localizedchainDraftPage(s.language)
	for _, name := range []string{"A", "B", "C"} {
		chooseChainItem(t, loadChainPage(t, p, s), name, s)
	}
	v := loadChainPage(t, p, s)
	if len(v.Items) != 3 || !v.Items[0].Selected || !v.Items[1].Selected || !v.Items[2].Selected || !strings.HasPrefix(v.Context, "This device → A → B → C → Destination") {
		t.Fatal(v)
	}
	s.styles = true
	s.render(p, v, 0, 2)
	for _, item := range v.Items {
		assertMenuStyled(t, out.String(), item.Label, "32")
	}
	chooseChainItem(t, v, "A", s)
	chooseChainItem(t, loadChainPage(t, p, s), "A", s)
	v = loadChainPage(t, p, s)
	if !strings.HasPrefix(v.Context, "This device → B → C → A → Destination") {
		t.Fatal(v)
	}
	invokeChainAction(t, v, "chain.reset", s)
	v = loadChainPage(t, p, s)
	if !strings.HasPrefix(v.Context, "This device → Destination") {
		t.Fatal(v)
	}
	invokeChainAction(t, v, "chain.apply", s)
	assertNoChainMutation(t, r.calls)
}

func TestChainNewDraftNeverLoadsExistingPaths(t *testing.T) {
	r := &nodeChainRunner{chains: []menuChainTarget{{Kind: "node", Name: "A"}, {Kind: "node", Name: "B", Via: []string{"A"}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	v := loadChainPage(t, p, s)
	if v.Context != "This device → Destination" {
		t.Fatal(v)
	}
	chooseChainItem(t, v, "B", s)
	v = loadChainPage(t, p, s)
	if v.Context != "This device → B → Destination" || v.Items[0].Selected {
		t.Fatal(v)
	}
	invokeChainAction(t, v, "chain.reset", s)
	if loadChainPage(t, p, s).Context != "This device → Destination" {
		t.Fatal("reset failed")
	}
	assertNoChainMutation(t, r.calls)
}

func TestChainEmptySingleAndEOFNeverMutate(t *testing.T) {
	for _, input := range []string{"", "3\n0\n", "1\n3\n0\n", "1\n2\n0\n", "1\n2\n1\n2\n3\n0\n"} {
		r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "A", Type: "ss"}, {Name: "B", Type: "ss"}}}
		RunPage(context.Background(), NewSession(r, strings.NewReader(input), io.Discard), localizedchainDraftPage(i18n.New(i18n.English)))
		assertNoChainMutation(t, r.calls)
	}
}

func TestChainDisconnectUsesDetailExitAndRetainsOnFailure(t *testing.T) {
	r := &nodeChainRunner{fail: 1, chains: []menuChainTarget{{Kind: "node", Name: "A"}, {Kind: "node", Name: "B", Via: []string{"A"}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDetailPage(s.language, "B")
	v := loadChainPage(t, p, s)
	if len(v.Actions) != 1 || v.Actions[0].ID != "chain.clear" {
		t.Fatal(v.Actions)
	}
	invokeChainAction(t, v, "chain.clear", s)
	if !strings.HasPrefix(loadChainPage(t, p, s).Context, "This device → A → B → Destination") {
		t.Fatal("failed clear changed detail")
	}
	invokeChainAction(t, loadChainPage(t, p, s), "chain.clear", s)
	if countMenuCall(r.calls, []string{"proxy", "chain", "clear", "node", "B"}) != 2 {
		t.Fatal(r.calls)
	}
	v = loadChainPage(t, p, s)
	if len(v.Actions) != 0 || !strings.Contains(v.Context, "This device → B → Destination") || !strings.Contains(v.Context, "Upstream segments") {
		t.Fatal(v)
	}
}

func TestChainMissingSelectedNodeBlocksActionsInsteadOfShorteningPath(t *testing.T) {
	r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "A", Type: "ss"}, {Name: "B", Type: "ss"}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	chooseChainItem(t, loadChainPage(t, p, s), "A", s)
	r.nodes = r.nodes[1:]
	v, err := p.Load(context.Background(), s)
	if err == nil || len(v.Items) != 0 || len(v.Actions) != 0 {
		t.Fatalf("%+v %v", v, err)
	}
	assertNoChainMutation(t, r.calls)
}

func TestChainNumericPaginationPreservesSelectionOrder(t *testing.T) {
	r := &nodeChainRunner{}
	for i := 0; i < 12; i++ {
		r.nodes = append(r.nodes, menuProxyNode{Name: fmt.Sprintf("Node%02d", i), Type: "ss"})
	}
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader("13\n2\n5\n1\n11\n0\n"), &out)
	RunPage(context.Background(), s, localizedchainDraftPage(s.language))
	want := []string{"proxy", "chain", "set", "node", "Node00", "--via", "Node11"}
	if countMenuCall(r.calls, want) != 1 || !strings.Contains(out.String(), "Node11 → Node00 → Destination") {
		t.Fatalf("%v\n%s", r.calls, out.String())
	}
}

func TestChainListShowsOnlyMaximalNodePathsAndNewAction(t *testing.T) {
	r := &nodeChainRunner{chains: []menuChainTarget{{Kind: "node", Name: "A"}, {Kind: "node", Name: "B", Via: []string{"A"}}, {Kind: "node", Name: "C", Via: []string{"A", "B"}}, {Kind: "node", Name: "D", Via: []string{"A"}}, {Kind: "provider", Name: "feed", Via: []string{"A", "B", "C"}}, {Kind: "subscription", Name: "work", Via: []string{"A", "B", "C"}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	v := loadChainPage(t, localizedchainScopePage(s.language), s)
	if len(v.Items) != 2 || v.Items[0].Name != "C" || v.Items[1].Name != "D" || len(v.Actions) != 1 || v.Actions[0].ID != "chain.new" {
		t.Fatal(v)
	}
	invokeChainAction(t, loadChainPage(t, localizedchainDetailPage(s.language, "C"), s), "chain.clear", s)
	v = loadChainPage(t, localizedchainScopePage(s.language), s)
	if len(v.Items) != 2 || v.Items[0].Name != "B" {
		t.Fatal(v)
	}
}

func TestChainEmptyListOffersNewDraft(t *testing.T) {
	r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "A", Type: "ss"}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	v := loadChainPage(t, localizedchainScopePage(s.language), s)
	if len(v.Items) != 0 || v.Empty != "No chains yet." || len(v.Actions) != 1 {
		t.Fatal(v)
	}
	draft := loadChainPage(t, v.Actions[0].Next(), s)
	if len(draft.Items) != 1 || draft.Items[0].Selected {
		t.Fatal(draft)
	}
}

type chainPlanResultRunner struct {
	nodeChainRunner
	result Result
}

func (r *chainPlanResultRunner) Run(ctx context.Context, args []string) Result {
	if len(args) > 0 && args[len(args)-1] == "--dry-run" {
		r.calls = append(r.calls, append([]string{}, args...))
		return r.result
	}
	return r.nodeChainRunner.Run(ctx, args)
}

func TestChainInvalidOrFailedPlansNeverSubmit(t *testing.T) {
	for _, result := range []Result{
		{Data: map[string]any{"changes": []any{}}},
		{Data: map[string]any{"plan": nil}},
		{Data: map[string]any{"plan": "invalid"}},
		{Code: 1, Error: &CLIError{Message: "validation failed https://secret.invalid/token"}},
	} {
		r := &chainPlanResultRunner{result: result}
		var out bytes.Buffer
		s := NewSession(r, strings.NewReader(""), &out)
		if applyMenuChain(context.Background(), s, []string{"proxy", "chain", "set", "node", "B", "--via", "A"}) {
			t.Fatal("invalid plan accepted")
		}
		assertNoChainMutation(t, r.calls)
		if strings.Contains(out.String(), "secret.invalid") {
			t.Fatal(out.String())
		}
	}
}

func TestChainConflictDescriptorsRemainLocalized(t *testing.T) {
	descriptor, err := json.Marshal(i18n.Descriptor{ID: "backend.profile.chain_policy_owns_edge"})
	if err != nil {
		t.Fatal(err)
	}
	r := &chainPlanResultRunner{result: Result{
		Data:     map[string]any{"plan": map[string]any{"conflicts": []string{"policy owns changed edge"}}},
		Messages: map[string]json.RawMessage{"/data/plan/conflicts/0": descriptor},
	}}
	var out bytes.Buffer
	s := NewSessionWithLanguage(r, strings.NewReader(""), &out, i18n.New(i18n.SimplifiedChinese))
	if applyMenuChain(context.Background(), s, []string{"proxy", "chain", "set", "node", "B", "--via", "A"}) {
		t.Fatal("conflict accepted")
	}
	if !strings.Contains(out.String(), "订阅策略") || strings.Contains(out.String(), "policy owns changed edge") {
		t.Fatal(out.String())
	}
	assertNoChainMutation(t, r.calls)
}

func TestChainReselectingAfterEmptyDraftDoesNotRestoreCancelledMembers(t *testing.T) {
	r := &nodeChainRunner{chains: []menuChainTarget{{Kind: "node", Name: "A", Type: "ss"}, {Kind: "node", Name: "B", Type: "ss", Via: []string{"A"}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	for _, name := range []string{"A", "B", "A", "B", "B"} {
		chooseChainItem(t, loadChainPage(t, p, s), name, s)
	}
	v := loadChainPage(t, p, s)
	if !strings.HasPrefix(v.Context, "This device → B → Destination") || v.Items[0].Selected || !v.Items[1].Selected {
		t.Fatal(v)
	}
	assertNoChainMutation(t, r.calls)
}
