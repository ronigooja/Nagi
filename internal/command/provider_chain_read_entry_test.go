package command

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/profile"
	"gopkg.in/yaml.v3"
)

// Exercise Run's read-store construction, not just the resolver closure. Use
// the production helper endpoint because there is intentionally no endpoint
// override. Never replace a real helper; unprivileged hosts may skip this test.
func TestProviderChainReadEntryUsesHelperMetadata(t *testing.T) {
	socket := privileged.SocketPath(os.Getuid())
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Skip("production helper endpoint already exists or cannot be inspected")
	}
	dir := filepath.Dir(socket)
	_, dirErr := os.Lstat(dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Skipf("cannot create isolated helper endpoint: %v", err)
	}
	if os.IsNotExist(dirErr) {
		t.Cleanup(func() { _ = os.Remove(dir) })
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("helper endpoint unavailable: %v", err)
	}
	var mu sync.Mutex
	var requests []privileged.Request
	running := true
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			req, err := privileged.ReadRequest(conn)
			if err != nil {
				conn.Close()
				continue
			}
			mu.Lock()
			requests = append(requests, req)
			active := running
			mu.Unlock()
			response := privileged.Response{Version: privileged.ProtocolVersion, OK: true}
			switch req.Operation {
			case privileged.Status:
				response.Running = active
				if active {
					response.PID = 123
				}
			case privileged.ProviderChainNodes:
				response.Body = json.RawMessage(`[{"name":"Raw B.+","type":"socks5"},{"name":"Raw A","type":"socks5"},{"name":"Raw Gone","type":"socks5"}]`)
			case privileged.Control:
				response.Status = 200
				response.Body = json.RawMessage(`{"proxies":[{"name":"P A","type":"Socks5"},{"name":"P B.+","type":"Socks5"}]}`)
			default:
				response.OK = false
				response.Error = "unexpected operation"
			}
			_ = privileged.WriteResponse(conn, response)
			conn.Close()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })

	paths := isolatedTestPaths(t, t.TempDir())
	if err := writeBackend(paths, privilegedBackend); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte(`mode: rule
proxies:
  - {name: Target, type: socks5, server: 192.0.2.1, port: 1080}
proxy-providers:
  P:
    type: http
    url: https://example.invalid/feed
    override:
      proxy-name:
        - {pattern: '^Raw (.*)$', target: '$1'}
      additional-prefix: 'P '
proxy-groups:
  - {name: Select, type: select, proxies: [Target], use: [P]}
rules: [MATCH,Select]
`)
	profilePath := filepath.Join(paths.ConfigDir, "profiles", "default.yaml")
	if err := os.WriteFile(profilePath, source, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAGI_MIHOMO_BIN", filepath.Join(t.TempDir(), "missing-engine"))
	var cfg map[string]any
	if err := yaml.Unmarshal(source, &cfg); err != nil {
		t.Fatal(err)
	}
	provider := cfg["proxy-providers"].(map[string]any)["P"].(map[string]any)
	hash := profile.ProviderSourceHash(provider)

	run := func(args ...string) map[string]json.RawMessage {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run(append([]string{"--json", "proxy", "chain"}, args...), &stdout, &stderr, "test", "commit"); code != 0 {
			t.Fatalf("chain %v exit=%d stderr=%s", args, code, stderr.String())
		}
		var envelope struct {
			OK   bool
			Data map[string]json.RawMessage
		}
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || !envelope.OK {
			t.Fatalf("response=%s err=%v", stdout.String(), err)
		}
		return envelope.Data
	}
	assertNames := func(data map[string]json.RawMessage, key string, want []string) {
		t.Helper()
		var nodes []profile.ProxyChainTarget
		if err := json.Unmarshal(data[key], &nodes); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, n := range nodes {
			if n.Kind == "provider-node" {
				if n.Provider != "P" || !n.Available || !n.Supported {
					t.Fatal(n)
				}
				names = append(names, n.Name)
			}
		}
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("%s provider names=%v want=%v", key, names, want)
		}
		var unavailable []string
		if err := json.Unmarshal(data["unavailable_providers"], &unavailable); err != nil || len(unavailable) != 0 {
			t.Fatalf("unavailable=%s err=%v", data["unavailable_providers"], err)
		}
	}
	for _, action := range []string{"list", "show", "candidates", "plan"} {
		t.Run(action, func(t *testing.T) {
			mu.Lock()
			requests = nil
			mu.Unlock()
			switch action {
			case "list":
				assertNames(run("list", "provider-node"), "chains", []string{"P B.+", "P A"})
			case "show":
				data := run("show", "provider-node", "P", "P B.+")
				var node profile.ProxyChainTarget
				if err := json.Unmarshal(data["chain"], &node); err != nil || node.Name != "P B.+" || !node.Available {
					t.Fatalf("chain=%s err=%v", data["chain"], err)
				}
			case "candidates":
				assertNames(run("candidates", "node", "Target"), "nodes", []string{"P B.+", "P A"})
			case "plan":
				data := run("set", "provider-node", "P", "P B.+", "--via-provider", "P", "P A", "--dry-run")
				var plan profile.ProxyChainPlan
				if err := json.Unmarshal(data["plan"], &plan); err != nil || len(plan.Conflicts) != 0 || plan.Outlet == "" {
					t.Fatalf("plan=%s err=%v", data["plan"], err)
				}
				if string(data["saved"]) != "false" || string(data["reloaded"]) != "false" {
					t.Fatal(data)
				}
			}
			mu.Lock()
			got := append([]privileged.Request(nil), requests...)
			mu.Unlock()
			metadata, control := 0, 0
			for _, req := range got {
				switch req.Operation {
				case privileged.Status:
				case privileged.ProviderChainNodes:
					metadata++
					if req.Version != privileged.ProtocolVersion || req.Profile != "default" || req.Provider != "P" || req.SourceHash != hash || req.Path != "" || len(req.Body) != 0 {
						t.Fatalf("unsafe metadata request: %+v", req)
					}
				case privileged.Control:
					control++
					if req.Method != "GET" || req.Path != "/providers/proxies/P" || len(req.Body) != 0 {
						t.Fatalf("non-read control request: %+v", req)
					}
				default:
					t.Fatalf("unexpected helper operation: %s", req.Operation)
				}
			}
			if metadata == 0 || control != metadata {
				t.Fatalf("metadata=%d control=%d", metadata, control)
			}
		})
	}
	// Stopped and ordinary backends must keep local evidence usable without
	// metadata/control requests. The cache exists only for these fallback checks.
	cachePath := filepath.Join(paths.ConfigDir, "profiles", "local-provider.yaml")
	if err := os.WriteFile(cachePath, []byte("proxies:\n  - {name: Raw Local, type: socks5, server: 192.0.2.2, port: 1080}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	provider["path"] = cachePath
	localSource, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, localSource, 0600); err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{privilegedBackend, userBackend} {
		t.Run(backend+"-local", func(t *testing.T) {
			if err := writeBackend(paths, backend); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			running = false
			requests = nil
			mu.Unlock()
			assertNames(run("list", "provider-node"), "chains", []string{"P Local"})
			mu.Lock()
			defer mu.Unlock()
			for _, req := range requests {
				if backend == userBackend || req.Operation != privileged.Status {
					t.Fatalf("fallback contacted helper: %+v", req)
				}
			}
		})
	}
	actual, err := os.ReadFile(profilePath)
	if err != nil || !bytes.Equal(actual, localSource) {
		t.Fatal("read commands changed source", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "overrides", "default.yaml")); !os.IsNotExist(err) {
		t.Fatal("read commands created override", err)
	}
}
