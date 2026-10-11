package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func init() { RegisterPage("runtime", RuntimePage) }

func statusSummary(result Result) string {
	if result.Error != nil {
		return "Mihomo: unknown | Profile: unknown. Status unavailable: " + result.Error.Message + ". Try `nagi status` or Diagnostics and recovery."
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
	return &Page{Title: "Runtime status", Help: []string{"status"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: statusSummary(s.Fetch(ctx, "status")), Actions: []Item{
			{Label: "Show status", Args: []string{"status"}, Help: []string{"status"}},
			{Label: "Start mihomo", Args: []string{"start"}, Help: []string{"start"}},
			{Label: "Stop mihomo", Args: []string{"stop"}, Impact: "Stops mihomo and interrupts traffic", Help: []string{"stop"}},
			{Label: "Restart mihomo", Args: []string{"restart"}, Impact: "Restarts mihomo and interrupts traffic", Help: []string{"restart"}},
			{Label: "Show current configuration", Args: []string{"config", "show"}, Help: []string{"config", "show"}},
			{Label: "Validate current configuration", Args: []string{"config", "validate"}, Help: []string{"config", "validate"}},
			{Label: "Reload configuration", Args: []string{"config", "reload"}, Help: []string{"config", "reload"}},
			{Label: "Change runtime mode", Next: func() *Page { return modePage(false) }, Help: []string{"mode"}},
			{Label: "Save persistent mode", Next: func() *Page { return modePage(true) }, Help: []string{"mode"}},
			{Label: "Quit Nagi for this login session", Args: []string{"quit"}, Impact: "Stops the login monitor and mihomo for this session", Help: []string{"quit"}},
		}}
		return view, nil
	}}
}

func modePage(persistent bool) *Page {
	title := "Change runtime mode"
	command := []string{"mode"}
	if persistent {
		title = "Save persistent mode"
		command = []string{"mode", "saved"}
	}
	return &Page{Title: title, Help: []string{"mode"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, command...)
		contextLine := "Current mode unavailable"
		if result.Error != nil {
			contextLine += ": " + result.Error.Message
		} else if data, ok := result.Data.(map[string]any); ok {
			contextLine = fmt.Sprintf("Current mode: %v", data["mode"])
		}
		view := PageView{Context: contextLine}
		for _, mode := range []string{"rule", "global", "direct"} {
			args := []string{"mode", mode}
			if persistent {
				args = []string{"mode", "save", mode}
			}
			view.Actions = append(view.Actions, Item{Label: mode, Args: args, Help: []string{"mode"}})
		}
		return view, nil
	}}
}

// pageStatus shows only the fields needed for a page summary, never raw JSON.
func pageStatus(ctx context.Context, s *Session, label string, args []string, fields ...string) string {
	result := s.Fetch(ctx, args...)
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
			values = append(values, strings.ReplaceAll(field, "_", " ")+"="+fmt.Sprint(value))
		}
	}
	if len(values) == 0 {
		return label + ": unknown"
	}
	return label + ": " + strings.Join(values, ", ")
}
