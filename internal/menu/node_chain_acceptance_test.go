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
	r := &acceptanceChainEnvelopeRunner{nodeChainRunner: nodeChainRunner{conflict: true}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	ctx := context.Background()
	draft := localizedchainDraftPage(s.language, "node", []string{"Target Node"})
	v, err := draft.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	nav, err := v.Actions[2].Navigate(ctx, s)
	if err != nil || nav.Next == nil {
		t.Fatalf("preview navigation %v %v", nav, err)
	}
	preview, err := nav.Next.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cycle detected", "Shared Node", "Old Hop", "First Hop"} {
		if !strings.Contains(preview.Context, want) {
			t.Fatalf("wrapped CLI plan lost %q: %s", want, preview.Context)
		}
	}
	if len(preview.Actions) != 0 {
		t.Fatalf("conflicting plan offers submission: %+v", preview.Actions)
	}
}
