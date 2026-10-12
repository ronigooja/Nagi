package menu

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

var menuSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Check both the semantic attributes and the reset immediately after the text.
// Accept either a combined SGR or consecutive SGR sequences.
func assertMenuStyled(t *testing.T, rendered, text string, attributes ...string) {
	t.Helper()
	start := strings.Index(rendered, text)
	if start < 0 {
		t.Fatalf("missing %q in %q", text, rendered)
	}
	prefix := rendered[:start]
	codes := []string{}
	for {
		matches := menuSGR.FindAllStringIndex(prefix, -1)
		if len(matches) == 0 || matches[len(matches)-1][1] != len(prefix) {
			break
		}
		last := matches[len(matches)-1]
		codes = append(codes, strings.Split(prefix[last[0]+2:last[1]-1], ";")...)
		prefix = prefix[:last[0]]
	}
	for _, attribute := range attributes {
		found := false
		for _, code := range codes {
			found = found || code == attribute
		}
		if !found {
			t.Errorf("%q lacks SGR %s: %q", text, attribute, rendered)
		}
	}
	for _, code := range codes {
		if code == "1" {
			t.Errorf("%q must use regular weight: %q", text, rendered)
		}
	}
	if !strings.HasPrefix(rendered[start+len(text):], "\x1b[0m") {
		t.Errorf("%q lacks immediate style reset: %q", text, rendered)
	}
}

func TestMenuStyleDisabledForNonTerminalOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, out := range []io.Writer{&bytes.Buffer{}, file} {
		s := NewSession(&fakeRunner{}, strings.NewReader(""), out)
		if s.styles {
			t.Fatalf("styles enabled for %T", out)
		}
	}
}

func TestMenuStyleRenderPreservesTextAndScopesHighlights(t *testing.T) {
	view := PageView{Context: "Mihomo: running", Items: []Item{
		{Label: "Ordinary (selected)"},
		{Label: "Actual (selected)", Selected: true},
	}, Actions: []Item{{Label: "Replace configuration", Impact: "Clears saved selections"}}}
	page := &Page{Title: "Nodes"}
	var plain, colored bytes.Buffer
	NewSession(&fakeRunner{}, strings.NewReader(""), &plain).render(page, view, 0, 1)
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &colored)
	s.styles = true
	s.render(page, view, 0, 1)
	got := colored.String()
	if menuSGR.ReplaceAllString(got, "") != plain.String() {
		t.Fatalf("styles changed visible menu text: %q vs %q", got, plain.String())
	}
	assertMenuStyled(t, got, "Nodes", "33")
	assertMenuStyled(t, got, "Ordinary (selected)", "94")
	assertMenuStyled(t, got, "Actual (selected)", "32")
	assertMenuStyled(t, got, "Replace configuration", "94")
	assertMenuStyled(t, got, " — Clears saved selections", "2")
	for _, line := range []string{
		"\nMihomo: \x1b[32mrunning\x1b[0m\n",
		"\n1. \x1b[94mOrdinary (selected)\x1b[0m\n", "\n2. \x1b[32mActual (selected)\x1b[0m\n",
		"\n3. \x1b[94mReplace configuration\x1b[0m\x1b[2m — Clears saved selections\x1b[0m\n", "\n0. Exit menu (mihomo keeps running)\n",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("incorrect style boundary or numbering: missing %q in %q", line, got)
		}
	}
	if strings.Contains(plain.String(), "\x1b") {
		t.Fatal("nonterminal rendering contains ANSI")
	}
	colored.Reset()
	for i := 0; i < 9; i++ {
		view.Items = append(view.Items, Item{Label: "Extra node"})
	}
	s.render(page, view, 0, 2)
	s.render(page, view, 1, 2)
	for _, line := range []string{"\n12. \x1b[94mNext page\x1b[0m\n", "\n3. \x1b[94mPrevious page\x1b[0m\n", "\n0. Back\n", "\nPage 1/2\n", "\nPage 2/2\n"} {
		if !strings.Contains(colored.String(), line) {
			t.Errorf("navigation numbering gained styling: missing %q in %q", line, colored.String())
		}
	}
}

