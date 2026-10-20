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

type menuChainRef struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
}

type menuChainTarget struct {
	Kind              string         `json:"kind"`
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	Via               []string       `json:"via"`
	ID                string         `json:"id,omitempty"`
	Provider          string         `json:"provider,omitempty"`
	PathRefs          []menuChainRef `json:"path_refs,omitempty"`
	Outlet            string         `json:"outlet,omitempty"`
	Available         *bool          `json:"available,omitempty"`
	Supported         *bool          `json:"supported,omitempty"`
	UnsupportedReason string         `json:"unsupported_reason,omitempty"`
}

func (t menuChainTarget) ref() menuChainRef {
	return menuChainRef{Kind: t.Kind, Name: t.Name, Provider: t.Provider}
}
func (t menuChainTarget) selectable() bool {
	return (t.Kind == "node" || t.Kind == "provider-node") && (t.Available == nil || *t.Available) && (t.Supported == nil || *t.Supported)
}
func (t menuChainTarget) refs() []menuChainRef {
	if len(t.PathRefs) > 0 {
		return t.PathRefs
	}
	refs := make([]menuChainRef, 0, len(t.Via)+1)
	for _, name := range t.Via {
		refs = append(refs, menuChainRef{Kind: "node", Name: name})
	}
	return append(refs, t.ref())
}
func chainRefLabel(ref menuChainRef) string {
	if ref.Kind == "provider-node" {
		return ref.Name + " [" + ref.Provider + "]"
	}
	return ref.Name
}
func chainRefsText(l i18n.Context, refs []menuChainRef) string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, chainRefLabel(ref))
	}
	return chainPathText(l, names)
}
func chainRefArgs(target menuChainRef, via []menuChainRef, clear bool) []string {
	targets := []string{target.Name}
	if target.Kind == "provider-node" {
		targets = []string{target.Provider, target.Name}
	}
	args := chainArgs(target.Kind, targets, nil, clear)
	for _, ref := range via {
		if ref.Kind == "provider-node" {
			args = append(args, "--via-provider", ref.Provider, ref.Name)
		} else {
			args = append(args, "--via", ref.Name)
		}
	}
	return args
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

// Chains are native edges, so list only terminal node paths, not their prefixes.
func terminalNodeChains(chains []menuChainTarget) []menuChainTarget {
	used := map[menuChainRef]bool{}
	for _, t := range chains {
		if (t.Kind == "node" || t.Kind == "provider-node") && len(t.refs()) > 1 {
			refs := t.refs()
			for _, ref := range refs[:len(refs)-1] {
				used[ref] = true
			}
		}
	}
	result := []menuChainTarget{}
	for _, t := range chains {
		if (t.Kind == "node" || t.Kind == "provider-node") && len(t.refs()) > 1 && (t.Outlet != "" || !used[t.ref()]) {
			result = append(result, t)
		}
	}
	return result
}

func readMenuChainInventory(ctx context.Context, s *Session) ([]menuChainTarget, []string, error) {
	var data struct {
		Chains               []menuChainTarget `json:"chains"`
		UnavailableProviders []string          `json:"unavailable_providers"`
	}
	r := s.Fetch(ctx, "proxy", "chain", "list")
	err := localizeddecodeMenuResult(s.language, r, &data)
	for i := range data.Chains {
		data.Chains[i].UnsupportedReason = r.message(s.language, "/data/chains/"+strconv.Itoa(i)+"/unsupported_reason", data.Chains[i].UnsupportedReason)
	}
	return data.Chains, data.UnavailableProviders, err
}
func readMenuChains(ctx context.Context, s *Session) ([]menuChainTarget, error) {
	chains, _, err := readMenuChainInventory(ctx, s)
	return chains, err
}

func localizedchainScopePage(l i18n.Context) *Page {
	return &Page{ID: "chain.list", Title: chainText(l, "title", "Node Chains"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: chainText(l, "empty", "No chains yet.")}
		chains, err := readMenuChains(ctx, s)
		if err != nil {
			return v, err
		}
		for _, t := range terminalNodeChains(chains) {
			t := t
			v.Items = append(v.Items, Item{ID: "chain.target", Name: t.Name, Label: chainRefsText(l, t.refs()), Next: func() *Page { return localizedchainRefDetailPage(l, t.ref()) }})
		}
		v.Actions = []Item{{ID: "chain.new", Label: chainText(l, "new", "New chain"), Next: func() *Page { return localizedchainDraftPage(l) }}}
		return v, nil
	}}
}

func localizedchainDetailPage(l i18n.Context, name string) *Page {
	return localizedchainRefDetailPage(l, menuChainRef{Kind: "node", Name: name})
}

