package requestrewrite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }
func str(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
func baseDoc() Document {
	return Document{Version: 2, Scripts: []Script{}, Rules: []Rule{}}
}
func rule(id string, ops ...Operation) Rule {
	return Rule{ID: id, Enabled: true, Stage: StageRequest, Match: Match{Domain: "api.service.example", Methods: []string{"POST", "GET"}, PathPrefix: "/v1/"}, OnError: "block", RedirectConsent: []Consent{}, Operations: ops}
}
func compiled(t *testing.T, d Document) *Snapshot {
	t.Helper()
	s, e := Compile(d)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestStrictSchemaAndCanonicalDigest(t *testing.T) {
	d := baseDoc()

	d.Rules = append(d.Rules, rule("json", Operation{Op: "json_set", Pointer: "", Value: raw(`{"z":9007199254740993,"a":"<&\u2028\n"}`)}))
	d.Rules[0].Match.Domain = "API.Service.Example."
	s := compiled(t, d)
	if s.doc.Rules[0].Match.Domain != "api.service.example" {
		t.Fatal("domain not normalized")
	}
	if !strings.Contains(string(s.canonical), `9007199254740993`) || !strings.Contains(string(s.canonical), `"pointer":""`) || strings.Contains(string(s.canonical), `\u003c`) {
		t.Fatal(string(s.canonical))
	}
	sum := sha256.Sum256(s.canonical)
	if s.Version() != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("digest")
	}
	copy := s.Document()
	copy.Rules[0].ID = "changed"
	copy.Rules[0].Operations[0].Value[0] = 'X'
	if s.Document().Rules[0].ID != "json" {
		t.Fatal("mutable snapshot")
	}
	again, e := Parse(s.canonical)
	if e != nil || again.Version() != s.Version() {
		t.Fatal("canonical not stable", e)
	}
	for _, input := range []string{`{"version":2,"enabled":false,"scripts":[],"rules":[],"extra":1}`, `{"version":2,"enabled":false,"scripts":[],"rules":[],"version":2}`, `{"version":2,"enabled":null,"scripts":[],"rules":[]}`, `{"version":2,"enabled":false,"scripts":null,"rules":[]}`, `{"version":3,"enabled":false,"scripts":[],"rules":[]}`, string(s.canonical) + ` {}`, strings.Replace(string(s.canonical), `"pointer":""`, `"pointer":"","valuex":false`, 1), strings.Replace(string(s.canonical), `"enabled":true`, `"enabled":null`, 1)} {
		if _, e := Parse([]byte(input)); e == nil {
			t.Fatalf("accepted bad schema %s", input)
		}
	}
}
func TestDomainPolicies(t *testing.T) {
	for _, host := range []string{"https://service.example", "127.0.0.1", "[::1]", "x:80", "a..example", "x@example", "a\n.example", "café.example", "xn--.example", "-x.example", "x-.example", "x.example.."} {
		if _, e := NormalizeDomain(host); e == nil {
			t.Errorf("accepted %q", host)
		}
	}
	if h, e := NormalizeDomain("XN--BCHER-KVA.Example."); e != nil || h != "xn--bcher-kva.example" {
		t.Fatal(h, e)
	}
	for _, p := range []string{"*", "*.com", "*.co.uk", "*.github.io"} {
		if _, e := normalizePattern(p); e == nil {
			t.Errorf("accepted suffix %s", p)
		}
	}
	d := baseDoc()
	d.Rules = []Rule{rule("wild", Operation{Op: "header_delete", Name: "X-Test"})}
	d.Rules[0].Match.Domain = "*.service.example"
	s := compiled(t, d)
	for h, want := range map[string]bool{"service.example": false, "a.service.example": true, "a.b.service.example": true, "badservice.example": false, "a.service.example.evil": false} {
		if s.Authorized(h) != want {
			t.Error(h)
		}
	}
	d.Rules = []Rule{rule("okay", Operation{Op: "header_delete", Name: "X-Test"})}
	compiled(t, d)
	d.Rules[0].Match.Domain = "*"
	if _, e := Compile(d); e == nil {
		t.Fatal("widened match")
	}
}
func TestOperationValidation(t *testing.T) {
	for _, name := range []string{"Host", "Content-Length", "Transfer-Encoding", "Content-Encoding", "Connection", "Upgrade", "Trailer", "Proxy-Authorization", "Nagi-Rewrite-Context", "X\rDemo"} {
		d := baseDoc()
		d.Rules = []Rule{rule("bad", Operation{Op: "header_set", Name: name, Value: str("secret")})}
		if _, e := Compile(d); e == nil {
			t.Error(name)
		}
	}
	for _, v := range []string{"one\rtwo", "one\ntwo", "one\x00two", strings.Repeat("x", 8193)} {
		d := baseDoc()
		d.Rules = []Rule{rule("bad", Operation{Op: "header_set", Name: "Authorization", Value: str(v)})}
		if _, e := Compile(d); e == nil {
			t.Fatal("accepted value")
		}
	}
	for _, p := range []string{"not/pointer", "/~", "/~2"} {
		d := baseDoc()
		d.Rules = []Rule{rule("bad", Operation{Op: "json_set", Pointer: p, Value: raw(`1`)})}
		if _, e := Compile(d); e == nil {
			t.Error(p)
		}
	}
	for _, v := range []string{"/%xx", "/a?query", "/a#frag"} {
		if validateURLValue("path", v) == nil {
			t.Error(v)
		}
	}
}
