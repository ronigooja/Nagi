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
)

// This runner models only the public CLI contract; it never reads configuration,
// auth-key files, or mihomo state. Mutations alter the subsequent read snapshots.
type tailscaleRunner struct {
	calls     [][]string
	nodes     []menuTailscaleNode
	targets   []menuTailscaleTarget
	fail      map[string]int
	keyInputs []string
}

func (r *tailscaleRunner) RunInput(ctx context.Context, args []string, input io.Reader) Result {
	key, err := io.ReadAll(input)
	if err != nil {
		return Result{Code: 1, Error: &CLIError{Message: "input read failed"}}
	}
	r.keyInputs = append(r.keyInputs, string(key))
	return r.Run(ctx, args)
}

func (r *tailscaleRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string(nil), args...))
	key := strings.Join(args, " ")
	if r.fail[key] > 0 {
		r.fail[key]--
		return Result{Code: 1, Error: &CLIError{Message: "operation failed; existing configuration retained https://secret.invalid/key"}}
	}
	if reflect.DeepEqual(args, []string{"status"}) {
		return Result{Data: map[string]any{"running": false, "profile": "work"}}
	}
	if reflect.DeepEqual(args, []string{"proxy", "groups"}) {
		return Result{Code: 1, Error: &CLIError{Message: "engine stopped"}}
	}
	if len(args) < 3 || args[0] != "proxy" || args[1] != "tailscale" {
		return Result{Code: 1, Error: &CLIError{Message: "unexpected command"}}
	}
	switch args[2] {
	case "list":
		return Result{Data: map[string]any{"profile": "work", "nodes": r.nodes}}
	case "targets":
		return Result{Data: map[string]any{"profile": "work", "targets": r.targets}}
	case "show":
		for _, node := range r.nodes {
			if node.Name == args[3] {
				bindings := []menuTailscaleTarget{}
				for _, target := range r.targets {
					if target.DialerProxy == node.Name {
						bindings = append(bindings, target)
					}
				}
				return Result{Data: map[string]any{"profile": "work", "node": node, "bindings": bindings}}
			}
		}
		return Result{Code: 1, Error: &CLIError{Message: "node missing"}}
	case "bind":
		for i := range r.targets {
			if r.targets[i].Kind == args[4] && r.targets[i].Name == args[5] {
				r.targets[i].DialerProxy = args[3]
			}
		}
	case "unbind":
		for i := range r.targets {
			if r.targets[i].Kind == args[3] && r.targets[i].Name == args[4] {
				r.targets[i].DialerProxy = ""
			}
		}
	case "remove":
		for i, node := range r.nodes {
			if node.Name == args[3] {
				r.nodes = append(r.nodes[:i], r.nodes[i+1:]...)
				break
			}
		}
	}
	return Result{Data: map[string]any{"profile": "work", "action": args[2], "saved": true, "reloaded": false}}
}

func tailscaleFixture() *tailscaleRunner {
	return &tailscaleRunner{nodes: []menuTailscaleNode{{Name: "Tail Shared", ExitNode: "100.64.0.1", AuthKeyConfigured: true}}, targets: []menuTailscaleTarget{
		{Kind: "node", Name: "SS Node", Type: "ss"}, {Kind: "node", Name: "VMess Node", Type: "vmess"},
		{Kind: "node", Name: "VLESS Node", Type: "vless"}, {Kind: "node", Name: "Trojan Node", Type: "trojan"},
		{Kind: "provider", Name: "Daily Provider", Type: "http"},
	}, fail: map[string]int{}}
}

func countTailscaleCall(calls [][]string, want ...string) int {
	count := 0
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			count++
		}
	}
	return count
}

func TestTailscaleOfflineEntryThroughHome(t *testing.T) {
	r := tailscaleFixture()
	var out bytes.Buffer
	Run(context.Background(), NewSession(r, strings.NewReader("2\n3\n1\n0\n0\n0\n0\n"), &out))
	if countTailscaleCall(r.calls, "proxy", "tailscale", "show", "Tail Shared") != 1 {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
	for _, text := range []string{"Mihomo: stopped", "Tailscale", "Auth key: configured", "Profile: work", "Exit node: 100.64.0.1"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q: %s", text, out.String())
		}
	}
}

