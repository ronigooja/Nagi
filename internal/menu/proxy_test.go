package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

type featureRunner struct {
	results map[string]Result
	calls   [][]string
}

func (r *featureRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string(nil), args...))
	if result, ok := r.results[strings.Join(args, "\x00")]; ok {
		return result
	}
	return Result{Code: 1, Error: &CLIError{Code: "unexpected", Message: "unexpected command"}}
}

func TestProxyGroupNodeNamesStaySingleArguments(t *testing.T) {
	ctx := context.Background()
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00group\x00list":                 {Data: map[string]any{"group_configs": []menuGroupConfig{{Name: "World Select", Type: "select"}}}},
		"proxy\x00group\x00show\x00World Select": {Data: map[string]any{"group_config": menuGroupConfig{Name: "World Select", Type: "select"}}},
		"proxy\x00show\x00World Select":          {Data: map[string]any{"name": "World Select", "node_info": ordinaryNodeInfo([]string{"Node A"}), "all": []string{"Node A"}, "now": "Node A", "selected_status": "available"}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	groups, err := groupManagementPage(s.language).Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	config, err := groups.Items[0].Next().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := groupAction(t, config, "group.select_node").Next().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"proxy", "select", "World Select", "Node A"}
	if !reflect.DeepEqual(detail.Items[0].Args, want) {
		t.Fatalf("select args = %#v, want %#v", detail.Items[0].Args, want)
	}
	if !strings.Contains(detail.Context, "Node A (available)") {
		t.Fatalf("selection status missing from context: %q", detail.Context)
	}
}

func hasProxyCall(calls [][]string, want []string) bool {
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			return true
		}
	}
	return false
}

func TestProxySwitchThroughManagementFromRoot(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00groups":                           {Data: map[string]any{"groups": []any{map[string]any{"name": "World Select"}}}},
		"proxy\x00group\x00list":                    {Data: map[string]any{"group_configs": []menuGroupConfig{{Name: "World Select", Type: "select"}}}},
		"proxy\x00group\x00show\x00World Select":    {Data: map[string]any{"group_config": menuGroupConfig{Name: "World Select", Type: "select"}}},
		"proxy\x00show\x00World Select":             {Data: map[string]any{"name": "World Select", "node_info": ordinaryNodeInfo([]string{"Node A"}), "all": []string{"Node A"}}},
		"proxy\x00select\x00World Select\x00Node A": {Data: map[string]any{"group": "World Select", "node": "Node A"}},
	}}
	root := &Page{Title: "Root", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Items: []Item{{Label: "Proxy", Next: ProxyPage}}}, nil
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("1\n1\n2\n1\n1\n1\nq\n"), &out), root)
	count := 0
	for _, call := range runner.calls {
		if len(call) > 1 && call[1] == "select" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("select commands=%d: %#v", count, runner.calls)
	}
	if strings.Contains(out.String(), "Proxy node:") || strings.Contains(out.String(), "yes") {
		t.Fatalf("extra flow: %s", out.String())
	}
}

func TestProxyPaginationAndBatchSelectSameNames(t *testing.T) {
	nodes := []string{}
	for i := 0; i < 12; i++ {
		nodes = append(nodes, fmt.Sprintf("Node %02d", i))
	}
	ms := 12
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Auto":              {Data: map[string]any{"name": "Auto", "type": "URLTest", "fixed": "Node 00", "now": "Node 00", "node_info": ordinaryNodeInfo(nodes), "all": nodes}},
		"proxy\x00delays\x00Auto":            {Data: map[string]any{"results": []any{map[string]any{"proxy": "Node 11", "delay_ms": ms}, map[string]any{"proxy": "Node 00", "error": "latency test failed"}}}},
		"proxy\x00select\x00Auto\x00Node 11": {Data: map[string]any{"group": "Auto", "node": "Node 11"}},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("17\n2\n9\n13\n1\n1\n0\n"), &out), proxyGroupPage("Auto"))
	count := 0
	for _, call := range runner.calls {
		if reflect.DeepEqual(call, []string{"proxy", "select", "Auto", "Node 11"}) {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("same-node selections=%d calls=%#v", count, runner.calls)
	}
	if strings.Count(out.String(), "Test all nodes") < 6 {
		t.Fatalf("fixed action not retained: %s", out.String())
	}
	for _, text := range []string{"Page 2/2", "Selection: fixed to Node 00", "May clear a fixed choice", "Node 11 — 12 ms"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %s", text, out.String())
		}
	}
}

