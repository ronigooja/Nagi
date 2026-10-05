package configpolicy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateLiteralSubset(t *testing.T) {
	data := []byte("mode: direct\nlog-level: info\nipv6: false\ntun:\n  enable: true\n  stack: gvisor\n  auto-route: true\n  strict-route: true\n")
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
}

func TestRejectPrivilegedEscapeSurfaces(t *testing.T) {
	cases := map[string]string{
		"controller":      "external-controller: 0.0.0.0:9090",
		"unix controller": "external-controller-unix: /tmp/controller.sock",
		"UI path":         "external-ui: /etc",
		"provider path":   "proxy-providers:\n  x:\n    type: file\n    path: /etc/shadow",
		"root listener":   "listeners:\n  - name: danger\n    type: http\n    port: 80",
		"node plugin":     "proxies:\n  - name: x\n    type: ss\n    plugin: /tmp/program",
		"script":          "script: {type: script, code: evil}",
		"exec":            "exec: /bin/sh",
		"TLS file":        "tls:\n  certificate: /etc/secret",
		"geo path":        "geox-url:\n  geoip: file:///etc/shadow",
		"tun device":      "tun:\n  enable: true\n  device: /dev/evil",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := Validate([]byte("mode: direct\n" + body + "\n"))
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("unsafe configuration accepted: %v", err)
			}
		})
	}
}

func TestRejectYAMLTricksAndInvalidValues(t *testing.T) {
	cases := []string{
		"mode: direct\nmode: global\n",
		"mode: direct\ntun: &t {enable: true}\n",
		"mode: direct\ntun: *t\n",
		"mode: direct\n---\nexternal-controller: :9090\n",
		"mode: direct\ntun: {enable: 1}\n",
		"mode: direct\nsecret: !!binary YQ==\n",
		"mode: global\n",
		"tun: {enable: true}\n",
	}
	for _, data := range cases {
		if err := Validate([]byte(data)); !errors.Is(err, ErrUnsupported) {
			t.Errorf("accepted %q: %v", data, err)
		}
	}
}

func TestSnapshotRejectsUntrustedDirectory(t *testing.T) {
	dir := t.TempDir()
	data := []byte("mode: direct\n")
	_, err := Snapshot(dir, data)
	if os.Geteuid() == 0 {
		if err == nil || !strings.Contains(err.Error(), "writable or readable") {
			t.Fatalf("snapshot in temp directory: %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "root privileges") {
		t.Fatalf("non-root snapshot: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := securePrivateDirectory(link); err == nil {
		t.Fatal("symlinked snapshot directory accepted")
	}
}
