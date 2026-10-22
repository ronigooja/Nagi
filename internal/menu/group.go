package menu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// These local snapshots come exclusively from CLI reads, including when stopped.
type menuGroupConfig struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Proxies   []string `json:"proxies"`
	Use       []string `json:"use"`
	URL       string   `json:"url"`
	Interval  int      `json:"interval"`
	Tolerance int      `json:"tolerance"`
	Strategy  string   `json:"strategy"`
}

func groupText(l i18n.Context, key, fallback string) string {
	return menuText(l, "menu.group."+key, fallback)
}

func groupManagementPage(l i18n.Context) *Page {
	return &Page{ID: "group.manage", Title: groupText(l, "manage", "Manage proxy groups"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: groupText(l, "empty", "No configured proxy groups."), Actions: []Item{{ID: "group.create", Label: groupText(l, "create", "Create proxy group"), Next: func() *Page { return groupDraftPage(l, "") }}}}
		var data struct {
			Groups []menuGroupConfig `json:"group_configs"`
		}
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "group", "list"), &data); err != nil {
			return v, err
		}
		if len(data.Groups) > 1 {
			v.Actions = append(v.Actions, Item{ID: "group.order", Label: groupText(l, "order", "Adjust display order"), Next: func() *Page { return groupOrderPage(l) }})
		}
		for _, g := range data.Groups {
			g := g
			v.Items = append(v.Items, Item{Name: g.Name, Label: g.Name + " (" + g.Type + ")", Next: func() *Page { return groupConfigPage(l, g.Name) }})
		}
		return v, nil
	}}
}
func groupConfigPage(l i18n.Context, name string) *Page {
	return &Page{ID: "group.detail", Title: name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data struct {
			Group menuGroupConfig `json:"group_config"`
		}
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "group", "show", name), &data); err != nil {
			return PageView{}, err
		}
		g := data.Group
		var live menuProxyGroup
		current := menuState(l, "unknown")
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "show", name), &live); err != nil {
			current += " — " + err.Error()
		} else if live.Now == "" {
			current = menuState(l, "none")
		} else {
			current = live.Now
			if live.SelectedStatus == "removed" || live.SelectedStatus == "unavailable" {
				current += " (" + menuState(l, live.SelectedStatus) + ")"
			}
		}
		v := PageView{
			Context: "\n" + groupText(l, "detail_type", "Type: ") + g.Type + "\n" + groupText(l, "current_selection", "Current selection: ") + current + "\n",
			Actions: []Item{{ID: "group.select_node", Label: groupText(l, "select_node", "Select node or group"), Next: func() *Page { return localizedproxyGroupPage(l, name) }}},
		}
		switch g.Type {
		case "select", "url-test", "fallback", "load-balance":
			v.Actions = append(v.Actions, Item{ID: "group.edit", Label: groupText(l, "edit", "Edit proxy group"), Next: func() *Page { return groupDraftPage(l, name) }})
		}
		v.Actions = append(v.Actions, Item{ID: "group.live", Label: groupText(l, "live", "Show live group details"), Args: []string{"proxy", "show", name}})
		v.Actions = append(v.Actions, Item{ID: "group.remove", Label: groupText(l, "remove", "Delete proxy group"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
			r := s.Run(ctx, "proxy", "group", "remove", name)
			return Navigation{Back: r.Code == 0 && r.Error == nil}, nil
		}})
		return v, nil
	}}
}

