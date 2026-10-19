package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
)

func init() {
	RegisterPage("service", ServicePage)
	RegisterPage("diagnostics", DiagnosticsPage)
	RegisterPage("advanced", AdvancedPage)
}

// ServicePage presents login activation separately from current engine controls.
func ServicePage() *Page {
	return &Page{Title: "Start at Login", Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, "startup", "status")
		actions := []Item{}
		if enabled, known := loginStartupEnabled(result); known {
			toggle := Item{Label: "Enable start at login", Args: []string{"startup", "enable"}}
			if enabled {
				toggle = Item{Label: "Disable start at login", Args: []string{"startup", "disable"}, Impact: "Keeps the current proxy running"}
			}
			actions = append(actions, toggle)
		}
		actions = append(actions,
			Item{Label: "View details", Args: []string{"startup", "status"}},
			Item{Label: "Advanced options", Next: loginStartupAdvancedPage},
		)
		return PageView{Context: loginStartupSummary(result), Actions: actions}, nil
	}}
}

func loginStartupAdvancedPage() *Page {
	return actionPage("Login Startup — Advanced",
		Item{Label: "Remove login startup service", Args: []string{"service", "uninstall"}, Impact: "Can stop mihomo; keeps Nagi and configuration"},
	)
}

func loginStartupEnabled(result Result) (bool, bool) {
	if result.Error != nil || result.Code != 0 {
		return false, false
	}
	var service map[string]json.RawMessage
	_ = json.Unmarshal(menuObject(result.Data)["service"], &service)
	var enabled *bool
	if json.Unmarshal(service["enabled"], &enabled) != nil || enabled == nil {
		return false, false
	}
	return *enabled, true
}

func loginStartupSummary(result Result) string {
	if result.Error != nil || result.Code != 0 {
		reason := "read failed; open View details"
		if result.Error != nil && result.Error.Message != "" {
			reason = result.Error.Message
		}
		return "Start automatically at login: unavailable | Mihomo: unavailable — " + reason
	}
	object := menuObject(result.Data)
	var service, engine map[string]json.RawMessage
	_ = json.Unmarshal(object["service"], &service)
	_ = json.Unmarshal(object["engine"], &engine)
	state := func(raw json.RawMessage, yes, no string) string {
		var flag *bool
		if json.Unmarshal(raw, &flag) != nil || flag == nil {
			return "unknown"
		}
		if *flag {
			return yes
		}
		return no
	}
	return "Start automatically at login: " + state(service["enabled"], "On", "Off") +
		" | Mihomo: " + state(engine["running"], "running", "stopped")
}

func DiagnosticsPage() *Page { return diagnosticsPageForOS(runtime.GOOS) }

func diagnosticsPageForOS(goos string) *Page {
	return &Page{Title: "Troubleshooting", Load: func(_ context.Context, _ *Session) (PageView, error) {
		items := []Item{
			{Label: "Basic checks", Args: []string{"doctor"}},
			{Label: "Detailed diagnostics", Args: []string{"diagnostics"}},
			{Label: "Export diagnostics", Action: exportDiagnostics},
			{Label: "Logs and traffic", Next: LogsPage},
			{Label: "Repair startup state", Args: []string{"startup", "check"}},
			{Label: "Clean stale runtime records", Args: []string{"recover"}, Impact: "Clears records left by an unexpected exit"},
			{Label: "Show version", Args: []string{"version"}},
		}

		return PageView{Actions: items}, nil
	}}
}

func exportDiagnostics(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "path", Label: "Destination file (absolute path)", Required: true, Validate: func(v string) error {
		if !filepath.IsAbs(v) {
			return fmt.Errorf("enter an absolute file path (for example, /tmp/nagi-diagnostics.json)")
		}
		return nil
	}}}, nil, func(v map[string]string) Result { return s.Run(ctx, "diagnostics", "export", v["path"]) })
	return err
}

func AdvancedPage() *Page { return advancedPageForOS(runtime.GOOS) }

func advancedPageForOS(goos string) *Page {
	return &Page{Title: "Settings & Maintenance", Load: func(_ context.Context, _ *Session) (PageView, error) {
		items := []Item{
			{Label: "Current Configuration", Next: configurationPage},
			{Label: "DNS", Next: DNSPage},
			{Label: "Routing Rules", Next: RulesPage},
			{Label: "Request Rewrite", Next: RequestRewritePage},
			{Label: "Connections", Next: ConnectionsPage},
			{Label: "Start at Login", Next: ServicePage},
			{Label: "Nagi Maintenance", Next: MaintenancePage},
		}
		if goos == "darwin" {
			items = append(items, Item{Label: "Privileged helper", Next: func() *Page { return helperPageForOS(goos) }})
		}
		return PageView{Actions: items}, nil
	}}
}

func MaintenancePage() *Page { return maintenancePageForOS(runtime.GOOS) }

func maintenancePageForOS(goos string) *Page {
	items := []Item{}
	if goos == "darwin" {
		items = append(items,
			Item{Label: "Install menu bar App", Args: []string{"app", "install"}, Impact: "Installs the App for this Nagi version"},
			Item{Label: "Uninstall menu bar App", Args: []string{"app", "uninstall"}, Impact: "Quit the App and disable its login item first"},
		)
	}
	items = append(items, Item{Label: "Update Nagi", Args: []string{"update"}, Impact: "Updates Nagi, mihomo and any installed App; resets the default profile"})
	uninstallImpact := "Removes Nagi and services; keeps user data"
	if goos == "darwin" {
		uninstallImpact = "Removes Nagi, App, helper and services; keeps user data"
	}
	items = append(items, Item{Label: "Uninstall Nagi", Args: []string{"uninstall"}, Impact: uninstallImpact})
	items = append(items, Item{Label: "Uninstall Nagi and clear user data", Args: []string{"uninstall", "--purge-data"}, Impact: "Permanently deletes all Nagi user data, including profiles and subscriptions"})
	return actionPage("Nagi Maintenance", items...)
}

func helperPageForOS(goos string) *Page {
	return &Page{Title: "Privileged helper", Load: func(ctx context.Context, s *Session) (PageView, error) {
		if goos != "darwin" {
			return PageView{Context: "Privileged helper is available on macOS only."}, nil
		}
		return PageView{Context: pageStatus(ctx, s, "Privileged helper", []string{"privileged-helper", "status"}, "installed", "enabled", "backend"), Actions: []Item{
			{Label: "Privileged helper details", Args: []string{"privileged-helper", "status"}},
			{Label: "Install privileged helper", Args: []string{"privileged-helper", "install"}, Impact: "Requires administrator privileges"},
			{Label: "Select privileged backend", Args: []string{"privileged-helper", "enable"}, Impact: "Applies on next start; stop user mihomo first"},
			{Label: "Select user backend", Args: []string{"privileged-helper", "disable"}, Impact: "Applies on next start; stop privileged mihomo first"},
			{Label: "Uninstall privileged helper", Args: []string{"privileged-helper", "uninstall"}, Impact: "Stop privileged mihomo and select the user backend first"},
		}}, nil
	}}
}