func TestTailscaleOneNodeSharedAcrossProtocolsAndProvider(t *testing.T) {
	for index, target := range tailscaleFixture().targets {
		t.Run(target.Type, func(t *testing.T) {
			r := tailscaleFixture()
			var out bytes.Buffer
			RunPage(context.Background(), NewSession(r, strings.NewReader(fmt.Sprintf("1\n2\n%d\n0\n0\n", index+1)), &out), tailscaleListPage())
			if countTailscaleCall(r.calls, "proxy", "tailscale", "bind", "Tail Shared", target.Kind, target.Name) != 1 || r.targets[index].DialerProxy != "Tail Shared" {
				t.Fatalf("calls=%v output=%s", r.calls, out.String())
			}
			if !strings.Contains(out.String(), "all nodes") || !strings.Contains(out.String(), "Configuration saved.") {
				t.Fatal(out.String())
			}
		})
	}
	r := tailscaleFixture()
	var out bytes.Buffer
	// Each success reloads the bindings list, increasing the numeric action index.
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n2\n1\n3\n2\n4\n3\n5\n4\n6\n5\n0\n0\n"), &out), tailscaleListPage())
	for _, target := range r.targets {
		if target.DialerProxy != "Tail Shared" {
			t.Fatalf("target=%v calls=%v output=%s", target, r.calls, out.String())
		}
	}
}

func TestTailscaleRetainedAddAndEdit(t *testing.T) {
	r := tailscaleFixture()
	first := "proxy tailscale add Tail New 100.64.0.2 --auth-key-stdin --control-url https://controlplane.tailscale.com"
	r.fail[first] = 1
	var out bytes.Buffer
	s := NewSession(r, strings.NewReader("Tail New\n100.64.0.2\n\n\ntskey-PASTED\n2\n100.64.0.3\n6\n"), &out)
	if err := addTailscale(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if countTailscaleCall(r.calls, "proxy", "tailscale", "add", "Tail New", "100.64.0.2", "--auth-key-stdin", "--control-url", tailscaleControlURL) != 1 || countTailscaleCall(r.calls, "proxy", "tailscale", "add", "Tail New", "100.64.0.3", "--auth-key-stdin", "--control-url", tailscaleControlURL) != 1 {
		t.Fatalf("calls=%v", r.calls)
	}
	if !reflect.DeepEqual(r.keyInputs, []string{"tskey-PASTED", "tskey-PASTED"}) || strings.Contains(out.String(), "tskey-PASTED") || strings.Contains(out.String(), "secret.invalid") || !strings.Contains(out.String(), "Values are retained") {
		t.Fatal(out.String())
	}
	r.calls = nil
	s = NewSession(r, strings.NewReader("\n\nhttps://headscale.example.invalid\n\n"), &out)
	if err := editTailscale(context.Background(), s, r.nodes[0]); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"proxy", "tailscale", "edit", "Tail Shared", "--exit-node", "100.64.0.1", "--control-url", "https://headscale.example.invalid"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("blank key must preserve existing key: %v", r.calls)
	}
}

func TestTailscaleBindRetryCancelAndReadFailure(t *testing.T) {
	for _, input := range []string{"1\n2\n", "1\n0\n", "0\n"} {
		r := tailscaleFixture()
		r.fail["proxy tailscale bind Tail Shared node SS Node"] = 1
		var out bytes.Buffer
		err := bindTailscale(context.Background(), NewSession(r, strings.NewReader(input), &out), "Tail Shared")
		want := 1
		if input == "1\n2\n" {
			want = 2
			if err != nil {
				t.Fatal(err)
			}
		} else if input == "0\n" {
			want = 0
		}
		if countTailscaleCall(r.calls, "proxy", "tailscale", "bind", "Tail Shared", "node", "SS Node") != want {
			t.Fatalf("calls=%v", r.calls)
		}
	}
	r := tailscaleFixture()
	r.fail["proxy tailscale targets"] = 1
	if err := bindTailscale(context.Background(), NewSession(r, strings.NewReader("1\n"), &bytes.Buffer{}), "Tail Shared"); err == nil || len(r.calls) != 1 {
		t.Fatalf("err=%v calls=%v", err, r.calls)
	}
}

