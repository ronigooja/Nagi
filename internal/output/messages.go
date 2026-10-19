package output

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// SafeMessage redacts both descriptor arguments and the English fallback before
// either text rendering or descriptor serialization can expose them.
func SafeMessage(message *i18n.Message) *i18n.Message {
	d := i18n.CopyDescriptor(message.Descriptor(), RedactError)
	return i18n.MessageFromDescriptor(d, RedactError(message.Error()), message.Unwrap())
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
	WriteError(WithLanguage(w, locale), false, code, locale.Render(message))
}

// WriteLocalized recognizes typed messages and otherwise preserves existing output.
func WriteLocalized(w io.Writer, jsonMode bool, locale i18n.Context, data any) error {
	if message, ok := data.(*i18n.Message); ok {
		return WriteMessage(w, jsonMode, locale, message)
	}
	return Write(WithLanguage(w, locale), jsonMode, data)
}

// WriteErrorCause retains a typed descriptor across ordinary error wrappers.
func WriteErrorCause(w io.Writer, jsonMode bool, locale i18n.Context, code string, err error) {
	var message *i18n.Message
	if errors.As(err, &message) && err.Error() == message.Error() {
		WriteMessageError(w, jsonMode, locale, code, message)
		return
	}
	// A wrapper or joined error owns additional context. Never replace the JSON
	// message with a nested error's shorter fallback or attach its misleading ID.
	if jsonMode {
		WriteError(w, true, code, err.Error())
		return
	}
	WriteError(WithLanguage(w, locale), false, code, renderError(locale, err))
}

func renderError(locale i18n.Context, err error) string {
	if message, ok := err.(*i18n.Message); ok {
		return locale.Render(SafeMessage(message))
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]string, 0, len(joined.Unwrap()))
		for _, child := range joined.Unwrap() {
			parts = append(parts, renderError(locale, child))
		}
		return strings.Join(parts, "\n")
	}
	// Transparent command wrappers retain the complete child identity.
	if child := errors.Unwrap(err); child != nil && err.Error() == child.Error() {
		return renderError(locale, child)
	}
	return RedactError(err.Error())
}

// localizedWriter carries one invocation's rendering context through the existing
// formatting helpers. It never inspects or changes bytes written through it.
type localizedWriter struct {
	io.Writer
	locale i18n.Context
}

func WithLanguage(w io.Writer, locale i18n.Context) io.Writer {
	return localizedWriter{Writer: w, locale: locale}
}

func writerLanguage(w io.Writer) i18n.Context {
	if annotated, ok := w.(messageWriter); ok {
		return writerLanguage(annotated.Writer)
	}
	if localized, ok := w.(localizedWriter); ok {
		return localized.locale
	}
	return i18n.New(i18n.English)
}

func text(w io.Writer, id, fallback string) string {
	return writerLanguage(w).RenderDescriptor(i18n.Descriptor{ID: id}, fallback)
}

func underlyingWriter(w io.Writer) io.Writer {
	for {
		if annotated, ok := w.(messageWriter); ok {
			w = annotated.Writer
			continue
		}
		localized, ok := w.(localizedWriter)
		if !ok {
			return w
		}
		w = localized.Writer
	}
}

// stateText receives a protocol state only; callers must never pass resource names.
func stateText(w io.Writer, state string) string {
	if writerLanguage(w).Language() == i18n.English {
		return state
	}
	switch strings.ToLower(state) {
	case "unexpected exit", "running", "stopped", "unknown", "unavailable", "available", "removed", "enabled", "disabled", "active", "inactive", "automatic", "fixed", "paused", "managed", "unmanaged", "unsupported", "attention", "partial", "warning", "failed", "failure", "error", "conflict", "healthy", "unhealthy", "ok", "none", "yes", "no", "true", "false", "rule", "rules", "global", "direct", "proxy", "mixed", "user", "system", "privileged", "doh", "stale":
		return text(w, "cli.state."+strings.ReplaceAll(strings.ToLower(state), " ", "_"), state)
	case "provider", "group", "node", "subscription", "profile", "override", "bind", "unbind", "prepared", "starting", "uncertain", "blocked", "absent", "idle", "generated", "missing", "invalid", "trusted", "untrusted", "expired", "request", "response", "websocket_client", "websocket_server", "fragment", "full", "block", "forward_original":
		return text(w, "cli.display."+strings.ToLower(state), state)
	default:
		return state
	}
}

func renderText(w io.Writer, id string, args map[string]any, english string) string {
	return writerLanguage(w).RenderDescriptor(i18n.Descriptor{ID: id, Args: args}, english)
}

// messageWriter retains source associations only for explicitly rendered fields.
// Raw bytes continue directly to the original writer.
type messageWriter struct {
	io.Writer
	source any
}

func fieldText(w io.Writer, pointer, fallback string) string {
	if annotated, ok := w.(messageWriter); ok {
		return RenderMessageField(annotated.source, pointer, fallback, writerLanguage(w))
	}
	return fallback
}
