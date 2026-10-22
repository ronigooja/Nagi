package menu

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/rules"
)

type managedRules struct {
	Profile string        `json:"profile"`
	Entries []rules.Entry `json:"entries"`
}

func fetchManagedRules(ctx context.Context, s *Session, profile string) (managedRules, error) {
	var data managedRules
	err := decodeMenuResult(s.Fetch(ctx, "rules", "custom"), &data)
	if err != nil {
		return data, err
	}
	if data.Profile == "" || data.Entries == nil || (profile != "" && profile != data.Profile) {
		return data, fmt.Errorf("%s", menuText(s.language, "menu.rules.manage.invalid", "Rules unavailable or selected profile changed. Return to Manage Rules and Sets."))
	}
	return data, nil
}

func ruleKind(language i18n.Context, kind string) string {
	switch kind {
	case "rule":
		return menuText(language, "menu.rules.manage.single", "Rule")
	case "rules":
		return menuText(language, "menu.rules.manage.bundle", "Imported rules")
	case "set":
		return menuText(language, "menu.rules.manage.set", "Set")
	}
	return kind
}

func ruleEnabled(language i18n.Context, enabled bool) string {
	if enabled {
		return menuText(language, "menu.rules.manage.enabled", "Enabled")
	}
	return menuText(language, "menu.rules.manage.disabled", "Disabled")
}

func localizedRuleManagementPage(language i18n.Context) *Page {
	return &Page{ID: "rules.manage", Title: menuText(language, "menu.rules.rules_page.manage_rules_and_sets", "Manage Rules and Sets"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: menuText(language, "menu.rules.manage.empty", "No custom rules or sets."), Actions: []Item{
			{ID: "rules.manage.add", Label: menuText(language, "menu.rules.rules_page.add_custom_rule", "Add custom rule"), Action: ruleAdd},
			{ID: "rules.manage.import", Label: menuText(language, "menu.rules.rules_page.import_rules", "Import rules"), Action: ruleImport},
		}}
		data, err := fetchManagedRules(ctx, s, "")
		if err != nil {
			return v, err
		}
		v.Context = menuText(language, "menu.rules.rules_page.selected_profile", "Selected profile: ") + data.Profile
		for _, entry := range data.Entries {
			name := entry.Name
			v.Items = append(v.Items, Item{ID: "rules.manage.entry", Name: name, Label: name + " [" + ruleEnabled(language, entry.Enabled) + " · " + ruleKind(language, entry.Kind) + "]", Next: func() *Page { return localizedRuleDetailPage(language, data.Profile, name) }})
		}
		return v, nil
	}}
}

func managedRule(ctx context.Context, s *Session, profile, name string) (rules.Entry, error) {
	data, err := fetchManagedRules(ctx, s, profile)
	if err != nil {
		return rules.Entry{}, err
	}
	for _, e := range data.Entries {
		if e.Name == name {
			return e, nil
		}
	}
	return rules.Entry{}, fmt.Errorf("%s", menuText(s.language, "menu.rules.manage.missing", "This rule or set no longer exists. Return to Manage Rules and Sets."))
}

// Re-read ownership before dispatch, including each explicit form retry.
func runManagedRule(ctx context.Context, s *Session, profile, name string, args ...string) Result {
	if _, err := managedRule(ctx, s, profile, name); err != nil {
		result := Result{Code: 1, Error: &CLIError{Message: err.Error()}}
		s.ShowResult(nil, result)
		return result
	}
	return s.Run(ctx, args...)
}

