package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func profileAction(t *testing.T, view PageView, label string) Item {
	t.Helper()
	for _, item := range view.Actions {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("missing action %q: %+v", label, view.Actions)
	return Item{}
}

func TestProfileListAndFileAction(t *testing.T) {
	ctx := context.Background()
	runner := &featureRunner{results: map[string]Result{"profile\x00export\x00default\x00/tmp/Profile A.yaml": {}, "profile\x00list": {Data: map[string]any{"profiles": []string{"work", "default"}, "current": "default"}}}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("\n/tmp/Profile A.yaml\n"), &out)
	view, err := profileListPage().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 2 || !strings.Contains(view.Items[1].Label, "selected") || view.Items[1].Name != "default" {
		t.Fatalf("resource list: %+v", view)
	}
	detail, err := view.Items[1].Next().Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := profileAction(t, detail, "Export source YAML").Action(ctx, s); err != nil {
		t.Fatal(err)
	}
	want := []string{"profile", "export", "default", "/tmp/Profile A.yaml"}
	if !reflect.DeepEqual(runner.calls[len(runner.calls)-1], want) {
		t.Fatalf("export args = %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "required") {
		t.Fatalf("required field not retried: %s", &out)
	}
	if operationItem(t, profileDetailPage("default"), "Restore latest backup").Impact == "" {
		t.Fatal("restore impact absent")
	}
}

func TestSubscriptionPreviewSummaryAndPaginatedNames(t *testing.T) {
	nodes := []string{"Node A", "HTTPS://secret.example/token", "trojan://password@secret.example:443"}
	for i := 0; i < 20; i++ {
		nodes = append(nodes, fmt.Sprintf("node %02d", i))
	}
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00list":            {Data: map[string]any{"subscriptions": []map[string]any{{"name": "feed", "updated_at": "2026-10-10T00:00:00Z", "expires_at": "2027-01-01T00:00:00Z", "upload": 10, "download": 20, "total": 100}}}},
		"subscription\x00preview\x00feed": {Data: map[string]any{"name": "feed", "added": nodes, "removed": []string{"Old"}, "changed": []string{"Changed"}, "changes": []map[string]any{{"path": "rules", "kind": "changed", "behavior": "routing policy"}}}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("11\n0\n"), &out)
	view, err := subscriptionDetailPage("feed").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Context, "23 nodes added, 1 removed, 1 changed; 1 configuration changes") || strings.Contains(view.Context, "Node A") || strings.Contains(view.Context, "secret.example") {
		t.Fatalf("summary: %q", view.Context)
	}
	if !strings.Contains(view.Context, "70 bytes available") || !strings.Contains(view.Context, "2027-01-01") {
		t.Fatalf("metadata: %s", view.Context)
	}
	changes, _ := profileAction(t, view, "View Changes").Next().Load(context.Background(), s)
	preview := profileAction(t, changes, "Compare with target profile").Next()
	previewView, err := preview.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(previewView.Items) != 26 || strings.Contains(previewView.Items[1].Label, "secret.example") || !strings.Contains(previewView.Items[1].Label, "[redacted URL]") {
		t.Fatalf("complete redacted changes: %+v", previewView.Items)
	}
	if code := RunPage(context.Background(), s, preview); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "Page 2/3") || strings.Contains(out.String(), "secret.example") {
		t.Fatalf("pagination: %s", &out)
	}
}

func TestSubscriptionApplyAvailableAfterPreviewFailure(t *testing.T) {
	for _, result := range []Result{{Code: 1, Error: &CLIError{Message: "cache missing at https://secret.example/token"}}, {Error: &CLIError{Message: "target corrupt"}}} {
		runner := &featureRunner{results: map[string]Result{"subscription\x00preview\x00feed": result}}
		view, err := subscriptionDetailPage("feed").Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
		if err != nil {
			t.Fatal(err)
		}
		item := profileAction(t, view, "Apply downloaded subscription")
		if !reflect.DeepEqual(item.Args, []string{"subscription", "apply", "feed"}) || !strings.Contains(item.Impact, "clears local overrides and saved node selections") {
			t.Fatalf("apply: %+v", item)
		}
		if !strings.Contains(view.Context, "preview unavailable") || strings.Contains(view.Context, "secret.example") {
			t.Fatalf("failure: %s", view.Context)
		}
	}
}

