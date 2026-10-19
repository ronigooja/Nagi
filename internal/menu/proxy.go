package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type menuProxyGroup struct {
	Name           string                   `json:"name"`
	Type           string                   `json:"type"`
	Now            string                   `json:"now"`
	Fixed          string                   `json:"fixed"`
	All            []string                 `json:"all"`
	Alive          bool                     `json:"alive"`
	SelectedStatus string                   `json:"selected_status"`
	Hidden         bool                     `json:"hidden"`
	Icon           string                   `json:"icon"`
	NodeInfo       map[string]menuNodeInfo  `json:"node_info"`
	NodeDelays     map[string]menuNodeDelay `json:"node_delays"`
}

func init() { RegisterPage("proxy", ProxyPage) }

func localizeddecodeMenuResult(language i18n.Context, result Result, target any) error {
	if result.Code != 0 || result.Error != nil {
		if result.Error != nil {
			return errors.New(result.errorText(language))
		}
		return fmt.Errorf("%s", menuFormat(language, "menu.proxy.decode_menu_result.command_exited_with_status_d", "command exited with status %d", result.Code))
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// ProxyPage groups proxy selection and profile-local proxy sources.
func localizedProxyPage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.proxy.proxy_page.proxies", "Proxies"), Item{ID: "proxy.ProxyPage.item1", Label: menuText(language, "menu.proxy.proxy_page.proxy_groups", "Proxy Groups"), Next: func() *Page {
		return localizedproxyGroupsPage(language)
	},
	}, Item{ID: "proxy.ProxyPage.item2", Label: menuText(language, "menu.proxy.proxy_page.proxy_providers", "Proxy Providers"), Next: func() *Page {
		return localizedproviderListPage(language)
	},
	}, Item{ID: "proxy.ProxyPage.item3", Label: menuText(language, "menu.proxy.proxy_page.tailscale", "Tailscale"), Next: func() *Page {
		return localizedtailscaleListPage(language)
	},
	})
}