func localizedRuleDetailPage(language i18n.Context, profile, name string) *Page {
	return &Page{ID: "rules.detail/" + name, Title: name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{}
		e, err := managedRule(ctx, s, profile, name)
		if err != nil {
			return v, err
		}
		v.Context = menuText(language, "menu.rules.rules_page.selected_profile", "Selected profile: ") + profile + "\n" +
			menuText(language, "menu.rules.manage.status", "Status: ") + ruleEnabled(language, e.Enabled) + "\n" +
			menuText(language, "menu.rules.manage.type", "Type: ") + ruleKind(language, e.Kind)
		if e.Kind == "rule" {
			v.Context += " (" + e.Type + ")"
		}
		if e.Kind == "set" {
			v.Context += " (" + e.Behavior + ")"
		}
		if e.Target != "" {
			v.Context += "\n" + menuText(language, "menu.rules.manage.target", "Target: ") + e.Target
		}
		count := len(e.Items)
		if e.Kind == "rule" {
			count = 1
		}
		v.Context += "\n" + menuText(language, "menu.rules.manage.count", "Rules: ") + strconv.Itoa(count)
		if e.Source != "" {
			v.Context += "\n" + menuText(language, "menu.rules.manage.source", "Source: ") + e.Source
		}
		v.Actions = append(v.Actions, Item{ID: "rules.detail.view", Label: menuText(language, "menu.rules.manage.view", "View rules"), Next: func() *Page { return localizedRuleContentsPage(language, profile, name) }})
		if e.Kind == "rule" {
			v.Actions = append(v.Actions, Item{ID: "rules.detail.edit", Label: menuText(language, "menu.rules.manage.edit", "Edit rule"), Action: func(ctx context.Context, s *Session) error {
				current, err := managedRule(ctx, s, profile, name)
				if err != nil {
					return err
				}
				return ruleEditForm(ctx, s, profile, current)
			}})
		} else {
			if e.CanUpdate {
				v.Actions = append(v.Actions, Item{ID: "rules.detail.update", Label: menuText(language, "menu.rules.manage.update", "Update from source"), Action: func(ctx context.Context, s *Session) error {
					// Use a retained form on failure so retry stays an explicit choice.
					_, err := s.Form(ctx, nil, nil, func(map[string]string) Result { return runManagedRule(ctx, s, profile, name, "rules", "update", name) })
					return err
				}})
			}
			label := menuText(language, "menu.rules.manage.change_source", "Change source…")
			if !e.CanUpdate {
				label = menuText(language, "menu.rules.manage.set_source", "Set update source…")
			}
			v.Actions = append(v.Actions, Item{ID: "rules.detail.source", Label: label, Action: func(ctx context.Context, s *Session) error {
				_, err := s.Form(ctx, []Field{{Key: "source", Label: menuText(language, "menu.rules.rule_import.local_file_path_or_http_s_url", "Local file path or HTTP(S) URL"), Required: true}}, nil, func(values map[string]string) Result {
					return runManagedRule(ctx, s, profile, name, "rules", "update", name, values["source"])
				})
				return err
			}})
		}
		verb, label := "enable", menuText(language, "menu.rules.manage.enable", "Enable")
		if e.Enabled {
			verb, label = "disable", menuText(language, "menu.rules.manage.disable", "Disable")
		}
		v.Actions = append(v.Actions,
			Item{ID: "rules.detail.toggle", Label: label, Action: func(ctx context.Context, s *Session) error {
				runManagedRule(ctx, s, profile, name, "rules", verb, name)
				return nil
			}},
			Item{ID: "rules.detail.remove", Label: menuText(language, "menu.rules.manage.remove", "Remove"), Impact: menuText(language, "menu.rules.manage.remove_count", "All rules: ") + strconv.Itoa(count), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				result := runManagedRule(ctx, s, profile, name, "rules", "remove", name)
				return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
			}},
		)
		return v, nil
	}}
}

// Contents are paginated in source order. Selecting a line explicitly opens its
// complete raw value, which may contain sensitive information.
func localizedRuleContentsPage(language i18n.Context, profile, name string) *Page {
	return &Page{ID: "rules.contents/" + name, Title: menuText(language, "menu.rules.manage.view", "View rules") + " — " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{}
		e, err := managedRule(ctx, s, profile, name)
		if err != nil {
			return v, err
		}
		lines := e.Items
		if e.Kind == "rule" {
			lines = []string{rules.Line(e)}
		}
		for _, line := range lines {
			value := line
			v.Items = append(v.Items, Item{ID: "rules.contents.line", Label: value, Next: func() *Page {
				return &Page{ID: "rules.contents.value", Title: name, Load: func(context.Context, *Session) (PageView, error) {
					return PageView{ContextRaw: true, Context: value}, nil
				}}
			}})
		}
		return v, nil
	}}
}
