package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const rewriteEffect = "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."
const rewriteSensitive = "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback."

func rewriteArgs(profile string, args ...string) []string {
	a := append([]string{"request-rewrite"}, args...)
	if profile != "" {
		a = append(a, "--profile", profile)
	}
	return a
}
func rewriteFailed(r Result) bool { return r.Code != 0 || r.Error != nil }
func rewriteFailure(message string) Result {
	return Result{Code: 1, Error: &CLIError{Message: message}}
}
func rewriteReadError(r Result) error {
	if r.Error != nil {
		return fmt.Errorf("%s", r.Error.Message)
	}
	return fmt.Errorf("request rewrite data unavailable")
}
func rewriteSubmit(ctx context.Context, s *Session, profile string, value any, args ...string) Result {
	command := rewriteArgs(profile, args...)
	var r Result
	if value == nil {
		r = s.Fetch(ctx, command...)
	} else if runner, ok := s.runner.(InputRunner); ok {
		b, err := json.Marshal(value)
		if err != nil {
			r = rewriteFailure("Invalid form data")
		} else {
			r = runner.RunInput(ctx, command, strings.NewReader(string(b)))
		}
	} else {
		r = rewriteFailure("Command input unavailable")
	}
	s.discardInterrupts()
	if rewriteFailed(r) {
		s.ShowResult(command, r)
	} else {
		s.feedback(feedbackSuccess, "Request rewrite operation completed.")
	}
	return r
}
func rewriteRaw(ctx context.Context, s *Session, profile string, args ...string) Result {
	runner, ok := s.runner.(RawRunner)
	if !ok {
		return rewriteFailure("Explicit export unavailable")
	}
	return runner.RunRaw(ctx, rewriteArgs(profile, args...))
}
func rewriteText(m map[string]json.RawMessage, key string) string {
	var str string
	if json.Unmarshal(m[key], &str) == nil && str != "" {
		return str
	}
	var b *bool
	if json.Unmarshal(m[key], &b) == nil && b != nil {
		return strconv.FormatBool(*b)
	}
	return "unknown"
}
func rewriteSummary(r Result) string {
	if rewriteFailed(r) {
		return "Profile: unknown | Intent: unknown | Actual: unavailable | TUN: unknown | Service: unknown | CA: unknown"
	}
	m := menuObject(r.Data)
	var ca map[string]json.RawMessage
	_ = json.Unmarshal(m["ca"], &ca)
	text := "Profile: " + rewriteText(m, "profile") + " | Intent: " + rewriteText(m, "enabled_intent") + " | Actual: " + rewriteText(m, "actual") + " | TUN: " + rewriteText(m, "tun_running") + " | Service: " + rewriteText(m, "service_running") + "\nCA: " + rewriteText(ca, "state") + " | Trust: " + rewriteText(ca, "trust")
	for _, key := range []string{"configured_rule_version", "active_rule_version", "generation", "engine_epoch", "last_error"} {
		text += "\n" + key + ": " + rewriteText(m, key)
	}
	// Recovery has a fixed safe status contract; it contains no request or rule values.
	if raw := m["recovery"]; len(raw) > 0 && string(raw) != "null" {
		text += "\nRecovery: " + string(raw)
	}
	return text
}

