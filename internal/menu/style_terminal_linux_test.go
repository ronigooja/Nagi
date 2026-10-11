package menu

import (
	"os"
	"strings"
	"testing"
)

func TestMenuStyleAutomaticallyEnabledOnlyForCapableTerminal(t *testing.T) {
	terminal, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("PTY unavailable: %v", err)
	}
	defer terminal.Close()
	if !IsTerminal(terminal) {
		t.Fatal("PTY is not recognized as terminal")
	}
	for _, tc := range []struct {
		name, term, noColor string
		want                bool
	}{
		{"capable terminal", "xterm-256color", "", true},
		{"NO_COLOR", "xterm-256color", "1", false},
		{"NO_COLOR arbitrary value", "xterm", "false", false},
		{"dumb terminal", "dumb", "", false},
		{"empty TERM", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM", tc.term)
			t.Setenv("NO_COLOR", tc.noColor)
			s := NewSession(&fakeRunner{}, strings.NewReader(""), terminal)
			if s.styles != tc.want {
				t.Errorf("styles=%v, want %v", s.styles, tc.want)
			}
		})
	}
}
