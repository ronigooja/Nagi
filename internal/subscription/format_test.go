package subscription

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestMergePreservesLocalSettingsAndValidChoices(t *testing.T) {
	old, _, _ := Document([]byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\ndns:\n  enable: true\nproxies:\n  - name: Alpha\n    type: direct\n  - name: Gone\n    type: direct\nproxy-groups:\n  - name: Choice\n    type: select\n    proxies: [Alpha, Gone, DIRECT]\n"))
	next, _, _ := Document([]byte("mixed-port: 9999\nrules:\n  - MATCH,REJECT\nproxies:\n  - name: Alpha\n    type: direct\n  - name: New\n    type: direct\nproxy-groups:\n  - name: Choice\n    type: select\n    proxies: [New, Alpha, DIRECT]\n"))
	merged := Merge(next, old)
	if merged["mixed-port"] != 7890 || merged["dns"] == nil || sliceValue(merged["rules"])[0] != "MATCH,DIRECT" {
		t.Fatalf("lost local settings: %#v", merged)
	}
	group := groupMap(merged["proxy-groups"])["Choice"]
	if !reflect.DeepEqual(group["proxies"], []any{"Alpha", "DIRECT", "New"}) {
		t.Fatalf("choices: %#v", group["proxies"])
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
