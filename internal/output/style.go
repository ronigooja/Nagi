package output

import (
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/ronigooja/Nagi/internal/terminal"
)

var stateWords = regexp.MustCompile(`(?i)\b(unexpected exit|running|stopped|unknown|unavailable|available|removed|enabled|disabled|active|inactive|automatic|fixed|paused|managed|unmanaged|unsupported|attention|partial|warning|failed|failure|error|conflict|healthy|unhealthy|ok|none|yes|no|true|false|rule|rules|global|direct|proxy|mixed|user|system|privileged|doh|stale)\b`)

func StylesEnabled(w io.Writer) bool {
	file, ok := w.(*os.File)
	term := os.Getenv("TERM")
	return ok && terminal.IsTerminal(file) && term != "" && term != "dumb" && os.Getenv("NO_COLOR") == ""
}

// StyleStateText colors recognized state words while preserving surrounding text.
func StyleStateText(text string, enabled bool) string {
	if !enabled || text == "" {
		return text
	}
	var out strings.Builder
	last := 0
	for _, loc := range stateWords.FindAllStringIndex(text, -1) {
		out.WriteString(text[last:loc[0]])
		word := text[loc[0]:loc[1]]
		code := stateColor(word)
		out.WriteString("\x1b[" + code + "m" + word + "\x1b[0m")
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
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
	if value == "" || !StylesEnabled(w) {
		return value
	}
	return "\x1b[" + stateColor(value) + "m" + value + "\x1b[0m"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
