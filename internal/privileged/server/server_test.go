package server

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

type recordingHandler struct{ called bool }

func (h *recordingHandler) Handle(_ context.Context, _ int, _ privileged.Request) (privileged.Response, error) {
	h.called = true
	return privileged.Response{OK: true}, nil
}

func TestPeerCredentialRejectsAnotherSession(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "ng-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "helper.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverConn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	h := &recordingHandler{}
	s := Server{SessionUID: os.Geteuid() + 1, Handler: h}
	if err := s.ServeConn(context.Background(), serverConn); err == nil {
		t.Fatal("other session accepted")
	}
	if h.called {
		t.Fatal("handler called for unauthorized peer")
	}
}

func TestControllerActionCannotForwardHTTP(t *testing.T) {
	for _, action := range []ControllerAction{"GET /configs", "version?x=1", "POST /configs", ""} {
		if _, err := QueryController(context.Background(), "/missing/socket", action); err == nil {
			t.Fatalf("accepted %q", action)
		}
	}
}

func TestSocketRequiresRootControlledAncestors(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "ng-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "controller.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControllerSocket(path); err == nil {
		t.Fatal("socket under writable /tmp accepted")
	}
	if err := os.Symlink(path, path+".link"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControllerSocket(path + ".link"); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestHelperFailureResponsePreservesOnlyExplicitPreForwardReloadEvidence(t *testing.T) {
	for _, tc := range []struct {
		req          privileged.Request
		marked, want bool
	}{
		{privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true"}, true, true},
		{privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true"}, false, false},
		{privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/configs"}, true, false},
		{privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/proxies/example"}, true, false},
		{privileged.Request{Operation: privileged.Start}, true, false},
	} {
		response := helperFailureResponse(tc.req, privileged.Response{NotForwarded: tc.marked}, errors.New("fixture failure"))
		if response.NotForwarded != tc.want || response.OK || response.Error != "fixture failure" {
			t.Fatalf("%+v: %+v", tc, response)
		}
		if err := response.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReloadParsingFailureIsDefinitelyNotForwarded(t *testing.T) {
	response, err := (&RuntimeHandler{}).reload(context.Background(), privileged.Request{Body: []byte(`{}`)})
	if err == nil || !response.NotForwarded {
		t.Fatalf("%+v %v", response, err)
	}
}
