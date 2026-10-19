package requestrewrite

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func upgradeRequest(t *testing.T) *http.Request {
	r := request(t, "")
	r.Method = http.MethodGet
	r.Header.Del("Content-Type")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	return r
}

func TestUpgradeBufferedScriptExecutesOnEmptyBody(t *testing.T) {
	defer SetScriptExecutor(nil)
	calls := 0
	SetScriptExecutor(func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
		calls++
		var in struct {
			Message map[string]json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			t.Fatal(err)
		}
		if string(in.Message["body_base64"]) != `""` {
			t.Fatalf("missing complete empty body: %s", input)
		}
		return raw(`{"action":"replace","message":{"url":"https://api.service.example/changed","headers":{"Upgrade":["websocket"],"Connection":["Upgrade"],"X-Edited":["yes"]},"body_base64":""}}`), nil
	})
	d := baseDoc()
	d.Scripts = []Script{{ID: "buffer", BodyAccess: "buffer", Source: "fixture"}}
	d.Rules = []Rule{rule("script", Operation{Op: "script", Script: "buffer"})}
	out, err := compiled(t, d).TransformUpgrade(context.Background(), upgradeRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Request.URL.Path != "/changed" || out.Request.Header.Get("X-Edited") != "yes" || output(t, out) != "" {
		t.Fatalf("ignored script: %+v", out)
	}
}

func TestUpgradeProtocolCommitCannotFallback(t *testing.T) {
	for _, op := range []Operation{
		{Op: "method_set", Value: str("POST")},
		{Op: "body_set", Encoding: "base64", Value: str("eA==")},
		{Op: "trailer_set", Name: "X-Trailer", Value: str("yes")},
	} {
		t.Run(op.Op, func(t *testing.T) {
			d := baseDoc()
			r := rule("invalid", op)
			r.OnError = "forward_original"
			d.Rules = []Rule{r}
			if out, err := compiled(t, d).TransformUpgrade(context.Background(), upgradeRequest(t)); err == nil {
				if out.Request != nil && out.Request.Body != nil {
					out.Request.Body.Close()
				}
				t.Fatalf("invalid upgrade accepted: %+v", out)
			}
		})
	}
}

func TestUpgradeBodyOperationsExecuteAndApplyRuleErrorPolicy(t *testing.T) {
	d := baseDoc()
	r := rule("json", Operation{Op: "json_set", Pointer: "", Value: raw(`{}`)})
	r.OnError = "forward_original"
	d.Rules = []Rule{r}
	out, err := compiled(t, d).TransformUpgrade(context.Background(), upgradeRequest(t))
	if err != nil || out.Outcome != "forward_original" {
		t.Fatalf("operation silently skipped: %+v %v", out, err)
	}
	if output(t, out) != "" {
		t.Fatal("fallback body")
	}
	d.Rules = []Rule{rule("empty", Operation{Op: "body_set", Encoding: "base64", Value: str("")})}
	out, err = compiled(t, d).TransformUpgrade(context.Background(), upgradeRequest(t))
	if err != nil || output(t, out) != "" {
		t.Fatalf("empty replacement: %v", err)
	}
	d.Rules = []Rule{rule("delete", Operation{Op: "trailer_delete", Name: "X-Trailer"})}
	out, err = compiled(t, d).TransformUpgrade(context.Background(), upgradeRequest(t))
	if err != nil || output(t, out) != "" {
		t.Fatalf("empty trailer deletion: %v", err)
	}
	// The same method/body edits remain legal for ordinary HTTP requests.
	d.Rules = []Rule{rule("http", Operation{Op: "method_set", Value: str("POST")}, Operation{Op: "body_set", Encoding: "base64", Value: str("eA==")})}
	out, err = compiled(t, d).Transform(context.Background(), upgradeRequest(t))
	if err != nil || out.Request.Method != "POST" || output(t, out) != "x" {
		t.Fatalf("HTTP changed: %+v %v", out, err)
	}
}

func TestUpgradeHeaderOnlyScriptDoesNotReadBody(t *testing.T) {
	defer SetScriptExecutor(nil)
	SetScriptExecutor(func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
		if strings.Contains(string(input), "body_base64") {
			t.Fatal("none script received a body")
		}
		return raw(`{"action":"unchanged"}`), nil
	})
	d := baseDoc()
	d.Scripts = []Script{{ID: "none", BodyAccess: "none", Source: "fixture"}}
	d.Rules = []Rule{rule("script", Operation{Op: "script", Script: "none"})}
	req := upgradeRequest(t)
	spy := &spyBody{Reader: strings.NewReader("")}
	req.Body = spy
	out, err := compiled(t, d).TransformUpgrade(context.Background(), req)
	if err != nil || spy.reads != 0 {
		t.Fatalf("body read: %d %v", spy.reads, err)
	}
	out.Request.Body.Close()
}

func TestUpgradePreviewUsesProtocolValidation(t *testing.T) {
	d := baseDoc()
	d.Rules = []Rule{rule("method", Operation{Op: "method_set", Value: str("POST")})}
	input := []byte(`{"stage":"request","request":{"scheme":"https","host":"api.service.example","port":443,"method":"GET","path":"/v1/items","query":"","headers":{"Upgrade":["websocket"],"Connection":["Upgrade"]},"body_base64":""}}`)
	if _, err := compiled(t, d).PreviewSensitive(context.Background(), input); err == nil {
		t.Fatal("preview accepted invalid upgrade")
	}
}

func TestUpgradeRejectsOriginalEntityBeforeRules(t *testing.T) {
	for _, kind := range []string{"method", "length", "chunked", "trailer"} {
		t.Run(kind, func(t *testing.T) {
			req := upgradeRequest(t)
			switch kind {
			case "method":
				req.Method = "POST"
			case "length":
				req.ContentLength = 1
			case "chunked":
				req.TransferEncoding = []string{"chunked"}
			case "trailer":
				req.Trailer = http.Header{"X-Trailer": nil}
			}
			d := baseDoc()
			r := rule("restore", Operation{Op: "method_set", Value: str("GET")}, Operation{Op: "body_set", Encoding: "base64", Value: str("")})
			r.OnError = "forward_original"
			d.Rules = []Rule{r}
			if _, err := compiled(t, d).TransformUpgrade(context.Background(), req); err == nil {
				t.Fatal("invalid original upgrade accepted")
			}
		})
	}
}
