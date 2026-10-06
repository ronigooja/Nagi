package menu

import (
	"context"
	"fmt"
)

func init() { RegisterPage("runtime", RuntimePage) }

func statusSummary(result Result) string {
	if result.Error != nil {
		return "Status unavailable: " + result.Error.Message + ". Try `nagi status` or Diagnostics and recovery."
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Status unavailable. Try `nagi status`."
	}
	state := "stopped"
	if data["running"] == true {
		state = "running"
	}
	profile, _ := data["profile"].(string)
	if profile == "" {
		return "Mihomo: " + state
	}
	return fmt.Sprintf("Mihomo: %s | Profile: %s", state, profile)
}

func RuntimePage() *Page {
	return &Page{Title: "Runtime status", Help: []string{"status"}, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: statusSummary(s.Fetch(ctx, "status")), Items: []Item{
			{Label: "Show status", Args: []string{"status"}, Help: []string{"status"}},
			{Label: "Start mihomo", Args: []string{"start"}, Help: []string{"start"}},
			{Label: "Stop mihomo", Args: []string{"stop"}, Confirm: "Stop mihomo?", Help: []string{"stop"}},
			{Label: "Restart mihomo", Args: []string{"restart"}, Confirm: "Restart mihomo?", Help: []string{"restart"}},
			{Label: "Show current configuration", Args: []string{"config", "show"}, Help: []string{"config", "show"}},
			{Label: "Validate current configuration", Args: []string{"config", "validate"}, Help: []string{"config", "validate"}},
			{Label: "Reload configuration", Args: []string{"config", "reload"}, Help: []string{"config", "reload"}},
			{Label: "Change runtime mode", Next: func() *Page { return modePage(false) }, Help: []string{"mode"}},
			{Label: "Save persistent mode", Next: func() *Page { return modePage(true) }, Help: []string{"mode"}},
			{Label: "Quit Nagi for this login session", Args: []string{"quit"}, Confirm: "Stop the login monitor and mihomo for this session?", Help: []string{"quit"}},
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
			view.Items = append(view.Items, Item{Label: mode, Args: args, Help: []string{"mode"}})
		}
		return view, nil
	}}
}
