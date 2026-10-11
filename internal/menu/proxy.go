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
	Fixed          string   `json:"fixed"`
	All            []string `json:"all"`
	Alive          bool     `json:"alive"`
	SelectedStatus string   `json:"selected_status"`
	Hidden         bool     `json:"hidden"`
	Icon           string   `json:"icon"`
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

// ProxyPage opens groups directly; selecting a group then a node is the daily flow.
func ProxyPage() *Page { return proxyGroupsPage() }

func proxyGroupsPage() *Page {
	return &Page{Title: "Proxies and groups", Help: []string{"proxy", "groups"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Choose a group, then a node to switch immediately. /word filters group names.", Empty: "No proxy groups returned. Start mihomo and run `nagi proxy groups` to inspect the configuration.", Actions: []Item{
			{Key: "s", Label: "Search node names across groups", Navigate: searchProxyNodes},
			{Key: "p", Label: "Manage proxy providers", Next: func() *Page { return providerListPage() }},
			{Key: "a", Label: "Restore saved selections", Args: []string{"proxy", "restore"}},
		}}
		var data struct {
			Groups []menuProxyGroup `json:"groups"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "groups"), &data); err != nil {
			view.Context = "Proxy group state: unknown"
			return view, err
		}
		for _, group := range data.Groups {
			if group.Hidden {
				continue
			}
			group := group
			view.Items = append(view.Items, Item{Name: group.Name, Label: proxyGroupLabel(group), Next: func() *Page { return proxyGroupPage(group.Name) }})
		}
		return view, nil
	}}
}

func automaticProxyGroup(group menuProxyGroup) bool {
	return strings.EqualFold(group.Type, "URLTest") || strings.EqualFold(group.Type, "Fallback")
}

func proxyGroupLabel(group menuProxyGroup) string {
	label := group.Name
	if group.Icon != "" {
		label = "[icon] " + label
	}
	if group.Now != "" {
		label += " → " + group.Now
	}
	if automaticProxyGroup(group) {
		if group.Fixed == "" {
			label += " (automatic)"
		} else {
			label += " (fixed: " + group.Fixed + ")"
		}
	}
	if group.SelectedStatus != "" && group.SelectedStatus != "available" {
		label += " (" + group.SelectedStatus + ")"
	}
	return label
}

type menuProxyMeasurement struct {
	Proxy   string `json:"proxy"`
	DelayMS *int   `json:"delay_ms"`
	Error   string `json:"error"`
}

func proxyGroupPage(name string) *Page { return proxyNodesPage(name, "", false) }

// Measurements belong to the page, so refresh and node selection retain their
// original order. Only a new successful batch test replaces that snapshot.
func proxyNodesPage(name, query string, testOnly bool) *Page {
	measurements := map[string]menuProxyMeasurement{}
	order := []string{}
	title := "Proxy group: " + name
	if testOnly {
		title = "Test node latency: " + name
	}
	return &Page{Title: title, Help: []string{"proxy", "show"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: "No matching nodes. Clear the filter, refresh, or inspect `nagi proxy show`.", Actions: []Item{
			{Key: "d", Label: "Show group details", Args: []string{"proxy", "show", name}},
		}}
		if !testOnly {
			view.Actions = append(view.Actions,
				Item{Key: "t", Label: "Test one node", Next: func() *Page { return proxyNodesPage(name, "", true) }},
				Item{Key: "l", Label: "Test all nodes and select from results", Impact: "May clear a fixed choice and its saved selection for automatic groups", Action: func(ctx context.Context, s *Session) error {
					s.WriteLine("Executing: test all nodes in this group")
					result := s.Fetch(ctx, "proxy", "delays", name)
					var data struct {
						Results []menuProxyMeasurement `json:"results"`
					}
					if err := decodeMenuResult(result, &data); err != nil {
						s.ShowResult([]string{"proxy", "delays", name}, result)
						return nil
					}
					measurements = map[string]menuProxyMeasurement{}
					order = nil
					for _, measurement := range data.Results {
						order = append(order, measurement.Proxy)
						measurements[measurement.Proxy] = measurement
					}
					s.Printf("Latency test completed: %d results. Choose a numbered node to switch.\n", len(order))
					return nil
				}},
				Item{Key: "s", Label: "Restore saved selections", Args: []string{"proxy", "restore"}},
			)
		}
		var group menuProxyGroup
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "show", name), &group); err != nil {
			view.Context = "Current node and selection state: unknown"
			return view, err
		}
		current := group.Now
		if current == "" {
			current = "none"
		}
		status := group.SelectedStatus
		if status == "" {
			status = "unknown"
		}
		view.Context = fmt.Sprintf("Current: %s (%s); available nodes: %d", current, status, len(group.All))
		if automaticProxyGroup(group) {
			if group.Fixed == "" {
				view.Context += "\nSelection: automatic"
			} else {
				view.Context += "\nSelection: fixed to " + group.Fixed
			}
			if !testOnly {
				view.Actions = append(view.Actions, Item{Key: "a", Label: "Resume automatic selection", Impact: "Clears this group's fixed choice and saved selection", Args: []string{"proxy", "auto", name}})
			}
		}
		if testOnly {
			view.Context += "\nChoose a node to measure latency."
		} else {
			view.Context += "\nChoose a node to switch immediately; /word filters node names."
		}
		if query != "" {
			view.Context += "\nSearch: " + query
		}
		live := map[string]bool{}
		for _, node := range group.All {
			live[node] = true
		}
		nodes := []string{}
		keptOrder := []string{}
		for _, node := range order {
			if live[node] {
				nodes = append(nodes, node)
				keptOrder = append(keptOrder, node)
				delete(live, node)
			} else {
				delete(measurements, node)
			}
		}
		order = keptOrder
		for _, node := range group.All {
			if live[node] {
				nodes = append(nodes, node)
			}
		}
		if len(order) > 0 {
			view.Context += "\nLast latency measurements retained in result order; unmeasured nodes follow."
		}
		for _, node := range nodes {
			if !strings.Contains(strings.ToLower(node), strings.ToLower(query)) {
				continue
			}
			label := node
			if measurement, ok := measurements[node]; ok {
				if measurement.DelayMS != nil {
					label += fmt.Sprintf(" — %d ms", *measurement.DelayMS)
				} else if measurement.Error != "" {
					label += " — " + measurement.Error
				} else {
					label += " — latency unknown"
				}
			}
			if node == group.Now {
				label += " (selected)"
			}
			args := []string{"proxy", "select", name, node}
			if testOnly {
				args = []string{"proxy", "delay", node}
			}
			view.Items = append(view.Items, Item{Name: node, Label: label, Args: args})
		}
		return view, nil
	}}
}

func searchProxyNodes(ctx context.Context, s *Session) (Navigation, error) {
	values := map[string]string{}
	_, err := s.Form(ctx, []Field{{Key: "query", Label: "Node name substring", Required: true}}, values, func(values map[string]string) Result {
		result := s.Run(ctx, "proxy", "search", values["query"])
		return result
	})
	if err != nil {
		return Navigation{}, err
	}
	query := values["query"]
	return Navigation{Next: &Page{Title: "Matching proxy groups", Load: func(ctx context.Context, s *Session) (PageView, error) {
		// Re-read on refresh so deleted groups never remain selectable.
		var data struct {
			Groups []menuProxyGroup `json:"groups"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "search", query), &data); err != nil {
			return PageView{}, err
		}
		view := PageView{Context: "Node search: " + query, Empty: "No matching nodes found."}
		for _, group := range data.Groups {
			group := group
			if group.Hidden {
				continue
			}
			view.Items = append(view.Items, Item{Name: group.Name, Label: proxyGroupLabel(group), Next: func() *Page { return proxyNodesPage(group.Name, query, false) }})
		}
		return view, nil
	}}}, nil
}
