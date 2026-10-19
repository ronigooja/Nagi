package installtx

import (
	"errors"
	"strings"
	"testing"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func TestTransactionOutcomePreservesCauseAndMessage(t *testing.T) {
	cause := errors.New("external storage failure")
	for _, err := range []error{&CommitUncertainError{Err: cause}, &CommittedError{Err: cause}} {
		if !errors.Is(err, cause) {
			t.Fatalf("lost cause: %v", err)
		}
		var message *i18n.Message
		if !errors.As(err, &message) || message.Error() != err.Error() {
			t.Fatalf("lost full context: %v", err)
		}
		got := i18n.New(i18n.SimplifiedChinese).Render(message)
		if got == err.Error() || !strings.Contains(got, cause.Error()) {
			t.Fatalf("localized=%q original=%q", got, err)
		}
	}
}
