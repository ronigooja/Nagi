package menu

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestInputViewportKeepsGraphemesWithinTerminal(t *testing.T) {
	for _, value := range []string{"🇨🇳国", strings.Repeat("国", 100), strings.Repeat("👨‍👩‍👧‍👦", 100), "é"} {
		for _, columns := range []int{1, 2, 10, 40, 79} {
			visible := inputViewport("Group name (Ctrl-C cancels): ", value, columns)
			if width := uniseg.StringWidth(visible); width > columns {
				t.Fatalf("width %d > %d: %q", width, columns, visible)
			}
		}
	}
	if got := inputViewport("Name: ", "🇨🇳国", 10); got != "Name: 🇨🇳国" {
		t.Fatalf("visible = %q", got)
	}
	if got := inputViewport("Name: ", "🇨🇳国", 9); got != "Name: 国" {
		t.Fatalf("clipped visible = %q", got)
	}
}
