package menu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func groupOrderRunner(names ...string) *featureRunner {
	groups := []menuGroupConfig{}
	for _, name := range names {
		groups = append(groups, menuGroupConfig{Name: name, Type: "select"})
	}
	return &featureRunner{results: map[string]Result{"proxy\x00group\x00list": {Data: map[string]any{"profile": "work", "group_configs": groups}}}}
}
func visibleOrder(v PageView) []string {
	names := []string{}
	for _, item := range v.Items {
		names = append(names, item.Name)
	}
	return names
}
func TestProxyGroupOrderingDraftMovesRetriesAndSavesOnlyOnce(t *testing.T) {
	ctx := context.Background()
	r := groupOrderRunner("A Group", "B Group", "C Group", "D Group")
	args := []string{"proxy", "group", "order", "D Group", "B Group", "A Group", "C Group"}
	r.results[strings.Join(args, "\x00")] = Result{Code: 1, Error: &CLIError{Message: "save failed"}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	page := groupOrderPage(s.language)
	move := func(name, id string) {
		t.Helper()
		v := loadChainPage(t, page, s)
		for _, item := range v.Items {
			if item.Name == name {
				detail := loadChainPage(t, item.Next(), s)
				nav, err := groupAction(t, detail, id).Navigate(ctx, s)
				if err != nil || !nav.Back {
					t.Fatalf("%v %v", nav, err)
				}
				return
			}
		}
		t.Fatal("missing group", name)
	}
	move("A Group", "group.order.down")   // B A C D
	move("D Group", "group.order.top")    // D B A C
	move("B Group", "group.order.bottom") // D A C B
	move("B Group", "group.order.up")     // D A B C
	move("B Group", "group.order.up")     // D B A C
	if len(r.calls) != 1 {
		t.Fatal("movement persisted configuration", r.calls)
	}
	v := loadChainPage(t, page, s)
	if !reflect.DeepEqual(visibleOrder(v), args[3:]) {
		t.Fatal(visibleOrder(v))
	}
	nav, err := groupAction(t, v, "group.order.save").Navigate(ctx, s)
	if err != nil || nav.Back {
		t.Fatalf("%v %v", nav, err)
	}
	v = loadChainPage(t, page, s)
	if !reflect.DeepEqual(visibleOrder(v), args[3:]) || countMenuCall(r.calls, args) != 1 {
		t.Fatal("draft lost or save retried", r.calls)
	}
	r.results[strings.Join(args, "\x00")] = Result{Data: map[string]any{"saved": true, "action": "order", "resource": "group"}}
	nav, err = groupAction(t, v, "group.order.save").Navigate(ctx, s)
	if err != nil || !nav.Back || countMenuCall(r.calls, args) != 2 {
		t.Fatalf("%v %v %v", nav, err, r.calls)
	}
}
func TestProxyGroupOrderFailedReadAndProfileSwitchHideOrBlockSave(t *testing.T) {
	r := groupOrderRunner("A", "B")
	s := NewSession(r, strings.NewReader(""), io.Discard)
	page := groupOrderPage(s.language)
	v := loadChainPage(t, page, s)
	r.results["proxy\x00group\x00list"] = Result{Data: map[string]any{"profile": "other"}}
	nav, err := groupAction(t, v, "group.order.save").Navigate(context.Background(), s)
	if err == nil || nav.Back {
		t.Fatal("profile switch not blocked")
	}
	for _, call := range r.calls {
		if len(call) > 2 && call[2] == "order" {
			t.Fatal("mutation after profile switch")
		}
	}
	r.results["proxy\x00group\x00list"] = Result{Code: 1, Error: &CLIError{Message: "read failed"}}
	failed, err := groupOrderPage(s.language).Load(context.Background(), s)
	if err == nil || len(failed.Items) != 0 || len(failed.Actions) != 0 {
		t.Fatal("stale actions after failure")
	}
}
func TestProxyGroupOrderCancellationPaginationAndLocalization(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		t.Run(string(lang), func(t *testing.T) {
			names := []string{"G01", "G02", "G03", "G04", "G05", "G06", "G07", "G08", "G09", "G10", "G11", "G12"}
			r := groupOrderRunner(names...)
			s := NewSessionWithLanguage(r, strings.NewReader(""), io.Discard, i18n.New(lang))
			v := loadChainPage(t, groupOrderPage(s.language), s)
			if !reflect.DeepEqual(visibleOrder(v), names) {
				t.Fatal(visibleOrder(v))
			}
			if lang == i18n.SimplifiedChinese && groupAction(t, v, "group.order.save").Label != "保存显示顺序" {
				t.Fatal(v.Actions)
			}
			for _, item := range v.Items {
				if item.Name == "G01" {
					detail := loadChainPage(t, item.Next(), s)
					want := "移动到指定位置"
					if lang == i18n.English {
						want = "Move to position"
					}
					if got := groupAction(t, detail, "group.order.position").Label; got != want {
						t.Fatalf("position label = %q, want %q", got, want)
					}
				}
			}
			// EOF exits the unsaved draft, including after numeric pagination.
			session := NewSessionWithLanguage(r, strings.NewReader("12\n"), io.Discard, i18n.New(lang))
			RunPage(context.Background(), session, groupOrderPage(s.language))
			for _, call := range r.calls {
				if len(call) > 2 && call[2] == "order" {
					t.Fatal("EOF saved draft", r.calls)
				}
			}
		})
	}
}

