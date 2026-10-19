package profile

import "github.com/ronigooja/Nagi/internal/i18n"

func (h Healthcheck) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(h.Messages)
}