func TestProxyMeasurementsPruneRemovedNodes(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Group":   {Data: map[string]any{"name": "Group", "node_info": ordinaryNodeInfo([]string{"A", "B"}), "all": []string{"A", "B"}}},
		"proxy\x00delays\x00Group": {Data: map[string]any{"results": []any{map[string]any{"proxy": "B", "delay_ms": 1}, map[string]any{"proxy": "A", "delay_ms": 2}}}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	page := proxyGroupPage("Group")
	view, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range view.Actions {
		if action.Label == "Test all nodes and select from results" {
			if err := action.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		}
	}
	runner.results["proxy\x00show\x00Group"] = Result{Data: map[string]any{"name": "Group", "node_info": ordinaryNodeInfo([]string{"A", "C"}), "all": []string{"A", "C"}}}
	view, err = page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 2 || view.Items[0].Name != "A" || view.Items[1].Name != "C" || strings.Contains(view.Items[1].Label, "ms") {
		t.Fatalf("pruned items=%#v", view.Items)
	}
	// A reappearing node does not inherit a stale measurement.
	runner.results["proxy\x00show\x00Group"] = Result{Data: map[string]any{"name": "Group", "node_info": ordinaryNodeInfo([]string{"A", "B"}), "all": []string{"A", "B"}}}
	view, _ = page.Load(context.Background(), s)
	if strings.Contains(view.Items[1].Label, "ms") {
		t.Fatalf("stale measurement=%q", view.Items[1].Label)
	}
}

func TestProxyNestedGroupLatencyStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delay map[string]any
		en    string
		zh    string
	}{
		{"success", map[string]any{"status": "ok", "delay_ms": 120}, "120 ms", "120 ms"},
		{"zero", map[string]any{"status": "ok", "delay_ms": 0}, "<1 ms", "<1 ms"},
		{"failure", map[string]any{"status": "failed"}, "latency test failed", "延迟测试失败"},
		{"untested", map[string]any{"status": "untested"}, "untested", "未测试"},
		{"unknown", map[string]any{"status": "ok"}, "latency unknown", "延迟未知"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
				runner := &featureRunner{results: map[string]Result{
					"proxy\x00show\x00Main": {Data: map[string]any{
						"all": []string{"Region"},
						"node_info": map[string]any{"Region": map[string]any{
							"kind": "group", "status": "available", "selected_delay": tc.delay,
						}},
					}},
				}}
				view, err := localizedproxyGroupPage(i18n.New(lang), "Main").Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
				if err != nil {
					t.Fatal(err)
				}
				want := tc.en
				if lang == i18n.SimplifiedChinese {
					want = tc.zh
				}
				if len(view.Items) != 1 || view.Items[0].Label != "Region — "+want {
					t.Fatalf("language=%s items=%+v", lang, view.Items)
				}
				if len(runner.calls) != 1 {
					t.Fatalf("display triggered extra commands: %v", runner.calls)
				}
			}
		})
	}
}

