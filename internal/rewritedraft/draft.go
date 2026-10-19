// Package rewritedraft turns explicitly viewed original captures into private,
// validated rules. It never sends network traffic or changes global intent.
package rewritedraft

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"

	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/requestrewrite"
)

type Draft struct {
	BaseRuleVersion string                       `json:"base_rule_version"`
	Position        int                          `json:"position"`
	Rule            requestrewrite.Rule          `json:"rule"`
	Fixture         *requestrewrite.PreviewInput `json:"fixture,omitempty"`
	Enable          bool                         `json:"enable"`
}

// Selection is the capture draft input wire format. stage, id and operations
// are required. match overrides the default domain/method/exact escaped path;
// it may explicitly include raw query/header values. position defaults to append,
// on_error to block, and enable to false. Existing rewrite validation owns every
// operation and predicate. Context is supplied by the caller for WS messages and
// is never accepted from JSON; retrieve the handshake by the record's parent_id.
type Selection struct {
	Stage           string                     `json:"stage"`
	ID              string                     `json:"id"`
	Operations      []requestrewrite.Operation `json:"operations"`
	Match           *requestrewrite.Match      `json:"match,omitempty"`
	Position        *int                       `json:"position,omitempty"`
	Enable          bool                       `json:"enable,omitempty"`
	OnError         string                     `json:"on_error,omitempty"`
	RangeMode       string                     `json:"range_mode,omitempty"`
	RedirectConsent []requestrewrite.Consent   `json:"redirect_consent,omitempty"`
	Context         *Context                   `json:"-"`
}

type Context struct {
	Record capture.Record
	Bodies map[string][]byte
}

func invalid() error { return &requestrewrite.Error{Code: "rewrite_invalid"} }

// Fixture preserves original encoded entity bytes and their encoding headers.
// Actual/rewritten snapshots are never used. Incomplete bodies cannot be fixtures.
func Fixture(record capture.Record, bodies map[string][]byte, stage string, parents ...Context) (requestrewrite.PreviewInput, error) {
	var out requestrewrite.PreviewInput
	original := record
	originalBodies := bodies
	switch stage {
	case requestrewrite.StageRequest, requestrewrite.StageResponse, "exchange":
		if record.Kind != "http" {
			return out, invalid()
		}
	case requestrewrite.StageWebSocketClient, requestrewrite.StageWebSocketServer:
		if record.Kind != "websocket" || record.Stage != stage || len(parents) != 1 || parents[0].Record.ID != record.ParentID || record.ParentID == "" || parents[0].Record.Kind != "http" || parents[0].Record.SessionID != record.SessionID {
			return out, invalid()
		}
		original = parents[0].Record
		originalBodies = parents[0].Bodies
	default:
		return out, invalid()
	}
	get := func(r capture.Record, bs map[string][]byte, name string) (capture.Snapshot, []byte, error) {
		s, ok := r.Snapshots[name]
		b := bs[name]
		if !ok || s.BodyState != "complete" || s.BodySize < 0 || s.Captured != s.BodySize || int64(len(b)) != s.BodySize || len(b) > requestrewrite.MaxBodyBytes {
			return s, nil, invalid()
		}
		return s, b, nil
	}
	s, b, e := get(original, originalBodies, "request_original")
	if e != nil {
		return out, e
	}
	out.Stage = stage
	out.Request = requestrewrite.Fixture{Scheme: s.Scheme, Host: s.Host, Port: s.Port, Method: s.Method, Path: s.Path, Query: s.Query, Headers: copyHeaders(s.Headers), Trailers: copyHeaders(s.Trailers), BodyBase64: base64.StdEncoding.EncodeToString(b)}
	if stage != requestrewrite.StageRequest {
		s, b, e = get(original, originalBodies, "response_original")
		if e != nil {
			return out, e
		}
		out.Response = &requestrewrite.ResponseFixture{Status: s.Status, Headers: copyHeaders(s.Headers), Trailers: copyHeaders(s.Trailers), BodyBase64: base64.StdEncoding.EncodeToString(b)}
	}
	if record.Kind == "websocket" {
		s, b, e = get(record, bodies, "message_original")
		if e != nil {
			return out, e
		}
		out.Message = &requestrewrite.MessageFixture{Type: s.MessageType, BodyBase64: base64.StdEncoding.EncodeToString(b)}
	}
	// Use the existing fixture validator without rules, scripts, or network work.
	input, e := json.Marshal(out)
	if e != nil {
		return out, invalid()
	}
	if _, e = validateFixture(input); e != nil {
		return requestrewrite.PreviewInput{}, e
	}
	return out, nil
}

func copyHeaders(h map[string][]string) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = append([]string{}, v...)
	}
	return out
}

