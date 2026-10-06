package menu

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func diagnosticItem(t *testing.T, page *Page, label string) Item {
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

func TestServiceAndDiagnosticCommandMapping(t *testing.T) {
	tests := []struct {
		page    *Page
		label   string
		args    []string
		confirm bool
	}{
		{ServicePage(), "Service status", []string{"service", "status"}, false},
		{ServicePage(), "Install login service", []string{"service", "install"}, true},
		{ServicePage(), "Uninstall login service", []string{"service", "uninstall"}, true},
		{ServicePage(), "Startup status", []string{"startup", "status"}, false},
		{ServicePage(), "Enable login startup", []string{"startup", "enable"}, true},
		{ServicePage(), "Disable login startup", []string{"startup", "disable"}, true},
		{ServicePage(), "Check startup and runtime state", []string{"startup", "check"}, false},
		{ServicePage(), "Recover stale runtime markers", []string{"recover"}, true},
		{diagnosticsPageForOS("linux"), "Run doctor checks", []string{"doctor"}, false},
		{diagnosticsPageForOS("linux"), "Run redacted diagnostics", []string{"diagnostics"}, false},
		{diagnosticsPageForOS("linux"), "Show version", []string{"version"}, false},
		{diagnosticsPageForOS("darwin"), "Privileged helper status", []string{"privileged-helper", "status"}, false},
		{diagnosticsPageForOS("darwin"), "Install privileged helper", []string{"privileged-helper", "install"}, true},
		{diagnosticsPageForOS("darwin"), "Uninstall privileged helper", []string{"privileged-helper", "uninstall"}, true},
		{diagnosticsPageForOS("darwin"), "Select privileged backend", []string{"privileged-helper", "enable"}, true},
		{diagnosticsPageForOS("darwin"), "Select user backend", []string{"privileged-helper", "disable"}, true},
		{CompletionPage(), "Generate Bash completion script", []string{"completion", "bash"}, true},
		{CompletionPage(), "Generate Zsh completion script", []string{"completion", "zsh"}, true},
		{CompletionPage(), "Generate Fish completion script", []string{"completion", "fish"}, true},
	}
	for _, tt := range tests {
		item := diagnosticItem(t, tt.page, tt.label)
		if !reflect.DeepEqual(item.Args, tt.args) || (item.Confirm != "") != tt.confirm {
			t.Errorf("%s: args=%v confirm=%q", tt.label, item.Args, item.Confirm)
		}
	}
}

func TestPrivilegedHelperGatedToMacOS(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		view, err := diagnosticsPageForOS(goos).Load(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range view.Items {
			if strings.Contains(item.Label, "Privileged helper") || strings.Contains(item.Label, "backend") {
				t.Errorf("%s: unsupported helper action %q", goos, item.Label)
			}
		}
	}
}

func TestDiagnosticExportRequiresAbsoluteFile(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  [][]string
	}{
		{"\n", nil},
		{"report.json\n", nil},
		{"/tmp/report with spaces.json\n", [][]string{{"diagnostics", "export", "/tmp/report with spaces.json"}}},
	} {
		runner := &fakeRunner{}
		var out bytes.Buffer
		item := diagnosticItem(t, DiagnosticsPage(), "Export redacted diagnostics")
		if err := item.Action(context.Background(), NewSession(runner, strings.NewReader(tt.input), &out)); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(runner.calls, tt.want) {
			t.Errorf("input %q: calls=%v", tt.input, runner.calls)
		}
	}
}

func TestServiceConfirmationAndErrorRecovery(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  bool
	}{
		{"3\nno\nb\nq\n", false},
		{"3\nyes\nb\nq\n", true},
	} {
		fake := &fakeRunner{results: map[string]Result{"service uninstall": {Code: 1, Error: &CLIError{Code: "service_error", Message: "manager unavailable"}}}}
		var out bytes.Buffer
		// Root has service at the ninth position; it is on its first page.
		Run(context.Background(), NewSession(fake, strings.NewReader("9\n"+tt.input), &out))
		found := false
		for _, args := range fake.calls {
			found = found || reflect.DeepEqual(args, []string{"service", "uninstall"})
		}
		if found != tt.want {
			t.Errorf("input %q: calls=%v", tt.input, fake.calls)
		}
		if tt.want && !strings.Contains(out.String(), "manager unavailable") {
			t.Errorf("command error not shown: %s", out.String())
		}
	}
}

func TestCompletionCandidatesArguments(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  [][]string
	}{
		{"1\n", [][]string{{"completion", "candidates", "profile"}}},
		{"4\nGroup With Spaces\n", [][]string{{"completion", "candidates", "nodes", "Group With Spaces"}}},
		{"4\n\n", [][]string{{"completion", "candidates", "nodes"}}},
	} {
		fake := &fakeRunner{}
		item := diagnosticItem(t, CompletionPage(), "Show completion candidates")
		if err := item.Action(context.Background(), NewSession(fake, strings.NewReader(tt.input), &bytes.Buffer{})); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fake.calls, tt.want) {
			t.Errorf("input %q: calls=%v", tt.input, fake.calls)
		}
	}
}
