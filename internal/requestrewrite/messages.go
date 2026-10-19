package requestrewrite

import "github.com/ronigooja/Nagi/internal/i18n"

// Unwrap exposes presentation metadata for a typed domain event. The public
// code, RuleID and original Error() remain independent of presentation.
func (e *Error) Unwrap() error {
	var id string
	switch e.Code {
	case "rewrite_invalid":
		id = "system.rewrite.invalid"
	case "rewrite_limit":
		id = "system.rewrite.limit"
	case "rewrite_cancelled":
		id = "system.rewrite.cancelled"
	case "rewrite_ca_exists":
		id = "system.rewrite.ca.exists"
	case "rewrite_ca_invalid":
		id = "system.rewrite.ca.invalid"
	case "rewrite_ca_required":
		id = "system.rewrite.ca.required"
	case "rewrite_storage":
		id = "system.rewrite.storage"
	case "rewrite_script_referenced":
		id = "system.rewrite.script.referenced"
	case "rewrite_auth":
		id = "system.rewrite.auth"
	case "rewrite_range_conflict":
		id = "system.rewrite.range.conflict"
	case "rewrite_transform":
		id = "system.rewrite.transform"
	case "rewrite_script_block":
		id = "system.rewrite.script.block"
	case "rewrite_script_execution":
		id = "system.rewrite.script.execution"
	case "rewrite_script_output":
		id = "system.rewrite.script.output"
	case "rewrite_script_timeout":
		id = "system.rewrite.script.timeout"
	case "rewrite_activation_failed":
		id = "system.rewrite.activation.failed"
	case "rewrite_activation_required":
		id = "system.rewrite.activation.required"
	case "rewrite_conflict":
		id = "system.rewrite.conflict"
	case "rewrite_state_uncertain":
		id = "system.rewrite.state.uncertain"
	default:
		return nil
	}
	if e.RuleID != "" {
		safe := i18n.Errorf(id, "%s", e.RuleID).Descriptor().Args["arg0"]
		return i18n.NewMessage(id+".rule", map[string]any{"rule": safe}, e.Error(), nil)
	}
	return i18n.NewMessage(id, nil, e.Error(), nil)
}
