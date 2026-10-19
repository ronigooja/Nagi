package output

import (
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ronigooja/Nagi/internal/terminal"
)

var stateWords = regexp.MustCompile(`(?i)\b(unexpected exit|running|stopped|unknown|unavailable|available|removed|enabled|disabled|active|inactive|automatic|fixed|paused|managed|unmanaged|unsupported|attention|partial|warning|failed|failure|error|conflict|healthy|unhealthy|ok|none|yes|no|true|false|rule|rules|global|direct|proxy|mixed|user|system|privileged|doh|stale)\b`)
var profileField = regexp.MustCompile(`(?i)(?:selected\s+)?profile\s*:\s*([^|\n,.(;]+)`)

func StylesEnabled(w io.Writer) bool {
	file, ok := underlyingWriter(w).(*os.File)
	term := os.Getenv("TERM")
	return ok && terminal.IsTerminal(file) && term != "" && term != "dumb" && os.Getenv("NO_COLOR") == ""
}

// StyleStateText colors recognized state words while preserving surrounding text.
func StyleStateText(text string, enabled bool) string {
	if !enabled || text == "" {
		return text
	}
	type span struct {
		start int
		end   int
		code  string
	}
	spans := []span{}
	profiles := profileField.FindAllStringSubmatchIndex(text, -1)
	for _, loc := range profiles {
		start, end := loc[2], loc[3]
		for start < end && text[start] == ' ' {
			start++
		}
		for end > start && text[end-1] == ' ' {
			end--
		}
		if start == end {
			continue
		}
		value := text[start:end]
		code := "32"
		switch strings.ToLower(value) {
		case "unknown", "none", "unavailable", "removed", "no profile":
			code = "33"
		}
		spans = append(spans, span{start: start, end: end, code: code})
	}
	for _, loc := range stateWords.FindAllStringIndex(text, -1) {
		if !stateValuePosition(text, loc[0], loc[1]) || overlapsProfile(profiles, loc[0], loc[1]) {
			continue
		}
		spans = append(spans, span{start: loc[0], end: loc[1], code: stateColor(text[loc[0]:loc[1]])})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out strings.Builder
	last := 0
	for _, part := range spans {
		if part.start < last {
			continue
		}
		out.WriteString(text[last:part.start])
		out.WriteString("\x1b[" + part.code + "m" + text[part.start:part.end] + "\x1b[0m")
		last = part.end
	}
	out.WriteString(text[last:])
	return out.String()
}

func stateValuePosition(text string, start, end int) bool {
	if prev := previousNonSpace(text, start); prev >= 0 {
		switch text[prev] {
		case ':', '(', '|', ',', ';', '=':
		default:
			return false
		}
	}
	if next := nextNonSpace(text, end); next < len(text) && text[next] == ':' {
		return false
	}
	for i := end; i < len(text); i++ {
		switch text[i] {
		case '|', ',', ';', '\n':
			return true
		case ':':
			return false
		}
	}
	return true
}

func previousNonSpace(text string, at int) int {
	for i := at - 1; i >= 0; i-- {
		if text[i] != ' ' && text[i] != '\t' {
			return i
		}
	}
	return -1
}

func nextNonSpace(text string, at int) int {
	for i := at; i < len(text); i++ {
		if text[i] != ' ' && text[i] != '\t' {
			return i
		}
	}
	return len(text)
}

func overlapsProfile(profiles [][]int, start, end int) bool {
	for _, loc := range profiles {
		if start < loc[3] && end > loc[2] {
			return true
		}
	}
	return false
}

func stateColor(word string) string {
	switch strings.ToLower(word) {
	case "running", "enabled", "active", "available", "automatic", "managed", "healthy", "ok", "yes", "true":
		return "32"
	case "stopped", "unexpected exit", "unknown", "unavailable", "removed", "disabled", "inactive", "paused", "unmanaged", "unsupported", "attention", "partial", "warning", "stale", "no", "false":
		return "33"
	case "failed", "failure", "error", "conflict", "unhealthy":
		return "31"
	default:
		return "94"
	}
}

func stateValue(w io.Writer, value string) string {
	display := stateText(w, value)
	if value == "" || !StylesEnabled(w) {
		return display
	}
	return "\x1b[" + stateColor(value) + "m" + display + "\x1b[0m"
}

// StyleStateValue colors a field value without inspecting its surrounding label.
func StyleStateValue(value string, enabled bool) string {
	if value == "" || !enabled {
		return value
	}
	return "\x1b[" + stateColor(value) + "m" + value + "\x1b[0m"
}

// StyleProfileValue highlights a selected profile name or warns on missing state.
func StyleProfileValue(value string, enabled bool) string {
	if value == "" || !enabled {
		return value
	}
	code := "32"
	switch strings.ToLower(value) {
	case "unknown", "none", "unavailable", "removed", "no profile":
		code = "33"
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// StyleProtocolState chooses styling from a stable protocol token, independently
// of its localized display text. Resource names must use profile styling instead.
func StyleProtocolState(token, display string, enabled bool) string {
	if display == "" || !enabled {
		return display
	}
	return "\x1b[" + stateColor(token) + "m" + display + "\x1b[0m"
}
