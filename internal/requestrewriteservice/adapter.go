package requestrewriteservice

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"golang.org/x/net/http2"
)

type certCache struct {
	mu      sync.Mutex
	entries map[string]*tls.Certificate
	order   []string
}

func (c *certCache) Fetch(host string, gen func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v := c.entries[host]; v != nil {
		return v, nil
	}
	v, e := gen()
	if e != nil {
		return nil, errUnavailable
	}
	if len(c.entries) >= 256 {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	c.entries[host] = v
	c.order = append(c.order, host)
	return v, nil
}
func fixedResponse(r *http.Request, code string) *http.Response {
	return &http.Response{StatusCode: 502, Status: "502 Bad Gateway", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader(code + "\n")), ContentLength: int64(len(code) + 1), Request: r}
}
func (s *Service) application(cc *connectionContext, wire net.Conn, authority, protocol string) {
	defer wire.Close()
	wire.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(wire)
	first, e := r.Peek(1)
	if e != nil || ((first[0] == 22) != (protocol == "tls")) {
		return
	}
	conn := net.Conn(&bufferedConn{wire, r})
	proxy := goproxy.NewProxyHttpServer()
	proxy.Logger = log.New(io.Discard, "", 0)
	proxy.Verbose = false
	proxy.KeepAcceptEncoding = true
	proxy.AllowHTTP2 = false
	denied := func(context.Context, string, string) (net.Conn, error) { return nil, errAuth }
	proxy.Tr = &http.Transport{Proxy: nil, DialContext: denied, DialTLSContext: denied, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableCompression: true}
	proxy.ConnectDial = func(string, string) (net.Conn, error) { return nil, errAuth }
	proxy.ConnectDialWithReq = func(*http.Request, string, string) (net.Conn, error) { return nil, errAuth }
	proxy.ConnectionErrHandler = func(w io.Writer, _ *goproxy.ProxyCtx, _ error) { wireError(w, "rewrite_unavailable") }
	proxy.CertStore = s.leaves
	if protocol == "tls" {
		host, _, _ := net.SplitHostPort(authority)
		leaf, e := s.leaves.Fetch(host, func() (*tls.Certificate, error) {
			cfg, e := goproxy.TLSConfigFromCA(&s.ca)(authority, &goproxy.ProxyCtx{Proxy: proxy})
			if e != nil {
				return nil, e
			}
			return &cfg.Certificates[0], nil
		})
		if e != nil {
			return
		}
		cfg := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{*leaf}, NextProtos: []string{"h2", "http/1.1"}}
		cfg.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			if hello.ServerName != "" && !strings.EqualFold(hello.ServerName, host) {
				return nil, errAuth
			}
			return nil, nil
		}
		tc := tls.Server(conn, cfg)
		hctx, cancel := context.WithTimeout(cc.ctx, 5*time.Second)
		e = tc.HandshakeContext(hctx)
		cancel()
		if e != nil {
			return
		}
		conn = tc
	}
	conn.SetDeadline(time.Time{})
	proxy.OnRequest().DoFunc(func(req *http.Request, pctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if req.Method == http.MethodConnect || req.ProtoMajor > 1 && req.Header.Get("Upgrade") != "" {
			return req, fixedResponse(req, "rewrite_invalid")
		}
		cap := s.beginCapture(cc, req, "http", "", "", 0)
		req = req.WithContext(context.WithValue(req.Context(), captureKey{}, cap))
		state := &exchangeState{original: req.Clone(req.Context()), capture: cap}
		if cap != nil {
			req.Body = cap.body("request_original", req.Body, func() http.Header { return req.Trailer })
		}
		stripHeaders(req.Header)
		pctx.UserData = state
		result, e := s.transformRequest(req, wire, req.Header.Get("Upgrade") != "")
		if e != nil {
			state.localFailure = true
			if cap != nil {
				cap.complete("blocked", "request_failed", 0)
			}
			return req, fixedResponse(req, transformCode(e))
		}
		out := result.Request.WithContext(req.Context())
		if cap != nil {
			cap.requestActual(out, result.RuleIDs, result.Outcome)
			out.Body = cap.body("request_actual", out.Body, func() http.Header { return out.Trailer })
		}
		pctx.RoundTripper = goproxy.RoundTripperFunc(func(r *http.Request, _ *goproxy.ProxyCtx) (*http.Response, error) {
			if r.Body != nil {
				defer r.Body.Close()
			}
			response, e := cc.roundTrip(r)
			if e != nil {
				state.localFailure = true
				return fixedResponse(r, "rewrite_unavailable"), nil
			}
			return response, nil
		})
		return out, nil
	})
	proxy.OnResponse().DoFunc(func(resp *http.Response, pctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil {
			return fixedResponse(pctx.Req, "rewrite_unavailable")
		}
		if resp.StatusCode == 101 || resp.Header.Get("Upgrade") != "" {
			valid := pctx.Req.ProtoMajor == 1 && strings.EqualFold(pctx.Req.Header.Get("Upgrade"), "websocket") && headerToken(pctx.Req.Header, "Connection", "upgrade") && resp.StatusCode == 101 && strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") && headerToken(resp.Header, "Connection", "upgrade")
			if !valid {
				if resp.Body != nil {
					resp.Body.Close()
				}
				return fixedResponse(pctx.Req, "rewrite_invalid")
			}
		}
		state, ok := pctx.UserData.(*exchangeState)
		if !ok || state.localFailure {
			return resp
		}
		if state.capture != nil {
			state.capture.response("response_original", resp)
			originalResponse := resp
			resp.Body = state.capture.body("response_original", resp.Body, func() http.Header { return originalResponse.Trailer })
		}
		originalHandshake := *resp
		originalHandshake.Header = resp.Header.Clone()
		originalHandshake.Body = http.NoBody
		transformed, err := s.transformResponse(state.original, resp, wire)
		if err != nil {
			state.localFailure = true
			return fixedResponse(pctx.Req, transformCode(err))
		}
		resp = transformed
		if resp.StatusCode == 101 {
			resp, err = s.snapshot.TransformHandshake(state.original, &originalHandshake, resp)
			if err != nil {
				if transformed.Body != nil {
					transformed.Body.Close()
				}
				return fixedResponse(pctx.Req, transformCode(err))
			}
		}
		if state.capture != nil {
			state.capture.response("response_actual", resp)
			resp.Body = state.capture.body("response_actual", resp.Body, func() http.Header { return resp.Trailer })
			state.capture.complete("complete", "", 0)
		}
		stripHeaders(resp.Header)
		if state, ok := pctx.Req.Context().Value(bodyStateKey{}).(*bodyState); ok && resp.StatusCode != 101 {
			resp.Body = &checkedBody{ReadCloser: resp.Body, state: state}
		}

		return resp
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(context.WithValue(req.Context(), captureConnectionKey{}, cc))
		if req.Method == http.MethodConnect {
			http.Error(w, "rewrite_invalid", 502)
			return
		}
		scheme := "http"
		if protocol == "tls" {
			scheme = "https"
		}
		if req.URL.IsAbs() && req.URL.Scheme != scheme {
			http.Error(w, "rewrite_invalid", 502)
			return
		}
		req.URL.Scheme = scheme
		if req.URL.Host == "" {
			req.URL.Host = req.Host
		}
		if req.Host == "" || !cc.authorizedRequest(req, scheme) {
			http.Error(w, "rewrite_auth", 502)
			return
		}
		upgrade := req.Header.Get("Upgrade")
		if upgrade != "" && (req.ProtoMajor != 1 || !strings.EqualFold(upgrade, "websocket") || !headerToken(req.Header, "Connection", "upgrade")) {
			http.Error(w, "rewrite_invalid", 502)
			return
		}
		if upgrade != "" {
			s.serveWebSocket(w, req, cc, wire)
			return
		}
		state := &bodyState{}
		req = req.WithContext(context.WithValue(req.Context(), bodyStateKey{}, state))
		proxy.ServeHTTP(flushingWriter{w}, req)
		if state.failed {
			panic(http.ErrAbortHandler)
		}
	})
	if tc, ok := conn.(*tls.Conn); ok && tc.ConnectionState().NegotiatedProtocol == "h2" {
		h2 := http2.Server{MaxConcurrentStreams: 64, MaxReadFrameSize: 64 << 10, IdleTimeout: time.Minute}
		h2.ServeConn(conn, &http2.ServeConnOpts{Context: cc.ctx, Handler: handler, BaseConfig: &http.Server{MaxHeaderBytes: 64 << 10, ErrorLog: log.New(io.Discard, "", 0)}})
		return
	}
	scheme := "http"
	if protocol == "tls" {
		scheme = "https"
	}
	serveHTTP1(cc.ctx, &idleConn{Conn: conn}, handler, scheme)
}
func headerToken(h http.Header, key, value string) bool {
	for _, v := range h.Values(key) {
		for _, x := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(x), value) {
				return true
			}
		}
	}
	return false
}

