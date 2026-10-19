package command

import "github.com/ronigooja/Nagi/internal/i18n"

// operationReason preserves a stable domain reason without parsing diagnostic text.
type operationReason struct {
	code  string
	cause error
}

func (e *operationReason) Error() string { return e.cause.Error() }
func (e *operationReason) Unwrap() error { return e.cause }
func (s RewriteStatus) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(s.Messages)
}
