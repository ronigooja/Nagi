package menu

import (
	"context"
	"encoding/json"
	"strings"
)

func init() { RegisterPage("rules", RulesPage) }

func RulesPage() *Page {
	return &Page{Title: "Rules", Help: []string{"rules"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Items: []Item{
			{Label: "List active rules", Args: []string{"rules", "list"}},
			{Label: "List active rule providers", Args: []string{"rules", "providers"}},
			{Label: "List custom rules and sets", Args: []string{"rules", "custom"}},
			{Label: "Show rule order and conflicts", Args: []string{"rules", "conflicts"}},
			{Label: "Show rule matched by a connection", Action: ruleConnection},
			{Label: "Add custom rule", Action: ruleAdd},
			{Label: "Remove custom rule or set", Action: ruleChange("remove")},
			{Label: "Enable custom rule or set", Action: ruleChange("enable")},
			{Label: "Disable custom rule or set", Action: ruleChange("disable")},
			{Label: "Import local rule set", Action: ruleImport("import-local")},
			{Label: "Import remote rule set", Action: ruleImport("import-remote")},
		}}, nil
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
	if result.Code != 0 {
		s.showResult(result)
		return "", nil
	}
	names := menuNames(result.Data, field)
	if len(names) == 0 {
		if field == "connections" {
			s.WriteLine("No active connections returned; start mihomo and run `nagi connections list`.")
		} else {
			s.WriteLine("No custom rules or sets returned; run `nagi rules custom`.")
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
	name, err := s.Prompt("Rule name")
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return nil
	}
	typ, err := s.Select("Rule type", []string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "IP-CIDR", "IP-CIDR6", "GEOIP", "GEOSITE", "PROCESS-NAME", "MATCH"})
	if err != nil {
		return err
	}
	payload := "-"
	if typ != "MATCH" {
		payload, err = s.Prompt("Rule payload")
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload) == "" {
			return nil
		}
	}
	target, err := s.Prompt("Target (DIRECT, REJECT, or proxy group)")
	if err != nil {
		return err
	}
	if strings.TrimSpace(target) == "" {
		return nil
	}
	ok, err := s.Confirm("Save and activate custom rule " + name + "?")
	if err != nil {
		return err
	}
	if ok {
		s.Run(ctx, "rules", "add", name, typ, payload, target)
	}
	return nil
}

func ruleChange(verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		name, err := menuSelectName(ctx, s, []string{"rules", "custom"}, "entries", "Custom rule or set")
		if err != nil {
			return err
		}
		if name == "" {
			return nil
		}
		ok, err := s.Confirm(strings.ToUpper(verb[:1]) + verb[1:] + " rule or set " + name + "?")
		if err != nil {
			return err
		}
		if ok {
			s.Run(ctx, "rules", verb, name)
		}
		return nil
	}
}

func ruleImport(verb string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		name, err := s.Prompt("Rule set name")
		if err != nil {
			return err
		}
		if strings.TrimSpace(name) == "" {
			return nil
		}
		behavior, err := s.Select("Rule set behavior", []string{"classical", "domain", "ipcidr"})
		if err != nil {
			return err
		}
		label := "Local rule set file"
		if verb == "import-remote" {
			label = "Remote HTTP(S) rule set URL"
		}
		source, err := s.Prompt(label)
		if err != nil {
			return err
		}
		if strings.TrimSpace(source) == "" {
			return nil
		}
		target, err := s.Prompt("Target (DIRECT, REJECT, or proxy group)")
		if err != nil {
			return err
		}
		if strings.TrimSpace(target) == "" {
			return nil
		}
		ok, err := s.Confirm("Import and activate rule set " + name + "?")
		if err != nil {
			return err
		}
		if ok {
			s.Run(ctx, "rules", verb, name, behavior, source, target)
		}
		return nil
	}
}
