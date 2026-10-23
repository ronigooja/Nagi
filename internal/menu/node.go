package menu

import (
	"context"
	"github.com/ronigooja/Nagi/internal/i18n"
	"strings"
)

type menuNodeGroups struct {
	Name      string   `json:"name"`
	Explicit  []string `json:"explicit"`
	Automatic []string `json:"automatic"`
	Excluded  []string `json:"excluded"`
	Available []string `json:"available"`
}
type menuProxyNode struct {
	Subscription string             `json:"subscription,omitempty"`
	Name         string             `json:"name"`
	Type         string             `json:"type"`
	Managed      bool               `json:"managed"`
	Groups       menuNodeGroups     `json:"groups"`
	Tailscale    *menuTailscaleNode `json:"tailscale,omitempty"`
}
type menuNodeList struct {
	Profile            string          `json:"profile"`
	Nodes              []menuProxyNode `json:"nodes"`
	GroupCandidates    []string        `json:"group_candidates"`
	ProviderCandidates []string        `json:"provider_candidates"`
}

func nodeText(l i18n.Context, key, fallback string) string {
	return menuText(l, "menu.node."+key, fallback)
}
func localizednodeListPage(l i18n.Context) *Page {
	return &Page{ID: "node.list", Title: nodeText(l, "title", "Node Management"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Empty: nodeText(l, "empty", "No nodes or providers configured."), Actions: []Item{{ID: "node.add", Label: nodeText(l, "add", "Add node"), Action: func(ctx context.Context, s *Session) error {
			_, err := s.Select(nodeText(l, "type", "Node type"), []string{"tailscale"})
			if err != nil {
				return err
			}
			return addTailscale(ctx, s)
		}}}}
		var data menuNodeList
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "node", "list"), &data); err != nil {
			return v, err
		}
		v.Context = nodeText(l, "profile", "Profile: ") + data.Profile
		appendNode := func(n menuProxyNode) {
			v.Items = append(v.Items, Item{ID: "node.detail", Name: n.Name, Label: n.Name + " (" + n.Type + ")", Next: func() *Page { return localizednodeDetailPage(l, n.Name) }})
		}
		for _, n := range data.Nodes {
			if n.Subscription != "" {
				appendNode(n)
			}
		}
		for _, name := range data.ProviderCandidates {
			name := name
			v.Items = append(v.Items, Item{ID: "node.provider", Name: name, Label: menuText(l, "menu.provider.provider_detail_page.provider", "Provider: ") + name, Next: func() *Page { return localizedproviderDetailPage(l, name) }})
		}
		for _, n := range data.Nodes {
			if n.Subscription == "" {
				appendNode(n)
			}
		}
		return v, nil
	}}
}
func localizednodeDetailPage(l i18n.Context, name string) *Page {
	return &Page{ID: "node.detail", Title: nodeText(l, "node", "Node: ") + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{}
		var data struct {
			Profile string        `json:"profile"`
			Node    menuProxyNode `json:"node"`
		}
		if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "node", "show", name), &data); err != nil {
			return v, err
		}
		n := data.Node
		v.Context = nodeText(l, "profile", "Profile: ") + data.Profile + "\n" + n.Name + " (" + n.Type + ")\n" + nodeText(l, "explicit", "Explicit groups: ") + strings.Join(n.Groups.Explicit, ", ") + "\n" + nodeText(l, "automatic", "Automatic groups: ") + strings.Join(n.Groups.Automatic, ", ") + "\n" + nodeText(l, "excluded", "Excluded groups: ") + strings.Join(n.Groups.Excluded, ", ")
		if n.Tailscale != nil {
			t := *n.Tailscale
			t.Name = name
			key := nodeText(l, "key_absent", "not configured")
			if t.AuthKeyConfigured {
				key = nodeText(l, "key_present", "configured")
			}
			v.Context += "\n" + nodeText(l, "exit", "Exit node: ") + t.ExitNode + "\n" + nodeText(l, "hostname", "Hostname: ") + t.Hostname + "\n" + nodeText(l, "control", "Control URL: ") + t.ControlURL + "\n" + nodeText(l, "key", "Auth key: ") + key
			v.Actions = append(v.Actions, Item{ID: "node.edit", Label: nodeText(l, "edit", "Edit Tailscale node"), Action: func(ctx context.Context, s *Session) error { return editTailscale(ctx, s, t) }})
			v.Actions = append(v.Actions, Item{ID: "node.tailscalePath", Label: nodeText(l, "path_status", "Tailscale path status"), Next: func() *Page { return localizedtailscalePathPage(l, name) }})
		}
		v.Actions = append(v.Actions, Item{ID: "node.groups", Label: nodeText(l, "groups", "Set group membership"), Action: func(ctx context.Context, s *Session) error {
			groups, err := selectNodeGroups(ctx, s, n.Groups.Explicit, nodeText(l, "save", "Save changes"))
			if err != nil {
				return err
			}
			args := []string{"groups", "set", name}
			if len(groups) == 0 {
				args = []string{"groups", "clear", name}
			} else {
				args = append(args, groups...)
			}
			_, err = s.Form(ctx, nil, nil, func(map[string]string) Result { return runTailscale(ctx, s, args...) })
			return err
		}})
		if n.Tailscale != nil {
			v.Actions = append(v.Actions, Item{ID: "node.remove", Label: nodeText(l, "remove", "Delete Tailscale node"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := runTailscale(ctx, s, "remove", name)
				return Navigation{Back: r.Code == 0 && r.Error == nil}, nil
			}})
		}
		return v, nil
	}}
}

// Group selections are local form state; only the final CLI mutation persists them.
func selectNodeGroups(ctx context.Context, s *Session, initial []string, doneLabel string) ([]string, error) {
	var data menuNodeList
	if err := localizeddecodeMenuResult(s.language, s.Fetch(ctx, "proxy", "node", "list"), &data); err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	for _, g := range initial {
		selected[g] = true
	}
	for {
		v := PageView{Context: nodeText(s.language, "group_hint", "Choose groups to toggle. No groups are selected by default."), Actions: []Item{{Label: doneLabel}}}
		for _, g := range data.GroupCandidates {
			v.Items = append(v.Items, Item{Name: g, Label: g, Selected: selected[g]})
		}
		choice, err := selectMenuItem(s, nodeText(s.language, "groups", "Set group membership"), v)
		if err != nil {
			return nil, err
		}
		if choice.Name == "" {
			groups := []string{}
			for _, g := range data.GroupCandidates {
				if selected[g] {
					groups = append(groups, g)
				}
			}
			return groups, nil
		}
		selected[choice.Name] = !selected[choice.Name]
	}
}
