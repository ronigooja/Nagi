package menu

import (
	"bytes"
	"context"
	"fmt"
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
	if err := profileAction(t, detail, "Export configuration").Action(ctx, s); err != nil {
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

func TestDefaultSettingsMenuSelectsTargetDNSGroupAndReusesBinding(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, input, bound string
		want               []string
	}{
		{name: "first apply", input: "2\n", want: []string{"profile", "override", "apply-default", "work", "--dns-proxy", "Other Group"}},
		{name: "saved binding", bound: "Other Group", want: []string{"profile", "override", "apply-default", "work"}},
		{name: "stale binding", input: "1\n", bound: "Removed Group", want: []string{"profile", "override", "apply-default", "work", "--dns-proxy", "Proxy Group"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &featureRunner{results: map[string]Result{
				"profile\x00override\x00default-status\x00work": {Data: map[string]any{"default_override_applied": false, "dns_proxy_required": true, "dns_proxy_group": tc.bound, "dns_proxy_groups": []string{"Proxy Group", "Other Group"}}},
				strings.Join(tc.want, "\x00"):                   {},
			}}
			var out bytes.Buffer
			s := NewSession(runner, strings.NewReader(tc.input), &out)
			if err := toggleDefaultOverride(ctx, s, "work", false); err != nil {
				t.Fatal(err)
			}
			if got := runner.calls[len(runner.calls)-1]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("apply command = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDefaultSettingsMenuNeedsEligibleDNSGroup(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"profile\x00override\x00default-status\x00work": {Data: map[string]any{"default_override_applied": false, "dns_proxy_required": true, "dns_proxy_groups": []string{}}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	err := toggleDefaultOverride(context.Background(), s, "work", false)
	if err == nil || !strings.Contains(err.Error(), "No eligible proxy group") || len(runner.calls) != 1 {
		t.Fatalf("result = %v, calls = %#v", err, runner.calls)
	}
}

func TestDefaultSettingsMenuNoNodesAppliesWithoutDNSGroup(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"profile\x00override\x00default-status\x00default": {Data: map[string]any{"default_override_applied": false, "dns_proxy_required": false, "dns_proxy_groups": []string{}}},
		"profile\x00override\x00apply-default\x00default":  {},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	if err := toggleDefaultOverride(context.Background(), s, "default", false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"profile", "override", "apply-default", "default"}; !reflect.DeepEqual(runner.calls[len(runner.calls)-1], want) {
		t.Fatalf("apply command = %#v", runner.calls)
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
	if !strings.Contains(profileAction(t, view, "Update and Apply").Impact, "retains local settings and nodes; clears saved node selections") {
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
				s := NewSession(runner, strings.NewReader("feed\nhttps://example.invalid/feed\n\n\n"), &out)
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
					feedback = "apply failed. cache retained"
				}
				if !strings.Contains(strings.ToLower(out.String()), feedback) || (stage != "success" && !strings.Contains(out.String(), "https://secret.example/token")) {
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
	s := NewSession(runner, strings.NewReader("\nbad name\nb\ninvalid\n"+url+"\n\n\n1\nq\n5\n"), &out)
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
	s := NewSession(runner, strings.NewReader("1\nfeed\nhttps://example.invalid/feed\n\n\n5\n0\n"), &out)
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

func TestLocalOverridesRefreshAfterToggle(t *testing.T) {
	status := func(applied bool) Result {
		return Result{Data: map[string]any{"default_override_applied": applied, "dns_proxy_required": false}}
	}
	for _, initial := range []bool{false, true} {
		runner := &sequenceRunner{results: map[string][]Result{
			"profile override default-status work": {status(initial), status(initial), status(!initial), status(!initial), status(initial)},
		}}
		var out bytes.Buffer
		RunPage(context.Background(), NewSession(runner, strings.NewReader("1\n1\n0\n"), &out), localOverridesPage("work"))
		var mutations []string
		for _, call := range runner.calls {
			if call[2] != "default-status" {
				mutations = append(mutations, call[2])
			}
		}
		want := []string{"apply-default", "restore-default"}
		if initial {
			want = []string{"restore-default", "apply-default"}
		}
		if !reflect.DeepEqual(mutations, want) || !strings.Contains(out.String(), "Apply default runtime settings") || !strings.Contains(out.String(), "Disable default runtime settings") {
			t.Fatalf("initial=%v mutations=%v output=%s", initial, mutations, out.String())
		}
	}
}

func TestLocalOverridesRefreshAfterSetAndClear(t *testing.T) {
	for _, action := range []string{"set", "clear"} {
		runner := &sequenceRunner{results: map[string][]Result{
			"profile override default-status work": {
				{Data: map[string]any{"default_override_applied": true}},
				{Data: map[string]any{"default_override_applied": false, "dns_proxy_group": "New Group"}},
			},
		}}
		input := "4\n0\n"
		if action == "set" {
			input = "3\n/tmp/local.yaml\n0\n"
		}
		var out bytes.Buffer
		RunPage(context.Background(), NewSession(runner, strings.NewReader(input), &out), localOverridesPage("work"))
		if !strings.Contains(out.String(), "Apply default runtime settings") || !strings.Contains(out.String(), "DNS via New Group") {
			t.Fatalf("%s did not refresh: %s", action, out.String())
		}
		if len(runner.calls) != 3 || runner.calls[1][2] != action {
			t.Fatalf("%s calls=%v", action, runner.calls)
		}
	}
}

func TestLocalOverridesRejectChangedStateBeforeMutation(t *testing.T) {
	runner := &sequenceRunner{results: map[string][]Result{
		"profile override default-status work": {
			{Data: map[string]any{"default_override_applied": true}},
			{Data: map[string]any{"default_override_applied": false}},
			{Data: map[string]any{"default_override_applied": false}},
		},
	}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("1\n0\n"), &out), localOverridesPage("work"))
	for _, call := range runner.calls {
		if call[2] != "default-status" {
			t.Fatalf("unexpected mutation: %v", runner.calls)
		}
	}
	if !strings.Contains(out.String(), "changed. Review the refreshed menu") || !strings.Contains(out.String(), "Apply default runtime settings") {
		t.Fatal(out.String())
	}
}

func TestSubscriptionSettingsReloadAfterOperations(t *testing.T) {
	for _, encryption := range []bool{false, true} {
		entry := func(enabled bool) Result {
			seconds := 0
			if enabled {
				seconds = 3600
			}
			return Result{Data: map[string]any{"subscriptions": []map[string]any{{"name": "feed", "key_configured": enabled, "schedule_seconds": seconds}}}}
		}
		runner := &sequenceRunner{results: map[string][]Result{
			"subscription list": {entry(false), entry(true), entry(false)},
		}}
		input, state, cleared := "1\n3600\n2\n0\n", "Download update interval: 3600 seconds", "Automatic updates: disabled"
		want := [][]string{{"subscription", "schedule", "feed", "3600"}, {"subscription", "schedule", "feed", "off"}}
		if encryption {
			input, state, cleared = "1\n/tmp/test-identity.txt\n2\n0\n", "Identity file: configured", "Identity file: not configured"
			want = [][]string{{"subscription", "key", "feed", "/tmp/test-identity.txt"}, {"subscription", "key", "feed", "clear"}}
		}
		var out bytes.Buffer
		RunPage(context.Background(), NewSession(runner, strings.NewReader(input), &out), subscriptionSettingsPage("feed", encryption))
		var mutations [][]string
		for _, call := range runner.calls {
			if call[1] != "list" {
				mutations = append(mutations, call)
			}
		}
		if !reflect.DeepEqual(mutations, want) || !strings.Contains(out.String(), state) || strings.Count(out.String(), cleared) != 2 {
			t.Fatalf("encryption=%v calls=%v output=%s", encryption, runner.calls, out.String())
		}
	}
}

func TestSettingsReadFailureRetainsOnlyRecoveryActions(t *testing.T) {
	for _, tc := range []struct {
		page    *Page
		command string
		actions int
	}{
		{localOverridesPage("work"), "profile override default-status work", 3},
		{subscriptionSettingsPage("feed", false), "subscription list", 1},
		{subscriptionSettingsPage("feed", true), "subscription list", 1},
	} {
		runner := &sequenceRunner{results: map[string][]Result{tc.command: {{Code: 1, Error: &CLIError{Message: "read failed"}}}}}
		view, err := tc.page.Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
		if err == nil || len(view.Actions) != tc.actions || !strings.Contains(view.Context, "unavailable") {
			t.Fatalf("%s view=%+v error=%v", tc.page.Title, view, err)
		}
	}
	for _, encryption := range []bool{false, true} {
		runner := &sequenceRunner{results: map[string][]Result{"subscription list": {{Data: map[string]any{"subscriptions": []any{}}}}}}
		view, err := subscriptionSettingsPage("missing", encryption).Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
		if err == nil || len(view.Actions) != 1 || !strings.Contains(view.Context, "unavailable") {
			t.Fatalf("missing record: view=%+v err=%v", view, err)
		}
	}
}

func TestSettingsRefreshFailureDropsStateDependentActions(t *testing.T) {
	for _, tc := range []struct {
		page              *Page
		command           string
		initial           Result
		input, staleLabel string
	}{
		{localOverridesPage("work"), "profile override default-status work", Result{Data: map[string]any{"default_override_applied": true}}, "4\n0\n", "Disable default runtime settings"},
		{subscriptionSettingsPage("feed", false), "subscription list", Result{Data: map[string]any{"subscriptions": []map[string]any{{"name": "feed", "schedule_seconds": 3600}}}}, "2\n0\n", "Disable automatic updates"},
		{subscriptionSettingsPage("feed", true), "subscription list", Result{Data: map[string]any{"subscriptions": []map[string]any{{"name": "feed", "key_configured": true}}}}, "2\n0\n", "Clear encryption key"},
	} {
		runner := &sequenceRunner{results: map[string][]Result{tc.command: {tc.initial, {Code: 1, Error: &CLIError{Message: "read failed https://example.invalid/private-token"}}}}}
		var out bytes.Buffer
		RunPage(context.Background(), NewSession(runner, strings.NewReader(tc.input), &out), tc.page)
		if strings.Count(out.String(), tc.staleLabel) != 1 || !strings.Contains(out.String(), "unavailable") || !strings.Contains(out.String(), "https://example.invalid/private-token") {
			t.Fatalf("%s stale action or missing actionable error: %s", tc.page.Title, out.String())
		}
	}
}

func TestProfileImportRemainsReachableOnEveryResourcePage(t *testing.T) {
	for _, count := range []int{0, 1, 10, 11, 21} {
		for _, failed := range []bool{false, true} {
			if failed && count != 0 {
				continue
			}
			t.Run(fmt.Sprintf("count=%d/failed=%v", count, failed), func(t *testing.T) {
				var names []string
				for n := 0; n < count; n++ {
					names = append(names, fmt.Sprintf("profile-%02d", n))
				}
				result := Result{Data: map[string]any{"profiles": names}}
				if failed {
					result = Result{Code: 1, Error: &CLIError{Message: "cannot read profiles"}}
				}
				last := 0
				if count > 0 {
					last = (count - 1) / pageSize
				}
				for targetPage := 0; targetPage <= last; targetPage++ {
					runner := &fakeRunner{results: map[string]Result{"profile list": result}}
					input := strings.Repeat("12\n", targetPage)
					remaining := count - targetPage*pageSize
					if remaining > pageSize {
						remaining = pageSize
					}
					choice := remaining + 1
					input += fmt.Sprintf("%d\nnew-profile\n/tmp/fictitious-profile.yaml\n0\n0\n", choice)
					var out bytes.Buffer
					RunPage(context.Background(), NewSession(runner, strings.NewReader(input), &out), profileListPage())
					found := 0
					for _, call := range runner.calls {
						if reflect.DeepEqual(call, []string{"profile", "import", "new-profile", "/tmp/fictitious-profile.yaml"}) {
							found++
						}
					}
					if found != 1 || strings.Contains(out.String(), "Choose a listed number") || strings.Count(out.String(), "\nProfiles\n") < 2 {
						t.Fatalf("page=%d import dispatches=%d calls=%v output=%s", targetPage, found, runner.calls, &out)
					}
					if !strings.Contains(out.String(), fmt.Sprintf("%d. Import a profile", choice)) || strings.Contains(out.String(), "Current Configuration") {
						t.Fatalf("page=%d fixed actions: %s", targetPage, &out)
					}
				}
			})
		}
	}
}
