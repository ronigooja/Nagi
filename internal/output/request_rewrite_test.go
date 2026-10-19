package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestRewriteHumanAndJSONBoundaries(t *testing.T) {
	v := RewriteResult{"profile": "work", "enabled_intent": true, "actual": "blocked", "tun_running": nil, "ca": map[string]any{"state": "generated", "trust": "unknown", "not_after": "2031-10-18T12:34:56.123Z"}, "last_error": "rewrite_tun_required", "recovery": "nagi tun on"}
	var text, structured bytes.Buffer
	if e := Write(&text, false, v); e != nil {
		t.Fatal(e)
	}
	if e := Write(&structured, true, v); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"Request rewrite: blocked", "Enabled intent: true", "TUN running: unknown", "OS trust: unknown", "rewrite_tun_required", "nagi tun on"} {
		if !strings.Contains(text.String(), want) {
			t.Fatal(want, text.String())
		}
	}
	if strings.Contains(text.String(), ":56") || strings.Contains(text.String(), ".123") {
		t.Fatal("human timestamp exceeds minute precision", text.String())
	}
	if !strings.Contains(structured.String(), `"ok":true`) || !strings.Contains(structured.String(), `2031-10-18T12:34:56.123Z`) {
		t.Fatal(structured.String())
	}
	var raw bytes.Buffer
	if e := Write(&raw, false, Raw("literal\n\n")); e != nil || raw.String() != "literal\n\n" {
		t.Fatal(e, raw.String())
	}
}

func TestRewriteScriptSourceAndStagedOutput(t *testing.T) {
	source := "function rewrite(input) { return {}; }\n\n"
	var out bytes.Buffer
	if e := Write(&out, false, RewriteResult{"sensitive": true, "script": map[string]any{"id": "local", "source": source}}); e != nil || out.String() != source {
		t.Fatal(e, out.String())
	}
	out.Reset()
	if e := Write(&out, false, RewriteResult{"scripts": []any{}}); e != nil || !strings.Contains(out.String(), "No local rewrite scripts.") {
		t.Fatal(e, out.String())
	}
	out.Reset()
	if e := Write(&out, false, RewriteResult{"sensitive": true, "stage": "response", "response": map[string]any{"status": 201}, "stages": []any{map[string]any{"stage": "response"}}}); e != nil || !strings.Contains(out.String(), `"status": 201`) || !strings.HasPrefix(out.String(), "Sensitive output:") {
		t.Fatal(e, out.String())
	}
}
