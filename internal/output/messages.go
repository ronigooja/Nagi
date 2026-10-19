package output

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// SafeMessage redacts both descriptor arguments and the English fallback before
// either text rendering or descriptor serialization can expose them.
func SafeMessage(message *i18n.Message) *i18n.Message {
	d := message.Descriptor()
	for key, value := range d.Args {
		if text, ok := value.(string); ok {
			d.Args[key] = RedactError(text)
		}
	}
	return i18n.NewMessage(d.ID, d.Args, RedactError(message.Error()), message.Unwrap())
}

// WriteMessage keeps the existing JSON value English, adding optional metadata.
func WriteMessage(w io.Writer, jsonMode bool, locale i18n.Context, message *i18n.Message) error {
	message = SafeMessage(message)
	if jsonMode {
		return json.NewEncoder(w).Encode(Envelope{OK: true, Data: message.Error(), Messages: map[string]i18n.Descriptor{"/data": message.Descriptor()}})
	}
	return line(w, locale.Render(message))
}

func WriteMessageError(w io.Writer, jsonMode bool, locale i18n.Context, code string, message *i18n.Message) {
	message = SafeMessage(message)
	if jsonMode {
		_ = json.NewEncoder(w).Encode(Envelope{OK: false, Error: &Failure{Code: code, Message: message.Error()}, Messages: map[string]i18n.Descriptor{"/error/message": message.Descriptor()}})
		return
	}
	WriteError(w, false, code, locale.Render(message))
}

// WriteLocalized recognizes typed messages and otherwise preserves existing output.
func WriteLocalized(w io.Writer, jsonMode bool, locale i18n.Context, data any) error {
	if message, ok := data.(*i18n.Message); ok {
		return WriteMessage(w, jsonMode, locale, message)
	}
	return Write(w, jsonMode, data)
}

// WriteErrorCause retains a typed descriptor across ordinary error wrappers.
func WriteErrorCause(w io.Writer, jsonMode bool, locale i18n.Context, code string, err error) {
	var message *i18n.Message
	if errors.As(err, &message) {
		WriteMessageError(w, jsonMode, locale, code, message)
		return
	}
	WriteError(w, jsonMode, code, err.Error())
}
