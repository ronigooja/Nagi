package menu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type menuProxyGroup struct {
	Name           string                   `json:"name"`
	Type           string                   `json:"type"`
	Now            string                   `json:"now"`
	Fixed          string                   `json:"fixed"`
	All            []string                 `json:"all"`
	Alive          bool                     `json:"alive"`
	SelectedStatus string                   `json:"selected_status"`
	Hidden         bool                     `json:"hidden"`
	Icon           string                   `json:"icon"`
	NodeDelays     map[string]menuNodeDelay `json:"node_delays"`
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
	return &Page{Title: "Select Node", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Select a group, then a node to switch.", Empty: "No proxy groups. Check the profile in Profiles.", Actions: []Item{
			{Label: "Restore saved selections", Args: []string{"proxy", "restore"}},
			{Label: "Proxy Providers", Next: func() *Page { return providerListPage() }},
			{Label: "Tailscale", Next: tailscaleListPage},
		}}
		var data struct {
			Groups []menuProxyGroup `json:"groups"`
		}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "groups"), &data); err != nil {
			view.Context = "Mihomo: unknown | Proxy group state: unknown"
			view.ReadErrorHint = "Open Troubleshooting to check status and logs, then return here."
			view.Actions = append(view.Actions, Item{Label: "Start / Stop", Next: RuntimePage})
			var status struct {
				Running        *bool `json:"running"`
				UnexpectedExit bool  `json:"unexpected_exit"`
			}
			if statusErr := decodeMenuResult(s.Fetch(ctx, "status"), &status); statusErr != nil {
				view.Context += "\nStatus unavailable: " + statusErr.Error()
			} else if status.Running != nil {
				switch {
				case *status.Running:
					view.Context = "Mihomo: running | Proxy group state: unknown"
					view.ReadErrorHint = "Check logs in Troubleshooting; use Start / Stop if a restart is needed."
				case status.UnexpectedExit:
					view.Context = "Mihomo: stopped (unexpected exit) | Proxy group state: unavailable"
					view.ReadErrorHint = "Check logs and clean stale runtime records in Troubleshooting, then start mihomo through Start / Stop."
				default:
					view.Context = "Mihomo: stopped | Proxy group state: unavailable"
					view.ReadErrorHint = "Start mihomo through Start / Stop, then return here."
					view.Empty = view.ReadErrorHint
					return view, nil
				}
			}
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

func proxyGroupPage(name string) *Page { return proxyNodesPage(name, false) }

// Batch ordering belongs to the page. Engine measurements remain authoritative
// on every reload, including failures newer than the last batch.
func proxyNodesPage(name string, testOnly bool) *Page {
	measurements := map[string]menuProxyMeasurement{}
	order := []string{}
	title := "Proxy group: " + name
	if testOnly {
		title = "Test node latency: " + name
	}
	return &Page{Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Empty: "No nodes available. Inspect `nagi proxy show`.", Actions: []Item{
			{Label: "Show group details", Args: []string{"proxy", "show", name}},
		}}
		if !testOnly {
			view.Actions = append(view.Actions,
				Item{Label: "Test one node", Next: func() *Page { return proxyNodesPage(name, true) }},
				Item{Label: "Test all nodes and select from results", Impact: "May clear a fixed choice and its saved selection for automatic groups", Action: func(ctx context.Context, s *Session) error {
					s.WriteLine("Testing node latency...")
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
					s.feedback(feedbackSuccess, fmt.Sprintf("Latency test completed: %d results.", len(order)))
					return nil
				}},
				Item{Label: "Restore saved selections", Args: []string{"proxy", "restore"}},
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
				view.Actions = append(view.Actions, Item{Label: "Resume automatic selection", Impact: "Clears this group's fixed choice and saved selection", Args: []string{"proxy", "auto", name}})
			}
		}
		if !testOnly {
			view.Actions = append(view.Actions, Item{Label: "Automatic latency checks", Next: func() *Page { return healthcheckPage("group", name) }})
		}
		if testOnly {
			view.Context += "\nChoose a node to measure latency."
		} else {
			view.Context += "\nChoose a node to switch immediately."
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
			view.Context += "\nLast latency results first; unmeasured nodes follow."
		}
		for _, node := range nodes {
			label := node
			if delay, ok := group.NodeDelays[node]; ok {
				label += " — " + delay.label()
			} else if measurement, ok := measurements[node]; ok {
				if measurement.DelayMS != nil {
					label += fmt.Sprintf(" — %d ms", *measurement.DelayMS)
				} else if measurement.Error != "" {
					label += " — " + measurement.Error
				} else {
					label += " — latency unknown"
				}
			} else {
				label += " — untested"
			}
			if node == group.Now {
				label += " (selected)"
			}
			args := []string{"proxy", "select", name, node}
			if testOnly {
				args = []string{"proxy", "delay", node}
			}
			view.Items = append(view.Items, Item{Name: node, Label: label, Selected: node == group.Now, Args: args})
		}
		return view, nil
	}}
}
