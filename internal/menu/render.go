package menu

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ronigooja/Nagi/internal/output"
	"github.com/ronigooja/Nagi/internal/rules"
)

func (s *Session) Printf(format string, args ...any) { fmt.Fprintf(s.out, format, args...) }
func (s *Session) WriteLine(line string)             { fmt.Fprintln(s.out, line) }

func (s *Session) render(page *Page, view PageView, index int, depth int) {
	titleStyle := styleTitle
	if page.Title == "Nagi menu" {
		titleStyle = styleRootTitle
	}
	fmt.Fprintf(s.out, "\n%s\n", s.styled(titleStyle, output.RedactError(page.Title)))
	if view.Context != "" {
		fmt.Fprintln(s.out, s.styledContext(output.RedactError(view.Context)))
	}
	if len(view.Items) == 0 && view.Empty != "" {
		fmt.Fprintln(s.out, output.RedactError(view.Empty))
	}
	for n, choice := range numberedChoices(view, index) {
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
		fmt.Fprintf(s.out, "Page %d/%d\n", index+1, (len(view.Items)+pageSize-1)/pageSize)
	}
	if depth == 0 {
		s.writeChoice(0, "Cancel")
	} else if depth > 1 {
		s.writeChoice(0, "Back")
	} else {
		s.writeChoice(0, "Exit menu (mihomo keeps running)")
	}

}

func (s *Session) showResult(result Result) {
	if result.Error != nil || result.Code != 0 {
		message := "Command failed"
		if result.Error != nil {
			message = result.Error.Message
		}
		s.writeFeedback(feedbackError, "Error: "+message)
		return
	}
	if result.Data == nil {
		s.writeFeedback(feedbackSuccess, "Done.")
		return
	}
	switch value := result.Data.(type) {
	case string:
		fmt.Fprintln(s.out, value)
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			s.writeFeedback(feedbackSuccess, "Done.")
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
		s.longResult = result.Code == 0 && result.Error == nil && pausesForReading(args)
		return
	}
	s.presentResult(args, result)
}

func pausesForReading(args []string) bool {
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
		s.WriteLine(text)
		return
	}
	data := menuObject(result.Data)
	if len(args) >= 2 && args[0] == "rules" && args[1] == "test" {
		encoded, _ := json.Marshal(result.Data)
		var test rules.TestResult
		if json.Unmarshal(encoded, &test) == nil {
			var text strings.Builder
			_ = output.Write(&text, false, test)
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
			s.writeFeedback(feedbackSuccess, "Subscription saved.")
			return
		case "update":
			s.writeFeedback(feedbackSuccess, "Cache downloaded.")
			return
		case "apply":
			s.writeFeedback(feedbackSuccess, "Cached subscription applied.")
			return
		}
	}
	if message := mutationFeedback(args); message != "" {
		s.writeFeedback(feedbackSuccess, message)
		return
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s.renderValue(strings.ReplaceAll(key, "_", " "), data[key], 0)
	}
}

func (s *Session) renderValue(label string, raw json.RawMessage, depth int) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
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
		if label == "node delays" && limit > pageSize {
			limit = pageSize
		}
		for _, k := range keys[:limit] {
			b, _ := json.Marshal(v[k])
			s.renderValue(strings.ReplaceAll(k, "_", " "), b, depth+1)
		}
		if len(keys) > limit {
			s.Printf("%s  %d more measurements; use the ordinary CLI for full details.\n", indent, len(keys)-limit)
		}
	case []any:
		s.Printf("%s%s: %d entries\n", indent, label, len(v))
		limit := len(v)
		if limit > pageSize {
			limit = pageSize
		}
		for _, entry := range v[:limit] {
			b, _ := json.Marshal(entry)
			s.renderValue("-", b, depth+1)
		}
		if len(v) > limit {
			s.Printf("%s  %d more entries; use the resource list or ordinary CLI for full details.\n", indent, len(v)-limit)
		}
	default:
		if value == nil {
			value = "unknown"
		}
		if label == "running" {
			state := "stopped"
			if running, ok := value.(bool); ok && running {
				state = "running"
			}
			s.Printf("%sMihomo: %s\n", indent, output.StyleStateValue(state, s.styles))
			return
		}
		text := output.RedactError(fmt.Sprint(value))
		if label == "test url" {
			label = "test endpoint"
			text = output.RedactError(menuTestEndpoint(fmt.Sprint(value)))
		}
		if strings.EqualFold(label, "profile") || strings.EqualFold(label, "selected profile") {
			text = output.StyleProfileValue(text, s.styles)
		} else if statusLabel(label) {
			if flag, ok := value.(bool); ok {
				text = "no"
				if flag {
					text = "yes"
				}
			}
			text = output.StyleStateValue(text, s.styles)
		}
		s.Printf("%s%s: %s\n", indent, label, text)
	}
}

func statusLabel(label string) bool {
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
func mutationFeedback(args []string) string {
	if len(args) < 2 {
		return ""
	}
	switch args[0] {
	case "profile":
		if args[1] == "override" && len(args) > 2 {
			return map[string]string{
				"set": "Local override saved.", "clear": "Local override cleared.",
				"apply-default": "Default runtime settings applied.", "restore-default": "Previous local settings restored.",
			}[args[2]]
		}
		return map[string]string{
			"use": "Profile selected.", "import": "Configuration imported.",
			"backup": "Configuration backed up.", "restore": "Backup restored.", "remove": "Profile removed.",
		}[args[1]]
	case "proxy":
		if args[1] == "healthcheck" && len(args) > 2 {
			return map[string]string{"set": "Local healthcheck interval saved.", "reset": "Profile healthcheck interval restored."}[args[2]]
		}
		if args[1] == "provider" && len(args) > 2 {
			return map[string]string{
				"add": "Provider added.", "remove": "Provider removed.", "update": "Provider updated.", "healthcheck": "Healthcheck started.",
			}[args[2]]
		}
	case "dns":
		if args[1] == "exception" && len(args) > 2 {
			return map[string]string{"add": "DNS exception saved.", "remove": "DNS exception removed."}[args[2]]
		}
		if args[1] == "tun" && len(args) > 2 {
			return map[string]string{"on": "TUN DNS interception enabled.", "off": "TUN DNS interception disabled."}[args[2]]
		}
		return map[string]string{"set": "DNS policy saved.", "flush": "DNS cache cleared."}[args[1]]
	case "rules":
		return map[string]string{
			"add": "Rule saved.", "remove": "Rule or set removed.",
			"enable": "Rule or set enabled.", "disable": "Rule or set disabled.",
			"import": "Rules imported.",
		}[args[1]]
	}
	return ""
}
