package requestrewriteservice

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/elazarl/goproxy"
	"github.com/ronigooja/Nagi/internal/capture"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
	"golang.org/x/net/http2"
)

type captureFixture struct {
	*fixture
	socket string
	policy capture.Policy
}

func enableCapture(t *testing.T, f *fixture, limit int) *captureFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "capture.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- capture.Serve(ctx, socket, os.Getuid()) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	var reply capture.Reply
	var err error
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		reply, err = capture.Call(ctx, socket, capture.Request{Action: "start", Start: &capture.StartOptions{Profile: "test", Backend: "user", Owner: "cli", BodyLimit: limit}})
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	p := capture.Policy{SessionID: reply.Status.SessionID, Revision: reply.Status.Revision, State: "recording", Scope: reply.Status.Scope}
	f.s.mu.Lock()
	f.s.capturePolicy = &p
	f.s.captureSink = capture.NewSink(socket, p.SessionID)
	f.s.config.BodyLimit = limit
	f.s.mu.Unlock()
	return &captureFixture{fixture: f, socket: socket, policy: p}
}
func (f *captureFixture) handoff(t *testing.T, protocol string) net.Conn {
	t.Helper()
	raw, err := net.Dial("tcp4", f.s.Announcement().HandoffAddr)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	peer, _ := x509.ParseCertificate(f.s.cert.Certificate[0])
	roots.AddCert(peer)
	conn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{f.engine}, RootCAs: roots, ServerName: childRole})
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err = conn.Handshake(); err != nil {
		t.Fatal(err)
	}
	tok, _ := randomValue(32)
	identity := capture.Identity{SessionID: f.policy.SessionID, Revision: f.policy.Revision, UID: uint32(os.Getuid()), Process: "Example", ProcessPath: "/Applications/Example.app/Contents/MacOS/Example", Source: "192.0.2.1:4321", Destination: "198.51.100.2:80"}
	b, _ := json.Marshal(identity)
	port := "80"
	if protocol == "tls" {
		port = "443"
	}
	fmt.Fprintf(conn, "CONNECT api.service.example:%s HTTP/1.1\r\nHost: api.service.example:%s\r\nAuthorization: Bearer %s\r\nNagi-Rewrite-Version: 2\r\nNagi-Rewrite-Generation: %s\r\nNagi-Rewrite-Epoch: %s\r\nNagi-Rewrite-Context: %s\r\nNagi-Rewrite-Protocol: %s\r\nNagi-Capture-Metadata: %s\r\n\r\n", port, port, f.config.HandoffSecret, f.config.Generation, f.config.EngineEpoch, tok, protocol, base64.RawURLEncoding.EncodeToString(b))
	r := bufio.NewReader(conn)
	if err = readOK(r); err != nil {
		t.Fatal(err)
	}
	return &bufferedConn{conn, r}
}
func (f *captureFixture) call(t *testing.T, q capture.Request) capture.Reply {
	t.Helper()
	q.SessionID = f.policy.SessionID
	r, e := capture.Call(context.Background(), f.socket, q)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *captureFixture) records(t *testing.T, n int) []capture.Record {
	t.Helper()
	var records []capture.Record
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		records = f.call(t, capture.Request{Action: "list"}).Records
		if len(records) >= n {
			ready := true
			for _, r := range records {
				if r.Outcome == "" {
					ready = false
				}
				if r.Kind == "http" && (len(r.Snapshots) < 4 || r.Outcome == "") {
					ready = false
				}
				for _, s := range r.Snapshots {
					if s.BodyState == "collecting" {
						ready = false
					}
				}
			}
			if ready {
				return records
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("capture not complete: %+v", records)
	return nil
}
func (f *captureFixture) body(t *testing.T, r capture.Record, name string) []byte {
	return f.call(t, capture.Request{Action: "body", RecordID: r.ID, Snapshot: name}).Body.Data
}

func TestCaptureHTTPOriginalActualAndTrailers(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "edited request" {
			t.Errorf("actual request %q", body)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Trailer", "X-Origin-Trailer")
		w.Header().Add("Set-Cookie", "a=secret")
		w.Header().Add("Set-Cookie", "b=secret")
		w.Write([]byte("original response"))
		w.Header().Set("X-Origin-Trailer", "secret trailer")
	}))
	defer origin.Close()
	request := v2Rule("req", domain.StageRequest, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("edited request")})
	response := v2Rule("resp", domain.StageResponse, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("edited response")})
	f := enableCapture(t, newFixture(t, strings.TrimPrefix(origin.URL, "http://"), v2Document(t, request, response)), 4096)
	conn := f.handoff(t, "http")
	defer conn.Close()
	req, _ := http.NewRequest("POST", "http://api.service.example/path?token=secret", strings.NewReader("original request"))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer original-secret")
	req.Write(conn)
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != "edited response" {
		t.Fatalf("response %q %v counters %+v records %+v", body, err, f.s.Status(), f.call(t, capture.Request{Action: "list"}).Records)
	}
	records := f.records(t, 1)
	r := records[0]
	for name, want := range map[string]string{"request_original": "original request", "request_actual": "edited request", "response_original": "original response", "response_actual": "edited response"} {
		if got := string(f.body(t, r, name)); got != want {
			t.Errorf("%s %q != %q", name, got, want)
		}
		if s := r.Snapshots[name]; s.BodyState != "complete" || s.BodySize != s.Captured {
			t.Errorf("%s state %+v", name, s)
		}
	}
	if r.Snapshots["request_original"].Query != "token=secret" || r.Snapshots["request_original"].Headers["Authorization"][0] != "Bearer original-secret" || len(r.Snapshots["response_original"].Headers["Set-Cookie"]) != 2 || r.Snapshots["response_original"].Trailers["X-Origin-Trailer"][0] != "secret trailer" {
		t.Fatal("raw metadata lost", r)
	}
	if strings.Join(r.RuleIDs, ",") != "req,resp" {
		t.Fatalf("rule ids %v", r.RuleIDs)
	}
}
func TestCaptureOnlyStreamsCapsAndGzip(t *testing.T) {
	var encoded bytes.Buffer
	z := gzip.NewWriter(&encoded)
	z.Write(bytes.Repeat([]byte("content"), 1000))
	z.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Trailer", "X-End")
		w.Write(encoded.Bytes())
		w.Header().Set("X-End", "done")
	}))
	defer origin.Close()
	f := enableCapture(t, newFixture(t, strings.TrimPrefix(origin.URL, "http://"), []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), 16)
	conn := f.handoff(t, "http")
	defer conn.Close()
	req, _ := http.NewRequest("POST", "http://api.service.example/p", strings.NewReader(strings.Repeat("z", 100000)))
	req.Write(conn)
	resp, e := http.ReadResponse(bufio.NewReader(conn), req)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || !bytes.Equal(got, encoded.Bytes()) {
		t.Fatal("forward changed", e)
	}
	r := f.records(t, 1)[0]
	for _, name := range []string{"request_original", "request_actual", "response_original", "response_actual"} {
		s := r.Snapshots[name]
		if s.BodyState != "truncated" || s.Captured != 16 {
			t.Fatalf("%s %+v all %+v counters %+v", name, s, r, f.s.Status())
		}
	}
	if !bytes.Equal(f.body(t, r, "response_original"), encoded.Bytes()[:16]) {
		t.Fatal("encoded original changed")
	}
	// Authenticated capture does not authorize an arbitrary application authority.
	conn2 := f.handoff(t, "http")
	defer conn2.Close()
	io.WriteString(conn2, "GET / HTTP/1.1\r\nHost: arbitrary.example\r\n\r\n")
	denied, e := http.ReadResponse(bufio.NewReader(conn2), nil)
	if e != nil || denied.StatusCode != 502 {
		t.Fatalf("arbitrary target accepted %v", e)
	}
}
func TestCaptureHTTPSHTTP2ConcurrentStreams(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trailer", "X-Trailer")
		w.Write([]byte(r.URL.Path))
		w.Header().Set("X-Trailer", "value")
	}))
	f := enableCapture(t, newFixture(t, origin.Listener.Addr().String(), []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), 4096)
	cfg, e := goproxy.TLSConfigFromCA(&f.s.ca)("api.service.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
	if e != nil {
		t.Fatal(e)
	}
	origin.TLS = cfg
	origin.StartTLS()
	defer origin.Close()
	f.s.originRoots = f.root
	tc := tls.Client(f.handoff(t, "tls"), &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"h2"}})
	if e = tc.Handshake(); e != nil {
		t.Fatal(e)
	}
	client, e := (&http2.Transport{}).NewClientConn(tc)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.service.example/%d", i), nil)
			resp, e := client.RoundTrip(req)
			if e != nil {
				t.Error(e)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}(i)
	}
	wg.Wait()
	for _, r := range f.records(t, 4) {
		if r.Snapshots["request_original"].Scheme != "https" || r.Snapshots["response_actual"].Trailers["X-Trailer"][0] != "value" {
			t.Error("TLS stream capture missing", r)
		}
	}
}
func TestCaptureWebSocketBothDirectionsOriginalActual(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		for {
			kind, b, e := c.Read(r.Context())
			if e != nil {
				return
			}
			if e = c.Write(r.Context(), kind, b); e != nil {
				return
			}
		}
	}))
	defer origin.Close()
	clientRule := v2Rule("client", domain.StageWebSocketClient, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("actual-client")})
	serverRule := v2Rule("server", domain.StageWebSocketServer, domain.Operation{Op: "body_set", Encoding: "text", Value: textValue("actual-server")})
	f := enableCapture(t, newFixture(t, strings.TrimPrefix(origin.URL, "http://"), v2Document(t, clientRule, serverRule)), 4096)
	tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return f.handoff(t, "http"), nil }}
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, "ws://api.service.example/ws", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	for _, kind := range []websocket.MessageType{websocket.MessageText, websocket.MessageBinary} {
		if e = c.Write(ctx, kind, []byte("original-client")); e != nil {
			t.Fatal(e)
		}
		_, b, e := c.Read(ctx)
		if e != nil || string(b) != "actual-server" {
			t.Fatalf("forward %q %v", b, e)
		}
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	records := f.records(t, 5)
	messages := 0
	for _, r := range records {
		if r.Kind != "websocket" {
			continue
		}
		messages++
		wantOriginal, wantActual := "original-client", "actual-client"
		if r.Stage == domain.StageWebSocketServer {
			wantOriginal, wantActual = "actual-client", "actual-server"
		}
		if string(f.body(t, r, "message_original")) != wantOriginal || string(f.body(t, r, "message_actual")) != wantActual || r.ParentID == "" {
			t.Errorf("message mismatch %+v", r)
		}
	}
	if messages != 4 {
		t.Fatalf("messages %d", messages)
	}
}

