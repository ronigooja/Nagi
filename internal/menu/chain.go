package menu

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
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
	return localizedactionPage(l, chainText(l, "title", "Node Chains"),
		Item{ID: "chain.scope.node", Label: chainText(l, "node", "Nodes"), Next: func() *Page { return localizedchainDraftPage(l, "node", nil) }},
		Item{ID: "chain.scope.provider", Label: chainText(l, "provider", "Entire providers"), Next: func() *Page { return localizedchainTargetsPage(l, "provider") }},
		Item{ID: "chain.scope.subscription", Label: chainText(l, "subscription", "Entire subscriptions"), Next: func() *Page { return localizedchainTargetsPage(l, "subscription") }})
}

// Provider and subscription scopes select a resource, never a batch of nodes.
func localizedchainTargetsPage(l i18n.Context, kind string) *Page {
	return &Page{ID: "chain.targets", Title: chainText(l, kind, kind), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: chainText(l, "empty", "No eligible targets available.")}
		var data struct {
			Chains []menuChainTarget `json:"chains"`
		}
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "chain", "list"), &data); err != nil {
			return v, err
		}
		for _, t := range data.Chains {
			if t.Kind != kind {
				continue
			}
			t := t
			v.Items = append(v.Items, Item{ID: "chain.target", Name: t.Name, Label: t.Name, Next: func() *Page { return localizedchainDraftPage(l, kind, []string{t.Name}) }})
		}
		return v, nil
	}}
}

func chainPathText(l i18n.Context, nodes []string) string {
	path := append([]string{chainText(l, "local", "This device")}, nodes...)
	path = append(path, chainText(l, "destination", "Destination"))
	return strings.Join(path, " → ")
}

func chainArgs(kind string, targets, via []string, clear bool) []string {
	action := "set"
	if clear {
		action = "clear"
	}
	args := append([]string{"proxy", "chain", action, kind}, targets...)
	for _, hop := range via {
		args = append(args, "--via", hop)
	}
	return args
}

func chainPlan(ctx context.Context, s *Session, args []string) (menuChainPlan, error) {
	r := s.Fetch(ctx, append(append([]string{}, args...), "--dry-run")...)
	var data struct {
		Plan *menuChainPlan `json:"plan"`
	}
	if err := localizeddecodeMenuResult(s.language, r, &data); err != nil {
		return menuChainPlan{}, err
	}
	if data.Plan == nil {
		return menuChainPlan{}, errors.New(chainText(s.language, "invalid_plan", "Could not read the chain plan. Return and try again."))
	}
	for i, text := range data.Plan.Conflicts {
		data.Plan.Conflicts[i] = r.message(s.language, "/data/plan/conflicts/"+strconv.Itoa(i), text)
	}
	return *data.Plan, nil
}

func chainPlanContext(l i18n.Context, plan menuChainPlan) string {
	lines := []string{}
	for _, use := range plan.SharedUse {
		lines = append(lines, chainText(l, "shared", "Also affected: ")+use.Name)
	}
	for _, conflict := range plan.Conflicts {
		lines = append(lines, chainText(l, "conflict", "Conflict: ")+conflict)
	}
	return strings.Join(lines, "\n")
}

