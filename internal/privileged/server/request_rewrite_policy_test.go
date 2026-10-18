package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func rewriteTestCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "nagi-rewrite-child.internal"}, DNSNames: []string{"nagi-rewrite-child.internal"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
func rewriteTestPrepare(t *testing.T) rewritePrepare {
	return rewritePrepare{Version: 1, Generation: strings.Repeat("a", 32), OwnerUID: 1000, HandoffAddr: "127.0.0.1:43101", ChildTransportCert: base64.StdEncoding.EncodeToString(rewriteTestCertificate(t).Certificate[0]), HandoffSecret: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), ReinjectionSecret: base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32))), Domains: []string{"api.service.example", "*.service.example"}, RuleVersion: "sha256:" + strings.Repeat("a", 64), BlockAuthorizedQUIC: true}
}
func rewriteTestBody(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestRewriteControlExactAllowlist(t *testing.T) {
	prepare := rewriteTestBody(t, rewriteTestPrepare(t))
	allowed := []privileged.Request{{Method: "GET", Path: "/request-rewrite/capabilities"}, {Method: "GET", Path: "/request-rewrite/status"}, {Method: "PUT", Path: "/request-rewrite/session", Body: prepare}, {Method: "POST", Path: "/request-rewrite/probe", Body: []byte(`{"version":1,"generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)}, {Method: "PUT", Path: "/request-rewrite/branch", Body: []byte(`{"version":1,"generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":false}`)}, {Method: "DELETE", Path: "/request-rewrite/session?generation=" + strings.Repeat("a", 32)}}
	for _, req := range allowed {
		if e := validateControl(req); e != nil {
			t.Fatalf("%s %s: %v", req.Method, req.Path, e)
		}
		want := req.Method == "GET"
		if snapshotControl(req) != want {
			t.Fatalf("snapshot classification %s", req.Path)
		}
	}
	paths := []string{"/request-rewrite", "/request-rewrite/status/", "/request-rewrite/heartbeat", "/request-rewrite/status?", "/request-rewrite/status?x=1", "/request-rewrite/%73tatus", "/request-rewrite/session?generation=" + strings.Repeat("a", 32) + "&generation=" + strings.Repeat("a", 32), "/request-rewrite/session?generation=" + strings.Repeat("A", 32), "/request-rewrite/session?generation=" + strings.Repeat("g", 32), "/request-rewrite/session?generation=%61" + strings.Repeat("a", 31)}
	for _, path := range paths {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT"} {
			if e := validateControl(privileged.Request{Method: method, Path: path}); e == nil {
				t.Errorf("allowed %s %s", method, path)
			}
		}
	}
	for _, req := range allowed {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT"} {
			if method == req.Method {
				continue
			}
			changed := req
			changed.Method = method
			if validateControl(changed) == nil {
				t.Errorf("wrong method %s %s", method, req.Path)
			}
		}
		changed := req
		changed.Body = append(append([]byte{}, req.Body...), []byte(`{}`)...)
		if validateControl(changed) == nil {
			t.Errorf("extra body %s", req.Path)
		}
	}
}
func TestRewriteStrictSchemaAndBounds(t *testing.T) {
	good := rewriteTestPrepare(t)
	body := string(rewriteTestBody(t, good))
	invalid := []string{"null", "[]", body + " {}", strings.Replace(body, `"version":1`, `"version":1,"version":1`, 1), strings.Replace(body, `"version":1`, `"Version":1`, 1), strings.Replace(body, `"version":1`, `"version":null`, 1), strings.Replace(body, `"version":1`, `"version":2`, 1), strings.Replace(body, `"version":1`, `"version":1,"path":"/etc/shadow"`, 1), strings.Replace(body, `"previous_generation":null`, `"previous_generation":{"x":1,"x":2}`, 1), strings.Replace(body, `"domains":[`, `"domains":[null,`, 1), strings.Repeat(" ", rewriteControlLimit) + body}
	for i, b := range invalid {
		if validateControl(privileged.Request{Method: "PUT", Path: "/request-rewrite/session", Body: []byte(b)}) == nil {
			t.Errorf("accepted invalid JSON case %d", i)
		}
	}
	changes := []func(*rewritePrepare){func(v *rewritePrepare) { v.OwnerUID = 0 }, func(v *rewritePrepare) { v.OwnerUID = ^uint32(0) }, func(v *rewritePrepare) { v.Generation = strings.Repeat("A", 32) }, func(v *rewritePrepare) { s := "bad"; v.PreviousGeneration = &s }, func(v *rewritePrepare) { v.HandoffAddr = "localhost:43101" }, func(v *rewritePrepare) { v.HandoffAddr = "127.0.0.1:0" }, func(v *rewritePrepare) { v.HandoffAddr = "127.0.0.1:043101" }, func(v *rewritePrepare) { v.HandoffAddr = "0.0.0.0:43101" }, func(v *rewritePrepare) { v.HandoffAddr = "[::1]:43101" }, func(v *rewritePrepare) { v.ChildTransportCert = "/etc/shadow" }, func(v *rewritePrepare) { v.HandoffSecret = v.ReinjectionSecret }, func(v *rewritePrepare) { v.HandoffSecret += "=" }, func(v *rewritePrepare) { v.RuleVersion = "sha256:" + strings.Repeat("A", 64) }, func(v *rewritePrepare) { v.BlockAuthorizedQUIC = false }, func(v *rewritePrepare) { v.Domains = nil }, func(v *rewritePrepare) { v.Domains = []string{"*.com"} }, func(v *rewritePrepare) { v.Domains = []string{"api.service.example", "API.SERVICE.EXAMPLE."} }, func(v *rewritePrepare) { v.Domains = []string{"http://api.service.example"} }, func(v *rewritePrepare) { v.Domains = make([]string, 257) }}
	for i, change := range changes {
		v := good
		change(&v)
		if _, e := decodeRewritePrepare(rewriteTestBody(t, v)); e == nil {
			t.Errorf("accepted invalid prepare %d", i)
		}
	}
	for _, b := range []string{`{"version":1,"generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":null}`, `{"version":1,"generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","Enabled":true}`, `{"version":1,"generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":true,"enabled":false}`, `{"version":1,"generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`} {
		if validateControl(privileged.Request{Method: "PUT", Path: "/request-rewrite/branch", Body: []byte(b)}) == nil {
			t.Error("accepted invalid branch")
		}
	}
}
func TestRewriteTrustedOwner(t *testing.T) {
	v := rewriteTestPrepare(t)
	for _, test := range []struct {
		session, peer int
		owner         uint32
		want          bool
	}{{1000, 1000, 1000, true}, {1000, 1001, 1000, false}, {1000, 1000, 1001, false}, {0, 0, 1000, false}, {1000, 0, 1000, false}, {-1, -1, 1000, false}} {
		h := &RuntimeHandler{SessionUID: test.session, rewriteOwnerBound: true}
		v.OwnerUID = test.owner
		req := privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/request-rewrite/session", Body: rewriteTestBody(t, v)}
		if got := h.validateRewriteOwner(test.peer, req) == nil; got != test.want {
			t.Errorf("owner validation %+v: %v", test, got)
		}
		if !test.want {
			if _, e := h.Handle(context.Background(), test.peer, req); e != errRewritePolicy {
				t.Errorf("owner check did not precede engine access: %v", e)
			}
		}
	}
	h := &RuntimeHandler{SessionUID: 1000}
	for _, path := range []string{"/request-rewrite/status", "/request-rewrite/capabilities"} {
		if _, e := h.Handle(context.Background(), 1001, privileged.Request{Operation: privileged.Control, Method: "GET", Path: path}); e != errRewritePolicy {
			t.Error("snapshot bypassed owner check")
		}
	}
}
func TestRewriteResponseProjection(t *testing.T) {
	req := privileged.Request{Method: "GET", Path: "/request-rewrite/status"}
	response, e := filterRewriteResponse(req, privileged.Response{OK: true, Status: 200, Body: []byte(`{"version":1,"state":"absent","handoff_secret":"SECRET","engine_transport_cert":"CERT","reinjection_addr":"ADDR","path":"PRIVATE"}`)})
	if e != nil || strings.Contains(string(response.Body), "SECRET") || strings.Contains(string(response.Body), "CERT") || strings.Contains(string(response.Body), "ADDR") || strings.Contains(string(response.Body), "PRIVATE") {
		t.Fatal("status exposed private metadata")
	}
	response, e = filterRewriteResponse(req, privileged.Response{Status: 400, Body: []byte(`{"error":{"code":"rewrite_invalid","field":"SECRET","message":"SECRET"}}`)})
	if e != nil || strings.Contains(string(response.Body), "SECRET") || !strings.Contains(string(response.Body), "rewrite_invalid") {
		t.Fatal("error exposed input")
	}
}

func TestRewriteOwnerBootstrapHelpDiscovery(t *testing.T) {
	for _, test := range []struct {
		name, script string
		want         bool
	}{
		{"supported", "echo '  -request-rewrite-owner-uid string'", true},
		{"old", "echo '  -config-policy string'", false},
		{"prose", "echo 'unsupported request-rewrite-owner-uid'", false},
		{"failed", "echo '  -request-rewrite-owner-uid string';exit 1", false},
		{"oversized", "head -c 70000 /dev/zero;echo '  -request-rewrite-owner-uid string'", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "engine")
			if e := os.WriteFile(path, []byte("#!/bin/sh\n"+test.script+"\n"), 0700); e != nil {
				t.Fatal(e)
			}
			if got := rewriteOwnerBootstrapSupported(context.Background(), path, dir); got != test.want {
				t.Fatalf("support = %v", got)
			}
		})
	}
	h := &RuntimeHandler{SessionUID: 1000}
	req := privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/request-rewrite/session", Body: rewriteTestBody(t, rewriteTestPrepare(t))}
	if _, e := h.Handle(context.Background(), 1000, req); e == nil || !strings.Contains(e.Error(), "rewrite_capability_missing") {
		t.Fatal("unbound engine did not request upgrade")
	}
}
