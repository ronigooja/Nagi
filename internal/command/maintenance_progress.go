package command

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/terminal"
)

// Transfer callbacks are joined before other stages or operation completion.
// Keep terminal control here; redirected output remains line-oriented.
type maintenanceProgress struct {
	writer io.Writer
	locale i18n.Context
	inline bool
	width  int
}

func newMaintenanceProgress(writer io.Writer, locale i18n.Context) *maintenanceProgress {
	file, ok := writer.(*os.File)
	term := os.Getenv("TERM")
	return &maintenanceProgress{writer: writer, locale: locale,
		inline: ok && terminal.IsTerminal(file) && term != "" && term != "dumb"}
}

func (p *maintenanceProgress) Show(message *i18n.Message) {
	descriptor := message.Descriptor()
	transfer := descriptor.ID == "system.installation.progress.transfer" || descriptor.ID == "system.installation.progress.transfer.total"
	if !p.inline || !transfer {
		p.Finish()
		fmt.Fprintln(p.writer, p.locale.Render(message))
		return
	}
	// The preceding stage names the archive. Keep the changing line compact
	// enough for ordinary terminal widths, using the source's formatted units.
	args := descriptor.Args
	text := fmt.Sprintf("%v, %v", args["arg1"], args["arg2"])
	if descriptor.ID == "system.installation.progress.transfer.total" {
		text = fmt.Sprintf("%v / %v (%v%%), %v", args["arg1"], args["arg2"], args["arg3"], args["arg4"])
	}
	padding := ""
	if p.width > len(text) {
		padding = strings.Repeat(" ", p.width-len(text))
	}
	fmt.Fprint(p.writer, "\r", text, padding)
	p.width = len(text)
}

func (p *maintenanceProgress) Finish() {
	if p.width > 0 {
		fmt.Fprintln(p.writer)
		p.width = 0
	}
}
