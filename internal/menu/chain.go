package menu

import (
	"context"
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"strconv"
	"strings"
)

type menuChainTarget struct {
	Kind string   `json:"kind"`
	Name string   `json:"name"`
	Type string   `json:"type"`
	Via  []string `json:"via"`
}

func chainText(l i18n.Context, key, fallback string) string {
	return menuText(l, "menu.chain."+key, fallback)
}

// selectMenuItem uses the same numeric and pagination map as ordinary pages.
func selectMenuItem(s *Session, title string, v PageView) (Item, error) {
	page := 0
	redraw := true
	for {
		if redraw {
			s.render(&Page{ID: "chain.select", Title: title}, v, page, 0)
			redraw = false
		}
		value, err := s.readChoice()
		if err != nil {
			return Item{}, err
		}
		if value == "0" {
			return Item{}, ErrBack
		}
		if value == "" {
			continue
		}
		choices := localizednumberedChoices(s.language, v, page)
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > len(choices) || strconv.Itoa(n) != value {
			s.writeFeedback(feedbackWarning, menuText(s.language, "menu.input.select.choose_a_listed_number", "Choose a listed number."))
			continue
		}
		c := choices[n-1]
		if c.PageDelta != 0 {
			page += c.PageDelta
			redraw = true
			continue
		}
		return c.Item, nil
	}
}
func localizedchainScopePage(l i18n.Context) *Page {
	items := []Item{}
	for _, kind := range []string{"node", "provider", "subscription"} {
		kind := kind
		items = append(items, Item{ID: "chain.scope." + kind, Label: chainText(l, kind, map[string]string{"node": "Specified nodes", "provider": "Entire providers", "subscription": "Entire subscriptions"}[kind]), Next: func() *Page { return localizedchainTargetsPage(l, kind) }})
	}
	return localizedactionPage(l, chainText(l, "title", "Node Chains"), items...)
}
func localizedchainTargetsPage(l i18n.Context, kind string) *Page {
	selected := map[string]bool{}
	return &Page{ID: "chain.targets", Title: chainText(l, "targets", "Choose targets"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: chainText(l, "empty", "No eligible targets available.")}
		var data struct {
			Chains []menuChainTarget `json:"chains"`
		}
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "chain", "list"), &data); err != nil {
			return v, err
		}
		targets := []string{}
		available := map[string]bool{}
		for _, t := range data.Chains {
			if t.Kind != kind {
				continue
			}
			t := t
			targets = append(targets, t.Name)
			available[t.Name] = true
			label := t.Name
			if t.Type != "" {
				label += " (" + t.Type + ")"
			}
			if len(t.Via) > 0 {
				label += " — " + strings.Join(append(append([]string{}, t.Via...), t.Name), " → ")
			}
			v.Items = append(v.Items, Item{ID: "chain.target", Name: t.Name, Label: label, Selected: selected[t.Name], Action: func(context.Context, *Session) error { selected[t.Name] = !selected[t.Name]; return nil }})
		}
		for name := range selected {
			if !available[name] {
				delete(selected, name)
			}
		}
		v.Context = chainText(l, "batch_hint", "Choose targets for one shared chain. Providers and subscriptions include future nodes.")
		if len(targets) > 0 {
			v.Actions = []Item{{ID: "chain.continue", Label: chainText(l, "continue", "Continue"), Navigate: func(context.Context, *Session) (Navigation, error) {
				names := []string{}
				for _, name := range targets {
					if selected[name] {
						names = append(names, name)
					}
				}
				if len(names) == 0 {
					s.feedback(feedbackWarning, chainText(l, "select_target", "Select at least one target."))
					return Navigation{}, nil
				}
				return Navigation{Next: localizedchainDraftPage(l, kind, names)}, nil
			}}}
		}
		return v, nil
	}}
}
func localizedchainDraftPage(l i18n.Context, kind string, targets []string) *Page {
	via := []string{}
	return &Page{ID: "chain.draft", Title: chainText(l, "title", "Node Chains"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Context: chainText(l, "order", "Traffic order: ") + strings.Join(append(append([]string{}, via...), "["+strings.Join(targets, ", ")+"]"), " → ") + "\n" + chainText(l, "draft_hint", "Append hops in traffic order, or remove the last hop. One chain is applied to all selected targets. No hops clears their chain.")}
		v.Actions = []Item{{ID: "chain.append", Label: chainText(l, "append", "Append hop"), Action: func(ctx context.Context, s *Session) error {
			args := append([]string{"proxy", "chain", "candidates", kind}, targets...)
			var data struct {
				Nodes []menuProxyNode `json:"nodes"`
			}
			if err := localizeddecodeMenuResult(l, s.Fetch(ctx, args...), &data); err != nil {
				return err
			}
			choices := []string{}
			labels := []string{}
			for _, n := range data.Nodes {
				choices = append(choices, n.Name)
				labels = append(labels, n.Name+" ("+n.Type+")")
			}
			if len(choices) == 0 {
				s.feedback(feedbackWarning, chainText(l, "no_hops", "No eligible hops available."))
				return nil
			}
			name, err := s.selectChoices(chainText(l, "append", "Append hop"), choices, labels)
			if err == nil {
				via = append(via, name)
			}
			return err
		}},
			{ID: "chain.pop", Label: chainText(l, "pop", "Remove last hop"), Action: func(context.Context, *Session) error {
				if len(via) > 0 {
					via = via[:len(via)-1]
				}
				return nil
			}},
			{ID: "chain.preview", Label: chainText(l, "preview", "Preview changes"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				action := "set"
				if len(via) == 0 {
					action = "clear"
				}
				args := append([]string{"proxy", "chain", action, kind}, targets...)
				for _, hop := range via {
					args = append(args, "--via", hop)
				}
				result := s.Fetch(ctx, append(append([]string{}, args...), "--dry-run")...)
				var data struct {
					Plan menuChainPlan `json:"plan"`
				}
				if err := localizeddecodeMenuResult(l, result, &data); err != nil {
					s.ShowResult(args, result)
					return Navigation{}, nil
				}
				return Navigation{Next: localizedchainPreviewPage(l, args, data.Plan)}, nil
			}}}
		return v, nil
	}}
}

