package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
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
		"proxy\x00groups":               {Data: map[string]any{"groups": []any{map[string]any{"name": "World Select", "all": []string{"Node A"}, "now": "Node A", "selected_status": "available"}}}},
		"proxy\x00show\x00World Select": {Data: map[string]any{"name": "World Select", "all": []string{"Node A"}, "now": "Node A", "selected_status": "available"}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	groups, err := ProxyPage().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := groups.Items[0].Next().Load(ctx, s)
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

func TestProxySearchSelectsReturnedNamesByNumber(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00groups":                                {Data: map[string]any{"groups": []any{map[string]any{"name": "World Select"}}}},
		"proxy\x00show\x00World Select":                  {Data: map[string]any{"name": "World Select", "all": []string{"Hong Kong A", "Tokyo"}}},
		"proxy\x00search\x00Hong":                        {Data: map[string]any{"groups": []any{map[string]any{"name": "World Select", "all": []string{"Hong Kong A"}}}}},
		"proxy\x00select\x00World Select\x00Hong Kong A": {Data: map[string]any{"group": "World Select", "node": "Hong Kong A"}},
	}}
	var out bytes.Buffer
	if code := RunPage(context.Background(), NewSession(runner, strings.NewReader("s\nHong\n1\n1\nq\n"), &out), ProxyPage()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !hasProxyCall(runner.calls, []string{"proxy", "select", "World Select", "Hong Kong A"}) {
		t.Fatalf("calls: %#v", runner.calls)
	}
	if strings.Contains(out.String(), "1. Tokyo") {
		t.Fatalf("nonmatching node displayed: %s", out.String())
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

func TestProxySwitchNeedsThreeSelectionsFromRoot(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00groups":                           {Data: map[string]any{"groups": []any{map[string]any{"name": "World Select"}}}},
		"proxy\x00show\x00World Select":             {Data: map[string]any{"name": "World Select", "all": []string{"Node A"}}},
		"proxy\x00select\x00World Select\x00Node A": {Data: map[string]any{"group": "World Select", "node": "Node A"}},
	}}
	root := &Page{Title: "Root", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Items: []Item{{Label: "Proxy", Next: ProxyPage}}}, nil
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("1\n1\n1\nq\n"), &out), root)
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

func TestProxyPaginationFilterAndBatchSelectSameNames(t *testing.T) {
	nodes := []string{}
	for i := 0; i < 12; i++ {
		nodes = append(nodes, fmt.Sprintf("Node %02d", i))
	}
	ms := 12
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Auto":              {Data: map[string]any{"name": "Auto", "type": "URLTest", "fixed": "Node 00", "now": "Node 00", "all": nodes}},
		"proxy\x00delays\x00Auto":            {Data: map[string]any{"results": []any{map[string]any{"proxy": "Node 11", "delay_ms": ms}, map[string]any{"proxy": "Node 00", "error": "latency test failed"}}}},
		"proxy\x00select\x00Auto\x00Node 11": {Data: map[string]any{"group": "Auto", "node": "Node 11"}},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("n\n2\n/node 11\n1\nl\n1\nr\n/\n1\nq\n"), &out), proxyGroupPage("Auto"))
	count := 0
	for _, call := range runner.calls {
		if reflect.DeepEqual(call, []string{"proxy", "select", "Auto", "Node 11"}) {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("same-node selections=%d calls=%#v", count, runner.calls)
	}
	if strings.Count(out.String(), "l. Test all nodes") < 7 {
		t.Fatalf("fixed action not retained: %s", out.String())
	}
	for _, text := range []string{"Page 2/2", "Selection: fixed to Node 00", "May clear a fixed choice", "Node 11 — 12 ms", "Filter: node 11"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %s", text, out.String())
		}
	}
}

func TestProxyMeasurementsPruneRemovedNodes(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Group":   {Data: map[string]any{"name": "Group", "all": []string{"A", "B"}}},
		"proxy\x00delays\x00Group": {Data: map[string]any{"results": []any{map[string]any{"proxy": "B", "delay_ms": 1}, map[string]any{"proxy": "A", "delay_ms": 2}}}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	page := proxyGroupPage("Group")
	view, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range view.Actions {
		if action.Key == "l" {
			if err := action.Action(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		}
	}
	runner.results["proxy\x00show\x00Group"] = Result{Data: map[string]any{"name": "Group", "all": []string{"A", "C"}}}
	view, err = page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 2 || view.Items[0].Name != "A" || view.Items[1].Name != "C" || strings.Contains(view.Items[1].Label, "ms") {
		t.Fatalf("pruned items=%#v", view.Items)
	}
	// A reappearing node does not inherit a stale measurement.
	runner.results["proxy\x00show\x00Group"] = Result{Data: map[string]any{"name": "Group", "all": []string{"A", "B"}}}
	view, _ = page.Load(context.Background(), s)
	if strings.Contains(view.Items[1].Label, "ms") {
		t.Fatalf("stale measurement=%q", view.Items[1].Label)
	}
}

func TestProxyFailedReadsKeepRecoveryActionsAndUnknownState(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{}}
	view, err := proxyGroupPage("Auto").Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
	if err == nil || len(view.Items) != 0 || len(view.Actions) < 3 || !strings.Contains(view.Context, "unknown") {
		t.Fatalf("view=%#v err=%v", view, err)
	}
}

func TestProxyBatchErrorsRetainUnderlyingStateExplanation(t *testing.T) {
	message := "measurement failed; fixed-node reset is unknown; run proxy show Auto"
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Auto":   {Data: map[string]any{"name": "Auto", "type": "Fallback", "all": []string{"Node"}}},
		"proxy\x00delays\x00Auto": {Code: 1, Error: &CLIError{Message: message}},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("l\na\nq\n"), &out), proxyGroupPage("Auto"))
	if !strings.Contains(out.String(), message) || !hasProxyCall(runner.calls, []string{"proxy", "auto", "Auto"}) {
		t.Fatalf("result: %s; calls=%#v", out.String(), runner.calls)
	}
}

func TestProxyLatencyFailureKeepsLastMeasurementsAndRedactsNames(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00https://secret.example/token":   {Data: map[string]any{"name": "https://secret.example/token", "all": []string{"Node"}}},
		"proxy\x00delays\x00https://secret.example/token": {Data: map[string]any{"results": []any{map[string]any{"proxy": "Node", "delay_ms": 7}}}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader(""), &out)
	page := proxyGroupPage("https://secret.example/token")
	view, _ := page.Load(context.Background(), s)
	var batch Item
	for _, action := range view.Actions {
		if action.Key == "l" {
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
	if strings.Contains(out.String(), "secret.example") || !strings.Contains(out.String(), "engine already changed; saving failed") {
		t.Fatalf("output: %s", out.String())
	}
}
