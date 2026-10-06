package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSupportedFormatsAndPreview(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		count        int
	}{
		{"yaml", "proxies:\n  - name: Alpha\n    type: direct\n", 1},
		{"v2ray-links", base64.StdEncoding.EncodeToString([]byte("vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&type=tcp#Node\nss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388#SS")), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, format, err := Document([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "v2ray-links" && format != "mihomo-v2ray" {
				t.Fatalf("format: %s", format)
			}
			if len(sliceValue(doc["proxies"])) != tc.count {
				t.Fatalf("nodes: %#v", doc)
			}
			if len(sliceValue(doc["proxy-groups"])) == 0 {
				t.Fatal("missing generated group")
			}
		})
	}
	old, _, _ := Document([]byte("proxies:\n  - name: Alpha\n    type: direct\n  - name: Beta\n    type: direct\n"))
	next, format, _ := Document([]byte("proxies:\n  - name: Alpha\n    type: reject\n  - name: Gamma\n    type: direct\n"))
	p := Compare("work", format, next, old)
	if !reflect.DeepEqual(p.Added, []string{"Gamma"}) || !reflect.DeepEqual(p.Removed, []string{"Beta"}) || !reflect.DeepEqual(p.Changed, []string{"Alpha"}) {
		t.Fatalf("preview: %+v", p)
	}
}

func TestNormalizeUsesOnlyDownloadedSettingsAndOrder(t *testing.T) {
	next, _, _ := Document([]byte("mixed-port: 9999\nrules:\n  - MATCH,REJECT\nproxies:\n  - name: Alpha\n    type: direct\n  - name: New\n    type: direct\nproxy-groups:\n  - name: Choice\n    type: select\n    proxies: [New, Alpha, DIRECT]\n"))
	merged := Normalize(next)
	if merged["mixed-port"] != 9999 || merged["dns"] != nil || sliceValue(merged["rules"])[0] != "MATCH,REJECT" {
		t.Fatalf("downloaded settings were not applied: %#v", merged)
	}
	group := sliceValue(merged["proxy-groups"])[0].(map[string]any)
	if !reflect.DeepEqual(group["proxies"], []any{"New", "Alpha", "DIRECT"}) {
		t.Fatalf("choices: %#v", group["proxies"])
	}
}

