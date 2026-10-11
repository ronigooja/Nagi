package menu

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type operationRunner struct {
	calls [][]string
	data  map[string]any
}

type operationStreamRunner struct {
	operationRunner
	streamCalls [][]string
}

func (r *operationStreamRunner) Stream(_ context.Context, args []string, out io.Writer) Result {
	r.streamCalls = append(r.streamCalls, append([]string(nil), args...))
	_, _ = io.WriteString(out, "sample line\n")
	return Result{}
}

func (r *operationRunner) Run(_ context.Context, args []string) Result {
	r.calls = append(r.calls, append([]string(nil), args...))
	return Result{Data: r.data[strings.Join(args, " ")]}
}

func operationItem(t *testing.T, page *Page, label string) Item {
	t.Helper()
	item, ok := findMenuItem(page, label)
	if !ok {
		t.Fatalf("missing item %q", label)
	}
	return item
}

func findMenuItem(page *Page, label string) (Item, bool) {
	view, _ := page.Load(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}))
	for _, item := range append(view.Items, view.Actions...) {
		if item.Label == label {
			return item, true
		}
		if item.Next != nil {
			if found, ok := findMenuItem(item.Next(), label); ok {
				return found, true
			}
		}
	}
	return Item{}, false
}

func TestOperationPageStaticCommands(t *testing.T) {
	tests := []struct {
		page   *Page
		label  string
		args   []string
		impact bool
	}{
		{DNSPage(), "DNS details", []string{"dns", "status"}, false},
		{DNSPage(), "Check DNS configuration", []string{"dns", "check"}, false},
		{DNSPage(), "Disable TUN DNS interception", []string{"dns", "tun", "off"}, true},
		{TrafficPage(), "TUN details", []string{"tun", "status"}, false},
		{TrafficPage(), "Listener ports", []string{"ports", "status"}, false},
		{TrafficPage(), "Disable LAN access", []string{"lan", "disable"}, true},
		{ConnectionsPage(), "Close all connections", []string{"connections", "close-all"}, true},
		{RulesPage(), "List active rule providers", []string{"rules", "providers"}, false},
	}
	if runtime.GOOS == "darwin" {
		item := operationItem(t, TrafficPage(), "System proxy details")
		if !reflect.DeepEqual(item.Args, []string{"system-proxy", "status"}) {
			t.Errorf("system proxy status args=%v", item.Args)
		}
		item = operationItem(t, TrafficPage(), "Disable system proxy")
		if !reflect.DeepEqual(item.Args, []string{"system-proxy", "disable"}) || item.Impact == "" {
			t.Errorf("disable system proxy: args=%v impact=%q", item.Args, item.Impact)
		}
	}
	for _, tt := range tests {
		item := operationItem(t, tt.page, tt.label)
		if !reflect.DeepEqual(item.Args, tt.args) || (item.Impact != "") != tt.impact {
			t.Errorf("%s: args=%v impact=%q", tt.label, item.Args, item.Impact)
		}
	}
}

func TestLinuxTrafficMenuOmitsSystemProxy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux menu only")
	}
	view, err := TrafficPage().Load(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range append(view.Items, view.Actions...) {
		if len(item.Args) > 0 && item.Args[0] == "system-proxy" {
			t.Fatalf("unsupported Linux system proxy action: %q", item.Label)
		}
	}
}

func TestDNSPageScope(t *testing.T) {
	dns, err := DNSPage().Load(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dns.Context, "not an OS DNS leak test") {
		t.Fatal(dns.Context)
	}
}

func TestMenuNamesFromCLIData(t *testing.T) {
	entries := struct {
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}{}
	entries.Entries = append(entries.Entries, struct {
		Name string `json:"name"`
	}{Name: "Work Rule"})
	if got := menuNames(entries, "entries"); !reflect.DeepEqual(got, []string{"Work Rule"}) {
		t.Fatal(got)
	}
	connections := map[string]any{"connections": []map[string]any{{"id": "first id"}}}
	if got := menuNames(connections, "connections"); !reflect.DeepEqual(got, []string{"first id"}) {
		t.Fatal(got)
	}
}

