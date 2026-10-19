package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	RegisterPage("runtime", RuntimePage)
	RegisterPage("routing", RoutingPage)
	RegisterPage("mode", func() *Page {
		return actionPage("Proxy Mode", Item{Label: "For this session", Next: func() *Page { return modePage(false) }}, Item{Label: "Save for future starts", Next: func() *Page { return modePage(true) }})
	})
}

func configurationPage() *Page {
	return &Page{Title: "Current Configuration", Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: statusSummary(s.Fetch(ctx, "status")), Actions: []Item{
			{Label: "Show current configuration", Args: []string{"config", "show"}},
			{Label: "Validate current configuration", Args: []string{"config", "validate"}},
			{Label: "Reload configuration", Args: []string{"config", "reload"}},
		}}, nil
	}}
}

func statusSummary(result Result) string {
	if result.Code != 0 && result.Error == nil {
		return "Mihomo: unknown | Profile: unknown. Status unavailable."
	}
	if result.Error != nil {
		return "Mihomo: unknown | Profile: unknown. Status unavailable: " + result.Error.Message + ". Open Service for status or Monitoring & Diagnostics for logs."
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Mihomo: unknown | Profile: unknown. Status unavailable. Try `nagi status`."
	}
	state := "unknown"
	if running, ok := data["running"].(bool); ok {
		state = "stopped"
		if running {
			state = "running"
		}
	}
	profile, _ := data["profile"].(string)
	if profile == "" {
		return "Mihomo: " + state + " | Profile: unknown"
	}
	return fmt.Sprintf("Mihomo: %s | Profile: %s", state, profile)
}

func RuntimePage() *Page {
	return &Page{Title: "Start / Stop", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: statusSummary(s.Fetch(ctx, "status")), Actions: []Item{
			{Label: "Start mihomo", Args: []string{"start"}},
			{Label: "Stop mihomo", Args: []string{"stop"}},
			{Label: "Restart mihomo", Args: []string{"restart"}},
			{Label: "Show full status", Args: []string{"status"}},
			{Label: "Quit Nagi for this login session", Args: []string{"quit"}, Impact: "Stops the login monitor and mihomo for this session"},
		}}
		return view, nil
	}}
}

func modePage(persistent bool) *Page {
	title := "For this session"
	command := []string{"mode"}
	if persistent {
		title = "Save for future starts"
		command = []string{"mode", "saved"}
	}
	return &Page{Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, command...)
		contextLine := "Current mode unavailable"
		if result.Error != nil {
			contextLine += ": " + result.Error.Message
		} else if data, ok := result.Data.(map[string]any); ok && result.Code == 0 && data["mode"] != nil {
			contextLine = fmt.Sprintf("Current mode: %v", data["mode"])
		}
		view := PageView{Context: contextLine}
		for _, mode := range []string{"rule", "global", "direct"} {
			args := []string{"mode", mode}
			if persistent {
				args = []string{"mode", "save", mode}
			}
			view.Actions = append(view.Actions, Item{Label: map[string]string{"rule": "Rule — follow routing rules", "global": "Global — use the GLOBAL proxy group", "direct": "Direct — connect without a proxy"}[mode], Args: args})
		}
		return view, nil
	}}
}

// pageStatus shows only the fields needed for a page summary, never raw JSON.
func pageStatus(ctx context.Context, s *Session, label string, args []string, fields ...string) string {
	return pageStatusResult(s.Fetch(ctx, args...), label, fields...)
}

func pageStatusResult(result Result, label string, fields ...string) string {
	if result.Error != nil {
		return label + ": unknown — " + result.Error.Message
	}
	if result.Code != 0 {
		return label + ": unknown — read failed"
	}
	object := menuObject(result.Data)
	values := []string{}
	for _, field := range fields {
		var value any
		if raw, ok := object[field]; ok && json.Unmarshal(raw, &value) == nil && value != nil {
			values = append(values, strings.ReplaceAll(field, "_", " ")+": "+readableState(value))
		}
	}
	if len(values) == 0 {
		return label + ": unknown"
	}
	return label + ": " + strings.Join(values, ", ")
}

func readableState(value any) string {
	if flag, ok := value.(bool); ok {
		if flag {
			return "yes"
		}
		return "no"
	}
	return fmt.Sprint(value)
}

func RoutingPage() *Page {
	return actionPage("Routing & DNS", Item{Label: "Proxy Mode", Next: registered["mode"]}, Item{Label: "Routing Rules", Next: RulesPage}, Item{Label: "DNS", Next: DNSPage})
}
