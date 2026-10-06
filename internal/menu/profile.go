package menu

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type menuProfiles struct {
	Profiles     []string `json:"profiles"`
	Current      string   `json:"current"`
	CurrentError string   `json:"current_error"`
}

type menuSubscription struct {
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at"`
	ExpiresAt string `json:"expires_at"`
	Upload    int64  `json:"upload"`
	Download  int64  `json:"download"`
	Total     int64  `json:"total"`
}

type menuPreview struct {
	Name    string   `json:"name"`
	Format  string   `json:"format"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}

var menuURLPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)

func init() { RegisterPage("profile", ProfilePage) }

// ProfilePage groups local profile and subscription operations.
func ProfilePage() *Page {
	return &Page{Title: "Profiles and subscriptions", Help: []string{"profile"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Items: []Item{
			{Label: "Profiles", Next: profileListPage},
			{Label: "Subscriptions", Next: subscriptionListPage},
		}}, nil
	}}
}

func profileListPage() *Page {
	return &Page{Title: "Profiles", Help: []string{"profile", "list"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data menuProfiles
		if err := decodeMenuResult(s.Fetch(ctx, "profile", "list"), &data); err != nil {
			return PageView{}, err
		}
		view := PageView{Empty: "No profiles found. Import one with `nagi profile import NAME FILE`.", Items: []Item{
			{Label: "List profiles", Args: []string{"profile", "list"}},
			{Label: "Import a profile", Action: importProfile},
		}}
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
			view.Items = append(view.Items, Item{Label: label, Next: func() *Page { return profileDetailPage(name) }})
		}
		return view, nil
	}}
}

func profileDetailPage(name string) *Page {
	return &Page{Title: "Profile: " + name, Help: []string{"profile", "use"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Items: []Item{
			{Label: "Use profile", Args: []string{"profile", "use", name}},
			{Label: "Export source YAML", Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "export", name) }},
			{Label: "Back up source YAML", Args: []string{"profile", "backup", name}},
			{Label: "Restore latest backup", Args: []string{"profile", "restore", name}, Confirm: "Replace this profile with its backup?"},
			{Label: "Compare with backup or another profile", Action: func(ctx context.Context, s *Session) error { return diffProfile(ctx, s, name) }},
			{Label: "Show local override", Args: []string{"profile", "override", "show", name}},
			{Label: "Set local override from file", Action: func(ctx context.Context, s *Session) error { return profileFileAction(ctx, s, "override", name) }},
			{Label: "Clear local override", Args: []string{"profile", "override", "clear", name}, Confirm: "Clear this profile's local override?"},
			{Label: "Remove profile", Args: []string{"profile", "remove", name}, Confirm: "Remove this profile? Its backup will remain."},
		}}, nil
	}}
}

func importProfile(ctx context.Context, s *Session) error {
	name, err := s.Prompt("New profile name (letters, digits, _ or -)")
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		s.WriteLine("A profile name is required.")
		return nil
	}
	file, err := s.Prompt("Local YAML file")
	if err != nil {
		return err
	}
	if strings.TrimSpace(file) == "" {
		s.WriteLine("A local YAML file is required.")
		return nil
	}
	s.Run(ctx, "profile", "import", name, file)
	return nil
}

func profileFileAction(ctx context.Context, s *Session, operation, name string) error {
	label := "Destination file (must not exist)"
	if operation == "override" {
		label = "Local override YAML file"
	}
	file, err := s.Prompt(label)
	if err != nil {
		return err
	}
	if strings.TrimSpace(file) == "" {
		s.WriteLine("A file path is required.")
		return nil
	}
	if operation == "override" {
		s.Run(ctx, "profile", "override", "set", name, file)
	} else {
		s.Run(ctx, "profile", "export", name, file)
	}
	return nil
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
	return &Page{Title: "Subscriptions", Help: []string{"subscription", "list"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data struct {
			Subscriptions []menuSubscription `json:"subscriptions"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "list"), &data); err != nil {
			return PageView{}, err
		}
		view := PageView{Empty: "No subscriptions configured. Add one with `nagi subscription add NAME URL`.", Items: []Item{
			{Label: "List subscriptions", Args: []string{"subscription", "list"}},
			{Label: "Add subscription", Action: addSubscription},
		}}
		if len(data.Subscriptions) == 0 {
			view.Context = "No subscriptions configured. Add one with `nagi subscription add NAME URL`."
		}
		for _, entry := range data.Subscriptions {
			entry := entry
			label := entry.Name
			if entry.UpdatedAt == "" {
				label += " (never updated)"
			} else {
				label += " (updated " + entry.UpdatedAt + ")"
			}
			view.Items = append(view.Items, Item{Label: label, Next: func() *Page { return subscriptionDetailPage(entry.Name) }})
		}
		return view, nil
	}}
}

