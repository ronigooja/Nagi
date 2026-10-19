package requestrewrite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func rangeResponse(body string, multipart bool) *http.Response {
	r := response(body)
	r.StatusCode = 206
	r.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if multipart {
		r.Header.Set("Content-Type", "multipart/byteranges; boundary=test")
	} else {
		r.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/100", len(body)-1))
	}
	return r
}
func rangeMultipart(a, b string) string {
	return fmt.Sprintf("--test\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Range: bytes 0-%d/100\r\n\r\n%s\r\n--test\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Range: bytes 50-%d/100\r\n\r\n%s\r\n--test--\r\n", len(a)-1, a, 49+len(b), b)
}
func TestRangeStageFinalLengthsAndOrderedScript(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		t.Run(fmt.Sprint(multipart), func(t *testing.T) {
			d := baseDoc()
			first := responseRule("grow", Operation{Op: "text_replace", Pattern: "old", Replacement: "longer", All: true}, Operation{Op: "header_set", Name: "X-Step", Value: str("ready")})
			second := responseRule("script", Operation{Op: "script", Script: "inspect"}, Operation{Op: "header_set", Name: "X-Step", Value: str("done")})
			second.Match.ResponseHeaders = map[string][]string{"X-Original": {"yes"}}
			d.Scripts = []Script{{ID: "inspect", BodyAccess: "buffer", Source: "inspect"}}
			d.Rules = []Rule{first, second}
			calls := 0
			SetScriptExecutor(func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
				calls++
				var in struct {
					Original struct {
						Response struct {
							Headers http.Header `json:"headers"`
						} `json:"response"`
					} `json:"original"`
					Message struct {
						Headers http.Header `json:"headers"`
						Body    string      `json:"body_base64"`
					} `json:"message"`
				}
				if e := json.Unmarshal(input, &in); e != nil {
					t.Fatal(e)
				}
				decoded, e := base64.StdEncoding.DecodeString(in.Message.Body)
				if e != nil || string(decoded) != "longer" || in.Message.Headers.Get("X-Step") != "ready" || in.Original.Response.Headers.Get("X-Step") != "" {
					t.Fatalf("out of order script: %s", input)
				}
				in.Message.Headers.Set("X-Script", "yes")
				return json.Marshal(map[string]any{"action": "replace", "message": map[string]any{"body_base64": "bmV3", "headers": in.Message.Headers}})
			})
			defer SetScriptExecutor(nil)
			body := "old"
			want := "new"
			expectedCalls := 1
			if multipart {
				body = rangeMultipart("old", "old")
				want = rangeMultipart("new", "new")
				expectedCalls = 2
			}
			original := rangeResponse(body, multipart)
			original.Header.Set("X-Original", "yes")
			out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), original)
			if e != nil {
				t.Fatal(e)
			}
			if got := responseBody(t, out.Response); got != want {
				t.Fatalf("body %q", got)
			}
			if calls != expectedCalls || out.Response.Header.Get("X-Step") != "done" || out.Response.Header.Get("X-Script") != "yes" || out.Response.Header.Get("Content-Type") != original.Header.Get("Content-Type") || out.Response.Header.Get("Content-Range") != original.Header.Get("Content-Range") {
				t.Fatal("metadata or script count", calls, out.Response.Header)
			}
			// Closing the returned body releases the stage admission reservation.
			next := rangeResponse(body, multipart)
			next.Header.Set("X-Original", "yes")
			again, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), next)
			if e != nil {
				t.Fatal(e)
			}
			responseBody(t, again.Response)
		})
	}
}

func TestRangeFinalMismatchPolicies(t *testing.T) {
	for _, policy := range []string{"block", "forward_original"} {
		t.Run(policy, func(t *testing.T) {
			d := baseDoc()
			edit := responseRule("body", Operation{Op: "text_replace", Pattern: "old", Replacement: "longer", All: true})
			edit.OnError = policy
			// A trailing header-only rule must not take ownership of final body failure.
			header := responseRule("header", Operation{Op: "header_set", Name: "X-Changed", Value: str("yes")})
			d.Rules = []Rule{edit, header}
			wire := rangeMultipart("old", "old")
			out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), rangeResponse(wire, true))
			if policy == "block" {
				if e == nil || out.Response != nil {
					t.Fatal("partial commit", out, e)
				}
				return
			}
			if e != nil || out.Outcome != "forward_original" || out.Response.Header.Get("X-Changed") != "" {
				t.Fatal(out, e)
			}
			if responseBody(t, out.Response) != wire {
				t.Fatal("original was not recovered")
			}
		})
	}
}

