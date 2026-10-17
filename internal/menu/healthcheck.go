package menu

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type menuNodeDelay struct {
	Status     string `json:"status"`
	DelayMS    *int   `json:"delay_ms"`
	MeasuredAt string `json:"measured_at"`
	TestURL    string `json:"test_url"`
	Reason     string `json:"reason"`
}

func (d menuNodeDelay) label() string {
	switch d.Status {
	case "ok":
		if d.DelayMS != nil {
			if *d.DelayMS == 0 {
				return "<1 ms"
			}
			return fmt.Sprintf("%d ms", *d.DelayMS)
		}
		return "latency unknown"
	case "failed":
		return "latency test failed"
	default:
		if d.Reason != "" {
			return "latency unknown"
		}
		return "untested"
	}
}

type menuNodeInfo struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

func nodeKind(name string, info map[string]menuNodeInfo) string {
	if node, exists := info[name]; exists {
		return node.Kind
	}
	switch name {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE":
		return "builtin"
	}
	return info[name].Kind
}
func nodeLabel(name string, info map[string]menuNodeInfo, delays map[string]menuNodeDelay) string {
	switch nodeKind(name, info) {
	case "node":
		return name + " — " + delays[name].label()
	case "group":
		if status := info[name].Status; status == "available" || status == "unavailable" {
			return name + " — " + status
		}
	}
	return name
}

type menuHealthcheck struct {
	IntervalDefaulted bool   `json:"interval_defaulted"`
	IntervalSeconds   *int   `json:"interval_seconds"`
	Enabled           *bool  `json:"enabled"`
	Lazy              *bool  `json:"lazy"`
	Source            string `json:"source"`
	Profile           string `json:"profile"`
	TestURL           string `json:"test_url"`
	Reason            string `json:"reason"`
}

func healthcheckPage(kind, name string) *Page {
	return &Page{Title: "Automatic latency checks: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Automatic latency check settings: unknown"}
		var config menuHealthcheck
		err := decodeMenuResult(s.Fetch(ctx, "proxy", "healthcheck", "show", kind, name), &config)
		saveImpact := "Saves a profile override; reloads running mihomo"
		resetImpact := "Removes this local override; reloads running mihomo"
		if kind == "subscription" {
			saveImpact = "Saves subscription setting; reloads its active profile"
			resetImpact = "Removes subscription setting; reloads its active profile"
		}
		view.Actions = []Item{
			{Label: "Set local interval", Impact: saveImpact, Action: func(ctx context.Context, s *Session) error {
				values := map[string]string{}
				if config.IntervalSeconds != nil && *config.IntervalSeconds > 0 {
					values["seconds"] = strconv.Itoa(*config.IntervalSeconds)
				}
				_, err := s.Form(ctx, []Field{{Key: "seconds", Label: "Check interval seconds (1–31536000)", Required: true, Validate: func(raw string) error {
					n, err := strconv.ParseInt(raw, 10, 64)
					if err != nil || n < 1 || n > 31536000 {
						return fmt.Errorf("enter an integer from 1 to 31536000 seconds")
					}
					return nil
				}}}, values, func(v map[string]string) Result {
					return s.Run(ctx, "proxy", "healthcheck", "set", kind, name, v["seconds"])
				})
				return err
			}},
			{Label: healthcheckResetLabel(kind), Impact: resetImpact, Args: []string{"proxy", "healthcheck", "reset", kind, name}},
		}
		if err != nil {
			return view, err
		}
		interval := "unknown"
		if config.IntervalDefaulted && (config.IntervalSeconds == nil || *config.IntervalSeconds == 0) {
			interval = "original configuration"
		} else if config.IntervalSeconds != nil {
			interval = fmt.Sprintf("%d seconds", *config.IntervalSeconds)
		}
		state := func(v *bool) string {
			if v == nil {
				return "unknown"
			}
			return strconv.FormatBool(*v)
		}
		source := config.Source
		if source == "" {
			source = "unknown"
		}
		if source == "override" {
			source = "local override"
		}
		profile := config.Profile
		if profile == "" {
			if kind == "subscription" {
				profile = "not applied"
			} else {
				profile = "unknown"
			}
		}
		view.Context = fmt.Sprintf("Profile: %s\nInterval: %s; enabled: %s; lazy: %s\nConfiguration source: %s", profile, interval, state(config.Enabled), state(config.Lazy), source)
		if kind == "subscription" {
			view.Context = fmt.Sprintf("Profile: %s\nInterval: %s\nConfiguration source: %s\nChecker enablement and lazy behavior follow each checker's configuration.", profile, interval, source)
		}
		if config.Reason != "" {
			view.Context += "\nScheduling note: " + config.Reason
		}
		if config.TestURL != "" {
			view.Context += "\nTest endpoint: " + menuTestEndpoint(config.TestURL)
		}
		view.Context += "\nMihomo schedules these checks. Lazy checks may skip unused groups or providers.\nAutomatic latency tests and subscription download intervals are separate."
		view.Context += "\nProvider local settings take precedence over subscription settings, then the original configuration."
		return view, nil
	}}
}

// Display only the origin provided by the CLI, never credentials or URL paths.
func menuTestEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "unknown"
	}
	return u.Host + " (" + u.Scheme + ")"
}

func healthcheckResetLabel(kind string) string {
	if kind == "provider" {
		return "Inherit subscription or original interval"
	}
	return "Use original configuration interval"
}
