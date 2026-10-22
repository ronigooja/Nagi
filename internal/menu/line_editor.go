package menu

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"github.com/ronigooja/Nagi/internal/terminal"
	"golang.org/x/sys/unix"
)

// Terminal input and rendering stay in the session's reader goroutine. Editing
// uses grapheme clusters, since an emoji may contain several Unicode code points.
func (s *Session) interruptibleRead(prompt string) (string, error) {
	select {
	case <-s.interrupts:
		fmt.Fprintln(s.out)
		return "", ErrBack
	default:
	}
	if s.file == nil || !IsTerminal(s.file) {
		return s.in.ReadString('\n')
	}
	if s.interrupts == nil {
		s.interrupts = make(chan os.Signal, 8)
		signal.Notify(s.interrupts, os.Interrupt)
		defer func() { signal.Stop(s.interrupts); s.interrupts = nil }()
	}
	restore, err := terminal.Raw(s.file)
	if err != nil {
		return "", err
	}
	defer restore()
	var value string
	var pending []byte
	escape := 0
	redraw := func() {
		columns := 80
		if size, err := unix.IoctlGetWinsize(int(s.file.Fd()), unix.TIOCGWINSZ); err == nil && size.Col > 1 {
			columns = int(size.Col)
		}
		// Keep one column free to avoid automatic wrapping. Long values scroll
		// horizontally while their full contents remain in the input buffer.
		fmt.Fprint(s.out, "\r\x1b[2K", inputViewport(prompt, value, columns-1))
	}
	redraw()
	for {
		select {
		case <-s.interrupts:
			fmt.Fprintln(s.out)
			return "", ErrBack
		default:
		}
		if s.in.Buffered() == 0 {
			n, errno := terminalReady(s.file.Fd())
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				return "", errno
			}
			if n == 0 {
				continue
			}
		}
		b, err := s.in.ReadByte()
		if err != nil {
			return value, err
		}
		// Ignore terminal key sequences instead of inserting their printable
		// suffixes into names. This editor supports editing at the end only.
		if escape != 0 {
			if escape == 1 && (b == '[' || b == 'O') {
				escape = 2
			} else if escape == 1 || b >= 0x40 && b <= 0x7e {
				escape = 0
			}
			continue
		}
		switch b {
		case 27:
			escape = 1
			pending = nil
		case '\n', '\r':
			fmt.Fprintln(s.out)
			return value, nil
		case 3:
			fmt.Fprintln(s.out)
			return "", ErrBack
		case 4:
			fmt.Fprintln(s.out)
			return value, io.EOF
		case 8, 127:
			pending = nil
			value = eraseGrapheme(value)
			redraw()
		case 21: // Ctrl-U clears the current input.
			pending, value = nil, ""
			redraw()
		case 23: // Ctrl-W erases the previous whitespace-delimited word.
			pending = nil
			value = strings.TrimRightFunc(value, unicode.IsSpace)
			for value != "" {
				r, _ := utf8.DecodeLastRuneInString(value)
				if unicode.IsSpace(r) {
					break
				}
				value = eraseGrapheme(value)
			}
			redraw()
		default:
			if b < 32 {
				continue
			}
			pending = append(pending, b)
			for utf8.FullRune(pending) {
				r, size := utf8.DecodeRune(pending)
				pending = pending[size:]
				if (r != utf8.RuneError || size > 1) && !unicode.IsControl(r) {
					value += string(r)
					redraw()
				}
			}
		}
	}
}

func eraseGrapheme(value string) string {
	start := 0
	g := uniseg.NewGraphemes(value)
	for g.Next() {
		start, _ = g.Positions()
	}
	return value[:start]
}

func inputViewport(prompt, value string, columns int) string {
	// Leave room for input even when a translated prompt exceeds the terminal.
	limit := columns / 2
	var label strings.Builder
	g := uniseg.NewGraphemes(prompt)
	width := 0
	if uniseg.StringWidth(prompt) < columns-2 {
		limit = columns - 2
	}
	for g.Next() && width+g.Width() <= limit {
		label.WriteString(g.Str())
		width += g.Width()
	}
	available := columns - width
	g = uniseg.NewGraphemes(value)
	remaining := uniseg.StringWidth(value)
	start := 0
	for remaining > available && g.Next() {
		_, start = g.Positions()
		remaining -= g.Width()
	}
	return label.String() + value[start:]
}
