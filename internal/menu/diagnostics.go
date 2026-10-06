package menu

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

func init() {
	RegisterPage("service", ServicePage)
	RegisterPage("diagnostics", DiagnosticsPage)
}

// ServicePage exposes the existing per-user service and startup commands.
func ServicePage() *Page {
	return &Page{Title: "Service and startup", Help: []string{"startup"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Context: "The login service can stop mihomo when disabled or removed. startup check repairs stale markers and checks managed proxy settings; it never starts mihomo.", Items: []Item{
			{Label: "Service status", Args: []string{"service", "status"}},
			{Label: "Install login service", Args: []string{"service", "install"}, Confirm: "Install and enable the login service?"},
			{Label: "Uninstall login service", Args: []string{"service", "uninstall"}, Confirm: "Stop and remove the login service? This can stop mihomo."},
			{Label: "Startup status", Args: []string{"startup", "status"}},
			{Label: "Enable login startup", Args: []string{"startup", "enable"}, Confirm: "Enable the installed login service?"},
			{Label: "Disable login startup", Args: []string{"startup", "disable"}, Confirm: "Disable the login service? This can stop mihomo."},
			{Label: "Check startup and runtime state", Args: []string{"startup", "check"}},
			{Label: "Recover stale runtime markers", Args: []string{"recover"}, Confirm: "Remove stale PID and socket markers after an unexpected exit?"},
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
		if goos == "darwin" {
			items = append(items,
				Item{Label: "Privileged helper status", Args: []string{"privileged-helper", "status"}},
				Item{Label: "Install privileged helper", Args: []string{"privileged-helper", "install"}, Confirm: "Install the macOS system helper? This requires administrator privileges and a root-owned /usr/local/bin/nagi."},
				Item{Label: "Uninstall privileged helper", Args: []string{"privileged-helper", "uninstall"}, Confirm: "Stop and remove the macOS system helper? Stop privileged mihomo and select the user backend first."},
				Item{Label: "Select privileged backend", Args: []string{"privileged-helper", "enable"}, Confirm: "Select the privileged backend for subsequent starts? The user mihomo process must be stopped."},
				Item{Label: "Select user backend", Args: []string{"privileged-helper", "disable"}, Confirm: "Select the user backend for subsequent starts? Privileged mihomo must be stopped."},
			)
		}
		items = append(items, Item{Label: "Shell completion", Next: CompletionPage})
		return PageView{Context: "Reports are redacted. A reachable control API or configured listener does not prove proxy traffic can pass.", Items: items}, nil
	}}
}

func exportDiagnostics(ctx context.Context, s *Session) error {
	path, err := s.Prompt("Absolute destination file for owner-only JSON report")
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		s.WriteLine("No destination entered; export cancelled.")
		return nil
	}
	if !filepath.IsAbs(path) {
		s.WriteLine("Enter an absolute file path (for example, /tmp/nagi-diagnostics.json).")
		return nil
	}
	s.Run(ctx, "diagnostics", "export", path)
	return nil
}

func CompletionPage() *Page {
	return &Page{Title: "Shell completion", Help: []string{"completion"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Context: "Generated scripts can be long and will print in the terminal. To install one, redirect `nagi completion SHELL` to a file using your shell.", Items: []Item{
			{Label: "Generate Bash completion script", Args: []string{"completion", "bash"}, Confirm: "Print the Bash completion script here?"},
			{Label: "Generate Zsh completion script", Args: []string{"completion", "zsh"}, Confirm: "Print the Zsh completion script here?"},
			{Label: "Generate Fish completion script", Args: []string{"completion", "fish"}, Confirm: "Print the Fish completion script here?"},
			{Label: "Show completion candidates", Action: completionCandidates},
		}}, nil
	}}
}

func completionCandidates(ctx context.Context, s *Session) error {
	resource, err := s.Select("Candidate resource", []string{"profile", "subscription", "groups", "nodes"})
	if err != nil {
		return err
	}
	args := []string{"completion", "candidates", resource}
	if resource == "nodes" {
		group, err := s.Prompt("Group name (blank for all nodes)")
		if err != nil {
			return err
		}
		if group = strings.TrimSpace(group); group != "" {
			args = append(args, group)
		}
	}
	s.Run(ctx, args...)
	return nil
}
