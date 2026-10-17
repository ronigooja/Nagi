package profile

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// groupHealthcheckIntervalsKey stores explicit interval intent independently of
// list snapshots, including values equal to the subscription's current value.
const groupHealthcheckIntervalsKey = "nagi-group-healthcheck-intervals"

func healthInterval(value any) (int, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n, nil
		}
	}
	return 0, errors.New("healthcheck interval must be an integer")
}
func healthBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}
func applyGroupHealthcheckIntervals(base map[string]any, raw any) error {
	if raw == nil {
		return nil
	}
	intervals, ok := raw.(map[string]any)
	if !ok {
		return errors.New("local group healthcheck intervals must be a mapping")
	}
	for name, value := range intervals {
		n, err := healthInterval(value)
		if err != nil || n < 1 || n > 31536000 {
			return errors.New("local group healthcheck interval must be 1..31536000")
		}
		target, err := healthTarget(base, "group", name)
		if err != nil {
			continue // Retain dormant intent if a subscription temporarily removes a group.
		}
		target["interval"] = n
	}
	return nil
}

type Healthcheck struct {
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Profile           string `json:"profile"`
	IntervalSeconds   int    `json:"interval_seconds"`
	Enabled           *bool  `json:"enabled"`
	Lazy              bool   `json:"lazy"`
	Source            string `json:"source"`
	URLSource         string `json:"url_source"`
	CanResetURL       bool   `json:"can_reset_url"`
	TestURL           string `json:"test_url,omitempty"`
	FullTestURL       string `json:"-"`
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

// A nil answer leaves filtered include-all expansion to mihomo.
func groupHasExplicitNodes(group, effective map[string]any) *bool {
	nodes, _ := group["proxies"].([]any)
	present := len(nodes) > 0
	if present {
		return &present
	}
	if !healthBool(group["include-all"]) && !healthBool(group["include-all-proxies"]) {
		return &present
	}
	top, _ := effective["proxies"].([]any)
	uses, _ := group["use"].([]any)
	hasProviders := len(uses) > 0
	if healthBool(group["include-all"]) || healthBool(group["include-all-providers"]) {
		providers, _ := effective["proxy-providers"].(map[string]any)
		hasProviders = len(providers) > 0
	}
	// Without providers, mihomo fills an empty expanded list with empty-fallback.
	if !hasProviders {
		present = true
		return &present
	}
	if len(top) == 0 {
		return &present
	}
	if filter, _ := group["filter"].(string); filter != "" {
		return nil
	}
	present = true
	return &present
}

func (s *Store) Healthcheck(name, kind, targetName string) (Healthcheck, error) {
	r := Healthcheck{Kind: kind, Name: targetName, Profile: name, Source: "profile", URLSource: "profile", CanResetURL: kind == "group", Lazy: true}
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
	r.IntervalSeconds, err = healthInterval(fields["interval"])
	if err != nil {
		return r, err
	}
	r.IntervalDefaulted = r.IntervalSeconds == 0
	if value, ok := fields["lazy"]; ok {
		r.Lazy = healthBool(value)
	}
	enabled := false
	if kind == "provider" {
		enabled = healthBool(fields["enable"])
		if enabled && r.IntervalSeconds == 0 {
			r.IntervalSeconds = 300
		}
		r.Reason = "Provider scheduling can also be enabled by groups registering a different test URL."
	} else {
		enabled = r.IntervalSeconds > 0
		if explicit := groupHasExplicitNodes(target, effective); explicit != nil && *explicit && target["type"] != "select" && r.IntervalSeconds == 0 {
			r.IntervalSeconds = 300
			enabled = true
		}
		r.Reason = "Group interval controls explicit nodes; provider nodes use their provider scheduler, whose nonzero interval takes precedence."
	}
	r.Enabled = &enabled
	if kind == "group" {
		if explicit := groupHasExplicitNodes(target, effective); explicit == nil || !*explicit {
			r.Enabled = nil
			if explicit == nil {
				r.Reason += " Included node membership is resolved by mihomo; automatic groups with included explicit nodes default to 300 seconds."
			}
		}
	}
	if r.IntervalDefaulted {
		r.Reason += " Interval is omitted or zero; mihomo defaults apply."
	}
	if interval, e := s.subscriptionHealthcheck(name); e != nil {
		return r, e
	} else if interval != nil && (kind == "provider" && fields != nil || kind == "group" && subscriptionGroupEligible(target)) {
		r.Source = "subscription"
	}
	local, _ := healthTarget(overlay, kind, targetName)
	localFields := healthFields(local, kind)
	if value, ok := localFields["interval"]; ok {
		r.Source = "override"
		if kind == "group" {
			original, _ := healthTarget(source, kind, targetName)
			if reflect.DeepEqual(value, original["interval"]) {
				r.Source = "profile"
				if interval, _ := s.subscriptionHealthcheck(name); interval != nil && subscriptionGroupEligible(target) {
					r.Source = "subscription"
				}
			}
		}
	}
	if intervals, _ := overlay[groupHealthcheckIntervalsKey].(map[string]any); intervals[targetName] != nil && kind == "group" {
		r.Source = "override"
	}
	if kind == "provider" {
		original, _ := healthTarget(source, kind, targetName)
		inherited, _ := healthFields(original, kind)["url"].(string)
		r.CanResetURL = ValidateHealthcheckURL(inherited) == nil
	}
	if value, e := s.subscriptionHealthcheckURL(name); e != nil {
		return r, e
	} else if value != nil && (kind == "provider" && fields != nil || kind == "group" && subscriptionGroupEligible(target)) {
		r.URLSource = "subscription"
		if kind == "provider" {
			r.CanResetURL = ValidateHealthcheckURL(*value) == nil
		}
	}
	if value, ok := localFields["url"]; ok {
		r.URLSource = "override"
		if kind == "group" {
			original, _ := healthTarget(source, kind, targetName)
			if reflect.DeepEqual(value, original["url"]) {
				r.URLSource = "profile"
				if value, _ := s.subscriptionHealthcheckURL(name); value != nil && subscriptionGroupEligible(target) {
					r.URLSource = "subscription"
				}
			}
		}
	}
	if urls, _ := overlay[groupHealthcheckURLsKey].(map[string]any); kind == "group" && urls[targetName] != nil {
		r.URLSource = "override"
	}
	raw, _ := fields["url"].(string)
	if kind == "group" && raw == "" {
		uses, _ := target["use"].([]any)
		providers, _ := effective["proxy-providers"].(map[string]any)
		if healthBool(target["include-all"]) || healthBool(target["include-all-providers"]) {
			names := make([]string, 0, len(providers))
			for name := range providers {
				names = append(names, name)
			}
			sort.Strings(names)
			uses = nil
			for _, name := range names {
				uses = append(uses, name)
			}
		}
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
		r.FullTestURL = raw
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
	_, err = healthTarget(effective, kind, targetName)
	if err != nil {
		return err
	}
	if _, err := s.Healthcheck(name, kind, targetName); err != nil {
		return err
	}
	var local map[string]any
	if kind == "group" {
		intervals, _ := overlay[groupHealthcheckIntervalsKey].(map[string]any)
		if intervals == nil {
			intervals = map[string]any{}
		}
		if seconds != nil {
			intervals[targetName] = *seconds
		} else {
			delete(intervals, targetName)
		}
		if len(intervals) > 0 {
			overlay[groupHealthcheckIntervalsKey] = intervals
		} else {
			delete(overlay, groupHealthcheckIntervalsKey)
		}
		// Legacy/manual group snapshots may independently hold an interval.
		// Reset only this target's leaf while retaining the rest of that snapshot.
		if seconds == nil {
			local, _ = healthTarget(overlay, kind, targetName)
		}
		if local == nil {
			data, e := yaml.Marshal(overlay)
			if e != nil {
				return e
			}
			return s.SetOverride(ctx, name, data)
		}

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
