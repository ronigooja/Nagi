package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Nagi/internal/privileged"
)

func TestRestrictedControl(t *testing.T) {
	allowed := []privileged.Request{
		{Method: "GET", Path: "/version"},
		{Method: "GET", Path: "/proxies/Group/delay?timeout=5000&url=https%3A%2F%2Fexample.com"},
		{Method: "PATCH", Path: "/configs", Body: []byte(`{"tun":{"enable":true,"auto-route":true,"auto-detect-interface":true}}`)},
		{Method: "PUT", Path: "/proxies/Group", Body: []byte(`{"name":"Node"}`)},
		{Method: "DELETE", Path: "/proxies/Auto"},
		{Method: "GET", Path: "/providers/proxies"},
		{Method: "GET", Path: "/providers/proxies/work/healthcheck"},
		{Method: "PUT", Path: "/providers/proxies/work"},
		{Method: "GET", Path: "/providers/proxies/work/Node/healthcheck?timeout=5000&url=https%3A%2F%2Fexample.com"},
		{Method: "GET", Path: "/group/Auto/delay?timeout=5000&url=https%3A%2F%2Fexample.com"},
		{Method: "GET", Path: "/proxies/Tail/tailscale/status"},
		{Method: "POST", Path: "/proxies/Tail/tailscale/ping", Body: []byte(`{"peer_ip":"100.64.0.2","timeout_ms":5000}`)},
	}
	for _, req := range allowed {
		if err := validateControl(req); err != nil {
			t.Errorf("rejected %s %s: %v", req.Method, req.Path, err)
		}
	}
	denied := []privileged.Request{
		{Method: "PUT", Path: "/configs?force=true", Body: []byte(`{"path":"/etc/shadow"}`)},
		{Method: "PATCH", Path: "/configs", Body: []byte(`{"external-controller":"0.0.0.0:9090"}`)},
		{Method: "PATCH", Path: "/configs", Body: []byte(`{"tun":{"enable":true,"device":"/dev/evil"}}`)},
		{Method: "GET", Path: "/configs/../../etc/shadow"},
		{Method: "GET", Path: "/proxies/%2Fetc%2Fshadow"},
		{Method: "POST", Path: "/configs"},
		{Method: "PUT", Path: "/providers/proxies/work", Body: []byte(`{"url":"https://evil.test"}`)},
		{Method: "DELETE", Path: "/providers/proxies/work"},
		{Method: "GET", Path: "/proxies/Tail/tailscale/status?force=true"},
		{Method: "POST", Path: "/proxies/Tail/tailscale/status", Body: []byte(`{}`)},
		{Method: "POST", Path: "/proxies/Tail/tailscale/ping", Body: []byte(`{"peer_ip":"host.example","timeout_ms":5000}`)},
		{Method: "POST", Path: "/proxies/Tail/tailscale/ping", Body: []byte(`{"peer_ip":"100.64.0.2","timeout_ms":5000,"extra":true}`)},
		{Method: "POST", Path: "/proxies/Tail/tailscale/ping", Body: []byte(`{"peer_ip":"100.64.0.2","peer_ip":"100.64.0.3","timeout_ms":5000}`)},
		{Method: "POST", Path: "/proxies/Tail/tailscale/ping", Body: []byte(`{"peer_ip":"100.64.0.2","timeout_ms":30000}`)},
	}
	for _, req := range denied {
		if err := validateControl(req); err == nil {
			t.Errorf("allowed %s %s", req.Method, req.Path)
		}
	}
}