// All edits stay in memory until Save. A failed CLI save keeps the whole draft.
func groupDraftPage(l i18n.Context, existing string) *Page {
	draft := menuGroupConfig{Name: existing, Type: "select", Proxies: []string{"DIRECT"}, URL: "https://cp.cloudflare.com/generate_204", Interval: 600, Strategy: "consistent-hashing"}
	initialized := existing == ""
	return &Page{ID: "group.draft", Title: groupText(l, "draft", "Proxy group draft"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		if !initialized {
			var data struct {
				Group menuGroupConfig `json:"group_config"`
			}
			if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "group", "show", existing), &data); err != nil {
				return PageView{}, err
			}
			draft = data.Group
			initialized = true
		}
		v := PageView{Context: draft.Name + " (" + draft.Type + ")\n" + groupText(l, "nodes", "Nodes and groups") + ": " + strings.Join(draft.Proxies, ", ") + "\n" + groupText(l, "providers", "Providers") + ": " + strings.Join(draft.Use, ", ")}
		if existing == "" {
			v.Actions = append(v.Actions, Item{ID: "group.name", Label: groupText(l, "name", "Group name"), Action: func(ctx context.Context, s *Session) error {

				for {
					value, err := s.Prompt(groupText(l, "name", "Group name"))
					if err != nil {
						return err
					}
					if value == "" && draft.Name != "" {
						return nil
					}
					valid := value != "" && len(value) <= 128 && strings.TrimSpace(value) == value
					for _, r := range value {
						if !unicode.IsPrint(r) {
							valid = false
						}
					}
					if valid {
						draft.Name = value
						return nil
					}
					s.WriteLine(menuText(l, "menu.provider.add_provider.enter_a_printable_group_name_from_1_to_128_bytes", "enter a printable group name from 1 to 128 bytes"))
				}

			}})
		}
		v.Actions = append(v.Actions, Item{ID: "group.type", Label: groupText(l, "type", "Group type"), Action: func(ctx context.Context, s *Session) error {
			kinds := []string{"select", "url-test", "fallback", "load-balance"}
			labels := []string{groupText(l, "select", "Manual selection"), groupText(l, "url_test", "Automatic latency selection"), groupText(l, "fallback", "Fallback"), groupText(l, "load_balance", "Load balance")}
			kind, err := s.selectChoices(groupText(l, "type", "Group type"), kinds, labels)
			if err == nil {
				draft.Type = kind
			}
			return err
		}}, Item{ID: "group.nodes", Label: groupText(l, "nodes", "Nodes and groups"), Action: func(ctx context.Context, s *Session) error {
			var nodes menuNodeList
			if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "node", "list"), &nodes); err != nil {
				return err
			}
			candidates := []string{"DIRECT", "REJECT", "REJECT-DROP"}
			for _, n := range nodes.Nodes {
				candidates = append(candidates, n.Name)
			}
			for _, g := range nodes.GroupCandidates {
				if g != draft.Name {
					candidates = append(candidates, g)
				}
			}
			selected, err := chooseGroupMembers(ctx, s, candidates, draft.Proxies, groupText(l, "nodes", "Nodes and groups"))
			if err == nil {
				draft.Proxies = selected
			}
			return err
		}}, Item{ID: "group.providers", Label: groupText(l, "providers", "Providers"), Action: func(ctx context.Context, s *Session) error {
			var nodes menuNodeList
			if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "node", "list"), &nodes); err != nil {
				return err
			}
			selected, err := chooseGroupMembers(ctx, s, nodes.ProviderCandidates, draft.Use, groupText(l, "providers", "Providers"))
			if err == nil {
				draft.Use = selected
			}
			return err
		}})
		if draft.Type != "select" {
			v.Context += "\n" + groupText(l, "url", "Test URL") + ": " + draft.URL + "\n" + groupText(l, "interval", "Check interval (seconds)") + ": " + strconv.Itoa(draft.Interval)
			v.Actions = append(v.Actions, Item{ID: "group.url", Label: groupText(l, "url", "Test URL"), Action: func(ctx context.Context, s *Session) error {
				for {
					value, err := s.Prompt(groupText(l, "url", "Test URL"))
					if err != nil {
						return err
					}
					if value == "" {
						return nil
					}
					if err := localizedtestURLField(l, true).Validate(value); err != nil {
						s.WriteLine(err.Error())
						continue
					}
					draft.URL = value
					return nil
				}
			}}, Item{ID: "group.interval", Label: groupText(l, "interval", "Check interval (seconds)"), Action: func(ctx context.Context, s *Session) error {
				return groupNumber(s, groupText(l, "interval", "Check interval (seconds)"), 1, 31536000, &draft.Interval)
			}})
		}
		if draft.Type == "url-test" {
			v.Context += "\n" + groupText(l, "tolerance", "Tolerance (milliseconds)") + ": " + strconv.Itoa(draft.Tolerance)
			v.Actions = append(v.Actions, Item{ID: "group.tolerance", Label: groupText(l, "tolerance", "Tolerance (milliseconds)"), Action: func(ctx context.Context, s *Session) error {
				return groupNumber(s, groupText(l, "tolerance", "Tolerance (milliseconds)"), 0, 65535, &draft.Tolerance)
			}})
		}
		if draft.Type == "load-balance" {
			v.Context += "\n" + groupText(l, "strategy", "Balance strategy") + ": " + draft.Strategy
			v.Actions = append(v.Actions, Item{ID: "group.strategy", Label: groupText(l, "strategy", "Balance strategy"), Action: func(ctx context.Context, s *Session) error {
				value, err := s.Select(groupText(l, "strategy", "Balance strategy"), []string{"consistent-hashing", "round-robin", "sticky-sessions"})
				if err == nil {
					draft.Strategy = value
				}
				return err
			}})
		}
		if draft.Name != "" {
			v.Actions = append(v.Actions, Item{ID: "group.save", Label: groupText(l, "save", "Save proxy group"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				action, name := "add", draft.Name
				if existing != "" {
					action, name = "edit", existing
				}
				args := []string{"proxy", "group", action, name, "--type", draft.Type}
				if len(draft.Proxies) == 0 {
					args = append(args, "--clear-nodes")
				} else {
					for _, n := range draft.Proxies {
						args = append(args, "--node", n)
					}
				}
				if len(draft.Use) == 0 {
					args = append(args, "--clear-providers")
				} else {
					for _, p := range draft.Use {
						args = append(args, "--provider", p)
					}
				}
				if draft.Type != "select" {
					if draft.URL != "" {
						args = append(args, "--url", draft.URL)
					}
					if draft.Interval > 0 {
						args = append(args, "--interval", strconv.Itoa(draft.Interval))
					}
				}
				if draft.Type == "url-test" {
					args = append(args, "--tolerance", strconv.Itoa(draft.Tolerance))
				}
				if draft.Type == "load-balance" && draft.Strategy != "" {
					args = append(args, "--strategy", draft.Strategy)
				}
				r := s.Run(ctx, args...)
				return Navigation{Back: r.Code == 0 && r.Error == nil}, nil
			}})
		}
		return v, nil
	}}
}