func TestTailscaleUnbindAndDeleteSingleDispatch(t *testing.T) {
	r := tailscaleFixture()
	r.targets[4].DialerProxy = "Tail Shared"
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n4\n1\n4\n0\n"), &out), tailscaleListPage())
	if countTailscaleCall(r.calls, "proxy", "tailscale", "unbind", "provider", "Daily Provider") != 1 || countTailscaleCall(r.calls, "proxy", "tailscale", "remove", "Tail Shared") != 1 || len(r.nodes) != 0 {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}

func TestTailscalePaginationUsesSnapshotAndPreservesTargetArguments(t *testing.T) {
	r := tailscaleFixture()
	r.nodes = nil
	for i := 1; i <= 21; i++ {
		r.nodes = append(r.nodes, menuTailscaleNode{Name: fmt.Sprintf("Tail %02d", i), ExitNode: "auto:any"})
	}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("12\n12\n1\n0\n0\n"), &out), tailscaleListPage())
	if countTailscaleCall(r.calls, "proxy", "tailscale", "show", "Tail 21") != 1 || countTailscaleCall(r.calls, "proxy", "tailscale", "list") != 2 || !strings.Contains(out.String(), "Page 3/3") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
	r = tailscaleFixture()
	r.targets = nil
	for i := 1; i <= 21; i++ {
		r.targets = append(r.targets, menuTailscaleTarget{Kind: "node", Name: fmt.Sprintf("VLESS %02d", i), Type: "vless"})
	}
	out.Reset()
	if err := bindTailscale(context.Background(), NewSession(r, strings.NewReader("11\n11\n1\n"), &out), "Tail Shared"); err != nil {
		t.Fatal(err)
	}
	if countTailscaleCall(r.calls, "proxy", "tailscale", "bind", "Tail Shared", "node", "VLESS 21") != 1 || countTailscaleCall(r.calls, "proxy", "tailscale", "targets") != 1 {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}

func TestTailscaleEmptyAndFailedReadsDoNotExposeStaleChoices(t *testing.T) {
	r := tailscaleFixture()
	s := NewSession(r, strings.NewReader(""), &bytes.Buffer{})
	page := tailscaleListPage()
	view, err := page.Load(context.Background(), s)
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("view=%v err=%v", view, err)
	}
	r.fail["proxy tailscale list"] = 1
	view, err = page.Load(context.Background(), s)
	if err == nil || len(view.Items) != 0 || len(view.Actions) != 1 {
		t.Fatalf("view=%v err=%v", view, err)
	}
	r.fail["proxy tailscale show Tail Shared"] = 1
	view, err = tailscaleDetailPage("Tail Shared").Load(context.Background(), s)
	if err == nil || len(view.Items) != 0 || len(view.Actions) != 0 {
		t.Fatalf("view=%v err=%v", view, err)
	}
	r.nodes = nil
	view, err = page.Load(context.Background(), s)
	if err != nil || len(view.Items) != 0 || len(view.Actions) != 1 {
		t.Fatalf("view=%v err=%v", view, err)
	}
	r.targets = nil
	if err = bindTailscale(context.Background(), s, "Tail Shared"); err != nil {
		t.Fatal(err)
	}
	if countTailscaleCall(r.calls, "proxy", "tailscale", "bind", "Tail Shared", "node", "SS Node") != 0 {
		t.Fatal(r.calls)
	}
}

func TestTailscaleAddRequiredFieldsAndEOF(t *testing.T) {
	r := tailscaleFixture()
	var out bytes.Buffer
	if err := addTailscale(context.Background(), NewSession(r, strings.NewReader("\nTail New\n\n100.64.0.2\n\nhttp://bad.invalid\nhttps://headscale.example.invalid\n\ntskey-PASTED\n"), &out)); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || !strings.Contains(out.String(), "This field is required") || !strings.Contains(out.String(), "Invalid value") {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
	r.calls = nil
	if err := addTailscale(context.Background(), NewSession(r, strings.NewReader("Tail New\n"), &out)); err != ErrQuit || len(r.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, r.calls)
	}
}