func addSubscription(ctx context.Context, s *Session) error {
	name, err := s.Prompt("New subscription name (letters, digits, _ or -)")
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		s.WriteLine("A subscription name is required.")
		return nil
	}
	url, err := s.Prompt("Subscription HTTP(S) URL")
	if err != nil {
		return err
	}
	if strings.TrimSpace(url) == "" {
		s.WriteLine("A subscription URL is required.")
		return nil
	}
	s.Run(ctx, "subscription", "add", name, url)
	return nil
}

func subscriptionDetailPage(name string) *Page {
	return &Page{Title: "Subscription: " + name, Help: []string{"subscription", "preview"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "No cached preview available. Update this subscription first.", Items: []Item{
			{Label: "Update cache and show node changes", Action: func(ctx context.Context, s *Session) error { return updateSubscription(ctx, s, name) }},
			{Label: "Preview changes against profile", Action: func(ctx context.Context, s *Session) error { return previewSubscription(ctx, s, name) }},
		}}
		var preview menuPreview
		if err := decodeMenuResult(s.Fetch(ctx, "subscription", "preview", name), &preview); err == nil {
			view.Context = strings.Join(previewLines(preview), "\n")
			view.Items = append(view.Items, Item{Label: "Apply cached subscription", Args: []string{"subscription", "apply", name}, Confirm: "Apply cached subscription as a profile?"})
		} else {
			view.Context = "Preview unavailable: " + menuURLPattern.ReplaceAllString(err.Error(), "[redacted URL]") + ". Update the cache or retry preview before applying."
		}
		view.Items = append(view.Items, Item{Label: "Remove subscription and cache", Args: []string{"subscription", "remove", name}, Confirm: "Remove this subscription and its cache?"})
		return view, nil
	}}
}

func updateSubscription(ctx context.Context, s *Session, name string) error {
	result := s.Fetch(ctx, "subscription", "update", name)
	if result.Code != 0 || result.Error != nil {
		s.showResult(result)
		return nil
	}
	var data struct {
		Bytes   int         `json:"bytes"`
		Preview menuPreview `json:"preview"`
	}
	if err := decodeMenuResult(result, &data); err != nil {
		return err
	}
	s.Printf("Updated cache for %s (%d bytes).\n", name, data.Bytes)
	if data.Preview.Name != "" {
		printMenuPreview(s, data.Preview)
	} else {
		s.WriteLine("Update succeeded; open Preview changes to inspect the cache against the profile.")
	}
	s.WriteLine("Review the preview before applying the cached subscription.")
	return nil
}

func previewSubscription(ctx context.Context, s *Session, name string) error {
	var preview menuPreview
	if err := decodeMenuResult(s.Fetch(ctx, "subscription", "preview", name), &preview); err != nil {
		return err
	}
	printMenuPreview(s, preview)
	return nil
}

func previewSummary(preview menuPreview) string {
	return fmt.Sprintf("Cached preview: %d added, %d removed, %d changed. Choose Preview changes to see names before applying.", len(preview.Added), len(preview.Removed), len(preview.Changed))
}

func printMenuPreview(s *Session, preview menuPreview) {
	for _, line := range previewLines(preview) {
		s.WriteLine(line)
	}
}

func previewLines(preview menuPreview) []string {
	lines := []string{previewSummary(preview)}
	for _, category := range []struct {
		name  string
		items []string
	}{{"Added", preview.Added}, {"Removed", preview.Removed}, {"Changed", preview.Changed}} {
		if len(category.items) == 0 {
			lines = append(lines, category.name+": none")
			continue
		}
		for _, name := range category.items {
			lines = append(lines, category.name+": "+menuURLPattern.ReplaceAllString(name, "[redacted URL]"))
		}
	}
	return lines
}
