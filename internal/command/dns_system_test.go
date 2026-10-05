package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/control"
	"github.com/ronigooja/Nagi/internal/profile"
)

func TestSystemDNSParsers(t *testing.T) {
	v4 := "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\neth0 00000000 01020304 0003 0 0 0 00000000 0 0 0\n"
	if got := linuxIPv4Default(v4); got != "eth0" {
		t.Fatalf("IPv4 interface = %q", got)
	}
	v6 := strings.Repeat("0", 32) + " 00 " + strings.Repeat("0", 32) + " 00 " + strings.Repeat("0", 32) + " 00000001 00000000 00000000 00000001 tun0\n"
	if got := linuxIPv6Default(v6); got != "tun0" {
		t.Fatalf("IPv6 interface = %q", got)
	}
	if got := resolverAddresses("nameserver 127.0.0.53\nnameserver 2001:db8::1\nnameserver 127.0.0.53\nsearch private.example\n"); len(got) != 2 || got[0] != "127.0.0.53" || got[1] != "2001:db8::1" {
		t.Fatalf("resolvers = %v", got)
	}
	if got := macRouteInterface("   route to: default\n interface: utun3\n"); got != "utun3" {
		t.Fatalf("macOS interface = %q", got)
	}
	if got := macResolverAddresses("resolver #1\n nameserver[0] : 192.0.2.1\n nameserver[1] : 2001:db8::1\n search domain[0] : private.example\n"); len(got) != 2 || got[0] != "192.0.2.1" || got[1] != "2001:db8::1" {
		t.Fatalf("macOS resolvers = %v", got)
	}
}

func TestDNSCheckKeepsLeakVerificationUnproven(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles", "default.yaml"), []byte("dns:\n  enable: true\n  nameserver: [https://1.1.1.1/dns-query]\n  default-nameserver: [https://1.1.1.1/dns-query]\ntun:\n  device: nagi-no-such-device\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := dnsCommand(context.Background(), []string{"dns", "check"}, profile.NewStore(root, nil, nil), "default", control.New(filepath.Join(root, "absent.sock")))
	if err != nil {
		t.Fatal(err)
	}
	result := data.(map[string]any)
	if result["leak_protection_verified"] != false || result["engine_query_ok"] != false {
		t.Fatalf("false verification: %v", result)
	}
	evidence, ok := result["system_evidence"].(map[string]any)
	if !ok || evidence["route_source"] == "" || evidence["resolver_source"] == "" {
		t.Fatalf("missing evidence sources: %v", result)
	}
	if evidence["configured_tun_device"] != "nagi-no-such-device" || evidence["configured_tun_present"] != false {
		t.Fatalf("incorrect TUN evidence: %v", evidence)
	}
}