func TestTailscaleEditFailureRetainsPastedKeyAndExplicitResubmit(t *testing.T) {
	r := tailscaleFixture()
	r.fail["proxy tailscale edit Tail Shared --exit-node 100.64.0.1 --control-url https://controlplane.tailscale.com --auth-key-stdin"] = 1
	var out bytes.Buffer
	err := editTailscale(context.Background(), NewSession(r, strings.NewReader("\n\n\ntskey-FIRST\n4\ntskey-SECOND\n5\n"), &out), r.nodes[0])
	if err != nil || !reflect.DeepEqual(r.keyInputs, []string{"tskey-FIRST", "tskey-SECOND"}) || strings.Contains(out.String(), "tskey-FIRST") || strings.Contains(out.String(), "tskey-SECOND") || len(r.calls) != 2 || !reflect.DeepEqual(r.calls[1], []string{"proxy", "tailscale", "edit", "Tail Shared", "--exit-node", "100.64.0.1", "--control-url", tailscaleControlURL, "--auth-key-stdin"}) {
		t.Fatalf("err=%v calls=%v output=%s", err, r.calls, out.String())
	}
}

func TestTailscaleFailedDeleteStaysInDetailAndDoesNotRetry(t *testing.T) {
	r := tailscaleFixture()
	r.fail["proxy tailscale remove Tail Shared"] = 1
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n4\n0\n0\n"), &out), tailscaleListPage())
	if countTailscaleCall(r.calls, "proxy", "tailscale", "remove", "Tail Shared") != 1 || countTailscaleCall(r.calls, "proxy", "tailscale", "show", "Tail Shared") != 2 || len(r.nodes) != 1 {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
	text := out.String()
	if strings.Contains(text, "secret.invalid") || strings.LastIndex(text, "operation failed") < strings.LastIndex(text, "Delete Tailscale node") {
		t.Fatalf("failure feedback must follow refreshed detail: %s", text)
	}
}

func TestTailscaleUnbindRetainedSelectionAndCancel(t *testing.T) {
	r := tailscaleFixture()
	r.targets[0].DialerProxy = "Tail Shared"
	r.fail["proxy tailscale unbind node SS Node"] = 1
	var out bytes.Buffer
	err := tailscaleTargetForm(context.Background(), NewSession(r, strings.NewReader("1\n0\n"), &out), "Tail Shared", r.targets[:1], true)
	if err != ErrBack || countTailscaleCall(r.calls, "proxy", "tailscale", "unbind", "node", "SS Node") != 1 || r.targets[0].DialerProxy != "Tail Shared" {
		t.Fatalf("err=%v calls=%v output=%s", err, r.calls, out.String())
	}
}

func TestTailscaleFormInterruptCancelsWithoutDispatch(t *testing.T) {
	r := tailscaleFixture()
	s := NewSession(r, strings.NewReader("Tail New\n"), &bytes.Buffer{})
	s.interrupts = make(chan os.Signal, 1)
	s.interrupts <- os.Interrupt
	if err := addTailscale(context.Background(), s); err != ErrBack || len(r.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, r.calls)
	}
}

func TestTailscaleBindingResourceUnbindsOnceAndReloadsDetail(t *testing.T) {
	r := tailscaleFixture()
	r.targets[4].DialerProxy = "Tail Shared"
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(r, strings.NewReader("1\n1\n0\n0\n"), &out), tailscaleListPage())
	if countTailscaleCall(r.calls, "proxy", "tailscale", "unbind", "provider", "Daily Provider") != 1 || countTailscaleCall(r.calls, "proxy", "tailscale", "show", "Tail Shared") != 2 {
		t.Fatalf("calls=%v output=%s", r.calls, out.String())
	}
}
