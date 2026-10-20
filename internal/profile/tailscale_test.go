package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func tailscaleFixture(t *testing.T, source string) *Store {
	t.Helper()
	s := NewStore(t.TempDir(), func([]byte) error { return nil }, nil)
	if err := os.MkdirAll(filepath.Join(s.Dir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "profiles", "work.yaml"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "settings.yaml"), []byte("profile: work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return s
}
func testKey(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func ptr(value string) *string { return &value }

const tsSource = `proxies:
  - {name: Alpha, type: socks5, server: 127.0.0.1, port: 1080}
  - {name: Local, type: direct}
proxy-groups:
  - {name: Group, type: select, proxies: [Alpha]}
proxy-providers:
  remote: {type: http, url: 'https://example.com/feed'}
  local: {type: inline, payload: [{name: Inner, type: socks5, server: 127.0.0.1, port: 1081}]}
rules: [MATCH,DIRECT]
`

func TestTailscaleOfflineStateRewrite(t *testing.T) {
	s := tailscaleFixture(t, tsSource+"\n")
	cfg, _ := s.tailscaleConfig("work")
	cfg["proxies"] = append(tsProxies(cfg), map[string]any{"name": "Tail", "type": "tailscale", "state-dir": "/unsafe/source", "exit-node": "exit"})
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(s.Dir, "profiles", "work.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	effective, err := s.Effective("work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(effective), "/unsafe/source") {
		t.Fatal("raw state path retained")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ".tailscale-state")); !os.IsNotExist(err) {
		t.Fatal("read-only query created identity state")
	}

}
func TestTailscaleKeyFileBoundsAndPrivacy(t *testing.T) {
	for _, content := range []string{"", " \n", strings.Repeat("k", MaxTailscaleAuthKeyBytes+1), "one\ntwo", "abc\x00secret"} {
		path := testKey(t, content)
		if _, err := readTailscaleAuthKey(path); err == nil || strings.Contains(err.Error(), path) || strings.Contains(err.Error(), content) && content != "" && strings.TrimSpace(content) != "" {
			t.Fatalf("unsafe key content accepted/leaked: %v", err)
		}
	}
	if _, err := readTailscaleAuthKey(testKey(t, strings.Repeat("k", MaxTailscaleAuthKeyBytes))); err != nil {
		t.Fatal("maximum rejected", err)
	}
	path := testKey(t, "secret")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readTailscaleAuthKey(link); err == nil {
		t.Fatal("key symlink accepted")
	}
	if _, err := readTailscaleAuthKey(t.TempDir()); err == nil {
		t.Fatal("directory key accepted")
	}
}
func TestTailscaleFailedValidationRestoresOverride(t *testing.T) {
	s := tailscaleFixture(t, tsSource)
	key := testKey(t, "SECRET")
	s.Validate = func([]byte) error { return errors.New("validation rejected") }
	if err := s.AddProxyNode(context.Background(), "work", "Tail", TailscaleOptions{ExitNode: ptr("exit"), AuthKeyFile: &key}, nil); err == nil {
		t.Fatal("validation failure ignored")
	}
	if _, err := s.Override("work"); !os.IsNotExist(err) {
		t.Fatal("failed validation retained override")
	}
}

func TestTailscaleRemovalProtectsNativeDNSReferences(t *testing.T) {
	for _, dns := range []string{
		"nameserver: [ts://Tail]",
		"fallback: [tailscale://Tail]",
		"nameserver-policy: {example.com: [tailscale://Tail]}",
		"proxy-server-nameserver: ['https://resolver.example/dns-query#Tail']",
		"direct-nameserver: ['https://resolver.example/dns-query#Tail&h3=true']",
	} {
		s := tailscaleFixture(t, "proxies: [{name: Tail, type: tailscale}]\ndns: {"+dns+"}")
		if err := s.RemoveProxyNode(context.Background(), "work", "Tail"); err == nil || !strings.Contains(err.Error(), "DNS") {
			t.Fatalf("DNS reference removed: %s %v", dns, err)
		}
	}
}
func TestTailscaleNameCollisions(t *testing.T) {
	for _, name := range []string{"DIRECT", "REJECT", "GLOBAL", "Alpha", "Group", "remote"} {
		s := tailscaleFixture(t, tsSource)
		key := testKey(t, "secret")
		if err := s.AddProxyNode(context.Background(), "work", name, TailscaleOptions{ExitNode: ptr("exit"), AuthKeyFile: &key}, nil); err == nil {
			t.Fatal("name collision accepted", name)
		}
	}
}

const tsBatchSource = `proxies:
  - {name: Tail, type: tailscale, exit-node: exit}
  - {name: Alpha, type: socks5, server: localhost, port: 1080}
  - {name: Beta, type: socks5, server: localhost, port: 1081}
proxy-providers:
  first: {type: http, url: 'https://example.com/first', proxy: DIRECT, override: {udp: true}}
  second: {type: inline, payload: [{name: Inner, type: socks5}], override: {tfo: true}}
`
