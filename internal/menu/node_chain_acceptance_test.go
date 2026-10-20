package menu

import (
	"context"
	"io"
	"strings"
	"testing"
)

// The production CLI wraps its plan in data.plan. Keep that wire contract in
// this independent fixture instead of returning a plan directly as data.
type acceptanceChainEnvelopeRunner struct{ nodeChainRunner }

func (r *acceptanceChainEnvelopeRunner) Run(ctx context.Context, args []string) Result {
	result := r.nodeChainRunner.Run(ctx, args)
	if len(args) > 0 && args[len(args)-1] == "--dry-run" {
		result.Data = map[string]any{"profile": "work", "action": "clear", "plan": map[string]any{"kind": "node", "targets": []string{"Target Node"}, "via": []string{"First Hop", "Last Hop"}, "changes": []any{map[string]any{"name": "First Hop", "before": "Old Hop", "after": ""}}, "shared_use": []any{map[string]any{"kind": "node", "name": "Shared Node", "via": "First Hop"}}, "conflicts": []string{"cycle detected"}}, "dry_run": true, "saved": false, "reloaded": false, "resource": "chain"}
	}
	return result
}

func TestAcceptanceMenuUsesWrappedCLIPlanAndBlocksConflictSubmission(t *testing.T) {
	r := &acceptanceChainEnvelopeRunner{nodeChainRunner: nodeChainRunner{conflict: true, nodes: []menuProxyNode{{Name: "First Hop", Type: "socks5"}, {Name: "Target Node", Type: "ss"}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	ctx := context.Background()
	draft := localizedchainDraftPage(s.language)
	v, err := draft.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"First Hop", "Target Node"} {
		for _, item := range v.Items {
			if item.Name == name {
				if err := item.Action(ctx, s); err != nil {
					t.Fatal(err)
				}
			}
		}
		v, err = draft.Load(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"cycle detected", "First Hop"} {
		if !strings.Contains(v.Context, want) {
			t.Fatalf("wrapped CLI plan lost %q: %s", want, v.Context)
		}
	}
	if _, err := v.Actions[0].Navigate(ctx, s); err != nil {
		t.Fatal(err)
	}
	assertNoChainMutation(t, r.calls)
}
