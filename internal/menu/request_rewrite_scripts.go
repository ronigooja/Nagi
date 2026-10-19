package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func localizedrewriteScriptsPage(language i18n.Context, profile string) *Page {
	return &Page{ID: "request_rewrite_scripts.rewriteScriptsPage.page1", Title: menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.local_scripts", "Local Scripts"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.profile", "Profile: ") + profile + menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.profile_local_javascript_ordinary_summaries_omit", "\nProfile-local JavaScript. Ordinary summaries omit source. Referenced scripts cannot be removed."), Empty: menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.no_local_scripts", "No local scripts."), Actions: []Item{{ID: "request_rewrite_scripts.rewriteScriptsPage.item2", Label: menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.add_script", "Add script"), Impact: menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + " " + menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Action: localizedrewriteScriptForm(language, profile, nil)}}}
		result := s.Fetch(ctx, localizedrewriteArgs(language, profile, "scripts", "list")...)
		if localizedrewriteFailed(language, result) {
			return view, localizedrewriteReadError(language, result)
		}
		var data struct {
			Profile string                  `json:"profile"`
			Scripts []rewrite.ScriptSummary `json:"scripts"`
		}
		if localizeddecodeMenuResult(language, result, &data) != nil || data.Profile != profile {
			return view, fmt.Errorf("%s", menuText(language, "menu.request_rewrite_scripts.rewrite_scripts_page.script_profile_mismatch_or_invalid_response", "script profile mismatch or invalid response"))
		}
		for _, script := range data.Scripts {
			script := script
			view.Items = append(view.Items, Item{ID: "request_rewrite_scripts.rewriteScriptsPage.item3", Label: menuFormat(language, "menu.request_rewrite_scripts.rewrite_scripts_page.s_body_s_d_source_bytes_rules_s", "%s | body %s | %d source bytes | rules: %s", script.ID, script.BodyAccess, script.SourceBytes, strings.Join(script.RuleIDs, ", ")), Next: func() *Page { return localizedrewriteScriptDetail(language, profile, script.ID) }})
		}
		return view, nil
	}}
}
func localizedrewriteScriptDetail(language i18n.Context, profile, id string) *Page {
	return &Page{ID: "request_rewrite_scripts.rewriteScriptDetail.page1", Title: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.script", "Script — ") + id, Load: func(ctx context.Context, s *Session) (PageView, error) {
		view := PageView{Context: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.profile", "Profile: ") + profile}
		result := s.Fetch(ctx, localizedrewriteArgs(language, profile, "scripts", "show", id)...)
		if localizedrewriteFailed(language, result) {
			return view, localizedrewriteReadError(language, result)
		}
		var data struct {
			Profile string                `json:"profile"`
			Script  rewrite.ScriptSummary `json:"script"`
		}
		if localizeddecodeMenuResult(language, result, &data) != nil || data.Profile != profile || data.Script.ID != id {
			return view, fmt.Errorf("%s", menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.script_profile_or_id_mismatch", "script profile or ID mismatch"))
		}
		view.Context += menuFormat(language, "menu.request_rewrite_scripts.rewrite_script_detail.body_access_s_source_bytes_d_referencing_rules_s", " | Body access: %s | Source bytes: %d\nReferencing rules: %s", data.Script.BodyAccess, data.Script.SourceBytes, strings.Join(data.Script.RuleIDs, ", "))
		loadSource := func(ctx context.Context, s *Session) (rewrite.Script, error) {
			r := s.Fetch(ctx, localizedrewriteArgs(language, profile, "scripts", "source", id)...)
			if localizedrewriteFailed(language, r) {
				return rewrite.Script{}, localizedrewriteReadError(language, r)
			}
			var full struct {
				Profile   string         `json:"profile"`
				Sensitive bool           `json:"sensitive"`
				Script    rewrite.Script `json:"script"`
			}
			if localizeddecodeMenuResult(language, r, &full) != nil || !full.Sensitive || full.Profile != profile || full.Script.ID != id {
				return rewrite.Script{}, fmt.Errorf("%s", menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.invalid_sensitive_script_response", "invalid sensitive script response"))
			}
			return full.Script, nil
		}
		view.Actions = []Item{
			{ID: "request_rewrite_scripts.rewriteScriptDetail.item2", Label: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.view_sensitive_source", "View sensitive source"), Impact: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback."), Action: func(ctx context.Context, s *Session) error {
				script, e := loadSource(ctx, s)
				if e != nil {
					return e
				}
				fmt.Fprintln(s.out, menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.sensitive_script_source_review_privately_before", "Sensitive script source — review privately before sharing:"))
				fmt.Fprintln(s.out, script.Source)
				s.longResult = true
				return nil
			}},
			{ID: "request_rewrite_scripts.rewriteScriptDetail.item3", Label: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.edit_script", "Edit script"), Impact: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + " " + menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Action: func(ctx context.Context, s *Session) error {
				script, e := loadSource(ctx, s)
				if e != nil {
					return e
				}
				return localizedrewriteScriptForm(language, profile, &script)(ctx, s)
			}},
			{ID: "request_rewrite_scripts.rewriteScriptDetail.item4", Label: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.remove_script", "Remove script"), Impact: menuText(language, "menu.request_rewrite_scripts.rewrite_script_detail.fails_while_any_enabled_or_disabled_rule_referen", "Fails while any enabled or disabled rule references it."), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := rewriteSubmit(ctx, s, profile, nil, "scripts", "remove", id)
				return Navigation{Back: !localizedrewriteFailed(language, r)}, nil
			}},
		}
		return view, nil
	}}
}
func localizedrewriteScriptForm(language i18n.Context, profile string, existing *rewrite.Script) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{{Key: "json", Label: menuText(language, "menu.request_rewrite_scripts.rewrite_script_form.complete_id_body_access_source_json_single_line", "Complete {id,body_access,source} JSON (single line; escape source newlines)"), Required: true, Validate: func(v string) error {
			var script rewrite.Script
			if e := localizedrewriteMenuJSON(language, v, &script); e != nil {
				return e
			}
			if existing != nil && script.ID != existing.ID {
				return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_scripts.rewrite_script_form.existing_script_id_cannot_change", "existing script ID cannot change"))
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
func rewriteScriptsPage(profile string) *Page {
	return localizedrewriteScriptsPage(i18n.New(i18n.English), profile)
}
func rewriteScriptDetail(profile, id string) *Page {
	return localizedrewriteScriptDetail(i18n.New(i18n.English), profile, id)
}
func rewriteScriptForm(profile string,
	existing *rewrite.Script) func(context.
	Context, *Session) error {
	return localizedrewriteScriptForm(i18n.New(i18n.English), profile, existing)
}
