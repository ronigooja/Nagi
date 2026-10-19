package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func TestRewriteMaximumDocumentCLIStoreRoundTrip(t *testing.T) {
	isolatedTestPaths(t, t.TempDir())
	d := rewrite.Document{Version: 2, Scripts: []rewrite.Script{}, Rules: []rewrite.Rule{{ID: "maximum", Enabled: true, Stage: rewrite.StageRequest, RangeMode: "fragment", Match: rewrite.Match{Domain: "api.service.example"}, OnError: "block", RedirectConsent: []rewrite.Consent{}, Operations: []rewrite.Operation{{Op: "body_set", Encoding: "text", Value: json.RawMessage(`""`)}}}}}
	empty, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	value, _ := json.Marshal(strings.Repeat("x", rewrite.MaxDocumentBytes-len(empty)))
	d.Rules[0].Operations[0].Value = value
	raw, e := json.Marshal(d)
	if e != nil || len(raw) != rewrite.MaxDocumentBytes {
		t.Fatal("fixture limit", len(raw), e)
	}
	path := filepath.Join(t.TempDir(), "maximum.json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	rewriteOK(t, "", "import", path)
	code, exported, stderr := rewriteCLIRun(t, "", "request-rewrite", "export", "-")
	if code != 0 || stderr != "" || len(exported) != rewrite.MaxDocumentBytes {
		t.Fatal(code, len(exported), stderr)
	}
	snapshot, e := rewrite.Parse([]byte(exported))
	if e != nil || snapshot.Document().Enabled || len(snapshot.Document().Rules) != 1 {
		t.Fatal("stored maximum did not roundtrip", e)
	}
	rewriteOK(t, exported, "validate", "-")
	// Canonical serialization remains within the same document ceiling.
	if len(snapshot.ExportSensitive()) != rewrite.MaxDocumentBytes {
		t.Fatal("canonical document grew past limit")
	}
	code, _, stderr = rewriteCLIRun(t, string(raw)+" ", "--json", "request-rewrite", "import", "-")
	if code != 1 || !strings.Contains(stderr, "rewrite_limit") {
		t.Fatal("input limit not enforced", code, stderr)
	}
}
