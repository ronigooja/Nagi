package command

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
)

func TestPrivilegedProviderResolverUsesRawEvidenceAndActiveIntersection(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(map[bool]string{true: "running", false: "stopped"}[running], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper.sock")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			source := map[string]any{"type": "http", "url": "https://example.invalid/feed", "override": map[string]any{"proxy-name": []any{map[string]any{"pattern": "^Raw (.*)$", "target": "$1"}}, "additional-prefix": "P "}}
			requests := make(chan privileged.Request, 3)
			go func() {
				for {
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
					response := privileged.Response{Version: privileged.ProtocolVersion, OK: true}
					switch req.Operation {
					case privileged.Status:
						response.Running = running
						response.PID = 123
					case privileged.ProviderChainNodes:
						response.Body = json.RawMessage(`[{"name":"Raw B","type":"socks5"},{"name":"Raw A","type":"socks5"},{"name":"Raw Gone","type":"socks5"}]`)
					case privileged.Control:
						response.Status = 200
						response.Body = json.RawMessage(`{"proxies":[{"name":"P A","type":"Socks5"},{"name":"P B","type":"Socks5"}]}`)
					}
					privileged.WriteResponse(conn, response)
					conn.Close()
				}
			}()
			b := helperBackend{socketPath: path}
			nodes, err := providerChainNodeResolver(context.Background(), b, b.controlClient())("work", "P", source)
			if err != nil {
				t.Fatal(err)
			}
			req := <-requests
			if req.Operation != privileged.Status {
				t.Fatal(req)
			}
			if !running {
				if nodes != nil {
					t.Fatal("stopped resolver did not delegate")
				}
				return
			}
			if len(nodes) != 2 || nodes[0]["name"] != "Raw B" || nodes[1]["name"] != "Raw A" {
				t.Fatal("raw identity/source order lost", nodes)
			}
			req = <-requests
			if req.Operation != privileged.ProviderChainNodes || req.Profile != "work" || req.Provider != "P" || req.SourceHash != profile.ProviderSourceHash(source) || req.Path != "" || len(req.Body) != 0 {
				t.Fatal("unsafe identity request", req)
			}
			req = <-requests
			if req.Operation != privileged.Control || req.Method != "GET" || req.Path != "/providers/proxies/P" {
				t.Fatal("inspection did not stay read-only", req)
			}
		})
	}
}
