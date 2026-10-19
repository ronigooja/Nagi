package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ronigooja/Nagi/internal/capture"
	rewrite "github.com/ronigooja/Nagi/internal/requestrewrite"
	"github.com/ronigooja/Nagi/internal/rewritedraft"
)

const captureSensitive = "Original URLs, headers, cookies and bodies are visible in terminal scrollback."

func localizedcaptureArgs(language i18n.Context, args ...string) []string {
	return append([]string{"request-rewrite", "capture"}, args...)
}
func captureCall(ctx context.Context, s *Session, value any, args ...string) Result {
	command := localizedcaptureArgs(s.language,
		args...)
	if value == nil {
		return s.Fetch(ctx, command...)
	}
	runner, ok := s.runner.(InputRunner)
	if !ok {
		return localizedrewriteFailure(s.language, menuText(s.language, "menu.request_capture.capture_call.command_input_unavailable", "Command input unavailable"))
	}
	b, err := json.Marshal(value)
	if err != nil {
		return localizedrewriteFailure(s.language, menuText(s.language, "menu.request_capture.capture_call.invalid_capture_form", "Invalid capture form"))
	}
	r := runner.RunInput(ctx, command, strings.NewReader(string(b)))
	s.discardInterrupts()
	return r
}
func captureMutation(ctx context.Context, s *Session, value any, args ...string) Result {
	r := captureCall(ctx, s, value, args...)
	if localizedrewriteFailed(s.language, r) {
		s.ShowResult(localizedcaptureArgs(s.language,
			args...), r)
	} else {
		s.feedback(feedbackSuccess, menuText(s.language, "menu.request_capture.capture_mutation.capture_operation_completed", "Capture operation completed."))
	}
	return r
}

