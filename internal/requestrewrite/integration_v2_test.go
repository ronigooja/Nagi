package requestrewrite

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type eofTrailerSource struct {
	reader  *strings.Reader
	trailer http.Header
}

func (s *eofTrailerSource) Read(p []byte) (int, error) {
	n, e := s.reader.Read(p)
	if e == io.EOF {
		s.trailer.Set("X-Origin", "at-eof")
	}
	return n, e
}
func (s *eofTrailerSource) Close() error { return nil }
func lateTrailerBody(text string, trailer http.Header) io.ReadCloser {
	return &eofTrailerSource{strings.NewReader(text), trailer}
}

func TestIntegrationTrailersSurviveStreamingAndBufferedStages(t *testing.T) {
	for _, stage := range []string{StageRequest, StageResponse} {
		for _, buffered := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/stream", true: "/buffer"}[buffered], func(t *testing.T) {
				op := Operation{Op: "header_set", Name: "X-Edited", Value: str("yes")}
				if buffered {
					op = Operation{Op: "text_replace", Pattern: "old", Replacement: "new", All: true}
				}
				d := baseDoc()
				r := rule("edit", op)
				r.Stage = stage
				d.Rules = []Rule{r}
				snapshot := compiled(t, d)
				req := request(t, "")
				req.Header.Set("Content-Type", "text/plain")
				trailer := http.Header{"X-Origin": nil}
				var body io.ReadCloser
				var resultTrailer http.Header
				if stage == StageRequest {
					req.Body = lateTrailerBody("old", trailer)
					req.ContentLength = -1
					req.Trailer = trailer
					out, e := snapshot.Transform(context.Background(), req)
					if e != nil {
						t.Fatal(e)
					}
					body = out.Request.Body
					resultTrailer = out.Request.Trailer
				} else {
					resp := response("old")
					resp.Header.Set("Content-Type", "text/plain")
					resp.Body = lateTrailerBody("old", trailer)
					resp.Trailer = trailer
					out, e := snapshot.TransformResponse(context.Background(), req, resp)
					if e != nil {
						t.Fatal(e)
					}
					body = out.Response.Body
					resultTrailer = out.Response.Trailer
				}
				defer body.Close()
				if _, e := io.ReadAll(body); e != nil {
					t.Fatal(e)
				}
				if resultTrailer.Get("X-Origin") != "at-eof" {
					t.Fatalf("lost upstream EOF trailer: %v", resultTrailer)
				}
			})
		}
	}
}

func TestIntegrationTrailerScriptStoredOrder(t *testing.T) {
	oldExecutor := func() ScriptExecutor {
		scriptExecutor.RLock()
		defer scriptExecutor.RUnlock()
		return scriptExecutor.run
	}()
	defer SetScriptExecutor(oldExecutor)
	SetScriptExecutor(func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
		var envelope struct {
			Message struct {
				Trailers http.Header `json:"trailers"`
			} `json:"message"`
		}
		if json.Unmarshal(input, &envelope) != nil {
			t.Fatal("input")
		}
		if envelope.Message.Trailers.Get("X-Origin") != "at-eof" || envelope.Message.Trailers.Get("X-Order") != "first" {
			t.Fatal("script observed stale trailers", envelope.Message.Trailers)
		}
		return json.RawMessage(`{"action":"replace","message":{"trailers":{"X-Origin":["at-eof"],"X-Order":["script"],"X-Script":["kept"]}}}`), nil
	})
	for _, stage := range []string{StageRequest, StageResponse} {
		d := baseDoc()
		d.Scripts = []Script{{ID: "local", BodyAccess: "buffer", Source: "function transform(input){return {action:'unchanged'}}"}}
		r := rule("edit", Operation{Op: "trailer_set", Name: "X-Order", Value: str("first")}, Operation{Op: "script", Script: "local"}, Operation{Op: "trailer_set", Name: "X-Order", Value: str("last")})
		r.Stage = stage
		d.Rules = []Rule{r}
		s := compiled(t, d)
		req := request(t, "")
		req.Header.Set("Content-Type", "text/plain")
		trailer := http.Header{"X-Origin": nil}
		var final http.Header
		var body io.ReadCloser
		if stage == StageRequest {
			req.ContentLength = -1
			req.Trailer = trailer
			req.Body = lateTrailerBody("old", trailer)
			out, e := s.Transform(context.Background(), req)
			if e != nil {
				t.Fatal(e)
			}
			final = out.Request.Trailer
			body = out.Request.Body
		} else {
			resp := response("old")
			resp.Header.Set("Content-Type", "text/plain")
			resp.Trailer = trailer
			resp.Body = lateTrailerBody("old", trailer)
			out, e := s.TransformResponse(context.Background(), req, resp)
			if e != nil {
				t.Fatal(e)
			}
			final = out.Response.Trailer
			body = out.Response.Body
		}
		if final.Get("X-Order") != "last" || final.Get("X-Script") != "kept" {
			body.Close()
			t.Fatal("trailer edits reordered", final)
		}
		body.Close()
	}
}

