package proxy

import "github.com/ronigooja/Nagi/internal/i18n"

func (i BatchDelayItem) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(i.Messages)
}
