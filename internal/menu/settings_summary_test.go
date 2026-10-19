package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestNetworkSummariesAndDetailsInBothLanguages(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		for _, goos := range []string{"linux", "darwin"} {
			t.Run(string(language)+"/"+goos, func(t *testing.T) {
				runner := &fakeRunner{results: map[string]Result{
					"tun status":          {Data: map[string]any{"enabled": true}},
					"ports status":        {Data: map[string]any{"http": 0, "socks": 0, "mixed": 17890}},
					"lan status":          {Data: map[string]any{"enabled": false, "bind_address": "127.0.0.1"}},
					"system-proxy status": {Data: map[string]any{"enabled": false}},
				}}
				var out bytes.Buffer
				lang := i18n.New(language)
				s := NewSessionWithLanguage(runner, strings.NewReader(""), &out, lang)
				view, err := localizedtrafficPageForOS(lang, goos).Load(context.Background(), s)
				if err != nil {
					t.Fatal(err)
				}
				want := "TUN: On\nListener Ports: Mixed 17890\nLAN Access: Off"
				proxy := "System Proxy: Off"
				if language == i18n.SimplifiedChinese {
					want = "TUN: 开\n监听端口: 混合 17890\n局域网访问: 关"
					proxy = "系统代理: 关"
				}
				if goos == "darwin" {
					want += "\n" + proxy
				}
				if view.Context != want {
					t.Fatalf("summary=%q, want %q", view.Context, want)
				}
				// Child settings use the same summary, while explicit details
				// still include the disabled listeners and loopback address.
				for _, action := range view.Actions {
					if action.Next == nil {
						continue
					}
					child, err := action.Next().Load(context.Background(), s)
					if err != nil || !strings.Contains(view.Context, child.Context) {
						t.Fatalf("child summary=%q err=%v", child.Context, err)
					}
				}
				for _, command := range []string{"ports", "lan"} {
					s.ShowResult([]string{command, "status"}, runner.results[command+" status"])
				}
				if !strings.Contains(out.String(), "127.0.0.1") || !strings.Contains(out.String(), "0") || !strings.Contains(out.String(), "17890") {
					t.Fatalf("details lost configuration: %s", &out)
				}
				for _, call := range runner.calls {
					if len(call) != 2 || call[1] != "status" {
						t.Fatalf("summary dispatched a mutation: %v", call)
					}
				}
			})
		}
	}
}

func TestSettingsSummariesDistinguishOffUnknownAndFailures(t *testing.T) {
	for _, tc := range []struct {
		command, label, data, want string
	}{
		{"tun", "TUN", `{"enabled":false}`, "TUN: Off"},
		{"tun", "TUN", `{}`, "TUN: unknown"},
		{"tun", "TUN", `{"enabled":null}`, "TUN: unknown"},
		{"tun", "TUN", `{"enabled":"false"}`, "TUN: unknown"},
		{"ports", "Listener Ports", `{"http":0,"socks":0,"mixed":0}`, "Listener Ports: None"},
		{"ports", "Listener Ports", `{}`, "Listener Ports: HTTP unknown, SOCKS unknown, Mixed unknown"},
		{"ports", "Listener Ports", `{"http":null,"socks":-1,"mixed":17890}`, "Listener Ports: HTTP unknown, SOCKS unknown, Mixed 17890"},
		{"ports", "Listener Ports", `{"http":65536,"socks":"0","mixed":1.5}`, "Listener Ports: HTTP unknown, SOCKS unknown, Mixed unknown"},
		{"lan", "LAN Access", `{"enabled":true,"bind_address":"192.0.2.10"}`, "LAN Access: On — Bind Address: 192.0.2.10"},
		{"lan", "LAN Access", `{"enabled":true}`, "LAN Access: On — Bind Address: unknown"},
		{"lan", "LAN Access", `{"bind_address":"127.0.0.1"}`, "LAN Access: unknown"},
	} {
		t.Run(tc.command+"/"+tc.data, func(t *testing.T) {
			got := localizedSettingsSummary(i18n.New(i18n.English), Result{Data: json.RawMessage(tc.data)}, []string{tc.command, "status"}, tc.label)
			if got != tc.want {
				t.Fatalf("summary=%q, want %q", got, tc.want)
			}
		})
	}
	for _, result := range []Result{{Code: 1}, {Error: &CLIError{Message: "read failed"}}} {
		got := localizedSettingsSummary(i18n.New(i18n.English), result, []string{"tun", "status"}, "TUN")
		if !strings.HasPrefix(got, "TUN: unknown — ") || !strings.Contains(got, "read failed") {
			t.Fatalf("read failure hidden: %q", got)
		}
	}
}

func TestDNSAndHelperSummariesUseCommandContracts(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		lang := i18n.New(language)
		runner := &fakeRunner{results: map[string]Result{
			"dns status": {Data: map[string]any{"enabled": true, "policy": "rules", "tun_enabled": false}},
			"privileged-helper status": {Data: map[string]any{
				"service": map[string]any{"installed": true, "active": false}, "selected_backend": "privileged", "available": false,
			}},
		}}
		s := NewSessionWithLanguage(runner, strings.NewReader(""), &bytes.Buffer{}, lang)
		view, _ := localizedDNSPage(lang).Load(context.Background(), s)
		want := "DNS: On\nUpstream Routing: Follow Rules\nTUN: Off"
		helperWant := "Privileged helper: installed\nService: inactive\nEngine Backend: Privileged"
		if language == i18n.SimplifiedChinese {
			want = "DNS: 开\n上游路由: 遵循规则\nTUN: 关"
			helperWant = "特权辅助程序: 已安装\n服务: 未活动\n引擎运行模式: 特权模式"
		}
		if view.Context != want {
			t.Fatalf("%s DNS summary=%q", language, view.Context)
		}
		for _, index := range []int{0, 2} {
			child, _ := view.Actions[index].Next().Load(context.Background(), s)
			if !strings.Contains(want, child.Context) {
				t.Fatalf("%s DNS child summary=%q", language, child.Context)
			}
		}
		view, _ = localizedhelperPageForOS(lang, "darwin").Load(context.Background(), s)
		if view.Context != helperWant || !reflect.DeepEqual(runner.calls[len(runner.calls)-1], []string{"privileged-helper", "status"}) {
			t.Fatalf("%s helper summary=%q calls=%v", language, view.Context, runner.calls)
		}
		// An unreadable installed field must not hide a known backend or
		// invent installation/activity from old top-level field names.
		runner.results["privileged-helper status"] = Result{Data: map[string]any{"installed": true, "enabled": true, "backend": "privileged", "selected_backend": "user"}}
		view, _ = localizedhelperPageForOS(lang, "darwin").Load(context.Background(), s)
		if strings.Count(view.Context, menuState(lang, "unknown")) != 2 || !strings.Contains(view.Context, menuText(lang, "menu.summary.user_backend", "User")) {
			t.Fatalf("%s helper inferred stale fields: %s", language, view.Context)
		}
		startup := localizedloginStartupSummary(lang, Result{Data: map[string]any{
			"service": map[string]any{"enabled": true}, "engine": map[string]any{"running": true},
		}})
		if !strings.Contains(startup, menuState(lang, "running")) {
			t.Fatalf("%s startup engine state is not localized: %s", language, startup)
		}
	}
}