func TestIntegrationReservationAcrossStagesAndRollback(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("body", Operation{Op: "json_set", Pointer: "/x", Value: raw(`1`)})}
	s := compiled(t, d)
	first, e := s.Transform(context.Background(), request(t, `{}`))
	if e != nil {
		t.Fatal(e)
	}
	defer first.Request.Body.Close()
	// Saturation still permits forward_original when the original was not read.
	d.Rules[0].OnError = "forward_original"
	fallback := compiled(t, d)
	source := &spyBody{Reader: strings.NewReader(`{}`)}
	req := request(t, `{}`)
	req.Body = source
	second, e := fallback.Transform(context.Background(), req)
	if e != nil || second.Outcome != "forward_original" || source.reads != 0 {
		t.Fatal(second, e)
	}
	second.Request.Body.Close()
	// Message transforms share the aggregate admission gate.
	ws := rule("message", Operation{Op: "body_set", Encoding: "text", Value: str("new")})
	ws.Stage = StageWebSocketClient
	d.Rules = []Rule{ws}
	if _, e := compiled(t, d).TransformMessage(context.Background(), StageWebSocketClient, request(t, ""), &http.Response{StatusCode: 101, Header: http.Header{}}, Message{Type: "text", Body: []byte("old")}); e == nil {
		t.Fatal("message escaped workspace admission")
	}
	first.Request.Body.Close()
	third, e := s.Transform(context.Background(), request(t, `{}`))
	if e != nil {
		t.Fatal(e)
	}
	third.Request.Body.Close()
}

func TestIntegrationInformationalAndNoBodyResponses(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{responseRule("edit", Operation{Op: "body_set", Encoding: "text", Value: str("new")})}
	s := compiled(t, d)
	for _, status := range []int{100, 102, 103, 204, 205, 304} {
		resp := response("old")
		resp.StatusCode = status
		resp.Header.Set("Content-Type", "text/plain")
		out, e := s.TransformResponse(context.Background(), request(t, ""), resp)
		if e == nil {
			out.Response.Body.Close()
			t.Fatalf("body rewrite accepted status %d", status)
		}
	}
}

func TestIntegrationMaximumBodyThroughPreview(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("text", Operation{Op: "text_replace", Pattern: "a", Replacement: "b", All: true})}
	s := compiled(t, d)
	max := bytes.Repeat([]byte("a"), MaxBodyBytes)
	fixture := PreviewInput{Stage: StageRequest, Request: Fixture{Scheme: "https", Host: "api.service.example", Port: 443, Method: "GET", Path: "/v1/", Headers: map[string][]string{"Content-Type": {"text/plain"}}, BodyBase64: base64.StdEncoding.EncodeToString(max)}}
	input, e := json.Marshal(fixture)
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.PreviewSensitive(context.Background(), input)
	if e != nil {
		t.Fatal(e)
	}
	output, e := base64.StdEncoding.Strict().DecodeString(result.Request.BodyBase64)
	if e != nil || len(output) != MaxBodyBytes || output[0] != 'b' || output[len(output)-1] != 'b' {
		t.Fatal("maximum preview output", len(output), e)
	}
	fixture.Request.BodyBase64 = base64.StdEncoding.EncodeToString(append(max, 'a'))
	input, _ = json.Marshal(fixture)
	if _, e = s.PreviewSensitive(context.Background(), input); e == nil {
		t.Fatal("oversized preview accepted")
	}
}

func TestIntegrationSnapshotRegexAndCallerIsolation(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("regex", Operation{Op: "regex_replace", Pattern: "(old)", Replacement: "${1}-new", All: true})}
	d.Rules[0].Match.PathRegex = "^/v1/"
	s := compiled(t, d)
	version := s.Version()
	d.Rules[0].Match.PathRegex = "["
	d.Rules[0].Operations[0].Pattern = "["
	if _, e := Compile(d); e == nil {
		t.Fatal("invalid regex saved")
	}
	exported := s.Document()
	exported.Rules[0].Match.Domain = "other.example"
	exported.Rules[0].Operations[0].Replacement = "corrupted"
	req := request(t, "old")
	req.Header.Set("Content-Type", "text/plain")
	out, e := s.Transform(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	defer out.Request.Body.Close()
	body, e := io.ReadAll(out.Request.Body)
	if e != nil || string(body) != "old-new" || s.Version() != version {
		t.Fatal(string(body), e)
	}
}