func TestCapturePauseResumeExistingConnection(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer origin.Close()
	f := enableCapture(t, newFixture(t, strings.TrimPrefix(origin.URL, "http://"), []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), 4096)
	conn := f.handoff(t, "http")
	defer conn.Close()
	reader := bufio.NewReader(conn)
	send := func() {
		req, _ := http.NewRequest("GET", "http://api.service.example/", nil)
		if e := req.Write(conn); e != nil {
			t.Fatal(e)
		}
		resp, e := http.ReadResponse(reader, req)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("paused forwarding status %d", resp.StatusCode)
		}
	}
	send()
	f.records(t, 1)
	paused := f.call(t, capture.Request{Action: "pause"}).Status
	p := f.policy
	p.Revision = paused.Revision
	p.State = "paused"
	if e := f.s.SetCapture(p); e != nil {
		t.Fatal(e)
	}
	send()
	if got := len(f.call(t, capture.Request{Action: "list"}).Records); got != 1 {
		t.Fatalf("paused recorded %d", got)
	}
	resumed := f.call(t, capture.Request{Action: "resume"}).Status
	p.Revision = resumed.Revision
	p.State = "recording"
	if e := f.s.SetCapture(p); e != nil {
		t.Fatal(e)
	}
	send()
	records := f.records(t, 2)
	found := false
	for _, r := range records {
		if r.Identity.Revision == resumed.Revision {
			found = true
		}
	}
	if !found {
		t.Fatal("resumed record kept stale identity revision")
	}
}

