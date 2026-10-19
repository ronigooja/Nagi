package output

import (
	"encoding/json"
	"io"
	"time"

	"github.com/ronigooja/Nagi/internal/i18n"
)

// CaptureResult renders explicitly authorized original values in full.
type CaptureResult struct{ Value any }

func (c CaptureResult) MessageDescriptors() map[string]i18n.Descriptor {
	messages := map[string]i18n.Descriptor{}
	for pointer, descriptor := range CollectMessages(c.Value) {
		messages[pointer[len("/data"):]] = descriptor
	}
	return messages
}

func (c CaptureResult) MarshalJSON() ([]byte, error) { return json.Marshal(c.Value) }
func writeCapture(w io.Writer, c CaptureResult) error {
	b, e := json.Marshal(c.Value)
	if e != nil {
		return e
	}
	var v any
	if e = json.Unmarshal(b, &v); e != nil {
		return e
	}

	// Only Nagi-owned status prose is rendered. Captured original content is raw.
	if root, ok := v.(map[string]any); ok {
		for _, target := range []struct{ object, field string }{{"readiness", "message"}, {"status", "recovery"}} {
			if object, ok := root[target.object].(map[string]any); ok {
				if original, ok := object[target.field].(string); ok {
					object[target.field] = RenderMessageField(c, "/"+target.object+"/"+target.field, original, writerLanguage(w))
				}
			}
		}
	}
	var walk func(any)
	walk = func(x any) {
		switch a := x.(type) {
		case map[string]any:
			for k, v := range a {
				if k == "started" {
					if s, ok := v.(string); ok {
						if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
							a[k] = t.Local().Format("2006-01-02 15:04")
						}
					}
				} else {
					walk(v)
				}
			}
		case []any:
			for _, v := range a {
				walk(v)
			}
		}
	}
	walk(v)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
