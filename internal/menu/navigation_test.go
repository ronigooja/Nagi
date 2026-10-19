package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestHomeOrderAndNumericDestinations(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		want := []string{"Service", "Network Access", "Proxies", "Routing & DNS", "Profiles & Subscriptions", "Rewrite & Capture", "Monitoring & Diagnostics", "Maintenance"}
		for index, title := range want {
			t.Run(goos+"/"+title, func(t *testing.T) {
				var out bytes.Buffer
				runner := &fakeRunner{}
				s := NewSession(runner, strings.NewReader(fmt.Sprintf("%d\n0\n0\n", index+1)), &out)
				page := rootPageForOS(goos)
				view, err := page.Load(context.Background(), s)
				if err != nil {
					t.Fatal(err)
				}
				choices := numberedChoices(view, 0)
				var labels []string
				for _, choice := range choices {
					labels = append(labels, choice.Item.Label)
				}
				if !reflect.DeepEqual(labels, want) {
					t.Fatalf("home order=%v, want %v", labels, want)
				}
				if code := RunPage(context.Background(), s, page); code != 0 {
					t.Fatalf("exit=%d", code)
				}
				if !strings.Contains(out.String(), "\n"+title+"\n") || strings.Count(out.String(), "\nNagi menu\n") != 2 {
					t.Fatalf("numeric entry or back navigation failed: %s", &out)
				}
			})
		}
	}
}

func TestTroubleshootingOrderAndDispatchOnBothPlatforms(t *testing.T) {
	want := []string{"Connections", "Traffic", "Logs", "Basic checks", "Detailed diagnostics", "Export diagnostics"}
	for _, goos := range []string{"linux", "darwin"} {
		page := diagnosticsPageForOS(goos)
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{})
		view, err := page.Load(context.Background(), s)
		if err != nil || page.Title != "Monitoring & Diagnostics" {
			t.Fatalf("%s page=%+v err=%v", goos, page, err)
		}
		var labels []string
		for _, item := range view.Actions {
			labels = append(labels, item.Label)
		}
		if !reflect.DeepEqual(labels, want) {
			t.Fatalf("%s order=%v", goos, labels)
		}
		for _, tc := range []struct {
			number int
			args   []string
		}{
			{4, []string{"doctor"}}, {5, []string{"diagnostics"}},
		} {
			runner := &fakeRunner{}
			RunPage(context.Background(), NewSession(runner, strings.NewReader(fmt.Sprintf("%d\n0\n", tc.number)), &bytes.Buffer{}), page)
			if !reflect.DeepEqual(runner.calls, [][]string{tc.args}) {
				t.Fatalf("%s number %d dispatched %v, want %v", goos, tc.number, runner.calls, tc.args)
			}
		}
		for _, number := range []int{1, 2, 3} {
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(fmt.Sprintf("%d\n0\n0\n", number)), &out), page)
			if !strings.Contains(out.String(), "\n"+want[number-1]+"\n") || strings.Count(out.String(), "\nMonitoring & Diagnostics\n") != 2 {
				t.Fatalf("%s child %d back navigation: %s", goos, number, &out)
			}
		}
	}
}

func TestCategoryNumericChildrenReturn(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for _, page := range []*Page{ProxyPage(), RoutingPage(), ProfilesAndSubscriptionsPage(), RewriteAndCapturePage(), serviceManagementPageForOS(goos)} {
			view, err := page.Load(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}))
			if err != nil {
				t.Fatal(err)
			}
			for index, item := range view.Actions {
				if item.Next == nil {
					continue
				}
				var out bytes.Buffer
				RunPage(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(fmt.Sprintf("%d\n0\n0\n", index+1)), &out), page)
				if !strings.Contains(out.String(), "\n"+item.Next().Title+"\n") || strings.Count(out.String(), "\n"+page.Title+"\n") != 2 {
					t.Fatalf("%s %s navigation failed: %s", goos, item.Label, &out)
				}
			}
		}
	}
}

func TestTrafficPagePlatformOrderAndChildMapping(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		page := trafficPageForOS(goos)
		want := []string{"TUN", "LAN Access", "Listener Ports"}
		if goos == "darwin" {
			want = []string{"System Proxy", "TUN", "LAN Access", "Listener Ports"}
		}
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{})
		view, err := page.Load(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		var labels []string
		for index, item := range view.Actions {
			labels = append(labels, item.Label)
			if item.Next == nil {
				if !reflect.DeepEqual(item.Args, []string{"ports", "status"}) {
					t.Fatalf("%s listener args=%v", goos, item.Args)
				}
				continue
			}
			child := item.Next()
			childView, err := child.Load(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			command := map[string]string{"TUN": "tun", "System Proxy": "system-proxy", "LAN Access": "lan"}[item.Label]
			if len(childView.Actions) != 3 {
				t.Fatalf("%s %s actions=%v", goos, item.Label, childView.Actions)
			}
			for n, verb := range []string{"enable", "disable", "status"} {
				if command == "lan" && verb == "enable" {
					if childView.Actions[n].Action == nil {
						t.Fatal("missing LAN address form")
					}
					continue
				}
				if !reflect.DeepEqual(childView.Actions[n].Args, []string{command, verb}) {
					t.Fatalf("%s %s choice %d args=%v", goos, item.Label, n+1, childView.Actions[n].Args)
				}
			}
			var out bytes.Buffer
			runner := &fakeRunner{}
			RunPage(context.Background(), NewSession(runner, strings.NewReader(fmt.Sprintf("%d\n2\n0\n0\n", index+1)), &out), page)
			dispatches := 0
			for _, call := range runner.calls {
				if reflect.DeepEqual(call, []string{command, "disable"}) {
					dispatches++
				}
			}
			if dispatches != 1 {
				t.Fatalf("%s %s disable dispatched %d times: %v", goos, item.Label, dispatches, runner.calls)
			}
			if strings.Count(out.String(), "\n"+page.Title+"\n") != 2 || !strings.Contains(out.String(), "\n"+child.Title+"\n") {
				t.Fatalf("%s child back navigation: %s", goos, &out)
			}
		}
		if !reflect.DeepEqual(labels, want) {
			t.Fatalf("%s traffic order=%v", goos, labels)
		}
	}
}