func TestMenuStyledPageRedactsBeforeApplyingHighlights(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	s.styles = true
	s.render(&Page{Title: "Title https://secret.example/title"}, PageView{
		Context: "State https://secret.example/context",
		Items:   []Item{{Label: "Node https://secret.example/node", Selected: true, Impact: "Effect https://secret.example/impact"}},
	}, 0, 1)
	got := out.String()
	if strings.Contains(got, "secret.example") || strings.Count(got, "[redacted URL]") != 4 {
		t.Fatalf("highlighted page leaked URL: %q", got)
	}
	assertMenuStyled(t, got, "Title [redacted URL]", "33")
	assertMenuStyled(t, got, "Node [redacted URL]", "32")
	assertMenuStyled(t, got, " — Effect [redacted URL]", "2")
	if !strings.Contains(got, "\nState [redacted URL]\n") {
		t.Fatalf("redacted context should remain plain: %q", got)
	}
}

func TestMenuSelectedFlagsFollowProfileAndNodeState(t *testing.T) {
	for _, source := range []string{"profile", "proxy"} {
		t.Run(source, func(t *testing.T) {
			runner := &featureRunner{results: map[string]Result{
				"profile\x00list":        {Data: map[string]any{"profiles": []string{"Literal (selected)", "Actual"}, "current": "Actual"}},
				"proxy\x00show\x00Group": {Data: map[string]any{"name": "Group", "all": []string{"Literal (selected)", "Actual"}, "now": "Actual"}},
			}}
			s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
			page := profileListPage()
			key, selectedKey := "profile\x00list", "current"
			if source == "proxy" {
				page, key, selectedKey = proxyGroupPage("Group"), "proxy\x00show\x00Group", "now"
			}
			for _, selected := range []string{"Actual", "Literal (selected)", ""} {
				runner.results[key].Data.(map[string]any)[selectedKey] = selected
				view, err := page.Load(context.Background(), s)
				if err != nil {
					t.Fatal(err)
				}
				if len(view.Items) != 2 {
					t.Fatalf("items=%v", view.Items)
				}
				for _, item := range view.Items {
					if item.Selected != (item.Name == selected) {
						t.Errorf("selected=%q item=%+v", selected, item)
					}
				}
			}
		})
	}
}

func TestMenuProfileUnavailableStateDoesNotHighlightSelection(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"profile\x00list": {Data: map[string]any{"profiles": []string{"work"}, "current": "work", "current_error": "profile unavailable"}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	view, err := profileListPage().Load(context.Background(), s)
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if view.Items[0].Selected || !strings.Contains(view.Context, "unavailable") {
		t.Fatalf("unavailable current profile highlighted: %+v", view)
	}
}

func TestMenuFeedbackKindsRedactAndResetBeforeFollowingOutput(t *testing.T) {
	for _, active := range []bool{false, true} {
		var out bytes.Buffer
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
		s.styles, s.active = true, active
		s.feedback(feedbackSuccess, "Saved.")
		s.feedback(feedbackWarning, "Cache remains; apply failed: https://secret.example/token")
		s.feedback(feedbackError, "Read failed: https://secret.example/token")
		if active {
			if out.Len() != 0 {
				t.Fatal("queued feedback rendered before flush")
			}
			s.flushResult()
		}
		s.WriteLine("plain stream content")
		got := out.String()
		assertMenuStyled(t, got, "Saved.", "32")
		assertMenuStyled(t, got, "Cache remains; apply failed: [redacted URL]", "33")
		assertMenuStyled(t, got, "Read failed: [redacted URL]", "31")
		if strings.Contains(got, "secret.example") || !strings.HasSuffix(got, "\x1b[0m\nplain stream content\n") || s.pending != "" {
			t.Fatalf("feedback leaked content or style: %q pending=%q", got, s.pending)
		}
	}
}

func TestMenuCommandAndReadFailuresAreRedAndSuccessIsGreen(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader("0\n"), &out)
	s.styles = true
	s.ShowResult([]string{"start"}, Result{})
	s.ShowResult([]string{"start"}, Result{Code: 1, Error: &CLIError{Message: "command failed"}})
	page := &Page{Title: "Resources", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{}, errors.New("read failed https://secret.example/token")
	}}
	RunPage(context.Background(), s, page)
	assertMenuStyled(t, out.String(), "Done.", "32")
	assertMenuStyled(t, out.String(), "Error: command failed", "31")
	assertMenuStyled(t, out.String(), "Error reading Resources: read failed [redacted URL]", "31")
	if strings.Contains(out.String(), "secret.example") {
		t.Fatal("read failure contains URL")
	}
}

