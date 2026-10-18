package requestrewriteservice

import (
	"bytes"
	"crypto/tls"
	"github.com/elazarl/goproxy"
	"golang.org/x/net/http2"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestHTTP2ConcurrentFlowAndAuthority(t *testing.T) {
	payload := bytes.Repeat([]byte("f"), 1<<20)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil || !bytes.Equal(body, payload) {
			t.Error("body corrupted")
		}
		w.Write(payload)
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
	tc := tls.Client(f.handoff(t, "tls"), &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"h2"}})
	if e = tc.Handshake(); e != nil {
		t.Fatal(e)
	}
	tr := &http2.Transport{}
	client, e := tr.NewClientConn(tc)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", "https://api.service.example/concurrent", bytes.NewReader(payload))
			resp, e := client.RoundTrip(req)
			if e != nil {
				t.Error(e)
				return
			}
			b, e := io.ReadAll(resp.Body)
			resp.Body.Close()
			if e != nil || resp.StatusCode != 200 || !bytes.Equal(b, payload) {
				t.Errorf("flow failed status=%d err=%v", resp.StatusCode, e)
			}
		}()
	}
	req, _ := http.NewRequest("GET", "https://unauthorized.example/", nil)
	resp, e := client.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Errorf("unauthorized authority status=%d", resp.StatusCode)
	}
	wg.Wait()
}
