package menu

import (
	"bytes"
	"context"
	"fmt"
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
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n2\n3\n1\n"), &out), providerListPage())
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
	RunPage(context.Background(), NewSession(r, strings.NewReader("2\n1\n3\n"), &out), ProxyPage())
	if !hasProxyCall(r.calls, []string{"proxy", "providers"}) ||
		!hasProxyCall(r.calls, []string{"proxy", "provider", "show", "work"}) ||
		!hasProxyCall(r.calls, []string{"proxy", "provider", "healthcheck", "work"}) {
		t.Fatalf("provider navigation did not dispatch: %#v; output=%s", r.calls, out.String())
	}
}

func TestProviderReadFailureClearsNodesAndKeepsRecovery(t *testing.T) {
	r := &changingProviderRunner{fail: true}
	view, err := providerDetailPage("work").Load(context.Background(), NewSession(r, strings.NewReader(""), &bytes.Buffer{}))
	if err == nil || len(view.Items) != 0 || !strings.Contains(view.Context, "unknown") || !reflect.DeepEqual([]string{view.Actions[0].Label, view.Actions[1].Label}, []string{"Update provider", "Start healthcheck"}) {
		t.Fatalf("view=%#v err=%v", view, err)
	}
}

func TestProviderAddAccessibleWithEmptyOrUnavailableRead(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable=%t", unavailable), func(t *testing.T) {
			read := Result{Data: map[string]any{"providers": []any{}}}
			if unavailable {
				read = Result{Code: 1, Error: &CLIError{Message: "mihomo is stopped"}}
			}
			r := &fakeRunner{results: map[string]Result{
				"proxy providers": read,
				"proxy provider add work https://example.invalid/feed Work Group 3600": {Data: map[string]any{"added": true, "group_created": true}},
			}}
			var out bytes.Buffer
			code := RunPage(context.Background(), NewSession(r, strings.NewReader("1\nwork\nhttps://example.invalid/feed\nWork Group\n\n0\n"), &out), providerListPage())
			want := [][]string{
				{"proxy", "providers"},
				{"proxy", "provider", "add", "work", "https://example.invalid/feed", "Work Group", "3600"},
				{"proxy", "providers"},
			}
			if code != 0 || !reflect.DeepEqual(r.calls, want) {
				t.Fatalf("code=%d calls=%v output=%s", code, r.calls, out.String())
			}
			for _, text := range []string{"Add provider", "default 3600", "A stopped engine stays stopped", "Routing rules are unchanged", "Global mode"} {
				if !strings.Contains(out.String(), text) {
					t.Fatalf("missing %q: %s", text, out.String())
				}
			}
			if strings.Contains(out.String(), "https://example.invalid/feed") {
				t.Fatalf("subscription URL exposed: %s", out.String())
			}
		})
	}
}

func TestProviderAddRetainsFieldsAndRequiresExplicitResubmit(t *testing.T) {
	r := &sequenceRunner{results: map[string][]Result{
		"proxy providers": {{Data: map[string]any{"providers": []any{}}}},
		"proxy provider add work https://example.invalid/feed Work Group 3600": {{Code: 1, Error: &CLIError{Message: "validation failed; existing profile retained"}}},
		"proxy provider add work https://example.invalid/feed Work Group 7200": {{Data: map[string]any{"added": true, "group_created": false}}},
	}}
	var out bytes.Buffer
	// Editing the interval does not submit; only the numbered Submit action does.
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\nwork\nhttps://example.invalid/feed\nWork Group\n\n4\n7200\n5\n0\n"), &out), providerListPage())
	want := [][]string{
		{"proxy", "providers"},
		{"proxy", "provider", "add", "work", "https://example.invalid/feed", "Work Group", "3600"},
		{"proxy", "provider", "add", "work", "https://example.invalid/feed", "Work Group", "7200"},
		{"proxy", "providers"},
	}
	if !reflect.DeepEqual(r.calls, want) || !strings.Contains(out.String(), "Values are retained") || !strings.Contains(out.String(), "existing profile retained") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
	if strings.Contains(out.String(), "New select group created") {
		t.Fatalf("existing group reported as created: %s", out.String())
	}
}

func TestProviderAddCancelAfterFailureNeverRetries(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{
		"proxy providers": {Data: map[string]any{"providers": []any{}}},
		"proxy provider add work https://example.invalid/feed 0 3600": {Code: 1, Error: &CLIError{Message: "provider already exists"}},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\nwork\nhttps://example.invalid/feed\n0\n\n0\n0\n"), &out), providerListPage())
	want := [][]string{
		{"proxy", "providers"},
		{"proxy", "provider", "add", "work", "https://example.invalid/feed", "0", "3600"},
		{"proxy", "providers"},
	}
	if !reflect.DeepEqual(r.calls, want) || !strings.Contains(out.String(), "0. Cancel") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}

func TestProviderAddValidatesInPlaceBeforeDispatch(t *testing.T) {
	r := &fakeRunner{}
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader("bad name\nwork\nftp://example.invalid/feed\nhttps://user:secret@example.invalid/feed\nhttps://example.invalid/feed\n\nExisting Group\n0\n31536001\nnot-a-number\n31536000\n"), &out)
	if err := addProvider(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"proxy", "provider", "add", "work", "https://example.invalid/feed", "Existing Group", "31536000"}}
	if !reflect.DeepEqual(r.calls, want) || strings.Count(out.String(), "Invalid value:") != 6 || !strings.Contains(out.String(), "This field is required.") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}

func TestProviderAddEOFBeforeSubmissionDoesNotDispatch(t *testing.T) {
	r := &fakeRunner{}
	s := NewSession(r, strings.NewReader("work\nhttps://example.invalid/feed\n"), &bytes.Buffer{})
	if err := addProvider(context.Background(), s); err != ErrQuit || len(r.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, r.calls)
	}
}