func localizedchainRefDetailPage(l i18n.Context, target menuChainRef) *Page {
	return &Page{ID: "chain.detail", Title: chainText(l, "title", "Node Chains"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{}
		chains, err := readMenuChains(ctx, s)
		if err != nil {
			return v, err
		}
		for _, t := range chains {
			if t.ref() != target {
				continue
			}
			v.Context = chainRefsText(l, t.refs())
			if len(t.refs()) <= 1 {
				v.Context += "\n" + chainText(l, "disconnected", "Chain disconnected.")
				return v, nil
			}
			clearArgs := chainRefArgs(target, nil, true)
			var effects []string
			if plan, err := chainPlan(ctx, s, clearArgs); err != nil {
				v.Context += "\n" + err.Error()
			} else {
				if details := chainPlanContext(l, plan); details != "" {
					v.Context += "\n" + details
				}
				effects = chainPathEffects(l, chains, plan)
			}
			v.Actions = []Item{
				{ID: "chain.clear", Label: chainText(l, "clear", "Disconnect chain"), Impact: chainText(l, "disconnect_effect", "Disconnects only the exit's incoming edge; upstream segments and nodes remain."), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
					if applyMenuChain(ctx, s, chainRefArgs(target, nil, true)) {
						return Navigation{ReturnTo: "chain.list"}, nil
					}
					return Navigation{}, nil
				}},
			}
			appendChainEffects(l, &v, effects)
			return v, nil
		}
		return v, messageChainTargetUnavailable(l)
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
	for _, conflict := range plan.Conflicts {
		lines = append(lines, chainText(l, "conflict", "Conflict: ")+conflict)
	}
	return strings.Join(lines, "\n")
}

// A new draft starts empty; selection order determines the exit and predecessors.
// The CLI remains the sole owner of validation, persistence and activation.
func localizedchainDraftPage(l i18n.Context) *Page {
	members := []menuChainRef{}
	return &Page{ID: "chain.draft", Title: chainText(l, "title", "Node Chains"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: chainText(l, "no_nodes", "No nodes available.")}
		reset := Item{ID: "chain.reset", Label: chainText(l, "reset", "Reset selection"), Action: func(context.Context, *Session) error { members = nil; return nil }}
		v.Context = chainRefsText(l, members)
		v.Actions = []Item{reset}
		chains, unavailable, err := readMenuChainInventory(ctx, s)
		if err != nil {
			return v, err
		}
		available := map[menuChainRef]menuChainTarget{}
		for _, t := range chains {
			if t.selectable() {
				available[t.ref()] = t
			}
		}
		missing := false
		for _, ref := range members {
			if _, ok := available[ref]; !ok {
				missing = true
			}
		}
		for _, n := range chains {
			if n.Kind != "node" && n.Kind != "provider-node" {
				continue
			}
			n := n
			label := n.Name + " (" + n.Type + ")"
			if n.Provider != "" {
				label += " — " + n.Provider
			}
			if !n.selectable() {
				reason := n.UnsupportedReason
				if reason == "" {
					reason = chainText(l, "node_unavailable", "Unavailable")
				}
				label += " — " + reason
				v.Items = append(v.Items, Item{ID: "chain.unavailable", Name: n.Name, Label: label, Action: func(context.Context, *Session) error { s.feedback(feedbackWarning, reason); return nil }})
				continue
			}
			v.Items = append(v.Items, Item{ID: "chain.member", Name: n.Name, Label: label, Selected: slices.Contains(members, n.ref()), Action: func(context.Context, *Session) error {
				if index := slices.Index(members, n.ref()); index >= 0 {
					members = slices.Delete(members, index, index+1)
				} else {
					members = append(members, n.ref())
				}
				return nil
			}})
		}
		v.Context = chainRefsText(l, members)
		if missing {
			v.Context += "\n" + chainText(l, "selected_unavailable", "A selected node is unavailable. The draft is retained.")
		}
		for _, provider := range unavailable {
			v.Context += "\n" + chainText(l, "provider_unavailable", "Provider nodes unavailable: ") + provider
		}
		var args []string
		if len(members) >= 2 && !missing {
			args = chainRefArgs(members[len(members)-1], members[:len(members)-1], false)
		}
		effects := []string{}
		if len(args) > 0 {
			plan, err := chainPlan(ctx, s, args)
			if err != nil {
				v.Context += "\n" + err.Error()
			} else {
				if details := chainPlanContext(l, plan); details != "" {
					v.Context += "\n" + details
				}
				effects = chainPathEffects(l, chains, plan)
			}
		}
		v.Actions = []Item{reset}
		if !missing {
			v.Actions = []Item{{ID: "chain.apply", Label: chainText(l, "apply", "Apply"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				if len(args) == 0 {
					message := chainText(l, "select_nodes", "Select at least two nodes.")
					s.feedback(feedbackWarning, message)
					return Navigation{}, nil
				}
				if !applyMenuChain(ctx, s, args) {
					return Navigation{}, nil
				}
				return Navigation{ReturnTo: "chain.list"}, nil
			}}, reset}
		}
		appendChainEffects(l, &v, effects)
		return v, nil
	}}
}

