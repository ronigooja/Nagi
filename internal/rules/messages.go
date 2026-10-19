package rules

import "github.com/ronigooja/Nagi/internal/i18n"

func (c Conflict) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(c.Messages)
}
