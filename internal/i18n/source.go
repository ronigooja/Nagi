package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
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
		copy[path] = CopyDescriptor(descriptor, nil)
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
	message := NewMessage(id, args, original.Error(), original)
	for index, value := range values {
		if err, ok := value.(error); ok {
			var child *Message
			if errors.As(err, &child) && child != nil && err.Error() == child.Error() {
				if message.descriptor.ArgumentMessages == nil {
					message.descriptor.ArgumentMessages = map[string]Descriptor{}
				}
				message.descriptor.ArgumentMessages[fmt.Sprintf("arg%d", index)] = child.Descriptor()
			}
		}
	}
	return message
}

// ValidDescriptor checks the exact catalog signature without rendering or
// matching any English text. Unknown or malformed optional metadata is ignored.
func ValidDescriptor(d Descriptor) bool {
	_, schemas := messageCatalog(d.ID)
	schema, ok := schemas[d.ID]
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

// CopyDescriptor bounds optional composition and independently discards invalid
// child descriptors. The original scalar args remain the compatibility fallback.
func CopyDescriptor(d Descriptor, redact func(string) string) Descriptor {
	nodes := 64
	var copy func(Descriptor, int) Descriptor
	copy = func(source Descriptor, depth int) Descriptor {
		result := Descriptor{ID: source.ID, Args: map[string]any{}}
		for key, value := range source.Args {
			if text, ok := value.(string); ok && redact != nil {
				value = redact(text)
			}
			result.Args[key] = value
		}
		if depth >= 8 {
			return result
		}
		for _, key := range descriptorKeys(source.ArgumentMessages) {
			child := source.ArgumentMessages[key]
			if nodes <= 0 {
				break
			}
			nodes--
			if _, ok := source.Args[key].(string); !ok || !ValidDescriptor(child) {
				continue
			}
			if result.ArgumentMessages == nil {
				result.ArgumentMessages = map[string]Descriptor{}
			}
			result.ArgumentMessages[key] = copy(child, depth+1)
		}
		return result
	}
	return copy(d, 0)
}

// MessageFromDescriptor retains the full original fallback and error graph.
func MessageFromDescriptor(d Descriptor, fallback string, cause error) *Message {
	return &Message{descriptor: CopyDescriptor(d, nil), fallback: fallback, cause: cause}
}

// UnmarshalJSON treats each optional child independently; malformed composition
// never invalidates an otherwise valid descriptor or response.
func (d *Descriptor) UnmarshalJSON(data []byte) error {
	nodes := 64
	value, err := decodeDescriptor(data, 0, &nodes)
	if err == nil {
		*d = value
	}
	return err
}
func decodeDescriptor(data []byte, depth int, nodes *int) (Descriptor, error) {
	var raw struct {
		ID       string                     `json:"id"`
		Args     map[string]any             `json:"args"`
		Children map[string]json.RawMessage `json:"argument_messages"`
	}
	// Decode the optional children separately so a malformed shape is ignored.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Descriptor{}, err
	}
	if err := json.Unmarshal(fields["id"], &raw.ID); err != nil {
		return Descriptor{}, err
	}
	if args, ok := fields["args"]; ok {
		if err := json.Unmarshal(args, &raw.Args); err != nil {
			return Descriptor{}, err
		}
	}
	_ = json.Unmarshal(fields["argument_messages"], &raw.Children)
	result := Descriptor{ID: raw.ID, Args: raw.Args}
	if depth >= 8 {
		return result, nil
	}
	for _, name := range rawKeys(raw.Children) {
		child := raw.Children[name]
		if *nodes <= 0 {
			break
		}
		*nodes--
		if _, ok := raw.Args[name].(string); !ok {
			continue
		}
		decoded, err := decodeDescriptor(child, depth+1, nodes)
		if err != nil {
			continue
		}
		if result.ArgumentMessages == nil {
			result.ArgumentMessages = map[string]Descriptor{}
		}
		result.ArgumentMessages[name] = decoded
	}
	return result, nil
}

func descriptorKeys(values map[string]Descriptor) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func rawKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// WithErrorContext adds presentation context at an external diagnostic origin
// while preserving its legacy English Error string and wrapping identity.
func WithErrorContext(id, format string, cause error, values ...any) *Message {
	message := Errorf(id, format, values...)
	return MessageFromDescriptor(message.Descriptor(), cause.Error(), cause)
}