func TestProxyGroupOrderMoveToPositionValidatesAndKeepsDraftOnly(t *testing.T) {
	ctx := context.Background()
	r := groupOrderRunner("A", "B", "C", "D")
	// A malformed value and an out-of-range position are rejected before 2 is accepted.
	s := NewSession(r, strings.NewReader("2.5\n5\n2\n"), io.Discard)
	page := groupOrderPage(s.language)
	v := loadChainPage(t, page, s)
	var selected Item
	for _, item := range v.Items {
		if item.Name == "D" {
			selected = item
		}
	}
	if selected.Next == nil {
		t.Fatal("missing group detail page")
	}
	detail := loadChainPage(t, selected.Next(), s)
	position := groupAction(t, detail, "group.order.position")
	nav, err := position.Navigate(ctx, s)
	if err != nil || !nav.Back {
		t.Fatalf("%v %v", nav, err)
	}
	v = loadChainPage(t, page, s)
	if want := []string{"A", "D", "B", "C"}; !reflect.DeepEqual(visibleOrder(v), want) {
		t.Fatalf("position draft = %v, want %v", visibleOrder(v), want)
	}
	args := []string{"proxy", "group", "order", "A", "D", "B", "C"}
	r.results[strings.Join(args, "\x00")] = Result{Code: 1, Error: &CLIError{Message: "save failed"}}
	nav, err = groupAction(t, v, "group.order.save").Navigate(ctx, s)
	if err != nil || nav.Back {
		t.Fatalf("failed save: %v %v", nav, err)
	}
	v = loadChainPage(t, page, s)
	if !reflect.DeepEqual(visibleOrder(v), []string{"A", "D", "B", "C"}) || countMenuCall(r.calls, args) != 1 {
		t.Fatalf("failed save lost target-position draft: %v", r.calls)
	}
	if len(r.calls) != 3 {
		t.Fatalf("unexpected calls while moving position: %v", r.calls)
	}
}

func TestProxyGroupOrderMoveToPositionCancellationLeavesDraftUnchanged(t *testing.T) {
	ctx := context.Background()
	r := groupOrderRunner("A", "B", "C")
	s := NewSession(r, strings.NewReader(""), io.Discard)
	s.interrupts = make(chan os.Signal, 1)
	s.interrupts <- os.Interrupt
	page := groupOrderPage(s.language)
	v := loadChainPage(t, page, s)
	var selected Item
	for _, item := range v.Items {
		if item.Name == "B" {
			selected = item
		}
	}
	detail := loadChainPage(t, selected.Next(), s)
	_, err := groupAction(t, detail, "group.order.position").Navigate(ctx, s)
	if err != ErrBack {
		t.Fatalf("cancel error = %v, want ErrBack", err)
	}
	v = loadChainPage(t, page, s)
	if want := []string{"A", "B", "C"}; !reflect.DeepEqual(visibleOrder(v), want) {
		t.Fatalf("canceled position changed draft: %v", visibleOrder(v))
	}
	for _, call := range r.calls {
		if len(call) > 2 && call[2] == "order" {
			t.Fatalf("canceled position persisted: %v", r.calls)
		}
	}
}

func TestProxyGroupOrderPositionInvalidInputFeedbackPrecedesRetryPrompt(t *testing.T) {
	r := groupOrderRunner("A", "B", "C", "D")
	var output bytes.Buffer
	s := NewSession(r, strings.NewReader("4\n3\n5\n2\nq\n"), &output)
	if code := RunPage(context.Background(), s, groupOrderPage(s.language)); code != 0 {
		t.Fatalf("RunPage exit code = %d", code)
	}
	text := output.String()
	firstPrompt := strings.Index(text, "Target position (1-4) (Ctrl-C cancels): ")
	secondPrompt := strings.Index(text[firstPrompt+1:], "Target position (1-4) (Ctrl-C cancels): ")
	if firstPrompt < 0 || secondPrompt < 0 {
		t.Fatalf("position retry prompts missing: %q", text)
	}
	secondPrompt += firstPrompt + 1
	warning := "Enter a whole number within the displayed range."
	warningAt := strings.Index(text, warning)
	if warningAt <= firstPrompt || warningAt >= secondPrompt || strings.Count(text, warning) != 1 {
		t.Fatalf("warning was not shown once between the invalid and retried prompts: %q", text)
	}
	if s.pending != "" {
		t.Fatalf("stale feedback remained pending: %q", s.pending)
	}
}