func TestCaptureMetadataRequiresNegotiationAndStrictIdentity(t *testing.T) {
	f := newFixture(t, "127.0.0.1:1")
	encoded := func(v string) string { return base64.RawURLEncoding.EncodeToString([]byte(v)) }
	if _, e := f.s.captureIdentity(encoded(`{"session_id":"s","revision":1,"uid":1,"process":"x","process_path":"/x","source":"x","destination":"y"}`)); e == nil {
		t.Fatal("unnegotiated metadata accepted")
	}
	c := enableCapture(t, f, 4096)
	valid := fmt.Sprintf(`{"session_id":%q,"revision":1,"uid":1,"process":"x","process_path":"/x","source":"x","destination":"y"}`, c.policy.SessionID)
	if _, e := f.s.captureIdentity(encoded(valid)); e != nil {
		t.Fatal(e)
	}
	for _, input := range []string{strings.Replace(valid, `"uid":1`, `"uid":1,"uid":2`, 1), strings.Replace(valid, `"uid":1,`, "", 1), strings.Replace(valid, `"revision":1`, `"revision":100`, 1), valid + " {}"} {
		if _, e := f.s.captureIdentity(encoded(input)); e == nil {
			t.Fatal("malformed identity accepted")
		}
	}
}

func TestCaptureWebSocketUneditedLargeMessageStreams(t *testing.T) {
	const size = domain.MaxBodyBytes + 1024
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		writer, e := c.Writer(r.Context(), websocket.MessageBinary)
		if e != nil {
			return
		}
		_, e = io.CopyN(writer, strings.NewReader(strings.Repeat("x", size)), size)
		if e == nil {
			e = writer.Close()
		}
		if e == nil {
			_, _, _ = c.Read(r.Context())
		}
	}))
	defer origin.Close()
	f := enableCapture(t, newFixture(t, strings.TrimPrefix(origin.URL, "http://"), []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), 16)
	tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return f.handoff(t, "http"), nil }}
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, "ws://api.service.example/ws", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	c.SetReadLimit(-1)
	kind, reader, e := c.Reader(ctx)
	if e != nil {
		t.Fatal(e)
	}
	n, e := io.Copy(io.Discard, reader)
	if e != nil || kind != websocket.MessageBinary || n != size {
		t.Fatalf("large forward %d %v", n, e)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	for _, r := range f.records(t, 2) {
		if r.Kind != "websocket" {
			continue
		}
		for _, s := range r.Snapshots {
			if s.BodyState != "truncated" || s.Captured != 16 || s.BodySize != size {
				t.Fatalf("large capture %+v", s)
			}
		}
	}
}

