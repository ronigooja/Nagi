package menu

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type menuProfiles struct {
	Profiles     []string `json:"profiles"`
	Current      string   `json:"current"`
	CurrentError string   `json:"current_error"`
}

type menuDefaultOverrideStatus struct {
	Applied          bool     `json:"default_override_applied"`
	DNSProxyGroup    string   `json:"dns_proxy_group"`
	DNSProxyGroups   []string `json:"dns_proxy_groups"`
	DNSProxyRequired bool     `json:"dns_proxy_required"`
}

type menuSubscription struct {
	Name              string `json:"name"`
	KeyConfigured     bool   `json:"key_configured"`
	ScheduleSeconds   int64  `json:"schedule_seconds"`
	NextDue           string `json:"next_due"`
	PauseReason       string `json:"pause_reason"`
	LastFailureStage  string `json:"last_failure_stage"`
	LastFailureReason string `json:"last_failure_reason"`
	UpdatedAt         string `json:"updated_at"`
	ExpiresAt         string `json:"expires_at"`
	Upload            int64  `json:"upload"`
	Download          int64  `json:"download"`
	Total             int64  `json:"total"`
}

var menuNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var menuURLPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)

func init() {
	RegisterPage("profile", ProfilePage)
	RegisterPage("subscription", subscriptionListPage)
}

// ProfilePage is the direct entry to local profiles.
func ProfilePage() *Page { return profileListPage() }

func profileListPage() *Page {
	return &Page{Title: "Profiles", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: "No profiles found. Choose Import a profile.", Actions: []Item{
			{Label: "Import a profile", Navigate: importProfile},
		}}
		var data menuProfiles
		if err := decodeMenuResult(s.Fetch(ctx, "profile", "list"), &data); err != nil {
			view.Context = "Selected profile: unknown"
			return view, err
		}
		if data.CurrentError != "" {
			view.Context = "Selected profile unavailable: " + data.CurrentError
		} else if data.Current != "" {
			view.Context = "Selected profile: " + data.Current
		}
		if len(data.Profiles) == 0 {
			view.Context += "\nNo profiles found. Import one with `nagi profile import NAME FILE`."
		}
		for _, name := range data.Profiles {
			name := name
			label := name
			if name == data.Current {
				label += " (selected)"
			}
			view.Items = append(view.Items, Item{Name: name, Label: label, Selected: data.CurrentError == "" && name == data.Current, Next: func() *Page { return profileDetailPage(name) }})
		}
		return view, nil
	}}
}

func profileDetailPage(name string) *Page {
	return &Page{Title: "Profile: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Actions: []Item{
			{Label: "Use profile", Args: []string{"profile", "use", name}},
			{Label: "Export configuration", Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "export", name) }},
			submenu(actionPage("Backup & Restore", Item{Label: "Back up configuration", Args: []string{"profile", "backup", name}}, Item{Label: "Restore latest backup", Args: []string{"profile", "restore", name}, Impact: "Replaces configuration; reloads if active"}, Item{Label: "Compare with backup or another profile", Action: func(ctx context.Context, s *Session) error { return diffProfile(ctx, s, name) }})),
			{Label: "Local Overrides", Next: func() *Page { return localOverridesPage(name) }},
			{Label: "Remove profile", Navigate: removeResource("profile", name), Impact: "Deletes configuration, backups, local overrides, custom rules, and saved choices; keeps subscriptions"},
		}}
		var data menuProfiles
		if err := decodeMenuResult(s.Fetch(ctx, "profile", "list"), &data); err != nil {
			view.Context = "Selected profile: unknown (" + menuURLPattern.ReplaceAllString(err.Error(), "[redacted URL]") + ")"
		} else if data.CurrentError != "" {
			view.Context = "Selected profile: unknown (" + menuURLPattern.ReplaceAllString(data.CurrentError, "[redacted URL]") + ")"
		} else if data.Current != "" {
			view.Context = "Selected profile: " + data.Current
		} else {
			view.Context = "Selected profile: none"
		}
		return view, nil
	}}
}

