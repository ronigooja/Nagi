package configpolicy

import (
	"errors"
	"fmt"
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

func TestValidateInlineSubscriptionProfile(t *testing.T) {
	data := []byte("mixed-port: 7890\nallow-lan: false\nmode: rule\nproxies:\n  - {name: SG, type: vless, server: example.com, port: 443, uuid: 00000000-0000-0000-0000-000000000000, network: ws, tls: true, ws-opts: {path: /connection}}\nproxy-groups:\n  - {name: Select, type: select, proxies: [SG, DIRECT]}\nrule-providers:\n  nagi-test: {type: inline, behavior: domain, payload: [example.com]}\nrules:\n  - RULE-SET,nagi-test,Select\n  - MATCH,Select\n")
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
}

func TestRejectPrivilegedEscapeSurfaces(t *testing.T) {
	cases := map[string]string{
		"controller":      "external-controller: 0.0.0.0:9090",
		"unix controller": "external-controller-unix: /tmp/controller.sock",
		"TLS controller":  "external-controller-tls: 0.0.0.0:9090",
		"UI path":         "external-ui: /etc",
		"node plugin":     "proxies:\n  - name: x\n    type: ss\n    plugin: /tmp/program",
		"script":          "script: {type: script, code: evil}",
		"exec":            "exec: /bin/sh",
		"TLS file":        "tls:\n  certificate: /etc/secret",
		"geo path":        "geox-url:\n  geoip: file:///etc/shadow",
		"tun device":      "tun:\n  enable: true\n  device: /dev/evil",
		"automatic geo":   "geo-auto-update: true",
		"privileged port": "mixed-port: 80",
		"DNS script":      "dns: {exec: /bin/sh}",
		"listener file":   "listeners: [{type: mixed, listen: 127.0.0.1, port: 17890, certificate: /etc/secret}]",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := Validate([]byte("mode: direct\nallow-lan: true\nbind-address: '*'\n" + body + "\n"))
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("unsafe configuration accepted: %v", err)
			}
		})
	}
}

func TestValidateLANWithTUN(t *testing.T) {
	// Validate is shared by startup snapshots and reload snapshots.
	for _, enabled := range []bool{false, true} {
		for _, address := range []string{"127.0.0.1", "::1", "localhost", "*", "0.0.0.0", "192.0.2.10", "::", "2001:db8::10"} {
			t.Run(fmt.Sprintf("%t/%s", enabled, address), func(t *testing.T) {
				data := fmt.Sprintf("mode: rule\ntun: {enable: true, auto-route: true}\nallow-lan: %t\nbind-address: %q\nmixed-port: 17890\n", enabled, address)
				if err := Validate([]byte(data)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestValidateLANRejectsInvalidValues(t *testing.T) {
	for _, field := range []string{
		"allow-lan: null", "allow-lan: 1", `allow-lan: "true"`, "allow-lan: []", "allow-lan: {}",
		"bind-address: null", "bind-address: true", "bind-address: 123", "bind-address: []",
		`bind-address: ""`, `bind-address: "proxy.example.test"`, `bind-address: "localhost.example.test"`,
		`bind-address: "fe80::1%en0"`, `bind-address: "[::1]"`, `bind-address: "127.0.0.1:7890"`,
		`bind-address: "192.0.2.1/24"`, `bind-address: " * "`,
	} {
		t.Run(field, func(t *testing.T) {
			if err := Validate([]byte("tun: {enable: true}\n" + field + "\n")); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("invalid LAN configuration accepted: %v", err)
			}
		})
	}
}

func TestValidateLANPreservesProxyPortPolicy(t *testing.T) {
	for _, field := range []string{"port", "socks-port", "mixed-port"} {
		for _, port := range []int{-1, 0, 1, 80, 1023, 1024, 17890, 65535, 65536} {
			t.Run(fmt.Sprintf("%s/%d", field, port), func(t *testing.T) {
				data := fmt.Sprintf("allow-lan: true\nbind-address: '*'\n%s: %d\n", field, port)
				err := Validate([]byte(data))
				wantOK := port == 0 || port >= 1024 && port <= 65535
				if (err == nil) != wantOK {
					t.Fatalf("port policy: valid=%t err=%v", wantOK, err)
				}
			})
		}
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
		"mode: unknown\n",
		"tun: {enable: 1}\n",
	}
	for _, data := range cases {
		if err := Validate([]byte(data)); !errors.Is(err, ErrUnsupported) {
			t.Errorf("accepted %q: %v", data, err)
		}
	}
}

func TestUnsupportedLiteralDoesNotEchoValue(t *testing.T) {
	const secret = "private-token-12345"
	err := Validate([]byte("mode: " + secret + "\n"))
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "mode") || strings.Contains(err.Error(), secret) {
		t.Fatalf("unredacted or missing field error: %v", err)
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
