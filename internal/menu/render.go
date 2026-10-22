package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/rules"
)

func (s *Session) Printf(format string, args ...any) { fmt.Fprintf(s.out, format, args...) }
func (s *Session) WriteLine(line string)             { fmt.Fprintln(s.out, line) }

func (s *Session) render(page *Page, view PageView, index int, depth int) {
	titleStyle := styleTitle
	if page.Root {
		titleStyle = styleRootTitle
	}
	fmt.Fprintf(s.out, "\n%s\n", s.styled(titleStyle, output.RedactError(page.Title)))
	if view.Context != "" {
		if view.ContextRaw {
			fmt.Fprintln(s.out, view.Context)
		} else {
			fmt.Fprintln(s.out, output.RedactError(view.Context))
		}
	}
	if len(view.Items) == 0 && view.Empty != "" {
		fmt.Fprintln(s.out, output.RedactError(view.Empty))
	}
	for n, choice := range localizednumberedChoices(s.language,
		view, index) {
		item := choice.Item
		label := output.RedactError(item.Label)
		style := styleChoice
		if item.Selected {
			style = styleSelected
		}
		fmt.Fprintf(s.out, "%d. %s", n+1, s.styled(style, label))
		if item.Impact != "" {
			fmt.Fprint(s.out, s.styled(styleImpact, " — "+output.RedactError(item.Impact)))
		}
		fmt.Fprintln(s.out)
	}
	if len(view.Items) > pageSize {
		fmt.Fprint(s.out, menuFormat(s.language, "menu.render.render.page_d_d", "Page %d/%d\n", index+1, (len(view.Items)+pageSize-1)/pageSize))
	}
	if depth == 0 {
		s.writeChoice(0, menuText(s.language, "menu.render.render.cancel", "Cancel"))
	} else if depth > 1 {
		s.writeChoice(0, menuText(s.language, "menu.render.render.back", "Back"))
	} else {
		s.writeChoice(0, menuText(s.language, "menu.render.render.exit_menu_mihomo_keeps_running", "Exit menu (mihomo keeps running)"))
	}

}

func (s *Session) showResult(result Result) {
	if result.Error != nil || result.Code != 0 {
		message := menuText(s.language, "menu.render.show_result.command_failed", "Command failed")

		if result.Error != nil {
			message = result.errorText(s.language)
		}
		s.writeFeedback(feedbackError, menuText(s.language, "menu.render.show_result.error", "Error: ")+
			message)
		return
	}
	if result.Data == nil {
		s.writeFeedback(feedbackSuccess, menuText(s.language, "menu.render.show_result.done", "Done."))
		return
	}
	switch value := result.Data.(type) {
	case string:
		fmt.Fprintln(s.out, value)
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			s.writeFeedback(feedbackSuccess, menuText(s.language, "menu.render.show_result.done", "Done."))
			return
		}
		fmt.Fprintln(s.out, strings.TrimSpace(string(data)))
	}
}

// ShowResult is the menu presentation boundary; the ordinary CLI is unchanged.
func (s *Session) ShowResult(args []string, result Result) {
	if s.active {
		var buf strings.Builder
		out := s.out
		s.out = &buf
		s.presentResult(args, result)
		s.out = out
		s.pending += buf.String()
		s.longResult = result.Code == 0 && result.Error == nil && localizedpausesForReading(s.language, args)
		return
	}
	s.presentResult(args, result)
}

func localizedpausesForReading(language i18n.Context, args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "logs":
		return true
	case "config":
		return len(args) > 1 && args[1] == "show"
	case "profile":
		return len(args) > 2 && (args[1] == "diff" || args[1] == "override" && args[2] == "show")
	}
	return false
}

func (s *Session) feedback(kind feedbackKind, text string) {
	text = s.styled(feedbackStyle(kind), output.RedactError(text))
	if s.active {
		s.pending += text + "\n"
	} else {
		s.WriteLine(text)
	}
}

func (s *Session) flushResult() {
	fmt.Fprint(s.out, s.pending)
	s.pending = ""
}