func TestCategoryInventoryAndReadOnlyEntry(t *testing.T) {
	for _, tc := range []struct {
		page *Page
		want []string
	}{
		{ProxyPage(), []string{"Proxy Groups", "Proxy Providers", "Tailscale"}},
		{RoutingPage(), []string{"Proxy Mode", "Routing Rules", "DNS"}},
		{ProfilesAndSubscriptionsPage(), []string{"Profiles", "Subscriptions", "Current Configuration"}},
		{RewriteAndCapturePage(), []string{"Request Rewrite", "Request Capture", "Certificate Authority"}},
	} {
		runner := &fakeRunner{}
		view, err := tc.page.Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
		var labels []string
		for _, choice := range numberedChoices(view, 0) {
			labels = append(labels, choice.Item.Label)
		}
		if err != nil || !reflect.DeepEqual(labels, tc.want) || len(runner.calls) != 0 {
			t.Fatalf("%s labels=%v calls=%v err=%v", tc.page.Title, labels, runner.calls, err)
		}
	}
}

func TestCurrentConfigurationReachableWithoutReadableProfiles(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"profile list": {Code: 1, Error: &CLIError{Message: "profiles unavailable"}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("5\n1\n0\n3\n2\n0\n0\n0\n"), &out), RootPage())
	if !hasProxyCall(runner.calls, []string{"config", "validate"}) || !strings.Contains(out.String(), "profiles unavailable") {
		t.Fatalf("configuration inaccessible after profile failure: calls=%v output=%s", runner.calls, &out)
	}
	for _, call := range runner.calls {
		if len(call) > 0 && call[0] != "status" && !reflect.DeepEqual(call, []string{"profile", "list"}) && !reflect.DeepEqual(call, []string{"config", "validate"}) {
			t.Fatalf("unexpected command: %v", call)
		}
	}
}

func TestServiceRecoveryNumericDispatchOnBothPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for index, args := range [][]string{{"startup", "check"}, {"recover"}} {
			runner := &fakeRunner{}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(runner, strings.NewReader(fmt.Sprintf("7\n%d\n0\n0\n", index+1)), &out), serviceManagementPageForOS(goos))
			var operations [][]string
			for _, call := range runner.calls {
				if !reflect.DeepEqual(call, []string{"status"}) {
					operations = append(operations, call)
				}
			}
			if !reflect.DeepEqual(operations, [][]string{args}) || strings.Count(out.String(), "\nService\n") != 2 {
				t.Fatalf("%s operations=%v output=%s", goos, operations, &out)
			}
		}
	}
}

func TestRewriteFailureDoesNotHideCaptureOrCertificateAuthority(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"request-rewrite status": {Code: 1, Error: &CLIError{Message: "rewrite status unavailable"}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("1\n0\n2\n0\n3\n0\n0\n"), &out), RewriteAndCapturePage())
	for _, title := range []string{"Request Rewrite", "Request Capture", "Certificate Authority"} {
		if !strings.Contains(out.String(), "\n"+title+"\n") {
			t.Fatalf("missing %s: %s", title, &out)
		}
	}
	view, err := RequestRewritePage().Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
	if err == nil || len(view.Actions) != 1 || view.Actions[0].Label != "Recovery" {
		t.Fatalf("failed rewrite page actions=%v err=%v", view.Actions, err)
	}
	for _, call := range runner.calls {
		if len(call) < 2 || (call[1] != "status" && call[1] != "capture" && call[1] != "ca") {
			t.Fatalf("unexpected command on page entry: %v", call)
		}
	}
}

func TestServiceFailedStatusKeepsLifecycleControls(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for number, command := range []string{"start", "stop", "restart", "status", "quit"} {
			runner := &fakeRunner{results: map[string]Result{"status": {Code: 1, Error: &CLIError{Message: "status unavailable"}}}}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(runner, strings.NewReader(fmt.Sprintf("%d\n0\n", number+1)), &out), serviceManagementPageForOS(goos))
			count := 0
			for _, call := range runner.calls {
				if reflect.DeepEqual(call, []string{command}) {
					count++
				}
			}
			want := 1
			if command == "status" {
				want = 3
			} // Entry, selected read, and reload.
			if count != want || !strings.Contains(out.String(), "Mihomo: unknown") {
				t.Fatalf("%s %s count=%d calls=%v output=%s", goos, command, count, runner.calls, &out)
			}
		}
	}
}

func TestRewriteCategoryCertificatePlatformGuard(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		runner := &fakeRunner{}
		session := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
		view, err := rewriteAndCapturePageForOS(goos).Load(context.Background(), session)
		if err != nil {
			t.Fatal(err)
		}
		ca, err := view.Actions[2].Next().Load(context.Background(), session)
		if err != nil {
			t.Fatal(err)
		}
		trust := false
		for _, item := range ca.Actions {
			if item.Label == "Install CA trust" {
				trust = true
			}
		}
		if trust != (goos == "darwin") {
			t.Fatalf("%s trust actions=%v", goos, ca.Actions)
		}
	}
}
