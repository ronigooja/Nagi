package subscription

import "github.com/ronigooja/Nagi/internal/i18n"

func (c ConfigChange) MessageDescriptors() map[string]i18n.Descriptor {
	return i18n.CopyDescriptors(c.Messages)
}
func (e Entry) MessageDescriptors() map[string]i18n.Descriptor {
	messages := map[string]i18n.Descriptor{}
	for field, reason := range map[string]string{"/pause_reason": e.PauseReason, "/last_failure_reason": e.LastFailureReason} {
		switch reason {
		case "network_error":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.network_error"}
		case "invalid_content":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.invalid_content"}
		case "identity_unavailable":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.identity_unavailable"}
		case "activation_failed":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.activation_failed"}
		case "engine_unavailable":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.engine_unavailable"}
		case "profile_changed":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.profile_changed"}
		case "schedule_off":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.schedule_off"}
		case "source_unreadable":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.source_unreadable"}
		case "dns_target_missing":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.dns_target_missing"}
		case "custom_rule_conflict":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.custom_rule_conflict"}
		case "selection_conflict":
			messages[field] = i18n.Descriptor{ID: "backend.subscription.reason.selection_conflict"}
		}
	}
	return messages
}
