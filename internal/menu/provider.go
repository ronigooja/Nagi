package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type menuProvider struct {
	Name        string                   `json:"name"`
	Type        string                   `json:"type"`
	VehicleType string                   `json:"vehicle_type"`
	Nodes       []string                 `json:"nodes"`
	NodeInfo    map[string]menuNodeInfo  `json:"node_info"`
	NodeDelays  map[string]menuNodeDelay `json:"node_delays"`
}

func init() { RegisterPage("provider", providerListPage) }

func localizedproviderListPage(language i18n.Context) *Page {
	return &Page{ID: "provider.providerListPage.page1", Title: menuText(language, "menu.provider.provider_list_page.proxy_providers", "Proxy providers"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data struct {
			Providers []menuProvider `json:"providers"`
		}
		view := PageView{Empty: menuText(language, "menu.provider.provider_list_page.no_proxy_providers_returned_choose_add_provider", "No proxy providers returned. Choose Add provider."), Actions: []Item{
			{ID: "provider.providerListPage.item2", Label: menuText(language, "menu.provider.provider_list_page.add_provider", "Add provider"), Impact: menuText(language, "menu.provider.provider_list_page.updates_the_selected_profile_reloads_running_mih", "Updates the selected profile; reloads running mihomo"), Action: addProvider},
		}}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "providers"), &data); err != nil {
			return view, err
		}
		for _, p := range data.Providers {
			p := p
			view.Items = append(view.Items, Item{ID: "provider.providerListPage.item3", Name: p.Name, Label: menuFormat(language, "menu.provider.provider_list_page.s_s_d_nodes", "%s (%s) · %d nodes", p.Name, p.Type, len(p.Nodes)), Next: func() *Page { return localizedproviderDetailPage(language, p.Name) }})
		}
		return view, nil
	}}
}

func addProvider(ctx context.Context, s *Session) error {
	s.WriteLine(menuText(s.language, "menu.provider.add_provider.saves_to_the_selected_profile_reloads_mihomo_if", "Saves to the selected profile; reloads mihomo if running."))
	values := map[string]string{"seconds": "3600"}
	result, err := s.Form(ctx, []Field{
		localizednameField(s.language, menuText(s.language, "menu.provider.add_provider.new_provider_name", "New provider name")),
		{Key: "url", Label: menuText(s.language, "menu.provider.add_provider.subscription_http_s_url", "Subscription HTTP(S) URL"), Required: true, Validate: func(raw string) error {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") {
				return fmt.Errorf("%s", menuText(s.language, "menu.provider.add_provider.enter_an_http_s_url_with_a_hostname_and_without", "enter an HTTP(S) URL with a hostname and without user credentials or a fragment"))
			}
			return nil
		}},
		{Key: "group", Label: menuText(s.language, "menu.provider.add_provider.target_group_existing_or_new", "Target group (existing or new)"), Required: true, Validate: func(value string) error {
			if len(value) == 0 || len(value) > 128 {
				return fmt.Errorf("%s", menuText(s.language, "menu.provider.add_provider.enter_a_printable_group_name_from_1_to_128_bytes", "enter a printable group name from 1 to 128 bytes"))
			}
			for _, r := range value {
				if !unicode.IsPrint(r) {
					return fmt.Errorf("%s", menuText(s.language, "menu.provider.add_provider.enter_a_printable_group_name_from_1_to_128_bytes", "enter a printable group name from 1 to 128 bytes"))
				}
			}
			return nil
		}},
		{Key: "seconds", Label: menuText(s.language, "menu.provider.add_provider.update_interval_seconds_default_3600_1_31536000", "Update interval seconds (default 3600; 1–31536000)"), Required: true, Validate: func(raw string) error {
			seconds, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || seconds < 1 || seconds > 31536000 {
				return fmt.Errorf("%s", menuText(s.language, "menu.provider.add_provider.enter_an_integer_from_1_to_31536000_seconds", "enter an integer from 1 to 31536000 seconds"))
			}
			return nil
		}},
		localizedtestURLField(s.language,
			false),
	}, values, func(v map[string]string) Result {
		args := []string{"proxy", "provider", "add", v["name"], v["url"], v["group"], v["seconds"]}
		if v["test_url"] != "" {
			args = append(args, "--test-url", v["test_url"])
		}
		return s.Run(ctx, args...)
	})
	if err != nil {
		return err
	}
	var added struct {
		GroupCreated bool `json:"group_created"`
	}
	if localizeddecodeMenuResult(s.language, result, &added) == nil && added.GroupCreated {
		s.feedback(feedbackWarning, menuText(s.language, "menu.provider.add_provider.new_group_created_with_direct_routing_rules_are", "New group created with DIRECT. Routing rules are unchanged; select the group in Global mode or use a rule targeting it."))
	}
	return nil
}

