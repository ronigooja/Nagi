package menu

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"io"
	"reflect"
	"strings"
	"testing"
)

type nodeChainRunner struct {
	calls    [][]string
	keys     []string
	fail     int
	conflict bool
	nodes    []menuProxyNode
	chains   []menuChainTarget
}

func (r *nodeChainRunner) RunInput(ctx context.Context, args []string, in io.Reader) Result {
	data, _ := io.ReadAll(in)
	r.keys = append(r.keys, string(data))
	return r.Run(ctx, args)
}
func (r *nodeChainRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string{}, args...))
	if len(args) < 3 {
		return Result{}
	}
	switch strings.Join(args[:3], " ") {
	case "proxy node list":
		return Result{Data: menuNodeList{Profile: "work", Nodes: r.nodes, GroupCandidates: []string{"World Select", "Backup"}}}
	case "proxy node show":
		return Result{Data: map[string]any{"profile": "work", "node": r.nodes[0]}}
	case "proxy chain candidates":
		return Result{Data: map[string]any{"nodes": r.nodes}}
	case "proxy chain list":
		return Result{Data: map[string]any{"chains": r.chainTargets()}}
	}
	if args[len(args)-1] == "--dry-run" {
		conflicts := []string{}
		if r.conflict {
			conflicts = append(conflicts, "cycle detected")
		}
		return Result{Data: map[string]any{"plan": map[string]any{"kind": "node", "targets": []string{args[4]}, "via": []string{}, "changes": []any{map[string]any{"name": "First Hop", "before": "Old Hop", "after": ""}}, "shared_use": []any{map[string]any{"kind": "node", "name": "Shared Node", "via": "First Hop"}}, "conflicts": conflicts}}}
	}
	if r.fail > 0 {
		r.fail--
		return Result{Code: 1, Error: &CLIError{Message: "operation failed https://secret.invalid/token"}}
	}
	if args[2] == "clear" {
		for i := range r.chains {
			if r.chains[i].Kind == args[3] && r.chains[i].Name == args[4] {
				r.chains[i].Via = nil
			}
		}
	}
	return Result{Data: map[string]any{"saved": true, "reloaded": false}}
}
func countMenuCall(calls [][]string, want []string) int {
	n := 0
	for _, c := range calls {
		if reflect.DeepEqual(c, want) {
			n++
		}
	}
	return n
}
func TestNodeTopLevelInventoryAndCRUDScope(t *testing.T) {
	r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "Ordinary", Type: "socks5"}, {Name: "Tail", Type: "tailscale", Tailscale: &menuTailscaleNode{ExitNode: "auto:any", AuthKeyConfigured: true}}}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	v, err := localizednodeListPage(s.language).Load(context.Background(), s)
	if err != nil || len(v.Items) != 2 || v.Items[0].Label != "Ordinary (socks5)" {
		t.Fatalf("%+v %v", v, err)
	}
	v, err = localizednodeDetailPage(s.language, "Ordinary").Load(context.Background(), s)
	if err != nil || len(v.Actions) != 1 {
		t.Fatalf("ordinary CRUD leaked: %+v %v", v, err)
	}
}
func TestTailscaleAddGroupsSingleCommitAndRetainedRetry(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		r := &nodeChainRunner{fail: 1}
		var out bytes.Buffer
		s := NewSessionWithLanguage(r, strings.NewReader("1\n2\n3\nTail Node\nauto:any\n\n\ntskey-auth-example-placeholder\n6\n"), &out, i18n.New(language))
		err := addTailscale(context.Background(), s)
		if err != nil {
			t.Fatalf("%v %s", err, out.String())
		}
		want := []string{"proxy", "node", "add", "tailscale", "Tail Node", "auto:any", "--auth-key-stdin", "--control-url", tailscaleControlURL, "--group", "World Select", "--group", "Backup"}
		if countMenuCall(r.calls, want) != 2 || len(r.keys) != 2 || r.keys[0] != r.keys[1] {
			t.Fatalf("calls=%v keys=%d output=%s", r.calls, len(r.keys), out.String())
		}
		if !strings.Contains(out.String(), "https://secret.invalid/token") {
			t.Fatalf("error URL context was hidden: %s", out.String())
		}
	}
}
func TestChainOrderedSelectionApplyAndExplicitRetry(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.SimplifiedChinese} {
		r := &nodeChainRunner{fail: 1, nodes: []menuProxyNode{{Name: "First Hop", Type: "tailscale"}, {Name: "Last Hop", Type: "socks5"}, {Name: "Target Node", Type: "ss"}}}
		var out bytes.Buffer
		s := NewSessionWithLanguage(r, strings.NewReader("1\n2\n3\n4\n4\n0\n"), &out, i18n.New(language))
		RunPage(context.Background(), s, localizedchainDraftPage(s.language))
		want := []string{"proxy", "chain", "set", "node", "Target Node", "--via", "First Hop", "--via", "Last Hop"}
		if countMenuCall(r.calls, want) != 2 {
			t.Fatalf("calls=%v output=%s", r.calls, out.String())
		}
		for _, text := range []string{"First Hop → Last Hop → Target Node"} {
			if !strings.Contains(out.String(), text) {
				t.Fatalf("missing %s: %s", text, out.String())
			}
		}
		if !strings.Contains(out.String(), "https://secret.invalid/token") {
			t.Fatalf("error URL context was hidden: %s", out.String())
		}
	}
}