func (s *Session) presentResult(args []string, result Result) {
	if result.Error != nil || result.Code != 0 || result.Data == nil {
		s.showResult(result)
		return
	}
	if text, ok := result.Data.(string); ok {
		if !rawResult(args) {
			text = result.message(s.language, "/data", text)
		}
		s.WriteLine(text)
		return
	}
	data := localizedmenuObject(s.language,
		result.Data)
	if len(args) > 1 && args[0] == "profile" && args[1] == "diff" {
		var text string
		if json.Unmarshal(data["diff"], &text) == nil {
			s.WriteLine(text)
			return
		}
	}

	// History for groups and built-in actions is not a node measurement.
	if _, ok := data["node_delays"]; ok {
		raw := data["node_info"]
		var info map[string]menuNodeInfo
		var delays map[string]json.RawMessage
		_ = json.Unmarshal(raw, &info)
		if json.Unmarshal(data["node_delays"], &delays) == nil {
			for name := range delays {
				if localizednodeKind(s.language, name, info) != "node" {
					delete(delays, name)
				}
			}
			data["node_delays"], _ = json.Marshal(delays)
		}
	}
	delete(data, "node_info")
	if len(args) >= 2 && args[0] == "rules" && args[1] == "test" {
		encoded, _ := json.Marshal(result.Data)
		var test rules.TestResult
		if json.Unmarshal(encoded, &test) == nil {
			var text strings.Builder
			_ = output.WriteLocalized(&text, false, s.language, test)
			s.WriteLine(output.RedactError(strings.TrimSpace(text.String())))
			return
		}
	}
	if (len(args) >= 2 && args[0] == "config" && args[1] == "show") || (len(args) >= 3 && args[0] == "profile" && args[1] == "override" && args[2] == "show") {
		for _, key := range []string{"content", "config", "yaml"} {
			var text string
			if json.Unmarshal(data[key], &text) == nil {
				s.WriteLine(text)
				return
			}
		}
	}
	if len(args) > 0 && args[0] == "logs" {
		for _, key := range []string{"lines", "logs", "content"} {
			var text string
			if json.Unmarshal(data[key], &text) == nil {
				s.WriteLine(text)
				return
			}
			var lines []string
			if json.Unmarshal(data[key], &lines) == nil {
				for _, line := range lines {
					s.WriteLine(line)
				}
				return
			}
		}
	}
	if len(args) >= 2 && args[0] == "subscription" {
		switch args[1] {
		case "add":
			s.writeFeedback(feedbackSuccess, menuText(s.language, "menu.render.present_result.subscription_saved", "Subscription saved."))
			return
		case "update":
			s.writeFeedback(feedbackSuccess, menuText(s.language, "menu.render.present_result.cache_downloaded", "Cache downloaded."))
			return
		case "apply":
			s.writeFeedback(feedbackSuccess, menuText(s.language, "menu.render.present_result.cached_subscription_applied", "Cached subscription applied."))
			return
		}
	}
	if message := localizedmutationFeedback(s.language,
		args); message != "" {
		s.writeFeedback(feedbackSuccess, message)
		return
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s.renderField(key, data[key], 0, result, messagePointer("/data", key), false)
	}
}

func (s *Session) renderValue(key string, raw json.RawMessage, depth int) {
	s.renderField(strings.ReplaceAll(key, " ", "_"), raw, depth, Result{}, "", false)
}

