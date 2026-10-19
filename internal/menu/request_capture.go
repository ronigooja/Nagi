package menu

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

func captureArgs(args ...string) []string {
	return append([]string{"request-rewrite", "capture"}, args...)
}
func captureCall(ctx context.Context, s *Session, value any, args ...string) Result {
	command := captureArgs(args...)
	if value == nil {
		return s.Fetch(ctx, command...)
	}
	runner, ok := s.runner.(InputRunner)
	if !ok {
		return rewriteFailure("Command input unavailable")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return rewriteFailure("Invalid capture form")
	}
	r := runner.RunInput(ctx, command, strings.NewReader(string(b)))
	s.discardInterrupts()
	return r
}
func captureMutation(ctx context.Context, s *Session, value any, args ...string) Result {
	r := captureCall(ctx, s, value, args...)
	if rewriteFailed(r) {
		s.ShowResult(captureArgs(args...), r)
	} else {
		s.feedback(feedbackSuccess, "Capture operation completed.")
	}
	return r
}

// Explicit capture reads bypass ordinary redaction and styles, like raw config.
// JSON preserves bytes losslessly; human record timestamps omit seconds.
func captureDisplay(s *Session, r Result) Result {
	if rewriteFailed(r) {
		s.ShowResult(nil, r)
		return r
	}
	b, err := json.Marshal(r.Data)
	if err != nil {
		return rewriteFailure("Invalid capture response")
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
	fmt.Fprintln(s.out, captureSensitive)
	fmt.Fprintln(s.out, string(b))
	s.longResult = true
	return r
}
func capturePage() *Page {
	return &Page{Title: "Original Capture", Load: func(ctx context.Context, s *Session) (view PageView, err error) {
		check := captureCall(ctx, s, nil, "check")
		preparation := "Capture prerequisites: unable to check. Reopen this page to retry."
		var readiness capture.Reply
		if !rewriteFailed(check) && decodeMenuResult(check, &readiness) == nil && readiness.Readiness != nil {
			preparation = "Capture prerequisites: " + readiness.Readiness.Message
		}
		r := captureCall(ctx, s, nil, "status")
		var data capture.Reply
		v := PageView{Context: "Capture: unavailable\n" + captureSensitive + "\nClosing this menu retains the session."}
		defer func() { view.Context = preparation + "\n" + view.Context }()
		v.Actions = []Item{
			{Label: "Start with scope", Impact: "Starts a new memory-only session; no TUN or trust changes.", Action: captureStartForm},
			{Label: "Observed applications", Impact: captureSensitive, Action: func(ctx context.Context, s *Session) error {
				captureDisplay(s, captureCall(ctx, s, nil, "apps"))
				return nil
			}},
			{Label: "Captured records", Impact: captureSensitive, Next: func() *Page { return captureRecordsPage("") }},
			{Label: "Import native archive", Impact: "Replaces stopped history with an offline read-only session.", Action: captureArchiveForm(false)},
			{Label: "Export archive", Impact: captureSensitive, Action: captureArchiveForm(true)},
			{Label: "Open versioned draft JSON", Impact: "Edits in memory; preview and save are separate actions.", Navigate: captureOpenDraft},
		}
		if rewriteFailed(r) {
			return v, rewriteReadError(r)
		}
		if decodeMenuResult(r, &data) != nil {
			return v, fmt.Errorf("capture status unavailable")
		}
		if data.Status == nil {
			v.Context = "No retained capture session.\n" + captureSensitive
			return v, nil
		}
		st := data.Status
		v.Context = fmt.Sprintf("Session: %s | State: %s | Profile: %s | Backend: %s\nRecords: %d | Bytes: %d | Dropped: %d | Truncated: %d | Read only: %t\n%s", st.SessionID, st.State, st.Profile, st.Backend, st.Records, st.Bytes, st.Dropped, st.Truncated, st.ReadOnly, captureSensitive)
		if st.RecorderState != "" {
			v.Context += "\nRecorder state: " + st.RecorderState
		}
		// Recovery is CLI-owned and carries no captured content.
		var envelope struct {
			Status map[string]json.RawMessage `json:"status"`
		}
		_ = decodeMenuResult(r, &envelope)
		if recovery := rewriteText(envelope.Status, "recovery"); recovery != "unknown" {
			v.Context += "\nRecovery: " + recovery
		}
		scope, _ := json.Marshal(st.Scope)
		v.Context += "\nScope: " + string(scope) + fmt.Sprintf(" | Body limit: %d", st.BodyLimit)
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
				v.Actions = append(v.Actions, Item{Label: strings.Title(verb) + " capture", Impact: map[string]string{"pause": "Stops collection; retains history and open streams.", "resume": "Resumes this session.", "stop": "Stops collection; retains history.", "clear": "Deletes this session's recorded content."}[verb], Action: func(ctx context.Context, s *Session) error { captureMutation(ctx, s, nil, verb, id); return nil }})
			}
		}
		return v, nil
	}}
}
func captureStartForm(ctx context.Context, s *Session) error {
	fields := []Field{{Key: "domains", Label: "Domain patterns, comma separated (blank for all)"}, {Key: "processes", Label: "Exact process paths, comma separated (blank for all)"}, {Key: "apps", Label: "Outermost macOS .app paths, comma separated (blank for all)"}, {Key: "limit", Label: "Body limit in bytes (1–8388608)", Required: true, Validate: func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > capture.MaxBodyLimit {
			return fmt.Errorf("enter 1–8388608")
		}
		return nil
	}}}
	_, err := s.rewriteForm(ctx, fields, map[string]string{"limit": strconv.Itoa(capture.DefaultBodyLimit)}, func(v map[string]string) Result {
		n, _ := strconv.Atoi(v["limit"])
		return captureMutation(ctx, s, capture.StartOptions{Owner: "cli", Scope: capture.Scope{Domains: rewriteCSV(v["domains"]), ProcessPaths: rewriteCSV(v["processes"]), AppPaths: rewriteCSV(v["apps"])}, BodyLimit: n}, "start", "-")
	})
	return err
}
func captureArchiveForm(export bool) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{{Key: "file", Label: "Private local archive path", Required: true, Validate: func(v string) error {
			if v == "-" {
				return fmt.Errorf("enter a local file path")
			}
			return nil
		}}}
		if export {
			fields = append(fields, Field{Key: "format", Label: "Archive format", Choices: []string{"native", "har"}})
		}
		_, err := s.rewriteForm(ctx, fields, nil, func(v map[string]string) Result {
			if !export {
				return captureMutation(ctx, s, nil, "import", v["file"])
			}
			r := rewriteRaw(ctx, s, "", "capture", "export", v["file"], v["format"])
			if rewriteFailed(r) {
				s.ShowResult(nil, r)
			} else {
				s.feedback(feedbackSuccess, "Original content exported to private archive.")
			}
			return r
		})
		return err
	}
}
func captureRecordsPage(cursor string) *Page {
	return &Page{Title: "Captured records", Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := captureCall(ctx, s, capture.ListOptions{Cursor: cursor, Limit: 100}, "list", "-")
		v := PageView{Context: captureSensitive, Empty: "No captured records."}
		if rewriteFailed(r) {
			return v, rewriteReadError(r)
		}
		var data capture.Reply
		if decodeMenuResult(r, &data) != nil {
			return v, fmt.Errorf("capture list unavailable")
		}
		for _, record := range data.Records {
			record := record
			v.Items = append(v.Items, Item{Label: fmt.Sprintf("%s | %s | %s | %s", record.ID, record.Kind, record.Started.Format("2006-01-02 15:04"), record.Outcome), Next: func() *Page { return captureRecordPage(record.ID) }})
		}
		if data.NextCursor != "" {
			next := data.NextCursor
			v.Actions = append(v.Actions, Item{Label: "Next record batch", Next: func() *Page { return captureRecordsPage(next) }})
		}
		return v, nil
	}}
}
func captureRecordPage(id string) *Page {
	return actionPage("Capture record — "+id,
		Item{Label: "Show original metadata", Impact: captureSensitive, Action: func(ctx context.Context, s *Session) error {
			captureDisplay(s, captureCall(ctx, s, nil, "show", id))
			return nil
		}},
		Item{Label: "Read body chunk", Impact: captureSensitive, Action: captureBodyForm(id)},
		Item{Label: "Original preview fixture", Impact: "Requires complete original bodies; no network requests.", Action: func(ctx context.Context, s *Session) error {
			stage, e := s.Select("Fixture stage", []string{"request", "response", "exchange", "websocket_client", "websocket_server"})
			if e != nil {
				return e
			}
			captureDisplay(s, captureCall(ctx, s, nil, "fixture", id, stage))
			return nil
		}},
		Item{Label: "Create quick rule", Impact: "Derives an exact original host/method/path match; edit, preview and save separately.", Next: func() *Page { return captureQuickRulePage(id) }},
	)
}
func captureBodyForm(id string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{{Key: "snapshot", Label: "Snapshot", Choices: []string{"request_original", "request_actual", "response_original", "response_actual", "message_original", "message_actual"}}, {Key: "offset", Label: "Decoded byte offset", Required: true, Validate: func(v string) error {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n < 0 {
				return fmt.Errorf("enter a nonnegative offset")
			}
			return nil
		}}, {Key: "limit", Label: "Chunk limit (1–262144)", Required: true, Validate: func(v string) error {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > capture.MaxChunk {
				return fmt.Errorf("enter 1–262144")
			}
			return nil
		}}}
		_, err := s.rewriteForm(ctx, fields, map[string]string{"offset": "0", "limit": strconv.Itoa(capture.MaxChunk)}, func(v map[string]string) Result {
			r := captureCall(ctx, s, nil, "body", id, v["snapshot"], v["offset"], v["limit"])
			if rewriteFailed(r) {
				s.ShowResult(nil, r)
				return r
			}
			var data capture.Reply
			if decodeMenuResult(r, &data) != nil || data.Body == nil {
				return rewriteFailure("Capture body unavailable")
			}
			captureDisplay(s, r)
			if utf8.Valid(data.Body.Data) {
				fmt.Fprintln(s.out, "Body text:")
				fmt.Fprintln(s.out, string(data.Body.Data))
			}
			return r
		})
		return err
	}
}
func captureQuickRulePage(id string) *Page {
	rule := rewrite.Rule{Stage: "request", Operations: []rewrite.Operation{}}
	enable := false
	return &Page{Title: "Quick rule from " + id, Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Context: fmt.Sprintf("Draft only | ID: %s | Stage: %s | Operations: %d | Enable on save: %t", rule.ID, rule.Stage, len(rule.Operations), enable), Actions: []Item{
			{Label: "Rule fields", Action: func(ctx context.Context, s *Session) error {
				_, e := s.Form(ctx, []Field{{Key: "id", Label: "Rule ID", Required: true}, {Key: "stage", Label: "Stage", Choices: []string{"request", "response", "websocket_client", "websocket_server"}}, {Key: "enable", Label: "Enable candidate on save", Choices: []string{"false", "true"}}}, map[string]string{"id": rule.ID, "stage": rule.Stage, "enable": strconv.FormatBool(enable)}, func(v map[string]string) Result {
					rule.ID = v["id"]
					rule.Stage = v["stage"]
					enable = v["enable"] == "true"
					return Result{}
				})
				return e
			}},
			{Label: "Ordered operations", Next: func() *Page { return rewriteOperationsPage(&rule) }},
			{Label: "Build versioned draft", Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				profileResult := s.Fetch(ctx, "profile", "list")
				if rewriteFailed(profileResult) {
					s.ShowResult(nil, profileResult)
					return Navigation{}, nil
				}
				profile := rewriteText(menuObject(profileResult.Data), "current")
				if profile == "unknown" {
					return Navigation{}, fmt.Errorf("selected profile unavailable")
				}
				r := captureCall(ctx, s, rewritedraft.Selection{Stage: rule.Stage, ID: rule.ID, Operations: rule.Operations, Enable: enable}, "draft", id, "-")
				if rewriteFailed(r) {
					s.ShowResult(nil, r)
					return Navigation{}, nil
				}
				var d rewritedraft.Draft
				if decodeMenuResult(r, &d) != nil {
					return Navigation{}, fmt.Errorf("invalid draft response")
				}
				return Navigation{Next: captureDraftPage(d, profile)}, nil
			}},
		}}, nil
	}}
}
func captureOpenDraft(ctx context.Context, s *Session) (Navigation, error) {
	var draft rewritedraft.Draft
	_, err := s.Form(ctx, []Field{{Key: "json", Label: "Complete versioned draft JSON (single line)", Required: true, Validate: func(v string) error { return rewriteMenuJSON(v, &draft) }}}, nil, func(v map[string]string) Result { _ = rewriteMenuJSON(v["json"], &draft); return Result{} })
	if err != nil {
		return Navigation{}, err
	}
	selection := s.Fetch(ctx, "profile", "list")
	if rewriteFailed(selection) {
		s.ShowResult(nil, selection)
		return Navigation{}, nil
	}
	profile := rewriteText(menuObject(selection.Data), "current")
	if profile == "unknown" {
		return Navigation{}, fmt.Errorf("selected profile unavailable")
	}
	return Navigation{Next: captureDraftPage(draft, profile)}, nil
}
func captureDraftPage(draft rewritedraft.Draft, profile string) *Page {
	return &Page{Title: "Versioned capture draft", Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Context: fmt.Sprintf("Rule: %s | Base version: %s | Position: %d | Enable: %t\nSave checks the base version and retains global rewrite intent. Preview runs the full chain locally.", draft.Rule.ID, draft.BaseRuleVersion, draft.Position, draft.Enable), Actions: []Item{
			{Label: "View complete draft", Impact: captureSensitive, Action: func(_ context.Context, s *Session) error { captureDisplay(s, Result{Data: draft}); return nil }},
			{Label: "Edit complete draft JSON", Impact: captureSensitive, Action: func(ctx context.Context, s *Session) error {
				b, _ := json.Marshal(draft)
				_, e := s.Form(ctx, []Field{{Key: "json", Label: "Complete versioned draft JSON", Required: true, Validate: func(v string) error { var d rewritedraft.Draft; return rewriteMenuJSON(v, &d) }}}, map[string]string{"json": string(b)}, func(v map[string]string) Result { _ = rewriteMenuJSON(v["json"], &draft); return Result{} })
				return e
			}},
			{Label: "Preview full chain", Impact: "No network requests or persistence; incomplete originals cannot preview.", Action: func(ctx context.Context, s *Session) error {
				captureDisplay(s, rewriteSubmit(ctx, s, profile, draft, "draft", "preview", "-"))
				return nil
			}},
			{Label: "Save rule", Impact: rewriteEffect, Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				r := rewriteSubmit(ctx, s, profile, draft, "draft", "save", "-")
				if rewriteFailed(r) {
					s.feedback(feedbackWarning, "Draft retained. Edit or explicitly save again; version conflicts require rebuilding against current rules.")
				}
				return Navigation{Back: !rewriteFailed(r)}, nil
			}},
		}}, nil
	}}
}
