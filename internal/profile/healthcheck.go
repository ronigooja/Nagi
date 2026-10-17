package profile

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"reflect"
	"sort"

	"gopkg.in/yaml.v3"
)

type Healthcheck struct {
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Profile           string `json:"profile"`
	IntervalSeconds   int    `json:"interval_seconds"`
	Enabled           *bool  `json:"enabled"`
	Lazy              bool   `json:"lazy"`
	Source            string `json:"source"`
	TestURL           string `json:"test_url,omitempty"`
	Reason            string `json:"reason,omitempty"`
	IntervalDefaulted bool   `json:"interval_defaulted"`
}

func healthTarget(m map[string]any, kind, name string) (map[string]any, error) {
	if kind == "provider" {
		providers, _ := m["proxy-providers"].(map[string]any)
		p, _ := providers[name].(map[string]any)
		if p == nil {
			return nil, errors.New("provider not found in selected profile")
		}
		return p, nil
	}
	if kind != "group" {
		return nil, errors.New("KIND must be group or provider")
	}
	_, groups, err := namedList(m["proxy-groups"])
	if err != nil {
		return nil, err
	}
	if groups[name] == nil {
		return nil, errors.New("group not found in selected profile")
	}
	return groups[name], nil
}
func healthFields(target map[string]any, kind string) map[string]any {
	if kind == "group" {
		return target
	}
	h, _ := target["health-check"].(map[string]any)
	return h
}
func (s *Store) healthMaps(name string) (map[string]any, map[string]any, map[string]any, error) {
	data, err := s.Effective(name)
	if err != nil {
		return nil, nil, nil, err
	}
	var effective, source, overlay map[string]any
	if yaml.Unmarshal(data, &effective) != nil {
		return nil, nil, nil, errors.New("invalid effective YAML")
	}
	data, err = s.Show(name)
	if err != nil {
		return nil, nil, nil, err
	}
	if yaml.Unmarshal(data, &source) != nil {
		return nil, nil, nil, errors.New("invalid source YAML")
	}
	data, err = s.Override(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil, err
	}
	overlay = map[string]any{}
	if err == nil && (yaml.Unmarshal(data, &overlay) != nil || overlay == nil) {
		return nil, nil, nil, errors.New("invalid override YAML")
	}
	return effective, source, overlay, nil
}
func (s *Store) Healthcheck(name, kind, targetName string) (Healthcheck, error) {
	r := Healthcheck{Kind: kind, Name: targetName, Profile: name, Source: "profile", Lazy: true}
	effective, source, overlay, err := s.healthMaps(name)
	if err != nil {
		return r, err
	}
	target, err := healthTarget(effective, kind, targetName)
	if err != nil {
		return r, err
	}
	fields := healthFields(target, kind)
	if kind == "group" {
		switch target["type"] {
		case "select", "url-test", "fallback", "load-balance":
		default:
			return r, errors.New("unsupported group type")
		}
	}
	r.IntervalSeconds, _ = fields["interval"].(int)
	r.IntervalDefaulted = r.IntervalSeconds == 0
	if lazy, ok := fields["lazy"].(bool); ok {
		r.Lazy = lazy
	}
	enabled := false
	if kind == "provider" {
		enabled, _ = fields["enable"].(bool)
		if enabled && r.IntervalSeconds == 0 {
			r.IntervalSeconds = 300
		}
		r.Reason = "Provider scheduling can also be enabled by groups registering a different test URL."
	} else {
		enabled = r.IntervalSeconds > 0
		if nodes, _ := target["proxies"].([]any); len(nodes) > 0 && target["type"] != "select" && r.IntervalSeconds == 0 {
			r.IntervalSeconds = 300
			enabled = true
		}
		r.Reason = "Group interval controls explicit nodes; provider nodes use their provider scheduler, whose nonzero interval takes precedence."
	}
	r.Enabled = &enabled
	if kind == "group" {
		if nodes, _ := target["proxies"].([]any); len(nodes) == 0 {
			r.Enabled = nil
		}
	}
	if r.IntervalDefaulted {
		r.Reason += " Interval is omitted or zero; mihomo defaults apply."
	}
	local, _ := healthTarget(overlay, kind, targetName)
	localFields := healthFields(local, kind)
	if value, ok := localFields["interval"]; ok {
		r.Source = "override"
		if kind == "group" {
			original, _ := healthTarget(source, kind, targetName)
			if reflect.DeepEqual(value, original["interval"]) {
				r.Source = "profile"
			}
		}
	}
	raw, _ := fields["url"].(string)
	if kind == "group" && raw == "" {
		uses, _ := target["use"].([]any)
		providers, _ := effective["proxy-providers"].(map[string]any)
		for _, v := range uses {
			key, _ := v.(string)
			provider, _ := providers[key].(map[string]any)
			check, _ := provider["health-check"].(map[string]any)
			raw, _ = check["url"].(string)
			if raw != "" {
				break
			}
		}
		if raw == "" {
			raw = "https://www.gstatic.com/generate_204"
		}
	}
	u, e := url.Parse(raw)
	if e == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		r.TestURL = u.Scheme + "://" + u.Host
	}
	return r, nil
}

// SetHealthcheck changes only the interval; nil restores the source interval.
func (s *Store) SetHealthcheck(ctx context.Context, name, kind, targetName string, seconds *int) error {
	if seconds != nil && (*seconds < 1 || *seconds > 31536000) {
		return errors.New("SECONDS must be an integer from 1 to 31536000")
	}
	effective, source, overlay, err := s.healthMaps(name)
	if err != nil {
		return err
	}
	target, err := healthTarget(effective, kind, targetName)
	if err != nil {
		return err
	}
	if _, err := s.Healthcheck(name, kind, targetName); err != nil {
		return err
	}
	var local map[string]any
	if kind == "group" {
		groups, _, err := namedList(effective["proxy-groups"])
		if err != nil {
			return err
		}
		overlay["proxy-groups"] = groups
		local = target
	} else {
		providers, _ := overlay["proxy-providers"].(map[string]any)
		if providers == nil {
			providers = map[string]any{}
			overlay["proxy-providers"] = providers
		}
		local, _ = providers[targetName].(map[string]any)
		if local == nil {
			local = map[string]any{}
			providers[targetName] = local
		}
		check, _ := local["health-check"].(map[string]any)
		if check == nil {
			check = map[string]any{}
			local["health-check"] = check
		}
		local = check
	}
	if seconds != nil {
		local["interval"] = *seconds
	} else {
		delete(local, "interval")
		if kind == "group" {
			original, _ := healthTarget(source, kind, targetName)
			if value, ok := original["interval"]; ok {
				local["interval"] = value
			}
		}
	}
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	return s.SetOverride(ctx, name, data)
}

// HealthcheckTargets lists names from local effective configuration, without API access.
func (s *Store) HealthcheckTargets(name, kind string) ([]string, error) {
	effective, _, _, err := s.healthMaps(name)
	if err != nil {
		return nil, err
	}
	names := []string{}
	switch kind {
	case "provider":
		providers, _ := effective["proxy-providers"].(map[string]any)
		for name := range providers {
			names = append(names, name)
		}
	case "group":
		_, groups, err := namedList(effective["proxy-groups"])
		if err != nil {
			return nil, err
		}
		for name, g := range groups {
			switch g["type"] {
			case "select", "url-test", "fallback", "load-balance":
				names = append(names, name)
			}
		}
	default:
		return nil, errors.New("KIND must be group or provider")
	}
	sort.Strings(names)
	return names, nil
}
