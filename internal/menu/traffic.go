package menu

import "github.com/ronigooja/Nagi/internal/i18n"

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

func localizedTrafficPage(language i18n.Context) *Page {
	return localizedtrafficPageForOS(language, runtime.GOOS)
}

func localizedtrafficPageForOS(language i18n.Context, goos string) *Page {
	return &Page{ID: "traffic.trafficPageForOS.page1", Title: menuText(language, "menu.traffic.traffic_page_for_o_s.network_access", "Network Access"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		items := []Item{
			{ID: "traffic.trafficPageForOS.item2", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.enable_tun", "Enable TUN"), Args: []string{"tun", "enable"}},
			{ID: "traffic.trafficPageForOS.item3", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.disable_tun", "Disable TUN"), Args: []string{"tun", "disable"}},
			{ID: "traffic.trafficPageForOS.item4", Label: menuText(language, "menu.traffic.tun_details", "TUN details"), Args: []string{"tun", "status"}},
			{ID: "traffic.trafficPageForOS.item5", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.listener_ports", "Listener Ports"), Args: []string{"ports", "status"}},
			{ID: "traffic.trafficPageForOS.item6", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.enable_lan_access", "Enable LAN access"), Impact: menuText(language, "menu.traffic.traffic_page_for_o_s.exposes_listeners_on_the_selected_interface", "Exposes listeners on the selected interface"), Action: enableLAN},
			{ID: "traffic.trafficPageForOS.item7", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.disable_lan_access", "Disable LAN access"), Args: []string{"lan", "disable"}, Impact: menuText(language, "menu.traffic.traffic_page_for_o_s.binds_listeners_to_loopback", "Binds listeners to loopback")},
			{ID: "traffic.trafficPageForOS.item8", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.lan_access_details", "LAN access details"), Args: []string{"lan", "status"}},
		}
		tun := localizedstatePage(language, "TUN", []string{"tun", "status"}, []string{"enabled"}, items[:3]...)
		lan := localizedstatePage(language, menuText(language, "menu.traffic.traffic_page_for_o_s.lan_access", "LAN Access"), []string{"lan", "status"}, []string{"enabled", "bind_address"}, items[4:]...)
		items = []Item{{ID: "traffic.trafficPageForOS.item9", Label: "TUN", Next: func() *Page { return tun }}, {ID: "traffic.trafficPageForOS.item10", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.lan_access", "LAN Access"), Next: func() *Page { return lan }}, items[3]}
		if goos == "darwin" {
			system := localizedstatePage(language, menuText(language, "menu.traffic.traffic_page_for_o_s.system_proxy", "System Proxy"), []string{"system-proxy", "status"}, []string{"enabled"},
				Item{ID: "traffic.trafficPageForOS.item11", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.enable_system_proxy", "Enable system proxy"), Args: []string{"system-proxy", "enable"}},
				Item{ID: "traffic.trafficPageForOS.item12", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.disable_system_proxy", "Disable system proxy"), Args: []string{"system-proxy", "disable"}, Impact: menuText(language, "menu.traffic.traffic_page_for_o_s.restores_os_proxy_settings_saved_by_nagi", "Restores OS proxy settings saved by Nagi")},
				Item{ID: "traffic.trafficPageForOS.item13", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.system_proxy_details", "System proxy details"), Args: []string{"system-proxy", "status"}})
			items = append([]Item{{ID: "traffic.trafficPageForOS.item14", Label: menuText(language, "menu.traffic.traffic_page_for_o_s.system_proxy", "System Proxy"), Next: func() *Page { return system }}}, items...)

		}
		commands := [][]string{{"tun", "status"}, {"ports", "status"}, {"lan", "status"}}
		if goos == "darwin" {
			commands = append(commands, []string{"system-proxy", "status"})
		}
		results := s.fetchReads(ctx, commands)
		summary := []string{
			localizedSettingsSummary(language, results[0], commands[0], "TUN", "enabled"),
			localizedSettingsSummary(language, results[1], commands[1], menuText(language, "menu.traffic.traffic_page_for_o_s.listener_ports", "Listener Ports"), "http", "socks", "mixed"),
			localizedSettingsSummary(language, results[2], commands[2], menuText(language, "menu.traffic.traffic_page_for_o_s.lan_access", "LAN Access"), "enabled", "bind_address"),
		}
		if goos == "darwin" {
			summary = append(summary, localizedSettingsSummary(language, results[3], commands[3], menuText(language, "menu.traffic.traffic_page_for_o_s.system_proxy", "System Proxy"), "enabled"))
		}
		return PageView{Context: strings.Join(summary, "\n"), Actions: items}, nil
	}}
}

func enableLAN(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "address", Label: menuText(s.language, "menu.traffic.enable_l_a_n.bind_ipv4_ipv6_address_blank_for_all_interfaces", "Bind IPv4/IPv6 address (blank for all interfaces)"), Validate: func(v string) error {
		if v != "" && net.ParseIP(v) == nil {
			return fmt.Errorf("%s", menuText(s.language, "menu.traffic.enable_l_a_n.enter_an_ipv4_or_ipv6_address", "enter an IPv4 or IPv6 address"))
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

func localizedConnectionsPage(language i18n.Context) *Page {
	return &Page{ID: "traffic.ConnectionsPage.page1", Title: menuText(language, "menu.traffic.connections_page.connections", "Connections"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Actions: []Item{
			{ID: "traffic.ConnectionsPage.item2", Label: menuText(language, "menu.traffic.connections_page.list_active_connections", "List active connections"), Args: []string{"connections", "list"}},
			{ID: "traffic.ConnectionsPage.item3", Label: menuText(language, "menu.traffic.connections_page.show_a_connection", "Show a connection"), Action: localizedconnectionAction(language, "show")},
			{ID: "traffic.ConnectionsPage.item4", Label: menuText(language, "menu.traffic.connections_page.close_a_connection", "Close a connection"), Action: localizedconnectionAction(language, "close")},
			{ID: "traffic.ConnectionsPage.item5", Label: menuText(language, "menu.traffic.connections_page.close_all_connections", "Close all connections"), Args: []string{"connections", "close-all"}},
			{ID: "traffic.ConnectionsPage.item6", Label: menuText(language, "menu.traffic.connections_page.live_connections", "Live connections"), Action: liveConnections},
		}}, nil
	}}
}

func localizedconnectionAction(language i18n.Context, verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		id, err := menuSelectName(ctx, s, []string{"connections", "list"}, "connections", menuText(language, "menu.traffic.connection_action.connection_id", "Connection ID"))
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

func localizedLogsPage(language i18n.Context) *Page {
	return &Page{ID: "traffic.LogsPage.page1", Title: menuText(language, "menu.traffic.logs_page.logs", "Logs"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Actions: []Item{
			{ID: "traffic.LogsPage.item2", Label: menuText(language, "menu.traffic.logs_page.show_recent_logs", "Show recent logs"), Action: recentLogs},
			{ID: "traffic.LogsPage.item3", Label: menuText(language, "menu.traffic.logs_page.follow_logs_ctrl_c_returns_here", "Follow logs (Ctrl-C returns here)"), Action: func(ctx context.Context, s *Session) error { s.Stream(ctx, "logs", "follow"); return nil }},
		}}, nil
	}}
}

func recentLogs(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "lines", Label: menuText(s.language, "menu.traffic.recent_logs.number_of_log_lines_blank_for_100_1_10000", "Number of log lines (blank for 100; 1-10000)"), Validate: func(v string) error {
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10000 {
			return fmt.Errorf("%s", menuText(s.language, "menu.traffic.recent_logs.enter_a_number_from_1_to_10000", "enter a number from 1 to 10000"))
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

func localizedTrafficMonitorPage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.traffic.traffic_monitor_page.traffic", "Traffic"), Item{ID: "traffic.TrafficMonitorPage.item1", Label: menuText(language, "menu.traffic.traffic_monitor_page.watch_traffic_ctrl_c_returns_here", "Watch traffic (Ctrl-C returns here)"), Action: func(ctx context.Context, s *Session) error { s.Stream(ctx, "traffic", "watch"); return nil }})
}
func TrafficPage() *Page {
	return localizedTrafficPage(i18n.New(i18n.English))
}
func trafficPageForOS(goos string) *Page {
	return localizedtrafficPageForOS(i18n.New(i18n.English), goos)
}
func ConnectionsPage() *Page {
	return localizedConnectionsPage(i18n.New(i18n.English))
}
func connectionAction(verb string) func(context.
	Context, *Session) error {
	return localizedconnectionAction(i18n.New(i18n.English), verb)
}
func LogsPage() *Page {
	return localizedLogsPage(i18n.New(i18n.English))
}
func TrafficMonitorPage() *Page {
	return localizedTrafficMonitorPage(i18n.New(i18n.English))
}
