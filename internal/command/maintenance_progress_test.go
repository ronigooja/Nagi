package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestMaintenanceProgressInlineAndPlain(t *testing.T) {
	for _, inline := range []bool{false, true} {
		var out bytes.Buffer
		p := &maintenanceProgress{writer: &out, locale: i18n.New(i18n.English), inline: inline}
		stage := i18n.Errorf("system.installation.progress.archive", "Downloading %s...", "nagi-linux-amd64.tar.gz")
		transfer := func(speed string) *i18n.Message {
			return i18n.Errorf("system.installation.progress.transfer.total", "Downloading %s: %s / %s (%s%%), %s",
				"nagi-linux-amd64.tar.gz", "1.00 MiB", "46.49 MiB", "2.2", speed)
		}
		p.Show(stage)
		p.Show(transfer("1024.0 KiB/s"))
		p.Show(transfer("0.0 KiB/s"))
		if inline {
			if strings.Count(out.String(), "\n") != 1 || strings.Count(out.String(), "\r") != 2 {
				t.Fatalf("transfer did not refresh in place: %q", out.String())
			}
			if !strings.HasSuffix(out.String(), "0.0 KiB/s   ") {
				t.Fatalf("shorter speed did not erase old characters: %q", out.String())
			}
		} else if strings.Contains(out.String(), "\r") || strings.Count(out.String(), "\n") != 3 {
			t.Fatalf("redirected progress is not plain lines: %q", out.String())
		}
		p.Show(i18n.Errorf("system.installation.progress.checksum", "Verifying archive checksum..."))
		p.Finish()
		if !strings.HasSuffix(out.String(), "\nVerifying archive checksum...\n") {
			t.Fatalf("next stage did not start on its own line: %q", out.String())
		}
	}
}

func TestMaintenanceProgressFinishesBeforeFailure(t *testing.T) {
	var out bytes.Buffer
	p := &maintenanceProgress{writer: &out, locale: i18n.New(i18n.English), inline: true}
	p.Show(i18n.Errorf("system.installation.progress.transfer", "Downloading %s: %s, %s", "nagi-linux-amd64.tar.gz", "1.00 MiB", "0.0 KiB/s"))
	p.Finish()
	p.Finish()
	if got := out.String(); got != "\r1.00 MiB, 0.0 KiB/s\n" {
		t.Fatalf("unfinished or duplicate line: %q", got)
	}
}