// RequestRewritePage binds child pages to the profile in this status snapshot.
func RequestRewritePage() *Page {
	return &Page{Title: "Request Rewrite", Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := s.Fetch(ctx, "request-rewrite", "status")
		m := menuObject(r.Data)
		profile := rewriteText(m, "profile")
		known := !rewriteFailed(r) && profile != "unknown"
		view := PageView{Context: rewriteSummary(r) + "\nDefault off. TUN must actually be running; generate CA and establish trust separately. Ordinary proxy listeners are outside this feature."}
		if known {
			if rewriteText(m, "enabled_intent") == "true" {
				view.Actions = append(view.Actions, rewriteRuntimeItem(profile, false))
			} else if rewriteText(m, "tun_running") == "true" {
				view.Actions = append(view.Actions, rewriteRuntimeItem(profile, true))
			} else {
				view.Context += "\nEnable unavailable: actual TUN is not confirmed running. Open Traffic Access to start TUN."
			}
			view.Actions = append(view.Actions,
				Item{Label: "Local Scripts", Next: func() *Page { return rewriteScriptsPage(profile) }},
				Item{Label: "Ordered Rules", Next: func() *Page { return rewriteRulesPage(profile) }},
				Item{Label: "Validate stored document", Action: func(ctx context.Context, s *Session) error {
					rewriteSubmit(ctx, s, profile, nil, "validate")
					return nil
				}},
				Item{Label: "Preview staged fixture", Impact: rewriteSensitive + " No DNS/network or persistence.", Next: func() *Page { return rewritePreviewPage(profile) }},
				Item{Label: "Import document from file", Impact: rewriteEffect, Action: rewriteFileForm(profile, "import")},
				Item{Label: "Export document to file", Impact: rewriteSensitive, Action: rewriteFileForm(profile, "export")})
		}
		view.Actions = append(view.Actions, Item{Label: "Original Capture", Next: capturePage}, Item{Label: "Certificate Authority", Next: rewriteCAPage}, Item{Label: "Recovery", Next: func() *Page { return rewriteRecoveryPage(profile, known) }})
		if rewriteFailed(r) {
			return view, rewriteReadError(r)
		}
		return view, nil
	}}
}
func rewriteRuntimeItem(profile string, enable bool) Item {
	action := "disable"
	effect := "Stops rewriting; retains rules, CA and OS trust."
	if enable {
		action = "enable"
		effect = "Intercepts matching TUN HTTP/HTTPS only; requires compatible engine and trusted CA."
	}
	return Item{Label: strings.Title(action) + " request rewrite", Impact: effect, Action: func(ctx context.Context, s *Session) error {
		r := s.Fetch(ctx, "request-rewrite", "status")
		if rewriteFailed(r) {
			s.ShowResult(nil, r)
			return nil
		}
		m := menuObject(r.Data)
		if rewriteText(m, "profile") != profile {
			s.feedback(feedbackWarning, "Selected profile changed. Reopen Request Rewrite and choose again.")
			return nil
		}
		if enable && rewriteText(m, "tun_running") != "true" {
			s.feedback(feedbackWarning, "Enable unavailable: actual TUN is not confirmed running.")
			return nil
		}
		rewriteSubmit(ctx, s, "", nil, action)
		return nil
	}}
}
func rewriteRecoveryPage(profile string, known bool) *Page {
	return &Page{Title: "Request Rewrite — Recovery", Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := s.Fetch(ctx, "request-rewrite", "status")
		v := PageView{Context: rewriteSummary(r), Actions: []Item{{Label: "Disable request rewrite", Impact: "Stops rewriting for the currently selected profile; retains rules, CA and trust.", Action: func(ctx context.Context, s *Session) error { rewriteSubmit(ctx, s, "", nil, "disable"); return nil }}}}
		v.Actions = append(v.Actions, Item{Label: "Validate stored document", Impact: "Read-only validation; uses the selected profile if status was unavailable.", Action: func(ctx context.Context, s *Session) error {
			target := profile
			if !known {
				selection := s.Fetch(ctx, "profile", "list")
				if rewriteFailed(selection) {
					s.ShowResult(nil, selection)
					return nil
				}
				target = rewriteText(menuObject(selection.Data), "current")
				if target == "unknown" {
					s.feedback(feedbackError, "Selected profile unavailable; repair profile selection through Profiles or CLI.")
					return nil
				}
			}
			rewriteSubmit(ctx, s, target, nil, "validate")
			return nil
		}})
		return v, nil
	}}
}
func rewriteFileForm(profile, action string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		_, err := s.rewriteForm(ctx, []Field{{Key: "file", Label: "Private local file path (not stdin/stdout)", Required: true, Validate: func(v string) error {
			if v == "-" {
				return fmt.Errorf("enter a local file path")
			}
			return nil
		}}}, nil, func(v map[string]string) Result {
			if action != "export" {
				return rewriteSubmit(ctx, s, profile, nil, action, v["file"])
			}
			r := rewriteRaw(ctx, s, profile, "export", v["file"])
			if rewriteFailed(r) {
				s.ShowResult(nil, r)
			} else {
				s.feedback(feedbackSuccess, "Sensitive document exported to private file.")
			}
			return r
		})
		return err
	}
}
func rewriteCAPage() *Page {
	return rewriteCAPageForOS(runtime.GOOS)
}
func rewriteCAPageForOS(goos string) *Page {
	return &Page{Title: "Request Rewrite — Certificate Authority", Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := s.Fetch(ctx, "request-rewrite", "ca", "status")
		m := menuObject(r.Data)
		text := "CA: " + rewriteText(m, "state") + " | Trust: " + rewriteText(m, "trust") + "\nGeneration does not install trust. Public export never includes the private key."
		for _, key := range []string{"fingerprint", "not_before", "not_after"} {
			if value := rewriteText(m, key); value != "unknown" {
				if t, e := time.Parse(time.RFC3339, value); e == nil {
					if t.IsZero() {
						continue
					}
					value = t.Format("2006-01-02 15:04 MST")
				}
				text += "\n" + key + ": " + value
			}
		}
		actions := []Item{{Label: "Generate local CA", Impact: "Creates a private per-user CA; refuses replacement; does not trust it.", Action: func(ctx context.Context, s *Session) error {
			rewriteSubmit(ctx, s, "", nil, "ca", "generate")
			return nil
		}},
			{Label: "Export public certificate to file", Action: func(ctx context.Context, s *Session) error {
				_, err := s.rewriteForm(ctx, []Field{{Key: "path", Label: "New public certificate file path", Required: true, Validate: func(v string) error {
					if v == "-" {
						return fmt.Errorf("enter a file path")
					}
					return nil
				}}}, nil, func(v map[string]string) Result {
					r := rewriteRaw(ctx, s, "", "ca", "export", v["path"])
					if rewriteFailed(r) {
						s.ShowResult(nil, r)
					} else {
						s.feedback(feedbackSuccess, "Public certificate exported; private key excluded.")
					}
					return r
				})
				return err
			}}}
		if goos == "darwin" {
			for _, verb := range []string{"install", "remove"} {
				verb := verb
				actions = append(actions, Item{Label: strings.Title(verb) + " CA trust", Impact: "Changes login keychain trust; removal tears down rewriting and retains intent/key.", Action: func(ctx context.Context, s *Session) error {
					rewriteSubmit(ctx, s, "", nil, "ca", "trust", verb)
					return nil
				}})
			}
		} else {
			text += "\nLinux: export the public certificate, then explicitly configure your distribution and each application's certificate store. To remove trust, remove that certificate from each store. Nagi never installs OS trust automatically."
		}
		if rewriteFailed(r) {
			return PageView{Context: text, Actions: actions}, rewriteReadError(r)
		}
		return PageView{Context: text, Actions: actions}, nil
	}}
}

// rewriteForm adds the first explicit Submit to the existing retained-form
// lifecycle. Cancellation after the last field therefore submits nothing.
// Failed attempts use Form's existing explicit Submit again, without a second
// confirmation or automatic retry.
func (s *Session) rewriteForm(ctx context.Context, fields []Field, values map[string]string, submit func(map[string]string) Result) (Result, error) {
	first := true
	var cancelled error
	result, err := s.Form(ctx, fields, values, func(v map[string]string) Result {
		if first {
			first = false
			_, cancelled = s.Select("Submit form", []string{"Submit"})
			if cancelled != nil {
				return Result{}
			}
		}
		return submit(v)
	})
	if cancelled != nil {
		return result, cancelled
	}
	return result, err
}
