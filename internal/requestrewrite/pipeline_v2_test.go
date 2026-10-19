package requestrewrite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func responseRule(id string, ops ...Operation) Rule {
	r := rule(id, ops...)
	r.Stage = StageResponse
	return r
}
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
func responseBody(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	v, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	return string(v)
}
func TestResponseUsesOriginalRequestAndResponse(t *testing.T) {
	d := baseDoc()
	first := responseRule("first", Operation{Op: "status_set", Value: raw(`201`)}, Operation{Op: "header_append", Name: "Set-Cookie", Value: str("one=example")}, Operation{Op: "json_set", Pointer: "/a", Value: raw(`1`)})
	first.Match.StatusCodes = []int{200}
	second := responseRule("second", Operation{Op: "header_append", Name: "Set-Cookie", Value: str("two=example")})
	second.Match.StatusCodes = []int{200}
	second.Match.Headers = map[string][]string{"X-Input": {"original"}}
	second.Match.Query = map[string][]string{"q": {"absent", "secret"}}
	second.Match.PathRegex = `^/v1/`
	second.Match.ContentTypes = []string{"application/json"}
	d.Rules = []Rule{first, second}
	s := compiled(t, d)
	req := request(t, "")
	req.Header.Set("X-Input", "original")
	orig := response(`{}`)
	orig.Header.Set("ETag", "old")
	r, e := s.TransformResponse(context.Background(), req, orig)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(r.RuleIDs, ",") != "first,second" || r.Response.StatusCode != 201 || len(r.Response.Header.Values("Set-Cookie")) != 2 || r.Response.Header.Get("ETag") != "" || orig.StatusCode != 200 {
		t.Fatal(r)
	}
	if responseBody(t, r.Response) != `{"a":1}` {
		t.Fatal("body")
	}
}
func TestResponseRollbackEntireStage(t *testing.T) {
	d := baseDoc()
	a := responseRule("header", Operation{Op: "header_set", Name: "X-Changed", Value: str("yes")})
	b := responseRule("fail", Operation{Op: "json_delete", Pointer: "/missing"})
	b.OnError = "forward_original"
	d.Rules = []Rule{a, b}
	orig := response(`{"a":9007199254740993}`)
	r, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), orig)
	if e != nil || r.Outcome != "forward_original" || r.Response.Header.Get("X-Changed") != "" || responseBody(t, r.Response) != `{"a":9007199254740993}` {
		t.Fatal(r, e)
	}
}
func TestRangeDecisionAndNoUnrequestedChange(t *testing.T) {
	d := baseDoc()
	a := responseRule("edit", Operation{Op: "text_replace", Pattern: "old", Replacement: "new", All: true})
	d.Rules = []Rule{a}
	s := compiled(t, d)
	req := request(t, "")
	req.Header.Set("Range", "bytes=0-2")
	req.Header.Set("If-Range", "etag")
	r, e := s.Transform(context.Background(), req)
	if e != nil || r.Request.Header.Get("Range") != "bytes=0-2" {
		t.Fatal(r, e)
	}
	r.Request.Body.Close()
	partial := response("old")
	partial.StatusCode = 206
	partial.Header.Set("Content-Type", "text/plain")
	partial.Header.Set("Content-Range", "bytes 0-2/100")
	rr, e := s.TransformResponse(context.Background(), req, partial)
	if e != nil || responseBody(t, rr.Response) != "new" {
		t.Fatal(rr, e)
	}
	d.Rules[0].RangeMode = "full"
	s = compiled(t, d)
	r, e = s.Transform(context.Background(), req)
	if e != nil || r.Request.Header.Get("Range") != "" || r.Request.Header.Get("If-Range") != "" {
		t.Fatal(r, e)
	}
	r.Request.Body.Close()
	partial = response("old")
	partial.StatusCode = 206
	if _, e = s.TransformResponse(context.Background(), req, partial); e == nil {
		t.Fatal("full accepted partial")
	}
	d.Rules = append(d.Rules, responseRule("conflict", Operation{Op: "body_set", Value: str("new"), Encoding: "text"}))
	if _, e = compiled(t, d).Transform(context.Background(), req); e == nil {
		t.Fatal("range conflict not detected before send")
	}
}
func TestNoBodySemanticsAndStreamingResponse(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{responseRule("header", Operation{Op: "header_set", Name: "X-Edit", Value: str("yes")})}
	s := compiled(t, d)
	orig := response("")
	spy := &spyBody{Reader: strings.NewReader(strings.Repeat("x", MaxBodyBytes+1))}
	orig.Body = spy
	r, e := s.TransformResponse(context.Background(), request(t, ""), orig)
	if e != nil || spy.reads != 0 || r.Response.Body != spy {
		t.Fatal(e)
	}
	r.Response.Body.Close()
	d.Rules = []Rule{responseRule("body", Operation{Op: "body_set", Value: str("bad"), Encoding: "text"})}
	s = compiled(t, d)
	for _, status := range []int{204, 205, 304, 101} {
		orig = response("")
		orig.StatusCode = status
		if _, e = s.TransformResponse(context.Background(), request(t, ""), orig); e == nil {
			t.Fatal(status)
		}
	}
}
func TestDerivedPolicyScriptReferencesAndSummaryRedaction(t *testing.T) {
	d := baseDoc()
	r := rule("source", Operation{Op: "url_set", Component: "host", Value: str("target.example")})
	r.RedirectConsent = []Consent{{Domain: "target.example", Schemes: []string{"https"}, Ports: []int{443}}}
	r.Match.Headers = map[string][]string{"Authorization": {"fictitious-secret"}}
	d.Rules = []Rule{r}
	s := compiled(t, d)
	if strings.Join(s.InterceptDomains(), ",") != "api.service.example" || strings.Join(s.TargetDomains(), ",") != "api.service.example,target.example" || s.Authorized("target.example") {
		t.Fatal("derived domain policy")
	}
	b, _ := json.Marshal(s.RuleSummaries())
	if strings.Contains(string(b), "fictitious-secret") {
		t.Fatal("summary exposes match credentials")
	}
	d.Rules[0].Enabled = false
	s = compiled(t, d)
	if s.HasEnabledRules() {
		t.Fatal("disabled policy retained")
	}
}
func TestScriptBodyAccessAndFinalPermission(t *testing.T) {
	defer SetScriptExecutor(nil)
	d := baseDoc()
	d.Scripts = []Script{{ID: "headers", BodyAccess: "none", Source: "function transform(i){}"}}
	d.Rules = []Rule{rule("script", Operation{Op: "script", Script: "headers"})}
	s := compiled(t, d)
	SetScriptExecutor(func(ctx context.Context, source string, in json.RawMessage) (json.RawMessage, error) {
		if strings.Contains(string(in), "body_base64") {
			t.Fatal("header script given body")
		}
		return raw(`{"action":"replace","message":{"method":"PUT"}}`), nil
	})
	req := request(t, "")
	spy := &spyBody{Reader: strings.NewReader(strings.Repeat("x", MaxBodyBytes+1))}
	req.Body = spy
	r, e := s.Transform(context.Background(), req)
	if e != nil || spy.reads != 0 || r.Request.Method != "PUT" {
		t.Fatal(r, e)
	}
	r.Request.Body.Close()
	SetScriptExecutor(func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return raw(`{"action":"replace","message":{"body_base64":"e30="}}`), nil
	})
	if _, e = s.Transform(context.Background(), request(t, "{}")); e == nil {
		t.Fatal("none script replaced body")
	}
	SetScriptExecutor(func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return raw(`{"action":"replace","message":{"url":"https://target.example/v1/x"}}`), nil
	})
	if _, e = s.Transform(context.Background(), request(t, "{}")); e == nil {
		t.Fatal("script bypassed final target permission")
	}
}
func TestWebSocketOriginalMessageAndFallback(t *testing.T) {
	d := baseDoc()
	r := rule("message", Operation{Op: "text_replace", Pattern: "old", Replacement: "new", All: true})
	r.Stage = StageWebSocketClient
	r.Match.MessageTypes = []string{"text"}
	d.Rules = []Rule{r}
	s := compiled(t, d)
	req := request(t, "")
	hs := response("")
	hs.StatusCode = 101
	out, e := s.TransformMessage(context.Background(), StageWebSocketClient, req, hs, Message{Type: "text", Body: []byte("old")})
	if e != nil || string(out.Message.Body) != "new" {
		t.Fatal(out, e)
	}
	d.Rules[0].Operations = []Operation{{Op: "body_set", Encoding: "base64", Value: str("/w==")}}
	d.Rules[0].OnError = "forward_original"
	out, e = compiled(t, d).TransformMessage(context.Background(), StageWebSocketClient, req, hs, Message{Type: "text", Body: []byte("original")})
	if e != nil || out.Outcome != "forward_original" || string(out.Message.Body) != "original" {
		t.Fatal(out, e)
	}
}
func TestPreviewExchangeAndMessage(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("request", Operation{Op: "header_set", Name: "X-Demo", Value: str("yes")}), responseRule("response", Operation{Op: "body_set", Encoding: "text", Value: str("new")})}
	s := compiled(t, d)
	in := PreviewInput{Stage: "exchange", Request: Fixture{Scheme: "https", Host: "api.service.example", Port: 443, Method: "POST", Path: "/v1/demo", Headers: map[string][]string{}, BodyBase64: ""}, Response: &ResponseFixture{Status: 200, Headers: map[string][]string{"Content-Type": {"text/plain"}}, BodyBase64: base64.StdEncoding.EncodeToString([]byte("old"))}}
	raw, _ := json.Marshal(in)
	out, e := s.PreviewSensitive(context.Background(), raw)
	if e != nil || len(out.Stages) != 2 || out.Response.BodyBase64 != "bmV3" || out.Request.Headers["X-Demo"][0] != "yes" {
		t.Fatal(out, e)
	}
}