func localOverridesPage(name string) *Page {
	return &Page{Title: "Local Overrides", Load: func(ctx context.Context, s *Session) (PageView, error) {
		defaultApplied := false
		var status menuDefaultOverrideStatus
		statusErr := decodeMenuResult(s.Fetch(ctx, "profile", "override", "default-status", name), &status)
		statusAvailable := statusErr == nil
		if statusAvailable {
			defaultApplied = status.Applied
		}
		defaultItem := Item{
			Label:    "Apply default runtime settings",
			Selected: statusAvailable && defaultApplied,
			Impact:   "Copies safe settings from the default profile; keeps nodes, groups, and rules",
			Action: func(ctx context.Context, s *Session) error {
				return toggleDefaultOverride(ctx, s, name, defaultApplied)
			},
		}
		if defaultApplied {
			defaultItem.Label = "Disable default runtime settings"
			defaultItem.Impact = "Restores the local override saved before applying defaults"
		} else if status.DNSProxyGroup != "" {
			defaultItem.Impact += "; DNS via " + status.DNSProxyGroup
		}
		view := PageView{Actions: []Item{
			{Label: "Show local override", Args: []string{"profile", "override", "show", name}},
			{Label: "Set local override from file", Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "override", name) }},
			{Label: "Clear local override", Args: []string{"profile", "override", "clear", name}, Impact: "Restores original settings; reloads if active"},
		}}
		if statusAvailable {
			view.Actions = append([]Item{defaultItem}, view.Actions...)
		} else {
			view.Context = "Default runtime settings: unavailable"
		}
		return view, statusErr
	}}
}

func toggleDefaultOverride(ctx context.Context, s *Session, name string, expected bool) error {
	var status menuDefaultOverrideStatus
	if err := decodeMenuResult(s.Fetch(ctx, "profile", "override", "default-status", name), &status); err != nil {
		return err
	}
	if status.Applied != expected {
		return errors.New("Default runtime settings changed. Review the refreshed menu before choosing again.")
	}
	var result Result
	if status.Applied {
		result = s.Run(ctx, "profile", "override", "restore-default", name)
	} else {
		if !status.DNSProxyRequired || status.DNSProxyGroup != "" && menuHasGroup(status.DNSProxyGroups, status.DNSProxyGroup) {
			result = s.Run(ctx, "profile", "override", "apply-default", name)
		} else {
			if len(status.DNSProxyGroups) == 0 {
				return errors.New("No eligible proxy group in this profile. Add a proxy group before applying default runtime settings.")
			}
			group, err := s.Select("DNS proxy group for "+name, status.DNSProxyGroups)
			if err != nil {
				return err
			}
			result = s.Run(ctx, "profile", "override", "apply-default", name, "--dns-proxy", group)
		}
	}
	if result.Code != 0 && result.Error != nil {
		return errors.New(result.Error.Message)
	}
	if result.Code != 0 {
		return fmt.Errorf("command exited with status %d", result.Code)
	}
	return nil
}

func menuHasGroup(groups []string, name string) bool {
	for _, group := range groups {
		if group == name {
			return true
		}
	}
	return false
}

func validateMenuName(value string) error {
	if len(value) > 128 || !menuNamePattern.MatchString(value) {
		return fmt.Errorf("use 1–128 letters, digits, _ or -")
	}
	return nil
}

func nameField(label string) Field {
	return Field{Key: "name", Label: label, Required: true, Validate: validateMenuName}
}

func importProfile(ctx context.Context, s *Session) (Navigation, error) {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{nameField("New profile name"), {Key: "file", Label: "Local YAML file", Required: true}}, values, func(v map[string]string) Result { return s.Run(ctx, "profile", "import", v["name"], v["file"]) })
	if err != nil {
		return Navigation{}, err
	}
	return Navigation{Next: profileDetailPage(values["name"])}, nil
}

func profileFileAction(ctx context.Context, s *Session, operation, name string) error {
	label := "Destination file (must not exist)"
	if operation == "override" {
		label = "Local override YAML file"
	}
	_, err := s.Form(ctx, []Field{{Key: "file", Label: label, Required: true}}, nil, func(v map[string]string) Result {
		if operation == "override" {
			return s.Run(ctx, "profile", "override", "set", name, v["file"])
		}
		return s.Run(ctx, "profile", "export", name, v["file"])
	})
	return err
}

