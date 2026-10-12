package menu

import (
	"io"
	"os"
	"regexp"
	"strings"
)

// textStyle keeps terminal presentation local to the menu. Every styled span
// resets immediately so user input, raw command output, and streams stay plain.
type textStyle uint8

const (
	styleTitle textStyle = iota
	styleSelected
	styleChoice
	styleImpact
	styleContext
	styleSuccess
	styleWarning
	styleError
	stylePrompt
	styleStatusPositive
	styleStatusWarning
	styleStatusError
	styleStatusNeutral
)

var contextStatusWords = regexp.MustCompile(`(?i)\b(unexpected exit|running|stopped|unknown|unavailable|available|enabled|disabled|active|inactive|automatic|fixed|paused|managed|unmanaged|unsupported|attention|partial|warning|failed|failure|error|conflict|healthy|unhealthy|ok|none|yes|no|rule|rules|global|direct|proxy|mixed|user|system|privileged)\b`)

func shouldStyle(out io.Writer) bool {
	file, ok := out.(*os.File)
	term := os.Getenv("TERM")
	return ok && IsTerminal(file) && term != "" && term != "dumb" && os.Getenv("NO_COLOR") == ""
}

func (s *Session) styled(style textStyle, text string) string {
	if !s.styles || text == "" {
		return text
	}
	code := ""
	switch style {
	case styleTitle:
		code = "33"
	case styleSelected:
		code = "32"
	case styleChoice:
		code = "94"
	case styleImpact:
		code = "2"
	case styleContext, stylePrompt:
		return text
	case styleSuccess:
		code = "32"
	case styleWarning:
		code = "33"
	case styleError:
		code = "31"
	case styleStatusPositive:
		code = "32"
	case styleStatusWarning:
		code = "33"
	case styleStatusError:
		code = "31"
	case styleStatusNeutral:
		code = "94"
	default:
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// styledContext colors recognizable state values while leaving the surrounding
// summary and recovery guidance in the terminal's default foreground.
func (s *Session) styledContext(text string) string {
	if !s.styles || text == "" {
		return text
	}
	var out strings.Builder
	last := 0
	for _, loc := range contextStatusWords.FindAllStringIndex(text, -1) {
		out.WriteString(text[last:loc[0]])
		word := text[loc[0]:loc[1]]
		style := styleStatusNeutral
		switch strings.ToLower(word) {
		case "running", "enabled", "active", "available", "automatic", "managed", "healthy", "ok", "yes":
			style = styleStatusPositive
		case "stopped", "unexpected exit", "unknown", "unavailable", "disabled", "inactive", "paused", "unmanaged", "unsupported", "attention", "partial", "warning", "no":
			style = styleStatusWarning
		case "failed", "failure", "error", "conflict", "unhealthy":
			style = styleStatusError
		}
		out.WriteString(s.styled(style, word))
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

type feedbackKind uint8

const (
	feedbackSuccess feedbackKind = iota
	feedbackWarning
	feedbackError
)

func feedbackStyle(kind feedbackKind) textStyle {
	switch kind {
	case feedbackSuccess:
		return styleSuccess
	case feedbackWarning:
		return styleWarning
	default:
		return styleError
	}
}
