package menu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func rewritePreviewPage(profile string) *Page {
	return actionPage("Preview staged fixture",
		Item{Label: "Enter sample request", Impact: rewriteSensitive + " Transforms locally without DNS/network, persistence or ordinary logging.", Action: rewritePreviewForm(profile)},
		Item{Label: "Preview private fixture file", Impact: rewriteSensitive, Action: func(ctx context.Context, s *Session) error {
			_, err := s.rewriteForm(ctx, []Field{{Key: "path", Label: "Private staged fixture file path", Required: true}}, nil, func(v map[string]string) Result {
				if v["path"] == "-" {
					r := rewriteFailure("Enter a private file path; use the sample form for terminal input")
					s.ShowResult(nil, r)
					return r
				}
				return rewritePreviewResult(ctx, s, nil, profile, "preview", v["path"])
			})
			return err
		}},
		Item{Label: "Enter complete staged fixture JSON", Impact: rewriteSensitive + " Request, response, exchange and both WebSocket directions.", Action: rewritePreviewJSONForm(profile)},
		Item{Label: "Validate private document file", Action: rewriteFileForm(profile, "validate")},
	)
}
func rewritePreviewForm(profile string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		fields := []Field{
			{Key: "scheme", Label: "Scheme", Choices: []string{"https", "http"}},
			{Key: "host", Label: "Request host within enabled rule scope", Required: true},
			{Key: "port", Label: "Port", Required: true, Validate: func(v string) error {
				n, e := strconv.Atoi(v)
				if e != nil || n < 1 || n > 65535 {
					return fmt.Errorf("enter port 1–65535")
				}
				return nil
			}},
			{Key: "method", Label: "HTTP method", Required: true},
			{Key: "path", Label: "Escaped request path", Required: true},
			{Key: "query", Label: "Raw query (blank for none)"},
			{Key: "headers", Label: "Headers JSON object (name to string arrays; blank for {})", Validate: func(v string) error {
				if v == "" {
					return nil
				}
				var m map[string][]string
				if json.Unmarshal([]byte(v), &m) != nil || m == nil {
					return fmt.Errorf("enter an object of string arrays")
				}
				return nil
			}},
			{Key: "body", Label: "Body as strict base64 (blank for empty)", Validate: func(v string) error {
				if _, e := base64.StdEncoding.Strict().DecodeString(v); e != nil {
					return fmt.Errorf("enter strict base64")
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
	command := rewriteArgs(profile, args...)
	var r Result
	if fixture == nil {
		r = s.Fetch(ctx, command...)
	} else if runner, ok := s.runner.(InputRunner); ok {
		b, _ := json.Marshal(fixture)
		r = runner.RunInput(ctx, command, strings.NewReader(string(b)))
	} else {
		r = rewriteFailure("Command input unavailable")
	}
	s.discardInterrupts()
	if rewriteFailed(r) {
		s.ShowResult(command, r)
		return r
	}
	var data struct {
		Sensitive bool            `json:"sensitive"`
		Request   json.RawMessage `json:"request"`
	}
	if decodeMenuResult(r, &data) != nil || !data.Sensitive || len(data.Request) == 0 {
		r = rewriteFailure("Invalid sensitive preview response")
		s.ShowResult(command, r)
		return r
	}
	// This is the explicit sensitive display, deliberately bypassing ordinary
	// result formatting/redaction. It is never queued in normal page context.
	raw, _ := json.MarshalIndent(r.Data, "", "  ")
	fmt.Fprintln(s.out, "Sensitive preview — review privately before sharing:")
	fmt.Fprintln(s.out, string(raw))
	s.longResult = true
	return r
}

func rewritePreviewJSONForm(profile string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		_, err := s.rewriteForm(ctx, []Field{{Key: "json", Label: "Complete {stage,request,response?,message?} JSON (single line)", Required: true, Validate: func(v string) error { var candidate map[string]json.RawMessage; return rewriteMenuJSON(v, &candidate) }}}, nil, func(v map[string]string) Result {
			return rewritePreviewResult(ctx, s, json.RawMessage(v["json"]), profile, "preview", "-")
		})
		return err
	}
}
