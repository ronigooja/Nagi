package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
)

func localizedrewriteRulesPage(language i18n.Context, profile string) *Page {
	return &Page{ID: "request_rewrite_rules.rewriteRulesPage.page1", Title: menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.ordered_rules", "Ordered Rules"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Context: menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.profile", "Profile: ") + profile + menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.rules_match_original_domain_method_and_escaped_p", "\nRules match original domain, method and escaped path in stored order. Each enabled rule domain grants interception scope. Values are hidden until explicit Edit."), Actions: []Item{{ID: "request_rewrite_rules.rewriteRulesPage.item2", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.add_rule", "Add rule"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Next: func() *Page {
			return localizedrewriteRuleDraft(language, profile, rewrite.Rule{Enabled: true, Stage: rewrite.StageRequest, RangeMode: "fragment", Match: rewrite.Match{Methods: []string{"GET"}, PathPrefix: "/"}, OnError: "block", RedirectConsent: []rewrite.Consent{}, Operations: []rewrite.Operation{}}, false)
		}}}}
		r := s.Fetch(ctx, localizedrewriteArgs(language, profile, "rules", "list")...)
		if localizedrewriteFailed(language, r) {
			return v, localizedrewriteReadError(language, r)
		}
		var data struct {
			Profile string                `json:"profile"`
			Rules   []rewrite.RuleSummary `json:"rules"`
		}
		if localizeddecodeMenuResult(language, r, &data) != nil || data.Profile != profile {
			return v, fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.rule_profile_mismatch_or_invalid_response", "rule profile mismatch or invalid response"))
		}
		for _, rule := range data.Rules {
			rule := rule
			v.Items = append(v.Items, Item{ID: "request_rewrite_rules.rewriteRulesPage.item3", Label: menuFormat(language, "menu.rewrite.rule_summary", "%s — enabled %t | %s | %s | %s", rule.ID+" ["+menuState(language, rule.Stage)+"]", rule.Enabled, rule.Match.Domain, strings.Join(rule.Match.Methods, ","), rule.Match.PathPrefix), Next: func() *Page { return localizedrewriteRuleDetail(language, profile, rule.ID) }})
		}
		v.Empty = menuText(language, "menu.request_rewrite_rules.rewrite_rules_page.no_rules", "No rules.")
		return v, nil
	}}
}
func localizedrewriteRuleDetail(language i18n.Context, profile, id string) *Page {
	return &Page{ID: "request_rewrite_rules.rewriteRuleDetail.page1", Title: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.rule", "Rule — ") + id, Load: func(ctx context.Context, s *Session) (PageView, error) {
		v := PageView{Context: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.profile", "Profile: ") + profile}
		r := s.Fetch(ctx, localizedrewriteArgs(language, profile, "rules", "show", id)...)
		if localizedrewriteFailed(language, r) {
			return v, localizedrewriteReadError(language, r)
		}
		var data struct {
			Profile string              `json:"profile"`
			Rule    rewrite.RuleSummary `json:"rule"`
		}
		if localizeddecodeMenuResult(language, r, &data) != nil || data.Profile != profile || data.Rule.ID != id {
			return v, fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.rule_profile_or_id_mismatch", "rule profile or ID mismatch"))
		}
		rule := data.Rule
		toggle := "enable"
		if rule.Enabled {
			toggle = "disable"
		}
		v.Context += menuFormat(language, "menu.request_rewrite_rules.rewrite_rule_detail.rule_s_enabled_t_match_s_s_s_on_error_s_operatio", " | Rule: %s | Enabled: %t\nMatch: %s | %s | %s\nOn error: %s | Operations: %s", id+" ["+rule.Stage+"]", rule.Enabled, rule.Match.Domain, strings.Join(rule.Match.Methods, ","), rule.Match.PathPrefix, rule.OnError, strings.Join(rule.Operations, ", "))
		v.Actions = []Item{
			{ID: "request_rewrite_rules.rewriteRuleDetail.item2", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.edit_rule", "Edit rule"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.explicitly_exports_complete_values_into_memory", " Explicitly exports complete values into memory."), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				raw := rewriteRaw(ctx, s, profile, "export", "-")
				if localizedrewriteFailed(language, raw) {
					s.ShowResult(nil, raw)
					return Navigation{}, nil
				}
				text, ok := raw.Data.(string)
				if !ok {
					s.feedback(feedbackError, menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.invalid_export_response", "Invalid export response."))
					return Navigation{}, nil
				}
				snapshot, err := rewrite.Parse([]byte(text))
				if err != nil || snapshot.Document().Enabled {
					s.feedback(feedbackError, menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.invalid_portable_rule_document", "Invalid portable rule document."))
					return Navigation{}, nil
				}
				for _, full := range snapshot.Document().Rules {
					if full.ID == id {
						return Navigation{Next: localizedrewriteRuleDraft(language, profile, full, true)}, nil
					}
				}
				s.feedback(feedbackWarning, menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.rule_no_longer_exists_return_to_ordered_rules", "Rule no longer exists. Return to Ordered Rules."))
				return Navigation{}, nil
			}},
			{ID: "request_rewrite_rules.rewriteRuleDetail.item3", Label: map[string]string{"enable": menuText(language, "menu.rewrite.rule.enable", "Enable rule"), "disable": menuText(language, "menu.rewrite.rule.disable", "Disable rule")}[toggle], Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Action: func(ctx context.Context, s *Session) error {
				rewriteSubmit(ctx, s, profile, nil, "rules", toggle, id)
				return nil
			}},
			{ID: "request_rewrite_rules.rewriteRuleDetail.item4", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.move_rule_to_position", "Move rule to position"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Action: func(ctx context.Context, s *Session) error { return rewriteRuleReorder(ctx, s, profile, id) }},
			{ID: "request_rewrite_rules.rewriteRuleDetail.item5", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.remove_rule", "Remove rule"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_detail.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := rewriteSubmit(ctx, s, profile, nil, "rules", "remove", id)
				return Navigation{Back: !localizedrewriteFailed(language, r)}, nil
			}},
		}
		return v, nil
	}}
}
func rewriteRuleReorder(ctx context.Context, s *Session, profile, id string) error {
	r := s.Fetch(ctx, localizedrewriteArgs(s.language,
		profile, "rules", "list")...)
	if localizedrewriteFailed(s.language, r) {
		s.ShowResult(nil, r)
		return nil
	}
	var data struct {
		Profile string                `json:"profile"`
		Rules   []rewrite.RuleSummary `json:"rules"`
	}
	if localizeddecodeMenuResult(s.language, r, &data) != nil || data.Profile != profile {
		return fmt.Errorf("%s", menuText(s.language, "menu.request_rewrite_rules.rewrite_rule_reorder.rule_profile_mismatch_or_invalid_response", "rule profile mismatch or invalid response"))
	}
	ids := []string{}
	index := -1
	for i, rule := range data.Rules {
		ids = append(ids, rule.ID)
		if rule.ID == id {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("%s", menuText(s.language, "menu.request_rewrite_rules.rewrite_rule_reorder.rule_no_longer_exists", "rule no longer exists"))
	}
	start := index / pageSize * pageSize
	count := min(pageSize, len(ids)-start)
	_, err := s.rewriteForm(ctx, []Field{{Key: "position", Label: menuFormat(s.language, "menu.request_rewrite_rules.rewrite_rule_reorder.position_1_d", "Position (1–%d)", count), Required: true, Validate: func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > count || strconv.Itoa(n) != v {
			return fmt.Errorf("%s", menuText(s.language, "menu.request_rewrite_rules.rewrite_rule_reorder.choose_an_existing_position", "choose an existing position"))
		}
		return nil
	}}}, nil, func(v map[string]string) Result {
		position, _ := strconv.Atoi(v["position"])
		position += start
		ordered := append([]string{}, ids...)
		ordered = append(ordered[:index], ordered[index+1:]...)
		ordered = append(ordered, "")
		copy(ordered[position:], ordered[position-1:])
		ordered[position-1] = id
		return rewriteSubmit(ctx, s, profile, ordered, "rules", "reorder", "-")
	})
	return err
}

// A draft is presentation state only. Nested pages change this in-memory copy;
// only Save submits it to the CLI. Returning or EOF discards an unsaved draft.
func localizedrewriteRuleDraft(language i18n.Context, profile string, initial rewrite.Rule, existing bool) *Page {
	rule := initial
	return &Page{ID: "request_rewrite_rules.rewriteRuleDraft.page1", Title: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.rule_draft", "Rule draft"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		summary := menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.profile", "Profile: ") + profile + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.rule", " | Rule: ") + rule.ID + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.enabled", " | Enabled: ") + strconv.FormatBool(rule.Enabled) + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.match", "\nMatch: ") + rule.Match.Domain + " | " + strings.Join(rule.Match.Methods, ",") + " | " + rule.Match.PathPrefix + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.on_error", "\nOn error: ") + rule.OnError + menuFormat(language, "menu.request_rewrite_rules.rewrite_rule_draft.operations_d_destination_consents_d", " | Operations: %d | Destination consents: %d", len(rule.Operations), len(rule.RedirectConsent)) + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.changes_remain_in_memory_until_save_back_discard", "\nChanges remain in memory until Save; Back discards unsaved changes.")
		fields := []Field{{Key: "domain", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.exact_or_wildcard_matching_domain", "Exact or wildcard matching domain"), Required: true}, {Key: "methods", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.http_methods_comma_separated_uppercase_tokens", "HTTP methods (comma separated uppercase tokens)"), Required: true}, {Key: "path", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.escaped_path_prefix", "Escaped path prefix"), Required: true}, {Key: "enabled", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.rule_enabled", "Rule enabled"), Choices: []string{"true", "false"}, ChoiceLabels: []string{menuState(language, "true"), menuState(language, "false")}}, {Key: "error", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.on_transformation_error", "On transformation error"), Choices: []string{"block", "forward_original"}, ChoiceLabels: []string{menuState(language, "block"), menuState(language, "forward_original")}}}
		if !existing {
			fields = append([]Field{{Key: "id", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.stable_rule_id", "Stable rule ID"), Required: true}}, fields...)
		}
		actions := []Item{{ID: "request_rewrite_rules.rewriteRuleDraft.item2", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.edit_matching_and_behavior", "Edit matching and behavior"), Action: func(ctx context.Context, s *Session) error {
			values := map[string]string{"id": rule.ID, "domain": rule.Match.Domain, "methods": strings.Join(rule.Match.Methods, ","), "path": rule.Match.PathPrefix, "enabled": strconv.FormatBool(rule.Enabled), "error": rule.OnError}
			_, err := s.Form(ctx, fields, values, func(v map[string]string) Result {
				rule.ID = v["id"]
				rule.Match.Domain = v["domain"]
				rule.Match.Methods = localizedrewriteCSV(language, v["methods"])
				rule.Match.PathPrefix = v["path"]
				rule.Enabled = v["enabled"] == "true"
				rule.OnError = v["error"]
				return Result{}
			})
			return err
		}},
			{ID: "request_rewrite_rules.rewriteRuleDraft.item3", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.ordered_operations", "Ordered operations"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback."), Next: func() *Page { return localizedrewriteOperationsPage(language, &rule) }},
			{ID: "request_rewrite_rules.rewriteRuleDraft.item4", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.destination_consent", "Destination consent"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.required_for_request_scheme_host_port_changes_ta", "Required for request scheme/host/port changes; target domains also require enabled rule scope."), Next: func() *Page { return localizedrewriteConsentPage(language, &rule) }},
			{ID: "request_rewrite_rules.rewriteRuleDraft.item5", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.save_rule", "Save rule"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				args := []string{"rules", "add", "-"}
				if existing {
					args = []string{"rules", "update", rule.ID, "-"}
				}
				r := rewriteSubmit(ctx, s, profile, localizedrewriteRuleWire(language, rule), args...)
				if localizedrewriteFailed(language, r) {
					s.feedback(feedbackWarning, menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.draft_retained_edit_fields_or_choose_save_again", "Draft retained. Edit fields or choose Save again explicitly."))
				}
				return Navigation{Back: !localizedrewriteFailed(language, r)}, nil
			}}}
		actions = append(actions, Item{ID: "request_rewrite_rules.rewriteRuleDraft.item6", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.edit_complete_rule_json", "Edit complete rule JSON"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.includes_stage_all_match_predicates_range_mode_a", " Includes stage, all match predicates, range mode and every operation."), Action: localizedrewriteRuleJSONForm(language, &rule, existing)})
		return PageView{Context: summary + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.stage", "\nStage: ") + rule.Stage + menuText(language, "menu.request_rewrite_rules.rewrite_rule_draft.range_mode", " | Range mode: ") + rule.RangeMode, Actions: actions}, nil
	}}
}
func localizedrewriteCSV(language i18n.Context, v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func localizedrewriteOperationsPage(language i18n.Context, rule *rewrite.Rule) *Page {
	return &Page{ID: "request_rewrite_rules.rewriteOperationsPage.page1", Title: menuText(language, "menu.request_rewrite_rules.rewrite_operations_page.ordered_operations_draft", "Ordered operations — draft"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		v := PageView{Context: menuText(language, "menu.request_rewrite_rules.rewrite_operations_page.explicit_sensitive_editing_values_remain_in_the", "Explicit sensitive editing. Values remain in the draft until Save rule."), Actions: []Item{{ID: "request_rewrite_rules.rewriteOperationsPage.item2", Label: menuText(language, "menu.request_rewrite_rules.rewrite_operations_page.add_operation", "Add operation"), Action: func(ctx context.Context, s *Session) error {
			return rewriteOperationForm(ctx, s, nil, func(op rewrite.Operation) { rule.Operations = append(rule.Operations, op) })
		}}}}
		for index, op := range rule.Operations {
			index := index
			v.Items = append(v.Items, Item{ID: "request_rewrite_rules.rewriteOperationsPage.item3", Label: op.Op, Next: func() *Page { return localizedrewriteOperationDetail(language, rule, index) }})
		}
		return v, nil
	}}
}
func localizedrewriteOperationDetail(language i18n.Context, rule *rewrite.Rule, index int) *Page {
	return &Page{ID: "request_rewrite_rules.rewriteOperationDetail.page1", Title: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.operation_draft", "Operation draft"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		raw, _ := json.MarshalIndent(rule.Operations[index], "", "  ")
		actions := []Item{
			{ID: "request_rewrite_rules.rewriteOperationDetail.item2", Label: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.edit_operation", "Edit operation"), Action: func(ctx context.Context, s *Session) error {
				return rewriteOperationForm(ctx, s, &rule.Operations[index], func(op rewrite.Operation) { rule.Operations[index] = op })
			}},
			{ID: "request_rewrite_rules.rewriteOperationDetail.item3", Label: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.move_operation", "Move operation"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				err := rewriteDraftPosition(ctx, s, index, len(rule.Operations), func(to int) {
					item := rule.Operations[index]
					rule.Operations = append(rule.Operations[:index], rule.Operations[index+1:]...)
					rule.Operations = append(rule.Operations, rewrite.Operation{})
					copy(rule.Operations[to+1:], rule.Operations[to:])
					rule.Operations[to] = item
				})
				return Navigation{Back: err == nil}, err
			}},
			{ID: "request_rewrite_rules.rewriteOperationDetail.item4", Label: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.remove_operation", "Remove operation"), Navigate: func(_ context.Context, _ *Session) (Navigation, error) {
				rule.Operations = append(rule.Operations[:index], rule.Operations[index+1:]...)
				return Navigation{Back: true}, nil
			}},
		}
		op := rule.Operations[index]
		if op.Op == "json_set" {
			actions = append(actions, Item{ID: "request_rewrite_rules.rewriteOperationDetail.item5", Label: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.use_root_json_pointer", "Use root JSON pointer"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.replaces_the_complete_json_body_when_this_draft", "Replaces the complete JSON body when this draft is saved."), Action: func(context.Context, *Session) error { rule.Operations[index].Pointer = ""; return nil }})
		}
		if op.Op == "header_set" || (op.Op == "url_set" && (op.Component == "query" || op.Component == "port")) {
			actions = append(actions, Item{ID: "request_rewrite_rules.rewriteOperationDetail.item6", Label: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.set_empty_value", "Set empty value"), Impact: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.clears_the_header_value_query_or_explicit_port_i", "Clears the header value, query, or explicit port in this draft."), Action: func(context.Context, *Session) error {
				rule.Operations[index].Value = json.RawMessage(`""`)
				return nil
			}})
		}
		return PageView{ContextRaw: true, Context: menuText(language, "menu.request_rewrite_rules.rewrite_operation_detail.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + "\n" + string(raw), Actions: actions}, nil
	}}
}
func rewriteDraftPosition(ctx context.Context, s *Session, index, total int, apply func(int)) error {
	start := index / pageSize * pageSize
	count := min(pageSize, total-start)
	_, err := s.Form(ctx, []Field{{Key: "position", Label: menuFormat(s.language, "menu.request_rewrite_rules.rewrite_draft_position.position_1_d", "Position (1–%d)", count), Required: true, Validate: func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > count || strconv.Itoa(n) != v {
			return fmt.Errorf("%s", menuText(s.language, "menu.request_rewrite_rules.rewrite_draft_position.choose_an_existing_position", "choose an existing position"))
		}
		return nil
	}}}, nil, func(v map[string]string) Result {
		n, _ := strconv.Atoi(v["position"])
		apply(start + n - 1)
		return Result{}
	})
	return err
}
func rewriteOperationForm(ctx context.Context, s *Session, existing *rewrite.Operation, save func(rewrite.Operation)) error {
	kind := ""
	if existing != nil {
		kind = existing.Op
	} else {
		var err error
		kind, err = s.selectProtocol(menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.operation_kind", "Operation kind"), []string{"header_set", "header_append", "header_delete", "url_set", "method_set", "status_set", "json_set", "json_delete", "body_set", "text_replace", "regex_replace", "trailer_set", "trailer_delete", "script"})
		if err != nil {
			return err
		}
	}
	if kind != "header_set" && kind != "header_delete" && kind != "url_set" && kind != "json_set" && kind != "json_delete" {
		op := rewrite.Operation{Op: kind}
		if existing != nil {
			op = *existing
		}
		raw, _ := json.Marshal(op)
		_, err := s.Form(ctx, []Field{{Key: "json", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.complete_operation_json_single_line", "Complete operation JSON (single line)"), Required: true, Validate: func(v string) error {
			var candidate rewrite.Operation
			if e := localizedrewriteMenuJSON(s.language,
				v, &candidate); e != nil {
				return e
			}
			if candidate.Op != kind {
				return fmt.Errorf("%s", menuFormat(s.language, "menu.request_rewrite_rules.rewrite_operation_form.operation_kind_must_remain_s", "operation kind must remain %s", kind))
			}
			return nil
		}}}, map[string]string{"json": string(raw)}, func(v map[string]string) Result {
			var candidate rewrite.Operation
			_ = localizedrewriteMenuJSON(s.language,
				v["json"], &candidate)
			save(candidate)
			return Result{}
		})
		return err
	}
	values := map[string]string{}
	// Preserve every current value; dedicated draft actions clear optional values.
	if existing != nil {
		values["name"] = existing.Name
		values["component"] = existing.Component
		values["pointer"] = existing.Pointer
		if kind == "json_set" {
			values["value"] = string(existing.Value)
		} else {
			var value string
			_ = json.Unmarshal(existing.Value, &value)
			values["value"] = value
		}
	}

	fields := []Field{}
	switch kind {
	case "header_set", "header_delete":
		fields = append(fields, Field{Key: "name", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.header_name", "Header name"), Required: true})
	case "url_set":
		fields = append(fields, Field{Key: "component", Label: menuText(s.language, "menu.rewrite.operation.url_component", "URL component"), Choices: []string{"scheme", "host", "port", "path", "query"}, ChoiceLabels: []string{menuState(s.language, "scheme"), menuState(s.language, "host"), menuState(s.language, "port"), menuState(s.language, "path"), menuState(s.language, "query")}})
	case "json_set", "json_delete":
		fields = append(fields, Field{Key: "pointer", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.json_pointer_new_set_may_use_blank_for_root_edit", "JSON Pointer (new set may use blank for root; edit preserves blank, use root action to clear)")})
	}
	if kind == "header_set" || kind == "url_set" {
		fields = append(fields, Field{Key: "value", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.literal_value_visible_paste", "Literal value (visible paste)")})
	}
	if kind == "json_set" {
		fields = append(fields, Field{Key: "value", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.json_value_including_quotes_for_a_string", "JSON value (including quotes for a string)"), Required: true, Validate: func(v string) error {
			if !json.Valid([]byte(v)) {
				return fmt.Errorf("%s", menuText(s.language, "menu.request_rewrite_rules.rewrite_operation_form.enter_a_json_value", "enter a JSON value"))
			}
			return nil
		}})
	}
	_, err := s.Form(ctx, fields, values, func(v map[string]string) Result {
		op := rewrite.Operation{Op: kind, Name: v["name"], Component: v["component"], Pointer: v["pointer"]}
		if kind == "json_set" {
			op.Value = json.RawMessage(v["value"])
		} else if kind == "header_set" || kind == "url_set" {
			op.Value, _ = json.Marshal(v["value"])
		}
		if existing != nil {
			op.MessageType = existing.MessageType
		}
		save(op)
		return Result{}
	})
	return err
}

func localizedrewriteRuleWire(language i18n.Context, rule rewrite.Rule) map[string]any {
	return map[string]any{"id": rule.ID, "enabled": rule.Enabled, "stage": rule.Stage, "match": rule.Match, "on_error": rule.OnError, "range_mode": rule.RangeMode, "redirect_consent": rule.RedirectConsent, "operations": rule.Operations}
}
func localizedrewriteRuleJSONForm(language i18n.Context, rule *rewrite.Rule, existing bool) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		raw, _ := json.Marshal(localizedrewriteRuleWire(language, *rule))
		values := map[string]string{"json": string(raw)}
		_, err := s.Form(ctx, []Field{{Key: "json", Label: menuText(language, "menu.request_rewrite_rules.rewrite_rule_j_s_o_n_form.complete_rule_json_single_line_blank_retains_dra", "Complete rule JSON (single line; blank retains draft)"), Required: true, Validate: func(v string) error {
			var candidate rewrite.Rule
			if err := localizedrewriteMenuJSON(language, v, &candidate); err != nil {
				return err
			}
			if existing && candidate.ID != rule.ID {
				return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_rule_j_s_o_n_form.existing_rule_id_cannot_change", "existing rule ID cannot change"))
			}
			return nil
		}}}, values, func(v map[string]string) Result {
			var candidate rewrite.Rule
			_ = localizedrewriteMenuJSON(language, v["json"], &candidate)
			*rule = candidate
			return Result{}
		})
		return err
	}
}
func localizedrewriteMenuJSON(language i18n.Context, value string, target any) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_menu_j_s_o_n.enter_valid_utf_8_json",

			// Drafts must not collapse duplicate fields before CLI validation sees them.
			"enter valid UTF-8 JSON"))
	}

	tokens := json.NewDecoder(strings.NewReader(value))
	tokens.UseNumber()
	var check func(int) error
	check = func(depth int) error {
		if depth > 256 {
			return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_menu_j_s_o_n.json_nesting_is_too_deep", "JSON nesting is too deep"))
		}
		token, e := tokens.Token()
		if e != nil {
			return e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for tokens.More() {
				key, e := tokens.Token()
				if e != nil {
					return e
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate JSON field")
				}
				seen[name] = true
				if e := check(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for tokens.More() {
				if e := check(depth + 1); e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("invalid JSON object")
		}
		_, e = tokens.Token()
		return e
	}
	if check(0) != nil {
		return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_menu_j_s_o_n.enter_valid_json_without_duplicate_fields", "enter valid JSON without duplicate fields"))
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_menu_j_s_o_n.enter_a_complete_json_object_with_known_fields", "enter a complete JSON object with known fields"))
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_rules.rewrite_menu_j_s_o_n.enter_exactly_one_json_object", "enter exactly one JSON object"))
	}
	return nil
}
func localizedrewriteConsentPage(language i18n.Context, rule *rewrite.Rule) *Page {
	return &Page{ID: "request_rewrite_rules.rewriteConsentPage.page1", Title: menuText(language, "menu.request_rewrite_rules.rewrite_consent_page.destination_consent_draft", "Destination consent — draft"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		v := PageView{Context: menuText(language, "menu.request_rewrite_rules.rewrite_consent_page.explicit_scheme_host_port_destination_consent_ea", "Explicit scheme/host/port destination consent. Each destination also requires enabled rule interception scope."), Actions: []Item{{ID: "request_rewrite_rules.rewriteConsentPage.item2", Label: menuText(language, "menu.request_rewrite_rules.rewrite_consent_page.add_destination_consent", "Add destination consent"), Action: func(ctx context.Context, s *Session) error {
			return rewriteConsentForm(ctx, s, nil, func(c rewrite.Consent) { rule.RedirectConsent = append(rule.RedirectConsent, c) })
		}}}}
		for index, c := range rule.RedirectConsent {
			index, c := index, c
			v.Items = append(v.Items, Item{ID: "request_rewrite_rules.rewriteConsentPage.item3", Label: c.Domain + " | " + strings.Join(c.Schemes, ",") + fmt.Sprint(c.Ports), Next: func() *Page {
				return localizedactionPage(language, menuText(language, "menu.request_rewrite_rules.rewrite_consent_page.destination_consent_draft2", "Destination consent draft"), Item{ID: "request_rewrite_rules.rewriteConsentPage.item4", Label: menuText(language, "menu.request_rewrite_rules.rewrite_consent_page.edit_destination_consent", "Edit destination consent"), Action: func(ctx context.Context, s *Session) error {
					return rewriteConsentForm(ctx, s, &rule.RedirectConsent[index], func(c rewrite.Consent) { rule.RedirectConsent[index] = c })
				}},
					Item{ID: "request_rewrite_rules.rewriteConsentPage.item5", Label: menuText(language, "menu.request_rewrite_rules.rewrite_consent_page.remove_destination_consent", "Remove destination consent"), Navigate: func(_ context.Context, _ *Session) (Navigation, error) {
						rule.RedirectConsent = append(rule.RedirectConsent[:index], rule.RedirectConsent[index+1:]...)
						return Navigation{Back: true}, nil
					}},
				)
			}})
		}
		return v, nil
	}}
}
func rewriteConsentForm(ctx context.Context, s *Session, c *rewrite.Consent, save func(rewrite.Consent)) error {
	values := map[string]string{}
	if c != nil {
		values["domain"] = c.Domain
		values["schemes"] = strings.Join(c.Schemes, ",")
		ports := []string{}
		for _, p := range c.Ports {
			ports = append(ports, strconv.Itoa(p))
		}
		values["ports"] = strings.Join(ports, ",")
	}
	_, err := s.Form(ctx, []Field{{Key: "domain", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_consent_form.authorized_destination_domain", "Authorized destination domain"), Required: true}, {Key: "schemes", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_consent_form.allowed_schemes_http_https_comma_separated", "Allowed schemes (http, https; comma separated)"), Required: true}, {Key: "ports", Label: menuText(s.language, "menu.request_rewrite_rules.rewrite_consent_form.allowed_ports_comma_separated", "Allowed ports (comma separated)"), Required: true, Validate: func(v string) error {
		for _, part := range localizedrewriteCSV(s.language,
			v) {
			p, e := strconv.Atoi(part)
			if e != nil || p < 1 || p > 65535 {
				return fmt.Errorf("%s", menuText(s.language, "menu.request_rewrite_rules.rewrite_consent_form.ports_must_be_between_1_and_65535", "ports must be between 1 and 65535"))
			}
		}
		return nil
	}}}, values, func(v map[string]string) Result {
		ports := []int{}
		for _, part := range localizedrewriteCSV(s.language,
			v["ports"]) {
			p, _ := strconv.Atoi(part)
			ports = append(ports, p)
		}
		save(rewrite.Consent{Domain: v["domain"], Schemes: localizedrewriteCSV(s.language,
			v["schemes"]), Ports: ports})
		return Result{}
	})
	return err
}
func rewriteRulesPage(profile string) *Page {
	return localizedrewriteRulesPage(i18n.New(i18n.English), profile)
}
func rewriteRuleDetail(profile, id string) *Page {
	return localizedrewriteRuleDetail(i18n.New(i18n.English), profile, id)
}

func rewriteRuleDraft(profile string, initial rewrite.Rule, existing bool) *Page {
	return localizedrewriteRuleDraft(i18n.New(i18n.English), profile, initial, existing)
}
func rewriteCSV(v string) []string {
	return localizedrewriteCSV(i18n.New(i18n.English), v)
}
func rewriteOperationsPage(rule *rewrite.
	Rule) *Page {
	return localizedrewriteOperationsPage(i18n.New(i18n.English), rule)
}
func rewriteOperationDetail(rule *rewrite.
	Rule, index int) *Page {
	return localizedrewriteOperationDetail(i18n.New(i18n.English), rule, index)
}
func rewriteRuleWire(
	rule rewrite.Rule) map[string]any {
	return localizedrewriteRuleWire(i18n.New(i18n.English), rule)
}
func rewriteRuleJSONForm(rule *rewrite.
	Rule,
	existing bool) func(context.
	Context, *Session) error {
	return localizedrewriteRuleJSONForm(i18n.New(i18n.English), rule, existing)
}
func rewriteMenuJSON(
	value string, target any) error {
	return localizedrewriteMenuJSON(i18n.New(i18n.English), value, target)
}
func rewriteConsentPage(rule *rewrite.
	Rule) *Page {
	return localizedrewriteConsentPage(i18n.New(i18n.English), rule)
}
