package menu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type menuProxyGroup struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Now            string   `json:"now"`
	All            []string `json:"all"`
	Alive          bool     `json:"alive"`
	SelectedStatus string   `json:"selected_status"`
}

func init() { RegisterPage("proxy", ProxyPage) }

func decodeMenuResult(result Result, target any) error {
	if result.Code != 0 || result.Error != nil {
		if result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return fmt.Errorf("command exited with status %d", result.Code)
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// ProxyPage exposes the existing proxy commands through numeric menu choices.
func ProxyPage() *Page {
	return &Page{Title: "Proxies and groups", Help: []string{"proxy"},
		Load: func(_ context.Context, _ *Session) (PageView, error) {
			return PageView{Items: []Item{
				{Label: "List proxy groups", Args: []string{"proxy", "groups"}},
				{Label: "Choose a group", Next: proxyGroupsPage},
				{Label: "Search nodes", Action: searchProxyNodes},
				{Label: "Test one node", Action: testOneProxy},
				{Label: "Test all nodes in a group", Action: testProxyGroup},
				{Label: "Restore saved selections", Args: []string{"proxy", "restore"}},
			}}, nil
		}}
}

func proxyGroupsPage() *Page {
	return &Page{Title: "Proxy groups", Help: []string{"proxy", "groups"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data struct {
			Groups []menuProxyGroup `json:"groups"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "groups"), &data); err != nil {
			return PageView{}, err
		}
		view := PageView{Empty: "No proxy groups returned. Start mihomo and run `nagi proxy groups` to inspect the configuration."}
		for _, group := range data.Groups {
			group := group
			view.Items = append(view.Items, Item{Label: proxyGroupLabel(group), Next: func() *Page { return proxyGroupPage(group.Name) }})
		}
		return view, nil
	}}
}

func proxyGroupLabel(group menuProxyGroup) string {
	label := group.Name
	if group.Now != "" {
		label += " → " + group.Now
	}
	if group.SelectedStatus != "" && group.SelectedStatus != "available" {
		label += " (" + group.SelectedStatus + ")"
	}
	return label
}

func proxyGroupPage(name string) *Page {
	return &Page{Title: "Proxy group: " + name, Help: []string{"proxy", "show"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var group menuProxyGroup
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "show", name), &group); err != nil {
			return PageView{}, err
		}
		current := "none"
		if group.Now != "" {
			current = group.Now
		}
		status := group.SelectedStatus
		if status == "" {
			status = "unknown"
		}
		view := PageView{Context: fmt.Sprintf("Current: %s (%s); available nodes: %d", current, status, len(group.All)), Empty: "No nodes in this group. Check `nagi proxy show`.", Items: []Item{
			{Label: "Show group details", Args: []string{"proxy", "show", name}},
			{Label: "Test all nodes", Args: []string{"proxy", "delays", name}},
			{Label: "Search nodes", Action: func(ctx context.Context, s *Session) error { return searchProxyNodesInGroup(ctx, s, name) }},
		}}
		for _, node := range group.All {
			node := node
			label := node
			if node == group.Now {
				label += " (selected)"
			}
			view.Items = append(view.Items, Item{Label: label, Next: func() *Page { return proxyNodePage(name, node) }})
		}
		return view, nil
	}}
}

func proxyNodePage(group, node string) *Page {
	return &Page{Title: "Proxy node: " + node, Help: []string{"proxy", "select"}, Load: func(_ context.Context, _ *Session) (PageView, error) {
		return PageView{Context: "Group: " + group, Items: []Item{
			{Label: "Select this node", Args: []string{"proxy", "select", group, node}},
			{Label: "Test latency", Args: []string{"proxy", "delay", node}},
		}}, nil
	}}
}

func searchProxyNodes(ctx context.Context, s *Session) error {
	query, err := s.Prompt("Node name substring")
	if err != nil {
		return err
	}
	if strings.TrimSpace(query) == "" {
		s.WriteLine("Enter a node name substring to search.")
		return nil
	}
	var data struct {
		Groups []menuProxyGroup `json:"groups"`
	}
	result := s.Fetch(ctx, "proxy", "search", query)
	if err := decodeMenuResult(result, &data); err != nil {
		return err
	}
	if len(data.Groups) == 0 {
		s.WriteLine("No matching nodes found.")
		return nil
	}
	groups := make([]string, 0, len(data.Groups))
	for _, group := range data.Groups {
		groups = append(groups, proxyGroupLabel(group))
	}
	selected, err := s.Select("Matching group", groups)
	if err != nil {
		return err
	}
	for i, label := range groups {
		if label == selected {
			return chooseSearchedNode(ctx, s, data.Groups[i])
		}
	}
	return nil
}

func searchProxyNodesInGroup(ctx context.Context, s *Session, groupName string) error {
	query, err := s.Prompt("Node name substring")
	if err != nil {
		return err
	}
	if strings.TrimSpace(query) == "" {
		s.WriteLine("Enter a node name substring to search.")
		return nil
	}
	var data struct {
		Groups []menuProxyGroup `json:"groups"`
	}
	if err := decodeMenuResult(s.Fetch(ctx, "proxy", "search", query), &data); err != nil {
		return err
	}
	for _, group := range data.Groups {
		if group.Name == groupName {
			return chooseSearchedNode(ctx, s, group)
		}
	}
	s.WriteLine("No matching nodes found in this group.")
	return nil
}

func chooseSearchedNode(ctx context.Context, s *Session, group menuProxyGroup) error {
	node, err := s.Select("Matching node", group.All)
	if err != nil {
		return err
	}
	if node != "" {
		s.Run(ctx, "proxy", "select", group.Name, node)
	}
	return nil
}

func testOneProxy(ctx context.Context, s *Session) error {
	group, err := selectProxyGroup(ctx, s)
	if err != nil || group.Name == "" {
		return err
	}
	if len(group.All) == 0 {
		s.WriteLine("No nodes in this group.")
		return nil
	}
	node, err := s.Select("Node to test", group.All)
	if err != nil {
		return err
	}
	if node != "" {
		s.Run(ctx, "proxy", "delay", node)
	}
	return nil
}

func testProxyGroup(ctx context.Context, s *Session) error {
	group, err := selectProxyGroup(ctx, s)
	if err != nil || group.Name == "" {
		return err
	}
	s.Run(ctx, "proxy", "delays", group.Name)
	return nil
}

func selectProxyGroup(ctx context.Context, s *Session) (menuProxyGroup, error) {
	var data struct {
		Groups []menuProxyGroup `json:"groups"`
	}
	if err := decodeMenuResult(s.Fetch(ctx, "proxy", "groups"), &data); err != nil {
		return menuProxyGroup{}, err
	}
	if len(data.Groups) == 0 {
		s.WriteLine("No proxy groups returned. Start mihomo and run `nagi proxy groups`.")
		return menuProxyGroup{}, nil
	}
	choices := make([]string, len(data.Groups))
	for i, group := range data.Groups {
		choices[i] = proxyGroupLabel(group)
	}
	choice, err := s.Select("Proxy group", choices)
	if err != nil {
		return menuProxyGroup{}, err
	}
	for i, label := range choices {
		if label == choice {
			return data.Groups[i], nil
		}
	}
	return menuProxyGroup{}, nil
}