func TestReadUserConfigRejectsSymlinksAndEscape(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profile.yaml")
	if err := os.WriteFile(path, []byte("mode: direct\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readUserConfig(root, path, os.Getuid())
	if err != nil || string(data) != "mode: direct\n" {
		t.Fatalf("safe profile: %q %v", data, err)
	}
	if _, err := readUserConfig(root, "/etc/passwd", os.Getuid()); err == nil {
		t.Fatal("accepted escaped path")
	}
	link := filepath.Join(root, "link.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readUserConfig(root, link, os.Getuid()); err == nil {
		t.Fatal("followed symlink")
	}
}

func TestOrphanMarkerPreventsDuplicateRootChild(t *testing.T) {
	dir := t.TempDir()
	h := &RuntimeHandler{PrivateDir: dir}
	if err := os.WriteFile(h.markerPath(), []byte("pending\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := h.Handle(context.Background(), os.Getuid(), privileged.Request{Operation: privileged.Start})
	if err == nil || !h.orphaned {
		t.Fatalf("orphan marker was ignored: %v", err)
	}
	if _, err := h.Handle(context.Background(), os.Getuid(), privileged.Request{Operation: privileged.Status}); err == nil {
		t.Fatal("orphan status reported stopped")
	}
}

func TestActivationIDValidation(t *testing.T) {
	good := `{"path":"/source/profile.yaml","activation_id":"0123456789abcdef0123456789abcdef"}`
	if !validActivationID([]byte(good)) {
		t.Fatal("valid activation ID rejected")
	}
	for _, body := range []string{
		`{"path":"/source/profile.yaml"}`,
		`{"path":"/source/profile.yaml","activation_id":"0123456789ABCDEF0123456789ABCDEF"}`,
		`{"path":"/source/profile.yaml","activation_id":"0123456789abcdef0123456789abcdeg"}`,
		`{"path":"/source/profile.yaml","activation_id":"0123456789abcdef0123456789abcdef","other":true}`,
	} {
		if validActivationID([]byte(body)) {
			t.Errorf("accepted invalid activation ID: %s", body)
		}
	}
}

func TestPrepareConfigSnapshotsRelativeResources(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root snapshot test")
	}
	source := t.TempDir()
	private, err := os.MkdirTemp("/root", "nagi-t6-private-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	profileDir := filepath.Join(source, "profiles")
	if err := os.Mkdir(profileDir, 0700); err != nil {
		t.Fatal(err)
	}
	tlsFile := filepath.Join(profileDir, "cert.pem")
	providerFile := filepath.Join(profileDir, "nodes.yaml")
	if err := os.WriteFile(tlsFile, []byte("private TLS material"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerFile, []byte("proxies: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{SessionUID: os.Getuid(), SourceDir: source, PrivateDir: private}
	data := []byte("proxies:\n  - {name: n, type: trojan, server: example.com, port: 443, password: secret, certificate: cert.pem}\nproxy-providers:\n  p: {type: file, path: nodes.yaml}\n")
	snapshot, resources, err := h.prepareConfig(data, filepath.Join(profileDir, "source.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer removeSnapshots(snapshot, resources)
	if len(resources) != 2 {
		t.Fatalf("got %d resources", len(resources))
	}
	config, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(config, []byte("cert.pem")) || bytes.Contains(config, []byte("nodes.yaml")) {
		t.Fatal("source references survived snapshot")
	}
	inline := []byte("proxy-providers:\n  inline: {type: inline, payload: [{name: n, type: trojan, server: example.com, port: 443, password: secret, certificate: cert.pem}]}\n")
	inlineSnapshot, inlineResources, err := h.prepareConfig(inline, filepath.Join(profileDir, "source.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer removeSnapshots(inlineSnapshot, inlineResources)
	if len(inlineResources) != 1 {
		t.Fatalf("inline provider resources: %d", len(inlineResources))
	}
	inlineConfig, err := os.ReadFile(inlineSnapshot)
	if err != nil || bytes.Contains(inlineConfig, []byte("cert.pem")) {
		t.Fatalf("inline provider source reference survived: %v", err)
	}
	for _, path := range append(resources, snapshot) {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe snapshot %s: %v", path, err)
		}
	}
}

func TestPrepareConfigRejectsEscapedResources(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root snapshot test")
	}
	source := t.TempDir()
	private, err := os.MkdirTemp("/root", "nagi-t6-private-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	h := &RuntimeHandler{SessionUID: os.Getuid(), SourceDir: source, PrivateDir: private}
	for _, reference := range []string{"/etc/passwd", "../../etc/passwd", "link.yaml"} {
		if reference == "link.yaml" {
			if err := os.Symlink("/etc/passwd", filepath.Join(source, reference)); err != nil {
				t.Fatal(err)
			}
		}
		data := []byte("proxy-providers:\n  p: {type: file, path: " + reference + "}\n")
		if _, _, err := h.prepareConfig(data, filepath.Join(source, "profile.yaml")); err == nil {
			t.Errorf("accepted %s", reference)
		}
	}
}

func TestNativeRestrictedValidation(t *testing.T) {
	binary := os.Getenv("NAGI_T6_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("set NAGI_T6_ENGINE to the reviewed restricted mihomo binary and run as root")
	}
	private, err := os.MkdirTemp("/root", "nagi-t6-native-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	h := &RuntimeHandler{BinaryPath: binary, PrivateDir: private}
	for _, tc := range []struct {
		yaml  string
		valid bool
	}{
		{"mode: direct\nallow-lan: false\n", true},
		{"mode: direct\nexternal-ui: /etc\n", false},
		{"proxies:\n  - {name: bad, type: ss, plugin: /bin/sh}\n", false},
		{"dns: {listen: '0.0.0.0:53'}\n", false},
	} {
		snapshot := filepath.Join(private, "check.yaml")
		if err := os.WriteFile(snapshot, []byte(tc.yaml), 0600); err != nil {
			t.Fatal(err)
		}
		err := h.checkNative(context.Background(), snapshot)
		if (err == nil) != tc.valid {
			t.Errorf("native validation valid=%v: %v", tc.valid, err)
		}
		if !tc.valid && err != nil && !strings.Contains(err.Error(), " at ") {
			t.Errorf("native validation lost redacted field location: %v", err)
		}
	}
}

func TestNativePrivilegedStartAndReload(t *testing.T) {
	binary := os.Getenv("NAGI_T6_ENGINE")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("set NAGI_T6_ENGINE to the reviewed restricted mihomo binary and run as root")
	}
	base, err := os.MkdirTemp("/root", "nagi-t6-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	source, private := filepath.Join(base, "source"), filepath.Join(base, "private")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(source, "profile.yaml")
	if err := os.WriteFile(profile, []byte("mode: direct\nallow-lan: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &RuntimeHandler{SessionUID: os.Getuid(), BinaryPath: binary, SourceDir: source, PrivateDir: private}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started, err := h.start(ctx, privileged.Request{ConfigPath: profile})
	if err != nil {
		t.Fatal(err)
	}
	if !started.Running {
		t.Fatal("child did not start")
	}
	defer h.Close()
	id := "0123456789abcdef0123456789abcdef"
	body := []byte(`{"path":"` + profile + `","activation_id":"` + id + `"}`)
	reloaded, err := h.reload(ctx, privileged.Request{Method: "PUT", Path: "/configs?force=true", Body: body})
	if err != nil || !reloaded.OK {
		t.Fatalf("reload: %+v %v", reloaded, err)
	}
	active, err := controlRequest(ctx, h.controllerPath(), privileged.Request{Method: "GET", Path: "/configs"})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		ActivationID string `json:"activation_id"`
	}
	if err := json.Unmarshal(active.Body, &config); err != nil || config.ActivationID != id {
		t.Fatalf("activation ID not forwarded: %+v %v", config, err)
	}
	old := append([]string(nil), h.resources...)
	newest := h.snapshot
	for _, path := range append(old, newest) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("snapshot removed while child runs: %v", err)
		}
	}
	if err := os.Chmod(h.controllerPath(), 0); err != nil {
		t.Fatal(err)
	}
	failure, failed := h.reload(ctx, privileged.Request{Method: "PUT", Path: "/configs?force=true", Body: body})
	if failed == nil {
		t.Fatal("unsafe controller socket accepted")
	}
	if failure.NotForwarded {
		t.Fatal("forwarding attempt incorrectly marked as a pre-forward rejection")
	}
	if err := os.Chmod(h.controllerPath(), 0600); err != nil {
		t.Fatal(err)
	}
	retained := append(append([]string(nil), h.resources...), h.snapshot)
	for _, path := range retained {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("uncertain reload lost snapshot: %v", err)
		}
	}
	if _, err := h.stop(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range retained {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("snapshot survived child exit: %v", err)
		}
	}
}