func groupNumber(s *Session, label string, min, max int, target *int) error {
	for {
		value, err := s.Prompt(fmt.Sprintf("%s [%d..%d]", label, min, max))
		if err != nil {
			return err
		}
		if value == "" {
			return nil
		}
		n, err := strconv.Atoi(value)
		if err == nil && n >= min && n <= max {
			*target = n
			return nil
		}
		s.WriteLine(groupText(s.language, "invalid_number", "Enter an integer within the supported range."))
	}
}
func chooseGroupMembers(ctx context.Context, s *Session, candidates, initial []string, title string) ([]string, error) {
	selected := append([]string(nil), initial...)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v := PageView{Context: strings.Join(selected, " → "), Actions: []Item{{ID: "group.members.done", Label: groupText(s.language, "done", "Continue")}}}
		// Keep existing members available even if a provider disappeared. Save will
		// validate them; opening the editor must never silently discard a member.
		choices := append([]string(nil), candidates...)
		for _, n := range selected {
			found := false
			for _, c := range choices {
				if n == c {
					found = true
				}
			}
			if !found {
				choices = append(choices, n)
			}
		}
		for _, n := range choices {
			active := false
			for _, x := range selected {
				if x == n {
					active = true
				}
			}
			v.Items = append(v.Items, Item{Name: n, Label: n, Selected: active})
		}
		choice, err := selectMenuItem(s, title, v)
		if err != nil {
			return nil, err
		}
		if choice.Name == "" {
			return selected, nil
		}
		found := -1
		for i, n := range selected {
			if n == choice.Name {
				found = i
				break
			}
		}
		if found < 0 {
			selected = append(selected, choice.Name)
		} else {
			selected = append(selected[:found], selected[found+1:]...)
		}
	}
}
