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
	return &Page{Title: "Service and startup", Help: []string{"startup"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: pageStatus(ctx, s, "Service", []string{"service", "status"}, "installed", "enabled", "active") + "\n" + pageStatus(ctx, s, "Startup", []string{"startup", "status"}, "enabled", "status") + "\nThe login service can stop mihomo when disabled or removed. startup check repairs stale markers and checks managed proxy settings; it never starts mihomo.", Actions: []Item{
			{Label: "Service status", Args: []string{"service", "status"}},
			{Label: "Install login service", Args: []string{"service", "install"}, Impact: "Installs and enables the login service"},
			{Label: "Uninstall login service", Args: []string{"service", "uninstall"}, Impact: "Stops and removes the login service; can stop mihomo"},
			{Label: "Startup status", Args: []string{"startup", "status"}},
			{Label: "Enable login startup", Args: []string{"startup", "enable"}, Impact: "Enables the installed login service"},
			{Label: "Disable login startup", Args: []string{"startup", "disable"}, Impact: "Disables the login service; can stop mihomo"},
			{Label: "Check startup and runtime state", Args: []string{"startup", "check"}},
			{Label: "Recover stale runtime markers", Args: []string{"recover"}, Impact: "Removes stale PID and socket markers after an unexpected exit"},
		}}, nil
	}}
}

func DiagnosticsPage() *Page { return diagnosticsPageForOS(runtime.GOOS) }

func diagnosticsPageForOS(goos string) *Page {
	return &Page{Title: "Diagnostics and recovery", Help: []string{"diagnostics"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		items := []Item{
			{Label: "Run doctor checks", Args: []string{"doctor"}},
			{Label: "Run redacted diagnostics", Args: []string{"diagnostics"}},
			{Label: "Export redacted diagnostics", Action: exportDiagnostics},
			{Label: "Show version", Args: []string{"version"}},
		}

		items = append(items, Item{Label: "Shell completion", Next: CompletionPage})
		return PageView{Context: "Reports are redacted. A reachable control API or configured listener does not prove proxy traffic can pass.", Actions: items}, nil
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

func CompletionPage() *Page {
	return &Page{Title: "Shell completion", Help: []string{"completion"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Context: "Generated scripts can be long and will print in the terminal. To install one, redirect `nagi completion SHELL` to a file using your shell.", Actions: []Item{
			{Label: "Generate Bash completion script", Args: []string{"completion", "bash"}, Impact: "Prints the full Bash completion script"},
			{Label: "Generate Zsh completion script", Args: []string{"completion", "zsh"}, Impact: "Prints the full Zsh completion script"},
			{Label: "Generate Fish completion script", Args: []string{"completion", "fish"}, Impact: "Prints the full Fish completion script"},
			{Label: "Show completion candidates", Action: completionCandidates},
		}}, nil
	}}
}

func completionCandidates(ctx context.Context, s *Session) error {
	resource, err := s.Select("Candidate resource", []string{"profile", "subscription", "groups", "nodes"})
	if err != nil {
		return err
	}
	fields := []Field{}
	if resource == "nodes" {
		fields = append(fields, Field{Key: "group", Label: "Group name (blank for all nodes)"})
	}
	_, err = s.Form(ctx, fields, nil, func(v map[string]string) Result {
		args := []string{"completion", "candidates", resource}
		if v["group"] != "" {
			args = append(args, v["group"])
		}
		return s.Run(ctx, args...)
	})
	return err
}

func AdvancedPage() *Page { return advancedPageForOS(runtime.GOOS) }

func advancedPageForOS(goos string) *Page {
	return &Page{Title: "Advanced features", Help: []string{"menu"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		items := []Item{
			{Label: "DNS", Next: DNSPage},
			{Label: "Rules", Next: RulesPage},
			{Label: "Connections", Next: ConnectionsPage},
			{Label: "Logs and traffic", Next: LogsPage},
			{Label: "Service and startup", Next: ServicePage},
		}
		if goos == "darwin" {
			items = append(items, Item{Label: "Privileged helper", Next: func() *Page { return helperPageForOS(goos) }})
		}
		return PageView{Actions: items}, nil
	}}
}

func helperPageForOS(goos string) *Page {
	return &Page{Title: "Privileged helper", Help: []string{"privileged-helper"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		if goos != "darwin" {
			return PageView{Context: "Privileged helper is available on macOS only."}, nil
		}
		return PageView{Context: pageStatus(ctx, s, "Privileged helper", []string{"privileged-helper", "status"}, "installed", "enabled", "backend"), Actions: []Item{
			{Label: "Privileged helper status", Args: []string{"privileged-helper", "status"}},
			{Label: "Install privileged helper", Args: []string{"privileged-helper", "install"}, Impact: "Requires administrator privileges and root-owned /usr/local/bin/nagi"},
			{Label: "Uninstall privileged helper", Args: []string{"privileged-helper", "uninstall"}, Impact: "Stops and removes the system helper; stop privileged mihomo and select the user backend first"},
			{Label: "Select privileged backend", Args: []string{"privileged-helper", "enable"}, Impact: "Changes subsequent starts; the user mihomo process must be stopped"},
			{Label: "Select user backend", Args: []string{"privileged-helper", "disable"}, Impact: "Changes subsequent starts; privileged mihomo must be stopped"},
		}}, nil
	}}
}
