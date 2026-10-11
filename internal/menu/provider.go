package menu

import (
	"context"
	"fmt"
)

type menuProvider struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	VehicleType string   `json:"vehicle_type"`
	Nodes       []string `json:"nodes"`
}

func init() { RegisterPage("provider", providerListPage) }

func providerListPage() *Page {
	return &Page{Title: "Proxy providers", Help: []string{"proxy", "providers"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data struct {
			Providers []menuProvider `json:"providers"`
		}
		view := PageView{Empty: "No proxy providers returned."}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "providers"), &data); err != nil {
			return view, err
		}
		for _, p := range data.Providers {
			p := p
			view.Items = append(view.Items, Item{Name: p.Name, Label: fmt.Sprintf("%s (%s) · %d nodes", p.Name, p.Type, len(p.Nodes)), Next: func() *Page { return providerDetailPage(p) }})
		}
		return view, nil
	}}
}

func providerDetailPage(p menuProvider) *Page {
	return &Page{Title: "Provider: " + p.Name, Load: func(context.Context, *Session) (PageView, error) {
		view := PageView{Context: fmt.Sprintf("Type: %s; nodes: %d", p.Type, len(p.Nodes)), Actions: []Item{
			{Key: "u", Label: "Update provider", Args: []string{"proxy", "provider", "update", p.Name}},
			{Key: "c", Label: "Start healthcheck", Args: []string{"proxy", "provider", "healthcheck", p.Name}},
		}}
		for _, node := range p.Nodes {
			// Make numbered provider nodes useful: invoke the canonical CLI latency check.
			view.Items = append(view.Items, Item{Name: node, Label: node + " (measure latency)", Args: []string{"proxy", "delay", node}})
		}
		return view, nil
	}}
}
