package menu

import (
	"context"
	"io"
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