func localizedproxyGroupsPage(language i18n.Context) *Page {
	return &Page{ID: "proxy.proxyGroupsPage.page1", Title: menuText(language, "menu.proxy.proxy_groups_page.proxy_groups", "Proxy Groups"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.proxy.proxy_groups_page.select_a_group_then_a_node_to_switch", "Select a group, then a node to switch."), Empty: menuText(language, "menu.proxy.proxy_groups_page.no_proxy_groups_check_the_profile_in_profiles_su", "No proxy groups. Check the profile in Profiles & Subscriptions > Profiles."), Actions: []Item{
			{ID: "proxy.proxyGroupsPage.item2", Label: menuText(language, "menu.proxy.proxy_groups_page.restore_saved_selections", "Restore saved selections"), Args: []string{"proxy", "restore"}},
		}}
		var data struct {
			Groups []menuProxyGroup `json:"groups"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "groups"), &data); err != nil {
			view.Context = menuText(language, "menu.proxy.proxy_groups_page.mihomo_unknown_proxy_group_state_unknown", "Mihomo: unknown | Proxy group state: unknown")
			view.ReadErrorHint = menuText(language, "menu.proxy.proxy_groups_page.open_service_to_check_status_or_monitoring_diagn", "Open Service to check status or Monitoring & Diagnostics to inspect logs, then return here.")
			var status struct {
				Running        *bool `json:"running"`
				UnexpectedExit bool  `json:"unexpected_exit"`
			}
			if statusErr := localizeddecodeMenuResult(language, s.Fetch(ctx, "status"), &status); statusErr != nil {
				view.Context += menuText(language, "menu.proxy.proxy_groups_page.status_unavailable", "\nStatus unavailable: ") + statusErr.Error()
			} else if status.Running != nil {
				switch {
				case *status.Running:
					view.Context = menuText(language, "menu.proxy.proxy_groups_page.mihomo_running_proxy_group_state_unknown", "Mihomo: running | Proxy group state: unknown")
					view.ReadErrorHint = menuText(language, "menu.proxy.proxy_groups_page.check_logs_in_monitoring_diagnostics_open_servic", "Check logs in Monitoring & Diagnostics; open Service if a restart is needed.")
				case status.UnexpectedExit:
					view.Context = menuText(language, "menu.proxy.proxy_groups_page.mihomo_stopped_unexpected_exit_proxy_group_state", "Mihomo: stopped (unexpected exit) | Proxy group state: unavailable")
					view.ReadErrorHint = menuText(language, "menu.proxy.proxy_groups_page.check_logs_in_monitoring_diagnostics_clean_stale", "Check logs in Monitoring & Diagnostics, clean stale runtime records in Service > Startup Recovery, then start mihomo through Service.")
				default:
					view.Context = menuText(language, "menu.proxy.proxy_groups_page.mihomo_stopped_proxy_group_state_unavailable", "Mihomo: stopped | Proxy group state: unavailable")
					view.ReadErrorHint = menuText(language, "menu.proxy.proxy_groups_page.start_mihomo_through_service_then_return_here", "Start mihomo through Service, then return here.")
					view.Empty = view.ReadErrorHint
					return view, nil
				}
			}
			return view, err
		}
		for _, group := range data.Groups {
			if group.Hidden {
				continue
			}
			group := group
			view.Items = append(view.Items, Item{ID: "proxy.proxyGroupsPage.item3", Name: group.Name, Label: localizedproxyGroupLabel(language, group), Next: func() *Page { return localizedproxyGroupPage(language, group.Name) }})
		}
		return view, nil
	}}
}

func localizedautomaticProxyGroup(language i18n.Context, group menuProxyGroup) bool {
	return strings.EqualFold(group.Type, "URLTest") || strings.EqualFold(group.Type, "Fallback")
}

func localizedproxyGroupLabel(language i18n.Context, group menuProxyGroup) string {
	label := group.Name
	if group.Icon != "" {
		label = "[icon] " + label
	}
	if group.Now != "" {
		label += " → " + group.Now
	}
	if localizedautomaticProxyGroup(language, group) {
		if group.Fixed == "" {
			label += menuText(language, "menu.proxy.group.automatic", " (automatic)")
		} else {
			label += menuText(language, "menu.proxy.group.fixed", " (fixed: ") + group.Fixed + ")"
		}
	}
	if group.SelectedStatus != "" && group.SelectedStatus != "available" {
		label += " (" + menuState(language, group.SelectedStatus) + ")"
	}
	return label
}

type menuProxyMeasurement struct {
	Proxy   string `json:"proxy"`
	DelayMS *int   `json:"delay_ms"`
	Error   string `json:"error"`
}

func localizedproxyGroupPage(language i18n.Context, name string) *Page {
	return localizedproxyNodesPage(language, name, false)
}

// Batch ordering belongs to the page. Engine measurements remain authoritative
// on every reload, including failures newer than the last batch.
func localizedproxyNodesPage(language i18n.Context, name string, testOnly bool) *Page {
	measurements := map[string]menuProxyMeasurement{}
	order := []string{}
	title := menuText(language, "menu.proxy.proxy_nodes_page.proxy_group", "Proxy group: ") + name
	if testOnly {
		title = menuText(language, "menu.proxy.proxy_nodes_page.test_node_latency", "Test node latency: ") + name
	}
	return &Page{ID: "proxy.proxyNodesPage.page1", Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: menuText(language, "menu.proxy.proxy_nodes_page.no_nodes_available_inspect_nagi_proxy_show", "No nodes available. Inspect `nagi proxy show`."), Actions: []Item{
			{ID: "proxy.proxyNodesPage.item2", Label: menuText(language, "menu.proxy.proxy_nodes_page.show_group_details", "Show group details"), Args: []string{"proxy", "show", name}},
		}}
		if !testOnly {
			view.Actions = append(view.Actions,
				Item{ID: "proxy.proxyNodesPage.item3", Label: menuText(language, "menu.proxy.proxy_nodes_page.test_one_node", "Test one node"), Next: func() *Page { return localizedproxyNodesPage(language, name, true) }},
				Item{ID: "proxy.proxyNodesPage.item4", Label: menuText(language, "menu.proxy.proxy_nodes_page.test_all_nodes_and_select_from_results", "Test all nodes and select from results"), Impact: menuText(language, "menu.proxy.proxy_nodes_page.may_clear_a_fixed_choice_and_its_saved_selection", "May clear a fixed choice and its saved selection for automatic groups"), Action: func(ctx context.Context, s *Session) error {
					s.WriteLine(menuText(language, "menu.proxy.proxy_nodes_page.testing_node_latency", "Testing node latency..."))
					result := s.Fetch(ctx, "proxy", "delays", name)
					var data struct {
						Results []menuProxyMeasurement `json:"results"`
					}
					if err := localizeddecodeMenuResult(language, result, &data); err != nil {
						s.ShowResult([]string{"proxy", "delays", name}, result)
						return nil
					}
					measurements = map[string]menuProxyMeasurement{}
					order = nil
					for _, measurement := range data.Results {
						order = append(order, measurement.Proxy)
						measurements[measurement.Proxy] = measurement
					}
					s.feedback(feedbackSuccess, menuFormat(language, "menu.proxy.proxy_nodes_page.latency_test_completed_d_results", "Latency test completed: %d results.", len(order)))
					return nil
				}},
				Item{ID: "proxy.proxyNodesPage.item5", Label: menuText(language, "menu.proxy.proxy_nodes_page.restore_saved_selections", "Restore saved selections"), Args: []string{"proxy", "restore"}},
			)
		}
		var group menuProxyGroup
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "show", name), &group); err != nil {
			view.Context = menuText(language, "menu.proxy.proxy_nodes_page.current_node_and_selection_state_unknown", "Current node and selection state: unknown")
			return view, err
		}
		current := group.Now
		if current == "" {
			current = "none"
		}
		status := group.SelectedStatus
		if status == "" {
			status = "unknown"
		}
		view.Context = menuFormat(language, "menu.proxy.proxy_nodes_page.current_s_s_available_nodes_d", "Current: %s (%s); available nodes: %d", current, status, len(group.All))
		if localizedautomaticProxyGroup(language, group) {
			if group.Fixed == "" {
				view.Context += menuText(language, "menu.proxy.proxy_nodes_page.selection_automatic", "\nSelection: automatic")
			} else {
				view.Context += menuText(language, "menu.proxy.proxy_nodes_page.selection_fixed_to", "\nSelection: fixed to ") + group.Fixed
			}
			if !testOnly {
				view.Actions = append(view.Actions, Item{ID: "proxy.proxyNodesPage.item6", Label: menuText(language, "menu.proxy.proxy_nodes_page.resume_automatic_selection", "Resume automatic selection"), Impact: menuText(language, "menu.proxy.proxy_nodes_page.clears_this_group_s_fixed_choice_and_saved_selec", "Clears this group's fixed choice and saved selection"), Args: []string{"proxy", "auto", name}})
			}
		}
		if testOnly {
			view.Context += menuText(language, "menu.proxy.proxy_nodes_page.choose_a_node_to_measure_latency", "\nChoose a node to measure latency.")
		} else {
			view.Context += menuText(language, "menu.proxy.proxy_nodes_page.choose_a_node_to_switch_immediately", "\nChoose a node to switch immediately.")
		}
		live := map[string]bool{}
		for _, node := range group.All {
			live[node] = true
		}
		nodes := []string{}
		keptOrder := []string{}
		for _, node := range order {
			if live[node] {
				nodes = append(nodes, node)
				keptOrder = append(keptOrder, node)
				delete(live, node)
			} else {
				delete(measurements, node)
			}
		}
		order = keptOrder
		for _, node := range group.All {
			if live[node] {
				nodes = append(nodes, node)
			}
		}
		if len(order) > 0 {
			view.Context += menuText(language, "menu.proxy.proxy_nodes_page.last_latency_results_first_unmeasured_nodes_foll", "\nLast latency results first; unmeasured nodes follow.")
		}
		for _, node := range nodes {
			if testOnly && localizednodeKind(language, node, group.NodeInfo) != "node" {
				continue
			}
			label := localizednodeLabel(language, node, group.NodeInfo, group.NodeDelays)
			if localizednodeKind(language, node, group.NodeInfo) == "node" {
				if _, liveHistory := group.NodeDelays[node]; !liveHistory {
					if measurement, ok := measurements[node]; ok {
						delay := menuNodeDelay{Status: "ok", DelayMS: measurement.DelayMS}
						if measurement.Error != "" {
							delay.Status = "failed"
						}
						label = node + " — " + delay.labelFor(language)
					}
				}
			}
			if node == group.Now {
				label += menuText(language, "menu.proxy.selected", " (selected)")
			}
			args := []string{"proxy", "select", name, node}
			if testOnly {
				args = []string{"proxy", "test", "group", name, node}
			}
			view.Items = append(view.Items, Item{ID: "proxy.proxyNodesPage.item7", Name: node, Label: label, Selected: node == group.Now, Args: args})
		}
		return view, nil
	}}
}
func decodeMenuResult(result Result, target any) error {
	return localizeddecodeMenuResult(i18n.New(i18n.English), result, target)
}

func ProxyPage() *Page {
	return localizedProxyPage(i18n.New(i18n.English))
}
func proxyGroupsPage() *Page {
	return localizedproxyGroupsPage(i18n.New(i18n.English))
}
func automaticProxyGroup(group menuProxyGroup) bool {
	return localizedautomaticProxyGroup(i18n.New(i18n.English), group)
}
func proxyGroupLabel(
	group menuProxyGroup) string {
	return localizedproxyGroupLabel(i18n.New(i18n.English), group)
}
func proxyGroupPage(name string) *Page {
	return localizedproxyGroupPage(i18n.New(i18n.English), name)
}

func proxyNodesPage(name string, testOnly bool) *Page {
	return localizedproxyNodesPage(i18n.New(i18n.English), name, testOnly)
}
