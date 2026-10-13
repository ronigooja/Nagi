package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLoginStartupPageSummary(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		code int
		err  *CLIError
		want string
	}{
		{name: "running", data: `{"service":{"installed":true,"enabled":true,"active":true},"engine":{"running":true}}`, want: "Login startup: enabled | Service: running | Mihomo: running"},
		{name: "engine stopped with monitor active", data: `{"service":{"enabled":true,"active":true},"engine":{"running":false}}`, want: "Login startup: enabled | Service: running | Mihomo: stopped"},
		{name: "disabled", data: `{"service":{"enabled":false,"active":false},"engine":{"running":false}}`, want: "Login startup: disabled | Service: stopped | Mihomo: stopped"},
		{name: "missing enabled", data: `{"service":{"active":true},"engine":{"running":true}}`, want: "Login startup: unknown | Service: running | Mihomo: running"},
		{name: "missing engine", data: `{"service":{"enabled":true,"active":false}}`, want: "Login startup: enabled | Service: stopped | Mihomo: unknown"},
		{name: "null and wrong types", data: `{"service":{"enabled":null,"active":"yes"},"engine":{"running":true}}`, want: "Login startup: unknown | Service: unknown | Mihomo: running"},
		{name: "malformed service", data: `{"service":true,"engine":{"running":false}}`, want: "Login startup: unknown | Service: unknown | Mihomo: stopped"},
		{name: "empty", data: `{}`, want: "Login startup: unknown | Service: unknown | Mihomo: unknown"},
		{name: "manager unavailable", code: 1, err: &CLIError{Message: "systemd user session unavailable"}, want: "Login startup: unavailable | Service: unavailable | Mihomo: unavailable — systemd user session unavailable"},
		{name: "read failed", code: 1, want: "Login startup: unavailable | Service: unavailable | Mihomo: unavailable — read failed; open Startup details"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]Result{"startup status": {Data: json.RawMessage(tt.data), Code: tt.code, Error: tt.err}}}
			view, err := ServicePage().Load(context.Background(), NewSession(runner, strings.NewReader(""), &bytes.Buffer{}))
			if err != nil || view.Context != tt.want || len(view.Actions) != 6 {
				t.Fatalf("view=%+v err=%v, want %q", view, err, tt.want)
			}
			if !reflect.DeepEqual(runner.calls, [][]string{{"startup", "status"}}) {
				t.Fatalf("unexpected reads: %v", runner.calls)
			}
		})
	}
}

func TestLoginStartupDisableRefreshesSummary(t *testing.T) {
	runner := &sequenceRunner{results: map[string][]Result{
		"startup status": {
			{Data: json.RawMessage(`{"service":{"enabled":true,"active":true},"engine":{"running":true}}`)},
			{Data: json.RawMessage(`{"service":{"enabled":false,"active":false},"engine":{"running":false}}`)},
		},
	}}
	var out bytes.Buffer
	code := RunPage(context.Background(), NewSession(runner, strings.NewReader("5\n0\n"), &out), ServicePage())
	wantCalls := [][]string{{"startup", "status"}, {"startup", "disable"}, {"startup", "status"}}
	if code != 0 || !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("code=%d calls=%v", code, runner.calls)
	}
	before := strings.Index(out.String(), "Login startup: enabled | Service: running | Mihomo: running")
	after := strings.Index(out.String(), "Login startup: disabled | Service: stopped | Mihomo: stopped")
	if before < 0 || after <= before {
		t.Fatalf("summary did not refresh: %s", out.String())
	}
}

