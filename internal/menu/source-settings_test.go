package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSourceCreationDispatchesScheduleAndOptionalTestURL(t *testing.T) {
	const testURL = "https://check.example.invalid/private?token=secret"
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscription", true: "provider"}[provider], func(t *testing.T) {
			runner := &fakeRunner{}
			var output bytes.Buffer
			input := "work\nhttps://feed.example.invalid/source\n"
			if provider {
				input += "Work Group\n7200\n"
			} else {
				input += "off\n"
			}
			session := NewSession(runner, strings.NewReader(input+testURL+"\n"), &output)
			var err error
			var want []string
			if provider {
				err = addProvider(context.Background(), session)
				want = []string{"proxy", "provider", "add", "work", "https://feed.example.invalid/source", "Work Group", "7200", "--test-url", testURL}
			} else {
				_, err = addSubscription(context.Background(), session)
				want = []string{"subscription", "add", "work", "https://feed.example.invalid/source", "--schedule", "off", "--test-url", testURL}
			}
			if err != nil || len(runner.calls) == 0 || !reflect.DeepEqual(runner.calls[0], want) {
				t.Fatalf("err=%v calls=%v", err, runner.calls)
			}
			if strings.Contains(output.String(), testURL) || strings.Contains(output.String(), "token=secret") {
				t.Fatalf("private URL displayed: %s", output.String())
			}
		})
	}
}

func TestTestURLSettingsRetainInputAndResetIndependentlyOfInterval(t *testing.T) {
	for _, kind := range []string{"subscription", "provider", "group"} {
		t.Run(kind, func(t *testing.T) {
			const endpoint = "https://check.example.invalid/ping?token=private"
			set := "proxy healthcheck url set " + kind + " Work " + endpoint
			runner := &sequenceRunner{results: map[string][]Result{
				"proxy healthcheck show " + kind + " Work": {{Data: map[string]any{"test_url": "https://original.example.invalid", "url_source": "profile", "interval_seconds": 300}}},
				set: {{Code: 1, Error: &CLIError{Message: "activation failed"}}, {Data: map[string]any{}}},
			}}
			var output bytes.Buffer
			RunPage(context.Background(), NewSession(runner, strings.NewReader("3\nftp://bad.invalid\n"+endpoint+"\n2\n4\n0\n"), &output), healthcheckPage(kind, "Work"))
			wantSet := []string{"proxy", "healthcheck", "url", "set", kind, "Work", endpoint}
			count := 0
			for _, call := range runner.calls {
				if reflect.DeepEqual(call, wantSet) {
					count++
				}
				if len(call) > 2 && (call[2] == "set" || call[2] == "reset") {
					t.Fatalf("URL operation changed interval: %v", call)
				}
			}
			if count != 2 || !hasProxyCall(runner.calls, []string{"proxy", "healthcheck", "url", "reset", kind, "Work"}) {
				t.Fatalf("calls=%v", runner.calls)
			}
			if !strings.Contains(output.String(), "Values are retained") || !strings.Contains(output.String(), "Test URL source: profile") || strings.Contains(output.String(), "token=private") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestProviderDownloadIntervalOffAndResetDispatch(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"proxy provider interval show Feed": {Data: map[string]any{"interval_seconds": 3600, "source": "profile", "can_reset": true}}}}
	var output bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("1\noff\n2\n0\n"), &output), providerIntervalPage("Feed"))
	for _, want := range [][]string{{"proxy", "provider", "interval", "set", "Feed", "off"}, {"proxy", "provider", "interval", "reset", "Feed"}} {
		if !hasProxyCall(runner.calls, want) {
			t.Fatalf("missing %v in %v", want, runner.calls)
		}
	}
	runner.results["proxy provider interval show Feed"] = Result{Data: map[string]any{"interval_seconds": 0, "source": "override", "can_reset": false}}
	view, err := providerIntervalPage("Feed").Load(context.Background(), NewSession(runner, strings.NewReader(""), &output))
	if err != nil || len(view.Actions) != 1 {
		t.Fatalf("local provider exposes invalid reset: %+v %v", view, err)
	}
}

func TestProviderWithoutInheritedTestURLHidesReset(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{"proxy healthcheck show provider Feed": {Data: map[string]any{
		"interval_seconds": 300, "source": "profile", "test_url": "https://check.example.invalid", "url_source": "override", "can_reset_url": false,
	}}}}
	var output bytes.Buffer
	view, err := healthcheckPage("provider", "Feed").Load(context.Background(), NewSession(runner, strings.NewReader(""), &output))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range view.Actions {
		if action.Label == "Inherit test URL" {
			t.Fatalf("provider without inherited URL exposes reset: %+v", view.Actions)
		}
	}
}
