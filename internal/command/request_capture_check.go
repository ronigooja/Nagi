package command

import (
	"context"
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"

	"github.com/ronigooja/Nagi/internal/capture"
	nagiruntime "github.com/ronigooja/Nagi/internal/runtime"
)

// Each failed stage returns immediately: later prerequisites are not inspected.
// This observation never starts the recorder or changes runtime/settings/trust.
func captureReadiness(ctx context.Context, p nagiruntime.Paths) *capture.Readiness {
	blocked := func(code string, message *i18n.Message) *capture.Readiness {
		return &capture.Readiness{Code: code, Message: message.Error(), Messages: map[string]i18n.Descriptor{"/message": message.Descriptor()}}
	}
	if _, err := rewriteSelected(p); err != nil {
		return blocked("capture_profile_required", i18n.Errorf("backend.capture.readiness.select_a_profile_before_starting_capture", "Select a profile before starting capture."))
	}
	client, err := rewriteClient(p)
	if err != nil {
		return blocked("rewrite_unavailable", i18n.Errorf("backend.capture.readiness.cannot_reach_the_engine_start_nagi_and_check_engine_status_before_capt", "Cannot reach the engine. Start Nagi and check engine status before capturing."))
	}
	var version map[string]any
	if client.Get(ctx, "/version", &version) != nil {
		return blocked("rewrite_unavailable", i18n.Errorf("backend.capture.readiness.cannot_reach_the_engine_start_nagi_and_check_engine_status_before_capt", "Cannot reach the engine. Start Nagi and check engine status before capturing."))
	}
	if _, _, err = rewriteCapturePrerequisites(ctx, p, nil); err != nil {
		for _, issue := range []struct {
			code    string
			message *i18n.Message
		}{
			{"rewrite_auth", i18n.Errorf("backend.capture.readiness.capture_requires_nagi_to_run_as_your_ordinary_user_not_root", "Capture requires Nagi to run as your ordinary user, not root.")},
			{"rewrite_capability_missing", i18n.Errorf("backend.capture.readiness.the_engine_does_not_provide_the_required_rewrite_capability_a_matching", "The engine does not provide the required rewrite capability. A matching engine update is required.")},
			{"capture_capability_missing", i18n.Errorf("backend.capture.readiness.the_engine_does_not_support_request_capture_a_matching_engine_update_i", "The engine does not support request capture. A matching engine update is required.")},
			{"rewrite_unavailable", i18n.Errorf("backend.capture.readiness.cannot_confirm_engine_state_check_engine_status_before_capturing", "Cannot confirm engine state. Check engine status before capturing.")},
			{"rewrite_tun_required", i18n.Errorf("backend.capture.readiness.tun_is_not_running_enable_tun_before_starting_capture", "TUN is not running. Enable TUN before starting capture.")},
			{"rewrite_ca_required", i18n.Errorf("backend.capture.readiness.a_valid_capture_ca_is_missing_generate_the_rewrite_ca_before_starting_", "A valid capture CA is missing. Generate the rewrite CA before starting capture.")},
			{"rewrite_ca_trust_required", i18n.Errorf("backend.capture.readiness.the_capture_ca_is_not_confirmed_trusted_establish_certificate_trust_be", "The capture CA is not confirmed trusted. Establish certificate trust before starting capture.")},
		} {
			var reason *operationReason
			if errors.As(err, &reason) && reason.code == issue.code {
				return blocked(issue.code, issue.message)
			}
		}
		return blocked("capture_check_failed", i18n.Errorf("backend.capture.readiness.unable_to_check_capture_prerequisites_reopen_or_refresh_to_retry", "Unable to check capture prerequisites. Reopen or refresh to retry."))
	}
	ready := blocked("", i18n.Errorf("backend.capture.readiness.ready_to_capture", "Ready to capture."))
	ready.Ready = true
	return ready
}
