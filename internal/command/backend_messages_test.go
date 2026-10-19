package command

import (
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"testing"
)

func TestScheduledDNSClassificationUsesSourceIdentity(t *testing.T) {
	source := messageError("cli.error.dns.dns_proxy_target_is_missing_or_no_longer_usable_update_nagi_dns_set_", "DNS proxy target is missing or no longer usable; update `nagi dns set proxy NAME URL` before activating this profile")
	if got := classifyScheduledApply(source); !errors.Is(got, errDNSConflict) {
		t.Fatal(got)
	}
	external := errors.New("external diagnostic mentions DNS proxy target is missing")
	if got := classifyScheduledApply(external); got != external {
		t.Fatal("external prose changed scheduling reason")
	}
}
func TestPrerequisiteFailureCarriesStableReason(t *testing.T) {
	for _, failure := range []struct {
		err  error
		code string
	}{{rewriteFailure("rewrite_auth"), "rewrite_auth"}, {captureFailure("capture_capability_missing"), "capture_capability_missing"}} {
		var reason *operationReason
		if !errors.As(failure.err, &reason) || reason.code != failure.code {
			t.Fatal(failure.err)
		}
		var message *i18n.Message
		if !errors.As(failure.err, &message) || !i18n.ValidDescriptor(message.Descriptor()) {
			t.Fatal(failure.err)
		}
	}
}
