package server

// Explicit platform acceptance, never enabled by ordinary tests. Linux callers
// must place this test in a fresh network namespace with loopback up. macOS
// callers must explicitly authorize its single documentation-address route.
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/elazarl/goproxy"
	"github.com/ronigooja/Nagi/internal/capture"
	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/privileged"
	domain "github.com/ronigooja/Nagi/internal/requestrewrite"
	"github.com/ronigooja/Nagi/internal/requestrewriteservice"
)

type nativeCaptureMessage struct {
	Action         string
	Config         requestrewriteservice.Configuration
	Request        capture.Request
	Announcement   requestrewriteservice.Announcement
	Reply          capture.Reply
	Port           string
	Error          string
	HelperSocket   string
	HelperRequest  privileged.Request
	HelperResponse privileged.Response
}

func TestCaptureNativeRestrictedTUN(t *testing.T) {
	if os.Getenv("NAGI_CAPTURE_NATIVE_CHILD") != "" {
		nativeCaptureChild(t)
		os.Exit(0)
	}
	binary := os.Getenv("NAGI_CAPTURE_NATIVE_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("requires root, isolated network, explicit NAGI_CAPTURE_NATIVE_ENGINE")
	}
	if runtime.GOOS == "linux" {
		self, e := os.Readlink("/proc/self/ns/net")
		if e != nil {
			t.Fatal(e)
		}
		host, e := os.Readlink("/proc/1/ns/net")
		if e != nil {
			t.Fatal(e)
		}
		if self == host {
			t.Fatal("refusing actual TUN in PID1 network namespace; use test-request-capture-native-linux.sh")
		}
	}
	root, err := os.MkdirTemp("", "nagi-native-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	// Ancestors are root-owned; only the child subtree is ordinary-UID writable.
	if err = os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	childDir := filepath.Join(root, "child")
	if err = os.Mkdir(childDir, 0700); err != nil {
		t.Fatal(err)
	}
	const owner = 65534
	if err = os.Chown(childDir, owner, owner); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// go test's build directory can be private to root.
	childExe := filepath.Join(root, "test-child")
	in, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(childExe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	in.Close()
	out.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(childExe, "-test.run=^TestCaptureNativeRestrictedTUN$")
	cmd.Env = append(os.Environ(), "NAGI_CAPTURE_NATIVE_CHILD="+childDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: owner, Gid: owner}}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	encoder, decoder := json.NewEncoder(stdin), json.NewDecoder(stdout)
	exchange := func(m nativeCaptureMessage) nativeCaptureMessage {
		t.Helper()
		if err := encoder.Encode(m); err != nil {
			t.Fatal(err)
		}
		var r nativeCaptureMessage
		if err := decoder.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Error != "" {
			t.Fatalf("%s: %s", m.Action, r.Error)
		}
		return r
	}
	var hello nativeCaptureMessage
	if err = decoder.Decode(&hello); err != nil {
		b, _ := io.ReadAll(decoder.Buffered())
		t.Fatalf("child startup: %v %s", err, b)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	trustedRoot, err := os.MkdirTemp(home, ".nagi-native-root-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(trustedRoot)
	trustedBinary := filepath.Join(trustedRoot, "mihomo")
	src, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(trustedBinary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(dst, src)
	src.Close()
	dst.Close()
	if err != nil {
		t.Fatal(err)
	}
	source, private := filepath.Join(trustedRoot, "source"), filepath.Join(trustedRoot, "private")
	for _, p := range []string{source, private} {
		if err = os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(source, "fixture.yaml")
	yaml := ("mixed-port: 0\nport: 0\nsocks-port: 0\nlog-level: silent\nfind-process-mode: always\ndns: {enable: false, fake-ip-range: 198.19.255.1/24}\nhosts: {'api.fixture.example': 127.0.0.1}\nsniffer:\n  enable: true\n  override-destination: true\n  sniff:\n    HTTP: {ports: [1-65535], override-destination: true}\ntun:\n  enable: true\n  stack: gvisor\n  auto-route: true\n  auto-detect-interface: false\n  route-address: [198.51.100.1/32]\n  dns-hijack: []\nrules: ['MATCH,DIRECT']\n")
	if err = os.WriteFile(cfg, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(cfg, owner, owner); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{SessionUID: owner, BinaryPath: trustedBinary, SourceDir: source, PrivateDir: private}
	defer func() {
		if t.Failed() {
			if b, e := os.ReadFile(filepath.Join(private, "mihomo.log")); e == nil {
				t.Logf("isolated fixture engine log: %s", b)
			}
		}
		if e := h.Close(); e != nil {
			t.Errorf("close root engine: %v", e)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtimeDir := "/run"
	if runtime.GOOS == "darwin" {
		// /private/var/run is group-writable on macOS and deliberately
		// rejected by the helper's root-controlled-ancestor requirement.
		runtimeDir = "/private/var"
	}
	socketDir, err := os.MkdirTemp(runtimeDir, "nagi-native-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	if err = os.Chmod(socketDir, 0755); err != nil {
		t.Fatal(err)
	}
	helperSocket := filepath.Join(socketDir, "helper.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: helperSocket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(helperSocket, owner, owner); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(helperSocket, 0600); err != nil {
		t.Fatal(err)
	}
	helper := Server{SessionUID: owner, Policy: privileged.Policy{AllowedPaths: []string{source}}, Handler: h}
	helperCtx, helperCancel := context.WithCancel(ctx)
	helperDone := make(chan error, 1)
	go func() { helperDone <- helper.Serve(helperCtx, listener) }()
	defer func() {
		helperCancel()
		if e := <-helperDone; e != nil {
			t.Errorf("helper serve: %v", e)
		}
	}()
	helperCall := func(q privileged.Request) privileged.Response {
		t.Helper()
		q.Version = privileged.ProtocolVersion
		r := exchange(nativeCaptureMessage{Action: "helper", HelperSocket: helperSocket, HelperRequest: q}).HelperResponse
		if !r.OK {
			t.Fatalf("helper %s %s HTTP%d: %s %s", q.Operation, q.Path, r.Status, r.Body, r.Error)
		}
		return r
	}
	response := helperCall(privileged.Request{Operation: privileged.Start, ConfigPath: cfg, Profile: "fixture"})
	if !response.Running {
		t.Fatal("root engine not running")
	}
	request := func(method, path string, v any) privileged.Response {
		t.Helper()
		var body []byte
		if v != nil {
			body = rewriteTestBody(t, v)
		}
		return helperCall(privileged.Request{Operation: privileged.Control, Method: method, Path: path, Body: body})
	}
	capResponse := request("GET", "/request-rewrite/capabilities", nil)
	if !strings.Contains(string(capResponse.Body), `"capture_protocol":1`) {
		t.Fatal("capture capability missing")
	}
	policy := capture.Policy{SessionID: "native-fixture", Revision: 1, State: "recording", Scope: capture.Scope{Domains: []string{"api.fixture.example"}, ProcessPaths: []string{}, AppPaths: []string{}}}
	spec := rewriteTestPrepare(t)
	spec.OwnerUID = owner
	spec.HandoffAddr = hello.Announcement.HandoffAddr
	spec.ChildTransportCert = hello.Announcement.ChildTransportCert
	spec.RuleVersion = hello.Announcement.RuleVersion
	spec.InterceptDomains = []string{}
	spec.TargetDomains = []string{}
	if os.Getenv("NAGI_CAPTURE_NATIVE_REWRITE") == "1" {
		spec.InterceptDomains = []string{"api.fixture.example"}
		spec.TargetDomains = []string{"api.fixture.example"}
	}
	spec.Capture = &policy
	prepared := request("PUT", "/request-rewrite/session", spec)
	var p struct {
		Generation  string `json:"generation"`
		Epoch       string `json:"engine_epoch"`
		Address     string `json:"reinjection_addr"`
		Certificate string `json:"engine_transport_cert"`
	}
	if err = json.Unmarshal(prepared.Body, &p); err != nil {
		t.Fatal(err)
	}
	exchange(nativeCaptureMessage{Action: "configure", Config: requestrewriteservice.Configuration{Capture: &policy, RecorderSocket: filepath.Join(childDir, "capture.sock"), Generation: p.Generation, EngineEpoch: p.Epoch, ReinjectionAddr: p.Address, EngineTransportCert: p.Certificate, HandoffSecret: spec.HandoffSecret, ReinjectionSecret: spec.ReinjectionSecret}})
	request("POST", "/request-rewrite/probe", map[string]any{"version": 2, "generation": spec.Generation})
	request("PUT", "/request-rewrite/branch", map[string]any{"version": 2, "generation": spec.Generation, "enabled": true})
	list := func() []capture.Record {
		return exchange(nativeCaptureMessage{Action: "recorder", Request: capture.Request{Action: "list"}}).Reply.Records
	}
	traffic := func(kind string) { t.Logf("traffic %s", kind); exchange(nativeCaptureMessage{Action: kind}) }
	waitCount := func(n int) []capture.Record {
		t.Helper()
		for end := time.Now().Add(8 * time.Second); time.Now().Before(end); {
			r := list()
			if len(r) >= n {
				return r
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("expected %d records, got %d", n, len(list()))
		return nil
	}
	traffic("http")
	rows := waitCount(1)
	identity := rows[0].Identity
	if os.Getenv("NAGI_CAPTURE_NATIVE_REWRITE") == "1" {
		if len(rows[0].Snapshots["request_original"].Headers["X-Rewritten"]) != 0 {
			t.Fatal("original snapshot rewritten")
		}
	}
	if identity.UID != owner || identity.ProcessPath != childExe {
		t.Fatalf("original process identity: %+v; executable=%s", identity, childExe)
	}
	if rows[0].Snapshots["request_original"].Headers["Authorization"][0] != "Bearer fictitious-token" {
		t.Fatal("original credential missing")
	}
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); {
		rows = list()
		if rows[0].Snapshots["response_original"].Captured > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	body := exchange(nativeCaptureMessage{Action: "recorder", Request: capture.Request{Action: "body", RecordID: rows[0].ID, Snapshot: "response_original", Limit: 1024}}).Reply.Body
	if body == nil || string(body.Data) != "native-original-response:native-original-body" {
		t.Fatalf("original response body: %+v", body)
	}
	httpCount := 1
	if runtime.GOOS == "linux" {
		traffic("https")
		httpCount++
	}
	traffic("websocket")
	rows = waitCount(httpCount + 3)
	messages := 0
	for _, r := range rows {
		if r.Kind == "websocket" {
			messages++
		}
	}
	if messages != 2 {
		t.Fatalf("bidirectional messages: %d", messages)
	}
	// Preserve generation while changing only capture scope/revision. Unknown app
	// identity on Linux must not match a macOS bundle restriction.
	var update func(capture.Scope, string)
	update = func(scope capture.Scope, state string) {
		if policy.State == state {
			if state == "recording" {
				update(policy.Scope, "paused")
			} else {
				t.Fatal("duplicate fixture update")
			}
		}
		action := "pause"
		if state == "recording" {
			action = "resume"
		}
		exchange(nativeCaptureMessage{Action: "recorder", Request: capture.Request{Action: action, SessionID: policy.SessionID}})
		policy.Revision++
		policy.Scope = scope
		policy.State = state
		exchange(nativeCaptureMessage{Action: "capture", Config: requestrewriteservice.Configuration{Capture: &policy}})
		request("PUT", "/request-rewrite/capture", control.RewriteCapture{Version: 2, Generation: spec.Generation, PreviousRevision: policy.Revision - 1, Capture: policy})
	}
	count := len(rows)
	update(capture.Scope{Domains: []string{"api.fixture.example"}, ProcessPaths: []string{childExe}, AppPaths: []string{}}, "recording")
	traffic("http")
	waitCount(count + 1)
	count++
	update(capture.Scope{Domains: []string{"api.fixture.example"}, ProcessPaths: []string{childExe + "-not-a-process"}, AppPaths: []string{}}, "recording")
	traffic("http")
	time.Sleep(200 * time.Millisecond)
	if len(list()) != count {
		t.Fatal("nonmatching process captured")
	}
	if runtime.GOOS == "linux" {
		update(capture.Scope{Domains: []string{"api.fixture.example"}, ProcessPaths: []string{}, AppPaths: []string{"/Applications/Fictitious.app"}}, "recording")
		traffic("http")
		if len(list()) != count {
			t.Fatal("unknown app identity captured")
		}
	}
	update(policy.Scope, "paused")
	traffic("http")
	if len(list()) != count {
		t.Fatal("paused capture admitted record")
	}
	request("GET", "/request-rewrite/status", nil)
	t.Logf("actual OS TUN root-helper acceptance passed: ordinary UID %d HTTP/HTTPS(Linux) + bidirectional WebSocket, original process/credential/body, exact process scope, unknown app exclusion, capture revision updates; platform=%s rewrite=%s", owner, runtime.GOOS, os.Getenv("NAGI_CAPTURE_NATIVE_REWRITE"))
}

func nativeCaptureChild(t *testing.T) {
	root := os.Getenv("NAGI_CAPTURE_NATIVE_CHILD")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(root, "capture.sock")
	go func() {
		if e := capture.Serve(ctx, socket, os.Getuid()); e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
	}()
	for i := 0; i < 100; i++ {
		if _, e := os.Stat(socket); e == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	start, e := capture.Call(ctx, socket, capture.Request{Action: "start", Start: &capture.StartOptions{SessionID: "native-fixture", Profile: "fixture", Backend: "privileged", Owner: "cli", Scope: capture.Scope{Domains: []string{"api.fixture.example"}, ProcessPaths: []string{}, AppPaths: []string{}}}})
	if e != nil || start.Status == nil {
		t.Fatal(e)
	}
	rules := []domain.Rule{}
	if os.Getenv("NAGI_CAPTURE_NATIVE_REWRITE") == "1" {
		rules = append(rules, domain.Rule{ID: "native-rewrite", Enabled: true, Stage: domain.StageRequest, Match: domain.Match{Domain: "api.fixture.example"}, OnError: "block", RedirectConsent: []domain.Consent{}, Operations: []domain.Operation{{Op: "header_set", Name: "X-Rewritten", Value: json.RawMessage(`"native-rewrite"`)}}})
	}
	snapshot, e := domain.Compile(domain.Document{Version: 2, Rules: rules, Scripts: []domain.Script{}})
	if e != nil {
		t.Fatal(e)
	}
	caStore := domain.CAStore{ConfigDir: root}
	if _, e = caStore.Generate(time.Now()); e != nil {
		t.Fatal(e)
	}
	caCert, caKey, e := caStore.LoadForChild(time.Now())
	if e != nil {
		t.Fatal(e)
	}
	public, e := caStore.ExportPublic(time.Now())
	if e != nil {
		t.Fatal(e)
	}
	publicPath := filepath.Join(root, "public-root.pem")
	if e = os.WriteFile(publicPath, public, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Setenv("SSL_CERT_FILE", publicPath); e != nil {
		t.Fatal(e)
	}
	emptyRoots := filepath.Join(root, "empty-roots")
	if e = os.Mkdir(emptyRoots, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Setenv("SSL_CERT_DIR", emptyRoots); e != nil {
		t.Fatal(e)
	}
	ca := tls.Certificate{Certificate: [][]byte{caCert.Raw}, PrivateKey: caKey}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw}))
	service, e := requestrewriteservice.New(snapshot, ca)
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			c, e := websocket.Accept(w, r, nil)
			if e != nil {
				return
			}
			defer c.CloseNow()
			kind, b, e := c.Read(r.Context())
			if e == nil {
				_ = c.Write(r.Context(), kind, b)
			}
			return
		}
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Observed-Rewrite", r.Header.Get("X-Rewritten"))
		_, _ = w.Write(append([]byte("native-original-response:"), b...))
	})
	origin := httptest.NewServer(handler)
	defer origin.Close()
	tlsOrigin := httptest.NewUnstartedServer(handler)
	tlsOrigin.TLS, e = goproxy.TLSConfigFromCA(&ca)("api.fixture.example", &goproxy.ProxyCtx{Proxy: &goproxy.ProxyHttpServer{}})
	if e != nil {
		t.Fatal(e)
	}
	tlsOrigin.StartTLS()
	defer tlsOrigin.Close()
	_, tlsPort, _ := net.SplitHostPort(tlsOrigin.Listener.Addr().String())
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		_, requestPort, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		return (&net.Dialer{}).DialContext(ctx, network, "198.51.100.1:"+requestPort)
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	encoder, decoder := json.NewEncoder(os.Stdout), json.NewDecoder(os.Stdin)
	_ = encoder.Encode(nativeCaptureMessage{Announcement: service.Announcement(), Port: port})
	for {
		var m nativeCaptureMessage
		if e := decoder.Decode(&m); e != nil {
			return
		}
		r := nativeCaptureMessage{}
		var err error
		switch m.Action {
		case "configure":
			err = service.Configure(m.Config)
			if err == nil {
				go service.Serve()
			}
		case "helper":
			var conn *net.UnixConn
			conn, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: m.HelperSocket, Net: "unix"})
			if err == nil {
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				err = privileged.WriteRequest(conn, m.HelperRequest)
				if err == nil {
					r.HelperResponse, err = privileged.ReadResponse(conn)
				}
				conn.Close()
			}
		case "capture":
			err = service.SetCapture(*m.Config.Capture)
		case "recorder":
			r.Reply, err = capture.Call(ctx, socket, m.Request)
		case "http", "https":
			scheme, requestPort := "http", port
			if m.Action == "https" {
				scheme, requestPort = "https", tlsPort
			}
			var req *http.Request
			req, err = http.NewRequest("POST", scheme+"://api.fixture.example:"+requestPort+"/original?credential=fictitious-token", strings.NewReader("native-original-body"))
			if err == nil {
				req.Header.Set("Authorization", "Bearer fictitious-token")
				var resp *http.Response
				resp, err = client.Do(req)
				if err == nil {
					b, e := io.ReadAll(resp.Body)
					resp.Body.Close()
					if os.Getenv("NAGI_CAPTURE_NATIVE_REWRITE") == "1" && resp.Header.Get("X-Observed-Rewrite") != "native-rewrite" {
						e = fmt.Errorf("rewrite did not execute independently of capture scope")
					}
					err = e
					if string(b) != "native-original-response:native-original-body" {
						err = fmt.Errorf("unexpected HTTP status %d body", resp.StatusCode)
					}
				}
			}
		case "websocket":
			var c *websocket.Conn
			c, _, err = websocket.Dial(ctx, "ws://api.fixture.example:"+port+"/ws", &websocket.DialOptions{HTTPClient: client})
			if err == nil {
				err = c.Write(ctx, websocket.MessageText, []byte("native-message"))
				if err == nil {
					_, b, e := c.Read(ctx)
					err = e
					if string(b) != "native-message" {
						err = fmt.Errorf("unexpected WebSocket body")
					}
				}
				c.Close(websocket.StatusNormalClosure, "fixture")
			}
		default:
			err = fmt.Errorf("unknown action")
		}
		if err != nil {
			r.Error = err.Error()
		}
		_ = encoder.Encode(r)
	}
}
