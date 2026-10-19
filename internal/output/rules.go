package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/ronigooja/Nagi/internal/rules"
)

func writeRuleTest(w io.Writer, result rules.TestResult) error {
	if result.Matched {
		if err := line(w, renderText(w, "cli.output.writeruletest.matched_s_s_s", map[string]any{"rule": fmt.Sprint(result.Rule), "payload": fmt.Sprint(result.RulePayload), "outbound": fmt.Sprint(result.Outbound)}, fmt.Sprintf("Matched: %s %s -> %s", result.Rule, result.RulePayload, result.Outbound))); err != nil {
			return err
		}
	} else if err := line(w, renderText(w, "cli.output.writeruletest.no_rule_matched_fallback.message", map[string]any{"outbound": fmt.Sprint(result.Outbound)}, "No rule matched; fallback: "+fmt.Sprint(result.Outbound))); err != nil {
		return err
	}
	if err := line(w, renderText(w, "cli.output.writeruletest.destination_s_s_port_d", map[string]any{"target": fmt.Sprint(result.Target), "network": fmt.Sprint(result.Network), "port": result.Port}, fmt.Sprintf("Destination: %s (%s port %d)", result.Target, result.Network, result.Port))); err != nil {
		return err
	}
	if result.DestinationIP != "" {
		if err := line(w, renderText(w, "cli.output.writeruletest.destination_ip.message", map[string]any{"ip": fmt.Sprint(result.DestinationIP)}, "Destination IP: "+fmt.Sprint(result.DestinationIP))); err != nil {
			return err
		}
	}
	if len(result.Providers) > 0 {
		if err := line(w, renderText(w, "cli.output.writeruletest.rule_providers.message", map[string]any{"providers": fmt.Sprint(strings.Join(result.Providers, ", "))}, "Rule providers: "+fmt.Sprint(strings.Join(result.Providers, ", ")))); err != nil {
			return err
		}
	}
	if result.Process != "" {
		if err := line(w, renderText(w, "cli.output.writeruletest.process.message", map[string]any{"process": fmt.Sprint(result.Process)}, "Process: "+fmt.Sprint(result.Process))); err != nil {
			return err
		}
	}
	if err := stateLine(w, text(w, "cli.output.writeruletest.current_mode", "Current mode"), result.Mode); err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		if err := line(w, renderText(w, "cli.output.writeruletest.note.message", map[string]any{"detail": fmt.Sprint(warning)}, "Note: "+fmt.Sprint(warning))); err != nil {
			return err
		}
	}
	return nil
}