func TestSubscriptionAddNavigatesAndRetriesRetainedFields(t *testing.T) {
	const url = "https://secret.example/feed?token=abc"
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00add\x00q\x00" + url: {}, "subscription\x00add\x00b\x00" + url: {Code: 1, Error: &CLIError{Message: "already exists"}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("\nbad name\nb\ninvalid\n"+url+"\n1\nq\n3\n"), &out)
	nav, err := addSubscription(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if nav.Next == nil || nav.Next.Title != "Subscription: q" {
		t.Fatalf("navigation: %+v", nav)
	}
	want := [][]string{{"subscription", "add", "b", url}, {"subscription", "add", "q", url}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("retry lost values or dispatched more than once: %#v", runner.calls)
	}
	if strings.Contains(out.String(), url) {
		t.Fatalf("URL leaked: %s", &out)
	}
}

func TestProfileImportLiteralNamesAndPaths(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{"profile\x00import\x00b\x00q": {}}}
	s := NewSession(runner, strings.NewReader("b\nq\n"), &bytes.Buffer{})
	nav, err := importProfile(context.Background(), s)
	if err != nil || nav.Next == nil || nav.Next.Title != "Profile: b" {
		t.Fatalf("nav=%+v err=%v", nav, err)
	}
	if !reflect.DeepEqual(runner.calls, [][]string{{"profile", "import", "b", "q"}}) {
		t.Fatalf("literal fields: %v", runner.calls)
	}
}

func TestSubscriptionDownloadStaysAndDistinguishesCacheComparison(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00preview\x00feed": {Data: map[string]any{"name": "feed", "added": []string{"target change"}}},
		"subscription\x00update\x00feed":  {Data: map[string]any{"name": "feed", "bytes": 42, "preview": map[string]any{"name": "feed", "removed": []string{"cache change"}}}},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader(""), &out)
	page := subscriptionDetailPage("feed")
	view, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	action := profileAction(t, view, "Download subscription")
	if action.Navigate != nil || action.Next != nil {
		t.Fatal("download navigates")
	}
	if err := action.Action(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runner.calls, [][]string{{"subscription", "update", "feed"}}) {
		t.Fatalf("download applied/started: %v", runner.calls)
	}
	view, err = page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Context, "Application changes (cache vs target profile): 1 nodes added") || !strings.Contains(view.Context, "Last download changes (new cache vs previous cache): 0 nodes added, 1 removed") {
		t.Fatalf("comparisons: %s", view.Context)
	}
	changes, _ := profileAction(t, view, "View Changes").Next().Load(context.Background(), s)
	old := profileAction(t, changes, "Compare with previous download").Next()
	snapshot, _ := old.Load(context.Background(), s)
	if len(snapshot.Items) != 1 || snapshot.Items[0].Label != "Removed: cache change" {
		t.Fatalf("download snapshot: %+v", snapshot)
	}
}

func TestResourceRemovalNavigatesOnlyOnSuccess(t *testing.T) {
	for _, kind := range []string{"subscription", "profile"} {
		for _, failed := range []bool{true, false} {
			result := Result{}
			if failed {
				result = Result{Code: 1, Error: &CLIError{Message: "cannot remove"}}
			}
			runner := &featureRunner{results: map[string]Result{kind + "\x00remove\x00feed": result}}
			nav, err := removeResource(kind, "feed")(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
			if err != nil || nav.Back == failed || len(runner.calls) != 1 {
				t.Fatalf("kind=%s failed=%v nav=%+v calls=%v err=%v", kind, failed, nav, runner.calls, err)
			}
		}
	}
}

func TestSubscriptionAddEOFCancelsWithoutSubmitting(t *testing.T) {
	runner := &featureRunner{}
	nav, err := addSubscription(context.Background(), NewSession(runner, strings.NewReader("feed\n"), &bytes.Buffer{}))
	if err != ErrQuit || nav.Next != nil || len(runner.calls) != 0 {
		t.Fatalf("nav=%+v err=%v calls=%v", nav, err, runner.calls)
	}
}

func TestSubscriptionMenuAddAndRemoveJourney(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00list": {Data: map[string]any{"subscriptions": []any{}}},
		"subscription\x00add\x00feed\x00https://example.invalid/feed": {},
		"subscription\x00preview\x00feed":                             {Error: &CLIError{Message: "target profile damaged"}},
		"subscription\x00apply\x00feed":                               {},
		"subscription\x00remove\x00feed":                              {},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("1\nfeed\nhttps://example.invalid/feed\n2\n6\n0\n"), &out)
	if code := RunPage(context.Background(), s, subscriptionListPage()); code != 0 {
		t.Fatal(code)
	}
	var writes [][]string
	for _, args := range runner.calls {
		if len(args) > 1 && (args[1] == "add" || args[1] == "apply" || args[1] == "remove") {
			writes = append(writes, args)
		}
	}
	want := [][]string{{"subscription", "add", "feed", "https://example.invalid/feed"}, {"subscription", "apply", "feed"}, {"subscription", "remove", "feed"}}
	if !reflect.DeepEqual(writes, want) {
		t.Fatalf("writes: %v", writes)
	}
	if !strings.Contains(out.String(), "Subscription: feed") || strings.Count(out.String(), "\nSubscriptions\n") != 2 || strings.Contains(out.String(), "yes") {
		t.Fatalf("navigation or confirmation: %s", &out)
	}
}

func TestProfileFailedReadPreservesImportAndClearsResources(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{"profile\x00list": {Code: 1, Error: &CLIError{Message: "cannot read profiles"}}}}
	view, err := profileListPage().Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
	if err == nil || len(view.Items) != 0 || len(view.Actions) != 1 || view.Actions[0].Navigate == nil {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}
