package proxy

import "github.com/ronigooja/Nagi/internal/i18n"

func (i BatchDelayItem) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(i.Messages)
}

func (d NodeDelay) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(d.Messages)
}