func TestProxyNestedGroupLatencyAfterBatchAndSelection(t *testing.T) {
	ctx := context.Background()
	data := map[string]any{
		"name": "Main", "all": []string{"DIRECT", "Region"}, "now": "Region",
		"node_info": map[string]any{"Region": map[string]any{"kind": "group", "status": "available"}},
	}
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Main": {Data: data},
		"proxy\x00delays\x00Main": {Data: map[string]any{"results": []any{
			map[string]any{"proxy": "Region", "delay_ms": 42},
			map[string]any{"proxy": "DIRECT", "delay_ms": 1},
		}}},
		"proxy\x00select\x00Main\x00Region": {Data: map[string]any{"group": "Main", "node": "Region"}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	page := proxyGroupPage("Main")
	view, err := page.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := profileAction(t, view, "Test all nodes and select from results").Action(ctx, s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		info map[string]any
		want string
	}{
		{map[string]any{"kind": "group", "status": "available", "selected_delay": map[string]any{"status": "ok", "delay_ms": 42}}, "42 ms"},
		{map[string]any{"kind": "group", "status": "unavailable", "selected_delay": map[string]any{"status": "failed"}}, "latency test failed"},
		{map[string]any{"kind": "group", "selected_delay": map[string]any{"status": "untested"}}, "untested"},
		{map[string]any{"kind": "group", "selected_builtin": "DIRECT"}, ""},
	} {
		data["node_info"] = map[string]any{"Region": tc.info}
		view, err = page.Load(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		want := "Region"
		if tc.want != "" {
			want += " — " + tc.want
		}
		want += " (selected)"
		if len(view.Items) != 2 || view.Items[0].Label != want || !view.Items[0].Selected || view.Items[1].Label != "DIRECT" {
			t.Fatalf("items=%+v want first=%q", view.Items, want)
		}
		if !reflect.DeepEqual(view.Items[0].Args, []string{"proxy", "select", "Main", "Region"}) {
			t.Fatal(view.Items[0].Args)
		}
		if result := s.Fetch(ctx, view.Items[0].Args...); result.Code != 0 {
			t.Fatal(result)
		}
	}
	for _, call := range runner.calls {
		if !reflect.DeepEqual(call, []string{"proxy", "show", "Main"}) &&
			!reflect.DeepEqual(call, []string{"proxy", "delays", "Main"}) &&
			!reflect.DeepEqual(call, []string{"proxy", "select", "Main", "Region"}) {
			t.Fatalf("unexpected command: %v", call)
		}
	}
}

func TestProxyFailedReadsKeepRecoveryActionsAndUnknownState(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{}}
	view, err := proxyGroupPage("Auto").Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
	if err == nil || len(view.Items) != 0 || len(view.Actions) < 3 || !strings.Contains(view.Context, "unknown") {
		t.Fatalf("view=%#v err=%v", view, err)
	}
}

func TestProxyGroupsReadFailureShowsRuntimeRecovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status Result
		state  string
		hint   string
		err    bool
	}{
		{"stopped", Result{Data: map[string]any{"running": false}}, "Mihomo: stopped |", "Start mihomo through Service", false},
		{"unexpected exit", Result{Data: map[string]any{"running": false, "unexpected_exit": true}}, "stopped (unexpected exit)", "clean stale runtime records", true},
		{"running", Result{Data: map[string]any{"running": true}}, "Mihomo: running |", "Check logs in Monitoring & Diagnostics", true},
		{"status failure", Result{Code: 1, Error: &CLIError{Message: "status failed: https://private.example/token"}}, "Mihomo: unknown |", "Open Service to check status", true},
		{"missing running field", Result{Data: map[string]any{}}, "Mihomo: unknown |", "Open Service to check status", true},
		{"null running field", Result{Data: map[string]any{"running": nil}}, "Mihomo: unknown |", "Open Service to check status", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &featureRunner{results: map[string]Result{
				"proxy\x00groups": {Code: 1, Error: &CLIError{Message: "mihomo control unavailable: https://private.example/token: socket missing"}},
				"status":          tc.status,
			}}
			var out bytes.Buffer
			if code := RunPage(context.Background(), NewSession(runner, strings.NewReader("0\n"), &out), proxyGroupsPage()); code != 0 {
				t.Fatalf("exit=%d", code)
			}
			for _, want := range []string{tc.state, tc.hint, "Restore saved selections"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "mihomo control unavailable:") != tc.err || strings.Contains(out.String(), "socket missing") != tc.err {
				t.Fatalf("read error visibility=%v output=%s", tc.err, out.String())
			}
			if strings.Contains(out.String(), ". Start / Stop") || strings.Contains(out.String(), ". Proxy Providers") || strings.Contains(out.String(), ". Tailscale") || strings.Contains(out.String(), "Data unavailable. Return to the parent page") {
				t.Fatalf("unsafe or misleading guidance: %s", out.String())
			}
			if tc.err && !strings.Contains(out.String(), "https://private.example/token") {
				t.Fatalf("actionable error lost URL context: %s", out.String())
			}
			if !reflect.DeepEqual(runner.calls, [][]string{{"proxy", "groups"}, {"status"}}) {
				t.Fatalf("reads must not start or retry the engine: %v", runner.calls)
			}
		})
	}
}

func TestProxyGroupsRecoverThroughRuntimePage(t *testing.T) {
	runner := &sequenceRunner{results: map[string][]Result{
		"proxy groups": {
			{Code: 1, Error: &CLIError{Message: "mihomo control unavailable: socket missing"}},
			{Data: map[string]any{"groups": []any{map[string]any{"name": "World Select"}}}},
		},
		"status": {
			{Data: map[string]any{"running": false}},
			{Data: map[string]any{"running": false}},
			{Data: map[string]any{"running": true}},
		},
		"start":                         {{Data: map[string]any{"running": true}}},
		"proxy group list":              {{Data: map[string]any{"group_configs": []menuGroupConfig{{Name: "World Select", Type: "select"}}}}},
		"proxy group show World Select": {{Data: map[string]any{"group_config": menuGroupConfig{Name: "World Select", Type: "select"}}}},
		"proxy show World Select": {
			{Data: map[string]any{"name": "World Select", "node_info": ordinaryNodeInfo([]string{"Node A"}), "all": []string{"Node A"}}},
			{Data: map[string]any{"name": "World Select", "node_info": ordinaryNodeInfo([]string{"Node A"}), "all": []string{"Node A"}, "now": "Node A"}},
		},
		"proxy select World Select Node A": {{Data: map[string]any{"group": "World Select", "node": "Node A"}}},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("3\n1\n0\n0\n1\n1\n0\n3\n1\n2\n1\n1\n1\nq\n"), &out), RootPage())
	for _, want := range [][]string{{"start"}, {"proxy", "select", "World Select", "Node A"}} {
		count := 0
		for _, call := range runner.calls {
			if reflect.DeepEqual(call, want) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%v dispatched %d times: %v; output=%s", want, count, runner.calls, out.String())
		}
	}
	if !strings.Contains(out.String(), "World Select") || !strings.Contains(out.String(), "Node A") {
		t.Fatal(out.String())
	}
}

