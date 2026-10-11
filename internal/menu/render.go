package menu

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ronigooja/Nagi/internal/output"
)

func (s *Session) Printf(format string, args ...any) { fmt.Fprintf(s.out, format, args...) }
func (s *Session) WriteLine(line string)             { fmt.Fprintln(s.out, line) }

func (s *Session) render(page *Page, view PageView, index int, depth int) {
	fmt.Fprintf(s.out, "\n%s\n", s.styled(styleTitle, output.RedactError(page.Title)))
	if view.Context != "" {
		fmt.Fprintln(s.out, s.styled(styleContext, output.RedactError(view.Context)))
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
			s.writeFeedback(feedbackSuccess, "Cached subscription applied. Starting mihomo and enabling traffic access are separate actions.")
			return
		}
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
		for _, k := range keys {
			b, _ := json.Marshal(v[k])
			s.renderValue(strings.ReplaceAll(k, "_", " "), b, depth+1)
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
		s.Printf("%s%s: %s\n", indent, label, output.RedactError(fmt.Sprint(value)))
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
