package menu

import "github.com/ronigooja/Nagi/internal/i18n"

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
	RegisterPage("profiles", ProfilesAndSubscriptionsPage)
	RegisterPage("subscription", subscriptionListPage)
}

func localizedProfilesAndSubscriptionsPage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.profile.profiles_and_subscriptions_page.profiles_subscriptions", "Profiles & Subscriptions"), Item{ID: "profile.ProfilesAndSubscriptionsPage.item1", Label: menuText(language, "menu.profile.profiles_and_subscriptions_page.profiles", "Profiles"), Next: func() *Page {
		return localizedProfilePage(language)
	},
	}, Item{ID: "profile.ProfilesAndSubscriptionsPage.item2", Label: menuText(language, "menu.profile.profiles_and_subscriptions_page.subscriptions", "Subscriptions"), Next: func() *Page {
		return localizedsubscriptionListPage(
			language,
		)
	},
	}, Item{ID: "profile.ProfilesAndSubscriptionsPage.item3", Label: menuText(language, "menu.profile.profiles_and_subscriptions_page.current_configuration",

		// ProfilePage is the direct entry to local profiles.
		"Current Configuration"), Next: func() *Page {
		return localizedconfigurationPage(language)
	},
	})
}

func localizedProfilePage(language i18n.Context) *Page {
	return localizedprofileListPage(language)
}

func localizedprofileListPage(language i18n.Context) *Page {
	return &Page{ID: "profile.profileListPage.page1", Title: menuText(language, "menu.profile.profile_list_page.profiles", "Profiles"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: menuText(language, "menu.profile.profile_list_page.no_profiles_found_choose_import_a_profile", "No profiles found. Choose Import a profile."), Actions: []Item{
			{ID: "profile.profileListPage.item2", Label: menuText(language, "menu.profile.profile_list_page.import_a_profile", "Import a profile"), Navigate: importProfile},
		}}
		var data menuProfiles
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "profile", "list"), &data); err != nil {
			view.Context = menuText(language, "menu.profile.profile_list_page.selected_profile_unknown", "Selected profile: unknown")
			return view, err
		}
		if data.CurrentError != "" {
			view.Context = menuText(language, "menu.profile.profile_list_page.selected_profile_unavailable", "Selected profile unavailable: ") + data.CurrentError
		} else if data.Current != "" {
			view.Context = menuText(language, "menu.profile.profile_list_page.selected_profile", "Selected profile: ") + data.Current
		}
		if len(data.Profiles) == 0 {
			view.Context += menuText(language, "menu.profile.profile_list_page.no_profiles_found_import_one_with_nagi_profile_i", "\nNo profiles found. Import one with `nagi profile import NAME FILE`.")
		}
		for _, name := range data.Profiles {
			name := name
			label := name
			if name == data.Current {
				label += menuText(language, "menu.profile.selected", " (selected)")
			}
			view.Items = append(view.Items, Item{ID: "profile.profileListPage.item3", Name: name, Label: label, Selected: data.CurrentError == "" && name == data.Current, Next: func() *Page { return localizedprofileDetailPage(language, name) }})
		}
		return view, nil
	}}
}