func (s *Session) renderField(key string, raw json.RawMessage, depth int, result Result, pointer string, resource bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	label := key
	if !resource {
		label = menuField(s.language, key)
	}
	label = output.RedactError(label)
	indent := strings.Repeat("  ", depth)
	switch v := value.(type) {
	case map[string]any:
		s.Printf("%s%s:\n", indent, label)
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		limit := len(keys)
		if key == "node_delays" && limit > pageSize {
			limit = pageSize
		}
		for _, k := range keys[:limit] {
			b, _ := json.Marshal(v[k])
			s.renderField(k, b, depth+1, result, messagePointer(pointer, k), resourceMap(key))
		}
		if len(keys) > limit {
			fmt.Fprint(s.out, menuFormat(s.language, "menu.render.render_value.s_d_more_measurements_use_the_ordinary_cli_for_f", "%s  %d more measurements; use the ordinary CLI for full details.\n", indent, len(keys)-limit))
		}
	case []any:
		fmt.Fprint(s.out, menuFormat(s.language, "menu.render.render_value.s_s_d_entries", "%s%s: %d entries\n", indent, label, len(v)))
		limit := len(v)
		if limit > pageSize {
			limit = pageSize
		}
		for i, entry := range v[:limit] {
			b, _ := json.Marshal(entry)
			s.renderField("-", b, depth+1, result, messagePointer(pointer, strconv.Itoa(i)), false)
		}
		if len(v) > limit {
			fmt.Fprint(s.out, menuFormat(s.language, "menu.render.render_value.s_d_more_entries_use_the_resource_list_or_ordina", "%s  %d more entries; use the resource list or ordinary CLI for full details.\n", indent, len(v)-limit))
		}
	default:
		text := fmt.Sprint(value)
		if value == nil {
			text = "unknown"
		}
		if original, ok := value.(string); ok {
			text = result.message(s.language, pointer, original)
		}
		text = output.RedactError(text)
		if !resource {
			switch key {
			case "running":
				state := "unknown"
				if running, ok := value.(bool); ok {
					state = "stopped"
					if running {
						state = "running"
					}
				}
				fmt.Fprint(s.out, menuFormat(s.language, "menu.render.render_value.smihomo_s", "%sMihomo: %s\n", indent, s.stateValue(state)))
				return
			case "delay_ms":
				if value == float64(0) {
					text = "<1"
				}
			case "measured_at", "started_at", "updated_at", "expires_at", "next_due_at", "last_attempt_at", "last_success_at":
				text = localizedsubscriptionTime(s.language, fmt.Sprint(value))
			case "test_url":
				text = output.RedactError(localizedmenuTestEndpoint(s.language, fmt.Sprint(value)))
			case "profile", "selected_profile":
				if value != nil && text != "" {
					text = s.styled(styleSelected, text)
				}
			}
			if stateField(key) {
				state := fmt.Sprint(value)
				if value == nil {
					state = "unknown"
				}
				text = s.stateValue(state)
			}
			if flag, ok := value.(bool); ok {
				state := "no"
				if flag {
					state = "yes"
				}
				text = s.stateValue(state)
			}
		}
		s.Printf("%s%s: %s\n", indent, label, text)
	}
}

func localizedstatusLabel(language i18n.Context, label string) bool {
	switch strings.ToLower(label) {
	case "active", "automatic", "backend", "enabled", "healthy", "installed", "mode", "policy", "scheduler", "selected status", "selection status", "status", "state", "tun enabled":
		return true
	default:
		return false
	}
}

func (s *Session) writeFeedback(kind feedbackKind, text string) {
	s.WriteLine(s.styled(feedbackStyle(kind), output.RedactError(text)))
}

func (s *Session) writeChoice(number int, label string) {
	label = output.RedactError(label)
	if number > 0 {
		label = s.styled(styleChoice, label)
	}
	s.Printf("%d. %s\n", number, label)
}

