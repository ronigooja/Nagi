package menu

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func groupAction(t *testing.T, v PageView, id string) Item {
	t.Helper()
	for _, a := range v.Actions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("missing action %s", id)
	return Item{}
}
func TestProxyGroupDraftSaveFailureKeepsAllFieldsAndRetriesExplicitly(t *testing.T) {
	ctx := context.Background()
	args := []string{"proxy", "group", "add", "My Group", "--type", "url-test", "--node", "DIRECT", "--node", "Node A", "--provider", "Remote Feed", "--url", "https://cp.cloudflare.com/generate_204", "--interval", "300", "--tolerance", "0"}
	r := &featureRunner{results: map[string]Result{
		"proxy\x00node\x00list":    {Data: menuNodeList{Nodes: []menuProxyNode{{Name: "Node A"}}, ProviderCandidates: []string{"Remote Feed"}}},
		strings.Join(args, "\x00"): {Code: 1, Error: &CLIError{Message: "validation failed"}},
	}}
	// Name, type, node selection (DIRECT stays first), provider selection.
	s := NewSession(r, strings.NewReader("My Group\n2\n4\n5\n1\n2\n"), io.Discard)
	page := groupDraftPage(s.language, "")
	v := loadChainPage(t, page, s)
	for _, a := range v.Actions {
		if a.ID == "group.save" {
			t.Fatal("save before required name")
		}
	}
	if err := groupAction(t, v, "group.name").Action(ctx, s); err != nil {
		t.Fatal(err)
	}
	v = loadChainPage(t, page, s)
	for _, id := range []string{"group.type", "group.nodes", "group.providers"} {
		if err := groupAction(t, v, id).Action(ctx, s); err != nil {
			t.Fatal(err)
		}
		v = loadChainPage(t, page, s)
	}
	if len(r.calls) != 2 {
		t.Fatalf("draft caused a mutation: %v", r.calls)
	}
	nav, err := groupAction(t, v, "group.save").Navigate(ctx, s)
	if err != nil || nav.Back {
		t.Fatalf("nav=%v err=%v calls=%v", nav, err, r.calls)
	}
	if !reflect.DeepEqual(r.calls[len(r.calls)-1], args) {
		t.Fatalf("got=%v want=%v", r.calls[len(r.calls)-1], args)
	}
	v = loadChainPage(t, page, s)
	if !strings.Contains(v.Context, "My Group") || !strings.Contains(v.Context, "Node A") || !strings.Contains(v.Context, "Remote Feed") {
		t.Fatal(v.Context)
	}
	if len(r.calls) != 3 {
		t.Fatal("automatically retried", r.calls)
	}
	r.results[strings.Join(args, "\x00")] = Result{Data: map[string]any{"saved": true}}
	nav, err = groupAction(t, v, "group.save").Navigate(ctx, s)
	if err != nil || !nav.Back {
		t.Fatalf("nav=%v err=%v", nav, err)
	}
	if countMenuCall(r.calls, args) != 2 {
		t.Fatal(r.calls)
	}
}
func TestProxyGroupMemberOrderCancellationAndOfflineActions(t *testing.T) {
	s := NewSession(&fakeRunner{}, strings.NewReader("2\n1\n3\n"), io.Discard)
	selected, err := chooseGroupMembers(context.Background(), s, []string{"B", "A"}, []string{"A"}, "members")
	if err != nil || !reflect.DeepEqual(selected, []string{"B"}) {
		t.Fatalf("%v %v", selected, err)
	}
	s = NewSession(&fakeRunner{}, strings.NewReader("1\n0\n"), io.Discard)
	initial := []string{"A"}
	if _, err := chooseGroupMembers(context.Background(), s, []string{"A", "B"}, initial, "members"); err == nil {
		t.Fatal("cancel ignored")
	}
	if !reflect.DeepEqual(initial, []string{"A"}) {
		t.Fatal(initial)
	}
	r := &featureRunner{results: map[string]Result{"proxy\x00groups": {Code: 1, Error: &CLIError{Message: "stopped"}}, "status": {Data: map[string]any{"running": false}}}}
	s = NewSession(r, strings.NewReader(""), io.Discard)
	v, err := proxyGroupsPage().Load(context.Background(), s)
	if err != nil || !strings.Contains(v.Context, "stopped") {
		t.Fatalf("view=%v err=%v", v, err)
	}
	if groupAction(t, v, "group.create").Next == nil || groupAction(t, v, "group.manage").Next == nil {
		t.Fatal("offline actions missing")
	}
}
func TestProxyGroupManagementLocalizationAndDeletionRefresh(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		t.Run(string(lang), func(t *testing.T) {
			r := &featureRunner{results: map[string]Result{
				"proxy\x00group\x00list":              {Data: map[string]any{"group_configs": []menuGroupConfig{{Name: "Z Group", Type: "select"}, {Name: "A Group", Type: "url-test"}}}},
				"proxy\x00group\x00show\x00Z Group":   {Data: map[string]any{"group_config": menuGroupConfig{Name: "Z Group", Type: "select", Proxies: []string{"DIRECT"}}}},
				"proxy\x00group\x00remove\x00Z Group": {Data: map[string]any{"saved": true}},
			}}
			var out bytes.Buffer
			s := NewSessionWithLanguage(r, strings.NewReader(""), &out, i18n.New(lang))
			v := loadChainPage(t, groupManagementPage(s.language), s)
			if v.Items[0].Name != "Z Group" || v.Items[1].Name != "A Group" {
				t.Fatal(v.Items)
			}
			if lang == i18n.SimplifiedChinese && v.Actions[0].Label != "创建代理组" {
				t.Fatal(v.Actions)
			}
			detail := v.Items[0].Next()
			d := loadChainPage(t, detail, s)
			nav, err := groupAction(t, d, "group.remove").Navigate(context.Background(), s)
			if err != nil || !nav.Back {
				t.Fatalf("%v %v", nav, err)
			}
			r.results["proxy\x00group\x00show\x00Z Group"] = Result{Code: 1, Error: &CLIError{Message: "missing"}}
			d, err = detail.Load(context.Background(), s)
			if err == nil || len(d.Actions) != 0 {
				t.Fatal("stale actions after failed read")
			}
		})
	}
}
func TestProxyGroupDraftEOFAndEditKeepTypeSpecificSettings(t *testing.T) {
	r := &featureRunner{results: map[string]Result{"proxy\x00group\x00show\x00My Group": {Data: map[string]any{"group_config": menuGroupConfig{Name: "My Group", Type: "load-balance", Use: []string{"Feed"}, Strategy: "sticky-sessions"}}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	page := groupDraftPage(s.language, "My Group")
	v := loadChainPage(t, page, s)
	if err := groupAction(t, v, "group.type").Action(context.Background(), s); err != ErrQuit {
		t.Fatal(err)
	}
	v = loadChainPage(t, page, s)
	if !strings.Contains(v.Context, "sticky-sessions") {
		t.Fatal(v.Context)
	}
	if len(r.calls) != 1 {
		t.Fatal("EOF dispatched mutation", r.calls)
	}
}

func TestProxyGroupMutationFeedbackDoesNotDumpCommandMetadata(t *testing.T) {
	for _, operation := range []string{"add", "edit", "remove"} {
		var out bytes.Buffer
		s := NewSession(&fakeRunner{}, strings.NewReader(""), &out)
		s.ShowResult([]string{"proxy", "group", operation, "My Group"}, Result{Data: map[string]any{"saved": true, "profile": "work", "resource": "group", "reloaded": true}})
		if !strings.Contains(out.String(), "Proxy group") || strings.Contains(out.String(), "resource:") || strings.Contains(out.String(), "profile:") || strings.Contains(out.String(), "reloaded:") {
			t.Fatal(out.String())
		}
	}
}

func TestGeneratedGlobalKeepsLiveDetails(t *testing.T) {
	r := &featureRunner{results: map[string]Result{"proxy\x00show\x00GLOBAL": {Data: map[string]any{"name": "GLOBAL", "type": "Selector", "all": []string{"DIRECT"}}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	v := loadChainPage(t, proxyGroupPage("GLOBAL"), s)
	a := groupAction(t, v, "proxy.proxyNodesPage.item2")
	if a.Next != nil || !reflect.DeepEqual(a.Args, []string{"proxy", "show", "GLOBAL"}) {
		t.Fatal(a)
	}
}

func TestProxyGroupDetailCurrentSelectionAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		language    i18n.Language
		typeLine    string
		current     string
		selectLabel string
	}{
		{i18n.English, "Type: select", "Current selection: ", "Select node or group"},
		{i18n.SimplifiedChinese, "类型：select", "当前选择：", "选择节点或代理组"},
	} {
		t.Run(string(tc.language), func(t *testing.T) {
			const name = "Default Group"
			const liveKey = "proxy\x00show\x00" + name
			r := &featureRunner{results: map[string]Result{
				"proxy\x00group\x00show\x00" + name: {Data: map[string]any{"group_config": menuGroupConfig{
					Name: name, Type: "select", Proxies: []string{"First Member", "Second Member"}, Use: []string{"Remote Feed"},
				}}},
				liveKey: {Data: menuProxyGroup{Now: "Automatic Group", SelectedStatus: "available"}},
			}}
			var out bytes.Buffer
			s := NewSessionWithLanguage(r, strings.NewReader(""), &out, i18n.New(tc.language))
			page := groupConfigPage(s.language, name)
			view := loadChainPage(t, page, s)
			s.render(page, view, 0, 2)
			if !strings.Contains(out.String(), tc.typeLine) || !strings.Contains(out.String(), tc.current+"Automatic Group") {
				t.Fatal(out.String())
			}
			for _, unwanted := range []string{"First Member", "Second Member", "Remote Feed", "Providers", "节点提供商", "Nodes and groups", "节点和代理组"} {
				if strings.Contains(out.String(), unwanted) {
					t.Fatalf("detail includes %q: %s", unwanted, out.String())
				}
			}
			if len(view.Actions) != 4 || view.Actions[0].Label != tc.selectLabel || view.Actions[3].ID != "group.remove" {
				t.Fatal(view.Actions)
			}
			// Returning from selection rereads the live value without changing configuration.
			r.results[liveKey] = Result{Data: menuProxyGroup{Now: "Second Member", SelectedStatus: "available"}}
			view = loadChainPage(t, page, s)
			if !strings.Contains(view.Context, tc.current+"Second Member") || strings.Contains(view.Context, "Automatic Group") {
				t.Fatal(view.Context)
			}
			// An unavailable runtime must not retain the previous choice or block editing.
			r.results[liveKey] = Result{Code: 1, Error: &CLIError{Message: "engine stopped"}}
			view = loadChainPage(t, page, s)
			if !strings.Contains(view.Context, tc.current+menuState(s.language, "unknown")) || !strings.Contains(view.Context, "engine stopped") || strings.Contains(view.Context, "Second Member") {
				t.Fatal(view.Context)
			}
			if groupAction(t, view, "group.edit").Next == nil || groupAction(t, view, "group.select_node").Next == nil {
				t.Fatal("runtime failure hid actions")
			}
			for _, state := range []menuProxyGroup{{}, {Now: "Second Member", SelectedStatus: "removed"}, {Now: "Second Member", SelectedStatus: "unavailable"}} {
				r.results[liveKey] = Result{Data: state}
				view = loadChainPage(t, page, s)
				want := menuState(s.language, "none")
				if state.Now != "" {
					want = state.Now + " (" + menuState(s.language, state.SelectedStatus) + ")"
				}
				if !strings.Contains(view.Context, tc.current+want) || strings.Contains(view.Context, "engine stopped") {
					t.Fatal(view.Context)
				}
			}
			for _, call := range r.calls {
				if !reflect.DeepEqual(call, []string{"proxy", "group", "show", name}) && !reflect.DeepEqual(call, []string{"proxy", "show", name}) {
					t.Fatalf("summary dispatched unexpected command: %v", call)
				}
			}
		})
	}
}