func TestChainConflictsNeverCommitAndCancel(t *testing.T) {
	r := &nodeChainRunner{conflict: true, nodes: []menuProxyNode{{Name: "First Hop", Type: "tailscale"}, {Name: "Target Node", Type: "ss"}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n2\n3\n0\n"), &out), localizedchainDraftPage(i18n.New(i18n.English)))
	if !strings.Contains(out.String(), "cycle detected") {
		t.Fatal(out.String())
	}
	assertNoChainMutation(t, r.calls)
}

func assertNoChainMutation(t *testing.T, calls [][]string) {
	t.Helper()
	for _, c := range calls {
		if len(c) > 2 && c[0] == "proxy" && c[1] == "chain" && (c[2] == "set" || c[2] == "clear") && c[len(c)-1] != "--dry-run" {
			t.Fatal(calls)
		}
	}
}

func (r *nodeChainRunner) chainTargets() []menuChainTarget {
	if r.chains != nil {
		return r.chains
	}
	values := []menuChainTarget{}
	for _, n := range r.nodes {
		values = append(values, menuChainTarget{Kind: "node", Name: n.Name, Type: n.Type})
	}
	return append(values, menuChainTarget{Kind: "provider", Name: "work-feed"}, menuChainTarget{Kind: "subscription", Name: "work-sub"})
}

func TestGroupSelectionPaginationAndCancellation(t *testing.T) {
	r := &nodeChainRunner{}
	var out bytes.Buffer
	_, err := selectNodeGroups(context.Background(), NewSession(r, strings.NewReader("0\n"), &out), nil, "Continue")
	if err != ErrBack || len(r.calls) != 1 {
		t.Fatalf("%v %v", err, r.calls)
	}
	v := PageView{Actions: []Item{{Label: "Continue"}}}
	for i := 0; i < 12; i++ {
		v.Items = append(v.Items, Item{Name: fmt.Sprint(i), Label: fmt.Sprint(i)})
	}
	s := NewSession(r, strings.NewReader("12\n2\n"), &out)
	item, err := selectMenuItem(s, "Groups", v)
	if err != nil || item.Name != "11" || !strings.Contains(out.String(), "Page 2/2") {
		t.Fatalf("%+v %v %s", item, err, out.String())
	}
}

func TestTailscaleAddDefaultsToNoGroupAndEOFDoesNotRetry(t *testing.T) {
	for _, fail := range []int{0, 1} {
		r := &nodeChainRunner{fail: fail}
		var out bytes.Buffer
		s := NewSession(r, strings.NewReader("3\nTail\nauto:any\n\n\ntskey-auth-placeholder\n"), &out)
		err := addTailscale(context.Background(), s)
		if fail == 0 && err != nil || fail == 1 && err != ErrQuit {
			t.Fatalf("fail=%d err=%v output=%s", fail, err, out.String())
		}
		commits := 0
		for _, c := range r.calls {
			if len(c) > 2 && c[2] == "add" {
				commits++
				for _, arg := range c {
					if arg == "--group" {
						t.Fatal(c)
					}
				}
			}
		}
		if commits != 1 {
			t.Fatal(r.calls)
		}
	}
}
func TestChainFailedReadClearsStaleTargets(t *testing.T) {
	r := &featureRunner{results: map[string]Result{}}
	s := NewSession(r, strings.NewReader(""), io.Discard)
	v, err := localizedchainScopePage(s.language).Load(context.Background(), s)
	if err == nil || len(v.Items) != 0 || len(v.Actions) != 0 {
		t.Fatalf("%+v %v", v, err)
	}
}
func TestTailscaleEditAndDeleteUseNodeCommands(t *testing.T) {
	r := &nodeChainRunner{nodes: []menuProxyNode{{Name: "Tail Node", Type: "tailscale", Tailscale: &menuTailscaleNode{ExitNode: "auto:any", ControlURL: tailscaleControlURL}}}}
	s := NewSession(r, strings.NewReader("\n\n\n\n"), io.Discard)
	if err := editTailscale(context.Background(), s, *r.nodes[0].Tailscale); err != nil {
		t.Fatal(err)
	}
	if len(r.keys) != 0 {
		t.Fatal("blank key should preserve existing key")
	}
	v, err := localizednodeDetailPage(s.language, "Tail Node").Load(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	n, err := v.Actions[len(v.Actions)-1].Navigate(context.Background(), s)
	if err != nil || !n.Back || !hasProxyCall(r.calls, []string{"proxy", "node", "remove", "Tail Node"}) {
		t.Fatalf("%v %v %v", n, err, r.calls)
	}
}
