package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/capture"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	"github.com/ronigooja/Nagi/internal/rewritedraft"
)

func TestCaptureStartPrivateInputRetainedFailureAndEOF(t *testing.T) {
	calls := 0
	f := &rewriteTestRunner{input: func(a []string, b string) Result {
		calls++
		if calls == 1 {
			return rewriteFailure("capture_unavailable")
		}
		return Result{}
	}}
	s := NewSession(f, strings.NewReader("api.fixture.example\n/opt/test process\n/Applications/Fictitious.app\n\n1\n5\n"), io.Discard)
	if err := captureStartForm(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if len(f.inputs) != 2 || f.inputs[0] != f.inputs[1] {
		t.Fatal(f.inputs)
	}
	for _, args := range f.calls {
		if !reflect.DeepEqual(args, captureArgs("start", "-")) {
			t.Fatal(args)
		}
	}
	var opt capture.StartOptions
	if json.Unmarshal([]byte(f.inputs[0]), &opt) != nil || opt.Owner != "cli" || opt.Scope.ProcessPaths[0] != "/opt/test process" || opt.BodyLimit != capture.DefaultBodyLimit {
		t.Fatal(opt)
	}
	for _, ending := range []string{"", "0\n"} {
		f = &rewriteTestRunner{}
		s = NewSession(f, strings.NewReader("\n\n\n\n"+ending), io.Discard)
		if captureStartForm(context.Background(), s) == nil || len(f.calls) != 0 {
			t.Fatal("EOF/cancel submitted", f.calls)
		}
	}
}
func TestCaptureStatusControlsAndUncertainRecovery(t *testing.T) {
	for _, state := range []string{"recording", "paused", "uncertain"} {
		f := &rewriteTestRunner{run: func([]string) Result {
			return Result{Data: map[string]any{"status": map[string]any{"session_id": "session", "state": state, "recorder_state": "recording", "profile": "work", "recovery": "nagi rewrite capture stop session"}}}
		}}
		s := NewSession(f, strings.NewReader(""), io.Discard)
		v := rewriteLoad(t, capturePage(), s)
		if !strings.Contains(v.Context, "Recovery: nagi rewrite capture stop session") {
			t.Fatal(v.Context)
		}
		item := rewriteFind(t, v, "Stop capture")
		if err := item.Action(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.calls[len(f.calls)-1], captureArgs("stop", "session")) {
			t.Fatal(f.calls)
		}
		for _, a := range v.Actions {
			if state == "uncertain" && (a.Label == "Pause capture" || a.Label == "Resume capture") {
				t.Fatal(a)
			}
		}
	}
	f := &rewriteTestRunner{run: func([]string) Result { return rewriteFailure("unavailable") }}
	v, e := capturePage().Load(context.Background(), NewSession(f, strings.NewReader(""), io.Discard))
	if e == nil {
		t.Fatal("expected read error")
	}
	rewriteFind(t, v, "Start with scope")
	rewriteFind(t, v, "Import native archive")
}
func TestCaptureOriginalMetadataBodyAndMinuteDisplay(t *testing.T) {
	record := capture.Record{ID: "r", Started: time.Date(2026, 10, 18, 12, 34, 56, 123, time.UTC), Snapshots: map[string]capture.Snapshot{"request_original": {Query: "token=fictitious", Headers: map[string][]string{"Authorization": {"Bearer fictitious"}, "Cookie": {"session=fictitious"}}}}}
	f := &rewriteTestRunner{run: func(a []string) Result {
		if a[2] == "show" {
			return Result{Data: capture.Reply{Record: &record}}
		}
		return Result{Data: capture.Reply{Body: &capture.BodyChunk{Data: []byte("fictitious body"), NextOffset: 15, EOF: true, State: "complete"}}}
	}}
	var out bytes.Buffer
	s := NewSession(f, strings.NewReader("1\n\n\n1\n"), &out)
	v := rewriteLoad(t, captureRecordPage("r"), s)
	_ = rewriteFind(t, v, "Show original metadata").Action(context.Background(), s)
	if e := captureBodyForm("r")(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	text := out.String()
	for _, value := range []string{"token=fictitious", "Bearer fictitious", "session=fictitious", "fictitious body", "2026-10-18 12:34"} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %s in %s", value, text)
		}
	}
	if strings.Contains(text, ":56") || !s.longResult {
		t.Fatal(text)
	}
	if !reflect.DeepEqual(f.calls[1], captureArgs("body", "r", "request_original", "0", "262144")) {
		t.Fatal(f.calls)
	}
	// The actual menu pauses raw reads until Enter, then reloads the same page.
	outText := rewriteRun(t, captureRecordPage("r"), f, "1\n\n0\n")
	if !strings.Contains(outText, "Press Enter to return:") {
		t.Fatal(outText)
	}
}
func TestCaptureListPaginationAndNoStaleRecords(t *testing.T) {
	f := &rewriteTestRunner{input: func(a []string, b string) Result {
		return Result{Data: capture.Reply{Records: []capture.Record{{ID: "r", Kind: "http"}}, NextCursor: "opaque+cursor"}}
	}}
	s := NewSession(f, strings.NewReader(""), io.Discard)
	v := rewriteLoad(t, captureRecordsPage(""), s)
	next := rewriteFind(t, v, "Next record batch")
	rewriteLoad(t, next.Next(), s)
	if !strings.Contains(f.inputs[1], `"cursor":"opaque+cursor"`) {
		t.Fatal(f.inputs)
	}
	f.input = func([]string, string) Result { return rewriteFailure("cursor_invalid") }
	v, e := captureRecordsPage("old").Load(context.Background(), s)
	if e == nil || len(v.Items) != 0 {
		t.Fatal(v, e)
	}
}
func TestCaptureQuickRulesThroughExistingCLIAndCASDraft(t *testing.T) {
	draft := rewritedraft.Draft{BaseRuleVersion: "v1", Position: 2, Rule: rewrite.Rule{ID: "quick", Stage: "request", Operations: []rewrite.Operation{{Op: "header_set", Name: "X-Demo", Value: json.RawMessage(`"fictitious"`)}}}}
	f := &rewriteTestRunner{run: func([]string) Result { return Result{Data: map[string]any{"current": "work"}} }, input: func(a []string, b string) Result {
		if a[1] == "capture" {
			return Result{Data: draft}
		}
		return rewriteFailure("rewrite_conflict")
	}}
	s := NewSession(f, strings.NewReader("quick\n1\n1\n"), io.Discard)
	p := captureQuickRulePage("r")
	v := rewriteLoad(t, p, s)
	if e := rewriteFind(t, v, "Rule fields").Action(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	v = rewriteLoad(t, p, s)
	nav, e := rewriteFind(t, v, "Build versioned draft").Navigate(context.Background(), s)
	if e != nil || nav.Next == nil {
		t.Fatal(nav, e)
	}
	if !reflect.DeepEqual(f.calls[1], captureArgs("draft", "r", "-")) {
		t.Fatal(f.calls)
	}
	dv := rewriteLoad(t, nav.Next, s)
	save := rewriteFind(t, dv, "Save rule")
	for i := 0; i < 2; i++ {
		n, e := save.Navigate(context.Background(), s)
		if e != nil || n.Back {
			t.Fatal(n, e)
		}
	}
	if len(f.inputs) != 3 || f.inputs[1] != f.inputs[2] {
		t.Fatal(f.inputs)
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], rewriteArgs("work", "draft", "save", "-")) {
		t.Fatal(f.calls)
	}
	for _, args := range f.calls {
		for _, arg := range args {
			if strings.Contains(arg, "fictitious") {
				t.Fatal("value in arguments", args)
			}
		}
	}
}
func TestCaptureRawExportAllowlistAndArchiveMenu(t *testing.T) {
	var calls [][]string
	runner := NewCommandRunner(func(args []string, out, errOut io.Writer) int { calls = append(calls, args); return 0 }).(RawRunner)
	for _, args := range [][]string{captureArgs("export", "private.ndjson", "native"), captureArgs("export", "private.har", "har"), captureArgs("export", "-")} {
		if rewriteFailed(runner.RunRaw(context.Background(), args)) {
			t.Fatal(args)
		}
	}
	for _, args := range [][]string{captureArgs("export", "private", "invalid"), captureArgs("import", "private"), captureArgs("export", "--json"), captureArgs("export", "private", "native", "--json")} {
		if !rewriteFailed(runner.RunRaw(context.Background(), args)) {
			t.Fatal(args)
		}
	}
	if len(calls) != 3 {
		t.Fatal(calls)
	}
	f := &rewriteTestRunner{}
	s := NewSession(f, strings.NewReader("/tmp/fictitious.har\n2\n1\n"), io.Discard)
	if e := captureArchiveForm(true)(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(f.rawCalls, [][]string{captureArgs("export", "/tmp/fictitious.har", "har")}) {
		t.Fatal(f.rawCalls)
	}
}

func TestCaptureQuickRuleJourneyEditsPreviewsAndSavesOnce(t *testing.T) {
	saves, previews := 0, 0
	draft := rewritedraft.Draft{BaseRuleVersion: "v1", Position: 0, Rule: rewrite.Rule{ID: "quick", Stage: "request", Operations: []rewrite.Operation{{Op: "header_set", Name: "X-Demo", Value: json.RawMessage(`"fictitious"`)}}}, Enable: true}
	f := &rewriteTestRunner{run: func(a []string) Result {
		if a[1] == "profile" || a[0] == "profile" {
			return Result{Data: map[string]any{"current": "work"}}
		}
		return Result{Data: map[string]any{"request": map[string]any{}}}
	}, input: func(a []string, b string) Result {
		if a[1] == "capture" {
			var selected rewritedraft.Selection
			if json.Unmarshal([]byte(b), &selected) != nil || selected.ID != "quick" || !selected.Enable || len(selected.Operations) != 1 || selected.Operations[0].Name != "X-Demo" {
				t.Fatal(b)
			}
			return Result{Data: draft}
		}
		if a[2] == "preview" {
			previews++
			return Result{Data: map[string]any{"sensitive": true, "request": map[string]any{"query": "token=fictitious"}}}
		}
		if a[2] == "save" {
			saves++
		}
		return Result{}
	}}
	// Rule fields, one ordinary header operation, build, preview, Enter, save.
	text := rewriteRun(t, captureQuickRulePage("r"), f, "1\nquick\n1\n2\n2\n1\n1\nX-Demo\nfictitious\n0\n3\n3\n\n4\n0\n")
	if saves != 1 || previews != 1 || !strings.Contains(text, "token=fictitious") || !strings.Contains(text, "Press Enter to return:") {
		t.Fatal(saves, previews, text)
	}
}
func TestCaptureFixtureUsesOriginalCLIStage(t *testing.T) {
	f := &rewriteTestRunner{run: func([]string) Result {
		return Result{Data: map[string]any{"stage": "exchange", "request": map[string]any{"query": "token=fictitious"}}}
	}}
	s := NewSession(f, strings.NewReader("3\n"), io.Discard)
	v := rewriteLoad(t, captureRecordPage("r"), s)
	if e := rewriteFind(t, v, "Original preview fixture").Action(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(f.calls, [][]string{captureArgs("fixture", "r", "exchange")}) || !s.longResult {
		t.Fatal(f.calls)
	}
}

func TestCaptureEntryShowsOnlyCurrentPrerequisiteAndRetainsOfflineActions(t *testing.T) {
	for _, failure := range []bool{false, true} {
		current := "TUN is not running. Enable TUN before starting capture."
		f := &rewriteTestRunner{run: func(args []string) Result {
			if args[2] == "check" {
				return Result{Data: capture.Reply{Readiness: &capture.Readiness{Code: "rewrite_tun_required", Message: current}}}
			}
			if failure {
				return rewriteFailure("capture_unavailable")
			}
			return Result{Data: capture.Reply{}}
		}}
		session := NewSession(f, strings.NewReader(""), io.Discard)
		for _, message := range []string{current, "A valid capture CA is missing."} {
			current = message
			view, err := capturePage().Load(context.Background(), session)
			if (err != nil) != failure {
				t.Fatal(err)
			}
			if strings.Count(view.Context, message) != 1 {
				t.Fatal(view.Context)
			}
			if message != "TUN is not running. Enable TUN before starting capture." && strings.Contains(view.Context, "TUN is not running") {
				t.Fatal(view.Context)
			}
			rewriteFind(t, view, "Import native archive")
			rewriteFind(t, view, "Captured records")
			rewriteFind(t, view, "Start with scope")
		}
		expected := [][]string{captureArgs("check"), captureArgs("status"), captureArgs("check"), captureArgs("status")}
		if !reflect.DeepEqual(f.calls, expected) {
			t.Fatal(f.calls)
		}
	}
}
