package menu

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
)

type menuProfiles struct {
	Profiles     []string `json:"profiles"`
	Current      string   `json:"current"`
	CurrentError string   `json:"current_error"`
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

type menuPreview struct {
	Name    string   `json:"name"`
	Format  string   `json:"format"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
	Changes []struct {
		Path     string `json:"path"`
		Kind     string `json:"kind"`
		Behavior string `json:"behavior"`
	} `json:"changes"`
	RemoteNodesCompared bool `json:"remote_nodes_compared"`
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
			view.Items = append(view.Items, Item{Name: name, Label: label, Next: func() *Page { return profileDetailPage(name) }})
		}
		return view, nil
	}}
}

func profileDetailPage(name string) *Page {
	return &Page{Title: "Profile: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Actions: []Item{
			{Label: "Use profile", Args: []string{"profile", "use", name}},
			{Label: "Export source YAML", Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "export", name) }},
			submenu(actionPage("Backup & Restore", Item{Label: "Back up source YAML", Args: []string{"profile", "backup", name}}, Item{Label: "Restore latest backup", Args: []string{"profile", "restore", name}, Impact: "Replaces source YAML; reloads when selected and running"}, Item{Label: "Compare with backup or another profile", Action: func(ctx context.Context, s *Session) error { return diffProfile(ctx, s, name) }})),
			submenu(actionPage("Local Overrides", Item{Label: "Show local override", Args: []string{"profile", "override", "show", name}}, Item{Label: "Set local override from file", Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "override", name) }}, Item{Label: "Clear local override", Args: []string{"profile", "override", "clear", name}, Impact: "Restores source settings; reloads when selected and running"})),
			{Label: "Remove profile", Navigate: removeResource("profile", name), Impact: "Deletes source YAML; backup and override remain"},
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
		view := PageView{Empty: "No subscriptions configured. Choose Add subscription.", Actions: []Item{{Label: "Add subscription", Navigate: addSubscription}}}
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
				label += " (updated " + entry.UpdatedAt + ")"
			}
			view.Items = append(view.Items, Item{Name: entry.Name, Label: label, Next: func() *Page { return subscriptionDetailPage(entry.Name) }})
		}
		return view, nil
	}}
}

func addSubscription(ctx context.Context, s *Session) (Navigation, error) {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{nameField("New subscription name"), {Key: "url", Label: "Subscription HTTP(S) URL", Required: true, Validate: func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return fmt.Errorf("enter an HTTP(S) URL without user credentials")
		}
		return nil
	}}}, values, func(v map[string]string) Result { return s.Run(ctx, "subscription", "add", v["name"], v["url"]) })
	if err != nil {
		return Navigation{}, err
	}
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

func subscriptionDetailPage(name string) *Page {
	var download *menuPreview
	return &Page{Title: "Subscription: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		changes := []Item{{Label: "Compare with target profile", Next: func() *Page { return subscriptionPreviewPage(name) }}}
		if download != nil {
			snapshot := *download
			changes = append([]Item{{Label: "Compare with previous download", Next: func() *Page { return previewNamesPage("Download changes vs previous cache", snapshot) }}}, changes...)
		}

		view := PageView{Actions: []Item{
			{Label: "Download subscription", Impact: "Downloads only; does not apply, start mihomo or enable traffic access", Action: func(ctx context.Context, s *Session) error {
				result := s.Run(ctx, "subscription", "update", name)
				if result.Code != 0 || result.Error != nil {
					return nil
				}
				var data struct {
					Preview *menuPreview `json:"preview"`
				}
				if err := decodeMenuResult(result, &data); err != nil {
					return err
				}
				download = data.Preview
				return nil
			}},
			{Label: "Apply downloaded subscription", Args: []string{"subscription", "apply", name}, Impact: "Replaces target source configuration, clears local overrides and saved node selections; does not start mihomo or enable traffic access"},
			submenu(actionPage("View Changes", changes...)),
			{Label: "Automatic Updates", Next: func() *Page {
				return actionPage("Automatic Updates",
					Item{Label: "Set update interval", Action: func(ctx context.Context, s *Session) error { return subscriptionSchedule(ctx, s, name) }},
					Item{Label: "Disable automatic updates", Args: []string{"subscription", "schedule", name, "off"}})
			}},
			{Label: "Encryption Settings", Next: func() *Page {
				return actionPage("Encryption Settings",
					Item{Label: "Set identity file", Action: func(ctx context.Context, s *Session) error { return subscriptionKey(ctx, s, name) }},
					Item{Label: "Clear encryption key", Args: []string{"subscription", "key", name, "clear"}})
			}},
			{Label: "Remove subscription and cache", Navigate: removeResource("subscription", name), Impact: "Deletes subscription record and downloaded cache; keeps local profile"},
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
		var preview menuPreview
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "preview", name), &preview); err != nil {
			view.Context += "\nApplication preview unavailable: " + menuURLPattern.ReplaceAllString(err.Error(), "[redacted URL]") + ". Apply can still validate and use a valid cache to repair the target profile."
		} else {
			view.Context += "\nApplication changes (cache vs target profile): " + previewSummary(preview)
		}
		if download != nil {
			view.Context += "\nLast download changes (new cache vs previous cache): " + previewSummary(*download)
		}
		return view, nil
	}}
}

func subscriptionMetadata(entry menuSubscription) string {
	updated, expiry := entry.UpdatedAt, entry.ExpiresAt
	if updated == "" {
		updated = "never downloaded"
	}
	if expiry == "" {
		expiry = "unknown"
	}
	quota := "unknown"
	if entry.Total > 0 {
		remaining := entry.Total - entry.Upload - entry.Download
		if remaining < 0 {
			remaining = 0
		}
		quota = fmt.Sprintf("%d bytes available / %d bytes total (%d uploaded, %d downloaded)", remaining, entry.Total, entry.Upload, entry.Download)
	}
	return "Updated: " + updated + "\nQuota: " + quota + "\nExpires: " + expiry
}

func subscriptionPreviewPage(name string) *Page {
	return &Page{Title: "Application changes vs target profile: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var preview menuPreview
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "preview", name), &preview); err != nil {
			return PageView{}, err
		}
		return previewView(preview), nil
	}}
}

func previewNamesPage(title string, preview menuPreview) *Page {
	return &Page{Title: title, Load: func(context.Context, *Session) (PageView, error) { return previewView(preview), nil }}
}

func previewSummary(preview menuPreview) string {
	return fmt.Sprintf("%d nodes added, %d removed, %d changed; %d configuration changes. Open the changes list for names and paths.", len(preview.Added), len(preview.Removed), len(preview.Changed), len(preview.Changes))
}

func previewView(preview menuPreview) PageView {
	view := PageView{Context: previewSummary(preview), Empty: "No changes."}
	if !preview.RemoteNodesCompared {
		view.Context += "\nRemote provider nodes are not compared."
	}
	for _, category := range []struct {
		name  string
		items []string
	}{{"Added", preview.Added}, {"Removed", preview.Removed}, {"Changed", preview.Changed}} {
		category := category
		for _, name := range category.items {
			safe := menuURLPattern.ReplaceAllString(name, "[redacted URL]")
			view.Items = append(view.Items, Item{Name: safe, Label: category.name + ": " + safe, Action: func(_ context.Context, s *Session) error { s.feedback(category.name + ": " + safe); return nil }})
		}
	}
	for _, change := range preview.Changes {
		label := menuURLPattern.ReplaceAllString(change.Kind+": "+change.Path+" — "+change.Behavior, "[redacted URL]")
		view.Items = append(view.Items, Item{Name: label, Label: label, Action: func(_ context.Context, s *Session) error { s.feedback(label); return nil }})
	}
	return view
}