func TestRangeMultipartScriptMetadataMustAgree(t *testing.T) {
	d := baseDoc()
	d.Scripts = []Script{{ID: "vary", BodyAccess: "buffer", Source: "vary"}}
	r := responseRule("script", Operation{Op: "script", Script: "vary"})
	r.OnError = "forward_original"
	d.Rules = []Rule{r}
	SetScriptExecutor(func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
		var v struct {
			Message struct {
				Headers http.Header `json:"headers"`
				Body    string      `json:"body_base64"`
			} `json:"message"`
		}
		json.Unmarshal(input, &v)
		v.Message.Headers.Set("X-Part", v.Message.Body)
		return json.Marshal(map[string]any{"action": "replace", "message": map[string]any{"headers": v.Message.Headers}})
	})
	defer SetScriptExecutor(nil)
	wire := rangeMultipart("old", "new")
	out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), rangeResponse(wire, true))
	if e != nil || out.Outcome != "forward_original" || out.Response.Header.Get("X-Part") != "" || responseBody(t, out.Response) != wire {
		t.Fatal(out, e)
	}
}

func TestRangeIntermediateBodyLimit(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{responseRule("grow", Operation{Op: "text_replace", Pattern: "a", Replacement: strings.Repeat("x", 1024), All: true}, Operation{Op: "body_set", Encoding: "text", Value: str("new")})}
	original := rangeResponse(strings.Repeat("a", 8193), false)
	// Keep Content-Range valid for the larger fragment.
	original.Header.Set("Content-Range", "bytes 0-8192/10000")
	if out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), original); e == nil || out.Response != nil {
		t.Fatal("oversized intermediate accepted", e)
	}
}

func TestRangeStageBuiltinOperationsRestoreLength(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		t.Run(fmt.Sprint(multipart), func(t *testing.T) {
			d := baseDoc()
			d.Rules = []Rule{
				responseRule("grow", Operation{Op: "text_replace", Pattern: "old", Replacement: "longer", All: true}),
				responseRule("shrink", Operation{Op: "text_replace", Pattern: "longer", Replacement: "new", All: true}),
			}
			wire, want := "old", "new"
			if multipart {
				wire = rangeMultipart("old", "old")
				want = rangeMultipart("new", "new")
			}
			out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), rangeResponse(wire, multipart))
			if e != nil {
				t.Fatal(e)
			}
			if responseBody(t, out.Response) != want {
				t.Fatal("incorrect final fragments")
			}
		})
	}
}

func TestRangeMultipartEachFinalLengthRequired(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{responseRule("edit", Operation{Op: "text_replace", Pattern: "old", Replacement: "long", All: true}, Operation{Op: "text_replace", Pattern: "new", Replacement: "no", All: true})}
	// Total bytes remain equal, but individual byte ranges no longer align.
	if out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), rangeResponse(rangeMultipart("old", "new"), true)); e == nil || out.Response != nil {
		t.Fatal("per-fragment length constraint bypassed", e)
	}
}

func TestRangeMultipartCannotChangeEnvelopeMetadata(t *testing.T) {
	for _, op := range []Operation{{Op: "header_set", Name: "Content-Type", Value: str("multipart/byteranges; boundary=other")}, {Op: "header_set", Name: "Content-Range", Value: str("bytes 0-2/100")}} {
		d := baseDoc()
		d.Rules = []Rule{responseRule("edit", Operation{Op: "text_replace", Pattern: "old", Replacement: "new", All: true}, op)}
		if out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), rangeResponse(rangeMultipart("old", "old"), true)); e == nil || out.Response != nil {
			t.Fatal("envelope metadata changed", e)
		}
	}
}

func TestBodyEditsInvalidateRepresentationTrailersInOrder(t *testing.T) {
	for _, stage := range []string{StageRequest, StageResponse} {
		t.Run(stage, func(t *testing.T) {
			d := baseDoc()
			edit := rule("edit", Operation{Op: "trailer_set", Name: "Digest", Value: str("stale-explicit")}, Operation{Op: "body_set", Encoding: "text", Value: str("new")}, Operation{Op: "trailer_set", Name: "Content-Digest", Value: str("fresh-explicit")})
			edit.Stage = stage
			d.Rules = []Rule{edit}
			original := http.Header{"Digest": {"stale"}, "Content-Digest": {"stale"}, "Repr-Digest": {"stale"}, "Content-Md5": {"stale"}, "Etag": {"stale"}, "X-Trace": {"keep"}}
			var got http.Header
			if stage == StageRequest {
				req := request(t, "old")
				req.Header.Set("Content-Type", "text/plain")
				req.Trailer = original
				out, e := compiled(t, d).Transform(context.Background(), req)
				if e != nil {
					t.Fatal(e)
				}
				out.Request.Body.Close()
				got = out.Request.Trailer
			} else {
				resp := response("old")
				resp.Header.Set("Content-Type", "text/plain")
				resp.Trailer = original
				out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), resp)
				if e != nil {
					t.Fatal(e)
				}
				out.Response.Body.Close()
				got = out.Response.Trailer
			}
			if got.Get("Content-Digest") != "fresh-explicit" || got.Get("X-Trace") != "keep" {
				t.Fatal(got)
			}
			for _, name := range []string{"Digest", "Repr-Digest", "Content-Md5", "Etag"} {
				if got.Get(name) != "" {
					t.Fatal("stale trailer", name, got)
				}
			}
			if original.Get("Digest") != "stale" {
				t.Fatal("original trailers mutated")
			}
		})
	}
}

