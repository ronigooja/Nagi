package requestrewriteservice

import (
	"context"
	"crypto/tls"
	"github.com/elazarl/goproxy"
	"golang.org/x/net/http2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIndependentH2StreamCancellation(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/block" {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			return
		}
		w.Write([]byte("alive"))
	}))
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
	raw := f.handoff(t, "tls")
	tc := tls.Client(raw, &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"h2"}})
	if e = tc.Handshake(); e != nil {
		t.Fatal(e)
	}
	tr := &http2.Transport{}
	client, e := tr.NewClientConn(tc)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.service.example/block", nil)
	result := make(chan error, 1)
	go func() {
		resp, e := client.RoundTrip(req)
		if resp != nil {
			resp.Body.Close()
		}
		result <- e
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked request not entered")
	}
	cancel()
	select {
	case e = <-result:
		if e == nil {
			t.Fatal("canceled stream succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream cancellation not returned")
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("origin stream not canceled")
	}
	req, _ = http.NewRequest("GET", "https://api.service.example/alive", nil)
	resp, e := client.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(body) != "alive" {
		t.Fatalf("other stream failed: %v", e)
	}
}

func TestHTTP2JSONBodyDeadlineAndReservationRelease(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.Write([]byte("ok")) }))
	doc := []byte(`{"version":1,"enabled":false,"domains":[{"id":"api","pattern":"api.service.example"}],"rules":[{"id":"json","enabled":true,"match":{"domain":"api.service.example","methods":["POST"],"path_prefix":"/"},"on_error":"block","redirect_consent":[],"operations":[{"op":"json_set","pointer":"/edited","value":true}]}]}`)
	f := newFixture(t, origin.Listener.Addr().String(), doc)
	f.s.transformTimeout = 100 * time.Millisecond
	cfg, e := goproxy.TLSConfigFromCA(&f.s.ca)("api.service.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
	if e != nil {
		t.Fatal(e)
	}
	cfg.InsecureSkipVerify = false
	origin.TLS = cfg
	origin.StartTLS()
	defer origin.Close()
	f.s.originRoots = f.root
	raw := f.handoff(t, "tls")
	tc := tls.Client(raw, &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"h2"}})
	if e = tc.Handshake(); e != nil {
		t.Fatal(e)
	}
	tr := &http2.Transport{}
	client, e := tr.NewClientConn(tc)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	reader, writer := io.Pipe()
	defer writer.Close()
	req, _ := http.NewRequest("POST", "https://api.service.example/stall", reader)
	req.Header.Set("Content-Type", "application/json")
	go writer.Write([]byte("{"))
	result := make(chan *http.Response, 1)
	go func() { resp, _ := client.RoundTrip(req); result <- resp }()
	select {
	case resp := <-result:
		if resp == nil || resp.StatusCode != 502 {
			t.Fatalf("stall response %v", resp)
		}
		resp.Body.Close()
	case <-time.After(time.Second):
		t.Fatal("JSON stall deadline failed")
	}
	writer.Close()
	req, _ = http.NewRequest("POST", "https://api.service.example/after", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("reservation retained %d %s", resp.StatusCode, b)
	}
}
