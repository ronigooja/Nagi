package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestLANCLIJSONContract(t *testing.T) {
	// Use only the isolated user socket. The privileged CLI resolves a fixed
	// system helper socket and must not be exercised against a real helper here.
	root, err := os.MkdirTemp("/tmp", "nlan-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	paths := isolatedTestPaths(t, root)
	if err := os.MkdirAll(paths.RuntimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	enabled, address := false, "127.0.0.1"
	var patches []map[string]any
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/configs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"allow-lan": enabled, "bind-address": address, "tun": map[string]any{"enable": true}})
		case http.MethodPatch:
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			patches = append(patches, patch)
			enabled, _ = patch["allow-lan"].(bool)
			address, _ = patch["bind-address"].(string)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	for _, tc := range []struct {
		args    []string
		enabled bool
		address string
	}{
		{[]string{"status"}, false, "127.0.0.1"},
		{[]string{"enable"}, true, "*"},
		{[]string{"enable", "127.0.0.1"}, true, "127.0.0.1"},
		{[]string{"enable", "192.0.2.10"}, true, "192.0.2.10"},
		{[]string{"enable", "2001:db8::10"}, true, "2001:db8::10"},
		{[]string{"disable"}, false, "127.0.0.1"},
	} {
		var out, errs bytes.Buffer
		args := append([]string{"--json", "lan"}, tc.args...)
		if code := Run(args, &out, &errs, "test", "test"); code != 0 || errs.Len() != 0 {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, out.String(), errs.String())
		}
		var response struct {
			OK   bool           `json:"ok"`
			Data map[string]any `json:"data"`
		}
		want := map[string]any{"enabled": tc.enabled, "bind_address": tc.address, "mixed_port": nil}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || !response.OK || !reflect.DeepEqual(response.Data, want) {
			t.Fatalf("%v: unexpected JSON %s: %v", args, out.String(), err)
		}
		if tc.args[0] != "status" {
			preference, err := savedLANPreference(paths)
			if err != nil || preference == nil || preference.Enabled != tc.enabled || preference.Address != tc.address {
				t.Fatalf("saved LAN preference: %+v %v", preference, err)
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := []map[string]any{
		{"allow-lan": true, "bind-address": "*"},
		{"allow-lan": true, "bind-address": "127.0.0.1"},
		{"allow-lan": true, "bind-address": "192.0.2.10"},
		{"allow-lan": true, "bind-address": "2001:db8::10"},
		{"allow-lan": false, "bind-address": "127.0.0.1"},
	}
	if !reflect.DeepEqual(patches, want) {
		t.Fatalf("LAN must change only allow-lan and bind-address, retaining TUN: %+v", patches)
	}
}

func TestLANHelperControlContract(t *testing.T) {
	// Exercise the same GET/PATCH transport with an explicit fixture socket,
	// never the installed helper's system endpoint.
	root, err := os.MkdirTemp("/tmp", "nlh-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "helper.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requests := make(chan privileged.Request, 3)
	go func() {
		for i := 0; i < 3; i++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			req, err := privileged.ReadRequest(conn)
			if err != nil {
				conn.Close()
				return
			}
			requests <- req
			body := json.RawMessage(`{"allow-lan":false,"bind-address":"127.0.0.1","tun":{"enable":true}}`)
			if i == 2 {
				body = json.RawMessage(`{"allow-lan":true,"bind-address":"*","tun":{"enable":true}}`)
			}
			_ = privileged.WriteResponse(conn, privileged.Response{Version: privileged.ProtocolVersion, OK: true, Status: http.StatusOK, Body: body})
			conn.Close()
		}
	}()
	client := (helperBackend{socketPath: socket}).controlClient()
	before, err := trafficConfig(context.Background(), client)
	if err != nil || before["allow-lan"] != false {
		t.Fatalf("before: %v %v", before, err)
	}
	patch := map[string]any{"allow-lan": true, "bind-address": "*"}
	if err := client.Patch(context.Background(), "/configs", patch, nil); err != nil {
		t.Fatal(err)
	}
	after, err := trafficConfig(context.Background(), client)
	if err != nil || after["allow-lan"] != true || after["bind-address"] != "*" || !reflect.DeepEqual(after["tun"], before["tun"]) {
		t.Fatalf("after: %v %v", after, err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodGet} {
		req := <-requests
		if req.Operation != privileged.Control || req.Method != method || req.Path != "/configs" {
			t.Fatalf("unexpected helper operation: %+v", req)
		}
		if method == http.MethodPatch {
			var got map[string]any
			if err := json.Unmarshal(req.Body, &got); err != nil || !reflect.DeepEqual(got, patch) {
				t.Fatalf("unexpected LAN patch: %s %v", req.Body, err)
			}
		}
	}
}
