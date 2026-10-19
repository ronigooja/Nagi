package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func localizedrewritePreviewPage(language i18n.Context, profile string) *Page {
	return localizedactionPage(language, menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.preview_staged_fixture", "Preview staged fixture"), Item{ID: "request_rewrite_preview.rewritePreviewPage.item1", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.enter_sample_request", "Enter sample request"), Impact: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.transforms_locally_without_dns_network_persisten", " Transforms locally without DNS/network, persistence or ordinary logging."), Action: localizedrewritePreviewForm(language, profile)},
		Item{ID: "request_rewrite_preview.rewritePreviewPage.item2", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.preview_private_fixture_file", "Preview private fixture file"), Impact: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback."), Action: func(ctx context.Context, s *Session) error {
			_, err := s.rewriteForm(ctx, []Field{{Key: "path", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.private_staged_fixture_file_path", "Private staged fixture file path"), Required: true}}, nil, func(v map[string]string) Result {
				if v["path"] == "-" {
					r := localizedrewriteFailure(language, menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.enter_a_private_file_path_use_the_sample_form_fo", "Enter a private file path; use the sample form for terminal input"))
					s.ShowResult(nil, r)
					return r
				}
				return rewritePreviewResult(ctx, s, nil, profile, "preview", v["path"])
			})
			return err
		}},
		Item{ID: "request_rewrite_preview.rewritePreviewPage.item3", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.enter_complete_staged_fixture_json", "Enter complete staged fixture JSON"), Impact: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.request_response_exchange_and_both_websocket_dir", " Request, response, exchange and both WebSocket directions."), Action: localizedrewritePreviewJSONForm(language, profile)},
		Item{ID: "request_rewrite_preview.rewritePreviewPage.item4", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_page.validate_private_document_file", "Validate private document file"), Action: localizedrewriteFileForm(language, profile, "validate")},
	)
}
func localizedrewritePreviewForm(language i18n.Context, profile string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{
			{Key: "scheme", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.scheme", "Scheme"), Choices: []string{"https", "http"}, ChoiceLabels: []string{menuState(language, "https"), menuState(language, "http")}},
			{Key: "host", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.request_host_within_enabled_rule_scope", "Request host within enabled rule scope"), Required: true},
			{Key: "port", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.port", "Port"), Required: true, Validate: func(v string) error {
				n, e := strconv.Atoi(v)
				if e != nil || n < 1 || n > 65535 {
					return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.enter_port_1_65535", "enter port 1–65535"))
				}
				return nil
			}},
			{Key: "method", Label: menuText(language, "menu.rewrite.preview.http_method", "HTTP method"), Required: true},
			{Key: "path", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.escaped_request_path", "Escaped request path"), Required: true},
			{Key: "query", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.raw_query_blank_for_none", "Raw query (blank for none)")},
			{Key: "headers", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.headers_json_object_name_to_string_arrays_blank", "Headers JSON object (name to string arrays; blank for {})"), Validate: func(v string) error {
				if v == "" {
					return nil
				}
				var m map[string][]string
				if json.Unmarshal([]byte(v), &m) != nil || m == nil {
					return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.enter_an_object_of_string_arrays", "enter an object of string arrays"))
				}
				return nil
			}},
			{Key: "body", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.body_as_strict_base64_blank_for_empty", "Body as strict base64 (blank for empty)"), Validate: func(v string) error {
				if _, e := base64.StdEncoding.Strict().DecodeString(v); e != nil {
					return fmt.Errorf("%s", menuText(language, "menu.request_rewrite_preview.rewrite_preview_form.enter_strict_base64", "enter strict base64"))
				}
				return nil
			}},
		}
		_, err := s.rewriteForm(ctx, fields, nil, func(v map[string]string) Result {
			port, _ := strconv.Atoi(v["port"])
			headers := map[string][]string{}
			if v["headers"] != "" {
				_ = json.Unmarshal([]byte(v["headers"]), &headers)
			}
			fixture := map[string]any{"scheme": v["scheme"], "host": v["host"], "port": port, "method": v["method"], "path": v["path"], "query": v["query"], "headers": headers, "body_base64": v["body"]}
			return rewritePreviewResult(ctx, s, map[string]any{"stage": "request", "request": fixture}, profile, "preview", "-")
		})
		return err
	}
}
func rewritePreviewResult(ctx context.Context, s *Session, fixture any, profile string, args ...string) Result {
	command := localizedrewriteArgs(s.language,
		profile, args...)
	var r Result
	if fixture == nil {
		r = s.Fetch(ctx, command...)
	} else if runner, ok := s.runner.(InputRunner); ok {
		b, _ := json.Marshal(fixture)
		r = runner.RunInput(ctx, command, strings.NewReader(string(b)))
	} else {
		r = localizedrewriteFailure(s.language, menuText(s.language, "menu.request_rewrite_preview.rewrite_preview_result.command_input_unavailable", "Command input unavailable"))
	}
	s.discardInterrupts()
	if localizedrewriteFailed(s.language, r) {
		s.ShowResult(command, r)
		return r
	}
	var data struct {
		Sensitive bool            `json:"sensitive"`
		Request   json.RawMessage `json:"request"`
	}
	if localizeddecodeMenuResult(s.language, r, &data) != nil || !data.Sensitive || len(data.Request) == 0 {
		r = localizedrewriteFailure(s.language, menuText(s.language, "menu.request_rewrite_preview.rewrite_preview_result.invalid_sensitive_preview_response", "Invalid sensitive preview response"))
		s.ShowResult(command, r)
		return r
	}
	// This is the explicit sensitive display, deliberately bypassing ordinary
	// result formatting/redaction. It is never queued in normal page context.
	raw, _ := json.MarshalIndent(r.Data, "", "  ")
	fmt.Fprintln(s.out, menuText(s.language, "menu.request_rewrite_preview.rewrite_preview_result.sensitive_preview_review_privately_before_sharin", "Sensitive preview — review privately before sharing:"))
	fmt.Fprintln(s.out, string(raw))
	s.longResult = true
	return r
}

func localizedrewritePreviewJSONForm(language i18n.Context, profile string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		_, err := s.rewriteForm(ctx, []Field{{Key: "json", Label: menuText(language, "menu.request_rewrite_preview.rewrite_preview_j_s_o_n_form.complete_stage_request_response_message_json_sin", "Complete {stage,request,response?,message?} JSON (single line)"), Required: true, Validate: func(v string) error {
			var candidate map[string]json.RawMessage
			return localizedrewriteMenuJSON(language, v, &candidate)
		}}}, nil, func(v map[string]string) Result {
			return rewritePreviewResult(ctx, s, json.RawMessage(v["json"]), profile, "preview", "-")
		})
		return err
	}
}
func rewritePreviewPage(profile string) *Page {
	return localizedrewritePreviewPage(i18n.New(i18n.English), profile)
}
func rewritePreviewForm(profile string) func(
	context.Context,
	*Session) error {
	return localizedrewritePreviewForm(i18n.New(i18n.English), profile)
}
func rewritePreviewJSONForm(profile string) func(context.Context, *Session) error {
	return localizedrewritePreviewJSONForm(i18n.New(i18n.English), profile)
}
