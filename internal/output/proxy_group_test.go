package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/profile"
)

func TestConfiguredGroupHumanAndJSON(t *testing.T) {
	data := map[string]any{"profile": "work", "group_configs": []profile.ProxyGroupConfig{{Name: "My Group", Type: "url-test", Proxies: []string{"B", "A"}, Use: []string{"Feed"}, URL: "https://example.invalid/test", Interval: 300, Tolerance: 42}}}
	var en, zh bytes.Buffer
	if err := WriteLocalized(&en, false, i18n.New(i18n.English), data); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalized(&zh, false, i18n.New(i18n.SimplifiedChinese), data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(en.String(), "Nodes and groups: B, A") || !strings.Contains(zh.String(), "节点和代理组: B, A") || !strings.Contains(zh.String(), "容差（毫秒）: 42") {
		t.Fatalf("%s\n%s", &en, &zh)
	}
	en.Reset()
	zh.Reset()
	_ = WriteLocalized(&en, true, i18n.New(i18n.English), data)
	_ = WriteLocalized(&zh, true, i18n.New(i18n.SimplifiedChinese), data)
	if en.String() != zh.String() || !strings.Contains(en.String(), `"group_configs"`) {
		t.Fatalf("%s\n%s", &en, &zh)
	}
}