func TestUnmatchedInvalidWebSocketTextRejected(t *testing.T) {
	d := baseDoc()
	r := rule("unmatched", Operation{Op: "body_set", Encoding: "text", Value: str("valid")})
	r.Stage = StageWebSocketClient
	r.Match.PathPrefix = "/unmatched"
	r.OnError = "forward_original"
	d.Rules = []Rule{r}
	if _, e := compiled(t, d).TransformMessage(context.Background(), StageWebSocketClient, request(t, ""), nil, Message{Type: "text", Body: []byte{0xff}}); e == nil {
		t.Fatal("invalid incoming UTF-8 passed unmatched stage")
	}
}

func TestRangeEncodedSingleFragmentKeepsEncoding(t *testing.T) {
	ctx := context.Background()
	wire, e := bodyEncode(ctx, []byte("old"), []string{"gzip"})
	if e != nil {
		t.Fatal(e)
	}
	d := baseDoc()
	d.Rules = []Rule{responseRule("edit", Operation{Op: "text_replace", Pattern: "old", Replacement: "longer", All: true}, Operation{Op: "text_replace", Pattern: "longer", Replacement: "new", All: true})}
	original := rangeResponse(string(wire), false)
	original.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/1000", len(wire)-1))
	original.Header.Set("Content-Encoding", "gzip")
	out, e := compiled(t, d).TransformResponse(ctx, request(t, ""), original)
	if e != nil {
		t.Fatal(e)
	}
	result := responseBody(t, out.Response)
	decoded, e := bodyDecode(ctx, []byte(result), []string{"gzip"})
	if e != nil || string(decoded) != "new" || len(result) != len(wire) || out.Response.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("encoding/range mismatch", e)
	}
}

func TestRangeJSONRequiresCompleteFragments(t *testing.T) {
	for _, fragment := range []string{`{"x":"old"}`, `{"x":`} {
		d := baseDoc()
		d.Rules = []Rule{responseRule("json", Operation{Op: "json_set", Pointer: "/x", Value: raw(`"longer"`)}, Operation{Op: "json_set", Pointer: "/x", Value: raw(`"new"`)})}
		original := rangeResponse(fragment, false)
		original.Header.Set("Content-Type", "application/json")
		out, e := compiled(t, d).TransformResponse(context.Background(), request(t, ""), original)
		if fragment == `{"x":` {
			if e == nil || out.Response != nil {
				t.Fatal("incomplete JSON accepted")
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		if responseBody(t, out.Response) != `{"x":"new"}` {
			t.Fatal("JSON result")
		}
	}
}

func TestScriptChangedHeaderValueLimit(t *testing.T) {
	for _, trailers := range []bool{false, true} {
		t.Run(fmt.Sprint(trailers), func(t *testing.T) {
			for _, size := range []int{8192, 8193} {
				value := strings.Repeat("x", size)
				encoded, _ := json.Marshal(http.Header{"X-Example": {value}})
				if _, e := scriptHeaders(encoded, http.Header{}, trailers); (e == nil) != (size == 8192) {
					t.Fatalf("size %d changed: %v", size, e)
				}
				// A script may round-trip a legal inbound aggregate with a long value.
				if _, e := scriptHeaders(encoded, http.Header{"X-Example": {value}}, trailers); e != nil {
					t.Fatalf("size %d unchanged: %v", size, e)
				}
			}
			old := http.Header{"X-Example": {strings.Repeat("x", 8193)}}
			// Appending even a small value changes the field and must validate every value.
			changed, _ := json.Marshal(http.Header{"X-Example": {old.Get("X-Example"), "added"}})
			if _, e := scriptHeaders(changed, old, trailers); e == nil {
				t.Fatal("changed field retained oversized value")
			}
		})
	}
}