func TestMenuInvalidChoicesAndFormValidationAreYellow(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader("wrong\n0\n"), &out)
	s.styles = true
	RunPage(context.Background(), s, actionPage("Actions", Item{Label: "Action"}))
	assertMenuStyled(t, out.String(), "Choose a listed number.", "33")
	if s.styled(stylePrompt, "Choice: ") != "Choice: " || !strings.HasSuffix(out.String(), "\x1b[0m\nChoice: ") {
		t.Fatalf("prompt must remain plain after the warning resets: %q", out.String())
	}
	out.Reset()
	s = NewSession(&fakeRunner{}, strings.NewReader("\ninvalid\nvalid\n0\n"), &out)
	s.styles = true
	_, err := s.Form(context.Background(), []Field{{Key: "name", Label: "Name", Required: true, Validate: func(value string) error {
		if value != "valid" {
			return errors.New("choose valid")
		}
		return nil
	}}}, nil, func(map[string]string) Result { return Result{Code: 1} })
	if err != ErrBack {
		t.Fatalf("failed form cancellation: %v", err)
	}
	assertMenuStyled(t, out.String(), "This field is required.", "33")
	assertMenuStyled(t, out.String(), "Invalid value: choose valid", "33")
	for _, line := range []string{"\n1. \x1b[94mName\x1b[0m\n", "\n2. \x1b[94mSubmit again\x1b[0m\n", "\n0. Cancel\n"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("form numbering gained styling: missing %q in %q", line, out.String())
		}
	}
	assertMenuStyled(t, out.String(), "Submit again", "94")
}

func TestMenuStyledRequestedRawOutputIsUnchanged(t *testing.T) {
	raw := "url: https://example.invalid/config\n\x1b[35mexisting content\x1b[0m\n"
	for _, args := range [][]string{{"config", "show"}, {"profile", "override", "show", "work"}, {"logs", "25"}} {
		for _, active := range []bool{false, true} {
			var out bytes.Buffer
			s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
			s.styles, s.active = true, active
			data := map[string]any{"content": raw}
			s.ShowResult(args, Result{Data: data})
			if active {
				s.flushResult()
			}
			if strings.TrimRight(out.String(), "\n") != strings.TrimRight(raw, "\n") {
				t.Fatalf("%v active=%v: raw output altered: %q", args, active, out.String())
			}
		}
	}
}

func TestMenuStyledSubscriptionCompletionAndPartialFailure(t *testing.T) {
	for _, stage := range []string{"success", "update", "apply"} {
		t.Run(stage, func(t *testing.T) {
			runner := &featureRunner{results: map[string]Result{
				"subscription\x00update\x00feed": {}, "subscription\x00apply\x00feed": {},
			}}
			if stage != "success" {
				runner.results["subscription\x00"+stage+"\x00feed"] = Result{Code: 1, Error: &CLIError{Message: "stage failed"}}
			}
			var out bytes.Buffer
			s := NewSession(runner, strings.NewReader(""), &out)
			s.styles, s.active = true, true
			updateAndApplySubscription(context.Background(), s, "feed", true)
			if out.Len() != 0 {
				t.Fatal("active operation rendered feedback before page refresh")
			}
			s.flushResult()
			if stage == "success" {
				assertMenuStyled(t, out.String(), "Subscription updated and applied.", "32")
				return
			}
			state := "Subscription saved, but download failed; apply was not attempted."
			if stage == "apply" {
				state = "Downloaded, but apply failed. Cache retained; retry with `nagi subscription apply NAME`."
			}
			assertMenuStyled(t, out.String(), state, "33")
			assertMenuStyled(t, out.String(), "Error: stage failed", "31")
			if strings.Contains(out.String(), "\x1b[32m") {
				t.Fatal("partial or failed operation presented as successful")
			}
		})
	}
}

