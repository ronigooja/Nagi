package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

type rewriteTestRunner struct {
	calls    [][]string
	inputs   []string
	rawCalls [][]string
	run      func([]string) Result
	input    func([]string, string) Result
	raw      Result
}

func (f *rewriteTestRunner) Run(_ context.Context, args []string) Result {
	f.calls = append(f.calls, append([]string{}, args...))
	if f.run != nil {
		return f.run(args)
	}
	return Result{}
}
func (f *rewriteTestRunner) RunInput(_ context.Context, args []string, in io.Reader) Result {
	b, _ := io.ReadAll(in)
	f.calls = append(f.calls, append([]string{}, args...))
	f.inputs = append(f.inputs, string(b))
	if f.input != nil {
		return f.input(args, string(b))
	}
	return Result{Data: map[string]any{"saved": true}}
}
func (f *rewriteTestRunner) RunRaw(_ context.Context, args []string) Result {
	f.rawCalls = append(f.rawCalls, append([]string{}, args...))
	return f.raw
}
func rewriteStatus(profile string, tun any) Result {
	return Result{Data: map[string]any{"profile": profile, "enabled_intent": false, "actual": "unavailable", "tun_running": tun, "service_running": nil, "ca": map[string]any{"state": "missing", "trust": "unknown"}}}
}
func rewriteFind(t *testing.T, view PageView, label string) Item {
	t.Helper()
	for _, i := range append(view.Items, view.Actions...) {
		if i.Label == label {
			return i
		}
	}
	t.Fatalf("missing %s in %+v", label, view)
	return Item{}
}
func rewriteLoad(t *testing.T, p *Page, s *Session) PageView {
	t.Helper()
	v, e := p.Load(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func rewriteRun(t *testing.T, page *Page, f Runner, input string) string {
	t.Helper()
	var out bytes.Buffer
	if code := RunPage(context.Background(), NewSession(f, strings.NewReader(input), &out), page); code != 0 {
		t.Fatalf("code %d: %s", code, out.String())
	}
	return out.String()
}

func TestRewriteStatusUnknownAndTUNGuard(t *testing.T) {
	for _, tun := range []any{nil, false} {
		f := &rewriteTestRunner{run: func([]string) Result { return rewriteStatus("work", tun) }}
		s := NewSession(f, strings.NewReader(""), io.Discard)
		v := rewriteLoad(t, RequestRewritePage(), s)
		if !strings.Contains(v.Context, "Enable unavailable") || !strings.Contains(v.Context, "Service: unknown") {
			t.Fatal(v.Context)
		}
		for _, a := range v.Actions {
			if a.Label == "Enable request rewrite" {
				t.Fatal("enable exposed without actual TUN")
			}
		}
	}
	f := &rewriteTestRunner{run: func([]string) Result { return rewriteStatus("other", true) }}
	s := NewSession(f, strings.NewReader(""), io.Discard)
	_ = rewriteRuntimeItem("work", true).Action(context.Background(), s)
	if len(f.calls) != 1 {
		t.Fatal(f.calls)
	}
	f.run = func([]string) Result { return rewriteStatus("work", false) }
	_ = rewriteRuntimeItem("work", true).Action(context.Background(), s)
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
}
func TestRewriteScriptsRetainRetryLiteralZeroAndProfile(t *testing.T) {
	submissions := 0
	f := &rewriteTestRunner{input: func(a []string, b string) Result {
		submissions++
		if submissions == 1 {
			return rewriteFailure("rewrite_invalid")
		}
		return Result{}
	}}
	var out bytes.Buffer
	s := NewSession(f, strings.NewReader(`{"id":"0","body_access":"none","source":"function rewrite(input) { return {}; }"}`+"\n1\n2\n"), &out)
	err := rewriteScriptForm("work", nil)(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.inputs) != 2 || f.inputs[0] != f.inputs[1] || !strings.Contains(f.inputs[0], `"id":"0"`) {
		t.Fatal(f.inputs)
	}
	for _, a := range f.calls {
		if !reflect.DeepEqual(a, []string{"request-rewrite", "scripts", "add", "-", "--profile", "work"}) {
			t.Fatal(a)
		}
	}
	if !strings.Contains(out.String(), "Values are retained") {
		t.Fatal(out.String())
	}
}
func TestRewriteDraftNoMutationBeforeExplicitSaveAndRetainedFailure(t *testing.T) {
	f := &rewriteTestRunner{input: func([]string, string) Result { return rewriteFailure("rewrite_invalid") }}
	initial := rewrite.Rule{ID: "demo", Enabled: true, Stage: rewrite.StageRequest, RangeMode: "fragment", Match: rewrite.Match{Domain: "api.service.example", Methods: []string{"GET"}, PathPrefix: "/"}, OnError: "block", RedirectConsent: []rewrite.Consent{}, Operations: []rewrite.Operation{}}
	out := rewriteRun(t, rewriteRuleDraft("work", initial, true), f, "1\napi.service.example\nPOST\n/v1/\n1\n2\n4\n4\n0\n")
	if len(f.inputs) != 2 || f.inputs[0] != f.inputs[1] || !strings.Contains(f.inputs[0], `"methods":["POST"]`) || !strings.Contains(f.inputs[0], `"on_error":"forward_original"`) {
		t.Fatal(f.inputs)
	}
	if !strings.Contains(out, "Draft retained") {
		t.Fatal(out)
	}
	f = &rewriteTestRunner{}
	rewriteRun(t, rewriteRuleDraft("work", initial, true), f, "1\nchanged.service.example\n")
	if len(f.calls) != 0 {
		t.Fatal("EOF submitted", f.calls)
	}
}
func TestRewriteOperationFormsAllKindsAndExactJSON(t *testing.T) {
	cases := []struct{ kind, input, want string }{
		{"header_set", "Authorization\n0\n", `"value":"0"`},
		{"header_delete", "X-Old\n", `"name":"X-Old"`},
		{"url_set", "5\n\n", `"value":""`},
		{"json_set", "\n9007199254740993\n", `"value":9007199254740993`},
		{"json_delete", "/old\n", `"pointer":"/old"`},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			f := &rewriteTestRunner{}
			s := NewSession(f, strings.NewReader(tc.input), io.Discard)
			var got rewrite.Operation
			err := rewriteOperationForm(context.Background(), s, &rewrite.Operation{Op: tc.kind}, func(op rewrite.Operation) { got = op })
			if err != nil {
				t.Fatal(err)
			}
			wire, _ := json.Marshal(rewriteRuleWire(rewrite.Rule{Operations: []rewrite.Operation{got}}))
			if !strings.Contains(string(wire), tc.want) {
				t.Fatal(string(wire))
			}
			if tc.kind == "json_set" && !strings.Contains(string(wire), `"pointer":""`) {
				t.Fatal("missing root pointer", string(wire))
			}
			if len(f.calls) != 0 {
				t.Fatal(f.calls)
			}
		})
	}
}
func TestRewriteOperationsPaginationMoveAndRemoveOnlyDraft(t *testing.T) {
	rule := rewrite.Rule{Operations: []rewrite.Operation{}}
	for i := 0; i < 12; i++ {
		rule.Operations = append(rule.Operations, rewrite.Operation{Op: "header_delete", Name: string(rune('A' + i))})
	}
	f := &rewriteTestRunner{}
	out := rewriteRun(t, rewriteOperationsPage(&rule), f, "12\n2\n2\n1\n0\n0\n")
	if !strings.Contains(out, "Next page") || rule.Operations[10].Name != "L" || rule.Operations[11].Name != "K" {
		t.Fatalf("%s %+v", out, rule.Operations)
	}
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}