func TestMenuSelectNameDisplaysCLIError(t *testing.T) {
	runner := &featureRunner{results: map[string]Result{"rules\x00custom": {Error: &CLIError{Code: "rule_error", Message: "rules file unreadable"}}}}
	var out bytes.Buffer
	name, err := menuSelectName(context.Background(), NewSession(runner, strings.NewReader(""), &out), []string{"rules", "custom"}, "entries", "Rule")
	if err != nil || name != "" {
		t.Fatalf("name=%q, err=%v", name, err)
	}
	if !strings.Contains(out.String(), "rules file unreadable") || strings.Contains(out.String(), "No custom rules") {
		t.Fatalf("wrong error presentation: %q", out.String())
	}
}

func TestOperationPromptCommands(t *testing.T) {
	tests := []struct {
		page         *Page
		label, input string
		calls        [][]string
	}{
		{DNSPage(), "Set direct DoH upstreams", "https://one.example/dns-query https://two.example/dns-query\n", [][]string{{"dns", "set", "direct", "https://one.example/dns-query", "https://two.example/dns-query"}}},
		{DNSPage(), "Add DNS exception", "corp.example\nudp://10.0.0.53:53\n", [][]string{{"dns", "exception", "add", "corp.example", "udp://10.0.0.53:53"}}},
		{TrafficPage(), "Enable LAN access", "192.0.2.10\n", [][]string{{"lan", "enable", "192.0.2.10"}}},
		{LogsPage(), "Show recent logs", "25\n", [][]string{{"logs", "25"}}},
	}
	for _, tt := range tests {
		runner := &operationRunner{}
		session := NewSession(runner, strings.NewReader(tt.input), &bytes.Buffer{})
		item := operationItem(t, tt.page, tt.label)
		if err := item.Action(context.Background(), session); err != nil {
			t.Fatalf("%s: %v", tt.label, err)
		}
		if !reflect.DeepEqual(runner.calls, tt.calls) {
			t.Errorf("%s: got %v, want %v", tt.label, runner.calls, tt.calls)
		}
	}
}

func TestRuleRemovalUsesFreshNameOnce(t *testing.T) {
	runner := &operationRunner{data: map[string]any{"rules custom": map[string]any{"entries": []map[string]any{{"name": "work rule"}}}}}
	session := NewSession(runner, strings.NewReader("1\n"), &bytes.Buffer{})
	item := operationItem(t, RulesPage(), "Remove custom rule or set")
	if err := item.Action(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"rules", "custom"}, {"rules", "remove", "work rule"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("got %v, want %v", runner.calls, want)
	}
}

