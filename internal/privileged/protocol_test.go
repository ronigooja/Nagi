package privileged

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Request, Policy) {
	t.Helper()
	root := t.TempDir()
	return Request{Version: ProtocolVersion, Operation: Start, Profile: "work_1", BinaryPath: filepath.Join(root, "mihomo"), ConfigPath: filepath.Join(root, "config.yaml"), WorkDir: root, SocketPath: filepath.Join(root, "mihomo.sock"), LogPath: filepath.Join(root, "mihomo.log"), PIDPath: filepath.Join(root, "mihomo.pid")}, Policy{PeerUID: 501, AllowedPaths: []string{root}}
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
		"outside":   func(r *Request) { r.LogPath = "/etc/passwd" },
		"unclean":   func(r *Request) { r.PIDPath += "/../x" },
		"missing":   func(r *Request) { r.SocketPath = "" },
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
	stop := Request{Version: ProtocolVersion, Operation: Stop, Profile: "work_1"}
	if err := stop.Validate(policy); err != nil {
		t.Fatal(err)
	}
	stop.BinaryPath = req.BinaryPath
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
	if err != nil || got != req {
		t.Fatalf("roundtrip: %#v %v", got, err)
	}
	b.Reset()
	resp := Response{Version: ProtocolVersion, OK: true, PID: 42, Running: true}
	if err := WriteResponse(&b, resp); err != nil {
		t.Fatal(err)
	}
	gotResp, err := ReadResponse(&b)
	if err != nil || gotResp != resp {
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
