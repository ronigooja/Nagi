package menu

import (
	"bytes"
	"context"
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
	groups, err := proxyGroupsPage().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := groups.Items[0].Next().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	node, err := detail.Items[3].Next().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"proxy", "select", "World Select", "Node A"}
	if !reflect.DeepEqual(node.Items[0].Args, want) {
		t.Fatalf("select args = %#v, want %#v", node.Items[0].Args, want)
	}
	if !strings.Contains(detail.Context, "Node A (available)") {
		t.Fatalf("selection status missing from context: %q", detail.Context)
	}
}

func TestProxySearchSelectsReturnedNamesByNumber(t *testing.T) {
	ctx := context.Background()
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00search\x00Hong":                        {Data: map[string]any{"groups": []any{map[string]any{"name": "World Select", "all": []string{"Hong Kong A"}}}}},
		"proxy\x00select\x00World Select\x00Hong Kong A": {Data: map[string]any{"group": "World Select", "node": "Hong Kong A"}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("Hong\n1\n1\n"), &out)
	if err := searchProxyNodes(ctx, s); err != nil {
		t.Fatal(err)
	}
	want := []string{"proxy", "select", "World Select", "Hong Kong A"}
	if !reflect.DeepEqual(runner.calls[len(runner.calls)-1], want) {
		t.Fatalf("last command = %#v, want %#v", runner.calls[len(runner.calls)-1], want)
	}
}
