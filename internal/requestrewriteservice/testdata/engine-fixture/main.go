// Opt-in real-engine fixture. It never creates an operating-system TUN.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/elazarl/goproxy"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	R "github.com/metacubex/mihomo/rules"
	"github.com/metacubex/mihomo/tunnel"
	D "github.com/ronigooja/Nagi/internal/requestrewrite"
	S "github.com/ronigooja/Nagi/internal/requestrewriteservice"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func secret() string {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	must(e)
	return base64.RawURLEncoding.EncodeToString(b)
}
func main() {
	dir, e := os.MkdirTemp("", "nagi-engine-fixture-")
	must(e)
	defer os.RemoveAll(dir)
	store := D.CAStore{ConfigDir: dir}
	_, e = store.Generate(time.Now())
	must(e)
	cert, key, e := store.LoadForChild(time.Now())
	must(e)
	ca := tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert}
	roots := dir + "/roots.pem"
	must(os.WriteFile(roots, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600))
	os.Setenv("SSL_CERT_FILE", roots)
	os.Setenv("SSL_CERT_DIR", dir)
	resolver.DisableSystemHosts = true
	resolver.UseSystemHosts = false
	for _, h := range []string{"api.service.example", "mirror.service.example"} {
		must(resolver.DefaultHosts.Insert(h, resolver.HostValue{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}))
	}
	direct := adapter.NewProxy(outbound.NewDirect())
	tunnel.UpdateProxies(map[string]C.Proxy{"DIRECT": direct, "GLOBAL": direct}, nil)
	rule, e := R.ParseRule("MATCH", "", "DIRECT", nil, nil)
	must(e)
	tunnel.UpdateRules([]C.Rule{rule}, nil, nil)
	tunnel.OnRunning()
	tunnel.Tunnel.RewriteTUNStarted("fixture")
	defer tunnel.Tunnel.RewriteTUNStopped("fixture")
	for _, scheme := range []string{"http", "https"} {
		for _, mode := range []tunnel.TunnelMode{tunnel.Rule, tunnel.Global, tunnel.Direct} {
			run(scheme, mode, ca)
		}
	}
	fmt.Println("actual-engine HTTP/TLS Rule/Global/Direct roundtrips passed")
}
func run(scheme string, mode tunnel.TunnelMode, ca tls.Certificate) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "mirror.service.example:") || r.Header.Get("X-Rewritten") != "yes" {
			panic("target/header not rewritten")
		}
		if r.URL.Path != "/changed" || r.URL.RawQuery != "case=fixture" {
			panic("URL not rewritten")
		}
		if r.TLS != nil && r.TLS.ServerName != "mirror.service.example" {
			panic("SNI not rewritten")
		}
		gz, e := gzip.NewReader(r.Body)
		must(e)
		defer gz.Close()
		dec := json.NewDecoder(gz)
		dec.UseNumber()
		var value map[string]any
		must(dec.Decode(&value))
		if value["edited"] != true || value["number"] != json.Number("9007199254740993") {
			panic("JSON/gzip precision failed")
		}

		io.WriteString(w, "actual-engine-ok")
	}))
	if scheme == "https" {
		cfg, e := goproxy.TLSConfigFromCA(&ca)("mirror.service.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
		must(e)
		cfg.InsecureSkipVerify = false
		origin.TLS = cfg
		origin.StartTLS()
	} else {
		origin.Start()
	}
	defer origin.Close()
	_, port, e := net.SplitHostPort(origin.Listener.Addr().String())
	must(e)
	p, e := strconv.Atoi(port)
	must(e)
	doc := fmt.Sprintf(`{"version":1,"enabled":false,"domains":[{"id":"api","pattern":"api.service.example"},{"id":"mirror","pattern":"mirror.service.example"}],"rules":[{"id":"edit","enabled":true,"match":{"domain":"api.service.example","methods":["POST"],"path_prefix":"/"},"on_error":"block","redirect_consent":[{"domain":"mirror.service.example","schemes":["%s"],"ports":[%d]}],"operations":[{"op":"header_set","name":"X-Rewritten","value":"yes"},{"op":"url_set","component":"host","value":"mirror.service.example"},{"op":"url_set","component":"path","value":"/changed"},{"op":"url_set","component":"query","value":"case=fixture"},{"op":"json_set","pointer":"/edited","value":true}]}]}`, scheme, p)
	snapshot, e := D.Parse([]byte(doc))
	must(e)
	service, e := S.New(snapshot, ca)
	must(e)
	defer service.Close()
	gen, e := S.NewGeneration()
	must(e)
	handoff, reinjection := secret(), secret()
	a := service.Announcement()
	prepared, e := tunnel.RewritePrepareSession(tunnel.RewritePrepare{Version: 1, Generation: gen, OwnerUID: uint32(os.Getuid()), HandoffAddr: a.HandoffAddr, ChildTransportCert: a.ChildTransportCert, HandoffSecret: handoff, ReinjectionSecret: reinjection, Domains: []string{"api.service.example", "mirror.service.example"}, RuleVersion: snapshot.Version(), BlockAuthorizedQUIC: true}, uint32(os.Getuid()))
	must(e)
	must(service.Configure(S.Configuration{Generation: gen, EngineEpoch: prepared["engine_epoch"].(string), ReinjectionAddr: prepared["reinjection_addr"].(string), EngineTransportCert: prepared["engine_transport_cert"].(string), HandoffSecret: handoff, ReinjectionSecret: reinjection}))
	go service.Serve()
	must(tunnel.RewriteProbe(tunnel.RewriteGeneration{Version: 1, Generation: gen}))
	var activation error
	for i := 0; i < 50; i++ {
		_, activation = tunnel.RewriteSetBranch(tunnel.RewriteBranch{Version: 1, Generation: gen, Enabled: true})
		if activation == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	must(activation)
	defer func() {
		_, e := tunnel.RewriteSetBranch(tunnel.RewriteBranch{Version: 1, Generation: gen, Enabled: false})
		must(e)
		must(tunnel.RewriteDeleteSession(gen))
	}()
	tunnel.SetMode(mode)
	app, engine := net.Pipe()
	go tunnel.Tunnel.HandleTCPConn(engine, &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "api.service.example", DstPort: uint16(p), SrcIP: netip.MustParseAddr("127.0.0.1"), SrcPort: 12345, InName: "fixture-tun"})
	defer app.Close()
	app.SetDeadline(time.Now().Add(10 * time.Second))
	conn := net.Conn(app)
	if scheme == "https" {
		conn = tls.Client(app, &tls.Config{ServerName: "api.service.example", NextProtos: []string{"http/1.1"}})
	}
	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	_, e = gz.Write([]byte(`{"number":9007199254740993}`))
	must(e)
	must(gz.Close())
	req, e := http.NewRequest("POST", scheme+"://api.service.example:"+port+"/fixture", bytes.NewReader(encoded.Bytes()))
	must(e)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	must(req.Write(conn))
	resp, e := http.ReadResponse(bufio.NewReader(conn), req)
	must(e)
	b, e := io.ReadAll(resp.Body)
	must(e)
	resp.Body.Close()
	if string(b) != "actual-engine-ok" {
		panic(string(b))
	}
	fmt.Printf("passed %s %s\n", scheme, mode.String())
}