func TestIntegrationScriptCannotFallbackOnTruncatedInput(t *testing.T) {
	d := baseDoc()
	d.Scripts = []Script{{ID: "local", BodyAccess: "buffer", Source: "function transform(input){return {action:'unchanged'}}"}}
	r := rule("script", Operation{Op: "script", Script: "local"})
	r.OnError = "forward_original"
	d.Rules = []Rule{r}
	s := compiled(t, d)
	req := request(t, "")
	req.Body = &integrationBrokenBody{}
	req.ContentLength = -1
	if _, e := s.Transform(context.Background(), req); e == nil {
		t.Fatal("forwarded truncated body")
	}
}

type integrationBrokenBody struct{ done bool }

func (b *integrationBrokenBody) Read(p []byte) (int, error) {
	if !b.done {
		b.done = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("fictitious upstream failure")
}
func (b *integrationBrokenBody) Close() error { return nil }

func TestIntegrationUpstreamHEADHasNoDownstreamEntity(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{responseRule("header", Operation{Op: "header_set", Name: "X-Edited", Value: str("yes")})}
	s := compiled(t, d)
	original := request(t, "")
	original.Method = "GET"
	upstream := original.Clone(context.Background())
	upstream.Method = "HEAD"
	resp := response("")
	resp.Request = upstream
	resp.ContentLength = 900
	resp.Header.Set("Content-Length", "900")
	out, e := s.TransformResponse(context.Background(), original, resp)
	if e != nil {
		t.Fatal(e)
	}
	defer out.Response.Body.Close()
	if out.Response.ContentLength != 0 || out.Response.Header.Get("Content-Length") != "" || out.Response.Body != http.NoBody {
		t.Fatal("HEAD metadata advertised unavailable GET entity", out.Response)
	}
	d.Rules = []Rule{responseRule("body", Operation{Op: "body_set", Encoding: "text", Value: str("new")})}
	resp = response("")
	resp.Header.Set("Content-Type", "text/plain")
	resp.Request = upstream
	if _, e := compiled(t, d).TransformResponse(context.Background(), original, resp); e == nil {
		t.Fatal("body operation accepted actual upstream HEAD")
	}
}

func TestIntegrationJSONCharsetLossless(t *testing.T) {
	h := http.Header{"Content-Type": {"application/json; charset=windows-1252"}}
	raw := append([]byte(`{"price":"`), 0x80)
	raw = append(raw, []byte(`","n":9007199254740993}`)...)
	output, e := BodyTransform(context.Background(), raw, h, []Operation{{Op: "json_set", Pointer: "/price", Value: str("€5")}})
	want := []byte(`{"n":9007199254740993,"price":"`)
	want = append(want, 0x80, '5', '"', '}')
	if e != nil || !bytes.Equal(output, want) {
		t.Fatalf("%x want %x err %v", output, want, e)
	}
	if _, e := BodyTransform(context.Background(), raw, h, []Operation{{Op: "json_set", Pointer: "/price", Value: str("界")}}); e == nil {
		t.Fatal("unrepresentable JSON accepted")
	}
	h.Set("Content-Type", "text/plain; charset=windows-1252")
	if _, e := BodyTransform(context.Background(), bytes.Repeat([]byte{0x80}, MaxBodyBytes/3+1), h, []Operation{{Op: "text_replace", Pattern: "x", Replacement: "y"}}); e == nil {
		t.Fatal("charset UTF-8 stage escaped limit")
	}
}

func TestIntegrationUTF16JSONRoundTrip(t *testing.T) {
	h := http.Header{"Content-Type": {"application/json; charset=utf-16le"}}
	// Explicit UTF-16LE has no BOM; all code units are retained on conversion.
	original := []byte{'{', 0, '"', 0, 'n', 0, '"', 0, ':', 0, '1', 0, '}', 0}
	got, e := BodyTransform(context.Background(), original, h, []Operation{{Op: "json_set", Pointer: "/n", Value: raw(`2`)}})
	want := append([]byte(nil), original...)
	want[10] = '2'
	if e != nil || !bytes.Equal(got, want) {
		t.Fatalf("UTF16 JSON changed encoding: %x want %x err %v", got, want, e)
	}
}

func TestIntegrationNoBodyTrailerFailurePreservesFallbackStream(t *testing.T) {
	d := baseDoc()
	r := responseRule("no-body", Operation{Op: "status_set", Value: raw(`204`)}, Operation{Op: "trailer_set", Name: "X-Trailer", Value: str("new")})
	r.OnError = "forward_original"
	d.Rules = []Rule{r}
	source := &spyBody{Reader: strings.NewReader("original")}
	resp := response("original")
	resp.Body = source
	result, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), resp)
	if e != nil || result.Outcome != "forward_original" || source.closes != 0 {
		t.Fatal("fallback stream closed before rollback", result, e, source.closes)
	}
	if got := responseBody(t, result.Response); got != "original" {
		t.Fatal(got)
	}
}
