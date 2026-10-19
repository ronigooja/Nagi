package requestrewrite

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func FuzzDocument(f *testing.F) {
	for _, seed := range []string{`{"version":2,"enabled":false,"rules":[],"scripts":[]}`, `{"version":2,"enabled":false,"rules":[{"id":"text","enabled":true,"stage":"request","match":{"domain":"api.service.example","path_regex":"^/v[0-9]+/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"regex_replace","pattern":"(old)","replacement":"${1}-new","all":true}]}],"scripts":[]}`, `{"version":2,"enabled":false,"rules":[],"scripts":[{"id":"local","body_access":"none","source":"function transform(input){return {action:'unchanged'}}"}]}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		snapshot, e := Parse(input)
		if e != nil {
			return
		}
		exported := snapshot.ExportSensitive()
		if len(exported) > MaxDocumentBytes {
			t.Fatal("unbounded export")
		}
		again, e := Parse(exported)
		if e != nil {
			t.Fatal("accepted document cannot be reparsed", e)
		}
		third, e := Compile(again.Document())
		if e != nil || again.Version() != third.Version() {
			t.Fatal("unstable canonical snapshot", e)
		}
		if again.Document().Enabled {
			t.Fatal("portable export enabled feature")
		}
	})
}

func FuzzBodyEncodings(f *testing.F) {
	f.Add([]byte("example text"), uint8(0), false)
	f.Add([]byte(`{"n":9007199254740993}`), uint8(5), true)
	f.Add([]byte{0xff, 0, 1}, uint8(4), false)
	f.Fuzz(func(t *testing.T, payload []byte, selector uint8, corrupt bool) {
		// Fuzz primarily codec composition/strict decoding; fixed maximum-size tests
		// exercise admission without letting each mutation consume a full workspace.
		if len(payload) > 64<<10 {
			return
		}
		choices := [][]string{{"identity"}, {"gzip"}, {"deflate"}, {"br"}, {"zstd"}, {"gzip", "br", "zstd"}, {"deflate", "gzip", "br"}}
		stack := choices[int(selector)%len(choices)]
		if out, e := bodyDecode(context.Background(), payload, stack); e == nil && len(out) > MaxBodyBytes {
			t.Fatal("arbitrary wire escaped stage limit")
		}
		encoded, e := bodyEncode(context.Background(), payload, stack)
		if e != nil {
			t.Fatal(e)
		}
		decoded, e := bodyDecode(context.Background(), encoded, stack)
		if e != nil || !bytes.Equal(decoded, payload) {
			t.Fatal("codec roundtrip changed bytes", stack, e)
		}
		header := http.Header{"Content-Encoding": {strings.Join(stack, ", ")}, "Content-Type": {"application/octet-stream"}}
		transformed, e := BodyTransform(context.Background(), encoded, header, nil)
		if e != nil {
			t.Fatal(e)
		}
		decoded, e = bodyDecode(context.Background(), transformed, stack)
		if e != nil || !bytes.Equal(decoded, payload) {
			t.Fatal("empty operation changed representation", e)
		}
		if corrupt && len(encoded) > 0 {
			encoded[len(encoded)/2] ^= 0x80
			out, e := bodyDecode(context.Background(), encoded, stack)
			if e == nil && len(out) > MaxBodyBytes {
				t.Fatal("corrupt stream escaped stage bound")
			}
		}
	})
}
