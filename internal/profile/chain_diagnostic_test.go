package profile

import (
	"errors"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"gopkg.in/yaml.v3"
)

func TestChainMissingReferenceNamesAndPrivacy(t *testing.T) {
	for _, tc := range []struct{ name, source, owner, target, reason string }{
		{"node", "proxies: [{name: app-node, dialer-proxy: missing-hop}]", "app-node", "missing-hop", "出站不存在"},
		{"group", "proxy-groups: [{name: app-group, proxies: [missing-node]}]", "app-group", "missing-node", "出站不存在"},
		{"provider dialer", "proxy-providers: {feed: {override: {dialer-proxy: missing-hop}}}", "feed", "missing-hop", "出站不存在"},
		{"provider use", "proxy-groups: [{name: app-group, use: [missing-feed]}]", "app-group", "missing-feed", "代理提供者不存在"},
		{"private names", "proxies: [{name: 'https://example.invalid/?token=PRIVATE', dialer-proxy: 'https://example.invalid/?key=SECRET'}]", "https://example.invalid/?token=%5Bredacted%5D", "https://example.invalid/?key=%5Bredacted%5D", "出站不存在"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg map[string]any
			if err := yaml.Unmarshal([]byte(tc.source), &cfg); err != nil {
				t.Fatal(err)
			}
			err := chainValidateGraph(cfg)
			var message *i18n.Message
			if !errors.As(err, &message) || !i18n.ValidDescriptor(message.Descriptor()) {
				t.Fatalf("missing descriptor: %v", err)
			}
			for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
				text := i18n.New(language).Render(message)
				if !strings.Contains(text, tc.owner) || !strings.Contains(text, tc.target) || strings.Contains(text, "PRIVATE") || strings.Contains(text, "SECRET") {
					t.Fatal(text)
				}
				if language == i18n.SimplifiedChinese && !strings.Contains(text, tc.reason) {
					t.Fatal(text)
				}
			}
			if strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "SECRET") {
				t.Fatal(err)
			}
		})
	}
}
