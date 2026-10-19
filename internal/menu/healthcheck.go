package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type menuNodeDelay struct {
	Status     string `json:"status"`
	DelayMS    *int   `json:"delay_ms"`
	MeasuredAt string `json:"measured_at"`
	TestURL    string `json:"test_url"`
	Reason     string `json:"reason"`
}

func (d menuNodeDelay) label() string { return d.labelFor(i18n.New(i18n.English)) }
func (d menuNodeDelay) labelFor(language i18n.Context) string {
	switch d.Status {
	case "ok":
		if d.DelayMS != nil {
			if *d.DelayMS == 0 {
				return "<1 ms"
			}
			return fmt.Sprintf("%d ms", *d.DelayMS)
		}
		return menuText(language, "menu.latency.unknown", "latency unknown")
	case "failed":
		return menuText(language, "menu.latency.failed", "latency test failed")
	default:
		if d.Reason != "" {
			return menuText(language, "menu.latency.unknown", "latency unknown")
		}
		return menuState(language, "untested")
	}
}

type menuNodeInfo struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

func localizednodeKind(language i18n.Context, name string, info map[string]menuNodeInfo) string {
	if node, exists := info[name]; exists {
		return node.Kind
	}
	switch name {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE":
		return "builtin"
	}
	return info[name].Kind
}
func localizednodeLabel(language i18n.Context, name string, info map[string]menuNodeInfo, delays map[string]menuNodeDelay) string {
	switch localizednodeKind(language, name, info) {
	case "node":
		return name + " — " + delays[name].labelFor(language)
	case "group":
		if status := info[name].Status; status == "available" || status == "unavailable" {
			return name + " — " + menuState(language, status)
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
	TestURLSource     string `json:"test_url_source"`
	URLSource         string `json:"url_source"`
	CanResetURL       *bool  `json:"can_reset_url"`
	Reason            string `json:"reason"`
}

func localizedhealthcheckPage(language i18n.Context, kind, name string) *Page {
	return &Page{ID: "healthcheck.healthcheckPage.page1", Title: menuText(language, "menu.healthcheck.healthcheck_page.automatic_latency_checks", "Automatic latency checks: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.healthcheck.healthcheck_page.automatic_latency_check_settings_unknown", "Automatic latency check settings: unknown")}
		var config menuHealthcheck
		err := localizeddecodeMenuResult(language, s.Fetch(ctx, "proxy", "healthcheck", "show", kind, name), &config)
		saveImpact := menuText(language, "menu.healthcheck.healthcheck_page.saves_a_profile_override_reloads_running_mihomo", "Saves a profile override; reloads running mihomo")
		resetImpact := menuText(language, "menu.healthcheck.healthcheck_page.removes_this_local_override_reloads_running_miho", "Removes this local override; reloads running mihomo")
		if kind == "subscription" {
			saveImpact = menuText(language, "menu.healthcheck.healthcheck_page.saves_subscription_setting_reloads_its_active_pr", "Saves subscription setting; reloads its active profile")
			resetImpact = menuText(language, "menu.healthcheck.healthcheck_page.removes_subscription_setting_reloads_its_active", "Removes subscription setting; reloads its active profile")
		}
		view.Actions = []Item{
			{ID: "healthcheck.healthcheckPage.item2", Label: menuText(language, "menu.healthcheck.healthcheck_page.set_local_interval", "Set local interval"), Impact: saveImpact, Action: func(ctx context.Context, s *Session) error {
				values := map[string]string{}
				if config.IntervalSeconds != nil && *config.IntervalSeconds > 0 {
					values["seconds"] = strconv.Itoa(*config.IntervalSeconds)
				}
				_, err := s.Form(ctx, []Field{{Key: "seconds", Label: menuText(language, "menu.healthcheck.healthcheck_page.check_interval_seconds_1_31536000", "Check interval seconds (1–31536000)"), Required: true, Validate: func(raw string) error {
					n, err := strconv.ParseInt(raw, 10, 64)
					if err != nil || n < 1 || n > 31536000 {
						return fmt.Errorf("%s", menuText(language, "menu.healthcheck.healthcheck_page.enter_an_integer_from_1_to_31536000_seconds", "enter an integer from 1 to 31536000 seconds"))
					}
					return nil
				}}}, values, func(v map[string]string) Result {
					return s.Run(ctx, "proxy", "healthcheck", "set", kind, name, v["seconds"])
				})
				return err
			}},
			{ID: "healthcheck.healthcheckPage.item3", Label: localizedhealthcheckResetLabel(language, kind), Impact: resetImpact, Args: []string{"proxy", "healthcheck", "reset", kind, name}},
		}
		view.Actions = append(view.Actions,
			Item{ID: "healthcheck.healthcheckPage.item4", Label: menuText(language, "menu.healthcheck.healthcheck_page.set_test_url", "Set test URL"), Impact: saveImpact, Action: func(ctx context.Context, s *Session) error {
				_, err := s.Form(ctx, []Field{localizedtestURLField(language, true)}, map[string]string{}, func(v map[string]string) Result {
					return s.Run(ctx, "proxy", "healthcheck", "url", "set", kind, name, v["test_url"])
				})
				return err
			}},
			Item{ID: "healthcheck.healthcheckPage.item5", Label: menuText(language, "menu.healthcheck.healthcheck_page.inherit_test_url", "Inherit test URL"), Impact: resetImpact, Args: []string{"proxy", "healthcheck", "url", "reset", kind, name}},
		)
		if err != nil {
			return view, err
		}
		if kind == "provider" && config.CanResetURL != nil && !*config.CanResetURL {
			view.Actions = view.Actions[:len(view.Actions)-1]
		}
		interval := "unknown"
		if config.IntervalDefaulted && (config.IntervalSeconds == nil || *config.IntervalSeconds == 0) {
			interval = menuText(language, "menu.healthcheck.healthcheck_page.original_configuration", "original configuration")
		} else if config.IntervalSeconds != nil {
			interval = menuFormat(language, "menu.healthcheck.healthcheck_page.d_seconds", "%d seconds", *config.IntervalSeconds)
		}
		state := func(v *bool) string {
			if v == nil {
				return menuState(language, "unknown")
			}
			return menuState(language, strconv.FormatBool(*v))
		}
		source := config.Source
		if source == "" {
			source = menuState(language, "unknown")
		}
		if source == "override" {
			source = menuText(language, "menu.healthcheck.healthcheck_page.local_override", "local override")
		}
		profile := config.Profile
		if profile == "" {
			if kind == "subscription" {
				profile = menuText(language, "menu.healthcheck.healthcheck_page.not_applied", "not applied")
			} else {
				profile = "unknown"
			}
		}
		view.Context = menuFormat(language, "menu.healthcheck.healthcheck_page.profile_s_interval_s_enabled_s_lazy_s_configurat", "Profile: %s\nInterval: %s; enabled: %s; lazy: %s\nConfiguration source: %s", profile, interval, state(config.Enabled), state(config.Lazy), source)
		if kind == "subscription" {
			view.Context = menuFormat(language, "menu.healthcheck.healthcheck_page.profile_s_interval_s_configuration_source_s_chec", "Profile: %s\nInterval: %s\nConfiguration source: %s\nChecker enablement and lazy behavior follow each checker's configuration.", profile, interval, source)
		}
		if config.Reason != "" {
			view.Context += menuText(language, "menu.healthcheck.healthcheck_page.scheduling_note", "\nScheduling note: ") + config.Reason
		}
		if config.TestURL != "" {
			view.Context += menuText(language, "menu.healthcheck.healthcheck_page.test_endpoint", "\nTest endpoint: ") + localizedmenuTestEndpoint(language, config.TestURL)
		}
		urlSource := config.TestURLSource
		if urlSource == "" {
			urlSource = config.URLSource
		}
		if urlSource != "" {
			view.Context += menuText(language, "menu.healthcheck.healthcheck_page.test_url_source", "\nTest URL source: ") + menuState(language, urlSource)
		}
		view.Context += menuText(language, "menu.healthcheck.healthcheck_page.mihomo_schedules_these_checks_lazy_checks_may_sk", "\nMihomo schedules these checks. Lazy checks may skip unused groups or providers.\nAutomatic latency tests and subscription download intervals are separate.")
		view.Context += menuText(language, "menu.healthcheck.healthcheck_page.provider_local_settings_take_precedence_over_sub", "\nProvider local settings take precedence over subscription settings, then the original configuration.")
		return view, nil
	}}
}

