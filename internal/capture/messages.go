package capture

import "github.com/ronigooja/Nagi/internal/i18n"

func (s Status) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(s.Messages)
}
func (r Readiness) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(r.Messages)
}
