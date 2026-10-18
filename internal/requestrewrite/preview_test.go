package requestrewrite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreviewSensitiveNoNetworkAndFraming(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("edit", Operation{Op: "header_set", Name: "Cookie", Value: str("fictitious-secret")}, Operation{Op: "json_set", Pointer: "/n", Value: raw(`9007199254740993`)})}
	s := compiled(t, d)
	fixture := Fixture{Scheme: "https", Host: "api.service.example", Port: 443, Method: "POST", Path: "/v1/items", Query: "token=fictitious", Headers: map[string][]string{"Content-Type": {"application/json"}}, BodyBase64: base64.StdEncoding.EncodeToString([]byte(`{}`))}
	input, _ := json.Marshal(fixture)
	result, e := s.PreviewSensitive(context.Background(), input)
	if e != nil || !result.Sensitive || result.Outcome != "rewritten" || result.Request.Headers["Cookie"][0] != "fictitious-secret" {
		t.Fatal(result, e)
	}
	b, _ := base64.StdEncoding.DecodeString(result.Request.BodyBase64)
	if string(b) != `{"n":9007199254740993}` {
		t.Fatal(string(b))
	}
	for _, bad := range []string{strings.Replace(string(input), `"port":443`, `"port":443,"port":443`, 1), strings.Replace(string(input), `"body_base64":"e30="`, `"body_base64":null`, 1), strings.Replace(string(input), `"headers":{`, `"headers":{"content-type":["application/json"],`, 1)} {
		if _, e = s.PreviewSensitive(context.Background(), []byte(bad)); e == nil {
			t.Fatal("unsafe fixture")
		}
	}
}