// Display only the origin provided by the CLI, never credentials or URL paths.
func localizedmenuTestEndpoint(language i18n.Context, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "unknown"
	}
	return u.Host + " (" + u.Scheme + ")"
}

func localizedhealthcheckResetLabel(language i18n.Context, kind string) string {
	if kind == "provider" {
		return menuText(language, "menu.healthcheck.healthcheck_reset_label.inherit_subscription_or_original_interval",

			// The CLI validates and persists URLs; the form checks input without echoing it.
			"Inherit subscription or original interval")
	}
	return menuText(language, "menu.healthcheck.healthcheck_reset_label.use_original_configuration_interval", "Use original configuration interval")
}

func localizedtestURLField(language i18n.Context, required bool) Field {
	label := menuText(language, "menu.healthcheck.test_u_r_l_field.test_http_s_url", "Test HTTP(S) URL")
	if !required {
		label += menuText(language, "menu.healthcheck.test_u_r_l_field.blank_inherits_configuration", " (blank inherits configuration)")
	}
	return Field{Key: "test_url", Label: label, Required: required, Validate: func(raw string) error {
		if strings.TrimSpace(raw) == "" && !required {
			return nil
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") {
			return fmt.Errorf("%s", menuText(language, "menu.healthcheck.test_u_r_l_field.enter_an_http_s_url_with_a_hostname_and_without", "enter an HTTP(S) URL with a hostname and without user credentials or a fragment"))
		}
		return nil
	}}
}

func localizedsecondsField(language i18n.Context, label string, allowOff bool) Field {
	return Field{Key: "seconds", Label: label, Required: true, Validate: func(raw string) error {
		if allowOff && raw == "off" {
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 || n > 31536000 {
			return fmt.Errorf("%s", menuText(language, "menu.healthcheck.seconds_field.enter_an_integer_from_1_to_31536000_seconds", "enter an integer from 1 to 31536000 seconds"))
		}
		return nil
	}}
}
func nodeKind(name string, info map[string]menuNodeInfo) string {
	return localizednodeKind(i18n.New(i18n.English), name, info)
}
func nodeLabel(name string, info map[string]menuNodeInfo, delays map[string]menuNodeDelay) string {
	return localizednodeLabel(i18n.New(i18n.English), name, info, delays)
}
func healthcheckPage(
	kind, name string) *Page {
	return localizedhealthcheckPage(i18n.New(i18n.English), kind, name)
}

func menuTestEndpoint(raw string) string {
	return localizedmenuTestEndpoint(i18n.New(i18n.English), raw)
}
func healthcheckResetLabel(kind string) string {
	return localizedhealthcheckResetLabel(i18n.New(i18n.English), kind)
}

func testURLField(required bool) Field {
	return localizedtestURLField(i18n.New(i18n.English), required)
}
func secondsField(label string, allowOff bool) Field {
	return localizedsecondsField(i18n.New(i18n.English), label, allowOff)
}
