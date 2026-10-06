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
	view, err := page.Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range view.Items {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("missing item %q", label)
	return Item{}
}

func TestOperationPageStaticCommands(t *testing.T) {
	tests := []struct {
		page    *Page
		label   string
		args    []string
		confirm bool
	}{
		{DNSPage(), "Show DNS status", []string{"dns", "status"}, false},
		{DNSPage(), "Check DNS configuration", []string{"dns", "check"}, false},
		{DNSPage(), "Disable TUN DNS interception", []string{"dns", "tun", "off"}, true},
		{TrafficPage(), "TUN status", []string{"tun", "status"}, false},
		{TrafficPage(), "Listener ports", []string{"ports", "status"}, false},
		{TrafficPage(), "Disable LAN access", []string{"lan", "disable"}, true},
		{ConnectionsPage(), "Close all connections", []string{"connections", "close-all"}, true},
		{RulesPage(), "List active rule providers", []string{"rules", "providers"}, false},
	}
	if runtime.GOOS == "darwin" {
		item := operationItem(t, TrafficPage(), "System proxy status")
		if !reflect.DeepEqual(item.Args, []string{"system-proxy", "status"}) {
			t.Errorf("system proxy status args=%v", item.Args)
		}
		item = operationItem(t, TrafficPage(), "Disable system proxy")
		if !reflect.DeepEqual(item.Args, []string{"system-proxy", "disable"}) || item.Confirm == "" {
			t.Errorf("disable system proxy: args=%v confirm=%q", item.Args, item.Confirm)
		}
	}
	for _, tt := range tests {
		item := operationItem(t, tt.page, tt.label)
		if !reflect.DeepEqual(item.Args, tt.args) || (item.Confirm != "") != tt.confirm {
			t.Errorf("%s: args=%v confirm=%q", tt.label, item.Args, item.Confirm)
		}
	}
}

func TestLinuxTrafficMenuOmitsSystemProxy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux menu only")
	}
	view, err := TrafficPage().Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range view.Items {
		if len(item.Args) > 0 && item.Args[0] == "system-proxy" {
			t.Fatalf("unsupported Linux system proxy action: %q", item.Label)
		}
	}
}

func TestOperationPageScope(t *testing.T) {
	dns, err := DNSPage().Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dns.Context, "not an OS DNS leak test") {
		t.Fatal(dns.Context)
	}
	traffic, err := TrafficPage().Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(traffic.Context, "neither proves") {
		t.Fatal(traffic.Context)
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

func TestRuleRemovalUsesFreshNameAndConfirmation(t *testing.T) {
	runner := &operationRunner{data: map[string]any{"rules custom": map[string]any{"entries": []map[string]any{{"name": "work rule"}}}}}
	session := NewSession(runner, strings.NewReader("1\nyes\n"), &bytes.Buffer{})
	item := operationItem(t, RulesPage(), "Remove custom rule or set")
	if err := item.Action(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"rules", "custom"}, {"rules", "remove", "work rule"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("got %v, want %v", runner.calls, want)
	}
}

func TestRuleRemovalCanceled(t *testing.T) {
	runner := &operationRunner{data: map[string]any{"rules custom": map[string]any{"entries": []map[string]any{{"name": "work rule"}}}}}
	session := NewSession(runner, strings.NewReader("1\nno\n"), &bytes.Buffer{})
	item := operationItem(t, RulesPage(), "Remove custom rule or set")
	if err := item.Action(context.Background(), session); err != nil {
		t.Fatal(err)
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