type captureReadFailure struct{ io.Reader }

func (r captureReadFailure) Read(p []byte) (int, error) {
	n, _ := r.Reader.Read(p)
	return n, io.ErrUnexpectedEOF
}

func TestCaptureFixedLengthCloseWithoutEOF(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected int64
		read     int
		failed   bool
		want     string
	}{
		{name: "exact", expected: 4, read: 4, want: "complete"},
		{name: "short", expected: 5, read: 4, want: "cutoff"},
		{name: "unknown", expected: -1, read: 4, want: "cutoff"},
		{name: "failed", expected: 4, read: 4, failed: true, want: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := enableCapture(t, newFixture(t, "127.0.0.1:1", []byte(`{"version":2,"enabled":false,"rules":[],"scripts":[]}`)), 4096)
			req, _ := http.NewRequest("POST", "http://api.service.example/path", nil)
			cc := &connectionContext{identity: &capture.Identity{SessionID: f.policy.SessionID, Revision: 1}}
			record := f.s.beginCapture(cc, req, "http", "", "", 0)
			var source io.Reader = strings.NewReader("body")
			if tc.failed {
				source = captureReadFailure{source}
			}
			body := record.body("request_original", io.NopCloser(source), tc.expected, func() http.Header { return nil })
			buffer := make([]byte, tc.read)
			_, _ = io.ReadFull(body, buffer)
			_ = body.Close()
			// Closing the sink drains asynchronous events before inspecting state.
			f.s.captureSink.Close()
			got := f.call(t, capture.Request{Action: "show", RecordID: record.record.ID}).Record.Snapshots["request_original"]
			if got.BodyState != tc.want || got.Captured != int64(tc.read) {
				t.Fatalf("close state: %+v; want %s", got, tc.want)
			}
		})
	}
}