func appendChainEffects(l i18n.Context, v *PageView, effects []string) {
	if len(effects) == 0 {
		return
	}
	v.Context += "\n" + chainText(l, "other_paths", "Other configured paths that will change: ") + strconv.Itoa(len(effects))
	v.Actions = append(v.Actions, Item{ID: "chain.effects", Label: chainText(l, "effects", "View changed paths"), Next: func() *Page {
		return &Page{ID: "chain.effects", Title: chainText(l, "effects", "View changed paths"), Load: func(context.Context, *Session) (PageView, error) {
			details := PageView{}
			for _, effect := range effects {
				effect := effect
				details.Items = append(details.Items, Item{ID: "chain.effect", Label: effect, Next: func() *Page { return localizedactionPage(l, effect) }})
			}
			return details, nil
		}}
	}})
}

// shared_use includes group membership and indirect references, not actual edge
// replacements. Derive display-only path differences from changes and inventory;
// never infer a group's selected route or alter the CLI plan contract.
func chainPathEffects(l i18n.Context, chains []menuChainTarget, plan menuChainPlan) []string {
	before := map[string]string{}
	after := map[string]string{}
	for _, t := range chains {
		if t.Kind != "node" {
			continue
		}
		edge := ""
		if len(t.Via) > 0 {
			edge = t.Via[len(t.Via)-1]
		}
		before[t.Name], after[t.Name] = edge, edge
	}
	for _, change := range plan.Changes {
		if change.Kind == "node" {
			after[change.Name] = change.After
		}
	}
	path := func(edges map[string]string, dialer string) ([]string, bool) {
		result := []string{}
		seen := map[string]bool{}
		for dialer != "" {
			next, ok := edges[dialer]
			if seen[dialer] {
				return nil, false
			}
			seen[dialer] = true
			result = append([]string{dialer}, result...)
			if !ok {
				break
			}
			dialer = next
		}
		return result, true
	}
	result := []string{}
	seen := map[string]bool{}
	for _, t := range chains {
		if (t.Kind != "node" && t.Kind != "provider") || len(t.Via) == 0 || (t.Kind == plan.Kind && slices.Contains(plan.Targets, t.Name)) || slices.Contains(plan.Via, t.Name) {
			continue
		}
		oldEdge, newEdge := t.Via[len(t.Via)-1], t.Via[len(t.Via)-1]
		if t.Kind == "node" {
			oldEdge, newEdge = before[t.Name], after[t.Name]
		}
		for _, change := range plan.Changes {
			if change.Kind == t.Kind && change.Name == t.Name {
				newEdge = change.After
			}
		}
		oldPath, oldOK := path(before, oldEdge)
		newPath, newOK := path(after, newEdge)
		if !oldOK || !newOK || slices.Equal(oldPath, newPath) {
			continue
		}
		text := chainPathText(l, append(oldPath, t.Name)) + " ⇒ " + chainPathText(l, append(newPath, t.Name))
		if !seen[text] {
			result = append(result, text)
			seen[text] = true
		}
	}
	return result
}

func messageChainTargetUnavailable(l i18n.Context) error {
	return errors.New(chainText(l, "unavailable", "A selected node is no longer available. Return and reopen Node Chains."))
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
		Saved    bool          `json:"saved"`
		Reloaded bool          `json:"reloaded"`
		Plan     menuChainPlan `json:"plan"`
	}
	if data, err := json.Marshal(r.Data); err == nil && json.Unmarshal(data, &result) == nil && result.Saved {
		message := chainText(s.language, "saved", "Chain saved.")
		if args[2] == "clear" {
			message = chainText(s.language, "cleared", "Chain disconnected.")
		}
		if args[2] != "clear" && result.Plan.Outlet != "" {
			message += " " + chainText(s.language, "outlet", "Select chain outlet: ") + result.Plan.Outlet
			groups := result.Plan.OutletGroups
			if len(groups) == 0 {
				groups = []string{"GLOBAL"}
			}
			message += " (" + strings.Join(groups, ", ") + ")."
		}
		if result.Reloaded && args[2] != "clear" {
			message += " " + chainText(s.language, "reloaded", "Configuration reloaded.")
		}
		s.feedback(feedbackSuccess, message)
	} else {
		s.ShowResult(args, r)
		return false
	}
	return true
}

type menuChainPlan struct {
	Outlet       string   `json:"outlet"`
	OutletGroups []string `json:"outlet_groups"`
	Kind         string   `json:"kind"`
	Targets      []string `json:"targets"`
	Via          []string `json:"via"`
	Changes      []struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Before string `json:"before"`
		After  string `json:"after"`
	} `json:"changes"`
	Conflicts []string `json:"conflicts"`
}
