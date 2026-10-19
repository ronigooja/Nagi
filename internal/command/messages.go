package command

import (
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
)

// messageError preserves source composition and the original English contract.
func messageError(id, format string, values ...any) *i18n.Message {
	message := i18n.Errorf(id, format, values...)
	return i18n.MessageFromDescriptor(i18n.CopyDescriptor(message.Descriptor(), output.RedactError), message.Error(), message.Unwrap())
}
