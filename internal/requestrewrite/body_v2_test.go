package requestrewrite

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func bodyTestHeader(contentType string) http.Header {
	return http.Header{"Content-Type": {contentType}}
}
func TestBodyEncodingStack(t *testing.T) {
	ctx := context.Background()
	payload := []byte(`{"n":9007199254740993,"x":"old"}`)
	for _, stack := range [][]string{{"identity"}, {"gzip"}, {"deflate"}, {"br"}, {"zstd"}, {"gzip", "br", "zstd"}} {
		wire, e := bodyEncode(ctx, payload, stack)
		if e != nil {
			t.Fatal(e)
		}
		h := bodyTestHeader("application/json")
		h.Set("Content-Encoding", strings.Join(stack, ", "))
		out, e := BodyTransform(ctx, wire, h, []Operation{{Op: "json_set", Pointer: "/x", Value: json.RawMessage(`"new"`)}})
		if e != nil {
			t.Fatalf("%v: %v", stack, e)
		}
		decoded, e := bodyDecode(ctx, out, stack)
		if e != nil || string(decoded) != `{"n":9007199254740993,"x":"new"}` {
			t.Fatalf("%v: %s %v", stack, decoded, e)
		}
	}
}
func TestBodyCharsetAndReplacement(t *testing.T) {
	h := bodyTestHeader("text/plain; charset=windows-1252")
	out, e := BodyTransform(context.Background(), []byte{0x80, ' ', 'a', 'a'}, h, []Operation{{Op: "text_replace", Pattern: "a", Replacement: "b", All: false}})
	if e != nil || !bytes.Equal(out, []byte{0x80, ' ', 'b', 'a'}) {
		t.Fatalf("%q %v", out, e)
	}
	if _, e = BodyTransform(context.Background(), []byte("a"), h, []Operation{{Op: "text_replace", Pattern: "a", Replacement: "界"}}); e == nil {
		t.Fatal("lossy encoding accepted")
	}
	for _, v := range [][]byte{{0xff}, {0xc0, 0xaf}} {
		if _, e := BodyTransform(context.Background(), v, bodyTestHeader("text/plain"), []Operation{{Op: "text_replace", Pattern: "a", Replacement: "b"}}); e == nil {
			t.Fatal("invalid UTF-8 accepted")
		}
	}
}
func TestBodyRegexExpansion(t *testing.T) {
	for _, replacement := range []string{"$1", "${named}-$2", "$$$1", "$unknown", "$01", "${", "$", "$named_"} {
		re := regexp.MustCompile(`(?P<named>a)(b)`)
		text := "ab ab"
		want := re.ReplaceAllString(text, replacement)
		got, e := BodyTransform(context.Background(), []byte(text), bodyTestHeader("text/plain"), []Operation{{Op: "regex_replace", Pattern: re.String(), Replacement: replacement, All: true}})
		if e != nil || string(got) != want {
			t.Fatalf("%q: got %q want %q err %v", replacement, got, want, e)
		}
	}
}
func TestBodyBoundsAndStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{} {}`, `{"a":NaN}`} {
		if _, e := BodyTransform(context.Background(), []byte(raw), bodyTestHeader("application/json"), []Operation{{Op: "json_set", Pointer: "/x", Value: json.RawMessage("1")}}); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	h := bodyTestHeader("text/plain")
	h.Set("Content-Encoding", "gzip,br,zstd,identity")
	if _, e := BodyTransform(context.Background(), nil, h, nil); e == nil {
		t.Fatal("four encodings")
	}
	bomb := bytes.Repeat([]byte("a"), MaxBodyBytes+1)
	wire, e := bodyEncode(context.Background(), bomb[:MaxBodyBytes], []string{"gzip"})
	if e != nil {
		t.Fatal(e)
	}
	// Every output is bounded, including replacement expansion.
	if _, e = BodyTransform(context.Background(), bomb[:MaxBodyBytes], bodyTestHeader("text/plain"), []Operation{{Op: "text_replace", Pattern: "a", Replacement: "aa", All: true}}); e == nil {
		t.Fatal("oversize output")
	}
	if _, e = BodyTransform(context.Background(), wire, bodyTestHeader("text/plain"), nil); e != nil {
		t.Fatal(e)
	}
}
func TestBodyCancellationClosesSource(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := ReadBodyBounded(ctx, r); done <- e }()
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked source not closed")
	}
}
func TestRangeFragments(t *testing.T) {
	h := bodyTestHeader("text/plain")
	h.Set("Content-Range", "bytes 0-2/9")
	ops := []Operation{{Op: "text_replace", Pattern: "a", Replacement: "b", All: true}}
	got, e := TransformFragments(context.Background(), []byte("aaa"), h, ops)
	if e != nil || string(got) != "bbb" {
		t.Fatalf("%q %v", got, e)
	}
	ops[0].Replacement = "bb"
	if _, e = TransformFragments(context.Background(), []byte("aaa"), h, ops); e == nil {
		t.Fatal("fragment size changed")
	}
	wire := "--test\r\nContent-Type: text/plain\r\nContent-Range: bytes 0-2/9\r\n\r\naaa\r\n--test\r\nContent-Type: text/plain\r\nContent-Range: bytes 6-8/9\r\n\r\naaa\r\n--test--\r\n"
	h = bodyTestHeader("multipart/byteranges; boundary=test")
	ops[0].Replacement = "b"
	got, e = TransformFragments(context.Background(), []byte(wire), h, ops)
	if e != nil || string(got) != strings.ReplaceAll(wire, "aaa", "bbb") {
		t.Fatalf("%q %v", got, e)
	}
}
func TestTrailersAtEOF(t *testing.T) {
	original := http.Header{}
	dest := http.Header{"X-Result": nil}
	source := &bodyTrailerFixture{original: original}
	wrapped := TrailerBody(source, original, dest, []Operation{{Op: "trailer_set", Name: "X-Added", Value: json.RawMessage(`"yes"`)}})
	if len(dest.Values("X-Added")) != 0 {
		t.Fatal("early trailer")
	}
	if _, e := io.ReadAll(wrapped); e != nil {
		t.Fatal(e)
	}
	if dest.Get("X-Result") != "late" || dest.Get("X-Added") != "yes" {
		t.Fatal(dest)
	}
	if _, e := TransformTrailers(nil, []Operation{{Op: "trailer_delete", Name: "Content-Length"}}); e == nil {
		t.Fatal("framing trailer")
	}
}

type bodyTrailerFixture struct{ original http.Header }

func (b *bodyTrailerFixture) Read(p []byte) (int, error) {
	b.original.Set("X-Result", "late")
	return 0, io.EOF
}
func (b *bodyTrailerFixture) Close() error { return nil }