func localizedprofileDetailPage(language i18n.Context, name string) *Page {
	return &Page{ID: "profile.profileDetailPage.page1", Title: menuText(language, "menu.profile.profile_detail_page.profile", "Profile: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Actions: []Item{
			{ID: "profile.profileDetailPage.item2", Label: menuText(language, "menu.profile.profile_detail_page.use_profile", "Use profile"), Args: []string{"profile", "use", name}},
			{ID: "profile.profileDetailPage.item3", Label: menuText(language, "menu.profile.profile_detail_page.export_configuration", "Export configuration"), Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "export", name) }},
			localizedsubmenu(language, localizedactionPage(language, menuText(language, "menu.profile.profile_detail_page.backup_restore", "Backup & Restore"), Item{ID: "profile.profileDetailPage.item4", Label: menuText(language, "menu.profile.profile_detail_page.back_up_configuration", "Back up configuration"), Args: []string{"profile", "backup", name}}, Item{ID: "profile.profileDetailPage.item5", Label: menuText(language, "menu.profile.profile_detail_page.restore_latest_backup", "Restore latest backup"), Args: []string{"profile", "restore", name}, Impact: menuText(language, "menu.profile.profile_detail_page.replaces_configuration_reloads_if_active", "Replaces configuration; reloads if active")}, Item{ID: "profile.profileDetailPage.item6", Label: menuText(language, "menu.profile.profile_detail_page.compare_with_backup_or_another_profile", "Compare with backup or another profile"), Action: func(ctx context.Context, s *Session) error { return diffProfile(ctx, s, name) }})),
			{ID: "profile.profileDetailPage.item7", Label: menuText(language, "menu.profile.profile_detail_page.local_overrides", "Local Overrides"), Next: func() *Page { return localizedlocalOverridesPage(language, name) }},
			{ID: "profile.profileDetailPage.item8", Label: menuText(language, "menu.profile.profile_detail_page.remove_profile", "Remove profile"), Navigate: localizedremoveResource(language, "profile", name), Impact: menuText(language, "menu.profile.profile_detail_page.deletes_configuration_backups_local_overrides_cu", "Deletes configuration, backups, local overrides, custom rules, and saved choices; keeps subscriptions")},
		}}
		var data menuProfiles
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "profile", "list"), &data); err != nil {
			view.Context = menuText(language, "menu.profile.profile_detail_page.selected_profile_unknown", "Selected profile: unknown (") + menuURLPattern.ReplaceAllString(err.Error(), "[redacted URL]") + ")"
		} else if data.CurrentError != "" {
			view.Context = menuText(language, "menu.profile.profile_detail_page.selected_profile_unknown", "Selected profile: unknown (") + menuURLPattern.ReplaceAllString(data.CurrentError, "[redacted URL]") + ")"
		} else if data.Current != "" {
			view.Context = menuText(language, "menu.profile.profile_detail_page.selected_profile", "Selected profile: ") + data.Current
		} else {
			view.Context = menuText(language, "menu.profile.profile_detail_page.selected_profile_none", "Selected profile: none")
		}
		return view, nil
	}}
}

func localizedlocalOverridesPage(language i18n.Context, name string) *Page {
	return &Page{ID: "profile.localOverridesPage.page1", Title: menuText(language, "menu.profile.local_overrides_page.local_overrides", "Local Overrides"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		defaultApplied := false
		var status menuDefaultOverrideStatus
		statusErr := localizeddecodeMenuResult(language, s.Fetch(ctx, "profile", "override", "default-status", name), &status)
		statusAvailable := statusErr == nil
		if statusAvailable {
			defaultApplied = status.Applied
		}
		defaultItem := Item{ID: "profile.localOverridesPage.item2", Label: menuText(language, "menu.profile.local_overrides_page.apply_default_runtime_settings", "Apply default runtime settings"),
			Selected: statusAvailable && defaultApplied,
			Impact:   menuText(language, "menu.profile.local_overrides_page.copies_safe_settings_from_the_default_profile_ke", "Copies safe settings from the default profile; keeps nodes, groups, and rules"),
			Action: func(ctx context.Context, s *Session) error {
				return toggleDefaultOverride(ctx, s, name, defaultApplied)
			},
		}
		if defaultApplied {
			defaultItem.Label = menuText(language, "menu.profile.local_overrides_page.disable_default_runtime_settings", "Disable default runtime settings")
			defaultItem.Impact = menuText(language, "menu.profile.local_overrides_page.restores_the_local_override_saved_before_applyin", "Restores the local override saved before applying defaults")
		} else if status.DNSProxyGroup != "" {
			defaultItem.Impact += menuText(language, "menu.profile.dns_via", "; DNS via ") + status.DNSProxyGroup
		}
		view := PageView{Actions: []Item{
			{ID: "profile.localOverridesPage.item3", Label: menuText(language, "menu.profile.local_overrides_page.show_local_override", "Show local override"), Args: []string{"profile", "override", "show", name}},
			{ID: "profile.localOverridesPage.item4", Label: menuText(language, "menu.profile.local_overrides_page.set_local_override_from_file", "Set local override from file"), Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "override", name) }},
			{ID: "profile.localOverridesPage.item5", Label: menuText(language, "menu.profile.local_overrides_page.clear_local_override", "Clear local override"), Args: []string{"profile", "override", "clear", name}, Impact: menuText(language, "menu.profile.local_overrides_page.restores_original_settings_reloads_if_active", "Restores original settings; reloads if active")},
		}}
		if statusAvailable {
			view.Actions = append([]Item{defaultItem}, view.Actions...)
		} else {
			view.Context = menuText(language, "menu.profile.local_overrides_page.default_runtime_settings_unavailable", "Default runtime settings: unavailable")
		}
		return view, statusErr
	}}
}