func TestProxyGroupOrderMovesUseDisplayedPage(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		for _, tc := range []struct {
			name    string
			input   string
			from    int
			to      int
			limit   int
			invalid bool
		}{
			{"first page", "10\n3\n1\n11\n", 9, 0, 10, false},
			{"second page", "12\n10\n3\n1\n11\n", 19, 10, 10, false},
			{"last page rejects action number", "12\n12\n6\n3\n7\n1\n7\n", 25, 20, 6, true},
			{"last page moves down", "12\n12\n1\n3\n6\n7\n", 20, 25, 6, false},
			{"first page bottom", "5\n4\n11\n", 4, 9, 0, false},
			{"second page top", "12\n5\n2\n11\n", 14, 10, 0, false},
			{"second page bottom", "12\n5\n4\n11\n", 14, 19, 0, false},
			{"last page top", "12\n12\n4\n2\n7\n", 23, 20, 0, false},
			{"last page bottom", "12\n12\n3\n4\n7\n", 22, 25, 0, false},
		} {
			t.Run(string(lang)+"/"+tc.name, func(t *testing.T) {
				names := make([]string, 26)
				for i := range names {
					names[i] = fmt.Sprintf("G%02d", i+1)
				}
				r := groupOrderRunner(names...)
				var output bytes.Buffer
				s := NewSessionWithLanguage(r, strings.NewReader(tc.input), &output, i18n.New(lang))
				if code := RunPage(context.Background(), s, groupOrderPage(s.language)); code != 0 {
					t.Fatalf("RunPage exit code = %d", code)
				}
				want := append([]string(nil), names...)
				moveGroupOrder(want, tc.from, tc.to)
				args := append([]string{"proxy", "group", "order"}, want...)
				if countMenuCall(r.calls, args) != 1 || len(r.calls) != 3 {
					t.Fatalf("expected one save with page-relative position: %v", r.calls)
				}
				prompt := fmt.Sprintf("%s (1-%d)", groupText(s.language, "target_position", "Target position"), tc.limit)
				count := 1
				if tc.limit == 0 {
					count = 0
				}
				if tc.invalid {
					count = 2
				}
				if strings.Count(output.String(), prompt) != count {
					t.Fatalf("expected %d prompts with page range %q: %s", count, prompt, output.String())
				}
			})
		}
	}
}

func TestProxyGroupOrderMovementPageBoundaries(t *testing.T) {
	ctx := context.Background()
	for _, total := range []int{21, 26} {
		names := make([]string, total)
		for i := range names {
			names[i] = fmt.Sprintf("G%02d", i+1)
		}
		for index := range names {
			start := index / pageSize * pageSize
			end := min(start+pageSize, total)
			for _, action := range []struct {
				id      string
				target  int
				visible bool
			}{
				{"up", index - 1, index > start},
				{"top", start, index > start},
				{"down", index + 1, index < end-1},
				{"bottom", end - 1, index < end-1},
			} {
				t.Run(fmt.Sprintf("%d/%d/%s", total, index+1, action.id), func(t *testing.T) {
					r := groupOrderRunner(names...)
					s := NewSession(r, strings.NewReader(""), io.Discard)
					page := groupOrderPage(s.language)
					v := loadChainPage(t, page, s)
					detail := loadChainPage(t, v.Items[index].Next(), s)
					var selected *Item
					for i := range detail.Actions {
						if detail.Actions[i].ID == "group.order."+action.id {
							selected = &detail.Actions[i]
						}
					}
					if (selected != nil) != action.visible {
						t.Fatalf("action visibility at page boundary: %v", detail.Actions)
					}
					if selected == nil {
						return
					}
					nav, err := selected.Navigate(ctx, s)
					if err != nil || !nav.Back {
						t.Fatalf("navigation: %v, %v", nav, err)
					}
					got := visibleOrder(loadChainPage(t, page, s))
					if got[action.target] != names[index] || !reflect.DeepEqual(got[:start], names[:start]) || !reflect.DeepEqual(got[end:], names[end:]) {
						t.Fatalf("move escaped selected page or missed target %d: %v", action.target+1, got)
					}
					if len(r.calls) != 1 {
						t.Fatalf("movement persisted before Save: %v", r.calls)
					}
				})
			}
		}
	}
}
