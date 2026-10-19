package requestrewrite

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func request(t *testing.T, body string) *http.Request {
	t.Helper()
	r, e := http.NewRequest("POST", "https://api.service.example/v1/items?q=secret", strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}
func output(t *testing.T, result Result) string {
	t.Helper()
	if result.Request.Body == nil {
		return ""
	}
	defer result.Request.Body.Close()
	b, e := io.ReadAll(result.Request.Body)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestOriginalMatchingCumulativeAndPointerNumbers(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("first", Operation{Op: "url_set", Component: "path", Value: str("/different")}, Operation{Op: "json_set", Pointer: "/options/a~1b", Value: raw(`true`)}, Operation{Op: "header_set", Name: "Authorization", Value: str("fictitious-secret")}), rule("second", Operation{Op: "json_set", Pointer: "/arr/-", Value: raw(`9007199254740993`)}, Operation{Op: "json_delete", Pointer: "/arr/0"})}
	s := compiled(t, d)
	r := request(t, `{"options":{},"arr":[1,2],"large":9007199254740993}`)
	result, e := s.Transform(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(result.RuleIDs, ",") != "first,second" || result.Request.URL.Path != "/different" || r.URL.Path != "/v1/items" {
		t.Fatal("original matching/immutability")
	}
	if got := output(t, result); got != `{"arr":[2,9007199254740993],"large":9007199254740993,"options":{"a/b":true}}` {
		t.Fatal(got)
	}
	if result.Request.Header.Get("Authorization") != "fictitious-secret" || r.Header.Get("Authorization") != "" {
		t.Fatal("headers")
	}
}
func TestFinalConsentEveryTargetContributor(t *testing.T) {
	d := baseDoc()
	first := rule("first", Operation{Op: "url_set", Component: "host", Value: str("api.service.example")})
	second := rule("second", Operation{Op: "url_set", Component: "host", Value: str("mirror.service.example")})
	allow := []Consent{{Domain: "mirror.service.example", Schemes: []string{"https"}, Ports: []int{443}}}
	second.RedirectConsent = allow
	d.Rules = []Rule{first, second}
	if _, e := compiled(t, d).Transform(context.Background(), request(t, "stream")); e == nil {
		t.Fatal("no-op contributor bypassed")
	}
	d.Rules[0].RedirectConsent = allow
	s := compiled(t, d)
	result, e := s.Transform(context.Background(), request(t, "stream"))
	if e != nil {
		t.Fatal(e)
	}
	if result.Request.Host != "mirror.service.example" || result.Request.URL.Host != "mirror.service.example" {
		t.Fatal("authority")
	}
	output(t, result)
	d.Rules = append(d.Rules, rule("restore", Operation{Op: "url_set", Component: "host", Value: str("api.service.example")}))
	for i := range d.Rules {
		d.Rules[i].RedirectConsent = []Consent{}
	}
	result, e = compiled(t, d).Transform(context.Background(), request(t, "stream"))
	if e != nil {
		t.Fatal("final original needs no consent", e)
	}
	output(t, result)
}
func TestRollbackDiscardsAllRulesAndExactGzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte(`{"a":1}`))
	gz.Close()
	original := append([]byte{}, buf.Bytes()...)
	d := baseDoc()
	d.Rules = []Rule{rule("first", Operation{Op: "header_set", Name: "Cookie", Value: str("new-secret")}, Operation{Op: "url_set", Component: "query", Value: str("changed=true")}), rule("fail", Operation{Op: "json_delete", Pointer: "/missing"})}
	d.Rules[1].OnError = "forward_original"
	r := request(t, string(original))
	r.Header.Set("Content-Encoding", "gzip")
	r.Header.Set("Cookie", "old-secret")
	result, e := compiled(t, d).Transform(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if result.Outcome != "forward_original" || result.Request.Header.Get("Cookie") != "old-secret" || result.Request.URL.RawQuery != "q=secret" || output(t, result) != string(original) {
		t.Fatal("rollback changed original")
	}
}
func TestGzipJSONValidationAndBounds(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("json", Operation{Op: "json_set", Pointer: "/x", Value: raw(`true`)})}
	s := compiled(t, d)
	pack := func(raw string) string {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		gz.Write([]byte(raw))
		gz.Close()
		return b.String()
	}
	for _, tc := range []struct{ body, encoding string }{{`{"a":1,"a":2}`, ""}, {`{} {}`, ""}, {`{"x":"` + string([]byte{255}) + `"}`, ""}, {"bad", "gzip"}, {pack(`{}`) + "trailing", "gzip"}, {`{}`, "br"}, {pack(`{"x":"` + strings.Repeat("z", MaxBodyBytes) + `"}`), "gzip"}} {
		r := request(t, tc.body)
		r.Header.Set("Content-Encoding", tc.encoding)
		if result, e := s.Transform(context.Background(), r); e == nil {
			result.Request.Body.Close()
			t.Fatal("accepted bad body/encoding")
		}
	}
	r := request(t, pack(`{"n":9007199254740993}`))
	r.Header.Set("Content-Encoding", "gzip")
	r.Header.Set("Digest", "stale")
	result, e := s.Transform(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	gz, e := gzip.NewReader(strings.NewReader(output(t, result)))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(gz)
	gz.Close()
	if e != nil || string(raw) != `{"n":9007199254740993,"x":true}` || result.Request.Header.Get("Digest") != "" || result.Request.ContentLength <= 0 {
		t.Fatal("gzip result", e)
	}
	// Replacing a short existing string with a valid document value can exceed
	// the output limit although the input itself fits the boundary.
	d.Rules[0].Operations[0] = Operation{Op: "json_set", Pointer: "/y", Value: str(strings.Repeat("x", 200000))}
	r = request(t, `{"x":"`+strings.Repeat("a", MaxBodyBytes-32)+`"}`)
	if result, e := compiled(t, d).Transform(context.Background(), r); e == nil {
		result.Request.Body.Close()
		t.Fatal("output limit")
	}
}

type spyBody struct {
	reads, closes int
	io.Reader
}

func (b *spyBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *spyBody) Close() error               { b.closes++; return nil }
func TestStreamingAdmissionAndNoConsumption(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("header", Operation{Op: "header_set", Name: "X-Test", Value: str("yes")})}
	r := request(t, "")
	b := &spyBody{Reader: strings.NewReader(strings.Repeat("x", MaxBodyBytes+1))}
	r.Body = b
	r.Header.Set("Content-Encoding", "br")
	result, e := compiled(t, d).Transform(context.Background(), r)
	if e != nil || b.reads != 0 || result.Request.Body != b {
		t.Fatal("non-json body buffered", e)
	}
	result.Request.Body.Close()
	d.Rules = []Rule{rule("json", Operation{Op: "json_set", Pointer: "/x", Value: raw(`1`)})}
	s := compiled(t, d)
	// Completed transformations release their workspace; returned body owns only
	// the final bytes and can be read without holding the conversion reservation.
	one, e := s.Transform(context.Background(), request(t, `{}`))
	if e != nil {
		t.Fatal(e)
	}
	two, e := s.Transform(context.Background(), request(t, `{}`))
	if e != nil {
		t.Fatal(e)
	}
	output(t, one)
	output(t, two)
}
func TestPointerErrorsAndRollbackUnconsumed(t *testing.T) {
	for _, pointer := range []string{"/array/01", "/array/3", "/array/-/x", "/missing/x"} {
		d := baseDoc()
		d.Rules = []Rule{rule("bad", Operation{Op: "json_set", Pointer: pointer, Value: raw(`true`)})}
		if result, e := compiled(t, d).Transform(context.Background(), request(t, `{"array":[1]}`)); e == nil {
			result.Request.Body.Close()
			t.Error(pointer)
		}
	}
	d := baseDoc()
	d.Rules = []Rule{rule("original", Operation{Op: "json_set", Pointer: "/x", Value: raw(`1`)})}
	d.Rules[0].OnError = "forward_original"
	r := request(t, "opaque")
	r.Header.Set("Content-Encoding", "br")
	result, e := compiled(t, d).Transform(context.Background(), r)
	if e != nil || output(t, result) != "opaque" {
		t.Fatal("unconsumed original fallback", e)
	}
}
func TestAuthorityAndFramingBlockedWithoutFallback(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("original", Operation{Op: "header_set", Name: "X-Test", Value: str("okay")})}
	d.Rules[0].OnError = "forward_original"
	s := compiled(t, d)
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Host = "unauthorized.example" }, func(r *http.Request) { r.URL.Host = "unauthorized.example"; r.Host = r.URL.Host }, func(r *http.Request) { r.Header["Content-Length"] = []string{"2", "3"} }, func(r *http.Request) { r.Header.Set("X-Test", "control\nsecret") }, func(r *http.Request) { r.TransferEncoding = []string{"chunked"}; r.Header.Set("Content-Length", "2") }} {
		r := request(t, "{}")
		mutate(r)
		if _, e := s.Transform(context.Background(), r); e == nil {
			t.Fatal("unsafe framing/authority")
		}
	}
}