func toggleDefaultOverride(ctx context.Context, s *Session, name string, expected bool) error {
	var status menuDefaultOverrideStatus
	if err := localizeddecodeMenuResult(s.language,
		s.Fetch(ctx, "profile", "override", "default-status", name), &status); err != nil {
		return err
	}
	if status.Applied != expected {
		return errors.New(menuText(s.language, "menu.profile.toggle_default_override.default_runtime_settings_changed_review_the_refr", "Default runtime settings changed. Review the refreshed menu before choosing again."))
	}
	var result Result
	if status.Applied {
		result = s.Run(ctx, "profile", "override", "restore-default", name)
	} else {
		if !status.DNSProxyRequired || status.DNSProxyGroup != "" && localizedmenuHasGroup(s.language, status.DNSProxyGroups, status.DNSProxyGroup) {
			result = s.Run(ctx, "profile", "override", "apply-default", name)
		} else {
			if len(status.DNSProxyGroups) == 0 {
				return errors.New(menuText(s.language, "menu.profile.toggle_default_override.no_eligible_proxy_group_in_this_profile_add_a_pr", "No eligible proxy group in this profile. Add a proxy group before applying default runtime settings."))
			}
			group, err := s.Select(menuText(s.language, "menu.profile.toggle_default_override.dns_proxy_group_for", "DNS proxy group for ")+
				name, status.DNSProxyGroups)
			if err != nil {
				return err
			}
			result = s.Run(ctx, "profile", "override", "apply-default", name, "--dns-proxy", group)
		}
	}
	if result.Code != 0 && result.Error != nil {
		return errors.New(result.errorText(s.language))
	}
	if result.Code != 0 {
		return fmt.Errorf("%s", menuFormat(s.language, "menu.profile.toggle_default_override.command_exited_with_status_d", "command exited with status %d", result.Code))
	}
	return nil
}

func localizedmenuHasGroup(language i18n.Context, groups []string, name string) bool {
	for _, group := range groups {
		if group == name {
			return true
		}
	}
	return false
}

func localizedvalidateMenuName(language i18n.Context, value string) error {
	if len(value) > 128 || !menuNamePattern.MatchString(value) {
		return fmt.Errorf("%s", menuText(language, "menu.profile.name_validation", "use 1\u2013128 letters, digits, _ or -"))
	}
	return nil
}

func localizednameField(language i18n.Context, label string) Field {
	return Field{Key: "name", Label: label, Required: true, Validate: func(value string) error {
		return localizedvalidateMenuName(

			language,
			value)
	},
	}
}

func importProfile(ctx context.Context, s *Session) (Navigation, error) {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{localizednameField(s.language, menuText(s.language, "menu.profile.import_profile.new_profile_name", "New profile name")), {Key: "file", Label: menuText(s.language, "menu.profile.import_profile.local_yaml_file", "Local YAML file"), Required: true}}, values, func(v map[string]string) Result { return s.Run(ctx, "profile", "import", v["name"], v["file"]) })
	if err != nil {
		return Navigation{}, err
	}
	return Navigation{Next: localizedprofileDetailPage(s.language,
		values["name"])}, nil
}

