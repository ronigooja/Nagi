package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/ronigooja/Nagi/internal/rules"
)

func init() { RegisterPage("rules", RulesPage) }

func localizedRulesPage(language i18n.Context) *Page {
	return &Page{ID: "rules.RulesPage.page1", Title: menuText(language, "menu.rules.rules_page.rules", "Rules"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, "rules", "custom")
		summary := menuText(language, "menu.rules.rules_page.custom_rules_and_sets_unknown", "Custom rules and sets: unknown")
		if result.Error != nil {
			summary += " — " + result.errorText(language)
		} else if result.Code == 0 {
			object := localizedmenuObject(language, result.Data)
			var entries []json.RawMessage
			if json.Unmarshal(object["entries"], &entries) == nil {
				summary = menuFormat(language, "menu.rules.rules_page.custom_rules_and_sets_d", "Custom rules and sets: %d", len(entries))
				var profile string
				if json.Unmarshal(object["profile"], &profile) == nil && profile != "" {
					summary = menuText(language, "menu.rules.rules_page.selected_profile", "Selected profile: ") + profile + "\n" + summary
				}
			}
		}
		view := PageView{Context: summary, Actions: []Item{
			{ID: "rules.RulesPage.item2", Label: menuText(language, "menu.rules.rules_page.test_rule", "Test Rule"), Action: ruleTest},
			localizedsubmenu(language, localizedactionPage(language, menuText(language, "menu.rules.rules_page.view_rules", "View Rules"), Item{ID: "rules.RulesPage.item3", Label: menuText(language, "menu.rules.rules_page.list_active_rules", "List active rules"), Args: []string{"rules", "list"}},
				Item{ID: "rules.RulesPage.item4", Label: menuText(language, "menu.rules.rules_page.list_active_rule_providers", "List active rule providers"), Args: []string{"rules", "providers"}},
				Item{ID: "rules.RulesPage.item5", Label: menuText(language, "menu.rules.rules_page.list_custom_rules_and_sets", "List custom rules and sets"), Args: []string{"rules", "custom"}},
				Item{ID: "rules.RulesPage.item6", Label: menuText(language, "menu.rules.rules_page.show_rule_order_and_conflicts", "Show rule order and conflicts"), Args: []string{"rules", "conflicts"}},
				Item{ID: "rules.RulesPage.item7", Label: menuText(language, "menu.rules.rules_page.show_rule_matched_by_a_connection", "Show rule matched by a connection"), Action: ruleConnection})),
			localizedsubmenu(language, localizedactionPage(language, menuText(language, "menu.rules.rules_page.manage_rules_and_sets", "Manage Rules and Sets"), Item{ID: "rules.RulesPage.item8", Label: menuText(language, "menu.rules.rules_page.add_custom_rule", "Add custom rule"), Impact: menuText(language, "menu.rules.rules_page.saves_to_the_selected_profile_reloads_if_running", "Saves to the selected profile; reloads if running"), Action: ruleAdd},
				Item{ID: "rules.RulesPage.item9", Label: menuText(language, "menu.rules.rules_page.enable_custom_rule_or_set", "Enable custom rule or set"), Action: localizedruleChange(language, "enable")},
				Item{ID: "rules.RulesPage.item10", Label: menuText(language, "menu.rules.rules_page.disable_custom_rule_or_set", "Disable custom rule or set"), Action: localizedruleChange(language, "disable")},
				Item{ID: "rules.RulesPage.item11", Label: menuText(language, "menu.rules.rules_page.remove_custom_rule_or_set", "Remove custom rule or set"), Impact: menuText(language, "menu.rules.rules_page.removes_the_whole_named_object_from_the_selected", "Removes the whole named object from the selected profile; reloads if running"), Action: localizedruleChange(language, "remove")})),
			{ID: "rules.RulesPage.item12", Label: menuText(language, "menu.rules.rules_page.import_rules", "Import rules"), Impact: menuText(language, "menu.rules.rules_page.reads_a_file_or_downloads_a_snapshot_into_the_se", "Reads a file or downloads a snapshot into the selected profile; reloads if running"), Action: ruleImport},
		}}
		return view, nil
	}}
}

func ruleTest(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{
		{Key: "target", Label: menuText(s.language, "menu.rules.rule_test.domain_or_ip_address_tcp_port_443", "Domain or IP address (TCP port 443)"), Required: true, Validate: func(v string) error {
			req := rules.TestRequest{Target: v}
			return req.Validate()
		}},
	}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "rules", "test", v["target"])
	})
	return err
}

// menuObject decodes CLI JSON data, including typed Go values supplied by fake runners.
func localizedmenuObject(language i18n.Context, value any) map[string]json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(encoded, &object) != nil {
		return nil
	}
	return object
}

func localizedmenuNames(language i18n.Context, value any, field string) []string {
	object := localizedmenuObject(language, value)
	var entries []map[string]json.RawMessage
	if json.Unmarshal(object[field], &entries) != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		var name string
		key := "name"
		if field == "connections" {
			key = "id"
		}
		if json.Unmarshal(entry[key], &name) == nil && name != "" {
			names = append(names, name)
		}
	}
	return names
}

