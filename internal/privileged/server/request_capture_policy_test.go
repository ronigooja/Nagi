package server

import (
	"encoding/json"
	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"
	"strings"
	"testing"
)

func TestCaptureHelperStrictNestedControlAndEmptyGenerationScope(t *testing.T) {
	policy := capture.Policy{SessionID: "fixture", Revision: 1, State: "recording", Scope: capture.Scope{Domains: []string{}, ProcessPaths: []string{}, AppPaths: []string{}}}
	prepare := rewriteTestPrepare(t)
	prepare.Capture = &policy
	prepare.InterceptDomains = []string{}
	prepare.TargetDomains = []string{}
	if _, e := decodeRewritePrepare(rewriteTestBody(t, prepare)); e != nil {
		t.Fatal("capture-only scope rejected", e)
	}
	update := control.RewriteCapture{Version: 2, Generation: strings.Repeat("a", 32), Capture: policy}
	b, _ := json.Marshal(update)
	if e := validateControl(privileged.Request{Method: "PUT", Path: "/request-rewrite/capture", Body: b}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(b), `"revision":1`, `"revision":1,"revision":2`, 1), strings.Replace(string(b), `"scope":{`, `"scope":{"unknown":"credential",`, 1), strings.Replace(string(b), `"process_paths":[]`, `"process_paths":["relative"]`, 1), strings.Replace(string(b), `"previous_revision":0`, `"previous_revision":3`, 1), strings.Replace(string(b), `"domains":[]`, `"domains":null`, 1)} {
		if validateControl(privileged.Request{Method: "PUT", Path: "/request-rewrite/capture", Body: []byte(bad)}) == nil {
			t.Fatal("accepted invalid nested capture")
		}
	}
	response, e := filterRewriteResponse(privileged.Request{Method: "GET", Path: "/request-rewrite/capabilities"}, privileged.Response{OK: true, Body: []byte(`{"capture_protocol":1,"credential":"must-not-leak"}`)})
	if e != nil || !strings.Contains(string(response.Body), `"capture_protocol":1`) || strings.Contains(string(response.Body), "credential") {
		t.Fatal(string(response.Body), e)
	}
}