func profileFileAction(ctx context.Context, s *Session, operation, name string) error {
	label := menuText(s.language, "menu.profile.profile_file_action.destination_file_must_not_exist", "Destination file (must not exist)")

	if operation == "override" {
		label = menuText(s.language, "menu.profile.profile_file_action.local_override_yaml_file", "Local override YAML file")

	}
	_, err := s.Form(ctx, []Field{{Key: "file", Label: label, Required: true}}, nil, func(v map[string]string) Result {
		if operation == "override" {
			return s.Run(ctx, "profile", "override", "set", name, v["file"])
		}
		return s.Run(ctx, "profile", "export", name, v["file"])
	})
	return err
}

func localizedremoveResource(language i18n.Context, kind, name string) func(context.Context, *Session) (Navigation, error) {
	return func(ctx context.Context, s *Session) (Navigation, error) {
		result := s.Run(ctx, kind, "remove", name)
		return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
	}
}

func diffProfile(ctx context.Context, s *Session, name string) error {
	var data menuProfiles
	if err := localizeddecodeMenuResult(s.language,
		s.Fetch(ctx, "profile", "list"), &data); err != nil {
		return err
	}
	choices := []string{"backup"}
	for _, other := range data.Profiles {
		if other != name {
			choices = append(choices, other)
		}
	}
	other, err := s.Select(menuText(s.language, "menu.profile.diff_profile.compare_with", "Compare with"), choices)
	if err != nil {
		return err
	}
	if other != "" {
		s.Run(ctx, "profile", "diff", name, other)
	}
	return nil
}

func localizedsubscriptionListPage(language i18n.Context) *Page {
	return &Page{ID: "profile.subscriptionListPage.page1", Title: menuText(language, "menu.profile.subscription_list_page.subscriptions", "Subscriptions"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: menuText(language, "menu.profile.subscription_list_page.no_subscriptions_configured_choose_add_and_apply", "No subscriptions configured. Choose Add and Apply subscription."), Actions: []Item{{ID: "profile.subscriptionListPage.item2", Label: menuText(language, "menu.profile.subscription_list_page.add_and_apply_subscription", "Add and Apply subscription"), Impact: menuText(language, "menu.profile.subscription_list_page.updates_subscription_retains_local_settings_and", "Updates subscription; retains local settings and nodes; clears saved node selections"), Navigate: addSubscription}}}
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			return view, err
		}
		for _, entry := range data.Subscriptions {
			entry := entry
			label := entry.Name
			if entry.ScheduleSeconds > 0 {
				label += fmt.Sprintf(" (every %ds)", entry.ScheduleSeconds)
			}
			if entry.UpdatedAt == "" {
				label += menuText(language, "menu.profile.subscription_list_page.never_downloaded", " (never downloaded)")
			} else {
				label += " (updated " + localizedsubscriptionTime(language, entry.UpdatedAt) + ")"
			}
			view.Items = append(view.Items, Item{ID: "profile.subscriptionListPage.item3", Name: entry.Name, Label: label, Next: func() *Page { return localizedsubscriptionDetailPage(language, entry.Name) }})
		}
		return view, nil
	}}
}

func addSubscription(ctx context.Context, s *Session) (Navigation, error) {
	values := map[string]string{"seconds": "3600"}
	_, err := s.Form(ctx, []Field{localizednameField(s.language, menuText(s.language, "menu.profile.add_subscription.new_subscription_name", "New subscription name")), {Key: "url", Label: menuText(s.language, "menu.profile.add_subscription.subscription_http_s_url", "Subscription HTTP(S) URL"), Required: true, Validate: func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return fmt.Errorf("%s", menuText(s.language, "menu.profile.add_subscription.enter_an_http_s_url_without_user_credentials", "enter an HTTP(S) URL without user credentials"))
		}
		return nil
	}}, localizedsecondsField(s.language, menuText(s.language, "menu.profile.add_subscription.update_interval_seconds_default_3600_off_disable", "Update interval seconds (default 3600; off disables)"), true), localizedtestURLField(s.language,
		false)}, values, func(v map[string]string) Result {
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
	return Navigation{Next: localizedsubscriptionDetailPage(s.language,
		values["name"])}, nil
}