func removeResource(kind, name string) func(context.Context, *Session) (Navigation, error) {
	return func(ctx context.Context, s *Session) (Navigation, error) {
		result := s.Run(ctx, kind, "remove", name)
		return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
	}
}

func diffProfile(ctx context.Context, s *Session, name string) error {
	var data menuProfiles
	if err := decodeMenuResult(s.Fetch(ctx, "profile", "list"), &data); err != nil {
		return err
	}
	choices := []string{"backup"}
	for _, other := range data.Profiles {
		if other != name {
			choices = append(choices, other)
		}
	}
	other, err := s.Select("Compare with", choices)
	if err != nil {
		return err
	}
	if other != "" {
		s.Run(ctx, "profile", "diff", name, other)
	}
	return nil
}

func subscriptionListPage() *Page {
	return &Page{Title: "Subscriptions", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: "No subscriptions configured. Choose Add and Apply subscription.", Actions: []Item{{Label: "Add and Apply subscription", Impact: subscriptionApplyImpact, Navigate: addSubscription}}}
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			return view, err
		}
		for _, entry := range data.Subscriptions {
			entry := entry
			label := entry.Name
			if entry.ScheduleSeconds > 0 {
				label += fmt.Sprintf(" (every %ds)", entry.ScheduleSeconds)
			}
			if entry.UpdatedAt == "" {
				label += " (never downloaded)"
			} else {
				label += " (updated " + subscriptionTime(entry.UpdatedAt) + ")"
			}
			view.Items = append(view.Items, Item{Name: entry.Name, Label: label, Next: func() *Page { return subscriptionDetailPage(entry.Name) }})
		}
		return view, nil
	}}
}

func addSubscription(ctx context.Context, s *Session) (Navigation, error) {
	values := map[string]string{"seconds": "3600"}
	_, err := s.Form(ctx, []Field{nameField("New subscription name"), {Key: "url", Label: "Subscription HTTP(S) URL", Required: true, Validate: func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return fmt.Errorf("enter an HTTP(S) URL without user credentials")
		}
		return nil
	}}, secondsField("Update interval seconds (default 3600; off disables)", true), testURLField(false)}, values, func(v map[string]string) Result {
		args := []string{"subscription", "add", v["name"], v["url"]}
		if v["seconds"] != "3600" {
			args = append(args, "--schedule", v["seconds"])
		}
		if v["test_url"] != "" {
			args = append(args, "--test-url", v["test_url"])
		}
		return s.Run(ctx, args...)
	})
	if err != nil {
		return Navigation{}, err
	}
	updateAndApplySubscription(ctx, s, values["name"], true)
	return Navigation{Next: subscriptionDetailPage(values["name"])}, nil
}

func subscriptionSchedule(ctx context.Context, s *Session, name string) error {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{{Key: "seconds", Label: "Interval seconds", Required: true}}, values, func(v map[string]string) Result {
		return s.Run(ctx, "subscription", "schedule", name, v["seconds"])
	})
	return err
}

func subscriptionKey(ctx context.Context, s *Session, name string) error {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{{Key: "file", Label: "Identity file path", Required: true}}, values, func(v map[string]string) Result {
		return s.Run(ctx, "subscription", "key", name, v["file"])
	})
	return err
}

const subscriptionApplyImpact = "Updates subscription; retains local settings and nodes; clears saved node selections"

// Each successful stage is kept if a later stage fails. Creation forms submit
// only add; download/apply failures return to the saved subscription's details.
func updateAndApplySubscription(ctx context.Context, s *Session, name string, added bool) {
	defer s.discardInterrupts()
	result := s.Fetch(ctx, "subscription", "update", name)
	if result.Code != 0 || result.Error != nil {
		state := "Download failed; apply was not attempted."
		if added {
			state = "Subscription saved, but download failed; apply was not attempted."
		}
		s.feedback(feedbackWarning, state)
		s.ShowResult([]string{"subscription", "update", name}, result)
		return
	}
	result = s.Fetch(ctx, "subscription", "apply", name)
	if result.Code != 0 || result.Error != nil {
		s.feedback(feedbackWarning, "Downloaded, but apply failed. Cache retained; retry with `nagi subscription apply NAME`.")
		s.ShowResult([]string{"subscription", "apply", name}, result)
		return
	}
	s.feedback(feedbackSuccess, "Subscription updated and applied.")
}

