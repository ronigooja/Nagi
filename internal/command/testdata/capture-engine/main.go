// Source-only coordinator fixture: real engine control and mTLS channels, with
// an internal synthetic TUN identity. It never creates an OS TUN or routes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/coder/websocket"
	"github.com/metacubex/mihomo/component/resolver"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/hub/route"
	"github.com/metacubex/mihomo/tunnel"
)

func main() {
	path := flag.String("f", "", "fixture config")
	home := flag.String("d", "", "fixture home")
	socket := flag.String("ext-ctl-unix", "", "private socket")
	check := flag.Bool("t", false, "validate")
	_ = flag.Bool("check-json", false, "JSON diagnostics")
	flag.Parse()
	C.SetHomeDir(*home)
	C.SetConfig(*path)
	cfg, e := executor.ParseWithPath(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, "fixture validation failed")
		os.Exit(1)
	}
	if *check {
		fmt.Println(`{"valid":true}`)
		return
	}
	executor.ApplyConfig(cfg, true)
	tunnel.Tunnel.RewriteTUNStarted("coordinator-fixture")
	defer tunnel.Tunnel.RewriteTUNStopped("coordinator-fixture")
	native := *socket + "n"
	route.ReCreateServer(&route.Config{UnixAddr: native})
	for i := 0; i < 300; i++ {
		if _, e := os.Stat(native); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	listener, e := net.Listen("unix", *socket)
	if e != nil {
		panic(e)
	}
	defer listener.Close()
	_ = os.Chmod(*socket, 0600)
	target, _ := url.Parse("http://engine.fixture")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", native)
	}}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { w.WriteHeader(503) }
	homeDir, _ := os.UserHomeDir()
	resolver.DisableSystemHosts = true
	resolver.UseSystemHosts = false
	_ = resolver.DefaultHosts.Insert("api.fixture.example", resolver.HostValue{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			kind, data, err := conn.Read(r.Context())
			if err == nil {
				_ = conn.Write(r.Context(), kind, data)
			}
			return
		}
		data, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Observed-Rewrite", r.Header.Get("X-Rewritten"))
		_, _ = w.Write(append([]byte("original-response:"), data...))
	}))
	defer origin.Close()
	_, portString, _ := net.SplitHostPort(origin.Listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		app, inbound := net.Pipe()
		go tunnel.Tunnel.HandleTCPConn(inbound, &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "api.fixture.example", DstPort: uint16(port), SrcIP: netip.MustParseAddr("127.0.0.1"), SrcPort: 12345, InName: "coordinator-fixture", Uid: uint32(os.Getuid()), Process: "fixture-app", ProcessPath: "/Applications/Fixture.app/Contents/MacOS/Fixture"})
		_ = app.SetDeadline(time.Now().Add(10 * time.Second))
		return app, nil
	}}
	defer transport.CloseIdleConnections()
	appClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/request-rewrite/capabilities" {
			if _, err := os.Stat(filepath.Join(homeDir, "old-capabilities")); err == nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"versions":[2],"tun_only":true,"original_context":true,"reinjection":true,"quic_domain_block":true,"max_contexts":1024,"max_streams_per_context":64,"context_idle_seconds":60,"context_max_seconds":3600}`))
				return
			}
		}
		if r.URL.Path == "/fixture/traffic" {
			if r.URL.Query().Get("kind") == "websocket" {
				conn, _, err := websocket.Dial(r.Context(), "ws://api.fixture.example:"+portString+"/ws", &websocket.DialOptions{HTTPClient: appClient})
				if err != nil {
					http.Error(w, "fixture websocket dial failed", 500)
					return
				}
				defer conn.CloseNow()
				err = conn.Write(r.Context(), websocket.MessageText, []byte("original-message"))
				_, data, err2 := conn.Read(r.Context())
				if err != nil || err2 != nil {
					http.Error(w, "fixture websocket failed", 500)
					return
				}
				_ = conn.Close(websocket.StatusNormalClosure, "fixture")
				_ = json.NewEncoder(w).Encode(map[string]string{"body": string(data)})
				return
			}
			req, _ := http.NewRequestWithContext(r.Context(), "POST", "http://api.fixture.example:"+portString+"/original?credential=fictitious-token", strings.NewReader("original-body"))
			req.Header.Set("Authorization", "Bearer fictitious-token")
			resp, err := appClient.Do(req)
			if err != nil {
				http.Error(w, "fixture HTTP failed", 500)
				return
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				http.Error(w, "fixture body failed", 500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"body": string(data), "rewrite": resp.Header.Get("X-Observed-Rewrite"), "status": resp.StatusCode})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/request-rewrite/probe" {
			marker := filepath.Join(homeDir, "fail-next-probe")
			if _, e := os.Stat(marker); e == nil {
				_ = os.Remove(marker)
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"code":"rewrite_unavailable","message":"fixture rejection"}}`))
				return
			}
		}
		proxy.ServeHTTP(w, r)
	})}
	go server.Serve(listener)
	defer server.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	<-signals
}
