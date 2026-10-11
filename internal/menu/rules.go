package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

func init() { RegisterPage("rules", RulesPage) }

func RulesPage() *Page {
	return &Page{Title: "Rules", Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, "rules", "custom")
		summary := "Custom rules: unknown"
		if result.Error != nil {
			summary += " — " + result.Error.Message
		} else if result.Code == 0 {
			object := menuObject(result.Data)
			var entries []json.RawMessage
			if json.Unmarshal(object["entries"], &entries) == nil {
				summary = fmt.Sprintf("Custom rules and sets: %d", len(entries))
			}
		}
		view := PageView{Context: summary, Actions: []Item{
			submenu(actionPage("View Rules", Item{Label: "List active rules", Args: []string{"rules", "list"}},
				Item{Label: "List active rule providers", Args: []string{"rules", "providers"}},
				Item{Label: "List custom rules and sets", Args: []string{"rules", "custom"}},
				Item{Label: "Show rule order and conflicts", Args: []string{"rules", "conflicts"}},
				Item{Label: "Show rule matched by a connection", Action: ruleConnection})),
			submenu(actionPage("Manage Custom Rules", Item{Label: "Add custom rule", Impact: "Saves and activates the rule", Action: ruleAdd},
				Item{Label: "Enable custom rule or set", Action: ruleChange("enable")},
				Item{Label: "Disable custom rule or set", Action: ruleChange("disable")},
				Item{Label: "Remove custom rule or set", Impact: "Removes and activates the change", Action: ruleChange("remove")})),
			submenu(actionPage("Import Rule Sets", Item{Label: "Import local rule set", Impact: "Saves and activates the set", Action: ruleImport("import-local")},
				Item{Label: "Import remote rule set", Impact: "Downloads, saves and activates the set", Action: ruleImport("import-remote")})),
		}}
		return view, nil
	}}
}

// menuObject decodes CLI JSON data, including typed Go values supplied by fake runners.
func menuObject(value any) map[string]json.RawMessage {
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

func menuNames(value any, field string) []string {
	object := menuObject(value)
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
	names := menuNames(result.Data, field)
	if len(names) == 0 {
		if field == "connections" {
			s.feedback(feedbackWarning, "No active connections returned; start mihomo and run `nagi connections list`.")
		} else {
			s.feedback(feedbackWarning, "No custom rules or sets returned; run `nagi rules custom`.")
		}
		return "", nil
	}
	return s.Select(label, names)
}

func ruleConnection(ctx context.Context, s *Session) error {
	id, err := menuSelectName(ctx, s, []string{"connections", "list"}, "connections", "Connection ID")
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
		{Key: "name", Label: "Rule name", Required: true, Validate: validateMenuRuleName},
		{Key: "type", Label: "Rule type", Choices: []string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "IP-CIDR", "IP-CIDR6", "GEOIP", "GEOSITE", "PROCESS-NAME", "MATCH"}},
		{Key: "payload", Label: "Rule payload (blank for MATCH)", Validate: func(v string) error {
			if values["type"] != "MATCH" && v == "" {
				return fmt.Errorf("a payload is required for this rule type")
			}
			return nil
		}},
		{Key: "target", Label: "Target (DIRECT, REJECT, or proxy group)", Required: true},
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

func ruleChange(verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		name, err := menuSelectName(ctx, s, []string{"rules", "custom"}, "entries", "Custom rule or set")
		if err != nil {
			return err
		}
		if name != "" {
			s.Run(ctx, "rules", verb, name)
		}
		return nil
	}
}

func ruleImport(verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		label := "Local rule set file"
		if verb == "import-remote" {
			label = "Remote HTTP(S) rule set URL"
		}
		_, err := s.Form(ctx, []Field{
			{Key: "name", Label: "Rule set name", Required: true, Validate: validateMenuRuleName},
			{Key: "behavior", Label: "Rule set behavior", Choices: []string{"classical", "domain", "ipcidr"}},
			{Key: "source", Label: label, Required: true},
			{Key: "target", Label: "Target (DIRECT, REJECT, or proxy group)", Required: true},
		}, nil, func(v map[string]string) Result {
			return s.Run(ctx, "rules", verb, v["name"], v["behavior"], v["source"], v["target"])
		})
		return err
	}
}

var menuRuleNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validateMenuRuleName(v string) error {
	if !menuRuleNamePattern.MatchString(v) {
		return fmt.Errorf("use 1-128 letters, digits, underscores or hyphens")
	}
	return nil
}