func subscriptionDetailPage(name string) *Page {
	return &Page{Title: "Subscription: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Actions: []Item{
			{Label: "Update and Apply", Impact: subscriptionApplyImpact, Action: func(ctx context.Context, s *Session) error {
				updateAndApplySubscription(ctx, s, name, false)
				return nil
			}},
			{Label: "Automatic Updates", Next: func() *Page { return subscriptionSettingsPage(name, false) }},
			{Label: "Automatic latency checks", Next: func() *Page { return healthcheckPage("subscription", name) }},
			{Label: "Encryption Settings", Next: func() *Page { return subscriptionSettingsPage(name, true) }},
			{Label: "Remove subscription and cache", Navigate: removeResource("subscription", name)},
		}}
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			view.Context = "Subscription metadata: unknown (" + menuURLPattern.ReplaceAllString(err.Error(), "[redacted URL]") + ")"
		} else {
			view.Context = "Subscription metadata: unknown (record unavailable)"
			for _, entry := range data.Subscriptions {
				if entry.Name == name {
					view.Context = subscriptionMetadata(entry)
					if entry.PauseReason != "" {
						view.Context += "\nAutomatic apply paused: " + entry.PauseReason
					}
					if entry.LastFailureStage != "" {
						view.Context += "\nLast scheduler failure: " + entry.LastFailureStage + "/" + entry.LastFailureReason
					}
					break
				}
			}
		}
		return view, nil
	}}
}

// Subscription settings read only public metadata, never the identity contents.
func subscriptionSettingsPage(name string, encryption bool) *Page {
	title := "Automatic Updates"
	if encryption {
		title = "Encryption Settings"
	}
	return &Page{Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Automatic updates: unavailable", Actions: []Item{
			{Label: "Set update interval", Action: func(ctx context.Context, s *Session) error { return subscriptionSchedule(ctx, s, name) }},
		}}
		if encryption {
			view.Context = "Identity file: unavailable"
			view.Actions = []Item{{Label: "Set identity file", Action: func(ctx context.Context, s *Session) error { return subscriptionKey(ctx, s, name) }}}
		}
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			return view, err
		}
		for _, entry := range data.Subscriptions {
			if entry.Name != name {
				continue
			}
			if encryption {
				view.Context = "Identity file: not configured"
				if entry.KeyConfigured {
					view.Context = "Identity file: configured"
					view.Actions[0].Label = "Replace identity file"
					view.Actions = append(view.Actions, Item{Label: "Clear encryption key", Args: []string{"subscription", "key", name, "clear"}})
				}
			} else {
				view.Context = "Automatic updates: disabled"
				if entry.ScheduleSeconds > 0 {
					view.Context = fmt.Sprintf("Download update interval: %d seconds", entry.ScheduleSeconds)
					view.Actions = append(view.Actions, Item{Label: "Disable automatic updates", Args: []string{"subscription", "schedule", name, "off"}})
				}
			}
			return view, nil
		}
		return view, errors.New("subscription record unavailable")
	}}
}

func subscriptionMetadata(entry menuSubscription) string {
	updated, expiry := subscriptionTime(entry.UpdatedAt), subscriptionTime(entry.ExpiresAt)
	if entry.UpdatedAt == "" {
		updated = "never downloaded"
	}
	quota := "unknown"
	if entry.Total > 0 {
		remaining := entry.Total - entry.Upload - entry.Download
		if remaining < 0 {
			remaining = 0
		}
		quota = fmt.Sprintf("%s available / %s total (%s uploaded, %s downloaded)", subscriptionBytes(remaining), subscriptionBytes(entry.Total), subscriptionBytes(entry.Upload), subscriptionBytes(entry.Download))
	}
	return "Updated: " + updated + "\nQuota: " + quota + "\nExpires: " + expiry
}

// subscriptionTime formats command timestamps only at the menu boundary.
func subscriptionTime(raw string) string {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "unknown"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func subscriptionBytes(bytes int64) string {
	if bytes < 0 {
		return "unknown"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value := float64(bytes) / 1024
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".") + " " + units[unit]
}
