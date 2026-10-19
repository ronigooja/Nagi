package requestrewriteservice

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/elazarl/goproxy"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
	"golang.org/x/net/http2"
)

type fixture struct {
	caDir    string
	childDER []byte
	s        *Service
	engine   tls.Certificate
	config   Configuration
	root     *x509.CertPool
	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	opens    []string
	origin   string
}

func engineCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: engineRole}, DNSNames: []string{engineRole}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
func newFixture(t *testing.T, origin string, documents ...[]byte) *fixture {
	t.Helper()
	dir := t.TempDir()
	store := domain.CAStore{ConfigDir: dir}
	if _, e := store.Generate(time.Now()); e != nil {
		t.Fatal(e)
	}
	cert, key, e := store.LoadForChild(time.Now())
	if e != nil {
		t.Fatal(e)
	}
	document := []byte(`{"version":2,"enabled":false,"rules":[{"id":"header","enabled":true,"match":{"domain":"api.service.example","methods":["GET","POST"],"path_prefix":"/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"header_set","name":"X-Test-Rewrite","value":"edited"}],"stage":"request"}],"scripts":[]}`)
	if len(documents) > 0 {
		document = documents[0]
	}
	snapshot, e := domain.Parse(document)
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(snapshot, tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert})
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	gen, _ := NewGeneration()
	epoch, _ := NewGeneration()
	handoff, _ := randomValue(32)
	reinjection, _ := randomValue(32)
	engine := engineCert(t)
	c := Configuration{gen, epoch, l.Addr().String(), base64.StdEncoding.EncodeToString(engine.Certificate[0]), handoff, reinjection}
	if e = s.Configure(c); e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	f := &fixture{caDir: dir, s: s, engine: engine, config: c, root: roots, listener: l, origin: origin}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			f.wg.Add(1)
			go func() { defer f.wg.Done(); f.receive(conn) }()
		}
	}()
	go s.Serve()
	t.Cleanup(func() { s.Close(); l.Close(); f.wg.Wait() })
	return f
}
func (f *fixture) receive(raw net.Conn) {
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(10 * time.Second))
	f.mu.Lock()
	der := f.childDER
	if der == nil {
		der = f.s.cert.Certificate[0]
	}
	peer, _ := x509.ParseCertificate(der)
	f.mu.Unlock()
	roots := x509.NewCertPool()
	roots.AddCert(peer)
	conn := tls.Server(raw, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{f.engine}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots})
	if conn.Handshake() != nil {
		return
	}
	r := bufio.NewReader(conn)
	req, e := http.ReadRequest(r)
	if e != nil {
		return
	}
	if req.Method == "POST" {
		io.Copy(io.Discard, req.Body)
		io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 17\r\n\r\n{\"accepted\":true}")
		return
	}
	if !bearer(req.Header.Get("Authorization"), f.config.ReinjectionSecret) {
		return
	}
	f.mu.Lock()
	f.opens = append(f.opens, req.Host)
	f.mu.Unlock()
	io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if req.Header.Get("Nagi-Rewrite-Probe") == "1" {
		b := make([]byte, len(probePayload))
		if _, e = io.ReadFull(r, b); e == nil {
			conn.Write(b)
		}
		return
	}
	up, e := net.Dial("tcp4", f.origin)
	if e != nil {
		return
	}
	defer up.Close()
	up.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() { io.Copy(up, r); close(done) }()
	io.Copy(conn, up)
	conn.Close()
	up.Close()
	<-done
}
func (f *fixture) handoff(t *testing.T, protocol string) net.Conn {
	t.Helper()
	raw, e := net.Dial("tcp4", f.s.Announcement().HandoffAddr)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	peer, _ := x509.ParseCertificate(f.s.cert.Certificate[0])
	roots.AddCert(peer)
	conn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{f.engine}, RootCAs: roots, ServerName: childRole})
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if e = conn.Handshake(); e != nil {
		t.Fatal(e)
	}
	tok, _ := randomValue(32)
	fmt.Fprintf(conn, "CONNECT api.service.example:80 HTTP/1.1\r\nHost: api.service.example:80\r\nAuthorization: Bearer %s\r\nNagi-Rewrite-Version: 2\r\nNagi-Rewrite-Generation: %s\r\nNagi-Rewrite-Epoch: %s\r\nNagi-Rewrite-Context: %s\r\nNagi-Rewrite-Protocol: %s\r\n\r\n", f.config.HandoffSecret, f.config.Generation, f.config.EngineEpoch, tok, protocol)
	r := bufio.NewReader(conn)
	if e = readOK(r); e != nil {
		t.Fatal(e)
	}
	return &bufferedConn{conn, r}
}
func TestHTTPStreamingAndHeaderBoundary(t *testing.T) {
	payload := bytes.Repeat([]byte("streaming-payload"), 700000)
	received := make(chan int64, 2)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Rewrite") != "edited" || r.Header.Get("Authorization") != "app-secret" || r.Header.Get("Nagi-Rewrite-Context") != "" || r.Header.Get("X-Hop") != "" {
			t.Error("request header boundary failed")
		}
		n, _ := io.Copy(io.Discard, r.Body)
		received <- n
		w.Write([]byte("ok"))
	}))
	defer origin.Close()
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"))
	conn := f.handoff(t, "http")
	defer conn.Close()
	req, _ := http.NewRequest("POST", "http://api.service.example/upload", bytes.NewReader(payload))
	req.Header.Set("Authorization", "app-secret")
	req.Header.Set("Nagi-Rewrite-Context", "client-fake")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "private")
	if e := req.Write(conn); e != nil {
		t.Fatal(e)
	}
	resp, e := http.ReadResponse(bufio.NewReader(conn), req)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(body) != "ok" || resp.StatusCode != 200 {
		t.Fatalf("response %d %q %v", resp.StatusCode, body, e)
	}
	if n := <-received; n != int64(len(payload)) {
		t.Fatalf("streamed %d", n)
	}
}
func TestApplicationTLSHTTP2(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("h2-ok")) }))
	f := newFixture(t, origin.Listener.Addr().String())
	cfg, e := goproxy.TLSConfigFromCA(&f.s.ca)("api.service.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
	if e != nil {
		t.Fatal(e)
	}
	cfg.InsecureSkipVerify = false
	origin.TLS = cfg
	origin.StartTLS()
	defer origin.Close()
	f.s.originRoots = f.root
	conn := f.handoff(t, "tls")
	tc := tls.Client(conn, &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"h2"}})
	if e := tc.Handshake(); e != nil {
		t.Fatal(e)
	}
	transport := &http2.Transport{}
	client, e := transport.NewClientConn(tc)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	req, _ := http.NewRequest("GET", "https://api.service.example/", nil)
	resp, e := client.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(body) != "h2-ok" {
		t.Fatalf("%q %v", body, e)
	}
}

