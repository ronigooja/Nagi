package output

import (
	"fmt"
	"github.com/ronigooja/Nagi/internal/profile"
	"io"
	"strings"
)

func writeProxyGroupConfig(w io.Writer, g profile.ProxyGroupConfig) error {
	if err := line(w, fmt.Sprintf("%s (%s)", g.Name, g.Type)); err != nil {
		return err
	}
	if err := line(w, text(w, "cli.output.group.nodes", "Nodes and groups")+": "+strings.Join(g.Proxies, ", ")); err != nil {
		return err
	}
	if err := line(w, text(w, "cli.output.group.providers", "Providers")+": "+strings.Join(g.Use, ", ")); err != nil {
		return err
	}
	if g.Type != "select" {
		if err := line(w, text(w, "cli.output.group.url", "Test URL")+": "+g.URL); err != nil {
			return err
		}
		if err := line(w, text(w, "cli.output.group.interval", "Check interval (seconds)")+": "+fmt.Sprint(g.Interval)); err != nil {
			return err
		}
	}
	if g.Type == "url-test" {
		return line(w, text(w, "cli.output.group.tolerance", "Tolerance (milliseconds)")+": "+fmt.Sprint(g.Tolerance))
	}
	if g.Type == "load-balance" {
		return line(w, text(w, "cli.output.group.strategy", "Balance strategy")+": "+g.Strategy)
	}
	return nil
}
