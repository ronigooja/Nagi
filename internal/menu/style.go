package menu

import (
	"io"

	"github.com/ronigooja/Nagi/internal/output"
)

// textStyle keeps terminal presentation local to the menu. Every styled span
// resets immediately so user input, raw command output, and streams stay plain.
type textStyle uint8

const (
	styleTitle textStyle = iota
	styleRootTitle
	styleSelected
	styleChoice
	styleImpact
	styleContext
	styleSuccess
	styleWarning
	styleError
	stylePrompt
)

func shouldStyle(out io.Writer) bool {
	return output.StylesEnabled(out)
}

func (s *Session) styled(style textStyle, text string) string {
	if !s.styles || text == "" {
		return text
	}
	code := ""
	switch style {
	case styleTitle:
		code = "33"
	case styleRootTitle:
		code = "1;96"
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
