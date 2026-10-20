package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func decodeChainPlan(t *testing.T, value string) menuChainPlan {
	t.Helper()
	var plan menuChainPlan
	if err := json.Unmarshal([]byte(value), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestChainPathEffectsUseActualChangesAndIgnoreGroupReferences(t *testing.T) {
	chains := []menuChainTarget{
		{Kind: "node", Name: "A"}, {Kind: "node", Name: "B", Via: []string{"A"}},
		{Kind: "node", Name: "C", Via: []string{"A", "B"}},
		{Kind: "node", Name: "D", Via: []string{"A", "B", "C"}},
		{Kind: "provider", Name: "feed", Via: []string{"A", "B", "C"}},
		{Kind: "group", Name: "Auto"},
		{Kind: "subscription", Name: "work", Via: []string{"A", "B", "C"}},
	}
	plan := decodeChainPlan(t, `{"kind":"node","targets":["C"],"via":[],"changes":[{"kind":"node","name":"C","before":"B","after":""}],"shared_use":[{"kind":"group","name":"Auto","via":"C"}]}`)
	s := NewSession(&nodeChainRunner{}, strings.NewReader(""), io.Discard)
	effects := chainPathEffects(s.language, chains, plan)
	if len(effects) != 2 {
		t.Fatal(effects)
	}
	for i, name := range []string{"D", "feed"} {
		want := "This device → A → B → C → " + name + " → Destination ⇒ This device → C → " + name + " → Destination"
		if effects[i] != want {
			t.Fatal(effects)
		}
	}
	plan.Changes = nil
	if got := chainPathEffects(s.language, chains, plan); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestChainPathEffectsPreserveLiteralGroupDialerWithoutInferringSelection(t *testing.T) {
	chains := []menuChainTarget{{Kind: "node", Name: "A", Via: []string{"Group"}}, {Kind: "node", Name: "B", Via: []string{"Group", "A"}}, {Kind: "node", Name: "C"}}
	plan := decodeChainPlan(t, `{"kind":"node","targets":["C"],"via":["A"],"changes":[{"kind":"node","name":"A","before":"Group","after":""},{"kind":"node","name":"C","before":"","after":"A"}]}`)
	s := NewSession(&nodeChainRunner{}, strings.NewReader(""), io.Discard)
	got := chainPathEffects(s.language, chains, plan)
	if len(got) != 1 || got[0] != "This device → Group → A → B → Destination ⇒ This device → A → B → Destination" {
		t.Fatal(got)
	}
}

func TestChainEffectsAreSummarizedAndPaginated(t *testing.T) {
	s := NewSession(&nodeChainRunner{}, strings.NewReader(""), io.Discard)
	effects := []string{}
	for i := 0; i < 15; i++ {
		effects = append(effects, fmt.Sprintf("Old%d ⇒ New%d", i, i))
	}
	v := PageView{Context: "path"}
	appendChainEffects(s.language, &v, effects)
	if strings.Contains(v.Context, "Old") || !strings.Contains(v.Context, "15") || len(v.Actions) != 1 {
		t.Fatal(v)
	}
	details := loadChainPage(t, v.Actions[0].Next(), s)
	if len(details.Items) != 15 || len(numberedChoices(details, 0)) != 11 || numberedChoices(details, 0)[10].PageDelta != 1 {
		t.Fatal(details)
	}
}

// Retain real edge mutation semantics in a scripted runner, so the navigation
// journey checks refreshed list paths rather than just a static success envelope.
type pathJourneyRunner struct{ nodeChainRunner }

func (r *pathJourneyRunner) Run(ctx context.Context, args []string) Result {
	result := r.nodeChainRunner.Run(ctx, args)
	if len(args) > 4 && args[0] == "proxy" && args[1] == "chain" && args[2] == "set" && args[len(args)-1] != "--dry-run" && result.Code == 0 {
		var via []string
		for i := 5; i+1 < len(args); i += 2 {
			via = append(via, args[i+1])
		}
		for i := range r.chains {
			if r.chains[i].Kind == "node" && r.chains[i].Name == args[4] {
				r.chains[i].Via = append([]string{}, via...)
			}
		}
	}
	return result
}
func TestChainApplyAndDisconnectReturnToRefreshedList(t *testing.T) {
	for _, journey := range []struct {
		name, input, want string
		commits           int
	}{
		{"new", "2\n1\n2\n3\n0\n", "This device → A → B → Destination", 1},
		{"disconnect", "1\n1\n0\n", "Chain disconnected.", 1},
	} {
		t.Run(journey.name, func(t *testing.T) {
			r := &pathJourneyRunner{nodeChainRunner: nodeChainRunner{chains: []menuChainTarget{{Kind: "node", Name: "A", Type: "ss"}, {Kind: "node", Name: "B", Type: "ss", Via: []string{"A"}}, {Kind: "node", Name: "C", Type: "ss"}}}}
			// The new-draft journey has three candidates, so Apply is choice 4.
			input := journey.input
			if journey.name == "new" {
				input = "2\n1\n2\n4\n0\n"
			}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(r, strings.NewReader(input), &out), localizedchainScopePage(NewSession(r, strings.NewReader(""), io.Discard).language))
			commits := 0
			for _, args := range r.calls {
				if len(args) > 2 && (args[2] == "set" || args[2] == "clear") && args[len(args)-1] != "--dry-run" {
					commits++
				}
			}
			if commits != journey.commits || !strings.Contains(out.String(), journey.want) {
				t.Fatalf("calls=%v\n%s", r.calls, out.String())
			}
			if journey.commits > 0 {
				marker := strings.LastIndex(out.String(), "Chain saved.")
				if journey.name == "disconnect" {
					marker = strings.LastIndex(out.String(), "Chain disconnected.")
					if marker < 0 || !strings.HasPrefix(out.String()[marker:], "Chain disconnected.\nChoice: ") {
						t.Fatal(out.String())
					}
				}
				lastPage := strings.LastIndex(out.String(), "\nNode Chains\n")
				if marker < 0 || lastPage < 0 || strings.Contains(out.String()[lastPage:], "Edit path") || !strings.Contains(out.String()[lastPage:], "New chain") {
					t.Fatal(out.String())
				}
			}
		})
	}
}

func TestChainFreshPlanConflictsBlockPreviouslyValidDraft(t *testing.T) {
	r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "A", Type: "ss"}, {Name: "B", Type: "ss"}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	p := localizedchainDraftPage(s.language)
	chooseChainItem(t, loadChainPage(t, p, s), "A", s)
	chooseChainItem(t, loadChainPage(t, p, s), "B", s)
	v := loadChainPage(t, p, s)
	r.conflict = true
	invokeChainAction(t, v, "chain.apply", s)
	assertNoChainMutation(t, r.calls)
	if !loadChainPage(t, p, s).Items[0].Selected {
		t.Fatal("draft lost")
	}
}