func subscriptionSchedule(ctx context.Context, s *Session, name string) error {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{{Key: "seconds", Label: menuText(s.language, "menu.profile.subscription_schedule.interval_seconds", "Interval seconds"), Required: true}}, values, func(v map[string]string) Result {
		return s.Run(ctx, "subscription", "schedule", name, v["seconds"])
	})
	return err
}

func subscriptionKey(ctx context.Context, s *Session, name string) error {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{{Key: "file", Label: menuText(s.language, "menu.profile.subscription_key.identity_file_path", "Identity file path"), Required: true}}, values, func(v map[string]string) Result {
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
		state := menuText(s.language, "menu.profile.update_and_apply_subscription.download_failed_apply_was_not_attempted", "Download failed; apply was not attempted.")

		if added {
			state = menuText(s.language, "menu.profile.update_and_apply_subscription.subscription_saved_but_download_failed_apply_was", "Subscription saved, but download failed; apply was not attempted.")

		}
		s.feedback(feedbackWarning, state)
		s.ShowResult([]string{"subscription", "update", name}, result)
		return
	}
	result = s.Fetch(ctx, "subscription", "apply", name)
	if result.Code != 0 || result.Error != nil {
		s.feedback(feedbackWarning, menuText(s.language, "menu.profile.update_and_apply_subscription.downloaded_but_apply_failed_cache_retained_retry", "Downloaded, but apply failed. Cache retained; retry with `nagi subscription apply NAME`."))
		s.ShowResult([]string{"subscription", "apply", name}, result)
		return
	}
	s.feedback(feedbackSuccess, menuText(s.language, "menu.profile.update_and_apply_subscription.subscription_updated_and_applied", "Subscription updated and applied."))
}

func localizedsubscriptionDetailPage(language i18n.Context, name string) *Page {
	return &Page{ID: "profile.subscriptionDetailPage.page1", Title: menuText(language, "menu.profile.subscription_detail_page.subscription", "Subscription: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Actions: []Item{
			{ID: "profile.subscriptionDetailPage.item2", Label: menuText(language, "menu.profile.subscription_detail_page.update_and_apply", "Update and Apply"), Impact: menuText(language, "menu.profile.subscription_detail_page.updates_subscription_retains_local_settings_and", "Updates subscription; retains local settings and nodes; clears saved node selections"), Action: func(ctx context.Context, s *Session) error {
				updateAndApplySubscription(ctx, s, name, false)
				return nil
			}},
			{ID: "profile.subscriptionDetailPage.item3", Label: menuText(language, "menu.profile.subscription_detail_page.automatic_updates", "Automatic Updates"), Next: func() *Page { return localizedsubscriptionSettingsPage(language, name, false) }},
			{ID: "profile.subscriptionDetailPage.item4", Label: menuText(language, "menu.profile.subscription_detail_page.automatic_latency_checks", "Automatic latency checks"), Next: func() *Page { return localizedhealthcheckPage(language, "subscription", name) }},
			{ID: "profile.subscriptionDetailPage.item5", Label: menuText(language, "menu.profile.subscription_detail_page.encryption_settings", "Encryption Settings"), Next: func() *Page { return localizedsubscriptionSettingsPage(language, name, true) }},
			{ID: "profile.subscriptionDetailPage.item6", Label: menuText(language, "menu.profile.subscription_detail_page.remove_subscription_and_cache", "Remove subscription and cache"), Navigate: localizedremoveResource(language, "subscription", name)},
		}}
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			view.Context = menuText(language, "menu.profile.subscription_detail_page.subscription_metadata_unknown", "Subscription metadata: unknown (") + menuURLPattern.ReplaceAllString(err.Error(), "[redacted URL]") + ")"
		} else {
			view.Context = menuText(language, "menu.profile.subscription_detail_page.subscription_metadata_unknown_record_unavailable", "Subscription metadata: unknown (record unavailable)")
			for _, entry := range data.Subscriptions {
				if entry.Name == name {
					view.Context = localizedsubscriptionMetadata(language, entry)
					if entry.PauseReason != "" {
						view.Context += menuText(language, "menu.profile.subscription_detail_page.automatic_apply_paused", "\nAutomatic apply paused: ") + entry.PauseReason
					}
					if entry.LastFailureStage != "" {
						view.Context += menuText(language, "menu.profile.subscription_detail_page.last_scheduler_failure", "\nLast scheduler failure: ") + entry.LastFailureStage + "/" + entry.LastFailureReason
					}
					break
				}
			}
		}
		return view, nil
	}}
}

