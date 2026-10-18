package server

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
	"github.com/ronigooja/Nagi/internal/requestrewriteservice"
)

// Explicit opt-in only: root-private temp home, fictitious local policy, no
// TUN, trust changes, external DNS, subscriptions or installed configuration.
func TestRewriteRealRestrictedOwnerAndReload(t *testing.T) {
	binary := os.Getenv("NAGI_REWRITE_TEST_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("requires root and explicit NAGI_REWRITE_TEST_ENGINE")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(home, ".nagi-rewrite-helper-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	trusted := filepath.Join(root, "mihomo")
	src, e := os.Open(binary)
	if e != nil {
		t.Fatal(e)
	}
	defer src.Close()
	dst, e := os.OpenFile(trusted, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		t.Fatal(e)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy explicit fixture: %v %v", copyErr, closeErr)
	}
	for _, owner := range []int{1000, 0} {
		t.Run(map[bool]string{true: "bound", false: "unbound"}[owner > 0], func(t *testing.T) {
			dir := filepath.Join(root, t.Name()[strings.LastIndex(t.Name(), "/")+1:])
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			sourceDir, private := filepath.Join(dir, "source"), filepath.Join(dir, "private")
			for _, d := range []string{sourceDir, private} {
				if e := os.Mkdir(d, 0700); e != nil {
					t.Fatal(e)
				}
			}
			path := filepath.Join(sourceDir, "fixture.yaml")
			data := []byte("mixed-port: 0\nlog-level: silent\nfind-process-mode: off\ntun:\n  enable: false\nrules: ['MATCH,DIRECT']\n")
			if e := os.WriteFile(path, data, 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Chown(path, owner, -1); e != nil {
				t.Fatal(e)
			}
			h := &RuntimeHandler{SessionUID: owner, BinaryPath: trusted, SourceDir: sourceDir, PrivateDir: private}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			defer func() {
				if e := h.Close(); e != nil {
					t.Errorf("close isolated child: %v", e)
				}
			}()
			response, e := h.Handle(ctx, owner, privileged.Request{Operation: privileged.Start, ConfigPath: path, Profile: "fixture"})
			if e != nil || !response.Running {
				t.Fatalf("start: %v", e)
			}
			var flags []string
			for _, arg := range h.child.Args {
				if strings.HasPrefix(arg, "--request-rewrite-owner-uid") {
					flags = append(flags, arg)
				}
			}
			if owner > 0 && (len(flags) != 1 || flags[0] != "--request-rewrite-owner-uid=1000") || owner == 0 && len(flags) != 0 {
				t.Fatal("incorrect trusted owner bootstrap argument")
			}
			snapshot, e := domain.Compile(domain.Document{Version: 1, Rules: []domain.Rule{}, Domains: []domain.Domain{{ID: "api", Pattern: "api.service.example"}}})
			if e != nil {
				t.Fatal(e)
			}
			service, e := requestrewriteservice.New(snapshot, rewriteTestCertificate(t))
			if e != nil {
				t.Fatal(e)
			}
			defer service.Close()
			announcement := service.Announcement()
			spec := rewriteTestPrepare(t)
			spec.HandoffAddr = announcement.HandoffAddr
			spec.ChildTransportCert = announcement.ChildTransportCert
			spec.RuleVersion = announcement.RuleVersion
			spec.Domains = []string{"api.service.example"}
			request := privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/request-rewrite/session", Body: rewriteTestBody(t, spec)}
			if owner == 0 {
				response, e = sendControl(ctx, h.controllerPath(), request)
				if e != nil || response.Status != 403 {
					t.Fatalf("unbound root accepted owner: HTTP %d, %v", response.Status, e)
				}
				return
			}
			wrong := spec
			wrong.OwnerUID = 1001
			wrongRequest := request
			wrongRequest.Body = rewriteTestBody(t, wrong)
			// Bypass the helper check to prove that root peer identity alone cannot
			// override the immutable owner UID in the actual restricted engine.
			response, e = sendControl(ctx, h.controllerPath(), wrongRequest)
			if e != nil || response.Status != 403 {
				t.Fatalf("engine accepted mismatched owner: HTTP %d, %v", response.Status, e)
			}
			response, e = h.Handle(ctx, owner, request)
			if e != nil || !response.OK {
				t.Fatalf("prepare: HTTP %d, %v", response.Status, e)
			}
			var prepared struct {
				Generation  string `json:"generation"`
				Epoch       string `json:"engine_epoch"`
				Address     string `json:"reinjection_addr"`
				Certificate string `json:"engine_transport_cert"`
			}
			if e := json.Unmarshal(response.Body, &prepared); e != nil {
				t.Fatal(e)
			}
			if e := service.Configure(requestrewriteservice.Configuration{Generation: prepared.Generation, EngineEpoch: prepared.Epoch, ReinjectionAddr: prepared.Address, EngineTransportCert: prepared.Certificate, HandoffSecret: spec.HandoffSecret, ReinjectionSecret: spec.ReinjectionSecret}); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- service.Serve() }()
			defer func() {
				service.Close()
				if e := <-done; e != nil {
					t.Errorf("service: %v", e)
				}
			}()
			probe := privileged.Request{Operation: privileged.Control, Method: "POST", Path: "/request-rewrite/probe", Body: rewriteTestBody(t, map[string]any{"version": 1, "generation": spec.Generation})}
			response, e = h.Handle(ctx, owner, probe)
			if e != nil || !response.OK || !strings.Contains(string(response.Body), `"ready":true`) {
				t.Fatalf("mTLS synthetic probe: HTTP %d, %v", response.Status, e)
			}
			reload := privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: rewriteTestBody(t, map[string]string{"path": path, "activation_id": strings.Repeat("b", 32)})}
			response, e = h.Handle(ctx, owner, reload)
			if e != nil || !response.OK {
				t.Fatalf("reload: HTTP %d, %v", response.Status, e)
			}
			response, e = sendControl(ctx, h.controllerPath(), wrongRequest)
			if e != nil || response.Status != 403 {
				t.Fatalf("reload lost owner binding: HTTP %d, %v", response.Status, e)
			}
			response, e = h.Handle(ctx, owner, probe)
			if e != nil || !response.OK {
				t.Fatalf("reload lost prepared session/probe: HTTP %d, %v", response.Status, e)
			}
			status := privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/request-rewrite/status"}
			response, e = h.Handle(ctx, owner, status)
			if e != nil || !response.OK || !strings.Contains(string(response.Body), `"tun_running":false`) {
				t.Fatalf("status without TUN: HTTP %d, %v", response.Status, e)
			}
			for _, secret := range []string{spec.HandoffSecret, spec.ReinjectionSecret, announcement.HandoffAddr, prepared.Address, prepared.Certificate} {
				if strings.Contains(string(response.Body), secret) {
					t.Fatal("status exposed session metadata")
				}
			}
			remove := privileged.Request{Operation: privileged.Control, Method: "DELETE", Path: "/request-rewrite/session?generation=" + spec.Generation}
			response, e = h.Handle(ctx, owner, remove)
			if e != nil || !response.OK {
				t.Fatalf("delete candidate: HTTP %d, %v", response.Status, e)
			}
		})
	}
}
