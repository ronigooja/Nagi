package menu

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type menuProvider struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	VehicleType string   `json:"vehicle_type"`
	Nodes       []string `json:"nodes"`
}

func init() { RegisterPage("provider", providerListPage) }

func providerListPage() *Page {
	return &Page{Title: "Proxy providers", Load: func(ctx context.Context, s *Session) (PageView, error) {
		var data struct {
			Providers []menuProvider `json:"providers"`
		}
		view := PageView{Empty: "No proxy providers returned. Choose Add provider.", Actions: []Item{
			{Label: "Add provider", Impact: "Updates the selected profile; reloads running mihomo", Action: addProvider},
		}}
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "providers"), &data); err != nil {
			return view, err
		}
		for _, p := range data.Providers {
			p := p
			view.Items = append(view.Items, Item{Name: p.Name, Label: fmt.Sprintf("%s (%s) · %d nodes", p.Name, p.Type, len(p.Nodes)), Next: func() *Page { return providerDetailPage(p.Name) }})
		}
		return view, nil
	}}
}

func addProvider(ctx context.Context, s *Session) error {
	s.WriteLine("Add an HTTP(S) provider to the selected profile and reload mihomo if running. A stopped engine stays stopped.")
	s.WriteLine("Enter an existing group name or a new one. A new select group includes DIRECT and does not change routing rules; use existing rules or Global mode to route through it.")
	values := map[string]string{"seconds": "3600"}
	result, err := s.Form(ctx, []Field{
		nameField("New provider name"),
		{Key: "url", Label: "Subscription HTTP(S) URL", Required: true, Validate: func(raw string) error {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
				return fmt.Errorf("enter an HTTP(S) URL without user credentials")
			}
			return nil
		}},
		{Key: "group", Label: "Target group name (existing or new; spaces allowed)", Required: true},
		{Key: "seconds", Label: "Update interval seconds (default 3600; 1–31536000)", Required: true, Validate: func(raw string) error {
			seconds, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || seconds < 1 || seconds > 31536000 {
				return fmt.Errorf("enter an integer from 1 to 31536000 seconds")
			}
			return nil
		}},
	}, values, func(v map[string]string) Result {
		return s.Run(ctx, "proxy", "provider", "add", v["name"], v["url"], v["group"], v["seconds"])
	})
	if err != nil {
		return err
	}
	var added struct {
		GroupCreated bool `json:"group_created"`
	}
	if decodeMenuResult(result, &added) == nil && added.GroupCreated {
		s.feedback("New select group created with DIRECT. Routing rules are unchanged; use existing rules or Global mode to route through the group.")
	}
	return nil
}

func providerDetailPage(name string) *Page {
	return &Page{Title: "Provider: " + name, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Provider state: unknown", Actions: []Item{
			{Label: "Update provider", Args: []string{"proxy", "provider", "update", name}},
			{Label: "Start healthcheck", Args: []string{"proxy", "provider", "healthcheck", name}},
		}}
		var p menuProvider
		if err := decodeMenuResult(s.Fetch(ctx, "proxy", "provider", "show", name), &p); err != nil {
			return view, err
		}
		view.Context = fmt.Sprintf("Type: %s; nodes: %d", p.Type, len(p.Nodes))
		for _, node := range p.Nodes {
			// Make numbered provider nodes useful: invoke the canonical CLI latency check.
			view.Items = append(view.Items, Item{Name: node, Label: node + " (measure latency)", Args: []string{"proxy", "delay", node}})
		}
		return view, nil
	}}
}