func diagnosticItem(t *testing.T, page *Page, label string) Item {
	t.Helper()
	view, err := page.Load(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range append(view.Items, view.Actions...) {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("missing item %q", label)
	return Item{}
}

func TestServiceAndDiagnosticCommandMapping(t *testing.T) {
	tests := []struct {
		page   *Page
		label  string
		args   []string
		impact bool
	}{
		{ServicePage(), "Service details", []string{"service", "status"}, false},
		{ServicePage(), "Install login service", []string{"service", "install"}, true},
		{ServicePage(), "Uninstall login service", []string{"service", "uninstall"}, true},
		{ServicePage(), "Startup details", []string{"startup", "status"}, false},
		{ServicePage(), "Enable login startup", []string{"startup", "enable"}, false},
		{ServicePage(), "Disable login startup", []string{"startup", "disable"}, true},
		{DiagnosticsPage(), "Repair startup state", []string{"startup", "check"}, false},
		{DiagnosticsPage(), "Clean stale runtime records", []string{"recover"}, true},
		{diagnosticsPageForOS("linux"), "Basic checks", []string{"doctor"}, false},
		{diagnosticsPageForOS("linux"), "Detailed diagnostics", []string{"diagnostics"}, false},
		{diagnosticsPageForOS("linux"), "Show version", []string{"version"}, false},
		{helperPageForOS("darwin"), "Privileged helper details", []string{"privileged-helper", "status"}, false},
		{helperPageForOS("darwin"), "Install privileged helper", []string{"privileged-helper", "install"}, true},
		{helperPageForOS("darwin"), "Uninstall privileged helper", []string{"privileged-helper", "uninstall"}, true},
		{helperPageForOS("darwin"), "Select privileged backend", []string{"privileged-helper", "enable"}, true},
		{helperPageForOS("darwin"), "Select user backend", []string{"privileged-helper", "disable"}, true},
		{maintenancePageForOS("linux"), "Update Nagi", []string{"update"}, true},
		{maintenancePageForOS("linux"), "Uninstall Nagi", []string{"uninstall"}, true},
		{maintenancePageForOS("linux"), "Uninstall Nagi and clear user data", []string{"uninstall", "--purge-data"}, true},
		{maintenancePageForOS("darwin"), "Install menu bar App", []string{"app", "install"}, true},
		{maintenancePageForOS("darwin"), "Update Nagi", []string{"update"}, true},
		{maintenancePageForOS("darwin"), "Uninstall menu bar App", []string{"app", "uninstall"}, true},
		{maintenancePageForOS("darwin"), "Uninstall Nagi", []string{"uninstall"}, true},
		{maintenancePageForOS("darwin"), "Uninstall Nagi and clear user data", []string{"uninstall", "--purge-data"}, true},
	}
	for _, tt := range tests {
		item := diagnosticItem(t, tt.page, tt.label)
		if !reflect.DeepEqual(item.Args, tt.args) || (item.Impact != "") != tt.impact {
			t.Errorf("%s: args=%v impact=%q", tt.label, item.Args, item.Impact)
		}
	}
}

func TestPrivilegedHelperGatedToMacOS(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		view, err := diagnosticsPageForOS(goos).Load(context.Background(), NewSession(&fakeRunner{}, strings.NewReader(""), &bytes.Buffer{}))
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range append(view.Items, view.Actions...) {
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
		{"\n/tmp/valid.json\n", [][]string{{"diagnostics", "export", "/tmp/valid.json"}}},
		{"report.json\n/tmp/valid.json\n", [][]string{{"diagnostics", "export", "/tmp/valid.json"}}},
		{"/tmp/report with spaces.json\n", [][]string{{"diagnostics", "export", "/tmp/report with spaces.json"}}},
	} {
		runner := &fakeRunner{}
		var out bytes.Buffer
		item := diagnosticItem(t, DiagnosticsPage(), "Export diagnostics")
		if err := item.Action(context.Background(), NewSession(runner, strings.NewReader(tt.input), &out)); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(runner.calls, tt.want) {
			t.Errorf("input %q: calls=%v", tt.input, runner.calls)
		}
	}
}

func TestServiceUninstallRunsOnceAndKeepsRecoveryPage(t *testing.T) {
	fake := &fakeRunner{results: map[string]Result{"service uninstall": {Code: 1, Error: &CLIError{Code: "service_error", Message: "manager unavailable"}}}}
	var out bytes.Buffer
	RunPage(context.Background(), NewSession(fake, strings.NewReader("6\n0\n"), &out), ServicePage())
	count := 0
	for _, args := range fake.calls {
		if reflect.DeepEqual(args, []string{"service", "uninstall"}) {
			count++
		}
	}
	if count != 1 || !strings.Contains(out.String(), "manager unavailable") || strings.Count(out.String(), "\nStart at Login\n") != 2 {
		t.Fatalf("count=%d calls=%v output=%s", count, fake.calls, out.String())
	}
}

func TestAdvancedRetainsAllFeatureEntrypoints(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		view, err := advancedPageForOS(goos).Load(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Current Configuration", "DNS", "Routing Rules", "Connections", "Start at Login", "Nagi Maintenance"}
		if goos == "darwin" {
			want = append(want, "Privileged helper")
		}
		if len(view.Items) != 0 || len(view.Actions) != len(want) {
			t.Fatalf("%s: %+v", goos, view)
		}
		for i, label := range want {
			if view.Actions[i].Label != label || view.Actions[i].Next == nil {
				t.Fatalf("%s: missing %s", goos, label)
			}
		}
	}
}

func TestMaintenancePlatformActionsAndOrder(t *testing.T) {
	for _, tt := range []struct {
		goos string
		want []string
	}{
		{goos: "linux", want: []string{"Update Nagi", "Uninstall Nagi", "Uninstall Nagi and clear user data"}},
		{goos: "darwin", want: []string{"Install menu bar App", "Uninstall menu bar App", "Update Nagi", "Uninstall Nagi", "Uninstall Nagi and clear user data"}},
	} {
		page := maintenancePageForOS(tt.goos)
		view, err := page.Load(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Actions) != len(tt.want) {
			t.Fatalf("%s: actions=%v", tt.goos, view.Actions)
		}
		for i, label := range tt.want {
			if view.Actions[i].Label != label {
				t.Errorf("%s action %d = %q, want %q", tt.goos, i, view.Actions[i].Label, label)
			}
		}
	}
}