// Subscription settings read only public metadata, never the identity contents.
func localizedsubscriptionSettingsPage(language i18n.Context, name string, encryption bool) *Page {
	title := menuText(language, "menu.profile.subscription_settings_page.automatic_updates", "Automatic Updates")
	if encryption {
		title = menuText(language, "menu.profile.subscription_settings_page.encryption_settings", "Encryption Settings")
	}
	return &Page{ID: "profile.subscriptionSettingsPage.page1", Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.profile.subscription_settings_page.automatic_updates_unavailable", "Automatic updates: unavailable"), Actions: []Item{
			{ID: "profile.subscriptionSettingsPage.item2", Label: menuText(language, "menu.profile.subscription_settings_page.set_update_interval", "Set update interval"), Action: func(ctx context.Context, s *Session) error { return subscriptionSchedule(ctx, s, name) }},
		}}
		if encryption {
			view.Context = menuText(language, "menu.profile.subscription_settings_page.identity_file_unavailable", "Identity file: unavailable")
			view.Actions = []Item{{ID: "profile.subscriptionSettingsPage.item3", Label: menuText(language, "menu.profile.subscription_settings_page.set_identity_file", "Set identity file"), Action: func(ctx context.Context, s *Session) error { return subscriptionKey(ctx, s, name) }}}
		}
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := localizeddecodeMenuResult(language, s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			return view, err
		}
		for _, entry := range data.Subscriptions {
			if entry.Name != name {
				continue
			}
			if encryption {
				view.Context = menuText(language, "menu.profile.subscription_settings_page.identity_file_not_configured", "Identity file: not configured")
				if entry.KeyConfigured {
					view.Context = menuText(language, "menu.profile.subscription_settings_page.identity_file_configured", "Identity file: configured")
					view.Actions[0].Label = menuText(language, "menu.profile.subscription_settings_page.replace_identity_file", "Replace identity file")
					view.Actions = append(view.Actions, Item{ID: "profile.subscriptionSettingsPage.item4", Label: menuText(language, "menu.profile.subscription_settings_page.clear_encryption_key", "Clear encryption key"), Args: []string{"subscription", "key", name, "clear"}})
				}
			} else {
				view.Context = menuText(language, "menu.profile.subscription_settings_page.automatic_updates_disabled", "Automatic updates: disabled")
				if entry.ScheduleSeconds > 0 {
					view.Context = menuFormat(language, "menu.profile.subscription_settings_page.download_update_interval_d_seconds", "Download update interval: %d seconds", entry.ScheduleSeconds)
					view.Actions = append(view.Actions, Item{ID: "profile.subscriptionSettingsPage.item5", Label: menuText(language, "menu.profile.subscription_settings_page.disable_automatic_updates", "Disable automatic updates"), Args: []string{"subscription", "schedule", name, "off"}})
				}
			}
			return view, nil
		}
		return view, errors.New(menuText(language, "menu.profile.subscription_settings_page.subscription_record_unavailable", "subscription record unavailable"))
	}}
}

