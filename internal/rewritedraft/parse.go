package rewritedraft

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"

	"github.com/ronigooja/Nagi/internal/requestrewrite"
)

// strict rejects duplicate keys at every depth, invalid UTF-8, excessive nesting,
// trailing values and unknown fields. It retains numeric text in RawMessage.
func strict(data []byte, out any) (map[string]json.RawMessage, error) {
	if len(data) == 0 || len(data) > requestrewrite.MaxPreviewBytes+requestrewrite.MaxDocumentBytes || !utf8.Valid(data) {
		return nil, invalid()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 256 {
			return invalid()
		}
		token, e := d.Token()
		if e != nil {
			return invalid()
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return invalid()
					}
					k, ok := key.(string)
					if !ok || seen[k] {
						return invalid()
					}
					seen[k] = true
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return invalid()
				}
			case '[':
				for d.More() {
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return invalid()
				}
			default:
				return invalid()
			}
		}
		return nil
	}
	if e := walk(0); e != nil {
		return nil, e
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return nil, invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, invalid()
	}
	return fields, nil
}
func required(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	return true
}

// ParseDraft checks the private wire format. Apply performs semantic validation
// against its exact base, including existing scripts and duplicate rule IDs.
func ParseDraft(data []byte) (Draft, error) {
	var d Draft
	fields, e := strict(data, &d)
	if e != nil {
		return d, e
	}
	if !required(fields, "base_rule_version", "position", "rule", "enable") || d.BaseRuleVersion == "" || d.Position < 0 {
		return Draft{}, invalid()
	}
	if e = requestrewrite.ValidateRuleJSON(fields["rule"]); e != nil {
		return Draft{}, e
	}
	if raw, ok := fields["fixture"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Draft{}, invalid()
		}
		if _, e = validateFixture(raw); e != nil {
			return Draft{}, e
		}
	}
	return d, nil
}

// ParseSelection checks the capture draft selection wire format. Values remain
// private and are accepted unchanged; contextual validation happens in FromRecord.
func ParseSelection(data []byte) (Selection, error) {
	var s Selection
	fields, e := strict(data, &s)
	if e != nil {
		return s, e
	}
	if !required(fields, "stage", "id", "operations") {
		return Selection{}, invalid()
	}
	for _, name := range []string{"match", "position", "enable", "on_error", "range_mode", "redirect_consent"} {
		if raw, ok := fields[name]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Selection{}, invalid()
		}
	}
	// Validate operation field shapes using the authoritative rule parser, even
	// when semantic validation must wait for the profile's scripts.
	match := json.RawMessage(`{"domain":"draft.service.example"}`)
	if raw, ok := fields["match"]; ok {
		match = raw
	}
	rule := map[string]any{"id": s.ID, "enabled": false, "stage": s.Stage, "match": match, "on_error": "block", "redirect_consent": []any{}, "operations": fields["operations"]}
	b, e := json.Marshal(rule)
	if e != nil {
		return Selection{}, invalid()
	}
	if e = requestrewrite.ValidateRuleJSON(b); e != nil {
		return Selection{}, e
	}
	return s, nil
}