// Only known mutations use short feedback; reads and diagnostics keep their data.
func localizedmutationFeedback(language i18n.Context, args []string) string {
	if len(args) < 2 {
		return ""
	}
	switch args[0] {
	case "profile":
		if args[1] == "override" && len(args) > 2 {
			return map[string]string{
				"set": menuText(language, "menu.render.mutation_feedback.local_override_saved", "Local override saved."), "clear": menuText(language, "menu.render.mutation_feedback.local_override_cleared", "Local override cleared."),
				"apply-default": menuText(language, "menu.render.mutation_feedback.default_runtime_settings_applied", "Default runtime settings applied."), "restore-default": menuText(language, "menu.render.mutation_feedback.previous_local_settings_restored", "Previous local settings restored."),
			}[args[2]]
		}
		return map[string]string{
			"use": menuText(language, "menu.render.mutation_feedback.profile_selected", "Profile selected."), "import": menuText(language, "menu.render.mutation_feedback.configuration_imported", "Configuration imported."),
			"backup": menuText(language, "menu.render.mutation_feedback.configuration_backed_up", "Configuration backed up."), "restore": menuText(language, "menu.render.mutation_feedback.backup_restored", "Backup restored."), "remove": menuText(language, "menu.render.mutation_feedback.profile_removed", "Profile removed."),
		}[args[1]]
	case "proxy":
		if args[1] == "healthcheck" && len(args) > 2 {
			if args[2] == "url" && len(args) > 3 {
				return map[string]string{
					"set":   menuText(language, "menu.render.mutation_feedback.healthcheck_test_url_saved", "Test URL saved."),
					"reset": menuText(language, "menu.render.mutation_feedback.inherited_healthcheck_test_url_restored", "Test URL inherited."),
				}[args[3]]
			}
			return map[string]string{"set": menuText(language, "menu.render.mutation_feedback.local_healthcheck_interval_saved", "Local healthcheck interval saved."), "reset": menuText(language, "menu.render.mutation_feedback.inherited_healthcheck_interval_restored", "Inherited healthcheck interval restored.")}[args[2]]
		}
		if args[1] == "group" && len(args) > 2 {
			return map[string]string{
				"add":    groupText(language, "created", "Proxy group created."),
				"edit":   groupText(language, "updated", "Proxy group updated."),
				"remove": groupText(language, "deleted", "Proxy group deleted."),
			}[args[2]]
		}
		if args[1] == "provider" && len(args) > 2 {
			return map[string]string{
				"add": menuText(language, "menu.render.mutation_feedback.provider_added", "Provider added."), "remove": menuText(language, "menu.render.mutation_feedback.provider_removed", "Provider removed."), "update": menuText(language, "menu.render.mutation_feedback.provider_updated", "Provider updated."), "healthcheck": menuText(language, "menu.render.mutation_feedback.healthcheck_started", "Healthcheck started."),
			}[args[2]]
		}
	case "dns":
		if args[1] == "exception" && len(args) > 2 {
			return map[string]string{"add": menuText(language, "menu.render.mutation_feedback.dns_exception_saved", "DNS exception saved."), "remove": menuText(language, "menu.render.mutation_feedback.dns_exception_removed", "DNS exception removed.")}[args[2]]
		}
		if args[1] == "tun" && len(args) > 2 {
			return map[string]string{"on": menuText(language, "menu.render.mutation_feedback.tun_dns_interception_enabled", "TUN DNS interception enabled."), "off": menuText(language, "menu.render.mutation_feedback.tun_dns_interception_disabled", "TUN DNS interception disabled.")}[args[2]]
		}
		return map[string]string{"set": menuText(language, "menu.render.mutation_feedback.dns_policy_saved", "DNS policy saved."), "flush": menuText(language, "menu.render.mutation_feedback.dns_cache_cleared", "DNS cache cleared.")}[args[1]]
	case "rules":
		return map[string]string{
			"add": menuText(language, "menu.render.mutation_feedback.rule_saved", "Rule saved."), "remove": menuText(language, "menu.render.mutation_feedback.rule_or_set_removed", "Rule or set removed."),
			"enable": menuText(language, "menu.render.mutation_feedback.rule_or_set_enabled", "Rule or set enabled."), "disable": menuText(language, "menu.render.mutation_feedback.rule_or_set_disabled", "Rule or set disabled."),
			"import": menuText(language, "menu.render.mutation_feedback.rules_imported", "Rules imported."),
			"edit":   menuText(language, "menu.rules.manage.saved", "Rule updated."),
			"update": menuText(language, "menu.rules.manage.updated", "Rules updated."),
		}[args[1]]
	}
	return ""
}
func pausesForReading(args []string) bool {
	return localizedpausesForReading(i18n.New(i18n.English), args)
}
func statusLabel(label string) bool {
	return localizedstatusLabel(i18n.New(i18n.English), label)
}

func mutationFeedback(args []string) string {
	return localizedmutationFeedback(i18n.New(i18n.English), args)
}