// Explicit capture reads bypass ordinary redaction and styles, like raw config.
// JSON preserves bytes losslessly; human record timestamps omit seconds.
func captureDisplay(s *Session, r Result) Result {
	if localizedrewriteFailed(s.language, r) {
		s.ShowResult(nil, r)
		return r
	}
	b, err := json.Marshal(r.Data)
	if err != nil {
		return localizedrewriteFailure(s.language, menuText(s.language, "menu.request_capture.capture_display.invalid_capture_response", "Invalid capture response"))
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	_ = decoder.Decode(&value)
	var dates func(any)
	dates = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, item := range v {
				if k == "started" {
					if str, ok := item.(string); ok {
						if t, e := time.Parse(time.RFC3339Nano, str); e == nil {
							v[k] = t.Format("2006-01-02 15:04 MST")
						}
					}
				}
				dates(item)
			}
		case []any:
			for _, item := range v {
				dates(item)
			}
		}
	}
	dates(value)
	b, _ = json.MarshalIndent(value, "", "  ")
	fmt.Fprintln(s.out, menuText(s.language, "menu.request_capture.capture_display.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."))
	fmt.Fprintln(s.out, string(b))
	s.longResult = true
	return r
}
func localizedcapturePage(language i18n.Context) *Page {
	return &Page{ID: "request_capture.capturePage.page1", Title: menuText(language, "menu.request_capture.capture_page.request_capture", "Request Capture"), Load: func(ctx context.Context, s *Session) (view PageView, err error) {
		check := captureCall(ctx, s, nil, "check")
		preparation := menuText(language, "menu.request_capture.capture_page.capture_prerequisites_unable_to_check_reopen_thi", "Capture prerequisites: unable to check. Reopen this page to retry.")
		var readiness capture.Reply
		if !localizedrewriteFailed(language, check) && localizeddecodeMenuResult(language, check, &readiness) == nil && readiness.Readiness != nil {
			preparation = menuText(language, "menu.request_capture.capture_page.capture_prerequisites", "Capture prerequisites: ") + readiness.Readiness.Message
		}
		r := captureCall(ctx, s, nil, "status")
		var data capture.Reply
		v := PageView{Context: menuText(language, "menu.request_capture.capture_page.capture_unavailable", "Capture: unavailable\n") + menuText(language, "menu.request_capture.capture_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback.") + menuText(language, "menu.request_capture.capture_page.closing_this_menu_retains_the_session", "\nClosing this menu retains the session.")}
		defer func() { view.Context = preparation + "\n" + view.Context }()
		v.Actions = []Item{
			{ID: "request_capture.capturePage.item2", Label: menuText(language, "menu.request_capture.capture_page.start_with_scope", "Start with scope"), Impact: menuText(language, "menu.request_capture.capture_page.starts_a_new_memory_only_session_no_tun_or_trust", "Starts a new memory-only session; no TUN or trust changes."), Action: captureStartForm},
			{ID: "request_capture.capturePage.item3", Label: menuText(language, "menu.request_capture.capture_page.observed_applications", "Observed applications"), Impact: menuText(language, "menu.request_capture.capture_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Action: func(ctx context.Context, s *Session) error {
				captureDisplay(s, captureCall(ctx, s, nil, "apps"))
				return nil
			}},
			{ID: "request_capture.capturePage.item4", Label: menuText(language, "menu.request_capture.capture_page.captured_records", "Captured records"), Impact: menuText(language, "menu.request_capture.capture_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Next: func() *Page { return localizedcaptureRecordsPage(language, "") }},
			{ID: "request_capture.capturePage.item5", Label: menuText(language, "menu.request_capture.capture_page.import_native_archive", "Import native archive"), Impact: menuText(language, "menu.request_capture.capture_page.replaces_stopped_history_with_an_offline_read_on", "Replaces stopped history with an offline read-only session."), Action: localizedcaptureArchiveForm(language, false)},
			{ID: "request_capture.capturePage.item6", Label: menuText(language, "menu.request_capture.capture_page.export_archive", "Export archive"), Impact: menuText(language, "menu.request_capture.capture_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Action: localizedcaptureArchiveForm(language, true)},
			{ID: "request_capture.capturePage.item7", Label: menuText(language, "menu.request_capture.capture_page.open_versioned_draft_json", "Open versioned draft JSON"), Impact: menuText(language, "menu.request_capture.capture_page.edits_in_memory_preview_and_save_are_separate_ac", "Edits in memory; preview and save are separate actions."), Navigate: captureOpenDraft},
		}
		if localizedrewriteFailed(language, r) {
			return v, localizedrewriteReadError(language, r)
		}
		if localizeddecodeMenuResult(language, r, &data) != nil {
			return v, fmt.Errorf("%s", menuText(language, "menu.request_capture.capture_page.capture_status_unavailable", "capture status unavailable"))
		}
		if data.Status == nil {
			v.Context = menuText(language, "menu.request_capture.capture_page.no_retained_capture_session", "No retained capture session.\n") + menuText(language, "menu.request_capture.capture_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback.")
			return v, nil
		}
		st := data.Status
		v.Context = menuFormat(language, "menu.request_capture.capture_page.session_s_state_s_profile_s_backend_s_records_d", "Session: %s | State: %s | Profile: %s | Backend: %s\nRecords: %d | Bytes: %d | Dropped: %d | Truncated: %d | Read only: %t\n%s", st.SessionID, menuState(language, st.State), st.Profile, menuState(language, st.Backend), st.Records, st.Bytes, st.Dropped, st.Truncated, st.ReadOnly, menuText(language, "menu.request_capture.capture_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."))
		if st.RecorderState != "" {
			v.Context += menuText(language, "menu.request_capture.capture_page.recorder_state",

				// Recovery is CLI-owned and carries no captured content.
				"\nRecorder state: ") + menuState(language, st.RecorderState)
		}

		var envelope struct {
			Status map[string]json.RawMessage `json:"status"`
		}
		_ = localizeddecodeMenuResult(language, r, &envelope)
		if recovery := localizedrewriteText(language, envelope.Status, "recovery"); recovery != "unknown" {
			v.Context += menuText(language, "menu.request_capture.capture_page.recovery", "\nRecovery: ") + recovery
		}
		scope, _ := json.Marshal(st.Scope)
		v.Context += menuText(language, "menu.request_capture.capture_page.scope", "\nScope: ") + string(scope) + menuFormat(language, "menu.request_capture.capture_page.body_limit_d", " | Body limit: %d", st.BodyLimit)
		if !st.ReadOnly {
			verbs := []string{"stop", "clear"}
			if st.State == "recording" {
				verbs = append([]string{"pause"}, verbs...)
			}
			if st.State == "paused" {
				verbs = append([]string{"resume"}, verbs...)
			}
			for _, verb := range verbs {
				verb := verb
				id := st.SessionID
				v.Actions = append(v.Actions, Item{ID: "request_capture.capturePage.item8", Label: map[string]string{"pause": menuText(language, "menu.capture.pause", "Pause capture"), "resume": menuText(language, "menu.capture.resume", "Resume capture"), "stop": menuText(language, "menu.capture.stop", "Stop capture"), "clear": menuText(language, "menu.capture.clear", "Clear capture")}[verb], Impact: map[string]string{"pause": menuText(language, "menu.request_capture.capture_page.stops_collection_retains_history_and_open_stream", "Stops collection; retains history and open streams."), "resume": menuText(language, "menu.request_capture.capture_page.resumes_this_session", "Resumes this session."), "stop": menuText(language, "menu.request_capture.capture_page.stops_collection_retains_history", "Stops collection; retains history."), "clear": menuText(language, "menu.request_capture.capture_page.deletes_this_session_s_recorded_content", "Deletes this session's recorded content.")}[verb], Action: func(ctx context.Context, s *Session) error { captureMutation(ctx, s, nil, verb, id); return nil }})
			}
		}
		return v, nil
	}}
}
func captureStartForm(ctx context.Context, s *Session) error {
	fields := []Field{{Key: "domains", Label: menuText(s.language, "menu.request_capture.capture_start_form.domain_patterns_comma_separated_blank_for_all", "Domain patterns, comma separated (blank for all)")}, {Key: "processes", Label: menuText(s.language, "menu.request_capture.capture_start_form.exact_process_paths_comma_separated_blank_for_al", "Exact process paths, comma separated (blank for all)")}, {Key: "apps", Label: menuText(s.language, "menu.request_capture.capture_start_form.outermost_macos_app_paths_comma_separated_blank", "Outermost macOS .app paths, comma separated (blank for all)")}, {Key: "limit", Label: menuText(s.language, "menu.request_capture.capture_start_form.body_limit_in_bytes_1_8388608", "Body limit in bytes (1–8388608)"), Required: true, Validate: func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > capture.MaxBodyLimit {
			return fmt.Errorf("enter 1–8388608")
		}
		return nil
	}}}
	_, err := s.rewriteForm(ctx, fields, map[string]string{"limit": strconv.Itoa(capture.DefaultBodyLimit)}, func(v map[string]string) Result {
		n, _ := strconv.Atoi(v["limit"])
		return captureMutation(ctx, s, capture.StartOptions{Owner: "cli", Scope: capture.Scope{Domains: localizedrewriteCSV(s.language,
			v["domains"]), ProcessPaths: localizedrewriteCSV(s.language,
			v["processes"]), AppPaths: localizedrewriteCSV(s.language,
			v["apps"])}, BodyLimit: n}, "start", "-")
	})
	return err
}
func localizedcaptureArchiveForm(language i18n.Context, export bool) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{{Key: "file", Label: menuText(language, "menu.request_capture.capture_archive_form.private_local_archive_path", "Private local archive path"), Required: true, Validate: func(v string) error {
			if v == "-" {
				return fmt.Errorf("%s", menuText(language, "menu.request_capture.capture_archive_form.enter_a_local_file_path", "enter a local file path"))
			}
			return nil
		}}}
		if export {
			fields = append(fields, Field{Key: "format", Label: menuText(language, "menu.request_capture.capture_archive_form.archive_format", "Archive format"), Choices: []string{"native", "har"}, ChoiceLabels: []string{menuState(language, "native"), menuState(language, "har")}})
		}
		_, err := s.rewriteForm(ctx, fields, nil, func(v map[string]string) Result {
			if !export {
				return captureMutation(ctx, s, nil, "import", v["file"])
			}
			r := rewriteRaw(ctx, s, "", "capture", "export", v["file"], v["format"])
			if localizedrewriteFailed(language, r) {
				s.ShowResult(nil, r)
			} else {
				s.feedback(feedbackSuccess, menuText(language, "menu.request_capture.capture_archive_form.original_content_exported_to_private_archive", "Original content exported to private archive."))
			}
			return r
		})
		return err
	}
}
func localizedcaptureRecordsPage(language i18n.Context, cursor string) *Page {
	return &Page{ID: "request_capture.captureRecordsPage.page1", Title: menuText(language, "menu.request_capture.capture_records_page.captured_records", "Captured records"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := captureCall(ctx, s, capture.ListOptions{Cursor: cursor, Limit: 100}, "list", "-")
		v := PageView{Context: menuText(language, "menu.request_capture.capture_records_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Empty: menuText(language, "menu.request_capture.capture_records_page.no_captured_records", "No captured records.")}
		if localizedrewriteFailed(language, r) {
			return v, localizedrewriteReadError(language, r)
		}
		var data capture.Reply
		if localizeddecodeMenuResult(language, r, &data) != nil {
			return v, fmt.Errorf("%s", menuText(language, "menu.request_capture.capture_records_page.capture_list_unavailable", "capture list unavailable"))
		}
		for _, record := range data.Records {
			record := record
			v.Items = append(v.Items, Item{ID: "request_capture.captureRecordsPage.item2", Label: fmt.Sprintf("%s | %s | %s | %s", record.ID, record.Kind, record.Started.Format("2006-01-02 15:04"), record.Outcome), Next: func() *Page { return localizedcaptureRecordPage(language, record.ID) }})
		}
		if data.NextCursor != "" {
			next := data.NextCursor
			v.Actions = append(v.Actions, Item{ID: "request_capture.captureRecordsPage.item3", Label: menuText(language, "menu.request_capture.capture_records_page.next_record_batch", "Next record batch"), Next: func() *Page { return localizedcaptureRecordsPage(language, next) }})
		}
		return v, nil
	}}
}
func localizedcaptureRecordPage(language i18n.Context, id string) *Page {
	return localizedactionPage(language, menuText(language, "menu.request_capture.capture_record_page.capture_record", "Capture record — ")+id, Item{ID: "request_capture.captureRecordPage.item1", Label: menuText(language, "menu.request_capture.capture_record_page.show_original_metadata", "Show original metadata"), Impact: menuText(language, "menu.request_capture.capture_record_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Action: func(ctx context.Context, s *Session) error {
		captureDisplay(s, captureCall(ctx, s, nil, "show", id))
		return nil
	}},
		Item{ID: "request_capture.captureRecordPage.item2", Label: menuText(language, "menu.request_capture.capture_record_page.read_body_chunk", "Read body chunk"), Impact: menuText(language, "menu.request_capture.capture_record_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Action: localizedcaptureBodyForm(language, id)},
		Item{ID: "request_capture.captureRecordPage.item3", Label: menuText(language, "menu.request_capture.capture_record_page.original_preview_fixture", "Original preview fixture"), Impact: menuText(language, "menu.request_capture.capture_record_page.requires_complete_original_bodies_no_network_req", "Requires complete original bodies; no network requests."), Action: func(ctx context.Context, s *Session) error {
			stage, e := s.selectProtocol(menuText(language, "menu.request_capture.capture_record_page.fixture_stage", "Fixture stage"), []string{"request", "response", "exchange", "websocket_client", "websocket_server"})
			if e != nil {
				return e
			}
			captureDisplay(s, captureCall(ctx, s, nil, "fixture", id, stage))
			return nil
		}},
		Item{ID: "request_capture.captureRecordPage.item4", Label: menuText(language, "menu.request_capture.capture_record_page.create_quick_rule", "Create quick rule"), Impact: menuText(language, "menu.request_capture.capture_record_page.derives_an_exact_original_host_method_path_match", "Derives an exact original host/method/path match; edit, preview and save separately."), Next: func() *Page { return localizedcaptureQuickRulePage(language, id) }},
	)
}
func localizedcaptureBodyForm(language i18n.Context, id string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{{Key: "snapshot", Label: menuText(language, "menu.request_capture.capture_body_form.snapshot", "Snapshot"), Choices: []string{"request_original", "request_actual", "response_original", "response_actual", "message_original", "message_actual"}, ChoiceLabels: []string{menuState(language, "request_original"), menuState(language, "request_actual"), menuState(language, "response_original"), menuState(language, "response_actual"), menuState(language, "message_original"), menuState(language, "message_actual")}}, {Key: "offset", Label: menuText(language, "menu.request_capture.capture_body_form.decoded_byte_offset", "Decoded byte offset"), Required: true, Validate: func(v string) error {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n < 0 {
				return fmt.Errorf("%s", menuText(language, "menu.request_capture.capture_body_form.enter_a_nonnegative_offset", "enter a nonnegative offset"))
			}
			return nil
		}}, {Key: "limit", Label: menuText(language, "menu.request_capture.capture_body_form.chunk_limit_1_262144", "Chunk limit (1–262144)"), Required: true, Validate: func(v string) error {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > capture.MaxChunk {
				return fmt.Errorf("enter 1–262144")
			}
			return nil
		}}}
		_, err := s.rewriteForm(ctx, fields, map[string]string{"offset": "0", "limit": strconv.Itoa(capture.MaxChunk)}, func(v map[string]string) Result {
			r := captureCall(ctx, s, nil, "body", id, v["snapshot"], v["offset"], v["limit"])
			if localizedrewriteFailed(language, r) {
				s.ShowResult(nil, r)
				return r
			}
			var data capture.Reply
			if localizeddecodeMenuResult(language, r, &data) != nil || data.Body == nil {
				return localizedrewriteFailure(language, menuText(language, "menu.request_capture.capture_body_form.capture_body_unavailable", "Capture body unavailable"))
			}
			captureDisplay(s, r)
			if utf8.Valid(data.Body.Data) {
				fmt.Fprintln(s.out, menuText(language, "menu.request_capture.capture_body_form.body_text", "Body text:"))
				fmt.Fprintln(s.out, string(data.Body.Data))
			}
			return r
		})
		return err
	}
}
func localizedcaptureQuickRulePage(language i18n.Context, id string) *Page {
	rule := rewrite.Rule{Stage: "request", Operations: []rewrite.Operation{}}
	enable := false
	return &Page{ID: "request_capture.captureQuickRulePage.page1", Title: menuText(language, "menu.request_capture.capture_quick_rule_page.quick_rule_from", "Quick rule from ") + id, Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Context: menuFormat(language, "menu.request_capture.capture_quick_rule_page.draft_only_id_s_stage_s_operations_d_enable_on_s", "Draft only | ID: %s | Stage: %s | Operations: %d | Enable on save: %t", rule.ID, rule.Stage, len(rule.Operations), enable), Actions: []Item{
			{ID: "request_capture.captureQuickRulePage.item2", Label: menuText(language, "menu.request_capture.capture_quick_rule_page.rule_fields", "Rule fields"), Action: func(ctx context.Context, s *Session) error {
				_, e := s.Form(ctx, []Field{{Key: "id", Label: menuText(language, "menu.request_capture.capture_quick_rule_page.rule_id", "Rule ID"), Required: true}, {Key: "stage", Label: menuText(language, "menu.request_capture.capture_quick_rule_page.stage", "Stage"), Choices: []string{"request", "response", "websocket_client", "websocket_server"}, ChoiceLabels: []string{menuState(language, "request"), menuState(language, "response"), menuState(language, "websocket_client"), menuState(language, "websocket_server")}}, {Key: "enable", Label: menuText(language, "menu.request_capture.capture_quick_rule_page.enable_candidate_on_save", "Enable candidate on save"), Choices: []string{"false", "true"}, ChoiceLabels: []string{menuState(language, "false"), menuState(language, "true")}}}, map[string]string{"id": rule.ID, "stage": rule.Stage, "enable": strconv.FormatBool(enable)}, func(v map[string]string) Result {
					rule.ID = v["id"]
					rule.Stage = v["stage"]
					enable = v["enable"] == "true"
					return Result{}
				})
				return e
			}},
			{ID: "request_capture.captureQuickRulePage.item3", Label: menuText(language, "menu.request_capture.capture_quick_rule_page.ordered_operations", "Ordered operations"), Next: func() *Page { return localizedrewriteOperationsPage(language, &rule) }},
			{ID: "request_capture.captureQuickRulePage.item4", Label: menuText(language, "menu.request_capture.capture_quick_rule_page.build_versioned_draft", "Build versioned draft"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				profileResult := s.Fetch(ctx, "profile", "list")
				if localizedrewriteFailed(language, profileResult) {
					s.ShowResult(nil, profileResult)
					return Navigation{}, nil
				}
				profile := localizedrewriteText(language, localizedmenuObject(language, profileResult.Data), "current")
				if profile == "unknown" {
					return Navigation{}, fmt.Errorf("%s", menuText(language, "menu.request_capture.capture_quick_rule_page.selected_profile_unavailable", "selected profile unavailable"))
				}
				r := captureCall(ctx, s, rewritedraft.Selection{Stage: rule.Stage, ID: rule.ID, Operations: rule.Operations, Enable: enable}, "draft", id, "-")
				if localizedrewriteFailed(language, r) {
					s.ShowResult(nil, r)
					return Navigation{}, nil
				}
				var d rewritedraft.Draft
				if localizeddecodeMenuResult(language, r, &d) != nil {
					return Navigation{}, fmt.Errorf("%s", menuText(language, "menu.request_capture.capture_quick_rule_page.invalid_draft_response", "invalid draft response"))
				}
				return Navigation{Next: localizedcaptureDraftPage(language, d, profile)}, nil
			}},
		}}, nil
	}}
}
func captureOpenDraft(ctx context.Context, s *Session) (Navigation, error) {
	var draft rewritedraft.Draft
	_, err := s.Form(ctx, []Field{{Key: "json", Label: menuText(s.language, "menu.request_capture.capture_open_draft.complete_versioned_draft_json_single_line", "Complete versioned draft JSON (single line)"), Required: true, Validate: func(v string) error {
		return localizedrewriteMenuJSON(s.language,
			v, &draft)
	}}}, nil, func(v map[string]string) Result {
		_ = localizedrewriteMenuJSON(s.language,
			v["json"], &draft)
		return Result{}
	})
	if err != nil {
		return Navigation{}, err
	}
	selection := s.Fetch(ctx, "profile", "list")
	if localizedrewriteFailed(s.language, selection) {
		s.ShowResult(nil, selection)
		return Navigation{}, nil
	}
	profile := localizedrewriteText(s.language,
		localizedmenuObject(s.language,
			selection.Data), "current")
	if profile == "unknown" {
		return Navigation{}, fmt.Errorf("%s", menuText(s.language, "menu.request_capture.capture_open_draft.selected_profile_unavailable", "selected profile unavailable"))
	}
	return Navigation{Next: localizedcaptureDraftPage(s.language,
		draft, profile)}, nil
}
func localizedcaptureDraftPage(language i18n.Context, draft rewritedraft.Draft, profile string) *Page {
	return &Page{ID: "request_capture.captureDraftPage.page1", Title: menuText(language, "menu.request_capture.capture_draft_page.versioned_capture_draft", "Versioned capture draft"), Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Context: menuFormat(language, "menu.request_capture.capture_draft_page.rule_s_base_version_s_position_d_enable_t_save_c", "Rule: %s | Base version: %s | Position: %d | Enable: %t\nSave checks the base version and retains global rewrite intent. Preview runs the full chain locally.", draft.Rule.ID, draft.BaseRuleVersion, draft.Position, draft.Enable), Actions: []Item{
			{ID: "request_capture.captureDraftPage.item2", Label: menuText(language, "menu.request_capture.capture_draft_page.view_complete_draft", "View complete draft"), Impact: menuText(language, "menu.request_capture.capture_draft_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Action: func(_ context.Context, s *Session) error { captureDisplay(s, Result{Data: draft}); return nil }},
			{ID: "request_capture.captureDraftPage.item3", Label: menuText(language, "menu.request_capture.capture_draft_page.edit_complete_draft_json", "Edit complete draft JSON"), Impact: menuText(language, "menu.request_capture.capture_draft_page.original_urls_headers_cookies_and_bodies_are_vis", "Original URLs, headers, cookies and bodies are visible in terminal scrollback."), Action: func(ctx context.Context, s *Session) error {
				b, _ := json.Marshal(draft)
				_, e := s.Form(ctx, []Field{{Key: "json", Label: menuText(language, "menu.request_capture.capture_draft_page.complete_versioned_draft_json", "Complete versioned draft JSON"), Required: true, Validate: func(v string) error { var d rewritedraft.Draft; return localizedrewriteMenuJSON(language, v, &d) }}}, map[string]string{"json": string(b)}, func(v map[string]string) Result {
					_ = localizedrewriteMenuJSON(language, v["json"], &draft)
					return Result{}
				})
				return e
			}},
			{ID: "request_capture.captureDraftPage.item4", Label: menuText(language, "menu.request_capture.capture_draft_page.preview_full_chain", "Preview full chain"), Impact: menuText(language, "menu.request_capture.capture_draft_page.no_network_requests_or_persistence_incomplete_or", "No network requests or persistence; incomplete originals cannot preview."), Action: func(ctx context.Context, s *Session) error {
				captureDisplay(s, rewriteSubmit(ctx, s, profile, draft, "draft", "preview", "-"))
				return nil
			}},
			{ID: "request_capture.captureDraftPage.item5", Label: menuText(language, "menu.request_capture.capture_draft_page.save_rule", "Save rule"), Impact: menuText(language, "menu.request_capture.capture_draft_page.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := rewriteSubmit(ctx, s, profile, draft, "draft", "save", "-")
				if localizedrewriteFailed(language, r) {
					s.feedback(feedbackWarning, menuText(language, "menu.request_capture.capture_draft_page.draft_retained_edit_or_explicitly_save_again_ver", "Draft retained. Edit or explicitly save again; version conflicts require rebuilding against current rules."))
				}
				return Navigation{Back: !localizedrewriteFailed(language, r)}, nil
			}},
		}}, nil
	}}
}
func captureArgs(args ...string) []string {
	return localizedcaptureArgs(i18n.New(i18n.English), args...)
}
func capturePage() *Page {
	return localizedcapturePage(i18n.New(i18n.English))
}
func captureArchiveForm(export bool) func(context.
	Context, *Session) error {
	return localizedcaptureArchiveForm(i18n.New(i18n.English), export)
}
func captureRecordsPage(cursor string) *Page {
	return localizedcaptureRecordsPage(i18n.New(i18n.English), cursor)
}
func captureRecordPage(id string) *Page {
	return localizedcaptureRecordPage(i18n.New(i18n.English), id)
}
func captureBodyForm(
	id string) func(context.
	Context, *Session) error {
	return localizedcaptureBodyForm(i18n.New(i18n.English), id)
}
func captureQuickRulePage(id string) *Page {
	return localizedcaptureQuickRulePage(i18n.New(i18n.English), id)
}
func captureDraftPage(draft rewritedraft.
	Draft, profile string) *Page {
	return localizedcaptureDraftPage(i18n.New(i18n.English), draft, profile)
}
