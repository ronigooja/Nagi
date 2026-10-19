package rules

import (
	"github.com/ronigooja/Nagi/internal/i18n"
	"strings"

	"gopkg.in/yaml.v3"
)

type Conflict struct {
	Earlier int    `json:"earlier"`
	Later   int    `json:"later"`
	Reason  string `json:"reason"`
}
type Report struct {
	Order     []string   `json:"order"`
	Conflicts []Conflict `json:"conflicts"`
}

func Line(e Entry) string {
	if e.Kind == "set" {
		return "RULE-SET,nagi-" + e.Name + "," + e.Target
	}
	if e.Type == "MATCH" {
		return "MATCH," + e.Target
	}
	return e.Type + "," + e.Payload + "," + e.Target
}
func Apply(base []byte, dir, profile string) ([]byte, error) {
	doc, err := New(dir, profile, nil).List()
	if err != nil {
		return nil, err
	}
	if len(doc.Entries) == 0 {
		return base, nil
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(base, &cfg); err != nil {
		return nil, i18n.Errorf("backend.rules.cannot_merge_custom_rules_with_invalid_profile_yaml", "cannot merge custom rules with invalid profile YAML")
	}
	if cfg == nil {
		return nil, i18n.Errorf("backend.rules.profile_must_be_a_yaml_mapping", "profile must be a YAML mapping")
	}
	existing, ok := cfg["rules"].([]any)
	if !ok && cfg["rules"] != nil {
		return nil, i18n.Errorf("backend.rules.profile_rules_must_be_a_yaml_list", "profile rules must be a YAML list")
	}
	ordered := make([]any, 0, len(doc.Entries)+len(existing))
	providers, _ := cfg["rule-providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	for _, e := range doc.Entries {
		if err := Validate(e); err != nil {
			return nil, i18n.Errorf("backend.rules.rule_s_w", "rule %s: %w", e.Name, err)
		}
		if !e.Enabled {
			continue
		}
		if e.Kind == "set" {
			name := "nagi-" + e.Name
			if _, exists := providers[name]; exists {
				return nil, i18n.Errorf("backend.rules.rule_provider_s_conflicts_with_selected_profile", "rule provider %s conflicts with selected profile", name)
			}
			providers[name] = map[string]any{"type": "inline", "behavior": e.Behavior, "payload": e.Items}
		}
		if e.Kind == "rules" {
			for _, line := range e.Items {
				ordered = append(ordered, line)
			}
		} else {
			ordered = append(ordered, Line(e))
		}
	}
	ordered = append(ordered, existing...)
	cfg["rules"] = ordered
	cfg["rule-providers"] = providers
	return yaml.Marshal(cfg)
}
func Analyze(base []byte, dir, profile string) (Report, error) {
	var r Report
	data, err := Apply(base, dir, profile)
	if err != nil {
		return r, err
	}
	return AnalyzeEffective(data)
}
func AnalyzeEffective(data []byte) (Report, error) {
	var r Report
	var cfg struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return r, err
	}
	r.Order = cfg.Rules
	r.Conflicts = []Conflict{}
	seen := map[string]int{}
	matchIndex := -1
	for i, line := range r.Order {
		parts := strings.Split(line, ",")
		key := line
		if len(parts) >= 2 {
			key = parts[0] + "," + parts[1]
		}
		if earlier, ok := seen[key]; ok {
			r.Conflicts = append(r.Conflicts, Conflict{earlier, i, "same rule matcher appears earlier"})
		} else {
			seen[key] = i
		}
		if matchIndex >= 0 && i > matchIndex {
			r.Conflicts = append(r.Conflicts, Conflict{matchIndex, i, "earlier MATCH rule shadows this rule"})
		}
		if len(parts) > 0 && parts[0] == "MATCH" && matchIndex < 0 {
			matchIndex = i
		}
	}
	return r, nil
}

// Targets returns proxy references for subscription preflight. Field positions
// follow the pinned mihomo ParseRulePayload grammar; actual validation remains
// mihomo's responsibility. SUB-RULE references are in a separate namespace.
func Targets(e Entry) []string {
	if e.Kind != "rules" {
		return []string{e.Target}
	}
	var targets []string
	for _, line := range e.Items {
		parts := strings.Split(line, ",")
		for i := range parts {
			parts[i] = strings.Trim(parts[i], " ")
		}
		var target string
		switch strings.ToUpper(parts[0]) {
		case "SUB-RULE":
			continue
		case "MATCH":
			if len(parts) > 1 {
				target = parts[1]
			}
		case "NOT", "OR", "AND", "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
			if len(parts) > 1 {
				target = parts[len(parts)-1]
			}
		default:
			if len(parts) > 2 {
				target = parts[2]
			}
		}
		if target != "" {
			targets = append(targets, target)
		}
	}
	return targets
}
