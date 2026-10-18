// Source-only coordinator fixture: real engine control and mTLS channels, with
// an internal synthetic TUN identity. It never creates an OS TUN or routes.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
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
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