func TestRuleSelectionCanceled(t *testing.T) {
	runner := &operationRunner{data: map[string]any{"rules custom": map[string]any{"entries": []map[string]any{{"name": "work rule"}}}}}
	session := NewSession(runner, strings.NewReader("0\n"), &bytes.Buffer{})
	item := operationItem(t, RulesPage(), "Remove custom rule or set")
	if err := item.Action(context.Background(), session); err != ErrBack {
		t.Fatalf("cancel = %v", err)
	}
	want := [][]string{{"rules", "custom"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("got %v, want %v", runner.calls, want)
	}
}

func TestConnectionSelectionPassesIDAsOneArgument(t *testing.T) {
	runner := &operationRunner{data: map[string]any{"connections list": map[string]any{"connections": []map[string]any{{"id": "id with spaces"}}}}}
	session := NewSession(runner, strings.NewReader("1\n"), &bytes.Buffer{})
	item := operationItem(t, ConnectionsPage(), "Show a connection")
	if err := item.Action(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"connections", "list"}, {"connections", "show", "id with spaces"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("got %v, want %v", runner.calls, want)
	}
}

func TestOperationStreamsUseStreamingRunner(t *testing.T) {
	for _, tt := range []struct {
		label string
		args  []string
	}{
		{"Follow logs (Ctrl-C returns here)", []string{"logs", "follow"}},
		{"Watch traffic (Ctrl-C returns here)", []string{"traffic", "watch"}},
	} {
		runner := &operationStreamRunner{}
		var out bytes.Buffer
		session := NewSession(runner, strings.NewReader(""), &out)
		item := operationItem(t, LogsPage(), tt.label)
		if err := item.Action(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(runner.streamCalls, [][]string{tt.args}) {
			t.Fatalf("%s: %v", tt.label, runner.streamCalls)
		}
		if !strings.Contains(out.String(), "sample line") {
			t.Fatalf("stream output missing: %q", out.String())
		}
	}
}

func TestFixedOperationExecutesOnceWithoutConfirmation(t *testing.T) {
	runner := &fakeRunner{}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader("4\n0\n"), &out), ConnectionsPage())
	count := 0
	for _, args := range runner.calls {
		if reflect.DeepEqual(args, []string{"connections", "close-all"}) {
			count++
		}
	}
	if count != 1 || strings.Contains(out.String(), "Type yes") {
		t.Fatalf("close-all count=%d output=%s", count, out.String())
	}
}

func TestDNSFormRetainsLiteralNamesAfterFailedSubmission(t *testing.T) {
	runner := &sequenceRunner{results: map[string][]Result{
		"dns exception add b q": {{Code: 1, Error: &CLIError{Message: "server is invalid"}}},
	}}
	var out bytes.Buffer
	err := dnsExceptionAdd(context.Background(), NewSession(runner, strings.NewReader("\nb\nq\n2\nudp://192.0.2.53:53\n3\n"), &out))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"dns", "exception", "add", "b", "q"}, {"dns", "exception", "add", "b", "udp://192.0.2.53:53"}}
	if !reflect.DeepEqual(runner.calls, want) || !strings.Contains(out.String(), "This field is required") {
		t.Fatalf("calls=%v output=%s", runner.calls, out.String())
	}
}

func TestOperationFormsValidateInPlace(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action func(context.Context, *Session) error
		input  string
		want   []string
	}{
		{"LAN", enableLAN, "bad-address\n192.0.2.10\n", []string{"lan", "enable", "192.0.2.10"}},
		{"logs", recentLogs, "0\nnot-a-number\n25\n", []string{"logs", "25"}},
		{"DNS", dnsSet("direct"), "http://dns.example/query\nhttps://dns.example/query\n", []string{"dns", "set", "direct", "https://dns.example/query"}},
		{"rules", ruleAdd, "invalid name\nwork\n10\n\nDIRECT\n", []string{"rules", "add", "work", "MATCH", "-", "DIRECT"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			var out bytes.Buffer
			if err := tt.action(context.Background(), NewSession(runner, strings.NewReader(tt.input), &out)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runner.calls, [][]string{tt.want}) || !strings.Contains(out.String(), "Invalid value") {
				t.Fatalf("calls=%v output=%s", runner.calls, out.String())
			}
		})
	}
}

func TestTrafficUnknownStatusPreservesActionsAndRedacts(t *testing.T) {
	runner := &fakeRunner{results: map[string]Result{
		"tun status": {Error: &CLIError{Message: "failed https://secret.example/token"}},
	}}
	selection := "1\n3"
	if runtime.GOOS == "darwin" {
		selection = "2\n3" // System proxy settings precede TUN.
	}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(runner, strings.NewReader(selection+"\nq\n"), &out), TrafficPage())
	if !strings.Contains(out.String(), "TUN: unknown") || strings.Contains(out.String(), "secret.example") {
		t.Fatal(out.String())
	}
	for _, args := range runner.calls {
		if reflect.DeepEqual(args, []string{"tun", "disable"}) {
			return
		}
	}
	t.Fatalf("recovery action was not dispatched: %v", runner.calls)
}
