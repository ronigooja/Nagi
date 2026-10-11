package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestSubscriptionMetadataWithoutPreview(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("menu test", -7*60*60)
	t.Cleanup(func() { time.Local = local })
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00list": {Data: map[string]any{"subscriptions": []map[string]any{{"name": "feed", "updated_at": "2026-10-10T00:15:42.123456789Z", "expires_at": "2027-01-01T00:00:00Z", "upload": 1073741824, "download": 536870912, "total": 3221225472}}}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	list, err := subscriptionListPage().Load(context.Background(), s)
	if err != nil || list.Items[0].Label != "feed (updated 2026-10-09 17:15)" {
		t.Fatalf("list: %+v %v", list, err)
	}
	view, err := list.Items[0].Next().Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if view.Context != "Updated: 2026-10-09 17:15\nQuota: 1.5 GiB available / 3 GiB total (1 GiB uploaded, 512 MiB downloaded)\nExpires: 2026-12-31 17:00" {
		t.Fatal(view.Context)
	}
	if !reflect.DeepEqual(runner.calls, [][]string{{"subscription", "list"}, {"subscription", "list"}}) {
		t.Fatalf("unexpected preview: %v", runner.calls)
	}
	if !strings.Contains(profileAction(t, view, "Update and Apply").Impact, "clears local overrides and saved node selections") {
		t.Fatal("missing apply disclosure")
	}
}

func TestSubscriptionUpdateAndApplyStages(t *testing.T) {
	for _, added := range []bool{false, true} {
		for _, stage := range []string{"success", "update", "apply"} {
			t.Run(fmtStage(added, stage), func(t *testing.T) {
				runner := &featureRunner{results: map[string]Result{"subscription\x00add\x00feed\x00https://example.invalid/feed": {}, "subscription\x00update\x00feed": {}, "subscription\x00apply\x00feed": {}}}
				if stage != "success" {
					runner.results["subscription\x00"+stage+"\x00feed"] = Result{Code: 1, Error: &CLIError{Message: "failed https://secret.example/token"}}
				}
				var out bytes.Buffer
				s := NewSession(runner, strings.NewReader("feed\nhttps://example.invalid/feed\n"), &out)
				if added {
					nav, err := addSubscription(context.Background(), s)
					if err != nil || nav.Next == nil || nav.Next.Title != "Subscription: feed" {
						t.Fatalf("saved record not reachable: %+v %v", nav, err)
					}
				} else {
					updateAndApplySubscription(context.Background(), s, "feed", false)
				}
				want := [][]string{}
				if added {
					want = append(want, []string{"subscription", "add", "feed", "https://example.invalid/feed"})
				}
				want = append(want, []string{"subscription", "update", "feed"})
				if stage != "update" {
					want = append(want, []string{"subscription", "apply", "feed"})
				}
				if !reflect.DeepEqual(runner.calls, want) {
					t.Fatalf("stage order: %v", runner.calls)
				}
				feedback := "updated and applied"
				if stage == "update" {
					feedback = "download failed"
					if added {
						feedback = "saved, but download failed"
					}
				}
				if stage == "apply" {
					feedback = "apply failed; the new cache remains"
				}
				if !strings.Contains(strings.ToLower(out.String()), feedback) || strings.Contains(out.String(), "secret.example") {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func fmtStage(added bool, stage string) string {
	if added {
		return "add/" + stage
	}
	return "existing/" + stage
}

func TestSubscriptionAddNavigatesAndRetriesRetainedFields(t *testing.T) {
	const url = "https://secret.example/feed?token=abc"
	runner := &featureRunner{results: map[string]Result{
		"subscription\x00add\x00q\x00" + url: {}, "subscription\x00update\x00q": {}, "subscription\x00apply\x00q": {}, "subscription\x00add\x00b\x00" + url: {Code: 1, Error: &CLIError{Message: "already exists"}},
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
	want := [][]string{{"subscription", "add", "b", url}, {"subscription", "add", "q", url}, {"subscription", "update", "q"}, {"subscription", "apply", "q"}}
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
		"subscription\x00apply\x00feed":                               {}, "subscription\x00update\x00feed": {},
		"subscription\x00remove\x00feed": {},
	}}
	var out bytes.Buffer
	s := NewSession(runner, strings.NewReader("1\nfeed\nhttps://example.invalid/feed\n4\n0\n"), &out)
	if code := RunPage(context.Background(), s, subscriptionListPage()); code != 0 {
		t.Fatal(code)
	}
	var writes [][]string
	for _, args := range runner.calls {
		if len(args) > 1 && (args[1] == "add" || args[1] == "update" || args[1] == "apply" || args[1] == "remove") {
			writes = append(writes, args)
		}
	}
	want := [][]string{{"subscription", "add", "feed", "https://example.invalid/feed"}, {"subscription", "update", "feed"}, {"subscription", "apply", "feed"}, {"subscription", "remove", "feed"}}
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

func TestSubscriptionMissingAndInvalidMetadata(t *testing.T) {
	for _, timestamp := range []string{"", "invalid https://secret.example/token"} {
		runner := &featureRunner{results: map[string]Result{
			"subscription\x00list": {Data: map[string]any{"subscriptions": []map[string]any{{"name": "feed", "updated_at": timestamp, "expires_at": timestamp}}}},
		}}
		s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
		list, err := subscriptionListPage().Load(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := list.Items[0].Next().Load(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		updated := "unknown"
		if timestamp == "" {
			updated = "never downloaded"
		}
		if !strings.Contains(list.Items[0].Label, updated) || !strings.Contains(detail.Context, "Updated: "+updated+"\nQuota: unknown\nExpires: unknown") || strings.Contains(detail.Context+list.Items[0].Label, "secret.example") {
			t.Fatalf("missing/invalid metadata: list=%+v, detail=%+v", list, detail)
		}
	}
}
