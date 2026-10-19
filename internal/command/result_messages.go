package command

import (
	"errors"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
)

// rememberError preserves the source descriptor before a result stores English
// text. A nontransparent wrapper owns different prose and needs its own ID.
func rememberError(messages map[string]i18n.Descriptor, pointer string, err error) {
	var message *i18n.Message
	if err != nil && errors.As(err, &message) && message.Error() == err.Error() {
		messages[pointer] = output.SafeMessage(message).Descriptor()
	}
}
func resultWithMessages(value any, messages map[string]i18n.Descriptor) any {
	if len(messages) == 0 {
		return value
	}
	return i18n.Result{Value: value, Messages: messages}
}