func TestProxyGroupsSuccessfulReadDoesNotQueryRuntime(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{"proxy\x00groups": {Data: map[string]any{"groups": []any{}}}}}
	view, err := proxyGroupsPage().Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
	if err != nil || len(view.Actions) != 3 || !reflect.DeepEqual(runner.calls, [][]string{{"proxy", "groups"}}) {
		t.Fatalf("view=%#v err=%v calls=%v", view, err, runner.calls)
	}
}

func TestProxyGroupsRunningMenuKeepsThreeStableEntries(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		t.Run(string(lang), func(t *testing.T) {
			groups := make([]menuProxyGroup, 26)
			for i := range groups {
				groups[i].Name = fmt.Sprintf("Group %02d", i+1)
			}
			runner := &featureRunner{results: map[string]Result{
				"proxy\x00groups": {Data: map[string]any{"groups": groups}},
			}}
			var out bytes.Buffer
			s := NewSessionWithLanguage(runner, strings.NewReader("0\n"), &out, i18n.New(lang))
			page := localizedproxyGroupsPage(s.language)
			view := loadChainPage(t, page, s)
			if len(view.Items) != 0 || len(view.Actions) != 3 || view.Actions[0].ID != "group.create" || view.Actions[1].ID != "group.manage" || !reflect.DeepEqual(view.Actions[2].Args, []string{"proxy", "restore"}) {
				t.Fatalf("outer menu changed with engine groups: %+v", view)
			}
			RunPage(context.Background(), s, page)
			for _, text := range []string{"Group 01", "Group 26", "Page 1/", "第 1/", "Select a group, then a node"} {
				if strings.Contains(out.String(), text) {
					t.Fatalf("group list leaked into outer menu: %s", out.String())
				}
			}
		})
	}
}

func TestProxyBatchErrorsRetainUnderlyingStateExplanation(t *testing.T) {
	message := "measurement failed; fixed-node reset is unknown; run proxy show Auto"
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Auto":   {Data: map[string]any{"name": "Auto", "type": "Fallback", "node_info": ordinaryNodeInfo([]string{"Node"}), "all": []string{"Node"}}},
		"proxy\x00delays\x00Auto": {Code: 1, Error: &CLIError{Message: message}},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("4\n6\n0\n"), &out), proxyGroupPage("Auto"))
	if !strings.Contains(out.String(), message) || !hasProxyCall(runner.calls, []string{"proxy", "auto", "Auto"}) {
		t.Fatalf("result: %s; calls=%#v", out.String(), runner.calls)
	}
}

func TestProxyLatencyFailureKeepsLastMeasurementsAndURLContext(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00https://secret.example/token":   {Data: map[string]any{"name": "https://secret.example/token", "node_info": ordinaryNodeInfo([]string{"Node"}), "all": []string{"Node"}}},
		"proxy\x00delays\x00https://secret.example/token": {Data: map[string]any{"results": []any{map[string]any{"proxy": "Node", "delay_ms": 7}}}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader(""), &out)
	page := proxyGroupPage("https://secret.example/token")
	view, _ := page.Load(context.Background(), s)
	var batch Item
	for _, action := range view.Actions {
		if action.Label == "Test all nodes and select from results" {
			batch = action
		}
	}
	batch.Action(context.Background(), s)
	runner.results["proxy\x00delays\x00https://secret.example/token"] = Result{Code: 1, Error: &CLIError{Message: "engine already changed; saving failed"}}
	batch.Action(context.Background(), s)
	view, _ = page.Load(context.Background(), s)
	if !strings.Contains(view.Items[0].Label, "7 ms") {
		t.Fatalf("measurement lost: %#v", view.Items)
	}
	s.render(page, view, 0, 1)
	if !strings.Contains(out.String(), "https://secret.example/token") || !strings.Contains(out.String(), "engine already changed; saving failed") {
		t.Fatalf("output: %s", out.String())
	}
}
