package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// DecodeJSON rejects duplicate object keys at every depth, trailing values,
// unknown fields and invalid UTF-8 before decoding private command inputs.
func DecodeJSON(data []byte, out any) error {
	invalid := errors.New("capture_request_invalid")
	if out == nil || reflect.TypeOf(out).Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() || !utf8.Valid(data) {
		return invalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var read func(int) error
	read = func(depth int) error {
		if depth > 64 {
			return invalid
		}
		token, e := d.Token()
		if e != nil {
			return invalid
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return invalid
					}
					k, ok := key.(string)
					if !ok || seen[k] {
						return invalid
					}
					seen[k] = true
					if read(depth+1) != nil {
						return invalid
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return invalid
				}
			case '[':
				for d.More() {
					if read(depth+1) != nil {
						return invalid
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return invalid
				}
			default:
				return invalid
			}
		}
		return nil
	}
	if read(0) != nil {
		return invalid
	}
	if _, e := d.Token(); e != io.EOF {
		return invalid
	}
	var value any
	if json.Unmarshal(data, &value) != nil || !exactJSONFields(value, reflect.TypeOf(out).Elem()) {
		return invalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return invalid
	}
	return nil
}

// encoding/json otherwise accepts case aliases such as Scope and SCOPE even
// when unknown fields are disallowed; the engine/helper contracts use exact keys.
func exactJSONFields(value any, typ reflect.Type) bool {
	if typ.Kind() == reflect.Pointer {
		return value == nil || exactJSONFields(value, typ.Elem())
	}
	if reflect.PointerTo(typ).Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
		return true
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			if key != "-" {
				if key == "" {
					key = f.Name
				}
				fields[key] = f.Type
			}
		}
		for key, v := range object {
			f, ok := fields[key]
			if !ok || !exactJSONFields(v, f) {
				return false
			}
		}
	case reflect.Map:
		if value == nil {
			return true
		}
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, v := range object {
			if !exactJSONFields(v, typ.Elem()) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		// []byte has JSON's base64-string representation.
		if typ.Elem().Kind() == reflect.Uint8 {
			return true
		}
		if value == nil {
			return true
		}
		array, ok := value.([]any)
		if !ok {
			return false
		}
		for _, v := range array {
			if !exactJSONFields(v, typ.Elem()) {
				return false
			}
		}
	}
	return true
}
