package menu

import "fmt"
import "encoding/json"
import "strings"
import "regexp"

import "github.com/ronigooja/Nagi/internal/i18n"

// menuText resolves an application-owned message using the session language.
// Resource values and external diagnostics are never translation keys.
func menuText(language i18n.Context, id, fallback string) string {
	return language.RenderDescriptor(i18n.Descriptor{ID: id}, fallback)
}

// menuFormat carries named scalar arguments through the typed catalog contract.
func menuFormat(language i18n.Context, id, fallback string, values ...any) string {
	original := fmt.Sprintf(fallback, values...)
	args := make(map[string]any, len(values))
	index := 0
	for _, token := range menuFormatSpec.FindAllString(fallback, -1) {
		if token == "%%" {
			continue
		}
		if index >= len(values) {
			return original
		}
		value := values[index]
		switch token[len(token)-1] {
		case 'd', 'f', 'g', 'e', 'b', 'o', 'x', 'X', 't':
		default:
			value = fmt.Sprintf(token, value)
		}
		index++
		names := menuArgumentNames[id]
		if index > len(names) {
			return original
		}
		args[names[index-1]] = value
	}
	return language.RenderDescriptor(i18n.Descriptor{ID: id, Args: args}, original)
}

var menuFormatSpec = regexp.MustCompile(`%[-+# 0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)

// message resolves only the descriptor attached to an existing string target.
// Unknown IDs and malformed optional metadata preserve the original value.
func (r Result) message(language i18n.Context, pointer, original string) string {
	var descriptor i18n.Descriptor
	raw, ok := r.Messages[pointer]
	if !ok {
		return original
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return original
	}
	if id, exists := fields["id"]; !exists || json.Unmarshal(id, &descriptor.ID) != nil || descriptor.ID == "" {
		return original
	}
	if arguments, exists := fields["args"]; exists {
		if len(arguments) == 0 || arguments[0] != '{' || json.Unmarshal(arguments, &descriptor.Args) != nil {
			return original
		}
	}

	return language.RenderDescriptor(descriptor, original)
}
func (r Result) errorText(language i18n.Context) string {
	if r.Error == nil {
		return ""
	}
	return r.message(language, "/error/message", r.Error.Message)
}
func messagePointer(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// Explicit requested content bypasses descriptor rendering, coloring and redaction.
func rawResult(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "logs":
		return true
	case "config":
		return len(args) > 1 && args[1] == "show"
	case "profile":
		return len(args) > 1 && (args[1] == "diff" || (args[1] == "override" && len(args) > 2 && args[2] == "show"))
	case "request-rewrite", "rewrite":
		return true
	}
	return false
}
