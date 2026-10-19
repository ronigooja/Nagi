package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	RegisterPage("runtime", RuntimePage)
	RegisterPage("routing", RoutingPage)
	RegisterPage("mode", func() *Page {
		return actionPage("Proxy Mode", Item{ID: "status.init.item1", Label: "For this session", Next: func() *Page { return modePage(false) }}, Item{ID: "status.init.item2", Label: "Save for future starts", Next: func() *Page { return modePage(true) }})
	})
}

func localizedconfigurationPage(language i18n.Context) *Page {
	return &Page{ID: "status.configurationPage.page1", Title: menuText(language, "menu.status.configuration_page.current_configuration", "Current Configuration"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: s.statusSummary(s.Fetch(ctx, "status")), Actions: []Item{
			{ID: "status.configurationPage.item2", Label: menuText(language, "menu.status.configuration_page.show_current_configuration", "Show current configuration"), Args: []string{"config", "show"}},
			{ID: "status.configurationPage.item3", Label: menuText(language, "menu.status.configuration_page.validate_current_configuration", "Validate current configuration"), Args: []string{"config", "validate"}},
			{ID: "status.configurationPage.item4", Label: menuText(language, "menu.status.configuration_page.reload_configuration", "Reload configuration"), Args: []string{"config", "reload"}},
		}}, nil
	}}
}

func localizedstatusSummary(language i18n.Context, result Result) string {
	if result.Code != 0 && result.Error == nil {
		return menuText(language, "menu.status.status_summary.mihomo_unknown_profile_unknown_status_unavailabl", "Mihomo: unknown | Profile: unknown. Status unavailable.")
	}
	if result.Error != nil {
		return menuText(language, "menu.status.status_summary.mihomo_unknown_profile_unknown_status_unavailabl2", "Mihomo: unknown | Profile: unknown. Status unavailable: ") + result.errorText(language) + menuText(language, "menu.status.status_summary.open_service_for_status_or_monitoring_diagnostic", ". Open Service for status or Monitoring & Diagnostics for logs.")
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return menuText(language, "menu.status.status_summary.mihomo_unknown_profile_unknown_status_unavailabl3", "Mihomo: unknown | Profile: unknown. Status unavailable. Try `nagi status`.")
	}
	state := "unknown"
	if running, ok := data["running"].(bool); ok {
		state = "stopped"
		if running {
			state = "running"
		}
	}
	profile, _ := data["profile"].(string)
	if profile == "" {
		return menuText(language, "menu.status.status_summary.mihomo", "Mihomo: ") + menuState(language, state) + menuText(language, "menu.status.status_summary.profile_unknown", " | Profile: unknown")
	}
	return menuFormat(language, "menu.status.status_summary.mihomo_s_profile_s", "Mihomo: %s | Profile: %s", menuState(language, state), profile)
}

func localizedRuntimePage(language i18n.Context) *Page {
	return &Page{ID: "status.RuntimePage.page1", Title: menuText(language, "menu.status.runtime_page.start_stop", "Start / Stop"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: s.statusSummary(s.Fetch(ctx, "status")), Actions: []Item{
			{ID: "status.RuntimePage.item2", Label: menuText(language, "menu.status.runtime_page.start_mihomo", "Start mihomo"), Args: []string{"start"}},
			{ID: "status.RuntimePage.item3", Label: menuText(language, "menu.status.runtime_page.stop_mihomo", "Stop mihomo"), Args: []string{"stop"}},
			{ID: "status.RuntimePage.item4", Label: menuText(language, "menu.status.runtime_page.restart_mihomo", "Restart mihomo"), Args: []string{"restart"}},
			{ID: "status.RuntimePage.item5", Label: menuText(language, "menu.status.runtime_page.show_full_status", "Show full status"), Args: []string{"status"}},
			{ID: "status.RuntimePage.item6", Label: menuText(language, "menu.status.runtime_page.quit_nagi_for_this_login_session", "Quit Nagi for this login session"), Args: []string{"quit"}, Impact: menuText(language, "menu.status.runtime_page.stops_the_login_monitor_and_mihomo_for_this_sess", "Stops the login monitor and mihomo for this session")},
		}}
		return view, nil
	}}
}

