package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func rewriteScriptsPage(profile string) *Page {
	return &Page{Title: "Local Scripts", Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Profile: " + profile + "\nProfile-local JavaScript. Ordinary summaries omit source. Referenced scripts cannot be removed.", Empty: "No local scripts.", Actions: []Item{{Label: "Add script", Impact: rewriteSensitive + " " + rewriteEffect, Action: rewriteScriptForm(profile, nil)}}}
		result := s.Fetch(ctx, rewriteArgs(profile, "scripts", "list")...)
		if rewriteFailed(result) {
			return view, rewriteReadError(result)
		}
		var data struct {
			Profile string                  `json:"profile"`
			Scripts []rewrite.ScriptSummary `json:"scripts"`
		}
		if decodeMenuResult(result, &data) != nil || data.Profile != profile {
			return view, fmt.Errorf("script profile mismatch or invalid response")
		}
		for _, script := range data.Scripts {
			script := script
			view.Items = append(view.Items, Item{Label: fmt.Sprintf("%s | body %s | %d source bytes | rules: %s", script.ID, script.BodyAccess, script.SourceBytes, strings.Join(script.RuleIDs, ", ")), Next: func() *Page { return rewriteScriptDetail(profile, script.ID) }})
		}
		return view, nil
	}}
}
func rewriteScriptDetail(profile, id string) *Page {
	return &Page{Title: "Script — " + id, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: "Profile: " + profile}
		result := s.Fetch(ctx, rewriteArgs(profile, "scripts", "show", id)...)
		if rewriteFailed(result) {
			return view, rewriteReadError(result)
		}
		var data struct {
			Profile string                `json:"profile"`
			Script  rewrite.ScriptSummary `json:"script"`
		}
		if decodeMenuResult(result, &data) != nil || data.Profile != profile || data.Script.ID != id {
			return view, fmt.Errorf("script profile or ID mismatch")
		}
		view.Context += fmt.Sprintf(" | Body access: %s | Source bytes: %d\nReferencing rules: %s", data.Script.BodyAccess, data.Script.SourceBytes, strings.Join(data.Script.RuleIDs, ", "))
		loadSource := func(ctx context.Context, s *Session) (rewrite.Script, error) {
			r := s.Fetch(ctx, rewriteArgs(profile, "scripts", "source", id)...)
			if rewriteFailed(r) {
				return rewrite.Script{}, rewriteReadError(r)
			}
			var full struct {
				Profile   string         `json:"profile"`
				Sensitive bool           `json:"sensitive"`
				Script    rewrite.Script `json:"script"`
			}
			if decodeMenuResult(r, &full) != nil || !full.Sensitive || full.Profile != profile || full.Script.ID != id {
				return rewrite.Script{}, fmt.Errorf("invalid sensitive script response")
			}
			return full.Script, nil
		}
		view.Actions = []Item{
			{Label: "View sensitive source", Impact: rewriteSensitive, Action: func(ctx context.Context, s *Session) error {
				script, e := loadSource(ctx, s)
				if e != nil {
					return e
				}
				fmt.Fprintln(s.out, "Sensitive script source — review privately before sharing:")
				fmt.Fprintln(s.out, script.Source)
				s.longResult = true
				return nil
			}},
			{Label: "Edit script", Impact: rewriteSensitive + " " + rewriteEffect, Action: func(ctx context.Context, s *Session) error {
				script, e := loadSource(ctx, s)
				if e != nil {
					return e
				}
				return rewriteScriptForm(profile, &script)(ctx, s)
			}},
			{Label: "Remove script", Impact: "Fails while any enabled or disabled rule references it.", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := rewriteSubmit(ctx, s, profile, nil, "scripts", "remove", id)
				return Navigation{Back: !rewriteFailed(r)}, nil
			}},
		}
		return view, nil
	}}
}
func rewriteScriptForm(profile string, existing *rewrite.Script) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{{Key: "json", Label: "Complete {id,body_access,source} JSON (single line; escape source newlines)", Required: true, Validate: func(v string) error {
			var script rewrite.Script
			if e := rewriteMenuJSON(v, &script); e != nil {
				return e
			}
			if existing != nil && script.ID != existing.ID {
				return fmt.Errorf("existing script ID cannot change")
			}
			return nil
		}}}
		values := map[string]string{}
		if existing != nil {
			raw, _ := json.Marshal(existing)
			values["json"] = string(raw)
		}
		_, err := s.rewriteForm(ctx, fields, values, func(v map[string]string) Result {
			args := []string{"scripts", "add", "-"}
			if existing != nil {
				args = []string{"scripts", "update", existing.ID, "-"}
			}
			return rewriteSubmit(ctx, s, profile, json.RawMessage(v["json"]), args...)
		})
		return err
	}
}