func menuSelectName(ctx context.Context, s *Session, command []string, field, label string) (string, error) {
	result := s.Fetch(ctx, command...)
	if result.Code != 0 || result.Error != nil {
		s.ShowResult(command, result)
		return "", nil
	}
	names := localizedmenuNames(s.language,
		result.Data, field)
	if len(names) == 0 {
		if field == "connections" {
			s.feedback(feedbackWarning, menuText(s.language, "menu.rules.menu_select_name.no_active_connections_returned_start_mihomo_and", "No active connections returned; start mihomo and run `nagi connections list`."))
		} else {
			s.feedback(feedbackWarning, menuText(s.language, "menu.rules.menu_select_name.no_custom_rules_or_sets_returned_run_nagi_rules", "No custom rules or sets returned; run `nagi rules custom`."))
		}
		return "", nil
	}
	return s.Select(label, names)
}

func ruleConnection(ctx context.Context, s *Session) error {
	id, err := menuSelectName(ctx, s, []string{"connections", "list"}, "connections", menuText(s.language, "menu.rules.rule_connection.connection_id", "Connection ID"))
	if err != nil {
		return err
	}
	if id != "" {
		s.Run(ctx, "rules", "connection", id)
	}
	return nil
}

func ruleAdd(ctx context.Context, s *Session) error {
	values := map[string]string{}
	fields := []Field{
		{Key: "name", Label: menuText(s.language, "menu.rules.rule_add.rule_name", "Rule name"), Required: true, Validate: func(v string) error {
			return localizedvalidateMenuRuleName(s.language, v)
		},
		},
		{Key: "type", Label: menuText(s.language, "menu.rules.rule_add.rule_type", "Rule type"), Choices: []string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "IP-CIDR", "IP-CIDR6", "GEOIP", "GEOSITE", "PROCESS-NAME", "MATCH"}, ChoiceLabels: []string{menuState(s.language, "DOMAIN"), menuState(s.language, "DOMAIN-SUFFIX"), menuState(s.language, "DOMAIN-KEYWORD"), menuState(s.language, "DOMAIN-REGEX"), menuState(s.language, "IP-CIDR"), menuState(s.language, "IP-CIDR6"), menuState(s.language, "GEOIP"), menuState(s.language, "GEOSITE"), menuState(s.language, "PROCESS-NAME"), menuState(s.language, "MATCH")}},
		{Key: "payload", Label: menuText(s.language, "menu.rules.rule_add.rule_payload_blank_for_match", "Rule payload (blank for MATCH)"), Validate: func(v string) error {
			if values["type"] != "MATCH" && v == "" {
				return fmt.Errorf("%s", menuText(s.language, "menu.rules.rule_add.a_payload_is_required_for_this_rule_type", "a payload is required for this rule type"))
			}
			return nil
		}},
		{Key: "target", Label: menuText(s.language, "menu.rules.rule_add.target_direct_reject_or_proxy_group", "Target (DIRECT, REJECT, or proxy group)"), Required: true},
	}
	_, err := s.Form(ctx, fields, values, func(v map[string]string) Result {
		payload := v["payload"]
		if v["type"] == "MATCH" {
			payload = "-"
		}
		return s.Run(ctx, "rules", "add", v["name"], v["type"], payload, v["target"])
	})
	return err
}

func localizedruleChange(language i18n.Context, verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		name, err := menuSelectName(ctx, s, []string{"rules", "custom"}, "entries", menuText(language, "menu.rules.rule_change.custom_rule_or_set", "Custom rule or set"))
		if err != nil {
			return err
		}
		if name != "" {
			s.Run(ctx, "rules", verb, name)
		}
		return nil
	}
}

func ruleImport(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{
		{Key: "name", Label: menuText(s.language, "menu.rules.rule_import.import_name", "Import name"), Required: true, Validate: func(v string) error {
			return localizedvalidateMenuRuleName(s.language, v)
		},
		},
		{Key: "source", Label: menuText(s.language, "menu.rules.rule_import.local_file_path_or_http_s_url", "Local file path or HTTP(S) URL"), Required: true},
	}, nil, func(v map[string]string) Result {
		return s.Run(ctx, "rules", "import", v["name"], v["source"])
	})
	return err
}

var menuRuleNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func localizedvalidateMenuRuleName(language i18n.Context, v string) error {
	if !menuRuleNamePattern.MatchString(v) {
		return fmt.Errorf("%s", menuText(language, "menu.rules.validate_menu_rule_name.use_1_128_letters_digits_underscores_or_hyphens", "use 1-128 letters, digits, underscores or hyphens"))
	}
	return nil
}
func RulesPage() *Page {
	return localizedRulesPage(i18n.New(i18n.English))
}

func menuObject(value any) map[string]json.RawMessage {
	return localizedmenuObject(i18n.New(i18n.English), value)
}
func menuNames(value any, field string) []string {
	return localizedmenuNames(i18n.New(i18n.English), value, field)
}
func ruleChange(verb string) func(context.
	Context, *Session) error {
	return localizedruleChange(i18n.New(i18n.English), verb)
}
func validateMenuRuleName(v string) error {
	return localizedvalidateMenuRuleName(i18n.New(i18n.English), v)
}
