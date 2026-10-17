package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestConfiguredMeasurementExpectedStatusPolicy(t *testing.T) {
	for _, path := range []string{
		"/proxies/Node/delay",
		"/group/Auto/delay",
		"/providers/proxies/airport/Node/healthcheck",
	} {
		base := path + "?url=https%3A%2F%2Fexample.test%2F204&timeout=5000"
		for _, expected := range []string{"", "*", "204", "200/204/300-399", "200,204"} {
			req := privileged.Request{Method: "GET", Path: base + "&expected=" + url.QueryEscape(expected)}
			if err := validateControl(req); err != nil {
				t.Fatalf("configured measurement rejected: %s: %v", req.Path, err)
			}
			if snapshotControl(req) {
				t.Fatalf("measurement must retain operation serialization: %s", req.Path)
			}
		}
		for _, suffix := range []string{
			"&unknown=204", "&expected=204&unknown=1",
			"&expected=204&expected=200", "&url=https%3A%2F%2Fother.test",
			"&timeout=10", "&expected=" + strings.Repeat("2", 1025),
		} {
			if err := validateControl(privileged.Request{Method: "GET", Path: base + suffix}); err == nil {
				t.Fatalf("unexpected measurement query accepted: %s", suffix)
			}
		}
	}
}
