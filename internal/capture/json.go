package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// DecodeJSON rejects duplicate object keys at every depth, trailing values,
// unknown fields and invalid UTF-8 before decoding private command inputs.
func DecodeJSON(data []byte, out any) error {
	invalid := errors.New("capture_request_invalid")
	if !utf8.Valid(data) {
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
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return invalid
	}
	return nil
}
