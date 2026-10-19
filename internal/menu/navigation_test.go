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
		traffic := "TUN & LAN"
		if goos == "darwin" {
			traffic = "System Proxy & TUN"
		}
		want := []string{"Start / Stop", "Select Node", "Proxy Mode", traffic, "Subscriptions", "Profiles", "Connections & Troubleshooting", "Settings & Maintenance"}
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
	want := []string{"Connections", "Logs and traffic", "Basic checks", "Detailed diagnostics", "Export diagnostics", "Repair startup state", "Clean stale runtime records", "Show version"}
	for _, goos := range []string{"linux", "darwin"} {
		page := diagnosticsPageForOS(goos)
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{})
		view, err := page.Load(context.Background(), s)
		if err != nil || page.Title != "Connections & Troubleshooting" {
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
			{3, []string{"doctor"}}, {4, []string{"diagnostics"}},
			{6, []string{"startup", "check"}}, {7, []string{"recover"}}, {8, []string{"version"}},
		} {
			runner := &fakeRunner{}
			RunPage(context.Background(), NewSession(runner, strings.NewReader(fmt.Sprintf("%d\n0\n", tc.number)), &bytes.Buffer{}), page)
			if !reflect.DeepEqual(runner.calls, [][]string{tc.args}) {
				t.Fatalf("%s number %d dispatched %v, want %v", goos, tc.number, runner.calls, tc.args)
			}
		}
		for _, number := range []int{1, 2} {
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(fmt.Sprintf("%d\n0\n0\n", number)), &out), page)
			if !strings.Contains(out.String(), "\n"+want[number-1]+"\n") || strings.Count(out.String(), "\nConnections & Troubleshooting\n") != 2 {
				t.Fatalf("%s child %d back navigation: %s", goos, number, &out)
			}
		}
	}
}

func TestSettingsNumericChildrenReturnOnBothPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		page := advancedPageForOS(goos)
		view, err := page.Load(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for index, item := range view.Actions {
			var out bytes.Buffer
			runner := &fakeRunner{}
			RunPage(context.Background(), NewSession(runner, strings.NewReader(fmt.Sprintf("%d\n0\n0\n", index+1)), &out), page)
			if !strings.Contains(out.String(), "\n"+item.Next().Title+"\n") || strings.Count(out.String(), "\nSettings & Maintenance\n") != 2 {
				t.Fatalf("%s %s navigation failed: %s", goos, item.Label, &out)
			}
		}
	}
}

func TestTrafficPagePlatformOrderAndChildMapping(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		page := trafficPageForOS(goos)
		want := []string{"TUN", "LAN Access", "Listener ports"}
		if goos == "darwin" {
			want = []string{"System Proxy", "TUN", "LAN Access", "Listener ports"}
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
