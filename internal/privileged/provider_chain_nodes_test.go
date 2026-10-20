package privileged

import (
	"strings"
	"testing"
)

func TestProviderIdentityRequestRejectsArbitraryInputs(t *testing.T) {
	req := Request{Version: ProtocolVersion, Operation: ProviderChainNodes, Profile: "work", Provider: "Provider with spaces", SourceHash: strings.Repeat("a", 64)}
	policy := Policy{PeerUID: 501}
	if err := req.Validate(policy); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Path = "/etc/passwd" }, func(r *Request) { r.ConfigPath = "/etc/passwd" },
		func(r *Request) { r.Body = []byte(`{"path":"/etc/passwd"}`) }, func(r *Request) { r.Profile = "../other" },
		func(r *Request) { r.SourceHash = "invalid" }, func(r *Request) { r.Provider = "" },
	} {
		bad := req
		mutate(&bad)
		if bad.Validate(policy) == nil {
			t.Fatal("arbitrary provider inputs accepted")
		}
	}
	if req.Validate(Policy{PeerUID: 0}) == nil {
		t.Fatal("untrusted peer accepted")
	}
	req.Operation = Status
	if req.Validate(policy) == nil {
		t.Fatal("provider fields smuggled through another operation")
	}
}
