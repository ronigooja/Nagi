package i18n

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MessageSource associates descriptors with existing string fields. Pointers are
// relative to the producing object; metadata never becomes operation data.
type MessageSource interface{ MessageDescriptors() map[string]Descriptor }

// Result attaches presentation metadata to map results without changing their
// JSON representation. Human renderers unwrap Value before ordinary dispatch.
type Result struct {
	Value    any
	Messages map[string]Descriptor
}

func (r Result) MarshalJSON() ([]byte, error)              { return json.Marshal(r.Value) }
func (r Result) MessageDescriptors() map[string]Descriptor { return CopyDescriptors(r.Messages) }
func CopyDescriptors(messages map[string]Descriptor) map[string]Descriptor {
	copy := make(map[string]Descriptor, len(messages))
	for path, descriptor := range messages {
		args := make(map[string]any, len(descriptor.Args))
		for key, value := range descriptor.Args {
			args[key] = value
		}
		copy[path] = Descriptor{ID: descriptor.ID, Args: args}
	}
	return copy
}

var errorFormat = regexp.MustCompile(`%[-+#0 .0-9]*[a-zA-Z%]`)
var diagnosticURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)

// Errorf constructs a descriptor at an error origin, preserving fmt's English
// formatting and complete wrapping graph. Descriptor arguments are sanitized
// formatted scalars, never arbitrary errors or configuration objects.
func Errorf(id, format string, values ...any) *Message {
	original := fmt.Errorf(format, values...)
	args := map[string]any{}
	index := 0
	for _, conversion := range errorFormat.FindAllString(format, -1) {
		if conversion == "%%" {
			continue
		}
		if index >= len(values) {
			break
		}
		if strings.HasSuffix(conversion, "w") {
			conversion = strings.TrimSuffix(conversion, "w") + "v"
		}
		args[fmt.Sprintf("arg%d", index)] = diagnosticURL.ReplaceAllString(fmt.Sprintf(conversion, values[index]), "[redacted URL]")
		index++
	}
	return NewMessage(id, args, original.Error(), original)
}

// ValidDescriptor checks the exact catalog signature without rendering or
// matching any English text. Unknown or malformed optional metadata is ignored.
func ValidDescriptor(d Descriptor) bool {
	schema, ok := argumentSchemas[d.ID]
	if !ok || len(schema) != len(d.Args) {
		return false
	}
	for name, value := range d.Args {
		kind, exists := schema[name]
		if !exists || !matchesType(value, kind) {
			return false
		}
	}
	return true
}
