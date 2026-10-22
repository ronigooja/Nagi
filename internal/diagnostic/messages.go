package diagnostic

import (
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/sensitive"
	"strings"
)

func (c Check) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(c.Messages)
}
func newCheck(name, status string, message *i18n.Message) Check {
	descriptor := i18n.CopyDescriptor(message.Descriptor(), sensitive.RedactURLDetails)
	c := Check{Name: name, Status: status, Message: sensitive.RedactURLDetails(message.Error()), Messages: map[string]i18n.Descriptor{"/message": descriptor}}
	switch name {
	case "config_directory":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.config_directory"}
	case "data_directory":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.data_directory"}
	case "state_directory":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.state_directory"}
	case "runtime_directory":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.runtime_directory"}
	case "mihomo_executable":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.mihomo_executable"}
	case "selected_profile":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.selected_profile"}
	case "process":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.process"}
	case "control_api":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.control_api"}
	case "config_permissions":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.config_permissions"}
	case "runtime_permissions":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.runtime_permissions"}
	case "socket_permissions":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.socket_permissions"}
	case "configuration":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.configuration"}
	case "dns":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.dns"}
	case "listener_exposure":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.listener_exposure"}
	case "controller_exposure":
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.controller_exposure"}
	}
	if strings.HasPrefix(name, "port_") {
		c.Messages["/name"] = i18n.Descriptor{ID: "backend.diagnostic.name.listener_port", Args: map[string]any{"field": strings.TrimPrefix(name, "port_")}}
	}
	return c
}
