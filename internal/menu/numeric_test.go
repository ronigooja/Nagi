package menu

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestAllPagesUseNumericChoices(t *testing.T) {
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{})
	pages := []*Page{RootPage(), profileDetailPage("work"), subscriptionDetailPage("feed"), proxyGroupPage("group"), providerDetailPage("provider"), helperPageForOS("darwin")}
	forbidden := regexp.MustCompile(`(?m)^[a-z/][./] `)
	seen := map[string]bool{}
	for len(pages) > 0 {
		page := pages[0]
		pages = pages[1:]
		if seen[page.Title] {
			continue
		}
		seen[page.Title] = true
		view, _ := page.Load(context.Background(), s)
		var out bytes.Buffer
		s.out = &out
		s.render(page, view, 0, 2)
		if forbidden.MatchString(out.String()) {
			t.Fatalf("letter action: %s", out.String())
		}
		for _, item := range append(view.Items, view.Actions...) {
			lower := strings.ToLower(item.Label)
			for _, removed := range []string{"refresh", "search", "filter", "completion", "page help"} {
				if strings.Contains(lower, removed) {
					t.Fatalf("%s retains %s", page.Title, item.Label)
				}
			}
			if item.Next != nil {
				pages = append(pages, item.Next())
			}
		}
	}
	for _, title := range []string{"Current Configuration", "Backup & Restore", "Local Overrides", "Servers", "Domain Exceptions", "TUN DNS Interception", "Checks & Queries", "View Rules", "Manage Custom Rules", "Import Rule Sets", "Start at Login", "View Changes", "Automatic Updates", "Encryption Settings", "For this session", "Save for future starts"} {
		if !seen[title] {
			t.Errorf("unreachable page: %s", title)
		}
	}
}

func TestPaginationDoesNotReloadAndNumbersMatchDisplay(t *testing.T) {
	for _, count := range []int{0, 1, 10, 11, 20, 21} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			loads := 0
			page := &Page{Title: "Resources", Load: func(context.Context, *Session) (PageView, error) {
				loads++
				view := PageView{Actions: []Item{{Label: "Action", Args: []string{"action"}}}}
				for i := 0; i < count; i++ {
					view.Items = append(view.Items, Item{Label: fmt.Sprint(i), Args: []string{"pick", fmt.Sprint(i)}})
				}
				return view, nil
			}}
			input := ""
			last := 0
			if count > 10 {
				last = (count - 1) / 10
			}
			for i := 0; i < last; i++ {
				input += "12\n"
			}
			remaining := count - last*10
			input += fmt.Sprintf("%d\n0\n", remaining+1)
			r := &fakeRunner{}
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(r, strings.NewReader(input), &out), page)
			if loads != 2 || !reflect.DeepEqual(r.calls, [][]string{{"action"}}) {
				t.Fatalf("loads=%d calls=%v output=%s", loads, r.calls, out.String())
			}
			if !strings.Contains(out.String(), fmt.Sprintf("%d. Action", remaining+1)) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestSelectorRejectsOldKeysWithoutRedrawAndTreatsZeroAsCancel(t *testing.T) {
	var out bytes.Buffer
	s := NewSession(&fakeRunner{}, strings.NewReader("\nr\nq\n/one\n+1\n01\n0\n"), &out)
	_, err := s.Select("Names", []string{"one"})
	if err != ErrBack || strings.Count(out.String(), "\nNames\n") != 1 || strings.Count(out.String(), "Choose a listed number.") != 5 {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
}

func TestFailedFormKeepsLiteralZeroAndRequiresExplicitResubmit(t *testing.T) {
	for _, input := range []string{"0\n0\n", "0\ns\n2\n0\n"} {
		calls := 0
		var out bytes.Buffer
		s := NewSession(&fakeRunner{}, strings.NewReader(input), &out)
		_, err := s.Form(context.Background(), []Field{{Key: "name", Label: "Name", Required: true}}, nil, func(v map[string]string) Result {
			calls++
			if v["name"] != "0" {
				t.Fatal(v)
			}
			return Result{Code: 1}
		})
		want := 1
		if strings.Contains(input, "s") {
			want = 2
		}
		if err != ErrBack || calls != want {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	}
}

func TestResultsFollowReloadedPageAndFailureDispatchesOnce(t *testing.T) {
	for _, result := range []Result{{Data: map[string]any{"result": "completed"}}, {Code: 1, Error: &CLIError{Message: "already changed; save failed"}}} {
		r := &fakeRunner{results: map[string]Result{"mutate": result}}
		var out bytes.Buffer
		RunPage(context.Background(), NewSession(r, strings.NewReader("1\n\n0\n"), &out), actionPage("Result page", Item{Label: "Change", Args: []string{"mutate"}}))
		text := out.String()
		marker := "result: completed"
		if result.Code != 0 {
			marker = "Error: already changed; save failed"
		}
		if len(r.calls) != 1 || strings.LastIndex(text, "\nResult page\n") > strings.Index(text, marker) || strings.Index(text, marker) > strings.LastIndex(text, "Choice: ") {
			t.Fatalf("calls=%v output=%s", r.calls, text)
		}
	}
}

func TestRawOutputWaitsBeforeRedisplayingPage(t *testing.T) {
	r := &fakeRunner{results: map[string]Result{"config show": {Data: map[string]any{"content": "RAW CONFIG\n"}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n\n0\n"), &out), actionPage("Configuration", Item{Label: "Show", Args: []string{"config", "show"}}))
	text := out.String()
	if !(strings.Index(text, "RAW CONFIG") < strings.Index(text, "Press Enter to return:") && strings.Index(text, "Press Enter to return:") < strings.LastIndex(text, "\nConfiguration\n")) {
		t.Fatal(text)
	}
}

func TestSubscriptionDetailOrderAndUnavailableDownloadComparison(t *testing.T) {
	s := NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{})
	view, _ := subscriptionDetailPage("feed").Load(context.Background(), s)
	labels := []string{}
	for _, item := range view.Actions {
		labels = append(labels, item.Label)
	}
	want := []string{"Download subscription", "Apply downloaded subscription", "View Changes", "Automatic Updates", "Encryption Settings", "Remove subscription and cache"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatal(labels)
	}
	changes, _ := view.Actions[2].Next().Load(context.Background(), s)
	if len(changes.Actions) != 1 || changes.Actions[0].Label != "Compare with target profile" {
		t.Fatal(changes)
	}
}
