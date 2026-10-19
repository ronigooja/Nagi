package menu

import "github.com/ronigooja/Nagi/internal/i18n"

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

func localizedrewriteArgs(language i18n.Context, profile string, args ...string) []string {
	a := append([]string{"request-rewrite"}, args...)
	if profile != "" {
		a = append(a, "--profile", profile)
	}
	return a
}
func localizedrewriteFailed(language i18n.Context, r Result) bool {
	return r.Code != 0 || r.Error != nil
}
func localizedrewriteFailure(language i18n.Context, message string) Result {
	return Result{Code: 1, Error: &CLIError{Message: message}}
}
func localizedrewriteReadError(language i18n.Context, r Result) error {
	if r.Error != nil {
		return fmt.Errorf("%s", r.errorText(language))
	}
	return fmt.Errorf("%s", menuText(language, "menu.request_rewrite.rewrite_read_error.request_rewrite_data_unavailable", "request rewrite data unavailable"))
}
func rewriteSubmit(ctx context.Context, s *Session, profile string, value any, args ...string) Result {
	command := localizedrewriteArgs(s.language,
		profile, args...)
	var r Result
	if value == nil {
		r = s.Fetch(ctx, command...)
	} else if runner, ok := s.runner.(InputRunner); ok {
		b, err := json.Marshal(value)
		if err != nil {
			r = localizedrewriteFailure(s.language, menuText(s.language, "menu.request_rewrite.rewrite_submit.invalid_form_data", "Invalid form data"))
		} else {
			r = runner.RunInput(ctx, command, strings.NewReader(string(b)))
		}
	} else {
		r = localizedrewriteFailure(s.language, menuText(s.language, "menu.request_rewrite.rewrite_submit.command_input_unavailable", "Command input unavailable"))
	}
	s.discardInterrupts()
	if localizedrewriteFailed(s.language, r) {
		s.ShowResult(command, r)
	} else {
		s.feedback(feedbackSuccess, menuText(s.language, "menu.request_rewrite.rewrite_submit.request_rewrite_operation_completed", "Request rewrite operation completed."))
	}
	return r
}
func rewriteRaw(ctx context.Context, s *Session, profile string, args ...string) Result {
	runner, ok := s.runner.(RawRunner)
	if !ok {
		return localizedrewriteFailure(s.language, menuText(s.language, "menu.request_rewrite.rewrite_raw.explicit_export_unavailable", "Explicit export unavailable"))
	}
	return runner.RunRaw(ctx, localizedrewriteArgs(s.language,
		profile, args...))
}
func localizedrewriteText(language i18n.Context, m map[string]json.RawMessage, key string) string {
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
func localizedrewriteSummary(language i18n.Context, r Result) string {
	if localizedrewriteFailed(language, r) {
		return menuText(language, "menu.request_rewrite.rewrite_summary.profile_unknown_intent_unknown_actual_unavailabl", "Profile: unknown | Intent: unknown | Actual: unavailable | TUN: unknown | Service: unknown | CA: unknown")
	}
	m := localizedmenuObject(language, r.Data)
	var ca map[string]json.RawMessage
	_ = json.Unmarshal(m["ca"], &ca)
	text := menuText(language, "menu.request_rewrite.rewrite_summary.profile", "Profile: ") + localizedrewriteText(language, m, "profile") + menuText(language, "menu.request_rewrite.rewrite_summary.intent", " | Intent: ") + menuState(language, localizedrewriteText(language, m, "enabled_intent")) + menuText(language, "menu.request_rewrite.rewrite_summary.actual", " | Actual: ") + menuState(language, localizedrewriteText(language, m, "actual")) + menuText(language, "menu.rewrite.summary.tun", " | TUN: ") + menuState(language, localizedrewriteText(language, m, "tun_running")) + menuText(language, "menu.request_rewrite.rewrite_summary.service", " | Service: ") + menuState(language, localizedrewriteText(language, m, "service_running")) + menuText(language, "menu.rewrite.summary.ca_line", "\nCA: ") + menuState(language, localizedrewriteText(language, ca, "state")) + menuText(language, "menu.request_rewrite.rewrite_summary.trust", " | Trust: ") + menuState(language, localizedrewriteText(language, ca, "trust"))
	for _, key := range []string{"configured_rule_version", "active_rule_version", "generation", "engine_epoch", "last_error"} {
		text += "\n" + menuField(language, key) + ": " + localizedrewriteText(language, m, key)
	}
	// Recovery has a fixed safe status contract; it contains no request or rule values.
	if raw := m["recovery"]; len(raw) > 0 && string(raw) != "null" {
		text += menuText(language, "menu.request_rewrite.rewrite_summary.recovery",

			// RequestRewritePage binds child pages to the profile in this status snapshot.
			"\nRecovery: ") + string(raw)
	}
	return text
}

func localizedRequestRewritePage(language i18n.Context) *Page {
	return &Page{ID: "request_rewrite.RequestRewritePage.page1", Title: menuText(language, "menu.request_rewrite.request_rewrite_page.request_rewrite", "Request Rewrite"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := s.Fetch(ctx, "request-rewrite", "status")
		m := localizedmenuObject(language, r.Data)
		profile := localizedrewriteText(language, m, "profile")
		known := !localizedrewriteFailed(language, r) && profile != "unknown"
		view := PageView{Context: localizedrewriteSummary(language, r) + menuText(language, "menu.request_rewrite.request_rewrite_page.default_off_tun_must_actually_be_running_generat", "\nDefault off. TUN must actually be running; generate CA and establish trust separately. Ordinary proxy listeners are outside this feature.")}
		if known {
			if localizedrewriteText(language, m, "enabled_intent") == "true" {
				view.Actions = append(view.Actions, localizedrewriteRuntimeItem(language, profile, false))
			} else if localizedrewriteText(language, m, "tun_running") == "true" {
				view.Actions = append(view.Actions, localizedrewriteRuntimeItem(language, profile, true))
			} else {
				view.Context += menuText(language, "menu.request_rewrite.request_rewrite_page.enable_unavailable_actual_tun_is_not_confirmed_r", "\nEnable unavailable: actual TUN is not confirmed running. Open Network Access to start TUN.")
			}
			view.Actions = append(view.Actions,
				Item{ID: "request_rewrite.RequestRewritePage.item2", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.local_scripts", "Local Scripts"), Next: func() *Page { return localizedrewriteScriptsPage(language, profile) }},
				Item{ID: "request_rewrite.RequestRewritePage.item3", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.ordered_rules", "Ordered Rules"), Next: func() *Page { return localizedrewriteRulesPage(language, profile) }},
				Item{ID: "request_rewrite.RequestRewritePage.item4", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.validate_stored_document", "Validate stored document"), Action: func(ctx context.Context, s *Session) error {
					rewriteSubmit(ctx, s, profile, nil, "validate")
					return nil
				}},
				Item{ID: "request_rewrite.RequestRewritePage.item5", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.preview_staged_fixture", "Preview staged fixture"), Impact: menuText(language, "menu.request_rewrite.request_rewrite_page.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback.") + menuText(language, "menu.request_rewrite.request_rewrite_page.no_dns_network_or_persistence", " No DNS/network or persistence."), Next: func() *Page { return localizedrewritePreviewPage(language, profile) }},
				Item{ID: "request_rewrite.RequestRewritePage.item6", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.import_document_from_file", "Import document from file"), Impact: menuText(language, "menu.request_rewrite.request_rewrite_page.saves_profile_local_settings_updates_the_active", "Saves profile-local settings; updates the active generation if enabled. Does not start TUN."), Action: localizedrewriteFileForm(language, profile, "import")},
				Item{ID: "request_rewrite.RequestRewritePage.item7", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.export_document_to_file", "Export document to file"), Impact: menuText(language, "menu.request_rewrite.request_rewrite_page.sensitive_values_may_contain_credentials_urls_an", "Sensitive values may contain credentials, URLs and bodies; visible locally in terminal scrollback."), Action: localizedrewriteFileForm(language, profile, "export")})
		}
		view.Actions = append(view.Actions, Item{ID: "request_rewrite.RequestRewritePage.item8", Label: menuText(language, "menu.request_rewrite.request_rewrite_page.recovery", "Recovery"), Next: func() *Page { return localizedrewriteRecoveryPage(language, profile, known) }})
		if localizedrewriteFailed(language, r) {
			return view, localizedrewriteReadError(language, r)
		}
		return view, nil
	}}
}
func localizedrewriteRuntimeItem(language i18n.Context, profile string, enable bool) Item {
	action := "disable"
	effect := menuText(language, "menu.request_rewrite.rewrite_runtime_item.stops_rewriting_retains_rules_ca_and_os_trust", "Stops rewriting; retains rules, CA and OS trust.")
	if enable {
		action = "enable"
		effect = menuText(language, "menu.request_rewrite.rewrite_runtime_item.intercepts_matching_tun_http_https_only_requires", "Intercepts matching TUN HTTP/HTTPS only; requires compatible engine and trusted CA.")
	}
	return Item{ID: "request_rewrite.rewriteRuntimeItem.item1", Label: map[string]string{"enable": menuText(language, "menu.rewrite.enable", "Enable request rewrite"), "disable": menuText(language, "menu.rewrite.disable", "Disable request rewrite")}[action], Impact: effect, Action: func(ctx context.Context, s *Session) error {
		r := s.Fetch(ctx, "request-rewrite", "status")
		if localizedrewriteFailed(language, r) {
			s.ShowResult(nil, r)
			return nil
		}
		m := localizedmenuObject(language, r.Data)
		if localizedrewriteText(language, m, "profile") != profile {
			s.feedback(feedbackWarning, menuText(language, "menu.request_rewrite.rewrite_runtime_item.selected_profile_changed_reopen_request_rewrite", "Selected profile changed. Reopen Request Rewrite and choose again."))
			return nil
		}
		if enable && localizedrewriteText(language, m, "tun_running") != "true" {
			s.feedback(feedbackWarning, menuText(language, "menu.request_rewrite.rewrite_runtime_item.enable_unavailable_actual_tun_is_not_confirmed_r", "Enable unavailable: actual TUN is not confirmed running."))
			return nil
		}
		rewriteSubmit(ctx, s, "", nil, action)
		return nil
	}}
}
func localizedrewriteRecoveryPage(language i18n.Context, profile string, known bool) *Page {
	return &Page{ID: "request_rewrite.rewriteRecoveryPage.page1", Title: menuText(language, "menu.request_rewrite.rewrite_recovery_page.request_rewrite_recovery", "Request Rewrite — Recovery"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := s.Fetch(ctx, "request-rewrite", "status")
		v := PageView{Context: localizedrewriteSummary(language, r), Actions: []Item{{ID: "request_rewrite.rewriteRecoveryPage.item2", Label: menuText(language, "menu.request_rewrite.rewrite_recovery_page.disable_request_rewrite", "Disable request rewrite"), Impact: menuText(language, "menu.request_rewrite.rewrite_recovery_page.stops_rewriting_for_the_currently_selected_profi", "Stops rewriting for the currently selected profile; retains rules, CA and trust."), Action: func(ctx context.Context, s *Session) error { rewriteSubmit(ctx, s, "", nil, "disable"); return nil }}}}
		v.Actions = append(v.Actions, Item{ID: "request_rewrite.rewriteRecoveryPage.item3", Label: menuText(language, "menu.request_rewrite.rewrite_recovery_page.validate_stored_document", "Validate stored document"), Impact: menuText(language, "menu.request_rewrite.rewrite_recovery_page.read_only_validation_uses_the_selected_profile_i", "Read-only validation; uses the selected profile if status was unavailable."), Action: func(ctx context.Context, s *Session) error {
			target := profile
			if !known {
				selection := s.Fetch(ctx, "profile", "list")
				if localizedrewriteFailed(language, selection) {
					s.ShowResult(nil, selection)
					return nil
				}
				target = localizedrewriteText(language, localizedmenuObject(language, selection.Data), "current")
				if target == "unknown" {
					s.feedback(feedbackError, menuText(language, "menu.request_rewrite.rewrite_recovery_page.selected_profile_unavailable_repair_profile_sele", "Selected profile unavailable; repair profile selection through Profiles or CLI."))
					return nil
				}
			}
			rewriteSubmit(ctx, s, target, nil, "validate")
			return nil
		}})
		return v, nil
	}}
}
func localizedrewriteFileForm(language i18n.Context, profile, action string) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error {
		_, err := s.rewriteForm(ctx, []Field{{Key: "file", Label: menuText(language, "menu.request_rewrite.rewrite_file_form.private_local_file_path_not_stdin_stdout", "Private local file path (not stdin/stdout)"), Required: true, Validate: func(v string) error {
			if v == "-" {
				return fmt.Errorf("%s", menuText(language, "menu.request_rewrite.rewrite_file_form.enter_a_local_file_path", "enter a local file path"))
			}
			return nil
		}}}, nil, func(v map[string]string) Result {
			if action != "export" {
				return rewriteSubmit(ctx, s, profile, nil, action, v["file"])
			}
			r := rewriteRaw(ctx, s, profile, "export", v["file"])
			if localizedrewriteFailed(language, r) {
				s.ShowResult(nil, r)
			} else {
				s.feedback(feedbackSuccess, menuText(language, "menu.request_rewrite.rewrite_file_form.sensitive_document_exported_to_private_file", "Sensitive document exported to private file."))
			}
			return r
		})
		return err
	}
}
func localizedrewriteCAPage(language i18n.Context) *Page {
	return localizedrewriteCAPageForOS(language, runtime.GOOS)
}
func localizedrewriteCAPageForOS(language i18n.Context, goos string) *Page {
	return &Page{ID: "request_rewrite.rewriteCAPageForOS.page1", Title: menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.certificate_authority", "Certificate Authority"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		r := s.Fetch(ctx, "request-rewrite", "ca", "status")
		m := localizedmenuObject(language, r.Data)
		text := menuText(language, "menu.rewrite.summary.ca", "CA: ") + localizedrewriteText(language, m, "state") + menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.trust", " | Trust: ") + localizedrewriteText(language, m, "trust") + menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.generation_does_not_install_trust_public_export", "\nGeneration does not install trust. Public export never includes the private key.")
		for _, key := range []string{"fingerprint", "not_before", "not_after"} {
			if value := localizedrewriteText(language, m, key); value != "unknown" {
				if t, e := time.Parse(time.RFC3339, value); e == nil {
					if t.IsZero() {
						continue
					}
					value = t.Format("2006-01-02 15:04 MST")
				}
				text += "\n" + menuField(language, key) + ": " + value
			}
		}
		actions := []Item{{ID: "request_rewrite.rewriteCAPageForOS.item2", Label: menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.generate_local_ca", "Generate local CA"), Impact: menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.creates_a_private_per_user_ca_refuses_replacemen", "Creates a private per-user CA; refuses replacement; does not trust it."), Action: func(ctx context.Context, s *Session) error {
			rewriteSubmit(ctx, s, "", nil, "ca", "generate")
			return nil
		}},
			{ID: "request_rewrite.rewriteCAPageForOS.item3", Label: menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.export_public_certificate_to_file", "Export public certificate to file"), Action: func(ctx context.Context, s *Session) error {
				_, err := s.rewriteForm(ctx, []Field{{Key: "path", Label: menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.new_public_certificate_file_path", "New public certificate file path"), Required: true, Validate: func(v string) error {
					if v == "-" {
						return fmt.Errorf("%s", menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.enter_a_file_path", "enter a file path"))
					}
					return nil
				}}}, nil, func(v map[string]string) Result {
					r := rewriteRaw(ctx, s, "", "ca", "export", v["path"])
					if localizedrewriteFailed(language, r) {
						s.ShowResult(nil, r)
					} else {
						s.feedback(feedbackSuccess, menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.public_certificate_exported_private_key_excluded", "Public certificate exported; private key excluded."))
					}
					return r
				})
				return err
			}}}
		if goos == "darwin" {
			for _, verb := range []string{"install", "remove"} {
				verb := verb
				actions = append(actions, Item{ID: "request_rewrite.rewriteCAPageForOS.item4", Label: map[string]string{"install": menuText(language, "menu.rewrite.ca.install_trust", "Install CA trust"), "remove": menuText(language, "menu.rewrite.ca.remove_trust", "Remove CA trust")}[verb], Impact: menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.changes_login_keychain_trust_removal_tears_down", "Changes login keychain trust; removal tears down rewriting and retains intent/key."), Action: func(ctx context.Context, s *Session) error {
					rewriteSubmit(ctx, s, "", nil, "ca", "trust", verb)
					return nil
				}})
			}
		} else {
			text += menuText(language, "menu.request_rewrite.rewrite_c_a_page_for_o_s.linux_export_the_public_certificate_then_explici", "\nLinux: export the public certificate, then explicitly configure your distribution and each application's certificate store. To remove trust, remove that certificate from each store. Nagi never installs OS trust automatically.")
		}
		if localizedrewriteFailed(language, r) {
			return PageView{Context: text, Actions: actions}, localizedrewriteReadError(language, r)
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
			_, cancelled = s.selectProtocol(menuText(s.language, "menu.request_rewrite.rewrite_form.submit_form", "Submit form"), []string{menuText(s.language, "menu.request_rewrite.rewrite_form.submit", "Submit")})
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

func init() { RegisterPage("rewrite-capture", RewriteAndCapturePage) }

func localizedRewriteAndCapturePage(language i18n.Context) *Page {
	return localizedrewriteAndCapturePageForOS(language, runtime.GOOS)
}

func localizedrewriteAndCapturePageForOS(language i18n.Context, goos string) *Page {
	return localizedactionPage(language, menuText(language, "menu.request_rewrite.rewrite_and_capture_page_for_o_s.rewrite_capture", "Rewrite & Capture"), Item{ID: "request_rewrite.rewriteAndCapturePageForOS.item1", Label: menuText(language, "menu.request_rewrite.rewrite_and_capture_page_for_o_s.request_rewrite", "Request Rewrite"), Next: func() *Page {
		return localizedRequestRewritePage(
			language,
		)
	},
	}, Item{ID: "request_rewrite.rewriteAndCapturePageForOS.item2", Label: menuText(language, "menu.request_rewrite.rewrite_and_capture_page_for_o_s.request_capture", "Request Capture"), Next: func() *Page {
		return localizedcapturePage(
			language)
	},
	}, Item{ID: "request_rewrite.rewriteAndCapturePageForOS.item3", Label: menuText(language, "menu.request_rewrite.rewrite_and_capture_page_for_o_s.certificate_authority", "Certificate Authority"), Next: func() *Page { return localizedrewriteCAPageForOS(language, goos) }})
}
func rewriteArgs(profile string, args ...string) []string {
	return localizedrewriteArgs(i18n.New(i18n.English), profile, args...)
}
func rewriteFailed(r Result) bool {
	return localizedrewriteFailed(i18n.New(i18n.English), r)
}
func rewriteFailure(message string) Result {
	return localizedrewriteFailure(i18n.New(i18n.English), message)
}
func rewriteReadError(r Result) error {
	return localizedrewriteReadError(i18n.New(i18n.English), r)
}
func rewriteText(m map[string]json.RawMessage, key string) string {
	return localizedrewriteText(i18n.New(i18n.English), m, key)
}
func rewriteSummary(r Result) string {
	return localizedrewriteSummary(i18n.New(i18n.English), r)
}

func RequestRewritePage() *Page {
	return localizedRequestRewritePage(i18n.New(i18n.English))
}
func rewriteRuntimeItem(profile string, enable bool) Item {
	return localizedrewriteRuntimeItem(i18n.New(i18n.English), profile, enable)
}
func rewriteRecoveryPage(profile string, known bool) *Page {
	return localizedrewriteRecoveryPage(i18n.New(i18n.English), profile, known)
}
func rewriteFileForm(
	profile, action string) func(context.Context, *Session) error {
	return localizedrewriteFileForm(i18n.New(i18n.English), profile, action)
}
func rewriteCAPage() *Page {
	return localizedrewriteCAPage(i18n.New(i18n.English))
}
func rewriteCAPageForOS(goos string) *Page {
	return localizedrewriteCAPageForOS(i18n.New(i18n.English), goos)
}
func RewriteAndCapturePage() *Page {
	return localizedRewriteAndCapturePage(i18n.New(i18n.English))
}
func rewriteAndCapturePageForOS(goos string) *Page {
	return localizedrewriteAndCapturePageForOS(i18n.New(i18n.English), goos)
}