type blockedBody struct {
	closed chan struct{}
	once   sync.Once
	closes int
}

func (b *blockedBody) Read(p []byte) (int, error) { <-b.closed; return 0, io.ErrClosedPipe }
func (b *blockedBody) Close() error               { b.once.Do(func() { b.closes++; close(b.closed) }); return nil }
func TestCancellationUnblocksReadAndReleasesReservation(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("json", Operation{Op: "json_set", Pointer: "/x", Value: raw(`1`)})}
	d.Rules[0].OnError = "forward_original"
	s := compiled(t, d)
	ctx, cancel := context.WithCancel(context.Background())
	r := request(t, "")
	body := &blockedBody{closed: make(chan struct{})}
	r.Body = body
	done := make(chan error, 1)
	go func() { _, e := s.Transform(ctx, r); done <- e }()
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled original forwarded")
		}
	case <-time.After(time.Second):
		t.Fatal("read cancellation stuck")
	}
	result, e := s.Transform(context.Background(), request(t, `{}`))
	if e != nil {
		t.Fatal("reservation retained", e)
	}
	output(t, result)
}
func TestJSONReadFailureAndTrailerCannotForwardPartial(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("json", Operation{Op: "json_set", Pointer: "/x", Value: raw(`1`)})}
	d.Rules[0].OnError = "forward_original"
	s := compiled(t, d)
	r := request(t, "")
	r.Body = &partialBody{}
	if _, e := s.Transform(context.Background(), r); e == nil {
		t.Fatal("partial original forwarded")
	}
	r = request(t, `{}`)
	r.Trailer = http.Header{"X-Late": nil}
	result, e := s.Transform(context.Background(), r)
	if e != nil || result.Outcome != "rewritten" {
		t.Fatal("body with trailer", e)
	}
	output(t, result)
}

