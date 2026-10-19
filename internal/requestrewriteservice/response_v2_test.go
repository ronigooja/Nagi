package requestrewriteservice

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elazarl/goproxy"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
	"golang.org/x/net/http2"
)

func v2Document(t *testing.T, rules ...domain.Rule) []byte {
	t.Helper()
	doc := domain.Document{Version: 2, Rules: rules, Scripts: []domain.Script{}}
	if _, err := domain.Compile(doc); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func v2Rule(id, stage string, operations ...domain.Operation) domain.Rule {
	return domain.Rule{ID: id, Enabled: true, Stage: stage, Match: domain.Match{Domain: "api.service.example"}, OnError: "block", RangeMode: "fragment", RedirectConsent: []domain.Consent{}, Operations: operations}
}
func textValue(v string) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestResponseV2OriginalMatchingBodyAndTrailers(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "http1", true: "http2"}[h2], func(t *testing.T) {
			requestRule := v2Rule("request", domain.StageRequest, domain.Operation{Op: "url_set", Component: "path", Value: textValue("/changed")})
			responseRule := v2Rule("response", domain.StageResponse, domain.Operation{Op: "json_set", Pointer: "/edited", Value: json.RawMessage(`true`)}, domain.Operation{Op: "header_set", Name: "X-Response", Value: textValue("changed")}, domain.Operation{Op: "trailer_set", Name: "X-Trailer", Value: textValue("edited")})
			responseRule.Match.PathPrefix = "/original"
			responseRule.Match.StatusCodes = []int{201}
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/changed" {
					t.Errorf("upstream path %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Trailer", "X-Trailer")
				w.WriteHeader(201)
				io.WriteString(w, `{"original":true}`)
				w.Header().Set("X-Trailer", "original")
			}))
			f := newFixture(t, origin.Listener.Addr().String(), v2Document(t, requestRule, responseRule))
			scheme := "http"
			if h2 {
				cfg, err := goproxy.TLSConfigFromCA(&f.s.ca)("api.service.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
				if err != nil {
					t.Fatal(err)
				}
				cfg.InsecureSkipVerify = false
				origin.TLS = cfg
				origin.StartTLS()
				f.s.originRoots = f.root
				scheme = "https"
			} else {
				origin.Start()
			}
			defer origin.Close()
			req, _ := http.NewRequest("GET", scheme+"://api.service.example/original", nil)
			var resp *http.Response
			var err error
			if h2 {
				raw := f.handoff(t, "tls")
				tc := tls.Client(raw, &tls.Config{RootCAs: f.root, ServerName: "api.service.example", NextProtos: []string{"h2"}})
				if err = tc.Handshake(); err != nil {
					t.Fatal(err)
				}
				tr := &http2.Transport{}
				client, e := tr.NewClientConn(tc)
				if e != nil {
					t.Fatal(e)
				}
				defer client.Close()
				resp, err = client.RoundTrip(req)
			} else {
				conn := f.handoff(t, "http")
				defer conn.Close()
				if err = req.Write(conn); err != nil {
					t.Fatal(err)
				}
				resp, err = http.ReadResponse(bufio.NewReader(conn), req)
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(body) != `{"edited":true,"original":true}` || resp.Header.Get("X-Response") != "changed" || resp.Trailer.Get("X-Trailer") != "edited" {
				t.Fatalf("response status=%d body=%q header=%v trailer=%v err=%v", resp.StatusCode, body, resp.Header, resp.Trailer, err)
			}
			if f.s.Status().Counters.Responses != 1 {
				t.Fatal("response counter missing")
			}
		})
	}
}

func TestResponseV2LocalFailureBypassesRules(t *testing.T) {
	rule := v2Rule("response", domain.StageResponse, domain.Operation{Op: "header_set", Name: "X-Must-Not-Apply", Value: textValue("unsafe")})
	f := newFixture(t, "127.0.0.1:1", v2Document(t, rule))
	conn := f.handoff(t, "http")
	defer conn.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://api.service.example/", nil)
	if err := req.Write(conn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 502 || resp.Header.Get("X-Must-Not-Apply") != "" || !strings.Contains(string(body), "rewrite_unavailable") {
		t.Fatalf("local error entered rules: %d %q %v", resp.StatusCode, body, resp.Header)
	}
}

func TestResponseV2StreamingRequestTrailers(t *testing.T) {
	seen := make(chan string, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			t.Error(err)
		}
		seen <- r.Trailer.Get("X-End")
		io.WriteString(w, "ok")
	}))
	defer origin.Close()
	rule := v2Rule("trailers", domain.StageRequest, domain.Operation{Op: "trailer_set", Name: "X-End", Value: textValue("edited")})
	f := newFixture(t, strings.TrimPrefix(origin.URL, "http://"), v2Document(t, rule))
	conn := f.handoff(t, "http")
	defer conn.Close()
	req, _ := http.NewRequest("POST", "http://api.service.example/", strings.NewReader("payload"))
	req.ContentLength = -1
	req.Trailer = http.Header{"X-End": {"original"}}
	if err := req.Write(conn); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if got := <-seen; got != "edited" {
		t.Fatalf("upstream trailer %q", got)
	}
}