func TestRewriteMovesUseCurrentPageNumbers(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		for _, total := range []int{12, 20, 26} {
			for _, kind := range []string{"rule", "operation"} {
				t.Run(fmt.Sprintf("%s/%s/%d", lang, kind, total), func(t *testing.T) {
					start := (total - 1) / pageSize * pageSize
					count := total - start
					ids := make([]string, total)
					rules := make([]rewrite.RuleSummary, total)
					draft := rewrite.Rule{}
					for i := range ids {
						ids[i] = fmt.Sprintf("R%02d", i+1)
						rules[i] = rewrite.RuleSummary{ID: ids[i], Stage: rewrite.StageRequest}
						draft.Operations = append(draft.Operations, rewrite.Operation{Op: "header_delete", Name: ids[i]})
					}
					want := append([]string{}, ids[:start]...)
					want = append(want, ids[total-1])
					want = append(want, ids[start:total-1]...)
					f := &rewriteTestRunner{run: func(a []string) Result {
						if a[2] == "list" {
							return Result{Data: map[string]any{"profile": "work", "rules": rules}}
						}
						return Result{Data: map[string]any{"profile": "work", "rule": rules[total-1]}}
					}}
					language := i18n.New(lang)
					page := localizedrewriteRulesPage(language, "work")
					move, submit := 3, "1\n"
					if kind == "operation" {
						page = localizedrewriteOperationsPage(language, &draft)
						move, submit = 2, ""
					}
					// Reject the action number after the last resource, then move to this page's first slot.
					input := strings.Repeat("12\n", start/pageSize) + fmt.Sprintf("%d\n%d\n%d\n1\n%s", count, move, count+1, submit)
					var out bytes.Buffer
					s := NewSessionWithLanguage(f, strings.NewReader(input), &out, language)
					if code := RunPage(context.Background(), s, page); code != 0 {
						t.Fatalf("exit %d: %s", code, out.String())
					}
					if kind == "rule" {
						var got []string
						if len(f.inputs) != 1 || json.Unmarshal([]byte(f.inputs[0]), &got) != nil || !reflect.DeepEqual(got, want) {
							t.Fatalf("saved rule order: %v; want %v", f.inputs, want)
						}
						if !strings.Contains(out.String(), "1. "+ids[start]+" [") {
							t.Fatalf("rule choice contains an extra sequence number: %s", out.String())
						}
					} else {
						got := make([]string, total)
						for i, op := range draft.Operations {
							got[i] = op.Name
						}
						if !reflect.DeepEqual(got, want) || len(f.calls) != 0 {
							t.Fatalf("draft order: %v; want %v; calls: %v", got, want, f.calls)
						}
						if !strings.Contains(out.String(), "1. header_delete\n") {
							t.Fatalf("operation choice contains an extra sequence number: %s", out.String())
						}
					}
					promptID := "menu.request_rewrite_rules.rewrite_rule_reorder.position_1_d"
					if kind == "operation" {
						promptID = "menu.request_rewrite_rules.rewrite_draft_position.position_1_d"
					}
					prompt := menuFormat(language, promptID, "Position (1–%d)", count)
					if strings.Count(out.String(), prompt) != 2 {
						t.Fatalf("missing page range or invalid-position retry: %s", out.String())
					}
				})
			}
		}
	}
}
func TestRewriteRuleEditExplicitSensitiveExportAndPortableValidation(t *testing.T) {
	summary := rewrite.RuleSummary{ID: "demo", Enabled: true, Match: rewrite.Match{Domain: "api.service.example", Methods: []string{"GET"}, PathPrefix: "/"}, Operations: []string{"header_set"}, OnError: "block"}
	f := &rewriteTestRunner{run: func([]string) Result { return Result{Data: map[string]any{"profile": "work", "rule": summary}} }, raw: Result{Data: `{"version":2,"enabled":false,"scripts":[],"rules":[{"id":"demo","enabled":true,"stage":"request","match":{"domain":"api.service.example","methods":["GET"],"path_prefix":"/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"header_set","name":"Authorization","value":"test-secret-value"}]}]}`}}
	s := NewSession(f, strings.NewReader(""), io.Discard)
	v := rewriteLoad(t, rewriteRuleDetail("work", "demo"), s)
	if len(f.rawCalls) != 0 {
		t.Fatal("ordinary detail exported values")
	}
	if strings.Contains(v.Context, "test-secret") {
		t.Fatal(v.Context)
	}
	nav, err := rewriteFind(t, v, "Edit rule").Navigate(context.Background(), s)
	if err != nil || nav.Next == nil {
		t.Fatal(nav, err)
	}
	if !reflect.DeepEqual(f.rawCalls, [][]string{{"request-rewrite", "export", "-", "--profile", "work"}}) {
		t.Fatal(f.rawCalls)
	}
	if strings.Contains(rewriteLoad(t, nav.Next, s).Context, "test-secret") {
		t.Fatal("draft summary leaked operation values")
	}
	f.raw.Data = strings.Replace(f.raw.Data.(string), `"enabled":false`, `"enabled":true`, 1)
	nav, _ = rewriteFind(t, v, "Edit rule").Navigate(context.Background(), s)
	if nav.Next != nil {
		t.Fatal("accepted active portable export")
	}
}
func TestRewriteRuleToggleReloadAndReorder(t *testing.T) {
	enabled := true
	f := &rewriteTestRunner{run: func(a []string) Result {
		if a[2] == "show" {
			return Result{Data: map[string]any{"profile": "work", "rule": rewrite.RuleSummary{ID: "b", Enabled: enabled}}}
		}
		if a[2] == "list" {
			return Result{Data: map[string]any{"profile": "work", "rules": []rewrite.RuleSummary{{ID: "a"}, {ID: "b"}, {ID: "c"}}}}
		}
		if a[2] == "disable" {
			enabled = false
		}
		return Result{}
	}}
	s := NewSession(f, strings.NewReader("1\n1\n"), io.Discard)
	p := rewriteRuleDetail("work", "b")
	v := rewriteLoad(t, p, s)
	_ = rewriteFind(t, v, "Disable rule").Action(context.Background(), s)
	v = rewriteLoad(t, p, s)
	rewriteFind(t, v, "Enable rule")
	if err := rewriteRuleReorder(context.Background(), s, "work", "b"); err != nil {
		t.Fatal(err)
	}
	if len(f.inputs) != 1 || f.inputs[0] != `["b","a","c"]` {
		t.Fatal(f.inputs)
	}
}
func TestRewriteReadFailuresClearTargetsAndKeepRecovery(t *testing.T) {
	f := &rewriteTestRunner{run: func([]string) Result { return rewriteFailure("unavailable") }}
	var out bytes.Buffer
	s := NewSession(f, strings.NewReader(""), &out)
	for _, p := range []*Page{rewriteScriptsPage("work"), rewriteRulesPage("work"), rewriteRuleDetail("work", "old"), RequestRewritePage()} {
		v, e := p.Load(context.Background(), s)
		if e == nil || len(v.Items) > 0 {
			t.Fatal(v, e)
		}
	}
	v, _ := RequestRewritePage().Load(context.Background(), s)
	rewriteFind(t, v, "Recovery")
	v = rewriteLoad(t, rewriteRecoveryPage("work", true), s)
	rewriteFind(t, v, "Disable request rewrite")
	rewriteFind(t, v, "Validate stored document")
}
func TestRewritePreviewSensitiveDisplayAndFailureRetention(t *testing.T) {
	calls := 0
	f := &rewriteTestRunner{input: func(a []string, b string) Result {
		calls++
		if calls == 1 {
			return rewriteFailure("rewrite_invalid")
		}
		return Result{Data: map[string]any{"sensitive": true, "rule_version": "v2", "rule_ids": []string{}, "outcome": "unchanged", "request": map[string]any{"query": "token=explicit-preview-value"}}}
	}}
	var out bytes.Buffer
	s := NewSession(f, strings.NewReader("1\napi.service.example\n443\nGET\n/v1/\ntoken=sample\n{}\n\n1\n9\n"), &out)
	if err := rewritePreviewForm("work")(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if len(f.inputs) != 2 || f.inputs[0] != f.inputs[1] || !strings.Contains(out.String(), "token=explicit-preview-value") || !s.longResult {
		t.Fatal(f.inputs, out.String())
	}
	for _, a := range f.calls {
		if !reflect.DeepEqual(a, []string{"request-rewrite", "preview", "-", "--profile", "work"}) {
			t.Fatal(a)
		}
	}
}
func TestRewritePublicCAPlatformAndMinuteTime(t *testing.T) {
	f := &rewriteTestRunner{run: func([]string) Result {
		return Result{Data: map[string]any{"state": "generated", "trust": "unknown", "not_after": "2027-10-18T10:20:39Z"}}
	}}
	s := NewSession(f, strings.NewReader("/tmp/public.pem\n1\n"), io.Discard)
	linux := rewriteLoad(t, rewriteCAPageForOS("linux"), s)
	if !strings.Contains(linux.Context, "2027-10-18 10:20") || strings.Contains(linux.Context, ":39") || !strings.Contains(linux.Context, "Linux:") {
		t.Fatal(linux.Context)
	}
	for _, a := range linux.Actions {
		if strings.Contains(a.Label, "CA trust") {
			t.Fatal("automatic Linux trust action")
		}
	}
	darwin := rewriteLoad(t, rewriteCAPageForOS("darwin"), s)
	rewriteFind(t, darwin, "Install CA trust")
	rewriteFind(t, darwin, "Remove CA trust")
	if err := rewriteFind(t, linux, "Export public certificate to file").Action(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.rawCalls, [][]string{{"request-rewrite", "ca", "export", "/tmp/public.pem"}}) {
		t.Fatal(f.rawCalls)
	}
}
func TestRewriteRawRunnerAllowlistAndNoJSON(t *testing.T) {
	calls := 0
	runner := NewCommandRunner(func(a []string, out, stderr io.Writer) int {
		calls++
		for _, arg := range a {
			if arg == "--json" {
				t.Fatal(a)
			}
		}
		io.WriteString(out, "raw explicit output")
		return 0
	}).(RawRunner)
	for _, args := range [][]string{{"request-rewrite", "export", "-", "--profile", "work"}, {"request-rewrite", "export", "private.json"}, {"request-rewrite", "ca", "export", "public.pem"}} {
		r := runner.RunRaw(context.Background(), args)
		if rewriteFailed(r) || r.Data != "raw explicit output" {
			t.Fatal(r)
		}
	}
	for _, args := range [][]string{{"config", "show"}, {"request-rewrite", "ca", "generate"}, {"request-rewrite", "export", "--help"}, {"request-rewrite", "export", "-", "--json"}, {"request-rewrite", "ca", "export", "--json"}, {"request-rewrite", "export", "-", "--profile", "work", "extra"}} {
		if !rewriteFailed(runner.RunRaw(context.Background(), args)) {
			t.Fatal(args)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}

func TestRewriteEditPreservesValuesAndExplicitClear(t *testing.T) {
	for _, op := range []rewrite.Operation{{Op: "header_set", Name: "Authorization", Value: json.RawMessage(`"existing-value"`)}, {Op: "json_set", Pointer: "/keep", Value: json.RawMessage(`9007199254740993`)}} {
		var edited rewrite.Operation
		s := NewSession(&rewriteTestRunner{}, strings.NewReader("\n\n"), io.Discard)
		if err := rewriteOperationForm(context.Background(), s, &op, func(next rewrite.Operation) { edited = next }); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(edited, op) {
			t.Fatalf("changed unedited fields: %+v %+v", op, edited)
		}
		rule := rewrite.Rule{Operations: []rewrite.Operation{op}}
		v := rewriteLoad(t, rewriteOperationDetail(&rule, 0), s)
		label := "Set empty value"
		if op.Op == "json_set" {
			label = "Use root JSON pointer"
		}
		if err := rewriteFind(t, v, label).Action(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		if op.Op == "json_set" && rule.Operations[0].Pointer != "" {
			t.Fatal(rule)
		}
		if op.Op == "header_set" && string(rule.Operations[0].Value) != `""` {
			t.Fatal(rule)
		}
	}
}
func TestRewriteMoveReturnsToListBeforeEditAndRemove(t *testing.T) {
	rule := rewrite.Rule{Operations: []rewrite.Operation{{Op: "header_delete", Name: "A"}, {Op: "header_delete", Name: "B"}, {Op: "header_delete", Name: "C"}}}
	f := &rewriteTestRunner{}
	// Move C to first, select C again, edit it, then remove it. A/B stay intact.
	rewriteRun(t, rewriteOperationsPage(&rule), f, "3\n2\n1\n1\n1\nChanged\n3\n0\n")
	if len(rule.Operations) != 2 || rule.Operations[0].Name != "A" || rule.Operations[1].Name != "B" {
		t.Fatal(rule.Operations)
	}
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}

func TestRewriteEOFOrCancelAfterFinalFieldDoesNotSubmit(t *testing.T) {
	for _, ending := range []string{"", "0\n"} {
		f := &rewriteTestRunner{}
		s := NewSession(f, strings.NewReader(`{"id":"api","body_access":"none","source":"function rewrite(input) { return {}; }"}`+"\n"+ending), io.Discard)
		err := rewriteScriptForm("work", nil)(context.Background(), s)
		if err == nil || len(f.calls) != 0 {
			t.Fatalf("ending %q: err=%v calls=%v", ending, err, f.calls)
		}
	}
}

func TestRewriteRecoveryValidationBindsFallbackProfile(t *testing.T) {
	f := &rewriteTestRunner{run: func(a []string) Result {
		if reflect.DeepEqual(a, []string{"profile", "list"}) {
			return Result{Data: map[string]any{"current": "work"}}
		}
		if len(a) > 1 && a[1] == "status" {
			return rewriteFailure("unavailable")
		}
		return Result{}
	}}
	s := NewSession(f, strings.NewReader(""), io.Discard)
	v := rewriteLoad(t, rewriteRecoveryPage("unknown", false), s)
	if err := rewriteFind(t, v, "Validate stored document").Action(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], []string{"request-rewrite", "validate", "--profile", "work"}) {
		t.Fatal(f.calls)
	}
}

func TestRewriteCompleteJSONDraftPreservesStageAndPredicates(t *testing.T) {
	f := &rewriteTestRunner{}
	original := rewrite.Rule{ID: "complete", Enabled: true, Stage: rewrite.StageRequest, RangeMode: "fragment", Match: rewrite.Match{Domain: "api.service.example"}, OnError: "block", RedirectConsent: []rewrite.Consent{}, Operations: []rewrite.Operation{{Op: "header_delete", Name: "X-Old"}}}
	updated := original
	updated.Stage = rewrite.StageResponse
	updated.RangeMode = "full"
	updated.Match.PathRegex = "^/v[0-9]+/"
	updated.Match.StatusCodes = []int{200}
	updated.Match.Query = map[string][]string{"token": {"fictitious"}}
	updated.Operations = []rewrite.Operation{{Op: "regex_replace", Pattern: "(old)", Replacement: "${1}-new", All: true}, {Op: "json_set", Pointer: "/n", Value: json.RawMessage(`9007199254740993`)}}
	raw, _ := json.Marshal(rewriteRuleWire(updated))
	// Complete JSON changes only the in-memory draft; Save sends it once.
	rewriteRun(t, rewriteRuleDraft("work", original, true), f, "5\n"+string(raw)+"\n4\n")
	if len(f.inputs) != 1 {
		t.Fatal(f.inputs)
	}
	var got rewrite.Rule
	if json.Unmarshal([]byte(f.inputs[0]), &got) != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("got %#v want %#v", got, updated)
	}
}

func TestRewriteScriptSummaryDoesNotFetchSource(t *testing.T) {
	f := &rewriteTestRunner{run: func(a []string) Result {
		if a[2] == "show" {
			return Result{Data: map[string]any{"profile": "work", "script": rewrite.ScriptSummary{ID: "script", BodyAccess: "none", SourceBytes: 17, RuleIDs: []string{"dependent"}}}}
		}
		if a[2] == "source" {
			return Result{Data: map[string]any{"profile": "work", "sensitive": true, "script": rewrite.Script{ID: "script", BodyAccess: "none", Source: "fictitious-source"}}}
		}
		return Result{}
	}}
	var out bytes.Buffer
	s := NewSession(f, strings.NewReader(""), &out)
	view := rewriteLoad(t, rewriteScriptDetail("work", "script"), s)
	if len(f.calls) != 1 || strings.Contains(view.Context, "fictitious-source") {
		t.Fatal(view, f.calls)
	}
	if e := rewriteFind(t, view, "View sensitive source").Action(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if len(f.calls) != 2 || !strings.Contains(out.String(), "fictitious-source") || !s.longResult {
		t.Fatal(f.calls, out.String())
	}
}

func TestRewriteDraftJSONRejectsAmbiguousFields(t *testing.T) {
	for _, raw := range []string{`{"op":"header_delete","name":"X-First","name":"X-Second"}`, `{"op":"header_delete","name":"X-First","extra":true}`, `{"op":"header_delete","name":"X-First"} {}`} {
		var op rewrite.Operation
		if rewriteMenuJSON(raw, &op) == nil {
			t.Fatalf("accepted ambiguous operation %s", raw)
		}
	}
}
