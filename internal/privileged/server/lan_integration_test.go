package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
	"github.com/ronigooja/Nagi/internal/traffic"
)

// This opt-in test changes real routes and, on macOS, network-service DNS.
// Run only as root on a disposable GitHub-hosted runner with the matching
// restricted engine. It must not run in parallel with other networking tests.
func TestLANPrivilegedTUNIntegration(t *testing.T) {
	binary := os.Getenv("NAGI_LAN_ENGINE")
	if binary == "" || !filepath.IsAbs(binary) || os.Getenv("GITHUB_ACTIONS") != "true" || os.Geteuid() != 0 {
		t.Skip("requires disposable GitHub-hosted runner, root, GITHUB_ACTIONS=true and absolute NAGI_LAN_ENGINE")
	}
	rootHome := "/root"
	switch runtime.GOOS {
	case "darwin":
		rootHome = "/private/var/root"
	case "linux":
	default:
		t.Skip("requires macOS or Linux")
	}
	lanIP := lanIntegrationIPv4(t)
	base, err := os.MkdirTemp(rootHome, ".nagi-lan-")
	if err != nil {
		t.Fatal(err)
	}
	var h *RuntimeHandler
	t.Cleanup(func() {
		if h != nil {
			if err := h.Close(); err != nil {
				// Keep the DNS recovery journal if restoration fails.
				t.Errorf("close runtime and restore DNS: %v; recovery files retained at %s", err, base)
				return
			}
		}
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove isolated root home: %v", err)
		}
	})
	// CI workspaces are not trusted root ancestors. Copy only the explicitly
	// supplied engine into this root-owned private home before starting it.
	trusted := filepath.Join(base, "mihomo")
	input, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(trusted, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		input.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	inputErr, outputErr := input.Close(), output.Close()
	if copyErr != nil || inputErr != nil || outputErr != nil {
		t.Fatalf("copy opt-in engine: %v, %v, %v", copyErr, inputErr, outputErr)
	}
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	h = &RuntimeHandler{SessionUID: 0, BinaryPath: trusted, SourceDir: source, PrivateDir: filepath.Join(base, "private")}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "nagi-lan-local-fixture")
	}))
	t.Cleanup(origin.Close)
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(source, "lan.yaml")
	writeProfile := func(enabled bool) {
		t.Helper()
		// The only permitted destination is the local TCP fixture. In
		// particular, deny UDP before the loopback DIRECT rule (SOCKS UDP
		// authorization is not equivalent to TCP lan-allowed-ips filtering).
		config := fmt.Sprintf(`mixed-port: %d
allow-lan: %t
bind-address: '*'
lan-allowed-ips: ['127.0.0.1/32', '%s/32']
ipv6: false
mode: rule
log-level: warning
find-process-mode: off
dns:
  enable: true
  ipv6: false
  enhanced-mode: redir-host
  nameserver: ['https://1.1.1.1/dns-query#DIRECT']
  default-nameserver: ['https://1.1.1.1/dns-query#DIRECT']
tun:
  enable: true
  stack: mixed
  auto-route: true
  auto-detect-interface: true
  strict-route: true
  dns-hijack: ['any:53', 'tcp://any:53']
rules:
  - NETWORK,udp,REJECT
  - AND,((IP-CIDR,127.0.0.1/32),(DST-PORT,%d)),DIRECT
  - MATCH,REJECT
`, port, enabled, lanIP, origin.Listener.Addr().(*net.TCPAddr).Port)
		if err := os.WriteFile(profile, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeProfile(true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	request := func(req privileged.Request) privileged.Response {
		t.Helper()
		callCtx, stop := context.WithTimeout(ctx, 45*time.Second)
		defer stop()
		response, err := h.Handle(callCtx, 0, req)
		if err != nil || !response.OK {
			t.Fatalf("%s %s %s: HTTP %d, %v", req.Operation, req.Method, req.Path, response.Status, err)
		}
		return response
	}
	var baselineDNS traffic.DNSState
	if err := json.Unmarshal(request(privileged.Request{Operation: privileged.TUNDNSStatus}).Body, &baselineDNS); err != nil {
		t.Fatal(err)
	}
	started := request(privileged.Request{Operation: privileged.Start, ConfigPath: profile})
	if !started.Running || started.PID <= 0 {
		t.Fatalf("runtime did not start: %+v", started)
	}
	type liveConfig struct {
		ActivationID string `json:"activation_id"`
		AllowLAN     bool   `json:"allow-lan"`
		TUN          struct {
			Enable bool   `json:"enable"`
			Device string `json:"device"`
		} `json:"tun"`
	}
	waitConfig := func(enabled bool, activationID string) liveConfig {
		t.Helper()
		deadline := time.NewTimer(35 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		var config liveConfig
		for {
			response := request(privileged.Request{Operation: privileged.Control, Method: "GET", Path: "/configs"})
			if err := json.Unmarshal(response.Body, &config); err != nil {
				t.Fatal(err)
			}
			adapter, adapterErr := net.InterfaceByName(config.TUN.Device)
			if config.ActivationID != "" && (activationID == "" || config.ActivationID == activationID) && config.AllowLAN == enabled && config.TUN.Enable && adapterErr == nil && adapter.Flags&net.FlagUp != 0 {
				return config
			}
			select {
			case <-ctx.Done():
				t.Fatalf("activation context: %v", ctx.Err())
			case <-deadline.C:
				t.Fatalf("LAN=%t TUN activation not ready: %+v; adapter: %v", enabled, config, adapterErr)
			case <-tick.C:
			}
		}
	}
	assertDNS := func(managed bool) {
		t.Helper()
		response := request(privileged.Request{Operation: privileged.TUNDNSStatus})
		var state traffic.DNSState
		if err := json.Unmarshal(response.Body, &state); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "darwin" {
			if !state.Supported || state.Managed != managed || len(state.Conflicts) != 0 || (managed && (state.Status != "managed" || len(state.Targets) == 0 || len(state.Services) == 0)) {
				t.Fatalf("unexpected managed DNS state: %+v", state)
			}
			if !managed && !reflect.DeepEqual(state.Services, baselineDNS.Services) {
				t.Fatalf("system DNS baseline not restored: before=%v after=%v", baselineDNS.Services, state.Services)
			}
		} else if state.Supported || state.Managed {
			t.Fatalf("Linux must not claim macOS DNS management: %+v", state)
		}
	}
	assertProxy := func(host string) {
		t.Helper()
		proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprint(port))}
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		response, err := client.Get(origin.URL)
		if err != nil {
			t.Fatalf("local origin through proxy %s: %v", host, err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 128))
		if err != nil || response.StatusCode != http.StatusOK || string(body) != "nagi-lan-local-fixture" {
			t.Fatalf("unexpected local proxy response: HTTP %d, %q, %v", response.StatusCode, body, err)
		}
	}
	initial := waitConfig(true, "")
	request(privileged.Request{Operation: privileged.TUNDNSSync})
	assertDNS(true)
	assertProxy(lanIP)
	for _, enabled := range []bool{false, true} {
		request(privileged.Request{Operation: privileged.Control, Method: "PATCH", Path: "/configs", Body: []byte(fmt.Sprintf(`{"allow-lan":%t,"bind-address":"*"}`, enabled))})
		config := waitConfig(enabled, initial.ActivationID)
		if config.TUN.Device != initial.TUN.Device {
			t.Fatalf("LAN PATCH replaced TUN: %q -> %q", initial.TUN.Device, config.TUN.Device)
		}
		assertDNS(true)
		assertProxy("127.0.0.1")
		if enabled {
			assertProxy(lanIP)
		} else {
			connection, err := net.DialTimeout("tcp4", net.JoinHostPort(lanIP, fmt.Sprint(port)), 2*time.Second)
			if err == nil {
				connection.Close()
				t.Fatal("LAN listener remains reachable after disabling LAN")
			}
		}
	}
	// A full profile reload must restore the saved LAN-disabled setting while
	// keeping TUN and its managed DNS available in the same engine process.
	writeProfile(false)
	const activationID = "a123456789abcdef0123456789abcdef"
	body, err := json.Marshal(map[string]string{"path": profile, "activation_id": activationID})
	if err != nil {
		t.Fatal(err)
	}
	request(privileged.Request{Operation: privileged.Control, Method: "PUT", Path: "/configs?force=true", Body: body})
	waitConfig(false, activationID)
	assertDNS(true)
	assertProxy("127.0.0.1")
	status := request(privileged.Request{Operation: privileged.Status})
	if !status.Running || status.PID != started.PID {
		t.Fatalf("LAN changes replaced the privileged engine: %+v", status)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("stop runtime and restore DNS: %v", err)
	}
	assertDNS(false)
}

func lanIntegrationIPv4(t *testing.T) string {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range interfaces {
		if adapter.Flags&net.FlagUp == 0 || adapter.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := adapter.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && ip.IsGlobalUnicast() {
				return ip.String()
			}
		}
	}
	t.Fatal("disposable runner has no up non-loopback IPv4 interface")
	return ""
}
