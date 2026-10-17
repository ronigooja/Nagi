package profile

import (
	"context"
	"errors"
	"gopkg.in/yaml.v3"
	"net/url"
	"strings"
)

const groupHealthcheckURLsKey = "nagi-group-healthcheck-urls"

// ValidateHealthcheckURL validates an explicit native mihomo probe URL without
// putting its private path or query in an error.
func ValidateHealthcheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "\r\n\t ") || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") {
		return errors.New("URL must be an absolute HTTP(S) URL without userinfo or fragment")
	}
	return nil
}

func applyGroupHealthcheckURLs(base map[string]any, raw any) error {
	if raw == nil {
		return nil
	}
	urls, ok := raw.(map[string]any)
	if !ok {
		return errors.New("local group healthcheck URLs must be a mapping")
	}
	for name, value := range urls {
		text, ok := value.(string)
		if !ok || ValidateHealthcheckURL(text) != nil {
			return errors.New("invalid local group healthcheck URL")
		}
		target, err := healthTarget(base, "group", name)
		if err == nil {
			target["url"] = text
		}
	}
	return nil
}

func (s *Store) SetHealthcheckURL(ctx context.Context, name, kind, targetName string, raw *string) error {
	if raw != nil {
		if err := ValidateHealthcheckURL(*raw); err != nil {
			return err
		}
	}
	_, source, overlay, err := s.healthMaps(name)
	if err != nil {
		return err
	}
	state, err := s.Healthcheck(name, kind, targetName)
	if err != nil {
		return err
	}
	if raw == nil && kind == "provider" && !state.CanResetURL {
		return errors.New("provider has no inherited test URL; set an explicit test URL instead")
	}
	if kind == "group" {
		urls, _ := overlay[groupHealthcheckURLsKey].(map[string]any)
		if urls == nil {
			urls = map[string]any{}
		}
		if raw != nil {
			urls[targetName] = *raw
		} else {
			delete(urls, targetName)
		}
		if len(urls) > 0 {
			overlay[groupHealthcheckURLsKey] = urls
		} else {
			delete(overlay, groupHealthcheckURLsKey)
		}
		// Reset legacy list snapshots without losing their interval or attachments.
		if raw == nil {
			if local, e := healthTarget(overlay, kind, targetName); e == nil {
				delete(local, "url")
				original, _ := healthTarget(source, kind, targetName)
				if v, ok := original["url"]; ok {
					local["url"] = v
				}
			}
		}
	} else {
		providers, _ := overlay["proxy-providers"].(map[string]any)
		if providers == nil {
			providers = map[string]any{}
			overlay["proxy-providers"] = providers
		}
		local, _ := providers[targetName].(map[string]any)
		if local == nil {
			local = map[string]any{}
			providers[targetName] = local
		}
		check, _ := local["health-check"].(map[string]any)
		if check == nil {
			check = map[string]any{}
			local["health-check"] = check
		}
		if raw != nil {
			check["url"] = *raw
		} else {
			delete(check, "url")
		}
	}
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	return s.SetOverride(ctx, name, data)
}

type ProviderInterval struct {
	Kind                  string `json:"kind"`
	Name                  string `json:"name"`
	Profile               string `json:"profile"`
	IntervalSeconds       int    `json:"interval_seconds"`
	Source                string `json:"source"`
	Enabled               bool   `json:"enabled"`
	IntervalDefaulted     bool   `json:"interval_defaulted"`
	CanReset              bool   `json:"can_reset"`
	SourceIntervalSeconds *int   `json:"source_interval_seconds,omitempty"`
}

func (s *Store) ProviderInterval(name, targetName string) (ProviderInterval, error) {
	r := ProviderInterval{Kind: "provider", Name: targetName, Profile: name, Source: "profile"}
	effective, source, overlay, err := s.healthMaps(name)
	if err != nil {
		return r, err
	}
	original, _ := healthTarget(source, "provider", targetName)
	r.CanReset = original != nil
	if original != nil {
		seconds, err := healthInterval(original["interval"])
		if err != nil {
			return r, err
		}
		r.SourceIntervalSeconds = &seconds
	}
	target, err := healthTarget(effective, "provider", targetName)
	if err != nil {
		return r, err
	}
	if target["type"] != "http" {
		return r, errors.New("download interval requires an HTTP provider")
	}
	r.IntervalSeconds, err = healthInterval(target["interval"])
	if err != nil {
		return r, err
	}
	_, present := target["interval"]
	r.IntervalDefaulted = !present
	r.Enabled = r.IntervalSeconds > 0
	local, _ := healthTarget(overlay, "provider", targetName)
	if _, ok := local["interval"]; ok {
		r.Source = "override"
	}
	return r, nil
}
func (s *Store) SetProviderInterval(ctx context.Context, name, targetName string, seconds *int) error {
	if seconds != nil && (*seconds < 0 || *seconds > 31536000) {
		return errors.New("SECONDS must be off or an integer from 1 to 31536000")
	}
	if state, err := s.ProviderInterval(name, targetName); err != nil {
		return err
	} else if seconds == nil && !state.CanReset {
		return errors.New("provider has no source download interval; set an explicit interval or off")
	}
	_, _, overlay, err := s.healthMaps(name)
	if err != nil {
		return err
	}
	providers, _ := overlay["proxy-providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		overlay["proxy-providers"] = providers
	}
	local, _ := providers[targetName].(map[string]any)
	if local == nil {
		local = map[string]any{}
		providers[targetName] = local
	}
	if seconds != nil {
		local["interval"] = *seconds
	} else {
		delete(local, "interval")
	}
	data, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	return s.SetOverride(ctx, name, data)
}
