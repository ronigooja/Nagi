package requestrewriteservice

import (
	"bufio"
	"bytes"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/elazarl/goproxy"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func gateClient(t *testing.T, f *fixture, cert tls.Certificate) net.Conn {
	t.Helper()
	raw, e := net.Dial("tcp4", f.s.Announcement().HandoffAddr)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	peer, _ := x509.ParseCertificate(f.s.cert.Certificate[0])
	roots.AddCert(peer)
	conn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: roots, ServerName: childRole})
	conn.SetDeadline(time.Now().Add(time.Second))
	return conn
}
func handoffHead(f *fixture, secret, token, extra string) string {
	return fmt.Sprintf("CONNECT api.service.example:80 HTTP/1.1\r\nHost: api.service.example:80\r\nAuthorization: Bearer %s\r\nNagi-Rewrite-Version: 2\r\nNagi-Rewrite-Generation: %s\r\nNagi-Rewrite-Epoch: %s\r\nNagi-Rewrite-Context: %s\r\nNagi-Rewrite-Protocol: http\r\n%s\r\n", secret, f.config.Generation, f.config.EngineEpoch, token, extra)
}
func TestAuthenticatedGateRejectsAndRedacts(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	token, _ := randomValue(32)
	wrongSecret, _ := randomValue(32)
	for _, head := range []string{handoffHead(f, wrongSecret, token, ""), handoffHead(f, f.config.HandoffSecret, token, "Nagi-Rewrite-Version: 2\r\n"), strings.Replace(handoffHead(f, f.config.HandoffSecret, token, ""), "Host: api.service.example:80", "Host: mirror.service.example:80", 1), strings.Replace(handoffHead(f, f.config.HandoffSecret, token, ""), "Nagi-Rewrite-Version: 2\r\n", "Nagi-Rewrite-Version: 2\n", 1)} {
		conn := gateClient(t, f, f.engine)
		io.WriteString(conn, head)
		resp, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
		if e != nil {
			conn.Close()
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		conn.Close()
		if resp.StatusCode == 200 || bytes.Contains(body, []byte(token)) || bytes.Contains(body, []byte(f.config.HandoffSecret)) || bytes.Contains(body, []byte(wrongSecret)) {
			t.Fatal("gate accepted or leaked credentials")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opens) != 0 {
		t.Fatal("rejected handoff opened upstream")
	}
	if f.s.Status().Contexts != 0 {
		t.Fatal("rejected gate allocated context")
	}
}
func resigned(t *testing.T, c tls.Certificate, edit func(*x509.Certificate)) tls.Certificate {
	t.Helper()
	template, e := x509.ParseCertificate(c.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	template.SerialNumber = new(big.Int).Add(template.SerialNumber, big.NewInt(1))
	edit(template)
	der, e := x509.CreateCertificate(cryptorand.Reader, template, template, template.PublicKey, c.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: c.PrivateKey}
}

func TestTransportCertificatePinsRolesAndValidity(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	for _, cert := range []tls.Certificate{engineCert(t), resigned(t, f.engine, func(*x509.Certificate) {})} {
		conn := gateClient(t, f, cert)
		io.WriteString(conn, handoffHead(f, f.config.HandoffSecret, strings.Repeat("a", 43), ""))
		if _, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"}); e == nil {
			t.Fatal("unregistered transport certificate accepted")
		}
		conn.Close()
	}
	for _, cert := range []tls.Certificate{resigned(t, f.engine, func(c *x509.Certificate) {
		c.NotBefore = time.Now().Add(-2 * time.Hour)
		c.NotAfter = time.Now().Add(-time.Hour)
	}), resigned(t, f.engine, func(c *x509.Certificate) { c.DNSNames = []string{"wrong-role.invalid"} })} {
		if _, e := parsePeer(base64.StdEncoding.EncodeToString(cert.Certificate[0])); e == nil {
			t.Fatal("invalid peer role/validity accepted")
		}
	}
}
func TestUpstreamTLSVerificationAndRedaction(t *testing.T) {
	for _, trust := range []bool{false, true} {
		var requests atomic.Int64
		origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
		f := newFixture(t, origin.Listener.Addr().String())
		if !trust {
			cfg, e := goproxy.TLSConfigFromCA(&f.s.ca)("api.service.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
			if e != nil {
				t.Fatal(e)
			}
			cfg.InsecureSkipVerify = false
			origin.TLS = cfg
		}
		origin.StartTLS()
		if trust {
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			f.s.originRoots = roots
		}
		wire := f.handoff(t, "tls")
		conn := tls.Client(wire, &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"http/1.1"}})
		req, _ := http.NewRequest("GET", "https://api.service.example/private?secret=fixture-secret", nil)
		req.Header.Set("Authorization", "application-secret")
		req.Header.Set("Cookie", "fixture-cookie")
		if e := req.Write(conn); e != nil {
			t.Fatal(e)
		}
		resp, e := http.ReadResponse(bufio.NewReader(conn), req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		conn.Close()
		origin.Close()
		status, _ := json.Marshal(f.s.Status())
		for _, value := range []string{"application-secret", "fixture-cookie", "fixture-secret", f.config.HandoffSecret, f.config.ReinjectionSecret} {
			if bytes.Contains(status, []byte(value)) || bytes.Contains(body, []byte(value)) {
				t.Fatal("status/error leaked sensitive input")
			}
		}
		if resp.StatusCode != 502 || string(body) != "rewrite_unavailable\n" || requests.Load() != 0 {
			t.Fatalf("TLS validation/redaction %d %q %d", resp.StatusCode, body, requests.Load())
		}
	}
}
func TestUnexpectedResponseUpgradesRejected(t *testing.T) {
	for index, response := range []string{"HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", "HTTP/1.1 200 OK\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nContent-Length: 0\r\n\r\n", "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: arbitrary\r\n\r\n"} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				return
			}
			defer conn.Close()
			io.WriteString(conn, response)
		}))
		f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"))
		conn := f.handoff(t, "http")
		requestHead := "GET / HTTP/1.1\r\nHost: api.service.example\r\n"
		if index == 2 {
			requestHead += "Upgrade: websocket\r\nConnection: Upgrade\r\n"
		}
		io.WriteString(conn, requestHead+"\r\n")
		resp, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		conn.Close()
		origin.Close()
		if resp.StatusCode != 502 || string(body) != "rewrite_invalid\n" {
			t.Fatalf("unexpected upgrade %d %q", resp.StatusCode, body)
		}
	}
}
