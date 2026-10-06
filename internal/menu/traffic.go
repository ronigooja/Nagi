package menu

import (
	"context"
	"runtime"
	"strings"
)

func init() {
	RegisterPage("traffic", TrafficPage)
	RegisterPage("connections", ConnectionsPage)
	RegisterPage("logs", LogsPage)
}

func TrafficPage() *Page {
	help := []string{"tun"}
	if runtime.GOOS == "darwin" {
		help = []string{"system-proxy"}
	}
	return &Page{Title: "Traffic access", Help: help, Load: func(_ context.Context, _ *Session) (PageView, error) {
		items := []Item{
			{Label: "TUN status", Args: []string{"tun", "status"}},
			{Label: "Enable TUN", Args: []string{"tun", "enable"}},
			{Label: "Disable TUN", Args: []string{"tun", "disable"}, Confirm: "Disable runtime TUN?"},
			{Label: "Listener ports", Args: []string{"ports", "status"}},
			{Label: "LAN access status", Args: []string{"lan", "status"}},
			{Label: "Enable LAN access", Action: enableLAN},
			{Label: "Disable LAN access", Args: []string{"lan", "disable"}, Confirm: "Disable LAN access and bind listeners to loopback?"},
		}
		if runtime.GOOS == "darwin" {
			items = append([]Item{
				{Label: "System proxy status", Args: []string{"system-proxy", "status"}},
				{Label: "Enable system proxy", Args: []string{"system-proxy", "enable"}},
				{Label: "Disable system proxy", Args: []string{"system-proxy", "disable"}, Confirm: "Restore the OS proxy settings saved by Nagi?"},
			}, items...)
		}
		return PageView{Items: items}, nil
	}}
}

func enableLAN(ctx context.Context, s *Session) error {
	address, err := s.Prompt("Bind IPv4/IPv6 address (blank for all interfaces)")
	if err != nil {
		return err
	}
	address = strings.TrimSpace(address)
	args := []string{"lan", "enable"}
	if address != "" {
		args = append(args, address)
	}
	s.Run(ctx, args...)
	return nil
}

func ConnectionsPage() *Page {
	return &Page{Title: "Connections", Help: []string{"connections"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Items: []Item{
			{Label: "List active connections", Args: []string{"connections", "list"}},
			{Label: "Show a connection", Action: connectionAction("show")},
			{Label: "Close a connection", Action: connectionAction("close")},
			{Label: "Close all connections", Args: []string{"connections", "close-all"}, Confirm: "Close all current connections? Type yes to confirm."},
		}}, nil
	}}
}

func connectionAction(verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		id, err := menuSelectName(ctx, s, []string{"connections", "list"}, "connections", "Connection ID")
		if err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		if verb == "close" {
			ok, err := s.Confirm("Close connection " + id + "?")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		s.Run(ctx, "connections", verb, id)
		return nil
	}
}

func LogsPage() *Page {
	return &Page{Title: "Logs and traffic", Help: []string{"logs"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Items: []Item{
			{Label: "Show recent logs", Action: recentLogs},
			{Label: "Follow logs (Ctrl-C returns here)", Action: func(ctx context.Context, s *Session) error { s.Stream(ctx, "logs", "follow"); return nil }},
			{Label: "Watch traffic (Ctrl-C returns here)", Action: func(ctx context.Context, s *Session) error { s.Stream(ctx, "traffic", "watch"); return nil }},
		}}, nil
	}}
}

func recentLogs(ctx context.Context, s *Session) error {
	lines, err := s.Prompt("Number of log lines (blank for 100; 1-10000)")
	if err != nil {
		return err
	}
	lines = strings.TrimSpace(lines)
	if lines == "" {
		s.Run(ctx, "logs")
	} else {
		s.Run(ctx, "logs", lines)
	}
	return nil
}