type flushingWriter struct{ http.ResponseWriter }

func (w flushingWriter) Write(b []byte) (int, error) {
	n, e := w.ResponseWriter.Write(b)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	return n, e
}
func (w flushingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w flushingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errInvalid
	}
	return h.Hijack()
}
func (w flushingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (c *connectionContext) roundTrip(req *http.Request) (*http.Response, error) {
	target := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return nil, errInvalid
	}
	target = net.JoinHostPort(target, port)
	t := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: req.URL.Hostname(), RootCAs: c.service.originRoots, NextProtos: []string{"http/1.1"}}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: time.Minute, MaxResponseHeaderBytes: 64 << 10, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != target {
			return nil, errAuth
		}
		return c.dial(ctx, target, false)
	}}
	if req.Header.Get("Upgrade") != "" {
		t.ResponseHeaderTimeout = 5 * time.Second
	}
	defer t.CloseIdleConnections()
	if c.ctx.Err() != nil {
		return nil, errUnavailable
	}
	resp, e := t.RoundTrip(req)
	if e != nil {
		return nil, errUnavailable
	}
	return resp, nil
}

type bodyStateKey struct{}
type bodyState struct{ failed bool }
type checkedBody struct {
	io.ReadCloser
	state *bodyState
}

func (b *checkedBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e != nil && e != io.EOF {
		b.state.failed = true
	}
	return n, e
}

type idleConn struct{ net.Conn }

func (c *idleConn) Read(p []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(time.Minute))
	return c.Conn.Read(p)
}
func (c *idleConn) Write(p []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(time.Minute))
	return c.Conn.Write(p)
}