func TestRootChildRefused(t *testing.T) {
	if e := RunChild(); e != 1 {
		t.Fatal("child accepted absent inherited pipe or root")
	}
}
func TestFrameStrictness(t *testing.T) {
	for _, body := range []string{`{"ready":true,"ready":false}`, `{"unknown":1}`, `{} {}`} {
		var b bytes.Buffer
		var n [4]byte
		size := len(body)
		n[0] = byte(size >> 24)
		n[1] = byte(size >> 16)
		n[2] = byte(size >> 8)
		n[3] = byte(size)
		b.Write(n[:])
		b.WriteString(body)
		var reply childReply
		if readFrame(&b, &reply) == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestCloseCancelsPendingContext(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	cc := &connectionContext{ctx: ctx, cancel: cancel, service: f.s, sockets: map[net.Conn]bool{}, transports: map[string]*http.Transport{}}
	a, b := net.Pipe()
	defer b.Close()
	tracked := &trackedConn{Conn: a, owner: cc}
	cc.sockets[tracked] = true
	done := make(chan struct{})
	go func() { cc.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close deadlock")
	}
}

func TestWebSocketBufferedFirstFrame(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		kind, body, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		if err = c.Write(ctx, kind, body); err != nil {
			t.Error(err)
		}
		_, _, _ = c.Read(ctx)
	}))
	defer origin.Close()
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"))
	conn := f.handoff(t, "http")
	defer conn.Close()
	// The complete masked frame is sent with the upgrade head, exercising both
	// HTTP gate and codec buffers before either peer has accepted the handshake.
	frame := []byte{0x81, 0x86, 1, 2, 3, 4, 'F' ^ 1, 'R' ^ 2, 'A' ^ 3, 'M' ^ 4, 'E' ^ 1, '1' ^ 2}
	head := []byte("GET /ws HTTP/1.1\r\nHost: api.service.example\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: ZmFrZS1maXh0dXJlLWtleQ==\r\n\r\n")
	if _, err := conn.Write(append(head, frame...)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade %v %v", response, err)
	}
	got := make([]byte, 8)
	if _, err = io.ReadFull(reader, got); err != nil || !bytes.Equal(got, append([]byte{0x01, 6}, []byte("FRAME1")...)) {
		t.Fatalf("frame %x %v", got, err)
	}
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.s.Status().Contexts == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("upgrade context retained")
}
func TestHTTPPipelineAndDuplicateFraming(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.Path)) }))
	defer origin.Close()
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"))
	conn := f.handoff(t, "http")
	defer conn.Close()
	io.WriteString(conn, "GET /one HTTP/1.1\r\nHost: api.service.example\r\n\r\nGET /two HTTP/1.1\r\nHost: api.service.example\r\n\r\n")
	r := bufio.NewReader(conn)
	for _, want := range []string{"/one", "/two"} {
		resp, e := http.ReadResponse(r, &http.Request{Method: "GET"})
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil || string(body) != want {
			t.Fatalf("pipeline %q %v", body, e)
		}
	}
	io.WriteString(conn, "POST / HTTP/1.1\r\nHost: api.service.example\r\nContent-Length: 0\r\nContent-Length: 0\r\n\r\n")
	resp, e := http.ReadResponse(r, &http.Request{Method: "POST"})
	if e == nil {
		if resp.StatusCode < 400 {
			t.Fatalf("framing accepted %v", resp)
		}
		resp.Body.Close()
	}
}