func localizedmodePage(language i18n.Context, persistent bool) *Page {
	title := menuText(language, "menu.status.mode_page.for_this_session", "For this session")
	command := []string{"mode"}
	if persistent {
		title = menuText(language, "menu.status.mode_page.save_for_future_starts", "Save for future starts")
		command = []string{"mode", "saved"}
	}
	return &Page{ID: "status.modePage.page1", Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, command...)
		contextLine := menuText(language, "menu.status.mode_page.current_mode_unavailable", "Current mode unavailable")
		if result.Error != nil {
			contextLine += ": " + result.errorText(language)
		} else if data, ok := result.Data.(map[string]any); ok && result.Code == 0 && data["mode"] != nil {
			contextLine = menuFormat(language, "menu.status.mode_page.current_mode_v", "Current mode: %v", menuState(language, fmt.Sprint(data["mode"])))
		}
		view := PageView{Context: contextLine}
		for _, mode := range []string{"rule", "global", "direct"} {
			args := []string{"mode", mode}
			if persistent {
				args = []string{"mode", "save", mode}
			}
			view.Actions = append(view.Actions, Item{ID: "status.modePage.item2", Label: map[string]string{"rule": menuText(language, "menu.status.mode_page.rule_follow_routing_rules", "Rule — follow routing rules"), "global": menuText(language, "menu.status.mode_page.global_use_the_global_proxy_group", "Global — use the GLOBAL proxy group"), "direct": menuText(language, "menu.status.mode_page.direct_connect_without_a_proxy", "Direct — connect without a proxy")}[mode], Args: args})
		}
		return view, nil
	}}
}

// pageStatus shows only the fields needed for a page summary, never raw JSON.
func pageStatus(ctx context.Context, s *Session, label string, args []string, fields ...string) string {
	return localizedpageStatusResult(s.language,
		s.Fetch(ctx, args...), label, fields...)
}

func localizedpageStatusResult(language i18n.Context, result Result, label string, fields ...string) string {
	if result.Error != nil {
		return label + ": " + menuState(language, "unknown") + " — " + result.errorText(language)
	}
	if result.Code != 0 {
		return label + menuText(language, "menu.status.page_status_result.unknown_read_failed", ": unknown — read failed")
	}
	object := localizedmenuObject(language, result.Data)
	values := []string{}
	for _, field := range fields {
		var value any
		if raw, ok := object[field]; ok && json.Unmarshal(raw, &value) == nil && value != nil {
			values = append(values, menuField(language, field)+": "+stateString(language, value))
		}
	}
	if len(values) == 0 {
		return label + ": " + menuState(language, "unknown")
	}
	return label + ": " + strings.Join(values, ", ")
}

func localizedreadableState(language i18n.Context, value any) string {
	if flag, ok := value.(bool); ok {
		if flag {
			return "yes"
		}
		return "no"
	}
	return fmt.Sprint(value)
}

func localizedRoutingPage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.status.routing_page.routing_dns", "Routing & DNS"), Item{ID: "status.RoutingPage.item1", Label: menuText(language, "menu.status.routing_page.proxy_mode", "Proxy Mode"), Next: func() *Page { return localizedProxyModePage(language) }}, Item{ID: "status.RoutingPage.item2", Label: menuText(language, "menu.status.routing_page.routing_rules", "Routing Rules"), Next: func() *Page {
		return localizedRulesPage(language)
	},
	}, Item{ID: "status.RoutingPage.item3", Label: "DNS", Next: func() *Page {
		return localizedDNSPage(language)
	},
	})
}
func configurationPage() *Page {
	return localizedconfigurationPage(i18n.New(i18n.English))
}
func statusSummary(result Result) string {
	return localizedstatusSummary(i18n.New(i18n.English), result)
}
func RuntimePage() *Page {
	return localizedRuntimePage(i18n.New(i18n.English))
}
func modePage(persistent bool) *Page {
	return localizedmodePage(i18n.New(i18n.English), persistent)
}
func pageStatusResult(result Result, label string, fields ...string) string {
	return localizedpageStatusResult(i18n.New(i18n.English), result, label, fields...)
}
func readableState(value any) string {
	return localizedreadableState(i18n.New(i18n.English), value)
}
func RoutingPage() *Page {
	return localizedRoutingPage(i18n.New(i18n.English))
}