func TestMenuStyledProfileComparisonPreservesContent(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
	s.styles, s.active = true, true
	result := Result{Data: map[string]any{"diff": "- url: https://old.example/config\n+ url: https://new.example/config", "changed": true}}
	s.ShowResult([]string{"profile", "diff", "work"}, result)
	if !s.longResult || out.Len() != 0 {
		t.Fatal("profile comparison must remain queued and pause for reading")
	}
	s.flushResult()
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("profile comparison gained styling: %q", out.String())
	}
	var plain bytes.Buffer
	NewSession(&fakeRunner{}, strings.NewReader(""), &plain).ShowResult([]string{"profile", "diff", "work"}, result)
	if out.String() != plain.String() {
		t.Fatalf("comparison changed: %q vs %q", out.String(), plain.String())
	}
}

func TestMenuSelectedFlagsRefreshWithLatencyRankedNodes(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{
		"proxy\x00show\x00Group": {Data: map[string]any{"name": "Group", "all": []string{"A", "B (selected)"}, "now": "A"}},
		"proxy\x00delays\x00Group": {Data: map[string]any{"results": []any{
			map[string]any{"proxy": "B (selected)", "delay_ms": 1}, map[string]any{"proxy": "A", "delay_ms": 10},
		}}},
	}}
	s := NewSession(runner, strings.NewReader(""), &bytes.Buffer{})
	page := proxyGroupPage("Group")
	view, err := page.Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if err := profileAction(t, view, "Test all nodes and select from results").Action(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	for _, selected := range []string{"A", "B (selected)"} {
		runner.results["proxy\x00show\x00Group"].Data.(map[string]any)["now"] = selected
		view, err = page.Load(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Items) != 2 || view.Items[0].Name != "B (selected)" || !strings.Contains(view.Items[0].Label, "1 ms") {
			t.Fatalf("latency ranking lost: %+v", view.Items)
		}
		for _, item := range view.Items {
			if item.Selected != (item.Name == selected) {
				t.Fatalf("stale selection after refresh: %+v now=%q", item, selected)
			}
		}
	}
}

type styledStreamRunner struct {
	fakeRunner
	content string
	args    []string
}

func (r *styledStreamRunner) Stream(_ context.Context, args []string, out io.Writer) Result {
	r.args = append([]string(nil), args...)
	_, _ = io.WriteString(out, r.content)
	return Result{}
}

func TestMenuStyledStreamsPreserveRawBytes(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		content string
	}{
		{[]string{"logs", "follow"}, "GET https://example.invalid/log\nnext line without trailing newline"},
		{[]string{"traffic", "watch"}, "{\"upload\":1,\"download\":2}\n{\"upload\":3,\"download\":4}\n"},
	} {
		runner := &styledStreamRunner{content: tc.content}
		var out bytes.Buffer
		s := NewSession(runner, strings.NewReader(""), &out)
		s.styles, s.active = true, true
		result := s.Stream(context.Background(), tc.args...)
		if result.Code != 0 || result.Error != nil || strings.Join(runner.args, " ") != strings.Join(tc.args, " ") || len(runner.calls) != 0 {
			t.Fatalf("stream dispatch failed: args=%v result=%+v ordinaryCalls=%v", runner.args, result, runner.calls)
		}
		if out.String() != tc.content || s.pending != "" {
			t.Fatalf("%v altered raw stream: output=%q pending=%q", tc.args, out.String(), s.pending)
		}
	}
}
