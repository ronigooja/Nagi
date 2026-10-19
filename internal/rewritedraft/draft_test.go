package rewritedraft

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/requestrewrite"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }
func operation() requestrewrite.Operation {
	return requestrewrite.Operation{Op: "header_set", Name: "X-Test", Value: raw(`"draft"`)}
}
func compile(t *testing.T, d requestrewrite.Document) *requestrewrite.Snapshot {
	t.Helper()
	s, e := requestrewrite.Compile(d)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func sample() (capture.Record, map[string][]byte) {
	req := capture.Snapshot{Scheme: "https", Host: "api.service.example", Port: 443, Method: "POST", Path: "/v1/a.b", Query: "token=fictitious", Headers: map[string][]string{"Content-Type": {"application/json"}, "Authorization": {"Bearer fictitious"}, "Cookie": {"example=private"}}, BodyState: "complete", BodySize: 2, Captured: 2}
	resp := capture.Snapshot{Status: 200, Headers: map[string][]string{"Content-Type": {"application/json"}, "Set-Cookie": {"example=one", "example=two"}}, BodyState: "complete", BodySize: 2, Captured: 2}
	actual := req
	actual.Query = "changed"
	actual.Headers = map[string][]string{"X-Rewritten": {"yes"}}
	return capture.Record{ID: "http-1", SessionID: "session", Kind: "http", Snapshots: map[string]capture.Snapshot{"request_original": req, "request_actual": actual, "response_original": resp, "response_actual": resp}}, map[string][]byte{"request_original": []byte(`{}`), "request_actual": []byte(`actual`), "response_original": []byte(`{}`), "response_actual": []byte(`actual`)}
}
func TestFromOriginalWithEmptyBaseAndExplicitSensitiveSelection(t *testing.T) {
	r, b := sample()
	base := requestrewrite.Empty()
	d, e := FromRecord(r, b, Selection{ID: "draft", Stage: "request", Operations: []requestrewrite.Operation{operation()}}, base)
	if e != nil {
		t.Fatal(e)
	}
	if d.Enable || d.Rule.Enabled || d.Position != 0 || d.Rule.Match.PathRegex != `^/v1/a\.b$` || len(d.Rule.Match.Headers) != 0 || len(d.Rule.Match.Query) != 0 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if d.Fixture.Request.Query != "token=fictitious" || d.Fixture.Request.Headers["Authorization"][0] != "Bearer fictitious" || d.Fixture.Request.BodyBase64 != "e30=" {
		t.Fatal("fixture is not raw original", d.Fixture)
	}
	result, e := Preview(context.Background(), base, d)
	if e != nil || result.Request.Headers["X-Test"][0] != "draft" {
		t.Fatal(result, e)
	}
	saved, e := Apply(base, d)
	if e != nil || saved.Document().Enabled || saved.Document().Rules[0].Enabled {
		t.Fatal("saved intent", e)
	}
	d.Enable = true
	saved, e = Apply(base, d)
	if e != nil || saved.Document().Enabled || !saved.Document().Rules[0].Enabled {
		t.Fatal("explicit rule enable changes global intent", e)
	}
	match := d.Rule.Match
	match.Headers = map[string][]string{"Authorization": {"Bearer fictitious"}}
	match.Query = map[string][]string{"token": {"fictitious"}}
	d, e = FromRecord(r, b, Selection{ID: "draft", Stage: "request", Match: &match, Operations: []requestrewrite.Operation{operation()}}, base)
	if e != nil {
		t.Fatal(e)
	}
	match.Headers["Authorization"][0] = "mutated"
	r.Snapshots["request_original"].Headers["Cookie"][0] = "mutated"
	if d.Rule.Match.Headers["Authorization"][0] != "Bearer fictitious" || d.Fixture.Request.Headers["Cookie"][0] != "example=private" {
		t.Fatal("caller aliases draft")
	}
}
func TestFixtureStagesCompletenessAndWebsocketParent(t *testing.T) {
	r, b := sample()
	for _, stage := range []string{"request", "response", "exchange"} {
		f, e := Fixture(r, b, stage)
		if e != nil || f.Request.Query != "token=fictitious" {
			t.Fatal(stage, e)
		}
		if stage != "request" && len(f.Response.Headers["Set-Cookie"]) != 2 {
			t.Fatal("repeated response headers")
		}
	}
	for _, state := range []string{"truncated", "failed", "cutoff", "collecting", ""} {
		s := r.Snapshots["request_original"]
		s.BodyState = state
		r.Snapshots["request_original"] = s
		if _, e := Fixture(r, b, "request"); e == nil {
			t.Fatal("incomplete accepted", state)
		}
	}
	r, b = sample()
	s := r.Snapshots["request_original"]
	s.Captured = 1
	r.Snapshots["request_original"] = s
	if _, e := Fixture(r, b, "request"); e == nil {
		t.Fatal("capture length mismatch")
	}
	r, b = sample()
	s = r.Snapshots["request_original"]
	s.BodySize = 0
	s.Captured = 0
	r.Snapshots["request_original"] = s
	delete(b, "request_original")
	if _, e := Fixture(r, b, "request"); e != nil {
		t.Fatal("empty complete body", e)
	}
	r, b = sample()
	s = r.Snapshots["response_original"]
	s.Status = 101
	r.Snapshots["response_original"] = s
	for _, stage := range []string{"websocket_client", "websocket_server"} {
		msg := capture.Record{ID: "message", ParentID: r.ID, SessionID: r.SessionID, Kind: "websocket", Stage: stage, Snapshots: map[string]capture.Snapshot{"message_original": {MessageType: "text", Headers: map[string][]string{}, BodyState: "complete", BodySize: 3, Captured: 3}}}
		mb := map[string][]byte{"message_original": []byte("old")}
		if _, e := Fixture(msg, mb, stage); e == nil {
			t.Fatal("missing parent accepted")
		}
		f, e := Fixture(msg, mb, stage, Context{Record: r, Bodies: b})
		if e != nil || f.Message.BodyBase64 != "b2xk" {
			t.Fatal(stage, f, e)
		}
		d, e := FromRecord(msg, mb, Selection{ID: "message", Stage: stage, Operations: []requestrewrite.Operation{{Op: "body_set", Encoding: "text", Value: raw(`"new"`)}}, Context: &Context{Record: r, Bodies: b}}, requestrewrite.Empty())
		if e != nil {
			t.Fatal(e)
		}
		result, e := Preview(context.Background(), requestrewrite.Empty(), d)
		if e != nil || result.Message.BodyBase64 != "bmV3" {
			t.Fatal(result, e)
		}
		wrong := r
		wrong.ID = "wrong"
		if _, e := Fixture(msg, mb, stage, Context{Record: wrong, Bodies: b}); e == nil {
			t.Fatal("wrong parent accepted")
		}
	}
}
func TestCompressedOriginalAndExactNumbers(t *testing.T) {
	r, b := sample()
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	_, _ = w.Write([]byte(`{"n":1}`))
	_ = w.Close()
	s := r.Snapshots["request_original"]
	s.Headers["Content-Encoding"] = []string{"gzip"}
	s.BodySize = int64(compressed.Len())
	s.Captured = s.BodySize
	r.Snapshots["request_original"] = s
	b["request_original"] = compressed.Bytes()
	d, e := FromRecord(r, b, Selection{ID: "json", Stage: "request", Operations: []requestrewrite.Operation{{Op: "json_set", Pointer: "/n", Value: raw(`90071992547409931234567890`)}}}, requestrewrite.Empty())
	if e != nil {
		t.Fatal(e)
	}
	original, _ := base64.StdEncoding.DecodeString(d.Fixture.Request.BodyBase64)
	if !bytes.Equal(original, compressed.Bytes()) {
		t.Fatal("original compressed bytes changed")
	}
	result, e := Preview(context.Background(), requestrewrite.Empty(), d)
	if e != nil {
		t.Fatal(e)
	}
	out, _ := base64.StdEncoding.DecodeString(result.Request.BodyBase64)
	reader, e := gzip.NewReader(bytes.NewReader(out))
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := io.ReadAll(reader)
	_ = reader.Close()
	if e != nil || string(decoded) != `{"n":90071992547409931234567890}` {
		t.Fatal(string(decoded), e)
	}
}
func TestInsertionFullChainAndConflict(t *testing.T) {
	base := requestrewrite.Empty()
	doc := base.Document()
	rule := requestrewrite.Rule{ID: "later", Enabled: true, Stage: "request", Match: requestrewrite.Match{Domain: "api.service.example"}, OnError: "block", RedirectConsent: []requestrewrite.Consent{}, Operations: []requestrewrite.Operation{{Op: "header_append", Name: "X-Test", Value: raw(`"later"`)}}}
	doc.Rules = []requestrewrite.Rule{rule}
	base = compile(t, doc)
	r, b := sample()
	pos := 0
	d, e := FromRecord(r, b, Selection{ID: "first", Stage: "request", Position: &pos, Operations: []requestrewrite.Operation{operation()}}, base)
	if e != nil {
		t.Fatal(e)
	}
	result, e := Preview(context.Background(), base, d)
	if e != nil || !reflect.DeepEqual(result.RuleIDs, []string{"first", "later"}) || !reflect.DeepEqual(result.Request.Headers["X-Test"], []string{"draft", "later"}) {
		t.Fatal(result, e)
	}
	if _, e = Apply(requestrewrite.Empty(), d); e == nil || e.Error() != "rewrite_conflict" {
		t.Fatal("stale version", e)
	}
	d.Position = 2
	if _, e = Apply(base, d); e == nil {
		t.Fatal("position outside chain")
	}
	d.Position = 0
	d.Rule.ID = "later"
	if _, e = Apply(base, d); e == nil {
		t.Fatal("duplicate rule")
	}
}
func TestStrictWireAndPrivateErrors(t *testing.T) {
	r, b := sample()
	d, e := FromRecord(r, b, Selection{ID: "draft", Stage: "request", Operations: []requestrewrite.Operation{operation()}}, requestrewrite.Empty())
	if e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(d)
	bad := []string{string(wire) + " {}", strings.Replace(string(wire), `"position":0`, `"position":0,"position":0`, 1), strings.Replace(string(wire), `"enable":false`, `"enable":null`, 1), strings.Replace(string(wire), `"stage":"request"`, `"stage":"request","unknown":"fictitious-secret"`, 1), strings.Replace(string(wire), `"op":"header_set"`, `"op":"header_set","pointer":"/unused"`, 1), strings.Replace(string(wire), `"X-Test"`, `"X-Test","fictitious-secret":true`, 1)}
	for _, input := range bad {
		if _, e := ParseDraft([]byte(input)); e == nil || strings.Contains(e.Error(), "fictitious-secret") {
			t.Fatal("invalid draft", e)
		}
	}
	selections := []string{`{"stage":"request","id":"a","operations":[],"unknown":true}`, `{"stage":"request","id":"a","id":"b","operations":[]}`, `{"stage":"request","id":"a","operations":[{"op":"json_set","pointer":"/x","value":{"x":1,"x":2}}]}`, `{"stage":"request","id":"a","operations":[{"op":"header_set","name":"X-A","value":"a","pointer":"/unused"}]}`, `{"stage":"request","id":"a","operations":[],"context":{}}`}
	for _, input := range selections {
		if _, e := ParseSelection([]byte(input)); e == nil {
			t.Fatal("invalid selection accepted", input)
		}
	}
	s, e := ParseSelection([]byte(`{"stage":"request","id":"a","operations":[{"op":"json_set","pointer":"/x","value":9007199254740993}],"match":{"domain":"api.service.example","headers":{"Authorization":["Bearer fictitious"]}}}`))
	if e != nil || string(s.Operations[0].Value) != "9007199254740993" || s.Match.Headers["Authorization"][0] != "Bearer fictitious" {
		t.Fatal(s, e)
	}
}

type activation struct {
	fail            bool
	apply, rollback int
}

func (a *activation) Apply(context.Context, *requestrewrite.Snapshot, *requestrewrite.Snapshot) error {
	a.apply++
	if a.fail {
		return errors.New("fictitious-private")
	}
	return nil
}
func (a *activation) Rollback(context.Context, *requestrewrite.Snapshot) error {
	a.rollback++
	return nil
}
func TestAtomicCASAndRollback(t *testing.T) {
	ctx := context.Background()
	store := requestrewrite.Store{ConfigDir: t.TempDir(), Profile: "test"}
	base := requestrewrite.Empty()
	r, b := sample()
	d, e := FromRecord(r, b, Selection{ID: "draft", Stage: "request", Enable: true, Operations: []requestrewrite.Operation{operation()}}, base)
	if e != nil {
		t.Fatal(e)
	}
	candidate, e := Apply(base, d)
	if e != nil {
		t.Fatal(e)
	}
	a := &activation{}
	store.Activation = a
	if _, e = store.SaveVersion(ctx, candidate, base.Version()); e != nil {
		t.Fatal(e)
	}
	if _, e = store.SaveVersion(ctx, base, base.Version()); e == nil || e.Error() != "rewrite_conflict" || a.apply != 1 {
		t.Fatal("stale save activated", e, a)
	}
	current, e := store.Load()
	if e != nil {
		t.Fatal(e)
	}
	d.BaseRuleVersion = current.Version()
	d.Rule.ID = "second"
	candidate, e = Apply(current, d)
	if e != nil {
		t.Fatal(e)
	}
	a.fail = true
	if _, e = store.SaveVersion(ctx, candidate, current.Version()); e == nil || e.Error() != "rewrite_activation_failed" || a.rollback != 1 {
		t.Fatal("rollback failure", e, a)
	}
	restored, e := store.Load()
	if e != nil || restored.Version() != current.Version() {
		t.Fatal("partial insert persisted", e)
	}
	// Two callers with the same base cannot both commit, even without an outer lock.
	store.Activation = nil
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := store.SaveVersion(ctx, candidate, current.Version()); results <- e }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if e.Error() == "rewrite_conflict" {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}

func TestAllOperationTypesUseExistingValidator(t *testing.T) {
	r, b := sample()
	doc := requestrewrite.Empty().Document()
	doc.Scripts = []requestrewrite.Script{{ID: "existing", BodyAccess: "none", Source: `function transform(input){return {action:"unchanged"}}`}}
	base := compile(t, doc)
	cases := []struct {
		stage string
		op    requestrewrite.Operation
	}{
		{"request", requestrewrite.Operation{Op: "header_set", Name: "X-Test", Value: raw(`"value"`)}},
		{"request", requestrewrite.Operation{Op: "header_append", Name: "X-Test", Value: raw(`"value"`)}},
		{"request", requestrewrite.Operation{Op: "header_delete", Name: "X-Test"}},
		{"request", requestrewrite.Operation{Op: "url_set", Component: "path", Value: raw(`"/new"`)}},
		{"request", requestrewrite.Operation{Op: "method_set", Value: raw(`"PATCH"`)}},
		{"response", requestrewrite.Operation{Op: "status_set", Value: raw(`202`)}},
		{"request", requestrewrite.Operation{Op: "json_set", Pointer: "/n", Value: raw(`9007199254740993`)}},
		{"request", requestrewrite.Operation{Op: "json_delete", Pointer: "/n"}},
		{"request", requestrewrite.Operation{Op: "body_set", Encoding: "text", Value: raw(`"value"`)}},
		{"request", requestrewrite.Operation{Op: "text_replace", Pattern: "old", Replacement: "new", All: false}},
		{"request", requestrewrite.Operation{Op: "regex_replace", Pattern: "(old)", Replacement: "${1}-new", All: true}},
		{"request", requestrewrite.Operation{Op: "trailer_set", Name: "X-Trailer", Value: raw(`"value"`)}},
		{"request", requestrewrite.Operation{Op: "trailer_delete", Name: "X-Trailer"}},
		{"request", requestrewrite.Operation{Op: "script", Script: "existing"}},
	}
	for _, tc := range cases {
		t.Run(tc.op.Op, func(t *testing.T) {
			selection := Selection{ID: "operation", Stage: tc.stage, Operations: []requestrewrite.Operation{tc.op}}
			wire, _ := json.Marshal(selection)
			parsed, e := ParseSelection(wire)
			if e != nil {
				t.Fatal(e)
			}
			draft, e := FromRecord(r, b, parsed, base)
			if e != nil {
				t.Fatal("draft failed existing operation", e)
			}
			candidate, e := Apply(base, draft)
			if e != nil {
				t.Fatal(e)
			}
			if candidate.Document().Rules[0].Operations[0].Op != tc.op.Op {
				t.Fatal("operation lost")
			}
		})
	}
	for _, op := range []requestrewrite.Operation{{Op: "header_set", Name: "Content-Length", Value: raw(`"5"`)}, {Op: "script", Script: "missing"}, {Op: "json_set", Pointer: "bad-pointer", Value: raw(`1`)}, {Op: "status_set", Value: raw(`101`)}} {
		if _, e := FromRecord(r, b, Selection{ID: "invalid", Stage: "request", Operations: []requestrewrite.Operation{op}}, base); e == nil {
			t.Fatal("bypassed existing validator", op.Op)
		}
	}
}

func TestIncompleteOriginalAllowsDraftWithoutInventedFixture(t *testing.T) {
	record := capture.Record{Kind: "http", Snapshots: map[string]capture.Snapshot{"request_original": {Scheme: "https", Host: "fixture.example", Method: "GET", Path: "/stream", BodyState: "cutoff", BodySize: 100, Captured: 2}}}
	selection := Selection{Stage: "request", ID: "header", Operations: []requestrewrite.Operation{{Op: "header_delete", Name: "X-Fixture"}}}
	draft, e := FromRecord(record, map[string][]byte{"request_original": []byte("ab")}, selection, requestrewrite.Empty())
	if e != nil {
		t.Fatal(e)
	}
	if draft.Fixture != nil {
		t.Fatal("incomplete original turned into fixture")
	}
	if _, e = Preview(context.Background(), requestrewrite.Empty(), draft); e == nil {
		t.Fatal("preview accepted missing original bytes")
	}
}