type partialBody struct{}

func (*partialBody) Read(p []byte) (int, error) { copy(p, "{"); return 1, io.ErrUnexpectedEOF }
func (*partialBody) Close() error               { return nil }

// cancelAtCheck provides deterministic cancellation at a chosen Err checkpoint,
// allowing the commit boundary to be tested without timing-dependent large work.
type cancelAtCheck struct {
	context.Context
	checks, at int
	done       chan struct{}
}

func (c *cancelAtCheck) Done() <-chan struct{} { return c.done }
func (c *cancelAtCheck) Err() error {
	c.checks++
	if c.checks >= c.at {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
		return context.Canceled
	}
	return nil
}
func TestCancellationAtEntryAndCommitWithoutMatchingRules(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("entry", Operation{Op: "header_delete", Name: "X-Test"})}
	s := compiled(t, d)
	for _, at := range []int{1, 2} {
		ctx := &cancelAtCheck{Context: context.Background(), at: at, done: make(chan struct{})}
		if result, e := s.Transform(ctx, request(t, "stream")); e == nil || result.Request != nil {
			t.Fatal("cancelled request committed", at, e)
		}
	}
}
func TestCancellationAfterJSONSerializationReleasesBody(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("json", Operation{Op: "json_set", Pointer: "/x", Value: raw(`1`)})}
	s := compiled(t, d)
	ctx := &cancelAtCheck{Context: context.Background(), at: 3, done: make(chan struct{})}
	if result, e := s.Transform(ctx, request(t, `{}`)); e == nil || result.Request != nil {
		t.Fatal("cancelled output committed", e)
	}
	result, e := s.Transform(context.Background(), request(t, `{}`))
	if e != nil {
		t.Fatal("cancelled output leaked reservation", e)
	}
	output(t, result)
}
func TestDuplicateAuthorityEmptyFramingAndContext(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("entry", Operation{Op: "header_delete", Name: "X-Test"})}
	s := compiled(t, d)
	for _, h := range []http.Header{{"Host": {"api.service.example", "evil.example"}}, {"Host": {}}, {"Content-Length": {""}}, {"Content-Length": {"+2"}}, {"Content-Length": {"-0"}}, {"Transfer-Encoding": {""}}, {"content-length": {"1"}}} {
		r := request(t, `{}`)
		r.Header = h
		if _, e := s.Transform(context.Background(), r); e == nil {
			t.Fatal("ambiguous framing", h)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, e := s.Transform(ctx, request(t, "stream"))
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if result.Request.Context().Err() != context.Canceled {
		t.Fatal("transport request lost transaction cancellation")
	}
	result.Request.Body.Close()
}