func localizedsubscriptionMetadata(language i18n.Context, entry menuSubscription) string {
	updated, expiry := localizedsubscriptionTime(language, entry.UpdatedAt), localizedsubscriptionTime(language, entry.ExpiresAt)
	if entry.UpdatedAt == "" {
		updated = menuText(language, "menu.profile.subscription_metadata.never_downloaded", "never downloaded")
	}
	quota := menuState(language, "unknown")
	if entry.Total > 0 {
		remaining := entry.Total - entry.Upload - entry.Download
		if remaining < 0 {
			remaining = 0
		}
		quota = menuFormat(language, "menu.profile.subscription_metadata.s_available_s_total_s_uploaded_s_downloaded", "%s available / %s total (%s uploaded, %s downloaded)", localizedsubscriptionBytes(language, remaining), localizedsubscriptionBytes(language, entry.Total), localizedsubscriptionBytes(language, entry.Upload), localizedsubscriptionBytes(language, entry.Download))
	}
	return menuText(language, "menu.profile.subscription_metadata.updated", "Updated: ") + updated + menuText(language, "menu.profile.subscription_metadata.quota",

		// subscriptionTime formats command timestamps only at the menu boundary.
		"\nQuota: ") + quota + menuText(language, "menu.profile.subscription_metadata.expires", "\nExpires: ") + expiry
}

func localizedsubscriptionTime(language i18n.Context, raw string) string {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return menuState(language, "unknown")
	}
	return value.Local().Format("2006-01-02 15:04")
}

func localizedsubscriptionBytes(language i18n.Context, bytes int64) string {
	if bytes < 0 {
		return menuState(language, "unknown")
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{menuText(language, "menu.profile.subscription_bytes.kib", "KiB"), menuText(language, "menu.profile.subscription_bytes.mib", "MiB"), menuText(language, "menu.profile.subscription_bytes.gib", "GiB"), menuText(language, "menu.profile.subscription_bytes.tib", "TiB"), menuText(language, "menu.profile.subscription_bytes.pib", "PiB"), menuText(language, "menu.profile.subscription_bytes.eib", "EiB")}
	value := float64(bytes) / 1024
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), menuText(language, "menu.profile.subscription_bytes.", ".")) + " " + units[unit]
}
func ProfilesAndSubscriptionsPage() *Page {
	return localizedProfilesAndSubscriptionsPage(i18n.New(i18n.English))
}

func ProfilePage() *Page {
	return localizedProfilePage(i18n.New(i18n.English))
}
func profileListPage() *Page {
	return localizedprofileListPage(i18n.New(i18n.English))
}
func profileDetailPage(name string) *Page {
	return localizedprofileDetailPage(i18n.New(i18n.English), name)
}
func localOverridesPage(name string) *Page {
	return localizedlocalOverridesPage(i18n.New(i18n.English), name)
}
func menuHasGroup(groups []string, name string) bool {
	return localizedmenuHasGroup(i18n.New(i18n.English), groups, name)
}
func validateMenuName(value string) error {
	return localizedvalidateMenuName(i18n.New(i18n.English), value)
}
func nameField(label string) Field {
	return localizednameField(i18n.New(i18n.English), label)
}
func removeResource(kind,
	name string) func(context.
	Context, *Session) (Navigation, error) {
	return localizedremoveResource(i18n.New(i18n.English), kind, name)
}
func subscriptionListPage() *Page {
	return localizedsubscriptionListPage(i18n.New(i18n.English))
}
func subscriptionDetailPage(name string) *Page {
	return localizedsubscriptionDetailPage(i18n.New(i18n.English), name)
}

func subscriptionSettingsPage(name string, encryption bool) *Page {
	return localizedsubscriptionSettingsPage(i18n.New(i18n.English), name, encryption)
}
func subscriptionMetadata(entry menuSubscription) string {
	return localizedsubscriptionMetadata(i18n.New(i18n.English), entry)
}

func subscriptionTime(raw string) string {
	return localizedsubscriptionTime(i18n.New(i18n.English), raw)
}
func subscriptionBytes(bytes int64) string {
	return localizedsubscriptionBytes(i18n.New(i18n.English), bytes)
}
