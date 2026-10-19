package menu

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func TestRewriteMaximumDocumentMenuSensitiveEdit(t *testing.T) {
	document := rewrite.Document{Version: 2, Scripts: []rewrite.Script{}, Rules: []rewrite.Rule{{ID: "maximum", Enabled: true, Stage: rewrite.StageRequest, RangeMode: "fragment", Match: rewrite.Match{Domain: "api.service.example"}, OnError: "block", RedirectConsent: []rewrite.Consent{}, Operations: []rewrite.Operation{{Op: "body_set", Encoding: "text", Value: json.RawMessage(`""`)}}}}}
	empty, _ := json.Marshal(document)
	value, _ := json.Marshal(strings.Repeat("x", rewrite.MaxDocumentBytes-len(empty)))
	document.Rules[0].Operations[0].Value = value
	raw, _ := json.Marshal(document)
	runner := &rewriteTestRunner{run: func([]string) Result {
		return Result{Data: map[string]any{"profile": "work", "rule": rewrite.RuleSummary{ID: "maximum", Stage: rewrite.StageRequest, Enabled: true}}}
	}, raw: Result{Data: string(raw)}}
	session := NewSession(runner, strings.NewReader(""), io.Discard)
	view := rewriteLoad(t, rewriteRuleDetail("work", "maximum"), session)
	nav, e := rewriteFind(t, view, "Edit rule").Navigate(context.Background(), session)
	if e != nil || nav.Next == nil {
		t.Fatal("maximum export not editable", e)
	}
	draft := rewriteLoad(t, nav.Next, session)
	if !strings.Contains(draft.Context, "Operations: 1") || len(runner.rawCalls) != 1 || len(runner.inputs) != 0 {
		t.Fatal("draft mutated persistent state", draft, runner.calls)
	}
}
