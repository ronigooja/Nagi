package menu

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strconv"
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
	return &Page{Title: "Traffic access", Help: help, Load: func(ctx context.Context, s *Session) (PageView, error) {
		items := []Item{
			{Label: "TUN status", Args: []string{"tun", "status"}},
			{Label: "Enable TUN", Args: []string{"tun", "enable"}},
			{Label: "Disable TUN", Args: []string{"tun", "disable"}, Impact: "Disables runtime TUN"},
			{Label: "Listener ports", Args: []string{"ports", "status"}},
			{Label: "LAN access status", Args: []string{"lan", "status"}},
			{Label: "Enable LAN access", Impact: "Exposes listeners on the selected interface", Action: enableLAN},
			{Label: "Disable LAN access", Args: []string{"lan", "disable"}, Impact: "Disables LAN access and binds listeners to loopback"},
		}
		if runtime.GOOS == "darwin" {
			items = append([]Item{
				{Label: "System proxy status", Args: []string{"system-proxy", "status"}},
				{Label: "Enable system proxy", Args: []string{"system-proxy", "enable"}},
				{Label: "Disable system proxy", Args: []string{"system-proxy", "disable"}, Impact: "Restores OS proxy settings saved by Nagi"},
			}, items...)
		}
		summary := []string{
			pageStatus(ctx, s, "TUN", []string{"tun", "status"}, "enabled"),
			pageStatus(ctx, s, "Listener ports", []string{"ports", "status"}, "http", "socks", "mixed"),
			pageStatus(ctx, s, "LAN", []string{"lan", "status"}, "enabled", "bind_address"),
		}
		if runtime.GOOS == "darwin" {
			summary = append(summary, pageStatus(ctx, s, "System proxy", []string{"system-proxy", "status"}, "enabled"))
		}
		return PageView{Context: strings.Join(summary, "\n"), Actions: items}, nil
	}}
}

func enableLAN(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "address", Label: "Bind IPv4/IPv6 address (blank for all interfaces)", Validate: func(v string) error {
		if v != "" && net.ParseIP(v) == nil {
			return fmt.Errorf("enter an IPv4 or IPv6 address")
		}
		return nil
	}}}, nil, func(v map[string]string) Result {
		args := []string{"lan", "enable"}
		if v["address"] != "" {
			args = append(args, v["address"])
		}
		return s.Run(ctx, args...)
	})
	return err
}

func ConnectionsPage() *Page {
	return &Page{Title: "Connections", Help: []string{"connections"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Actions: []Item{
			{Label: "List active connections", Args: []string{"connections", "list"}},
			{Label: "Show a connection", Action: connectionAction("show")},
			{Label: "Close a connection", Impact: "Closes the selected connection", Action: connectionAction("close")},
			{Label: "Close all connections", Args: []string{"connections", "close-all"}, Impact: "Closes all current connections"},
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

		s.Run(ctx, "connections", verb, id)
		return nil
	}
}

func LogsPage() *Page {
	return &Page{Title: "Logs and traffic", Help: []string{"logs"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Actions: []Item{
			{Label: "Show recent logs", Action: recentLogs},
			{Label: "Follow logs (Ctrl-C returns here)", Action: func(ctx context.Context, s *Session) error { s.Stream(ctx, "logs", "follow"); return nil }},
			{Label: "Watch traffic (Ctrl-C returns here)", Action: func(ctx context.Context, s *Session) error { s.Stream(ctx, "traffic", "watch"); return nil }},
		}}, nil
	}}
}

func recentLogs(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "lines", Label: "Number of log lines (blank for 100; 1-10000)", Validate: func(v string) error {
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10000 {
			return fmt.Errorf("enter a number from 1 to 10000")
		}
		return nil
	}}}, nil, func(v map[string]string) Result {
		if v["lines"] == "" {
			return s.Run(ctx, "logs")
		}
		return s.Run(ctx, "logs", v["lines"])
	})
	return err
}