func localizedproviderDetailPage(language i18n.Context, name string) *Page {
	return &Page{ID: "provider.providerDetailPage.page1", Title: menuText(language, "menu.provider.provider_detail_page.provider", "Provider: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.provider.provider_detail_page.provider_state_unknown", "Provider state: unknown"), Actions: []Item{
			{ID: "provider.providerDetailPage.item2", Label: menuText(language, "menu.provider.provider_detail_page.update_provider", "Update provider"), Args: []string{"proxy", "provider", "update", name}},
			{ID: "provider.providerDetailPage.item3", Label: menuText(language, "menu.provider.provider_detail_page.start_healthcheck", "Start healthcheck"), Args: []string{"proxy", "provider", "healthcheck", name}},
			{ID: "provider.providerDetailPage.item4", Label: menuText(language, "menu.provider.provider_detail_page.automatic_latency_checks", "Automatic latency checks"), Next: func() *Page { return localizedhealthcheckPage(language, "provider", name) }},
			{ID: "provider.providerDetailPage.item5", Label: menuText(language, "menu.provider.provider_detail_page.update_interval", "Update interval"), Next: func() *Page { return localizedproviderIntervalPage(language, name) }},
			{ID: "provider.providerDetailPage.item6", Label: menuText(language, "menu.provider.provider_detail_page.show_measurement_details", "Show measurement details"), Args: []string{"proxy", "provider", "show", name}},
			{ID: "provider.providerDetailPage.item7", Label: menuText(language, "menu.provider.provider_detail_page.delete_provider", "Delete provider"), Impact: menuText(language, "menu.provider.provider_detail_page.removes_provider_and_group_references_reloads_ru", "Removes provider and group references; reloads running mihomo"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				result := s.Run(ctx, "proxy", "provider", "remove", name)
				s.ShowResult([]string{"proxy", "provider", "remove", name}, result)
				return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
			}},
		}}
		var p menuProvider
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "provider", "show", name), &p); err != nil {
			return view, err
		}
		view.Context = menuFormat(language, "menu.provider.provider_detail_page.type_s_nodes_d", "Type: %s; nodes: %d", p.Type, len(p.Nodes))
		for _, node := range p.Nodes {
			item := Item{ID: "provider.providerDetailPage.item8", Name: node, Label: localizednodeLabel(language, node, p.NodeInfo, p.NodeDelays)}
			if localizednodeKind(language, node, p.NodeInfo) == "node" {
				item.Args = []string{"proxy", "test", "provider", name, node}
			}
			view.Items = append(view.Items, item)
		}
		return view, nil
	}}
}

func localizedproviderIntervalPage(language i18n.Context, name string) *Page {
	return &Page{ID: "provider.providerIntervalPage.page1", Title: menuText(language, "menu.provider.provider_interval_page.provider_update_interval", "Provider update interval: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var config struct {
			IntervalSeconds int    `json:"interval_seconds"`
			Source          string `json:"source"`
			CanReset        bool   `json:"can_reset"`
		}
		view := PageView{Context: menuText(language, "menu.provider.provider_interval_page.update_interval_unknown", "Update interval: unknown"), Actions: []Item{
			{ID: "provider.providerIntervalPage.item2", Label: menuText(language, "menu.provider.provider_interval_page.set_update_interval", "Set update interval"), Impact: menuText(language, "menu.provider.provider_interval_page.saves_a_profile_override_reloads_running_mihomo", "Saves a profile override; reloads running mihomo"), Action: func(ctx context.Context, s *Session) error {
				_, err := s.Form(ctx, []Field{localizedsecondsField(language, menuText(language, "menu.provider.provider_interval_page.update_interval_seconds_off_disables", "Update interval seconds (off disables)"), true)}, map[string]string{}, func(v map[string]string) Result {
					return s.Run(ctx, "proxy", "provider", "interval", "set", name, v["seconds"])
				})
				return err
			}},
			{ID: "provider.providerIntervalPage.item3", Label: menuText(language, "menu.provider.provider_interval_page.use_original_update_interval", "Use original update interval"), Args: []string{"proxy", "provider", "interval", "reset", name}},
		}}
		err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "provider", "interval", "show", name), &config)
		if err == nil {
			if !config.CanReset {
				view.Actions = view.Actions[:1]
			}
			interval := menuFormat(language, "menu.provider.provider_interval_page.d_seconds", "%d seconds", config.IntervalSeconds)
			if config.IntervalSeconds == 0 {
				interval = "off"
			}
			view.Context = menuFormat(language, "menu.provider.provider_interval_page.update_interval_s_source_s", "Update interval: %s; source: %s", interval, config.Source)
		}
		return view, err
	}}
}
func providerListPage() *Page {
	return localizedproviderListPage(i18n.New(i18n.English))
}
func providerDetailPage(name string) *Page {
	return localizedproviderDetailPage(i18n.New(i18n.English), name)
}
func providerIntervalPage(name string) *Page {
	return localizedproviderIntervalPage(i18n.New(i18n.English), name)
}