func FromRecord(record capture.Record, bodies map[string][]byte, selection Selection, base *requestrewrite.Snapshot) (Draft, error) {
	if base == nil {
		return Draft{}, invalid()
	}
	var parents []Context
	if selection.Context != nil {
		parents = append(parents, *selection.Context)
	}
	fixture, e := Fixture(record, bodies, selection.Stage, parents...)
	completeFixture := e == nil
	if e != nil {
		// A rule can be derived from trustworthy original metadata when a stream
		// body is incomplete. Keep fixture absent: preview must not invent bytes.
		original := record
		switch selection.Stage {
		case requestrewrite.StageRequest, requestrewrite.StageResponse, "exchange":
			if record.Kind != "http" {
				return Draft{}, invalid()
			}
		case requestrewrite.StageWebSocketClient, requestrewrite.StageWebSocketServer:
			if record.Kind != "websocket" || record.Stage != selection.Stage || len(parents) != 1 || parents[0].Record.ID != record.ParentID || parents[0].Record.SessionID != record.SessionID {
				return Draft{}, invalid()
			}
			original = parents[0].Record
		default:
			return Draft{}, invalid()
		}
		request, ok := original.Snapshots["request_original"]
		if !ok || request.Host == "" || request.Method == "" {
			return Draft{}, invalid()
		}
		fixture = requestrewrite.PreviewInput{Stage: selection.Stage, Request: requestrewrite.Fixture{Scheme: request.Scheme, Host: request.Host, Port: request.Port, Method: request.Method, Path: request.Path, Query: request.Query, Headers: copyHeaders(request.Headers)}}
	}
	match := requestrewrite.Match{Domain: fixture.Request.Host, Methods: []string{fixture.Request.Method}, PathRegex: "^" + regexp.QuoteMeta(fixture.Request.Path) + "$"}
	if selection.Match != nil {
		match = *selection.Match
	}
	position := len(base.Document().Rules)
	if selection.Position != nil {
		position = *selection.Position
	}
	onError := selection.OnError
	if onError == "" {
		onError = "block"
	}
	consent := selection.RedirectConsent
	if consent == nil {
		consent = []requestrewrite.Consent{}
	}
	d := Draft{BaseRuleVersion: base.Version(), Position: position, Rule: requestrewrite.Rule{ID: selection.ID, Stage: selection.Stage, Match: match, OnError: onError, RangeMode: selection.RangeMode, RedirectConsent: consent, Operations: selection.Operations}, Enable: selection.Enable}
	if completeFixture {
		d.Fixture = &fixture
	}
	if _, e = Apply(base, d); e != nil {
		return Draft{}, e
	}
	// Detach rule values and optional matching maps from caller-owned memory.
	data, e := json.Marshal(d)
	if e != nil {
		return Draft{}, invalid()
	}
	return ParseDraft(data)
}

// Apply checks the version and compiles one insertion, preserving global intent.
// A draft's enable field is the sole authority for the inserted rule's enabled flag.
func Apply(base *requestrewrite.Snapshot, draft Draft) (*requestrewrite.Snapshot, error) {
	if base == nil {
		return nil, invalid()
	}
	if draft.BaseRuleVersion != base.Version() {
		return nil, &requestrewrite.Error{Code: "rewrite_conflict"}
	}
	doc := base.Document()
	if draft.Position < 0 || draft.Position > len(doc.Rules) {
		return nil, invalid()
	}
	rule := draft.Rule
	rule.Enabled = draft.Enable
	doc.Rules = append(doc.Rules, requestrewrite.Rule{})
	copy(doc.Rules[draft.Position+1:], doc.Rules[draft.Position:])
	doc.Rules[draft.Position] = rule
	return requestrewrite.Compile(doc)
}

// Preview enables the candidate only in an ephemeral snapshot and runs the full
// existing ordered stage pipeline against the supplied original fixture.
func Preview(ctx context.Context, base *requestrewrite.Snapshot, draft Draft) (requestrewrite.PreviewResult, error) {
	if draft.Fixture == nil {
		return requestrewrite.PreviewResult{}, invalid()
	}
	draft.Enable = true
	candidate, e := Apply(base, draft)
	if e != nil {
		return requestrewrite.PreviewResult{}, e
	}
	data, e := json.Marshal(draft.Fixture)
	if e != nil {
		return requestrewrite.PreviewResult{}, invalid()
	}
	return candidate.PreviewSensitive(ctx, data)
}

// validateFixture uses a harmless domain-authorized snapshot because the request
// pipeline correctly rejects fixtures outside its interception scope.
func validateFixture(data []byte) (requestrewrite.PreviewResult, error) {
	var input requestrewrite.PreviewInput
	if json.Unmarshal(data, &input) != nil {
		return requestrewrite.PreviewResult{}, invalid()
	}
	doc := requestrewrite.Empty().Document()
	doc.Rules = []requestrewrite.Rule{{ID: "fixture-validation", Enabled: true, Stage: requestrewrite.StageRequest, Match: requestrewrite.Match{Domain: input.Request.Host}, OnError: "block", RedirectConsent: []requestrewrite.Consent{}, Operations: []requestrewrite.Operation{{Op: "header_delete", Name: "X-Nagi-Fixture-Validation"}}}}
	snapshot, e := requestrewrite.Compile(doc)
	if e != nil {
		return requestrewrite.PreviewResult{}, e
	}
	return snapshot.PreviewSensitive(context.Background(), data)
}