// Node mode selects the whole path. Scoped modes retain a fixed final resource.
// Only existing CLI operations validate, persist, and activate the draft.
func localizedchainDraftPage(l i18n.Context, kind string, targets []string) *Page {
	members := []string{}
	initialized := false
	loadSavedPath := true
	return &Page{ID: "chain.draft", Title: chainText(l, "title", "Node Chains"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: chainText(l, "no_nodes", "No nodes available.")}
		var data struct {
			Chains []menuChainTarget `json:"chains"`
		}
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "chain", "list"), &data); err != nil {
			return v, err
		}
		available := map[string]menuChainTarget{}
		var current menuChainTarget
		for _, t := range data.Chains {
			if t.Kind == "node" {
				available[t.Name] = t
			}
			if kind != "node" && t.Kind == kind && slices.Contains(targets, t.Name) {
				current = t
			}
		}
		nodes := []menuChainTarget{}
		if kind == "node" {
			for _, t := range data.Chains {
				if t.Kind == "node" {
					nodes = append(nodes, t)
				}
			}
			if !initialized {
				used := map[string]bool{}
				for _, n := range nodes {
					for _, hop := range n.Via {
						used[hop] = true
					}
				}
				exits := []menuChainTarget{}
				for _, n := range nodes {
					if len(n.Via) > 0 && !used[n.Name] {
						exits = append(exits, n)
					}
				}
				if len(exits) == 1 {
					members = append(append([]string{}, exits[0].Via...), exits[0].Name)
					loadSavedPath = false
				}
			}
		} else {
			if current.Name == "" {
				return v, messageChainTargetUnavailable(l)
			}
			var candidates struct {
				Nodes []menuProxyNode `json:"nodes"`
			}
			args := append([]string{"proxy", "chain", "candidates", kind}, targets...)
			if err := localizeddecodeMenuResult(l, s.Fetch(ctx, args...), &candidates); err != nil {
				return v, err
			}
			for _, n := range candidates.Nodes {
				nodes = append(nodes, menuChainTarget{Kind: "node", Name: n.Name, Type: n.Type})
			}
			if !initialized {
				members = append([]string{}, current.Via...)
			}
		}
		initialized = true
		// A failed or changed read must not silently shorten a pending path.
		for _, name := range members {
			if _, ok := available[name]; !ok {
				return v, messageChainTargetUnavailable(l)
			}
		}
		for _, n := range nodes {
			n := n
			label := n.Name + " (" + n.Type + ")"
			if len(n.Via) > 0 {
				label += " — " + strings.Join(append(append([]string{}, n.Via...), n.Name), " → ")
			}
			v.Items = append(v.Items, Item{ID: "chain.member", Name: n.Name, Label: label, Selected: slices.Contains(members, n.Name), Action: func(context.Context, *Session) error {
				if index := slices.Index(members, n.Name); index >= 0 {
					members = slices.Delete(members, index, index+1)
				} else if kind == "node" && loadSavedPath && len(members) == 0 && len(n.Via) > 0 {
					members = append(append([]string{}, n.Via...), n.Name)
				} else {
					members = append(members, n.Name)
				}
				loadSavedPath = false
				return nil
			}})
		}
		path := append([]string{}, members...)
		if kind != "node" {
			path = append(path, targets...)
			v.Context = chainText(l, kind, kind) + ": " + current.Name + "\n"
		}
		v.Context += chainPathText(l, path)
		if kind != "node" {
			v.Context += "\n" + chainText(l, "persistent", "Includes nodes added by future updates.")
		}
		var args []string
		if kind == "node" && len(members) >= 2 {
			args = chainArgs(kind, members[len(members)-1:], members[:len(members)-1], false)
		} else if kind != "node" && len(members) > 0 {
			args = chainArgs(kind, targets, members, false)
		}
		if len(args) > 0 {
			plan, err := chainPlan(ctx, s, args)
			if err != nil {
				v.Context += "\n" + err.Error()
			} else if details := chainPlanContext(l, plan); details != "" {
				v.Context += "\n" + details
			}
		}
		v.Actions = append(v.Actions, Item{ID: "chain.apply", Label: chainText(l, "apply", "Apply"), Action: func(ctx context.Context, s *Session) error {
			if len(args) == 0 {
				message := chainText(l, "select_nodes", "Select at least two nodes.")
				if kind != "node" {
					message = chainText(l, "select_hops", "Select at least one node.")
				}
				s.feedback(feedbackWarning, message)
				return nil
			}
			if !applyMenuChain(ctx, s, args) {
				return nil
			}
			if kind != "node" {
				initialized = false
			}
			return nil
		}})
		clearTargets := targets
		hasChain := len(current.Via) > 0
		if kind == "node" && len(members) > 0 {
			last := members[len(members)-1]
			clearTargets = []string{last}
			hasChain = len(available[last].Via) > 0
		}
		if hasChain {
			clearArgs := chainArgs(kind, clearTargets, nil, true)
			v.Actions = append(v.Actions, Item{ID: "chain.clear", Label: chainText(l, "clear", "Clear chain"), Action: func(ctx context.Context, s *Session) error {
				if applyMenuChain(ctx, s, clearArgs) {
					members = nil
					loadSavedPath = false
					initialized = kind == "node"
				}
				return nil
			}})
		}
		return v, nil
	}}
}

func messageChainTargetUnavailable(l i18n.Context) error {
	return errors.New(chainText(l, "unavailable", "A selected node or scope is no longer available. Return and reopen Node Chains."))
}

func applyMenuChain(ctx context.Context, s *Session, args []string) bool {
	plan, err := chainPlan(ctx, s, args)
	if err != nil {
		s.feedback(feedbackError, err.Error())
		return false
	}
	if len(plan.Conflicts) > 0 {
		s.feedback(feedbackWarning, chainPlanContext(s.language, plan))
		return false
	}
	r := s.Fetch(ctx, args...)
	if r.Code != 0 || r.Error != nil {
		s.ShowResult(args, r)
		return false
	}
	var result struct {
		Saved    bool `json:"saved"`
		Reloaded bool `json:"reloaded"`
	}
	if data, err := json.Marshal(r.Data); err == nil && json.Unmarshal(data, &result) == nil && result.Saved {
		message := chainText(s.language, "saved", "Chain saved.")
		if args[2] == "clear" {
			message = chainText(s.language, "cleared", "Chain cleared.")
		}
		if result.Reloaded {
			message += " " + chainText(s.language, "reloaded", "Configuration reloaded.")
		}
		s.feedback(feedbackSuccess, message)
	} else {
		s.ShowResult(args, r)
	}
	return true
}

type menuChainPlan struct {
	SharedUse []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		Via  string `json:"via"`
	} `json:"shared_use"`
	Conflicts []string `json:"conflicts"`
}
