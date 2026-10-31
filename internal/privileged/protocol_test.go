package privileged

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Request, Policy) {
	t.Helper()
	root := t.TempDir()
	return Request{Version: ProtocolVersion, Operation: Start, Profile: "work_1", ConfigPath: filepath.Join(root, "config.yaml")}, Policy{PeerUID: 501, AllowedPaths: []string{root}}
}

func TestRequestValidation(t *testing.T) {
	req, policy := fixture(t)
	if err := req.Validate(policy); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Request){
		"version":   func(r *Request) { r.Version++ },
		"operation": func(r *Request) { r.Operation = "exec" },
		"profile":   func(r *Request) { r.Profile = "../evil" },
		"relative":  func(r *Request) { r.ConfigPath = "config.yaml" },
		"outside":   func(r *Request) { r.ConfigPath = "/etc/passwd" },
		"unclean":   func(r *Request) { r.ConfigPath += "/../x" },
		"missing":   func(r *Request) { r.ConfigPath = "" },
		"binary":    func(r *Request) { r.BinaryPath = "/usr/local/bin/mihomo" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := req
			mutate(&copy)
			if err := copy.Validate(policy); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	policy.PeerUID = 0
	if err := req.Validate(policy); err == nil {
		t.Fatal("untrusted peer accepted")
	}
	policy.PeerUID = 501
	stop := Request{Version: ProtocolVersion, Operation: Stop}
	if err := stop.Validate(policy); err != nil {
		t.Fatal(err)
	}
	stop.ConfigPath = req.ConfigPath
	if err := stop.Validate(policy); err == nil {
		t.Fatal("stop start fields accepted")
	}
}

func TestRejectSymlinkPathComponent(t *testing.T) {
	req, policy := fixture(t)
	root := policy.AllowedPaths[0]
	if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	req.ConfigPath = filepath.Join(root, "link", "config.yaml")
	if err := req.Validate(policy); err == nil {
		t.Fatal("symlink component accepted")
	}
}

func TestProtocolRoundTripAndBounds(t *testing.T) {
	req, _ := fixture(t)
	var b bytes.Buffer
	if err := WriteRequest(&b, req); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(&b)
	if err != nil || !reflect.DeepEqual(got, req) {
		t.Fatalf("roundtrip: %#v %v", got, err)
	}
	b.Reset()
	resp := Response{Version: ProtocolVersion, OK: true, PID: 42, Running: true}
	if err := WriteResponse(&b, resp); err != nil {
		t.Fatal(err)
	}
	gotResp, err := ReadResponse(&b)
	if err != nil || !reflect.DeepEqual(gotResp, resp) {
		t.Fatalf("roundtrip: %#v %v", gotResp, err)
	}
	if err := resp.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"{\"version\":1,\"operation\":\"start\",\"unknown\":true}\n", "{} {}\n", strings.Repeat("x", (1<<20)+1) + "\n"} {
		if _, err := ReadRequest(strings.NewReader(raw)); err == nil {
			t.Fatal("malformed or oversized message accepted")
		}
	}
}

func TestDNSOperationsAreParameterFree(t *testing.T) {
	_, policy := fixture(t)
	for _, op := range []Operation{TUNDNSSync, TUNDNSRestore, TUNDNSStatus} {
		valid := Request{Version: ProtocolVersion, Operation: op}
		if err := valid.Validate(policy); err != nil {
			t.Fatal(err)
		}
		for _, mutate := range []func(*Request){func(r *Request) { r.Profile = "anything" }, func(r *Request) { r.ConfigPath = "/etc/passwd" }, func(r *Request) { r.Body = []byte(`{"dns":["1.1.1.1"]}`) }, func(r *Request) { r.Path = "/etc/passwd" }, func(r *Request) { r.Method = "exec" }, func(r *Request) { r.BinaryPath = "/bin/sh" }, func(r *Request) { r.WorkDir = "/tmp" }, func(r *Request) { r.PIDPath = "/tmp/pid" }, func(r *Request) { r.SocketPath = "/tmp/socket" }, func(r *Request) { r.LogPath = "/etc/passwd" }, func(r *Request) { r.Lines = 1 }, func(r *Request) { r.Offset = 1 }} {
			req := valid
			mutate(&req)
			if err := req.Validate(policy); err == nil {
				t.Fatalf("accepted DNS payload %#v", req)
			}
		}
	}
}

func TestConnectionEventsRequestContract(t *testing.T) {
	_, policy := fixture(t)
	valid := Request{Version: ProtocolVersion, Operation: ConnectionEvents, EventKind: "failure", WaitMS: 25000}
	if err := valid.Validate(policy); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.EventKind = "all" },
		func(r *Request) { r.WaitMS = 30001 },
		func(r *Request) { r.Path = "/connection-events" },
		func(r *Request) { r.Body = []byte(`{}`) },
	} {
		copy := valid
		mutate(&copy)
		if err := copy.Validate(policy); err == nil {
			t.Fatalf("invalid event request accepted: %+v", copy)
		}
	}
}

func TestPreForwardFailureResponseContract(t *testing.T) {
	response := Response{Version: ProtocolVersion, Error: "restricted validation rejected", NotForwarded: true}
	if err := response.Validate(); err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := WriteResponse(&wire, response); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadResponse(&wire)
	if err != nil || !reflect.DeepEqual(response, decoded) {
		t.Fatalf("%+v %v", decoded, err)
	}
	for _, bad := range []Response{
		{Version: ProtocolVersion, OK: true, NotForwarded: true},
		{Version: ProtocolVersion, Error: "rejected", Status: 400, NotForwarded: true},
		{Version: ProtocolVersion, NotForwarded: true},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("contradictory response accepted: %+v", bad)
		}
	}
	legacy, err := ReadResponse(strings.NewReader("{\"version\":1,\"ok\":false,\"error\":\"unclassified\"}\n"))
	if err != nil || legacy.NotForwarded {
		t.Fatalf("legacy response claims pre-forward rejection: %+v %v", legacy, err)
	}
}
