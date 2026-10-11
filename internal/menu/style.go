package menu

import (
	"io"
	"os"
)

// textStyle keeps terminal presentation local to the menu. Every styled span
// resets immediately so user input, raw command output, and streams stay plain.
type textStyle uint8

const (
	styleTitle textStyle = iota
	styleSelected
	styleContext
	styleSuccess
	styleWarning
	styleError
	stylePrompt
)

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
		code = "1;33"
	case styleSelected:
		code = "1;32"
	case styleContext, stylePrompt:
		code = "1"
	case styleSuccess:
		code = "32"
	case styleWarning:
		code = "33"
	case styleError:
		code = "31"
	default:
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
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
