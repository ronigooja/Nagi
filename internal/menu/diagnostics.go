package menu

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
)

func init() {
	RegisterPage("service", ServicePage)
	RegisterPage("diagnostics", DiagnosticsPage)
	RegisterPage("advanced", AdvancedPage)
}

// ServicePage exposes the existing per-user service and startup commands.
func ServicePage() *Page {
	return &Page{Title: "Start at Login", Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: pageStatus(ctx, s, "Service", []string{"service", "status"}, "installed", "enabled", "active") + "\n" + pageStatus(ctx, s, "Startup", []string{"startup", "status"}, "enabled", "status"), Actions: []Item{
			{Label: "Service details", Args: []string{"service", "status"}},
			{Label: "Install login service", Args: []string{"service", "install"}, Impact: "Also enables login startup"},
			{Label: "Startup details", Args: []string{"startup", "status"}},
			{Label: "Enable login startup", Args: []string{"startup", "enable"}},
			{Label: "Disable login startup", Args: []string{"startup", "disable"}, Impact: "Can stop mihomo"},
			{Label: "Uninstall login service", Args: []string{"service", "uninstall"}, Impact: "Can stop mihomo"},
		}}, nil
	}}
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
			{Label: "Clean stale runtime records", Args: []string{"recover"}, Impact: "Removes stale PID and socket markers after an unexpected exit"},
			{Label: "Show version", Args: []string{"version"}},
		}

		return PageView{Actions: items}, nil
	}}
}

func exportDiagnostics(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "path", Label: "Absolute destination file for owner-only JSON report", Required: true, Validate: func(v string) error {
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
			{Label: "Connections", Next: ConnectionsPage},
			{Label: "Start at Login", Next: ServicePage},
		}
		if goos == "darwin" {
			items = append(items, Item{Label: "Privileged helper", Next: func() *Page { return helperPageForOS(goos) }})
			items = append(items, Item{Label: "Nagi App & Updates", Next: AppPage})
		}
		return PageView{Actions: items}, nil
	}}
}

func AppPage() *Page {
	return statePage("Nagi App & Updates", []string{"app", "status"}, []string{"installed", "app"},
		Item{Label: "Install menu bar App", Args: []string{"app", "install"}, Impact: "Installs only the App matching this CLI release; may require administrator authorization"},
		Item{Label: "Update Nagi", Args: []string{"update"}, Impact: "Updates CLI, mihomo, and any installed App; quit the App first; refreshes the default profile"},
		Item{Label: "Uninstall menu bar App", Args: []string{"app", "uninstall"}, Impact: "Removes only the App; quit it and disable its login item first"},
		Item{Label: "Uninstall Nagi", Args: []string{"uninstall"}, Impact: "Stops Nagi and removes managed executables, App, and services; preserves user data (use the CLI --purge-data option to remove it)"},
	)
}

func helperPageForOS(goos string) *Page {
	return &Page{Title: "Privileged helper", Load: func(ctx context.Context, s *Session) (PageView, error) {
		if goos != "darwin" {
			return PageView{Context: "Privileged helper is available on macOS only."}, nil
		}
		return PageView{Context: pageStatus(ctx, s, "Privileged helper", []string{"privileged-helper", "status"}, "installed", "enabled", "backend"), Actions: []Item{
			{Label: "Privileged helper details", Args: []string{"privileged-helper", "status"}},
			{Label: "Install privileged helper", Args: []string{"privileged-helper", "install"}, Impact: "Requires administrator privileges and root-owned /usr/local/bin/nagi"},
			{Label: "Select privileged backend", Args: []string{"privileged-helper", "enable"}, Impact: "Changes subsequent starts; the user mihomo process must be stopped"},
			{Label: "Select user backend", Args: []string{"privileged-helper", "disable"}, Impact: "Changes subsequent starts; privileged mihomo must be stopped"},
			{Label: "Uninstall privileged helper", Args: []string{"privileged-helper", "uninstall"}, Impact: "Stops and removes the system helper; stop privileged mihomo and select the user backend first"},
		}}, nil
	}}
}