type menuChainPlan struct {
	Kind       string   `json:"kind"`
	Targets    []string `json:"targets"`
	Via        []string `json:"via"`
	Persistent bool     `json:"persistent"`
	Changes    []struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Before string `json:"before"`
		After  string `json:"after"`
	} `json:"changes"`
	SharedUse []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		Via  string `json:"via"`
	} `json:"shared_use"`
	Conflicts []string `json:"conflicts"`
}

func localizedchainPreviewPage(l i18n.Context, args []string, plan menuChainPlan) *Page {
	return &Page{ID: "chain.preview", Title: chainText(l, "preview", "Preview changes"), Load: func(context.Context, *Session) (PageView, error) {
		lines := []string{chainText(l, "order", "Traffic order: ") + strings.Join(append(append([]string{}, plan.Via...), "["+strings.Join(plan.Targets, ", ")+"]"), " → "), chainText(l, "replacements", "Replacements:")}
		for _, c := range plan.Changes {
			lines = append(lines, fmt.Sprintf("%s: %v → %v", c.Name, c.Before, c.After))
		}
		lines = append(lines, chainText(l, "shared", "Shared use affected by first-hop clearing or intermediate-hop replacement:"))
		for _, r := range plan.SharedUse {
			lines = append(lines, fmt.Sprintf("%s: %s (%v)", r.Kind, r.Name, r.Via))
		}
		if plan.Persistent {
			lines = append(lines, chainText(l, "persistent", "Persistent scope includes nodes added by future refreshes."))
		}
		for _, c := range plan.Conflicts {
			lines = append(lines, chainText(l, "conflict", "Conflict: ")+c)
		}
		v := PageView{Context: strings.Join(lines, "\n")}
		if len(plan.Conflicts) == 0 {
			v.Actions = []Item{{ID: "chain.submit", Label: chainText(l, "submit", "Submit changes"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := s.Fetch(ctx, args...)
				s.ShowResult(args, r)
				if r.Code != 0 || r.Error != nil {
					s.feedback(feedbackWarning, chainText(l, "retained", "Inputs are retained. Choose Submit to retry, or 0 to edit."))
					return Navigation{}, nil
				}
				return Navigation{Back: true}, nil
			}}}
		}
		return v, nil
	}}
}
