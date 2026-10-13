package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/ronigooja/Nagi/internal/rules"
)

func writeRuleTest(w io.Writer, result rules.TestResult) error {
	if result.Matched {
		if err := line(w, fmt.Sprintf("Matched: %s %s -> %s", result.Rule, result.RulePayload, result.Outbound)); err != nil {
			return err
		}
	} else if err := line(w, "No rule matched; fallback: "+result.Outbound); err != nil {
		return err
	}
	if err := line(w, fmt.Sprintf("Destination: %s (%s port %d)", result.Target, result.Network, result.Port)); err != nil {
		return err
	}
	if result.DestinationIP != "" {
		if err := line(w, "Destination IP: "+result.DestinationIP); err != nil {
			return err
		}
	}
	if len(result.Providers) > 0 {
		if err := line(w, "Rule providers: "+strings.Join(result.Providers, ", ")); err != nil {
			return err
		}
	}
	if result.Process != "" {
		if err := line(w, "Process: "+result.Process); err != nil {
			return err
		}
	}
	if err := stateLine(w, "Current mode", result.Mode); err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		if err := line(w, "Note: "+warning); err != nil {
			return err
		}
	}
	return nil
}
