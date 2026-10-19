package output

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// CollectMessages gathers only producer-associated descriptors. Existing string
// values, including raw capture/configuration content, are never interpreted.
// Invalid optional metadata is discarded independently of its siblings.
func CollectMessages(data any) map[string]i18n.Descriptor {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var target any
	if json.Unmarshal(encoded, &target) != nil {
		return nil
	}
	messages := map[string]i18n.Descriptor{}
	visited := map[visit]bool{}
	var walk func(reflect.Value, string, int)
	walk = func(v reflect.Value, path string, depth int) {
		if !v.IsValid() || depth > 64 {
			return
		}
		if v.Kind() == reflect.Interface {
			if !v.IsNil() {
				walk(v.Elem(), path, depth+1)
			}
			return
		}
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			key := visit{v.Type(), v.Pointer()}
			if visited[key] {
				return
			}
			visited[key] = true
			defer delete(visited, key)
		}
		if v.CanInterface() {
			if source, ok := v.Interface().(i18n.MessageSource); ok {
				for pointer, descriptor := range source.MessageDescriptors() {
					full := strings.TrimPrefix(path, "/data") + pointer
					if !i18n.ValidDescriptor(descriptor) || !stringTarget(target, full) {
						continue
					}
					safe := SafeMessage(i18n.MessageFromDescriptor(descriptor, "", nil)).Descriptor()
					messages[path+pointer] = safe
				}
			}
			if result, ok := v.Interface().(i18n.Result); ok {
				walk(reflect.ValueOf(result.Value), path, depth+1)
				return
			}
		}
		if v.Kind() == reflect.Pointer {
			walk(v.Elem(), path, depth+1)
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				f := v.Type().Field(i)
				if f.PkgPath != "" {
					continue
				}
				tag := strings.Split(f.Tag.Get("json"), ",")[0]
				if tag == "-" {
					continue
				}
				if tag == "" {
					tag = f.Name
				}
				walk(v.Field(i), path+"/"+pointerToken(tag), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), path+"/"+strconv.Itoa(i), depth+1)
			}
		case reflect.Map:
			if v.Type().Key().Kind() != reflect.String {
				return
			}
			iter := v.MapRange()
			for iter.Next() {
				walk(iter.Value(), path+"/"+pointerToken(iter.Key().String()), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(data), "/data", 0)
	if len(messages) == 0 {
		return nil
	}
	return messages
}

type visit struct {
	typ     reflect.Type
	pointer uintptr
}

func pointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
func stringTarget(value any, pointer string) bool {
	if pointer == "" {
		_, ok := value.(string)
		return ok
	}
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		for i := 0; i < len(token); i++ {
			if token[i] == '~' {
				if i+1 >= len(token) || (token[i+1] != '0' && token[i+1] != '1') {
					return false
				}
				i++
			}
		}
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch container := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = container[token]
			if !ok {
				return false
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(container) || strconv.Itoa(index) != token {
				return false
			}
			value = container[index]
		default:
			return false
		}
	}
	_, ok := value.(string)
	return ok
}

// RenderMessageField renders an explicitly annotated field, preserving fallback
// whenever the optional producer metadata is absent, unknown or malformed.
func RenderMessageField(source any, pointer, fallback string, locale i18n.Context) string {
	value := reflect.ValueOf(source)
	if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return fallback
	}
	if source, ok := source.(i18n.MessageSource); ok {
		if descriptor, exists := source.MessageDescriptors()[pointer]; exists && i18n.ValidDescriptor(descriptor) {
			safe := SafeMessage(i18n.MessageFromDescriptor(descriptor, fallback, nil))
			return locale.Render(safe)
		}
	}
	return fallback
}
