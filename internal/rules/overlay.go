package rules

import (
	"errors"
	"fmt"
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
func Apply(base []byte, dir string) ([]byte, error) {
	doc, err := New(dir, nil).List()
	if err != nil {
		return nil, err
	}
	if len(doc.Entries) == 0 {
		return base, nil
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(base, &cfg); err != nil {
		return nil, errors.New("cannot merge custom rules with invalid profile YAML")
	}
	if cfg == nil {
		return nil, errors.New("profile must be a YAML mapping")
	}
	existing, ok := cfg["rules"].([]any)
	if !ok && cfg["rules"] != nil {
		return nil, errors.New("profile rules must be a YAML list")
	}
	ordered := make([]any, 0, len(doc.Entries)+len(existing))
	providers, _ := cfg["rule-providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	for _, e := range doc.Entries {
		if err := Validate(e); err != nil {
			return nil, fmt.Errorf("rule %s: %w", e.Name, err)
		}
		if !e.Enabled {
			continue
		}
		if e.Kind == "set" {
			name := "nagi-" + e.Name
			if _, exists := providers[name]; exists {
				return nil, fmt.Errorf("rule provider %s conflicts with selected profile", name)
			}
			providers[name] = map[string]any{"type": "inline", "behavior": e.Behavior, "payload": e.Items}
		}
		ordered = append(ordered, Line(e))
	}
	ordered = append(ordered, existing...)
	cfg["rules"] = ordered
	cfg["rule-providers"] = providers
	return yaml.Marshal(cfg)
}
func Analyze(base []byte, dir string) (Report, error) {
	var r Report
	data, err := Apply(base, dir)
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
