package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

type changingProviderRunner struct {
	calls [][]string
	shows int
	fail  bool
}

func (r *changingProviderRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string(nil), args...))
	switch strings.Join(args, " ") {
	case "proxy groups":
		return Result{Data: map[string]any{"groups": []any{}}}
	case "proxy providers":
		return Result{Data: map[string]any{"providers": []any{map[string]any{"name": "work", "type": "HTTP", "nodes": []string{"Old"}}}}}
	case "proxy provider show work":
		r.shows++
		if r.fail {
			return Result{Code: 1, Error: &CLIError{Message: "provider unavailable"}}
		}
		node := "Old"
		if r.shows > 1 {
			node = "New"
		}
		return Result{Data: map[string]any{"name": "work", "type": "HTTP", "nodes": []string{node}}}
	default:
		return Result{Data: map[string]any{}}
	}
}

func TestProviderSessionRefreshesAfterUpdateAndDispatchesActions(t *testing.T) {
	r := &changingProviderRunner{}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\nu\nc\n1\nq\n"), &out), providerListPage())
	for _, want := range [][]string{{"proxy", "provider", "show", "work"}, {"proxy", "provider", "update", "work"}, {"proxy", "provider", "healthcheck", "work"}, {"proxy", "delay", "New"}} {
		if !hasProxyCall(r.calls, want) {
			t.Fatalf("missing %v in %#v", want, r.calls)
		}
	}
	if hasProxyCall(r.calls, []string{"proxy", "delay", "Old"}) || !strings.Contains(out.String(), "New (measure latency)") {
		t.Fatalf("stale provider node: %s", out.String())
	}
}

func TestProviderEntryAndHealthcheckKeysDispatchThroughSession(t *testing.T) {
	r := &changingProviderRunner{}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("v\n1\nc\nq\n"), &out), ProxyPage())
	if !hasProxyCall(r.calls, []string{"proxy", "providers"}) ||
		!hasProxyCall(r.calls, []string{"proxy", "provider", "show", "work"}) ||
		!hasProxyCall(r.calls, []string{"proxy", "provider", "healthcheck", "work"}) {
		t.Fatalf("provider navigation did not dispatch: %#v; output=%s", r.calls, out.String())
	}
}

func TestProviderReadFailureClearsNodesAndKeepsRecovery(t *testing.T) {
	r := &changingProviderRunner{fail: true}
	view, err := providerDetailPage("work").Load(context.Background(), NewSession(r, strings.NewReader(""), &bytes.Buffer{}))
	if err == nil || len(view.Items) != 0 || !strings.Contains(view.Context, "unknown") || !reflect.DeepEqual([]string{view.Actions[0].Key, view.Actions[1].Key}, []string{"u", "c"}) {
		t.Fatalf("view=%#v err=%v", view, err)
	}
}
