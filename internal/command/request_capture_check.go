package command

import (
	"context"
	"strings"

	"github.com/ronigooja/Nagi/internal/capture"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// Each failed stage returns immediately: later prerequisites are not inspected.
// This observation never starts the recorder or changes runtime/settings/trust.
func captureReadiness(ctx context.Context, p nagiruntime.Paths) *capture.Readiness {
	blocked := func(code, message string) *capture.Readiness {
		return &capture.Readiness{Code: code, Message: message}
	}
	if _, err := rewriteSelected(p); err != nil {
		return blocked("capture_profile_required", "Select a profile before starting capture.")
	}
	client, err := rewriteClient(p)
	if err != nil {
		return blocked("rewrite_unavailable", "Cannot reach the engine. Start Nagi and check engine status before capturing.")
	}
	var version map[string]any
	if client.Get(ctx, "/version", &version) != nil {
		return blocked("rewrite_unavailable", "Cannot reach the engine. Start Nagi and check engine status before capturing.")
	}
	if _, _, err = rewriteCapturePrerequisites(ctx, p, nil); err != nil {
		for _, issue := range []struct{ code, message string }{
			{"rewrite_auth", "Capture requires Nagi to run as your ordinary user, not root."},
			{"rewrite_capability_missing", "The engine does not provide the required rewrite capability. A matching engine update is required."},
			{"capture_capability_missing", "The engine does not support request capture. A matching engine update is required."},
			{"rewrite_unavailable", "Cannot confirm engine state. Check engine status before capturing."},
			{"rewrite_tun_required", "TUN is not running. Enable TUN before starting capture."},
			{"rewrite_ca_required", "A valid capture CA is missing. Generate the rewrite CA before starting capture."},
			{"rewrite_ca_trust_required", "The capture CA is not confirmed trusted. Establish certificate trust before starting capture."},
		} {
			if strings.Contains(err.Error(), issue.code) {
				return blocked(issue.code, issue.message)
			}
		}
		return blocked("capture_check_failed", "Unable to check capture prerequisites. Reopen or refresh to retry.")
	}
	return &capture.Readiness{Ready: true, Message: "Ready to capture."}
}