func TestMergeKeepsSubscriptionListenerLocal(t *testing.T) {
	incoming, _, err := Document([]byte("mixed-port: 7890\nallow-lan: true\nbind-address: '*'\nexternal-controller: 127.0.0.1:9090\nsecret: provider-secret\nproxies:\n  - name: Node\n    type: direct\n"))
	if err != nil {
		t.Fatal(err)
	}
	merged := Normalize(incoming)
	if merged["mixed-port"] != 7890 || merged["allow-lan"] != false || merged["bind-address"] != "127.0.0.1" {
		t.Fatalf("listener settings: %#v", merged)
	}
	if _, ok := merged["external-controller"]; ok {
		t.Fatal("subscription exposed a controller")
	}
	if _, ok := merged["secret"]; ok {
		t.Fatal("subscription controller secret was retained")
	}
	bare, _, err := Document([]byte("proxies:\n  - name: Node\n    type: direct\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Normalize(bare)["mixed-port"]; got != 17890 {
		t.Fatalf("default mixed port = %v", got)
	}
	if got := bare["profile"].(map[string]any)["store-selected"]; got != false {
		t.Fatalf("mihomo selection persistence = %v", got)
	}
}

func TestRefreshMetadataAndRejectInvalidWithoutReplacingCache(t *testing.T) {
	body := "proxies:\n  - name: Alpha\n    type: direct\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=10; download=20; total=100; expire=2000000000")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	dir := t.TempDir()
	s := NewStore(dir, filepath.Join(dir, "cache"), nil)
	if err := s.Add("work", server.URL); err != nil {
		t.Fatal(err)
	}
	result, err := s.Refresh(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview == nil || len(result.Preview.Added) != 1 || result.Total != 100 || result.ExpiresAt == nil || !result.ExpiresAt.Equal(time.Unix(2000000000, 0)) {
		t.Fatalf("result: %+v", result)
	}
	before, _ := s.Cached("work")
	body = "invalid subscription"
	if _, err := s.Refresh(context.Background(), "work"); err == nil {
		t.Fatal("invalid response accepted")
	}
	after, _ := s.Cached("work")
	if string(after) != string(before) {
		t.Fatal("working cache replaced")
	}
	entries, _ := s.List()
	if entries[0].URL != "" || entries[0].Total != 100 {
		t.Fatalf("metadata: %+v", entries)
	}
	data, _ := os.ReadFile(s.indexPath())
	if strings.Contains(string(data), "invalid subscription") {
		t.Fatal("bad response persisted")
	}
}

func TestGeneratedGroupsUseNodesAndProviders(t *testing.T) {
	for _, source := range []string{
		"proxies:\n  - name: Subscription\n    type: direct\n  - name: Subscription Auto\n    type: direct\nproxy-providers:\n  remote:\n    type: http\n    url: https://example.invalid/list\n",
		"proxy-providers:\n  remote:\n    type: http\n    url: https://example.invalid/list\n",
	} {
		doc, _, err := Document([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		groups := sliceValue(doc["proxy-groups"])
		if len(groups) != 5 {
			t.Fatalf("groups: %#v", groups)
		}
		if strings.Contains(source, "Subscription Auto") {
			if groups[0].(map[string]any)["name"] != "Subscription Auto-2" || groups[3].(map[string]any)["name"] != "Subscription-2" {
				t.Fatalf("name collisions: %#v", groups)
			}
		}
		auto := groups[0].(map[string]any)
		if auto["type"] != "url-test" || auto["url"] != "https://www.gstatic.com/generate_204" || auto["interval"] != 300 || auto["tolerance"] != 0 || !reflect.DeepEqual(auto["use"], []any{"remote"}) {
			t.Fatalf("auto: %#v", auto)
		}
		manual := groups[3].(map[string]any)
		if sliceValue(manual["proxies"])[0] != auto["name"] || sliceValue(manual["proxies"])[len(sliceValue(manual["proxies"]))-1] != "DIRECT" || !reflect.DeepEqual(manual["use"], []any{"remote"}) {
			t.Fatalf("manual: %#v", manual)
		}
		if groups[4].(map[string]any)["proxies"].([]any)[0] != manual["name"] {
			t.Fatalf("GLOBAL: %#v", groups[4])
		}
		if sliceValue(doc["rules"])[0] != "MATCH,"+manual["name"].(string) {
			t.Fatalf("rules: %#v", doc["rules"])
		}
	}
}

func TestGeneratedGroupsKeepGlobalNodeSelectable(t *testing.T) {
	for _, tc := range []struct{ name, source, alias string }{
		{"global node", "proxies:\n  - name: GLOBAL\n    type: direct\n", "GLOBAL-2"},
		{"occupied suffix", "proxies:\n  - name: GLOBAL\n    type: direct\n  - name: GLOBAL-2\n    type: direct\n", "GLOBAL-3"},
		{"inline provider", "proxy-providers:\n  local:\n    type: inline\n    payload:\n      - name: GLOBAL\n        type: direct\n", ""},
		{"inline provider collision", "proxies:\n  - name: GLOBAL\n    type: direct\nproxy-providers:\n  local:\n    type: inline\n    payload:\n      - name: GLOBAL-2\n        type: direct\n      - name: Subscription Auto\n        type: direct\n", "GLOBAL-3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := Document([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			groups := sliceValue(doc["proxy-groups"])
			manual := groups[3].(map[string]any)
			if groups[4].(map[string]any)["name"] != "GLOBAL" || !reflect.DeepEqual(groups[4].(map[string]any)["proxies"], []any{"Subscription"}) {
				t.Fatalf("global-mode selection: %#v", groups[4])
			}
			if tc.alias != "" {
				if sliceValue(doc["proxies"])[0].(map[string]any)["name"] != tc.alias || !containsChoice(manual, tc.alias) || !containsChoice(groups[0].(map[string]any), tc.alias) {
					t.Fatalf("renamed node unavailable: %#v", doc)
				}
			}
			if doc["proxy-providers"] != nil && !reflect.DeepEqual(manual["use"], []any{"local"}) {
				t.Fatalf("manual provider membership: %#v", manual)
			}
			if tc.name == "inline provider collision" && groups[0].(map[string]any)["name"] != "Subscription Auto-2" {
				t.Fatalf("generated group collision: %#v", groups[0])
			}
			validateGeneratedWithMihomo(t, doc)
		})
	}
}

func TestGeneratedGlobalAliasPreservesDownloadedReferences(t *testing.T) {
	source := `proxies:
  - name: GLOBAL
    type: direct
  - name: Relay
    type: direct
    dialer-proxy: GLOBAL
proxy-providers:
  Subscription Auto:
    type: inline
    override:
      dialer-proxy: GLOBAL
    payload:
      - name: ProviderNode
        type: direct
rules:
  - DOMAIN,example.com,GLOBAL
  - MATCH,GLOBAL
sub-rules:
  extra:
    - DOMAIN,example.org,GLOBAL,no-resolve
dns:
  nameserver: ["https://dns.example/dns-query#GLOBAL", "https://dns.example/other#DIRECT"]
  nameserver-policy:
    example.com: "https://dns.example/dns-query#GLOBAL&eth0"
`
	doc, _, err := Document([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	alias := sliceValue(doc["proxies"])[0].(map[string]any)["name"]
	if alias != "GLOBAL-2" {
		t.Fatalf("alias: %v", alias)
	}
	if sliceValue(doc["proxies"])[1].(map[string]any)["dialer-proxy"] != alias {
		t.Fatal("node dialer reference was not updated")
	}
	provider := doc["proxy-providers"].(map[string]any)["Subscription Auto"].(map[string]any)
	if provider["override"].(map[string]any)["dialer-proxy"] != alias {
		t.Fatal("provider dialer reference was not updated")
	}
	if sliceValue(doc["proxy-groups"])[0].(map[string]any)["name"] != "Subscription Auto-2" {
		t.Fatal("generated group collided with provider")
	}
	if !reflect.DeepEqual(doc["rules"], []any{"DOMAIN,example.com,GLOBAL-2", "MATCH,GLOBAL-2"}) {
		t.Fatalf("rules: %v", doc["rules"])
	}
	if !reflect.DeepEqual(doc["sub-rules"].(map[string]any)["extra"], []any{"DOMAIN,example.org,GLOBAL-2,no-resolve"}) {
		t.Fatalf("sub-rules: %v", doc["sub-rules"])
	}
	dns := doc["dns"].(map[string]any)
	if sliceValue(dns["nameserver"])[0] != "https://dns.example/dns-query#GLOBAL-2" || sliceValue(dns["nameserver"])[1] != "https://dns.example/other#DIRECT" {
		t.Fatalf("dns: %v", dns)
	}
	if dns["nameserver-policy"].(map[string]any)["example.com"] != "https://dns.example/dns-query#GLOBAL-2&eth0" {
		t.Fatalf("dns policy: %v", dns)
	}
	validateGeneratedWithMihomo(t, doc)
}

func containsChoice(group map[string]any, name string) bool {
	for _, item := range sliceValue(group["proxies"]) {
		if item == name {
			return true
		}
	}
	return false
}

func validateGeneratedWithMihomo(t *testing.T, doc map[string]any) {
	t.Helper()
	bin := os.Getenv("NAGI_MIHOMO_BIN")
	if bin == "" {
		t.Skip("set NAGI_MIHOMO_BIN to validate generated YAML with mihomo")
	}
	data, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(bin, "-t", "-f", path, "-d", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo rejected generated YAML: %v\n%s\n%s", err, data, output)
	}
}

func TestExistingGroupsKeepRulesAndMissingRulesGetFallback(t *testing.T) {
	source := "proxies:\n  - name: A\n    type: direct\nproxy-groups:\n  - name: Choice\n    type: select\n    proxies: [A, DIRECT]\n"
	doc, _, err := Document([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(sliceValue(doc["proxy-groups"])) != 1 || sliceValue(doc["rules"])[0] != "MATCH,Choice" {
		t.Fatalf("document: %#v", doc)
	}
	doc, _, err = Document([]byte(source + "rules: ['MATCH,REJECT']\n"))
	if err != nil || sliceValue(doc["rules"])[0] != "MATCH,REJECT" {
		t.Fatalf("rules: %#v %v", doc, err)
	}
}

func TestNormalizeIndependentListenersAndPreviewRedaction(t *testing.T) {
	doc, _, err := Document([]byte("proxies:\n  - name: A\n    type: direct\ndns:\n  listen: 0.0.0.0:5353\n  nameserver: [https://secret.invalid/query]\nlisteners:\n  - name: extra\n    type: socks\n    listen: 0.0.0.0\n    port: 1080\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err = NormalizeChecked(doc)
	if err != nil {
		t.Fatal(err)
	}
	if doc["dns"].(map[string]any)["listen"] != "127.0.0.1:5353" || sliceValue(doc["listeners"])[0].(map[string]any)["listen"] != "127.0.0.1" {
		t.Fatalf("listeners: %#v", doc)
	}
	p := Compare("test", "mihomo-yaml", doc, nil)
	data, err := json.Marshal(p.Changes)
	if err != nil || strings.Contains(string(data), "secret.invalid") || len(p.Changes) == 0 {
		t.Fatalf("preview: %s %v", data, err)
	}
	if p.RemoteNodesCompared != true {
		t.Fatalf("unexpected provider marker: %+v", p)
	}
	withProvider := map[string]any{"proxy-providers": map[string]any{"remote": map[string]any{"type": "http"}}}
	if Compare("test", "mihomo-yaml", withProvider, nil).RemoteNodesCompared {
		t.Fatal("remote nodes marked compared")
	}
	for _, bad := range []string{"dns:\n  listen: invalid\n", "listeners:\n  - type: socks\n    listen: 127.0.0.1:1080\n"} {
		invalid, _, err := Document([]byte("proxies:\n  - name: A\n    type: direct\n" + bad))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NormalizeChecked(invalid); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
}

func TestNormalizeOtherIndependentInbounds(t *testing.T) {
	doc, _, err := Document([]byte("proxies:\n  - name: A\n    type: direct\ntuic-server:\n  enable: true\n  listen: '[::]:443'\ntunnels:\n  - network: [tcp]\n    address: '0.0.0.0:8080'\n    target: 'example.com:80'\n  - 'udp,0.0.0.0:5353,example.com:53'\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err = NormalizeChecked(doc)
	if err != nil {
		t.Fatal(err)
	}
	if doc["tuic-server"].(map[string]any)["listen"] != "127.0.0.1:443" {
		t.Fatalf("tuic: %#v", doc["tuic-server"])
	}
	items := sliceValue(doc["tunnels"])
	if items[0].(map[string]any)["address"] != "127.0.0.1:8080" || items[1] != "udp,127.0.0.1:5353,example.com:53" {
		t.Fatalf("tunnels: %#v", items)
	}
	for _, tc := range []struct{ field, source string }{
		{"dns.listen", "dns:\n  listen: 'tcp://0.0.0.0:53'\n"},
		{"tuic-server.listen", "tuic-server:\n  enable: true\n  listen: invalid\n"},
		{"tunnels[0].address", "tunnels:\n  - 'tcp,invalid,example.com:80'\n"},
		{"ss-config", "ss-config: /tmp/server.json\n"},
		{"vmess-config", "vmess-config: /tmp/server.json\n"},
	} {
		bad, _, err := Document([]byte("proxies:\n  - name: A\n    type: direct\n" + tc.source))
		if err != nil {
			t.Fatal(err)
		}
		_, err = NormalizeChecked(bad)
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("%s: %v", tc.field, err)
		}
	}
}