func TestHTTP1JSONStallClosesAndReleasesReservation(t *testing.T) {
	doc := []byte(`{"version":2,"enabled":false,"rules":[{"id":"json","enabled":true,"match":{"domain":"api.service.example","methods":["POST"],"path_prefix":"/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"json_set","pointer":"/x","value":true}],"stage":"request","range_mode":"fragment"}],"scripts":[]}`)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.Write([]byte("ok")) }))
	defer origin.Close()
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"), doc)
	f.s.transformTimeout = 40 * time.Millisecond
	conn := f.handoff(t, "http")
	defer conn.Close()
	io.WriteString(conn, "POST / HTTP/1.1\r\nHost: api.service.example\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n")
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "POST"})
	if ne, ok := e.(net.Error); e == nil || ok && ne.Timeout() {
		t.Fatalf("stall not closed: %v", e)
	}
	deadline := time.Now().Add(time.Second)
	for f.s.Status().Contexts != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.s.Status().Contexts != 0 {
		t.Fatal("stalled context retained")
	}
	next := f.handoff(t, "http")
	defer next.Close()
	req, _ := http.NewRequest("POST", "http://api.service.example/after", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	if e = req.Write(next); e != nil {
		t.Fatal(e)
	}
	resp, e := http.ReadResponse(bufio.NewReader(next), req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("workspace retained %d %s", resp.StatusCode, b)
	}
}

func TestMalformedApplicationHeaderGate(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	for _, head := range []string{
		"POST / HTTP/1.1\r\nHost: api.service.example\r\nContent-Length: 0\nContent-Length: 0\r\n\r\n",
		"GET http://api.service.example/ HTTP/1.1\r\nHost: mirror.service.example\r\n\r\n",
		"GET http://user:placeholder@api.service.example/ HTTP/1.1\r\nHost: api.service.example\r\n\r\n",
		"GET https://api.service.example/ HTTP/1.1\r\nHost: api.service.example\r\n\r\n",
	} {
		conn := f.handoff(t, "http")
		conn.SetDeadline(time.Now().Add(time.Second))
		io.WriteString(conn, head)
		response, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
		conn.Close()
		if ne, ok := e.(net.Error); e == nil && response.StatusCode < 400 || ok && ne.Timeout() {
			t.Fatalf("malformed head not rejected %v %v %q", response, e, head)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opens) != 0 {
		t.Fatal("malformed request reached reinjection")
	}
}
func TestTruncatedUpstreamResponseCannotComplete(t *testing.T) {
	for _, framing := range []string{"Content-Length: 100\r\n\r\nshort", "Transfer-Encoding: chunked\r\n\r\na\r\nshort"} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				return
			}
			defer conn.Close()
			io.WriteString(conn, "HTTP/1.1 200 OK\r\n"+framing)
		}))
		f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"))
		conn := f.handoff(t, "http")
		io.WriteString(conn, "GET / HTTP/1.1\r\nHost: api.service.example\r\n\r\n")
		resp, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
		if e == nil {
			_, e = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		conn.Close()
		origin.Close()
		if e == nil {
			t.Fatal("truncated response completed successfully")
		}
	}
}
